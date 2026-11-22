package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v2"
)

// TestFuzzTargetCoverage keeps the smoke and scheduled runs aligned with real fuzz functions.
func TestFuzzTargetCoverage(t *testing.T) {
	var actual []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".")) && path != "." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, name := range fuzzFunctions(f) {
			actual = append(actual, "./"+filepath.ToSlash(filepath.Dir(path))+":"+name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(actual)
	check := func(label string, got []string) {
		t.Helper()
		slices.Sort(got)
		if !slices.Equal(actual, got) {
			t.Errorf("%s targets = %v; actual = %v", label, got, actual)
		}
	}
	out, err := exec.Command("just", "--dump", "--dump-format", "json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var dump struct {
		Recipes map[string]struct {
			Dependencies []struct {
				Recipe    string   `json:"recipe"`
				Arguments []string `json:"arguments"`
			} `json:"dependencies"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(out, &dump); err != nil {
		t.Fatal(err)
	}
	var deps []string
	for _, dep := range dump.Recipes["fuzz-smoke"].Dependencies {
		if dep.Recipe != "fuzz-one" || len(dep.Arguments) != 2 {
			t.Fatalf("unexpected fuzz-smoke dependency: %+v", dep)
		}
		deps = append(deps, dep.Arguments[0]+":"+dep.Arguments[1])
	}
	check("just", deps)
	for _, workflow := range []struct{ file, job string }{{".github/workflows/ci.yml", "fuzz-smoke"}, {".github/workflows/fuzz.yml", "fuzz"}} {
		data, err := os.ReadFile(workflow.file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Jobs map[string]struct {
				Strategy struct {
					Matrix struct {
						Include []struct {
							Package string `yaml:"package"`
							Target  string `yaml:"target"`
						} `yaml:"include"`
					} `yaml:"matrix"`
				} `yaml:"strategy"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		var targets []string
		for _, entry := range doc.Jobs[workflow.job].Strategy.Matrix.Include {
			targets = append(targets, entry.Package+":"+entry.Target)
		}
		check(workflow.file, targets)
	}
}

func fuzzFunctions(f *ast.File) []string {
	var names []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Fuzz") {
			continue
		}
		suffix := strings.TrimPrefix(fn.Name.Name, "Fuzz")
		first, _ := utf8.DecodeRuneInString(suffix)
		if suffix != "" && unicode.IsLower(first) {
			continue
		}
		if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) > 1 {
			continue
		}
		ptr, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		switch typ := ptr.X.(type) {
		case *ast.Ident:
			if typ.Name == "F" {
				names = append(names, fn.Name.Name)
			}
		case *ast.SelectorExpr:
			if typ.Sel.Name == "F" {
				names = append(names, fn.Name.Name)
			}
		}
	}
	return names
}

func TestFuzzFunctionDiscovery(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "fixture_test.go", `package fixture
import testalias "testing"
func Fuzz(f *testalias.F) {}
func FuzzAlias(f *testalias.F) {}
func Fuzzlower(f *testalias.F) {}
func Fuzzé(f *testalias.F) {}
func FuzzBad(f *testalias.F) int { return 0 }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fuzzFunctions(f), []string{"Fuzz", "FuzzAlias"}; !slices.Equal(got, want) {
		t.Errorf("discovered %v, want %v", got, want)
	}
}

func TestFuzzOneRequiresExecution(t *testing.T) {
	for _, target := range []string{"Fuzz", "FuzzDoesNotExist", "FuzzParseEnvelope\nOther"} {
		t.Run(target, func(t *testing.T) {
			cmd := exec.Command("just", "fuzz-one", "./internal/logship/syslog", target, "1x", "30s")
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Errorf("invalid target returned success: %s", out)
			}
			if target != "FuzzParseEnvelope\nOther" && !strings.Contains(string(out), target) {
				t.Errorf("missing target is absent from diagnostic: %s", out)
			}
		})
	}
	t.Run("saved skip", func(t *testing.T) {
		envFile := filepath.Join(t.TempDir(), "goenv")
		if err := os.WriteFile(envFile, []byte("GOFLAGS=-skip=.\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("just", "fuzz-one", "./internal/logship/syslog", "FuzzParseEnvelope", "1x", "30s")
		cmd.Env = append(os.Environ(), "GOENV="+envFile, "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fuzz failed: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "fuzz: elapsed:") {
			t.Errorf("fuzz did not execute: %s", out)
		}
	})
	t.Run("inherited skip", func(t *testing.T) {
		cmd := exec.Command("just", "fuzz-one", "./internal/logship/syslog", "FuzzParseEnvelope", "1x", "30s")
		cmd.Env = append(os.Environ(), "GOFLAGS=-skip=.")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fuzz failed: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "fuzz: elapsed:") {
			t.Errorf("fuzz did not execute: %s", out)
		}
	})
}

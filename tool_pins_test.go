package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/repotest"
	"go.yaml.in/yaml/v2"
)

func TestToolPinsAreSingleSourced(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	checkCloudScriptPins(t, root)
	// Match install commands, including module paths and release download URLs.
	literal := regexp.MustCompile(`(?i)(syft|goreleaser|go-licenses|govulncheck|kubeconform)[^\n]*(@v[0-9]|[/_-]v[0-9])`)
	err = filepath.WalkDir(filepath.Join(root, ".github", "workflows"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") && literal.MatchString(line) {
				t.Errorf("%s:%d: literal tool version: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		var wf struct {
			Jobs map[string]struct {
				Uses  string                 `yaml:"uses"`
				With  map[string]interface{} `yaml:"with"`
				Steps []struct {
					Uses string                 `yaml:"uses"`
					With map[string]interface{} `yaml:"with"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &wf); err != nil {
			return err
		}
		for name, job := range wf.Jobs {
			if strings.Contains(job.Uses, "/workflows/binaries.yml@") {
				version, ok := job.With["goreleaser-version"].(string)
				if !ok || version != "${{ needs.tool-pins.outputs.goreleaser }}" {
					t.Errorf("%s: job %s: goreleaser-version must use the validated tool-pins output", path, name)
				}
				preCmd, ok := job.With["pre-cmd"].(string)
				if !ok || preCmd != "go install github.com/anchore/syft/cmd/syft@${{ needs.tool-pins.outputs.syft }}" {
					t.Errorf("%s: job %s: pre-cmd must install syft from the validated tool-pins output", path, name)
				}
			}
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "actions/setup-go@") {
					if _, ok := step.With["go-version"]; ok {
						t.Errorf("%s: job %s: setup-go has go-version", path, name)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checkWorkflowsLeaveLintToJust(t, root)
	checkPinnedLintRecipe(t, root)
	checkRequiredLintJob(t, filepath.Join(root, ".github", "workflows", "ci.yml"))
}

// checkWorkflowsLeaveLintToJust is one blanket rule: outside a whole-line comment,
// no workflow or local action may name golangci at all, so no action, image,
// installer or module path can bring a second version.
func checkWorkflowsLeaveLintToJust(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{"workflows", "actions"} {
		err := filepath.WalkDir(filepath.Join(root, ".github", dir), func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(data), "\n") {
				text := strings.TrimSpace(line)
				if !strings.HasPrefix(text, "#") && strings.Contains(strings.ToLower(text), "golangci") {
					t.Errorf("%s:%d: golangci-lint must run through just lint: %s", path, i+1, text)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// checkCloudScriptPins requires each cloud script's route to the justfile pin and
// rejects any line that gives golangci-lint a version of its own.
func checkCloudScriptPins(t *testing.T, root string) {
	t.Helper()
	versionLike := regexp.MustCompile(`(?i)\bv?[0-9]+\.[0-9]|=["']?v?[0-9]|@["']?v?["']?[0-9]|@["']?latest`)
	bareVersion := regexp.MustCompile(`(?i)(^|[\s;])version=["']?v?[0-9]`)
	for path, pinRoute := range map[string]string{
		"scripts/cloud-environment-setup.sh": `version="$(golangci_lint_pin justfile)"`,
		".cursor/cloud-bootstrap.sh":         "just _tool-golangci-lint",
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		routed := false
		for i, line := range strings.Split(string(data), "\n") {
			text := strings.TrimSpace(line)
			if strings.HasPrefix(text, "#") {
				continue
			}
			routed = routed || text == pinRoute
			names := strings.Contains(strings.ToLower(text), "golangci")
			if bareVersion.MatchString(text) || (names && versionLike.MatchString(text)) ||
				(names && strings.Contains(text, "@") && !strings.HasSuffix(text, `@${version}"`)) {
				t.Errorf("%s:%d: independent golangci-lint version: %s", path, i+1, text)
			}
		}
		if !routed {
			t.Errorf("%s must take the golangci-lint version through %s", path, pinRoute)
		}
	}
}

// checkPinnedLintRecipe requires one annotated pin and a lint recipe that runs
// the binary installed from it, never whatever is on PATH.
func checkPinnedLintRecipe(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "justfile"))
	if err != nil {
		t.Fatal(err)
	}
	pin := regexp.MustCompile(`(?m)^# renovate: datasource=github-releases depName=golangci/golangci-lint\ngolangci_lint_version := "v[0-9]+\.[0-9]+\.[0-9]+"$`)
	declarations := regexp.MustCompile(`(?m)^\s*golangci_lint_version\s*:=`)
	if len(pin.FindAll(data, -1)) != 1 || len(declarations.FindAll(data, -1)) != 1 {
		t.Error("justfile must declare golangci_lint_version once, under its Renovate annotation")
	}
	if regexp.MustCompile(`(?m)^[ \t]*set[ \t]+allow-duplicate-recipes`).Match(data) {
		t.Error("justfile must not allow duplicate recipes: a later lint would replace the pinned one")
	}
	if n := len(regexp.MustCompile(`(?m)^@?lint(?:[ \t][^:\n]*)?:(?:[^=]|$)`).FindAll(data, -1)); n != 1 {
		t.Errorf("justfile must define exactly one lint recipe, found %d", n)
	}
	installer := regexp.MustCompile(`(?m)^_tool-golangci-lint:\n((?:[ \t]+[^\n]*\n)+)`).FindSubmatch(data)
	if installer == nil {
		t.Fatal("justfile: _tool-golangci-lint recipe missing")
	}
	body := string(installer[1])
	if !regexp.MustCompile(`(?m)^export GOLANGCI_LINT_PIN := golangci_lint_version$`).Match(data) ||
		strings.Count(body, `version="$GOLANGCI_LINT_PIN"`) != 1 || !strings.Contains(body, `go install "$package@$version"`) {
		t.Error("justfile: _tool-golangci-lint must install the version from golangci_lint_version")
	}
	if regexp.MustCompile(`(?i)\bv?[0-9]+\.[0-9]|@["']?v?[0-9]|@["']?latest`).MatchString(body) {
		t.Error("justfile: _tool-golangci-lint holds a version literal or @latest")
	}
	recipe := regexp.MustCompile(`(?m)^lint: _tool-golangci-lint\n((?:[ \t]+\S[^\n]*\n)+)`).FindSubmatch(data)
	if recipe == nil {
		t.Fatal("justfile: lint must depend on _tool-golangci-lint")
	}
	const binary = `{{ quote(tools_dir / "golangci-lint") }}`
	want := []string{binary + " version", binary + " run ./..."}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(string(recipe[1])), "\n") {
		got = append(got, strings.TrimSpace(line))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("justfile: lint recipe = %q, want %q", got, want)
	}
}

// lintStepInputs names the inputs each setup step of the Lint job may take.
func lintStepInputs(uses string) (map[string]bool, bool) {
	switch {
	case strings.HasPrefix(uses, "actions/checkout@"):
		return map[string]bool{"persist-credentials": true}, true
	case strings.HasPrefix(uses, "actions/setup-go@"):
		return map[string]bool{"go-version-file": true}, true
	case strings.HasPrefix(uses, "rknightion/.github/.github/actions/setup-just@"):
		return map[string]bool{}, true
	}
	return nil, false
}

// checkRequiredLintJob proves that every pull request runs just lint: the job is
// required by ci-success and nothing can skip, soften or redirect its steps.
func checkRequiredLintJob(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		On       map[string]interface{}            `yaml:"on"`
		Defaults interface{}                       `yaml:"defaults"`
		Env      interface{}                       `yaml:"env"`
		Jobs     map[string]map[string]interface{} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	trigger, ok := wf.On["pull_request"]
	if !ok {
		t.Errorf("%s: workflow must run on pull_request", path)
	}
	if filters, ok := trigger.(map[interface{}]interface{}); ok {
		for _, filter := range []string{"paths", "paths-ignore", "branches-ignore"} {
			if _, ok := filters[filter]; ok {
				t.Errorf("%s: pull_request must not filter on %s", path, filter)
			}
		}
	}
	if wf.Defaults != nil || wf.Env != nil {
		t.Errorf("%s: workflow defaults or env could redirect just lint", path)
	}
	required := false
	if needs, ok := wf.Jobs["ci-success"]["needs"].([]interface{}); ok {
		for _, need := range needs {
			required = required || need == "lint"
		}
	}
	if !required {
		t.Errorf("%s: ci-success must need the lint job", path)
	}
	if wf.Jobs["ci-success"]["if"] != "always()" {
		t.Errorf("%s: ci-success must keep if: always()", path)
	}
	job, ok := wf.Jobs["lint"]
	if !ok {
		t.Fatalf("%s: Lint job missing", path)
	}
	for key := range job {
		switch key {
		case "name", "runs-on", "steps", "timeout-minutes", "permissions":
		default:
			t.Errorf("%s: Lint job key %q could skip or change just lint", path, key)
		}
	}
	steps, _ := job["steps"].([]interface{})
	recipeRuns := 0
	for i, raw := range steps {
		step, _ := raw.(map[interface{}]interface{})
		if run, ok := step["run"].(string); ok && strings.TrimSpace(run) == "just lint" {
			recipeRuns++
			for key := range step {
				if key != "name" && key != "run" {
					t.Errorf("%s: Lint step %d key %v could skip or change just lint", path, i, key)
				}
			}
			continue
		}
		uses, _ := step["uses"].(string)
		allowedWith, known := lintStepInputs(uses)
		if !known {
			t.Errorf("%s: Lint step %d is not checkout, setup-go, setup-just or just lint", path, i)
		}
		with, _ := step["with"].(map[interface{}]interface{})
		for key := range with {
			// Action inputs are case-insensitive.
			if input, _ := key.(string); !allowedWith[strings.ToLower(input)] {
				t.Errorf("%s: Lint step %d must not set input %v", path, i, key)
			}
		}
		for _, key := range []string{"if", "continue-on-error"} {
			if _, ok := step[key]; ok {
				t.Errorf("%s: Lint step %d must not set %s", path, i, key)
			}
		}
	}
	if recipeRuns != 1 {
		t.Errorf("%s: Lint job must run just lint exactly once, found %d", path, recipeRuns)
	}
}

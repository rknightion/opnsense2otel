package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// linterConfigRef matches a reference to the linter's configuration file. It
// carries no version, so a paths filter or hashFiles call may name it.
var linterConfigRef = regexp.MustCompile(`(?i)\.golangci\.(ya?ml|toml|json)\b`)

// workflowBringsLinter reports whether a workflow line, outside a whole-line
// comment, names the linter other than through its configuration file: any
// action, image, installer, module path or command could bring a second version.
func workflowBringsLinter(line string) bool {
	text := strings.TrimSpace(line)
	if strings.HasPrefix(text, "#") {
		return false
	}
	return strings.Contains(strings.ToLower(linterConfigRef.ReplaceAllString(text, "")), "golangci")
}

// checkWorkflowsLeaveLintToJust is one blanket rule: outside a whole-line comment,
// no workflow or local action may name the linter except by its configuration
// file, so no action, image, installer or module path can bring a second version.
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
				if workflowBringsLinter(line) {
					t.Errorf("%s:%d: golangci-lint must run through just lint: %s", path, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

var (
	linterVersionLike = regexp.MustCompile(`(?i)\bv?[0-9]+\.[0-9]|=["']?v?[0-9]|@["']?v?["']?[0-9]|@["']?latest`)
	versionAssignment = regexp.MustCompile(`(?i)(^|[\s;])version=["']?v?[0-9]`)
	functionBoundary  = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*[ \t]*\(\)|\})`)
)

// cloudScriptVersionLines returns the 1-based lines of a cloud script that give
// golangci-lint a version of its own. A literal version= assignment counts only
// in a function or top-level block that also names golangci, so another tool's
// version in the same script is left alone.
func cloudScriptVersionLines(script string) []int {
	var bad []int
	var assignments []int
	blockNamesLinter := false
	flush := func() {
		if blockNamesLinter {
			bad = append(bad, assignments...)
		}
		assignments, blockNamesLinter = nil, false
	}
	for i, line := range strings.Split(script, "\n") {
		text := strings.TrimSpace(line)
		if functionBoundary.MatchString(line) {
			flush()
		}
		if strings.HasPrefix(text, "#") {
			continue
		}
		names := strings.Contains(strings.ToLower(text), "golangci")
		blockNamesLinter = blockNamesLinter || names
		if names && (linterVersionLike.MatchString(text) ||
			(strings.Contains(text, "@") && !strings.HasSuffix(text, `@${version}"`))) {
			bad = append(bad, i+1)
		} else if versionAssignment.MatchString(text) {
			assignments = append(assignments, i+1)
		}
	}
	flush()
	sort.Ints(bad)
	return bad
}

// TestCloudSetupRequirementsFilesExist keeps the cloud setup script from installing
// Python packages out of a requirements file the repository does not carry: pip stops
// at the first missing file, so the script never reached its end on a clean checkout.
func TestCloudSetupRequirementsFilesExist(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "scripts", "cloud-environment-setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	requirement := regexp.MustCompile(`(?:^|\s)-r\s+(\S+)`)
	found := 0
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, match := range requirement.FindAllStringSubmatch(line, -1) {
			found++
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(match[1]))); err != nil {
				t.Errorf("scripts/cloud-environment-setup.sh:%d: pip requirements file %s: %v", i+1, match[1], err)
			}
		}
	}
	if found == 0 {
		t.Error("scripts/cloud-environment-setup.sh installs no pip requirements file: the API contract tools need one")
	}
}

// checkCloudScriptPins requires each cloud script's route to the justfile pin and
// rejects any line that gives golangci-lint a version of its own.
func checkCloudScriptPins(t *testing.T, root string) {
	t.Helper()
	for path, pinRoute := range map[string]string{
		"scripts/cloud-environment-setup.sh": `version="$(golangci_lint_pin justfile)"`,
		".cursor/cloud-bootstrap.sh":         "just _tool-golangci-lint",
	} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		for _, n := range cloudScriptVersionLines(string(data)) {
			t.Errorf("%s:%d: independent golangci-lint version: %s", path, n, strings.TrimSpace(lines[n-1]))
		}
		routed := false
		for _, line := range lines {
			text := strings.TrimSpace(line)
			routed = routed || (!strings.HasPrefix(text, "#") && text == pinRoute)
		}
		if !routed {
			t.Errorf("%s must take the golangci-lint version through %s", path, pinRoute)
		}
	}
}

func TestToolPinRulesOnFixtures(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	justfile, err := os.ReadFile(filepath.Join(root, "justfile"))
	if err != nil {
		t.Fatal(err)
	}
	if problems := pinnedLintRecipeProblems(justfile); len(problems) != 0 {
		t.Fatalf("the real justfile must pass before mutating it: %q", problems)
	}
	// The installer recipe ends at the last indented line before the next recipe.
	const anchor = "    fi\n\n[private]\n_tool-go-licenses:"
	const second = "    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v1.0.0\n"
	if strings.Count(string(justfile), anchor) != 1 {
		t.Fatal("fixture anchor missing after the installer recipe")
	}
	t.Run("justfile", func(t *testing.T) {
		installer := func(extra string) []byte {
			return []byte(strings.Replace(string(justfile), anchor, "    fi\n"+extra+"\n[private]\n_tool-go-licenses:", 1))
		}
		lint := func(extra string) []byte {
			return []byte(strings.Replace(string(justfile), "run ./...\n", "run ./...\n"+extra, 1))
		}
		for name, data := range map[string][]byte{
			"second install directly":        installer(second),
			"second install after blank":     installer("\n" + second),
			"second install after 2 blanks":  installer("\n\n" + second),
			"literal version after blank":    installer("\n    version=v1.0.0\n"),
			"latest install after blank":     installer("\n    go install \"$package@latest\"\n"),
			"extra lint command directly":    lint("    golangci-lint run ./...\n"),
			"extra lint command after blank": lint("\n    golangci-lint run ./...\n"),
		} {
			if len(pinnedLintRecipeProblems(data)) == 0 {
				t.Errorf("%s: mutation accepted", name)
			}
		}
	})
	t.Run("workflow lines", func(t *testing.T) {
		for line, rejected := range map[string]bool{
			"          - '.golangci.yml'":                                                           false,
			"          key: lint-${{ hashFiles('.golangci.yml', 'go.sum') }}":                       false,
			"      - .golangci.yaml":                                                                false,
			"      # uses: golangci/golangci-lint-action@v8":                                        false,
			"      - uses: golangci/golangci-lint-action@v8":                                        true,
			"      - uses: golangci/golangci-lint-action@abcdefabcdefabcdefabcdefabcdefabcdefabcd":  true,
			"        run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.0": true,
			"        run: golangci-lint run":                                                        true,
			"        image: golangci/golangci-lint:latest":                                          true,
			"        run: golangci-lint run --config .golangci.yml":                                 true,
			"      - uses: other/action@v1":                                                         false,
		} {
			if got := workflowBringsLinter(line); got != rejected {
				t.Errorf("workflowBringsLinter(%q) = %t, want %t", line, got, rejected)
			}
		}
	})
	t.Run("cloud scripts", func(t *testing.T) {
		for name, tc := range map[string]struct {
			script string
			bad    []int
		}{
			"unrelated tool version":                 {"install_helm() {\n  local version\n  version=3.14.0\n}\ninstall_golangci_lint() {\n  version=\"$(golangci_lint_pin justfile)\"\n  go install \"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${version}\"\n}\n", nil},
			"literal version in the linter function": {"install_golangci_lint() {\n  version=v1.0.0\n  go install \"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${version}\"\n}\n", []int{2}},
			"literal version beside a golangci line": {"version=v1.0.0 golangci-lint version\n", []int{1}},
			"literal pin in the install line":        {"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v1.0.0\n", []int{1}},
			"latest":                                 {"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest\n", []int{1}},
			"commented assignment":                   {"install_golangci_lint() {\n  # version=v1.0.0\n  golangci-lint version\n}\n", nil},
		} {
			got := cloudScriptVersionLines(tc.script)
			if fmt.Sprint(got) != fmt.Sprint(tc.bad) {
				t.Errorf("%s: lines %v, want %v", name, got, tc.bad)
			}
		}
	})
}

// recipeBody returns the body of the justfile recipe whose header line matches
// header, with blank lines kept out. just ends a recipe at the next unindented
// line, never at a blank one, so a command after a blank line still runs.
func recipeBody(justfile []byte, header *regexp.Regexp) (string, bool) {
	lines := strings.Split(string(justfile), "\n")
	for i, line := range lines {
		if !header.MatchString(line) {
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			if next[0] != ' ' && next[0] != '\t' {
				break
			}
			body = append(body, next)
		}
		return strings.Join(body, "\n") + "\n", true
	}
	return "", false
}

// pinnedLintRecipeProblems requires one annotated pin and a lint recipe that runs
// the binary installed from it, never whatever is on PATH.
func pinnedLintRecipeProblems(data []byte) []string {
	var problems []string
	pin := regexp.MustCompile(`(?m)^# renovate: datasource=github-releases depName=golangci/golangci-lint\ngolangci_lint_version := "v[0-9]+\.[0-9]+\.[0-9]+"$`)
	declarations := regexp.MustCompile(`(?m)^\s*golangci_lint_version\s*:=`)
	if len(pin.FindAll(data, -1)) != 1 || len(declarations.FindAll(data, -1)) != 1 {
		problems = append(problems, "justfile must declare golangci_lint_version once, under its Renovate annotation")
	}
	if regexp.MustCompile(`(?m)^[ \t]*set[ \t]+allow-duplicate-recipes`).Match(data) {
		problems = append(problems, "justfile must not allow duplicate recipes: a later lint would replace the pinned one")
	}
	if n := len(regexp.MustCompile(`(?m)^@?lint(?:[ \t][^:\n]*)?:(?:[^=]|$)`).FindAll(data, -1)); n != 1 {
		problems = append(problems, fmt.Sprintf("justfile must define exactly one lint recipe, found %d", n))
	}
	body, ok := recipeBody(data, regexp.MustCompile(`^_tool-golangci-lint:$`))
	if !ok {
		return append(problems, "justfile: _tool-golangci-lint recipe missing")
	}
	if !regexp.MustCompile(`(?m)^export GOLANGCI_LINT_PIN := golangci_lint_version$`).Match(data) ||
		strings.Count(body, `version="$GOLANGCI_LINT_PIN"`) != 1 || !strings.Contains(body, `go install "$package@$version"`) {
		problems = append(problems, "justfile: _tool-golangci-lint must install the version from golangci_lint_version")
	}
	if regexp.MustCompile(`(?i)\bv?[0-9]+\.[0-9]|@["']?v?[0-9]|@["']?latest`).MatchString(body) {
		problems = append(problems, "justfile: _tool-golangci-lint holds a version literal or @latest")
	}
	lint, ok := recipeBody(data, regexp.MustCompile(`^lint: _tool-golangci-lint$`))
	if !ok {
		return append(problems, "justfile: lint must depend on _tool-golangci-lint")
	}
	const binary = `{{ quote(tools_dir / "golangci-lint") }}`
	want := []string{binary + " version", binary + " run ./..."}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(lint), "\n") {
		got = append(got, strings.TrimSpace(line))
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		problems = append(problems, fmt.Sprintf("justfile: lint recipe = %q, want %q", got, want))
	}
	return problems
}

func checkPinnedLintRecipe(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "justfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range pinnedLintRecipeProblems(data) {
		t.Error(problem)
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

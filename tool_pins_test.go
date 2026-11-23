package main

import (
	"fmt"
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
	versionExpansion  = regexp.MustCompile(`@["']?\$\{?version\b`)
	functionHeader    = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)[ \t]*\(\)`)
)

const (
	setupPinRoute      = `version="$(golangci_lint_pin justfile)"`
	setupInstallFunc   = "install_golangci_lint"
	setupInstallSuffix = `@${version}"`
)

// cloudScriptVersionLines returns the 1-based lines of a cloud script that give
// golangci-lint a version of its own. A line naming golangci may carry no version
// literal, and may name a module version only as the go install line that sits
// directly after the pin route inside install_golangci_lint. A version=
// assignment elsewhere is another tool's and is left alone, but it cannot reach
// the linter: any other line installing a module at the shell variable is
// rejected too.
func cloudScriptVersionLines(script string) []int {
	var bad []int
	function, previous := "", ""
	for i, line := range strings.Split(script, "\n") {
		text := strings.TrimSpace(line)
		if match := functionHeader.FindStringSubmatch(line); match != nil {
			function = match[1]
		} else if strings.HasPrefix(line, "}") {
			function = ""
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		approved := function == setupInstallFunc && previous == setupPinRoute &&
			strings.HasSuffix(text, setupInstallSuffix)
		names := strings.Contains(strings.ToLower(text), "golangci")
		switch {
		case approved:
		case names && (linterVersionLike.MatchString(text) || strings.Contains(text, "@")),
			versionExpansion.MatchString(text):
			bad = append(bad, i+1)
		}
		previous = text
	}
	return bad
}

type requirementRef struct {
	line int
	path string
}

var pipInstall = regexp.MustCompile(`(?:^|\s)pip[0-9.]*\s+install(?:\s|$)`)

// cloudScriptRequirements returns the requirements files a cloud script hands to
// pip install, through -r or --requirement (also in the = and attached forms).
// A -r flag on any other command, such as read -r, is not one.
func cloudScriptRequirements(script string) []requirementRef {
	var refs []requirementRef
	var command strings.Builder
	start := 0
	for i, line := range strings.Split(script, "\n") {
		if command.Len() == 0 {
			start = i + 1
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") && command.Len() == 0 {
			continue
		}
		if joined, more := strings.CutSuffix(strings.TrimRight(line, " \t"), "\\"); more {
			command.WriteString(joined + " ")
			continue
		} else {
			command.WriteString(joined)
		}
		text := command.String()
		command.Reset()
		loc := pipInstall.FindStringIndex(text)
		if loc == nil {
			continue
		}
		args := text[loc[1]:]
		if end := strings.IndexAny(args, ";&|"); end >= 0 {
			args = args[:end]
		}
		tokens := strings.Fields(args)
		for k, token := range tokens {
			path := ""
			switch {
			case token == "-r" || token == "--requirement":
				if k+1 < len(tokens) {
					path = tokens[k+1]
				}
			case strings.HasPrefix(token, "--requirement="):
				path = strings.TrimPrefix(token, "--requirement=")
			case strings.HasPrefix(token, "-r") && !strings.HasPrefix(token, "--"):
				path = strings.TrimPrefix(token, "-r")
			}
			if path = strings.Trim(path, `"'`); path != "" {
				refs = append(refs, requirementRef{start, path})
			}
		}
	}
	return refs
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
	found := 0
	for _, ref := range cloudScriptRequirements(string(data)) {
		found++
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref.path))); err != nil {
			t.Errorf("scripts/cloud-environment-setup.sh:%d: pip requirements file %s: %v", ref.line, ref.path, err)
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
	t.Run("pip requirements", func(t *testing.T) {
		const pip = "  python -m pip install --quiet \\\n"
		for name, tc := range map[string]struct {
			script string
			want   []string
		}{
			"short flag on a continuation":     {pip + "    -r tools/a.txt\n", []string{"tools/a.txt"}},
			"long flag":                        {pip + "    --requirement scripts/b.txt\n", []string{"scripts/b.txt"}},
			"long flag with equals":            {"pip install --requirement=scripts/b.txt\n", []string{"scripts/b.txt"}},
			"attached short flag":              {"pip install -rscripts/c.txt\n", []string{"scripts/c.txt"}},
			"quoted path":                      {"pip3 install -r \"scripts/d.txt\"\n", []string{"scripts/d.txt"}},
			"read -r is not pip":               {"read -r first_line < justfile\n", nil},
			"read -r beside a pip install":     {"read -r first_line < justfile\npip install pkg\n", nil},
			"commented pip install":            {"# pip install -r scripts/e.txt\n", nil},
			"flag after a separator is no pip": {"pip install pkg && read -r x < justfile\n", nil},
		} {
			var got []string
			for _, ref := range cloudScriptRequirements(tc.script) {
				got = append(got, ref.path)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("%s: files %v, want %v", name, got, tc.want)
			}
		}
	})
	t.Run("cloud scripts", func(t *testing.T) {
		const setupHead = "install_golangci_lint() {\n  local version\n  version=\"$(golangci_lint_pin justfile)\"\n"
		const setupInstall = "  go install \"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${version}\"\n}\n"
		for name, tc := range map[string]struct {
			script string
			bad    []int
		}{
			"real shape": {setupHead + setupInstall, nil},
			"comment and blank between pin and install":        {setupHead + "\n  # install\n" + setupInstall, nil},
			"second version through a helper call":             {"set_release() {\n  version=v1.0.0\n}\n" + setupHead + "  set_release\n" + setupInstall, []int{8}},
			"literal assignment between pin and install":       {setupHead + "  version=v1.0.0\n" + setupInstall, []int{5}},
			"install outside the linter function":              {strings.Replace(setupHead, "install_golangci_lint", "install_other", 1) + setupInstall, []int{4}},
			"install through a package variable":               {"version=v1.0.0\npkg=github.com/golangci/golangci-lint/v2/cmd/golangci-lint\ngo install \"$pkg@${version}\"\n", []int{3}},
			"unrelated assignment in a script of no functions": {"version=1.2.3\njust _tool-govulncheck\njust _tool-golangci-lint\n", nil},
			"unrelated assignment before go mod download":      {setupHead + setupInstall + "version=3.14.0\ngo mod download\n", nil},
			"unrelated tool version":                           {"install_helm() {\n  local version\n  version=3.14.0\n}\n" + setupHead + setupInstall, nil},
			"literal version in the linter function":           {"install_golangci_lint() {\n  version=v1.0.0\n  go install \"github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${version}\"\n}\n", []int{3}},
			"literal version beside a golangci line":           {"version=v1.0.0 golangci-lint version\n", []int{1}},
			"literal pin in the install line":                  {"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v1.0.0\n", []int{1}},
			"latest":                                           {"go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest\n", []int{1}},
			"commented assignment":                             {"install_golangci_lint() {\n  # version=v1.0.0\n  golangci-lint version\n}\n", nil},
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

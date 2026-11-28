package repotest_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/repotest"
	"go.yaml.in/yaml/v2"
)

// appGate is the condition of an application suite that the fast paths skip
// (rknightion/.github docs/changes/README.md). A job gated on it counts as running a
// check leg only because ci-success's verdict step fails when the classifier gave no
// verdict, or said the suites had to run and one did not succeed.
const appGate = "needs.changes.outputs.app == 'true'"

type ciStep struct {
	Name            string            `yaml:"name"`
	Env             map[string]string `yaml:"env"`
	Run             string            `yaml:"run"`
	If              string            `yaml:"if"`
	ContinueOnError string            `yaml:"continue-on-error"`
}

type ciJob struct {
	Uses            string            `yaml:"uses"`
	With            map[string]string `yaml:"with"`
	Needs           []string          `yaml:"needs"`
	If              string            `yaml:"if"`
	ContinueOnError string            `yaml:"continue-on-error"`
	Strategy        struct {
		Matrix struct {
			Exclude []any `yaml:"exclude"`
			Include []struct {
				Package string `yaml:"package"`
				Target  string `yaml:"target"`
			} `yaml:"include"`
		} `yaml:"matrix"`
	} `yaml:"strategy"`
	Steps []ciStep `yaml:"steps"`
}

func readCI(t *testing.T, root string) map[string]ciJob {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]ciJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow.Jobs
}

var suiteResult = regexp.MustCompile(`\$\{\{ needs\.([a-z0-9-]+)\.result \}\}`)

// verdictSuites returns the jobs that ci-success's fast-path verdict step holds to
// their result whenever the classifier says the application suites must run, or an
// error when that step is missing or would not fail on a missing verdict. A skipped
// suite is evidence of nothing unless it is listed here.
func verdictSuites(jobs map[string]ciJob) (map[string]bool, error) {
	success := jobs["ci-success"]
	if !contains(success.Needs, "changes") {
		return nil, fmt.Errorf("ci-success does not need changes")
	}
	for _, step := range success.Steps {
		if step.Env["CHANGES"] != "${{ needs.changes.result }}" || step.Env["APP"] != "${{ needs.changes.outputs.app }}" {
			continue
		}
		if step.If != "" || (step.ContinueOnError != "" && step.ContinueOnError != "false") {
			return nil, fmt.Errorf("verdict step %q is conditional", step.Name)
		}
		for _, guard := range []string{
			`"$CHANGES" != success || ( "$APP" != true && "$APP" != false )`,
			`for result in $SUITES; do`,
			`"$APP" == true && "$result" != success`,
		} {
			if !strings.Contains(step.Run, guard) {
				return nil, fmt.Errorf("verdict step %q lacks %s", step.Name, guard)
			}
		}
		suites := make(map[string]bool)
		for _, match := range suiteResult.FindAllStringSubmatch(step.Env["SUITES"], -1) {
			suites[match[1]] = true
		}
		if len(suites) == 0 {
			return nil, fmt.Errorf("verdict step %q lists no suites", step.Name)
		}
		return suites, nil
	}
	return nil, fmt.Errorf("ci-success has no fast-path verdict step")
}

// entries are the lines of a multi-line input as the classifier reads them: blank
// lines and '#' comments dropped.
func entries(input string) []string {
	var out []string
	for _, line := range strings.Split(input, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// TestCIFastPath pins the caller pattern of rknightion/.github's changes classifier:
// the application suites are gated on it, lint, security and the pull request checks
// are not, and ci-success fails when changes gave no verdict or a suite that had to
// run did not pass.
func TestCIFastPath(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	jobs := readCI(t, root)
	changes, ok := jobs["changes"]
	if !ok {
		t.Fatal("ci.yml has no changes job")
	}
	if !regexp.MustCompile(`^rknightion/\.github/\.github/workflows/changes\.yml@[0-9a-f]{40}$`).MatchString(changes.Uses) {
		t.Errorf("changes calls %q, want the hub changes.yml pinned by SHA", changes.Uses)
	}
	if changes.If != "" {
		t.Errorf("changes is conditional (%q): it must run on every event", changes.If)
	}
	suites, err := verdictSuites(jobs)
	if err != nil {
		t.Fatal(err)
	}
	needed := jobs["ci-success"].Needs
	for _, name := range needed {
		job := jobs[name]
		if !strings.Contains(job.If, "needs.changes.") {
			if suites[name] {
				t.Errorf("%s is in the verdict suites but is not gated on changes", name)
			}
			continue
		}
		if !contains(job.Needs, "changes") || !suites[name] {
			t.Errorf("%s is gated on changes but ci-success's verdict step does not hold it to its result", name)
		}
	}
	for name := range suites {
		if !contains(needed, name) {
			t.Errorf("verdict suite %s is not needed by ci-success", name)
		}
	}
	// Lint, security and the pull request checks always run.
	for _, name := range []string{"repo-meta", "lint", "govulncheck", "privacy-sweep", "zizmor"} {
		if !contains(needed, name) {
			t.Errorf("ci-success does not need %s", name)
		}
		if strings.Contains(jobs[name].If, "needs.changes.") {
			t.Errorf("%s is gated on changes; it must always run", name)
		}
	}
	// Every extra file release-please bumps is a version file, or a release pull request
	// could never take the fast path.
	config, err := os.ReadFile(filepath.Join(root, "release-please-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rp struct {
		Packages map[string]struct {
			ExtraFiles []struct {
				Path string `json:"path"`
			} `json:"extra-files"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(config, &rp); err != nil {
		t.Fatal(err)
	}
	versionFiles := entries(changes.With["version-files"])
	for _, pkg := range rp.Packages {
		for _, extra := range pkg.ExtraFiles {
			if !contains(versionFiles, extra.Path) {
				t.Errorf("release-please extra-file %s is not in changes' version-files", extra.Path)
			}
		}
	}
	// docs-check lints AGENTS.md and CONTRIBUTING.md and the docgen tests read AGENTS.md,
	// so a pull request changing either is never vended-only.
	for _, path := range entries(changes.With["vended-paths"]) {
		if path == "AGENTS.md" || path == "CONTRIBUTING.md" {
			t.Errorf("vended-paths lists %s, which gated jobs read", path)
		}
	}
}

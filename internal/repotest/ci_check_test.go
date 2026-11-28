package repotest_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/repotest"
)

func TestCIRunsEveryCheckLeg(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	justfile, err := os.ReadFile(filepath.Join(root, "justfile"))
	if err != nil {
		t.Fatal(err)
	}
	check := regexp.MustCompile(`(?m)^check:\s*([^\n]*)$`).FindStringSubmatch(string(justfile))
	if len(check) != 2 || len(strings.Fields(check[1])) == 0 {
		t.Fatal("cannot find check dependencies in justfile")
	}
	jobs := readCI(t, root)
	// A gated suite counts only when the verdict step holds it to its result; without
	// a valid verdict step no gated job counts.
	suites, err := verdictSuites(jobs)
	if err != nil {
		suites = nil
	}
	needed := jobs["ci-success"].Needs
	if len(needed) == 0 {
		t.Fatal("ci-success has no needs")
	}
	run := make(map[string]bool)
	// Only whole-step invocations count: a matching line inside a shell script
	// could be a heredoc or otherwise not execute as a required command.
	command := regexp.MustCompile(`^just[ \t]+([a-z][a-z0-9-]*)$`)
	fuzzTargets := make(map[string]bool)
	for _, name := range needed {
		job, ok := jobs[name]
		if !ok {
			t.Errorf("ci-success needs missing job %s", name)
			continue
		}
		gated := job.If == appGate && contains(job.Needs, "changes") && suites[name]
		if (job.If != "" && !gated) || (job.ContinueOnError != "" && job.ContinueOnError != "false") {
			continue
		}
		for _, step := range job.Steps {
			if step.If != "" || (step.ContinueOnError != "" && step.ContinueOnError != "false") {
				continue
			}
			invocation := strings.TrimSpace(step.Run)
			if len(job.Strategy.Matrix.Exclude) == 0 && invocation == `just fuzz-one "$FUZZ_PACKAGE" "$FUZZ_TARGET"` && step.Env["FUZZ_PACKAGE"] == "${{ matrix.package }}" && step.Env["FUZZ_TARGET"] == "${{ matrix.target }}" {
				for _, target := range job.Strategy.Matrix.Include {
					fuzzTargets[target.Package+" "+target.Target] = true
				}
			}
			if match := command.FindStringSubmatch(invocation); match != nil {
				run[match[1]] = true
			}
		}
	}
	// Equivalents must be present in unconditional jobs required by ci-success.
	// The repository metric scan is a Go test and just test runs all packages.
	defaultTest := exec.Command("just", "--dry-run", "test")
	defaultTest.Dir = root
	testCommand, err := defaultTest.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve default test recipe: %v", err)
	}
	covered := map[string]bool{
		"metric-lint": run["test"] && metricScanSelected(string(testCommand)),
		"gen-check":   run["docs-check"] && run["grafana-check"],
	}
	fuzzRecipe := regexp.MustCompile(`(?m)^fuzz-smoke:([^\n]*)$`).FindSubmatch(justfile)
	fuzzCall := regexp.MustCompile(`\(fuzz-one "([^"]+)" "([^"]+)"\)`)
	fuzzOK := len(fuzzRecipe) == 2
	if fuzzOK {
		calls := fuzzCall.FindAllSubmatch(fuzzRecipe[1], -1)
		fuzzOK = len(calls) > 0 && strings.TrimSpace(fuzzCall.ReplaceAllString(string(fuzzRecipe[1]), "")) == ""
		for _, call := range calls {
			if !fuzzTargets[fmt.Sprintf("%s %s", call[1], call[2])] {
				fuzzOK = false
			}
		}
	}
	covered["fuzz-smoke"] = fuzzOK
	for _, recipe := range strings.Fields(check[1]) {
		if !run[recipe] && !covered[recipe] {
			t.Errorf("check dependency %s is not run by a ci-success job", recipe)
		}
	}
}

// metricScanSelected conservatively accepts the known full-suite command only.
// Extra arguments could select away the repository metric scan.
func metricScanSelected(command string) bool {
	return strings.Join(strings.Fields(command), " ") == "go test -race -timeout 15m ./..."
}

func TestMetricScanSelection(t *testing.T) {
	cases := []struct {
		command  string
		selected bool
	}{
		{"go test -race -timeout 15m ./...", true},
		{"go test -race -timeout 15m -skip '^TestRepositoryMetricNames$' ./...", false},
		{"go test -race -timeout 15m -run '^TestOther$' ./...", false},
		{"go test -race -timeout 15m ./internal/repotest", false},
	}
	for _, tc := range cases {
		if got := metricScanSelected(tc.command); got != tc.selected {
			t.Errorf("metricScanSelected(%q) = %t, want %t", tc.command, got, tc.selected)
		}
	}
}

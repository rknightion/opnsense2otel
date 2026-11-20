package repotest_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/repotest"
	"go.yaml.in/yaml/v2"
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
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Needs           []string `yaml:"needs"`
			If              string   `yaml:"if"`
			ContinueOnError string   `yaml:"continue-on-error"`
			Steps           []struct {
				Run             string `yaml:"run"`
				If              string `yaml:"if"`
				ContinueOnError string `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	needed := workflow.Jobs["ci-success"].Needs
	if len(needed) == 0 {
		t.Fatal("ci-success has no needs")
	}
	run := make(map[string]bool)
	// Only whole-step invocations count: a matching line inside a shell script
	// could be a heredoc or otherwise not execute as a required command.
	command := regexp.MustCompile(`^just[ \t]+([a-z][a-z0-9-]*)$`)
	format := regexp.MustCompile(`^just[ \t]+--fmt[ \t]+--check$`)
	fmtJust := false
	for _, name := range needed {
		job, ok := workflow.Jobs[name]
		if !ok {
			t.Errorf("ci-success needs missing job %s", name)
			continue
		}
		if job.If != "" || (job.ContinueOnError != "" && job.ContinueOnError != "false") {
			continue
		}
		for _, step := range job.Steps {
			if step.If != "" || (step.ContinueOnError != "" && step.ContinueOnError != "false") {
				continue
			}
			invocation := strings.TrimSpace(step.Run)
			if match := command.FindStringSubmatch(invocation); match != nil {
				run[match[1]] = true
			}
			fmtJust = fmtJust || format.MatchString(invocation)
		}
	}
	// These recipes are covered by other CI steps, rather than invoked directly.
	covered := map[string]string{
		"fmt-check":   "golangci-lint formats Go; CI checks justfile formatting separately",
		"lint":        "the pinned golangci-lint action",
		"metric-lint": "repository scan test under just test",
		"fuzz-smoke":  "the fuzz matrix runs just fuzz-one",
		"gen-check":   "just docs-check and just grafana-check",
	}
	for _, recipe := range strings.Fields(check[1]) {
		if !run[recipe] && covered[recipe] == "" {
			t.Errorf("check dependency %s is not run by a ci-success job", recipe)
		}
	}
	if !fmtJust {
		t.Error("just --fmt --check is not run by a ci-success job")
	}
}

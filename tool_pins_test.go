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
}

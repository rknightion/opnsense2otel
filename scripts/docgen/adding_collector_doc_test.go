package main

import (
	"os"
	"strings"
	"testing"
)

func TestAddingCollectorGuideMatchesInterface(t *testing.T) {
	root := findRepoRoot()
	interfaceFile, err := os.ReadFile(root + "/internal/collector/collector.go")
	if err != nil {
		t.Fatal(err)
	}
	guide, err := os.ReadFile(root + "/docs/development/adding-collector.md")
	if err != nil {
		t.Fatal(err)
	}
	inInterface := false
	signature := ""
	for _, line := range strings.Split(string(interfaceFile), "\n") {
		line = strings.TrimSpace(line)
		if line == "type CollectorInstance interface {" {
			inInterface = true
			continue
		}
		if inInterface && line == "}" {
			break
		}
		if inInterface && strings.HasPrefix(line, "Update(") {
			signature = line
			break
		}
	}
	if signature == "" {
		t.Fatal("CollectorInstance Update signature not found")
	}
	if !strings.Contains(string(guide), signature) {
		t.Errorf("guide missing CollectorInstance signature: %s", signature)
	}
	if !strings.Contains(string(guide), "buildPrometheusDesc") {
		t.Error("guide missing buildPrometheusDesc")
	}
}

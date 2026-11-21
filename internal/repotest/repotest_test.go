package repotest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/repotest"
)

func TestDockerignoreCoversLocalCredentialFiles(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	patterns := []string{".env", ".env.*", "key", "secret", "local.docker-compose.yml"}
	for _, name := range []string{".gitignore", ".dockerignore"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lines := strings.Split(string(data), "\n")
		for _, pattern := range patterns {
			found := false
			for _, line := range lines {
				if line == pattern {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s missing whole line %q", name, pattern)
			}
		}
	}
}

func TestRootFindsTheModuleRoot(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("Root = %q, want an absolute path", root)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("Root = %q has no go.mod: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "repotest")); err != nil {
		t.Fatalf("Root = %q is not this repository root: %v", root, err)
	}
}

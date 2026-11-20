package webui

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The console must use snapshots rather than gathering the live registry.
func TestWebUINeverImportsPrometheusClient(t *testing.T) {
	if err := checkWebUIImports("."); err != nil {
		t.Fatal(err)
	}
}

func checkWebUIImports(root string) error {
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		count++
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if name == "github.com/prometheus/client_golang" || strings.HasPrefix(name, "github.com/prometheus/client_golang/") {
				return fmt.Errorf("%s imports Prometheus client %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no non-test Go files found under %s", root)
	}
	return nil
}

func TestWebUIImportGuardRejectsEmptyDirectory(t *testing.T) {
	if err := checkWebUIImports(t.TempDir()); err == nil {
		t.Fatal("import guard accepted a directory with no non-test Go files")
	}
}

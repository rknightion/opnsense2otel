package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryDocsPageIsInNav catches documentation pages that readers cannot find
// through the site navigation. Only site infrastructure and the testbed-only
// measurement contract are intentionally unlisted.
func TestEveryDocsPageIsInNav(t *testing.T) {
	root := findRepoRoot()
	f, err := os.Open(filepath.Join(root, "docs.toml"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	listed, err := docsNavPages(f)
	if err != nil {
		t.Fatal(err)
	}

	unlisted := map[string]string{
		"404.md":                    "site error page, not a navigation destination",
		"includes/abbreviations.md": "included abbreviation definitions, not a standalone page",
		"udp-throughput-harness.md": "testbed measurement contract, not an operator guide",
	}
	docs := filepath.Join(root, "docs")
	err = filepath.WalkDir(docs, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(docs, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !listed[rel] && unlisted[rel] == "" {
			t.Errorf("docs page %s is missing from site navigation", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// docsNavPages handles the current docs.toml navigation layout: one site.nav
// table per section and double-quoted paths, not arbitrary TOML syntax.
func docsNavPages(r io.Reader) (map[string]bool, error) {
	listed := make(map[string]bool)
	page := regexp.MustCompile(`"([^"\n]+\.md)"`)
	inNav := false
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(withoutTOMLComment(scanner.Text()))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inNav = line == "[[site.nav]]"
		}
		if inNav {
			for _, match := range page.FindAllStringSubmatch(line, -1) {
				listed[match[1]] = true
			}
		}
	}
	return listed, scanner.Err()
}

// withoutTOMLComment ignores # inside double-quoted TOML strings, including
// escaped quotes. The navigation uses single-line basic strings only.
func withoutTOMLComment(line string) string {
	quoted, escaped := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quoted && escaped {
			escaped = false
			continue
		}
		if quoted && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		} else if c == '#' && !quoted {
			return line[:i]
		}
	}
	return line
}

func TestDocsNavPagesComments(t *testing.T) {
	input := `[[site.nav]] # nav section
Pages = [
  "present.md", # "absent.md"
  # "also-absent.md",
  { "Hash # label" = "hash#page.md" },
]
[assets] # not nav
other = "outside.md"
`
	pages, err := docsNavPages(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"present.md": true, "hash#page.md": true,
		"absent.md": false, "also-absent.md": false, "outside.md": false,
	} {
		if pages[name] != want {
			t.Errorf("navigation presence of %q = %v, want %v", name, pages[name], want)
		}
	}
}

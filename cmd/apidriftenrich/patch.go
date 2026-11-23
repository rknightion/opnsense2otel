package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	commentFile     = "comment.md"
	patchFile       = "change.patch"
	maxCommentBytes = 40_000
	maxPatchBytes   = 64_000
	maxEdits        = 24
	commentHeader   = "_Automated drift analysis. It is derived from untrusted upstream data: verify it before acting on it._\n\n"
	withheldComment = "The analysis output was withheld because it looked like it contained a credential. See the workflow run."
)

// allowedFiles is the complete set of files a drift fix may touch.
var allowedFiles = map[string]bool{
	"opnsense/client.go":           true,
	"opnsense/client_test.go":      true,
	"opnsense/contract.go":         true,
	"opnsense/contract_test.go":    true,
	"opnsense/testhelpers_test.go": true,
}

var (
	diffHeaderRE = regexp.MustCompile(`^diff --git a/([A-Za-z0-9_./-]+) b/([A-Za-z0-9_./-]+)$`)
	indexRE      = regexp.MustCompile(`^index [0-9a-f]{40}\.\.[0-9a-f]{40} 100644$`)
	hunkRE       = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)
	apiPathRE    = regexp.MustCompile(`api/[A-Za-z0-9_/-]+`)
	numberRE     = regexp.MustCompile(`\b[0-9]+\b`)
	postEntryRE  = regexp.MustCompile(`^\t"[A-Za-z0-9]+":\s+\{\},$`)
	mentionRE    = regexp.MustCompile(`@([A-Za-z0-9])`)
	secretRE     = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-ant-[A-Za-z0-9_-]{16,}|AKIA[0-9A-Z]{16}`)
)

// CheckPatch accepts only a plain text diff of allowlisted regular files whose changed
// lines are endpoint-table updates. It rejects renames, mode changes, symlinks, binary
// content and any line that could add code, because the validation job executes the
// patched tests.
func CheckPatch(patch []byte) ([]string, error) {
	if len(patch) == 0 || len(patch) > maxPatchBytes {
		return nil, fmt.Errorf("patch size %d outside 1..%d", len(patch), maxPatchBytes)
	}
	if !utf8.Valid(patch) || bytes.IndexByte(patch, 0) >= 0 || bytes.IndexByte(patch, '\r') >= 0 {
		return nil, errors.New("patch is not plain UTF-8 text")
	}
	if patch[len(patch)-1] != '\n' {
		return nil, errors.New("patch does not end in a newline")
	}
	lines := strings.Split(strings.TrimSuffix(string(patch), "\n"), "\n")
	seen := map[string]bool{}
	for i := 0; i < len(lines); {
		if i+3 >= len(lines) {
			return nil, errors.New("truncated file header")
		}
		match := diffHeaderRE.FindStringSubmatch(lines[i])
		if match == nil || match[1] != match[2] {
			return nil, fmt.Errorf("unexpected diff header %q", lines[i])
		}
		path := match[1]
		if !allowedFiles[path] {
			return nil, fmt.Errorf("file %q is not allowlisted", path)
		}
		if seen[path] {
			return nil, fmt.Errorf("file %q appears twice", path)
		}
		seen[path] = true
		if !indexRE.MatchString(lines[i+1]) {
			return nil, fmt.Errorf("unexpected header for %s: %q", path, lines[i+1])
		}
		if lines[i+2] != "--- a/"+path || lines[i+3] != "+++ b/"+path {
			return nil, fmt.Errorf("mismatched file names for %s", path)
		}
		i += 4
		hunks := 0
		for i < len(lines) && strings.HasPrefix(lines[i], "@@") {
			next, err := checkHunk(path, lines, i)
			if err != nil {
				return nil, err
			}
			i = next
			hunks++
		}
		if hunks == 0 {
			return nil, fmt.Errorf("no hunks for %s", path)
		}
	}
	files := make([]string, 0, len(seen))
	for path := range seen {
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func checkHunk(path string, lines []string, start int) (int, error) {
	match := hunkRE.FindStringSubmatch(lines[start])
	if match == nil {
		return 0, fmt.Errorf("bad hunk header %q", lines[start])
	}
	oldLeft, newLeft := hunkCount(match[2]), hunkCount(match[4])
	if oldLeft < 0 || newLeft < 0 {
		return 0, fmt.Errorf("bad hunk counts %q", lines[start])
	}
	var removed, added []string
	flush := func() error {
		err := checkChange(path, removed, added)
		removed, added = nil, nil
		return err
	}
	i := start + 1
	for oldLeft > 0 || newLeft > 0 {
		if i >= len(lines) || lines[i] == "" {
			return 0, fmt.Errorf("truncated hunk in %s", path)
		}
		body := lines[i][1:]
		switch lines[i][0] {
		case ' ':
			if err := flush(); err != nil {
				return 0, err
			}
			oldLeft--
			newLeft--
		case '-':
			if len(added) > 0 {
				if err := flush(); err != nil {
					return 0, err
				}
			}
			removed = append(removed, body)
			oldLeft--
		case '+':
			added = append(added, body)
			newLeft--
		default:
			return 0, fmt.Errorf("unexpected hunk line in %s: %q", path, lines[i])
		}
		if oldLeft < 0 || newLeft < 0 {
			return 0, fmt.Errorf("hunk counts do not match in %s", path)
		}
		i++
	}
	return i, flush()
}

func hunkCount(raw string) int {
	if raw == "" {
		return 1
	}
	count, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return count
}

// checkChange allows a one-for-one line replacement that differs only in an API path
// (and, in a test, a number such as a golden count), or a POST-table entry in
// contract.go. Nothing else can be added, so a patch cannot introduce code.
func checkChange(path string, removed, added []string) error {
	if len(removed) == 0 && len(added) == 0 {
		return nil
	}
	if len(removed) == len(added) {
		same := true
		for i := range removed {
			same = same && maskLine(path, removed[i]) == maskLine(path, added[i])
		}
		if same {
			return nil
		}
	}
	if path == "opnsense/contract.go" {
		all := true
		for _, line := range append(append([]string{}, removed...), added...) {
			all = all && postEntryRE.MatchString(line)
		}
		if all {
			return nil
		}
	}
	return fmt.Errorf("change in %s is not an endpoint-table update", path)
}

func maskLine(path, line string) string {
	masked := apiPathRE.ReplaceAllString(line, "api/*")
	if strings.HasSuffix(path, "_test.go") {
		masked = numberRE.ReplaceAllString(masked, "N")
	}
	return masked
}

// SanitizeComment makes model text safe to post: no control characters, no live
// mentions, no hidden HTML comments and no credential-shaped strings.
func SanitizeComment(text string) string {
	text = strings.ToValidUTF8(text, "")
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			return r
		}
		return -1
	}, text)
	if secretRE.MatchString(text) {
		return withheldComment
	}
	text = strings.ReplaceAll(text, "<!--", "&lt;!--")
	text = mentionRE.ReplaceAllString(text, "@\u200b$1")
	if len(text) > maxCommentBytes {
		text = strings.ToValidUTF8(text[:maxCommentBytes], "")
	}
	return text
}

type analysisEdit struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

type analysisOutput struct {
	Comment string         `json:"comment_markdown"`
	Edits   []analysisEdit `json:"edits"`
}

// MaterializeResult holds the digests of the files written to the artifact.
type MaterializeResult struct {
	CommentSHA string
	PatchSHA   string
}

// Materialize turns the structured analysis output into a comment and, when the edits
// are valid, a checked patch. A rejected edit costs the patch and never the comment.
func Materialize(raw []byte, repo, outDir string) (MaterializeResult, error) {
	var output analysisOutput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return MaterializeResult{}, fmt.Errorf("parse analysis output: %w", err)
	}
	if strings.TrimSpace(output.Comment) == "" {
		return MaterializeResult{}, errors.New("analysis output has no comment")
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return MaterializeResult{}, err
	}
	comment := commentHeader + SanitizeComment(output.Comment)
	var patch []byte
	if len(output.Edits) > 0 {
		var err error
		if patch, err = buildPatch(repo, output.Edits); err != nil {
			patch = nil
			comment += "\n\n---\nNo pull request was prepared: the proposed edit was rejected (" + SanitizeComment(err.Error()) + ")."
		}
	}
	result := MaterializeResult{CommentSHA: digest([]byte(comment))}
	if err := os.WriteFile(filepath.Join(outDir, commentFile), []byte(comment), 0o600); err != nil {
		return MaterializeResult{}, err
	}
	if patch != nil {
		if err := os.WriteFile(filepath.Join(outDir, patchFile), patch, 0o600); err != nil {
			return MaterializeResult{}, err
		}
		result.PatchSHA = digest(patch)
	}
	return result, nil
}

func buildPatch(repo string, edits []analysisEdit) ([]byte, error) {
	if len(edits) > maxEdits {
		return nil, fmt.Errorf("%d edits exceeds the limit of %d", len(edits), maxEdits)
	}
	// os.Root refuses any path or symlink that leaves the checkout.
	root, err := os.OpenRoot(repo)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, edit := range edits {
		if !allowedFiles[edit.Path] {
			return nil, fmt.Errorf("file %q is not allowlisted", edit.Path)
		}
		if edit.Old == "" || edit.Old == edit.New {
			return nil, fmt.Errorf("empty or no-op edit for %s", edit.Path)
		}
		info, err := root.Lstat(edit.Path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", edit.Path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", edit.Path)
		}
		source, err := root.ReadFile(edit.Path)
		if err != nil {
			return nil, err
		}
		if count := strings.Count(string(source), edit.Old); count != 1 {
			return nil, fmt.Errorf("edit for %s matches %d times, want 1", edit.Path, count)
		}
		updated := strings.Replace(string(source), edit.Old, edit.New, 1)
		if err := root.WriteFile(edit.Path, []byte(updated), info.Mode().Perm()); err != nil {
			return nil, err
		}
	}
	args := []string{"-C", repo, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false",
		"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--full-index", "--"}
	for path := range allowedFiles {
		args = append(args, path)
	}
	patch, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	if _, err := CheckPatch(patch); err != nil {
		return nil, err
	}
	return patch, nil
}

// VerifyArtifact checks a downloaded artifact byte for byte against the digests the
// analysis job published, then repeats the content checks.
func VerifyArtifact(dir, commentSHA, patchSHA string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	want := map[string]string{commentFile: commentSHA}
	if patchSHA != "" {
		want[patchFile] = patchSHA
	}
	if len(entries) != len(want) {
		return fmt.Errorf("artifact holds %d entries, want %d", len(entries), len(want))
	}
	contents := map[string][]byte{}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact entry %q is not a regular file", entry.Name())
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// An unexpected name has no digest in want, so it can never match.
		if digest(raw) != want[entry.Name()] {
			return fmt.Errorf("digest mismatch for %q", entry.Name())
		}
		contents[entry.Name()] = raw
	}
	comment := string(contents[commentFile])
	if len(comment) > maxCommentBytes+len(commentHeader)+1024 || !utf8.ValidString(comment) ||
		secretRE.MatchString(comment) || mentionRE.MatchString(comment) || strings.Contains(comment, "<!--") {
		return errors.New("comment failed its content checks")
	}
	if patchSHA != "" {
		if _, err := CheckPatch(contents[patchFile]); err != nil {
			return err
		}
	}
	return nil
}

// CheckStaged confirms the index differs from HEAD only in allowlisted regular files.
func CheckStaged(repo string) error {
	out, err := exec.Command("git", "-C", repo, "diff", "--cached", "--no-renames", "--raw", "-z", "--no-abbrev").Output()
	if err != nil {
		return fmt.Errorf("git diff --cached: %w", err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields) < 2 || len(fields)%2 != 0 {
		return errors.New("nothing staged")
	}
	for i := 0; i < len(fields); i += 2 {
		meta := strings.Fields(fields[i])
		if len(meta) != 5 || meta[0] != ":100644" || meta[1] != "100644" || meta[4] != "M" {
			return fmt.Errorf("staged change is not a plain modification: %q", fields[i])
		}
		if !allowedFiles[fields[i+1]] {
			return fmt.Errorf("staged file %q is not allowlisted", fields[i+1])
		}
	}
	return nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

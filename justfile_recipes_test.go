package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/repotest"
)

func recipeCommand(t *testing.T, dir, home, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// Resolve only the tools the recipes need; do not inherit shell or just settings.
	path := []string{"/usr/bin", "/bin"}
	for _, tool := range []string{"git", "just", "bash"} {
		resolved, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("find %s: %v", tool, err)
		}
		path = append(path, filepath.Dir(resolved))
	}
	cmd.Env = []string{
		"PATH=" + strings.Join(path, string(os.PathListSeparator)),
		"HOME=" + home, "XDG_CONFIG_HOME=" + home, "TMPDIR=" + home,
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func TestJustfileRecipeCommandEnvironment(t *testing.T) {
	t.Setenv("BASH_ENV", "inert-startup")
	t.Setenv("JUST_DOTENV_COMMAND", "inert-command")
	t.Setenv("GIT_DIR", "inert-dir")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("OTHER_UNTRUSTED_SETTING", "inert-value")
	tmp := t.TempDir()
	out := recipeCommand(t, tmp, tmp, "env")
	safeGit := map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("invalid environment entry %q", line)
		}
		if strings.HasPrefix(key, "GIT_") {
			if want, found := safeGit[key]; !found || value != want {
				t.Errorf("unexpected git environment %s=%q", key, value)
			}
			delete(safeGit, key)
		}
		if key == "BASH_ENV" || key == "JUST_DOTENV_COMMAND" || key == "OTHER_UNTRUSTED_SETTING" {
			t.Errorf("inherited %s=%q", key, value)
		}
	}
	if len(safeGit) != 0 {
		t.Errorf("missing safe git environment: %v", safeGit)
	}
}

func TestJustfileRecipeInstallHooksInLinkedWorktree(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	worktree := filepath.Join(tmp, "worktree")
	template := filepath.Join(tmp, "template")
	if err := os.MkdirAll(template, 0o755); err != nil {
		t.Fatal(err)
	}
	recipeCommand(t, tmp, tmp, "git", "init", "--template="+template, repo)
	recipeCommand(t, repo, tmp, "git", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	recipeCommand(t, repo, tmp, "git", "worktree", "add", "-b", "linked", worktree)
	hook, err := os.ReadFile(filepath.Join(root, "scripts", "hooks", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(worktree, "scripts", "hooks")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "pre-commit"), hook, 0o644); err != nil {
		t.Fatal(err)
	}
	recipeCommand(t, tmp, tmp, "just", "--justfile", filepath.Join(root, "justfile"), "--working-directory", worktree, "install-hooks")
	info, err := os.Stat(filepath.Join(repo, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed hook is not executable: %v", info.Mode())
	}
}

func TestJustfileRecipeSbomBuildsItsTarget(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	target := filepath.Join(tmp, "scratch", "binary")
	out := recipeCommand(t, tmp, tmp, "just", "--justfile", filepath.Join(root, "justfile"), "--dry-run", "sbom", target)
	var build, scan string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "go build ") {
			build = line
		}
		if strings.Contains(line, "/syft ") && strings.Contains(line, " -q ") {
			scan = line
		}
	}
	if !strings.Contains(build, "-o '"+target+"'") {
		t.Errorf("build does not write target %q: %s", target, build)
	}
	if !strings.Contains(scan, "syft '"+target+"' ") {
		t.Errorf("syft does not scan target %q: %s", target, scan)
	}
}

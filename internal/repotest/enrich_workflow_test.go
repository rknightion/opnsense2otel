package repotest_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/repotest"
	"go.yaml.in/yaml/v2"
)

type enrichStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	If   string            `yaml:"if"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type enrichJob struct {
	If          string            `yaml:"if"`
	Needs       any               `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Steps       []enrichStep      `yaml:"steps"`
}

type enrichWorkflow struct {
	On          map[string]any       `yaml:"on"`
	Permissions map[string]string    `yaml:"permissions"`
	Jobs        map[string]enrichJob `yaml:"jobs"`
}

var pinnedUses = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)

// checkEnrichWorkflow returns every way the workflow departs from the boundary between
// model output, code execution and repository write access.
func checkEnrichWorkflow(raw []byte) []string {
	var problems []string
	fail := func(format string) { problems = append(problems, format) }
	var wf enrichWorkflow
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return []string{"parse: " + err.Error()}
	}
	if len(wf.On) != 1 || wf.On["workflow_dispatch"] == nil {
		fail("trigger must be workflow_dispatch only")
	}
	if wf.Permissions == nil || len(wf.Permissions) != 0 {
		fail("top-level permissions must be empty")
	}
	want := map[string]map[string]string{
		"prepare":  {"contents": "read", "issues": "read"},
		"agent":    {"contents": "read"},
		"validate": {"contents": "read"},
		"publish":  {"contents": "write", "issues": "write", "pull-requests": "write"},
	}
	if len(wf.Jobs) != len(want) {
		fail("unexpected job set")
	}
	for name, permissions := range want {
		job, ok := wf.Jobs[name]
		if !ok {
			fail("missing job " + name)
			continue
		}
		if !reflect.DeepEqual(job.Permissions, permissions) {
			fail(name + " permissions changed")
		}
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "${{") {
				fail(name + " interpolates an expression into a run script: " + step.Name)
			}
			for key, value := range step.With {
				if strings.Contains(value, "secrets.") && (name != "agent" || key != "claude_code_oauth_token") {
					fail(name + " passes a secret through " + key)
				}
			}
			for key, value := range step.Env {
				if strings.Contains(value, "secrets.") {
					fail(name + " exposes a secret in env " + key)
				}
			}
			if step.Uses != "" {
				if !pinnedUses.MatchString(step.Uses) {
					fail(name + " has an unpinned action: " + step.Uses)
				}
				if strings.Contains(step.Uses, "claude-code-action") && name != "agent" {
					fail(name + " runs the agent")
				}
				if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["persist-credentials"] != "false" {
					fail(name + " persists checkout credentials")
				}
				if strings.HasPrefix(step.Uses, "actions/setup-go@") && step.With["cache"] != "false" {
					fail(name + " enables the Go cache")
				}
			}
		}
	}
	if problems != nil && (len(wf.Jobs) == 0 || wf.Jobs["agent"].Steps == nil) {
		return problems
	}
	problems = append(problems, checkEnrichAgent(wf.Jobs["agent"])...)
	problems = append(problems, checkEnrichValidate(wf.Jobs["validate"])...)
	problems = append(problems, checkEnrichPublish(wf.Jobs["publish"])...)
	return problems
}

func checkEnrichAgent(job enrichJob) []string {
	var problems []string
	agentIndex := -1
	for i, step := range job.Steps {
		if strings.Contains(step.Uses, "claude-code-action") {
			if agentIndex >= 0 {
				problems = append(problems, "agent job runs the action twice")
			}
			agentIndex = i
		}
	}
	if agentIndex < 0 {
		return append(problems, "agent job does not run the action")
	}
	step := job.Steps[agentIndex]
	allowedInputs := map[string]bool{"claude_code_oauth_token": true, "github_token": true, "prompt": true, "claude_args": true}
	for key := range step.With {
		if !allowedInputs[key] {
			problems = append(problems, "agent step sets input "+key)
		}
	}
	if step.Env["CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"] != "1" {
		problems = append(problems, "agent step does not scrub subprocess env")
	}
	args := strings.Fields(step.With["claude_args"])
	flags := map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			flags[args[i]] = args[i+1]
		}
	}
	for flag, value := range map[string]string{"--tools": "StructuredOutput", "--permission-mode": "dontAsk", "--setting-sources": "user"} {
		if flags[flag] != value {
			problems = append(problems, "agent args need "+flag+" "+value)
		}
	}
	joined := step.With["claude_args"]
	if !strings.Contains(joined, "--strict-mcp-config") || !strings.Contains(joined, "--json-schema") {
		problems = append(problems, "agent args need --strict-mcp-config and --json-schema")
	}
	for _, banned := range []string{"allowedTools", "allowed-tools", " --mcp-config", "add-dir", "dangerously", "bypassPermissions", "plugin"} {
		if strings.Contains(" "+joined, banned) {
			problems = append(problems, "agent args contain "+banned)
		}
	}
	// Nothing from the repository may be on disk when the session starts.
	for _, before := range job.Steps[:agentIndex] {
		if strings.HasPrefix(before.Uses, "actions/checkout@") || strings.HasPrefix(before.Uses, "actions/setup-go@") {
			problems = append(problems, "agent job prepares the repository before the session: "+before.Uses)
		}
		if regexp.MustCompile(`(^|\s)(go|just)\s`).MatchString(before.Run) {
			problems = append(problems, "agent job runs repository tooling before the session")
		}
	}
	return problems
}

func checkEnrichValidate(job enrichJob) []string {
	var problems []string
	var script string
	for _, step := range job.Steps {
		script += step.Run + "\n"
	}
	for _, required := range []string{"--network none", "--cap-drop ALL", "no-new-privileges", "--read-only", "checkout-index", "apidriftenrich\" verify", "apidriftenrich\" staged"} {
		if !strings.Contains(script, required) {
			problems = append(problems, "validate job lacks "+required)
		}
	}
	if strings.Contains(script, "docker.sock") || strings.Contains(script, "--privileged") {
		problems = append(problems, "validate sandbox is weakened")
	}
	// The sandbox mounts the exported tree, never the workspace holding .git.
	var dockerStep *enrichStep
	for i := range job.Steps {
		if strings.Contains(job.Steps[i].Run, "docker run") {
			dockerStep = &job.Steps[i]
		}
	}
	if dockerStep == nil {
		return append(problems, "validate job has no sandbox step")
	}
	if !strings.Contains(dockerStep.Run, "go test") {
		problems = append(problems, "validate sandbox does not run tests")
	}
	for _, line := range strings.Split(dockerStep.Run, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "go ") || strings.HasPrefix(trimmed, "just ") {
			problems = append(problems, "validate runs patched code outside the sandbox")
		}
	}
	return problems
}

func checkEnrichPublish(job enrichJob) []string {
	var problems []string
	verified, applied := false, false
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "apidriftenrich\" verify") {
			verified = true
		}
		if strings.Contains(step.Run, "git apply") {
			if !verified {
				problems = append(problems, "publish applies the patch before verifying it")
			}
			applied = true
		}
		if regexp.MustCompile(`(^|\s)go (test|run|generate)\b|(^|\s)just\s+[a-z]|docker run`).MatchString(step.Run) {
			problems = append(problems, "publish executes repository code: "+step.Name)
		}
		if strings.Contains(step.Run, "go build") && applied {
			problems = append(problems, "publish builds after applying the patch")
		}
		if strings.Contains(step.Run, "gh issue comment") {
			if !strings.Contains(step.Run, "--body-file") {
				problems = append(problems, "publish comment must use --body-file")
			}
			if !strings.Contains(step.If, "steps.verify.outcome == 'success'") {
				problems = append(problems, "publish comment must depend on the artifact check")
			}
		}
		if strings.Contains(step.Run, "gh pr create") && !strings.Contains(step.Run, "--body-file") {
			problems = append(problems, "publish PR must use --body-file")
		}
	}
	if !verified || !applied {
		problems = append(problems, "publish must verify and apply the artifact")
	}
	return problems
}

func TestEnrichWorkflowKeepsAgentExecutionAndWriteApart(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/api-contract-enrich.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range checkEnrichWorkflow(raw) {
		t.Error(problem)
	}
}

func TestEnrichWorkflowCheckRejectsWeakenedBoundaries(t *testing.T) {
	root, err := repotest.Root()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/api-contract-enrich.yml"))
	if err != nil {
		t.Fatal(err)
	}
	good := string(data)
	cases := map[string][2]string{
		"agent write token":    {"    permissions:\n      contents: read\n    outputs:\n      artifact_id", "    permissions:\n      contents: write\n    outputs:\n      artifact_id"},
		"agent gets Bash":      {"--tools StructuredOutput", "--tools Bash,StructuredOutput"},
		"auto permission":      {"--permission-mode dontAsk", "--permission-mode auto"},
		"allow rule added":     {"--strict-mcp-config", "--strict-mcp-config --allowedTools Bash"},
		"project settings":     {"--setting-sources user", "--setting-sources user,project"},
		"scrub removed":        {"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB: '1'", "CLAUDE_CODE_SUBPROCESS_ENV_SCRUB: '0'"},
		"sandbox network":      {"--network none", "--network bridge"},
		"sandbox caps":         {"--cap-drop ALL", "--cap-add ALL"},
		"go cache":             {"go-version-file: go.mod\n          cache: false\n      - name: Build the enrichment checks\n        run: go build -o \"$RUNNER_TEMP/apidriftenrich\" ./cmd/apidriftenrich\n\n      - name: Verify", "go-version-file: go.mod\n          cache: true\n      - name: Build the enrichment checks\n        run: go build -o \"$RUNNER_TEMP/apidriftenrich\" ./cmd/apidriftenrich\n\n      - name: Verify"},
		"publish runs tests":   {"gh auth setup-git", "go test ./...\n          gh auth setup-git"},
		"publish skips check":  {"      - name: Recheck the artifact\n", "      - name: Recheck the artifact\n        if: false\n        continue-on-error: true\n"},
		"comment interpolated": {"--body-file \"$body\"", "--body \"${{ steps.pr.outputs.url }}\""},
		"comment skips verify": {"if: ${{ !cancelled() && steps.verify.outcome == 'success' }}", "if: ${{ !cancelled() }}"},
		"secret in validate":   {"          COMMENT_SHA256: ${{ needs.agent.outputs.comment_sha256 }}\n          PATCH_SHA256: ${{ needs.agent.outputs.patch_sha256 }}\n        run: |\n          set -euo pipefail\n          \"$RUNNER_TEMP/apidriftenrich\" verify", "          COMMENT_SHA256: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}\n          PATCH_SHA256: ${{ needs.agent.outputs.patch_sha256 }}\n        run: |\n          set -euo pipefail\n          \"$RUNNER_TEMP/apidriftenrich\" verify"},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(good, change[0]) {
				t.Fatalf("mutation anchor missing: %q", change[0])
			}
			mutated := strings.Replace(good, change[0], change[1], 1)
			if name == "publish skips check" {
				mutated = strings.Replace(good, "\"$RUNNER_TEMP/apidriftenrich\" verify \\\n            --dir \"$RUNNER_TEMP/enrichment\" \\\n            --comment-sha256 \"$COMMENT_SHA256\" \\\n            --patch-sha256 \"$PATCH_SHA256\"\n\n      - name: Open", "true\n\n      - name: Open", 1)
				if mutated == good {
					t.Fatal("publish verify anchor missing")
				}
			}
			if len(checkEnrichWorkflow([]byte(mutated))) == 0 {
				t.Fatal("weakened workflow passed the check")
			}
		})
	}
}

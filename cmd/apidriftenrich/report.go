package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	reportHeading = "## OPNsense API contract check"
	issueTitle    = "OPNsense API contract drift"
	driftLabel    = "api-drift"
	// The Actions bot account. A login alone is a display string, so the numeric id
	// and account type are checked with it.
	actionsBotLogin = "github-actions[bot]"
	actionsBotID    = 41898282
	actionsAppSlug  = "github-actions"

	maxRefs     = 6
	maxFindings = 60
)

var (
	refHeadingRE = regexp.MustCompile("^### `([^`]+)`$")
	refRE        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,63}$`)
	findingRE    = regexp.MustCompile("^\\| `([A-Za-z0-9]{1,64})` \\| `(api/[A-Za-z0-9_/-]{1,160})` \\| .*\\|$")
)

type account struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"`
}

func (a account) isActionsBot() bool {
	return a.Login == actionsBotLogin && a.ID == actionsBotID && a.Type == "Bot"
}

type issuePayload struct {
	Title       string          `json:"title"`
	State       string          `json:"state"`
	Body        string          `json:"body"`
	User        account         `json:"user"`
	PullRequest json.RawMessage `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type commentPayload struct {
	ID   int64   `json:"id"`
	Body string  `json:"body"`
	User account `json:"user"`
	App  *struct {
		Slug string `json:"slug"`
	} `json:"performed_via_github_app"`
}

// Finding is one drifted endpoint, holding only values that passed the report grammar.
type Finding struct {
	Ref      string `json:"ref"`
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint"`
	Path     string `json:"path"`
}

// Findings is the validated reduction of a canary report.
type Findings struct {
	Refs     []string  `json:"refs"`
	Findings []Finding `json:"findings"`
}

// TrustedReport returns the newest report written by the canary. The issue must be
// canary-created; a comment counts only when the Actions app wrote it, because anyone
// can comment on a public issue.
func TrustedReport(issueJSON, commentsNDJSON []byte) (string, error) {
	var issue issuePayload
	if err := json.Unmarshal(issueJSON, &issue); err != nil {
		return "", fmt.Errorf("parse issue: %w", err)
	}
	hasLabel := false
	for _, label := range issue.Labels {
		hasLabel = hasLabel || label.Name == driftLabel
	}
	isPR := len(issue.PullRequest) > 0 && !bytes.Equal(issue.PullRequest, []byte("null"))
	if issue.Title != issueTitle || !issue.User.isActionsBot() || issue.State != "open" || isPR || !hasLabel ||
		!strings.HasPrefix(issue.Body, reportHeading) {
		return "", errors.New("refusing an issue that is not an open canary drift report")
	}
	report := issue.Body
	var newest int64
	decoder := json.NewDecoder(bytes.NewReader(commentsNDJSON))
	for decoder.More() {
		var comment commentPayload
		if err := decoder.Decode(&comment); err != nil {
			return "", fmt.Errorf("parse comment: %w", err)
		}
		if !comment.User.isActionsBot() || comment.App == nil || comment.App.Slug != actionsAppSlug {
			continue
		}
		if comment.ID > newest && strings.HasPrefix(comment.Body, reportHeading) {
			newest, report = comment.ID, comment.Body
		}
	}
	return report, nil
}

// ParseReport keeps the ref, endpoint and path of every row. The free-text detail
// column is dropped so that it never reaches the analysis prompt.
func ParseReport(report string) (Findings, error) {
	var out Findings
	var ref, kind string
	seenRefs := map[string]bool{}
	for _, line := range strings.Split(report, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "### "):
			match := refHeadingRE.FindStringSubmatch(line)
			if match == nil || !validRef(match[1]) {
				return Findings{}, fmt.Errorf("invalid ref heading %q", line)
			}
			ref, kind = match[1], ""
		case strings.HasPrefix(line, "**Missing endpoints"):
			kind = "missing"
		case strings.HasPrefix(line, "**Verb drift"):
			kind = "verb"
		case strings.HasPrefix(line, "| Endpoint |"), strings.HasPrefix(line, "|---"):
		case strings.HasPrefix(line, "|"):
			match := findingRE.FindStringSubmatch(line)
			if match == nil || ref == "" || kind == "" {
				return Findings{}, fmt.Errorf("malformed report row %q", line)
			}
			if !seenRefs[ref] {
				seenRefs[ref] = true
				out.Refs = append(out.Refs, ref)
			}
			out.Findings = append(out.Findings, Finding{Ref: ref, Kind: kind, Endpoint: match[1], Path: match[2]})
		}
	}
	if len(out.Findings) == 0 {
		return Findings{}, errors.New("report has no drift findings")
	}
	if len(out.Refs) > maxRefs || len(out.Findings) > maxFindings {
		return Findings{}, fmt.Errorf("report too large: %d refs, %d findings", len(out.Refs), len(out.Findings))
	}
	return out, nil
}

func validRef(ref string) bool {
	return refRE.MatchString(ref) && !strings.Contains(ref, "..") && !strings.HasSuffix(ref, "/")
}

func readFindings(path string) (Findings, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Findings{}, err
	}
	var findings Findings
	if err := json.Unmarshal(raw, &findings); err != nil {
		return Findings{}, fmt.Errorf("parse findings: %w", err)
	}
	for _, ref := range findings.Refs {
		if !validRef(ref) {
			return Findings{}, fmt.Errorf("invalid ref %q", ref)
		}
	}
	return findings, nil
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

const (
	maxControllerBytes = 2 << 20
	maxActions         = 120
	maxSiblings        = 40
	// The prompt reaches the action through one environment variable, which Linux
	// limits to 128 KiB. Detail is shed at the soft limit; past the hard limit the
	// run stops rather than sending a clipped context.
	softContextBytes = 60_000
	hardContextBytes = 80_000
	// The action also passes its inputs as one JSON string, where quotes double up.
	maxEscapedContextBytes = 118_000
)

var (
	controllerPathRE = regexp.MustCompile(`/mvc/app/controllers/OPNsense/([A-Za-z0-9_]+)/Api/([A-Za-z0-9_]+)Controller\.php$`)
	actionRE         = regexp.MustCompile(`(?m)^\s*public\s+function\s+([A-Za-z0-9_]+)Action\s*\(`)
	safeRelPathRE    = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
)

// UpstreamAction is one action method, reduced to its name and POST guard.
type UpstreamAction struct {
	Name        string `json:"name"`
	PostGuarded bool   `json:"post_guarded"`
}

// ContextEntry is what the analysis job learns about one finding.
type ContextEntry struct {
	Finding
	Stale          bool             `json:"stale,omitempty"`
	CurrentMethod  string           `json:"current_method,omitempty"`
	ClientLine     string           `json:"client_go_line,omitempty"`
	PostEntryLine  string           `json:"contract_go_line,omitempty"`
	ControllerFile string           `json:"controller_file,omitempty"`
	Actions        []UpstreamAction `json:"actions,omitempty"`
	Siblings       []string         `json:"sibling_controllers,omitempty"`
	Truncated      bool             `json:"truncated,omitempty"`
}

type controller struct {
	module, name, file, rel string
}

// BuildContext joins the findings with the current contract and with the action
// names found upstream. Upstream PHP is reduced to identifiers, so its comments and
// strings never reach the analysis prompt.
func BuildContext(findings Findings, upstreamDir, repo string) ([]byte, error) {
	manifest := opnsense.ContractManifest()
	clientLines, err := readLines(filepath.Join(repo, "opnsense/client.go"))
	if err != nil {
		return nil, err
	}
	contractLines, err := readLines(filepath.Join(repo, "opnsense/contract.go"))
	if err != nil {
		return nil, err
	}
	indexes := map[string]map[string]controller{}
	for _, ref := range findings.Refs {
		index, err := indexControllers(filepath.Join(upstreamDir, strings.ReplaceAll(ref, "/", "-")))
		if err != nil {
			return nil, err
		}
		indexes[ref] = index
	}
	entries := make([]ContextEntry, 0, len(findings.Findings))
	used := 0
	for _, finding := range findings.Findings {
		entry := ContextEntry{Finding: finding}
		contract, ok := manifest[opnsense.EndpointName(finding.Endpoint)]
		if !ok || string(contract.Path) != finding.Path {
			entry.Stale = true
		} else {
			entry.CurrentMethod = contract.Method
			entry.ClientLine = entryLine(clientLines, finding.Endpoint)
			entry.PostEntryLine = entryLine(contractLines, finding.Endpoint)
			if err := describeUpstream(&entry, indexes[finding.Ref]); err != nil {
				return nil, err
			}
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		if used+len(raw) > softContextBytes {
			entry.Actions, entry.Siblings, entry.Truncated = nil, nil, true
			if raw, err = json.Marshal(entry); err != nil {
				return nil, err
			}
		}
		used += len(raw)
		entries = append(entries, entry)
	}
	out, err := json.Marshal(map[string]any{"findings": entries})
	if err != nil {
		return nil, err
	}
	if err := checkContextSize(out); err != nil {
		return nil, err
	}
	return out, nil
}

func checkContextSize(context []byte) error {
	if len(context) > hardContextBytes {
		return fmt.Errorf("context is %d bytes, over the %d limit", len(context), hardContextBytes)
	}
	escaped, err := json.Marshal(string(context))
	if err != nil {
		return err
	}
	if len(escaped) > maxEscapedContextBytes {
		return fmt.Errorf("escaped context is %d bytes, over the %d limit", len(escaped), maxEscapedContextBytes)
	}
	return nil
}

func describeUpstream(entry *ContextEntry, index map[string]controller) error {
	parts := strings.Split(entry.Path, "/")
	if len(parts) < 4 {
		return nil
	}
	module, name := normalize(parts[1]), normalize(parts[2])
	if found, ok := index[module+"/"+name]; ok {
		actions, err := extractActions(found.file)
		if err != nil {
			return err
		}
		entry.ControllerFile, entry.Actions = found.rel, actions
		return nil
	}
	for key, candidate := range index {
		if strings.HasPrefix(key, module+"/") {
			entry.Siblings = append(entry.Siblings, candidate.name)
		}
	}
	sort.Strings(entry.Siblings)
	if len(entry.Siblings) > maxSiblings {
		entry.Siblings = entry.Siblings[:maxSiblings]
	}
	return nil
}

// indexControllers walks regular files only; WalkDir does not follow symlinks.
func indexControllers(root string) (map[string]controller, error) {
	index := map[string]controller{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		match := controllerPathRE.FindStringSubmatch("/" + rel)
		if match == nil || !safeRelPathRE.MatchString(rel) {
			return nil
		}
		key := normalize(match[1]) + "/" + normalize(match[2])
		if existing, ok := index[key]; !ok || rel < existing.rel {
			index[key] = controller{module: match[1], name: match[2], file: path, rel: rel}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return index, nil
	}
	return index, err
}

func extractActions(path string) ([]UpstreamAction, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxControllerBytes {
		return nil, fmt.Errorf("refusing controller %s", filepath.Base(path))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	source := string(raw)
	matches := actionRE.FindAllStringSubmatchIndex(source, -1)
	actions := make([]UpstreamAction, 0, len(matches))
	for i, match := range matches {
		if i == maxActions {
			break
		}
		end := len(source)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		actions = append(actions, UpstreamAction{
			Name:        source[match[2]:match[3]],
			PostGuarded: strings.Contains(source[match[1]:end], "isPost()"),
		})
	}
	return actions, nil
}

func normalize(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

func readLines(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(raw), "\n"), nil
}

func entryLine(lines []string, endpoint string) string {
	prefix := "\"" + endpoint + "\":"
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return line
		}
	}
	return ""
}

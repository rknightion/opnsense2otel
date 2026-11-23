package opnsense

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const knownBlockedTestPath = "testdata/schemas/known-blocked.json"

func TestLoadKnownBlockedLedgerMissingFileIsEmpty(t *testing.T) {
	l, err := LoadKnownBlockedLedger(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadKnownBlockedLedger on a missing file: %v", err)
	}
	if len(l) != 0 {
		t.Errorf("want an empty ledger, got %v", l)
	}
}

// A hand-edited ledger must fail loudly on a typo: a misspelt key would
// otherwise silently drop the entry's reason or its profile scoping.
func TestLoadKnownBlockedLedgerRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"malformed JSON":                "{nope",
		"unknown key":                   `{"ep":{"paths":[{"path":"a","profile":{}}]}}`,
		"unknown reason":                `{"ep":{"skipped":{"nightly":{"why":"x"}}}}`,
		"path without a required state": `{"ep":{"paths":[{"path":"a","profiles":{"nightly":{"reason":"r","unblockedBy":"u"}}}]}}`,
		"path with an unknown state":    `{"ep":{"paths":[{"path":"a","profiles":{"nightly":{"reason":"r","unblockedBy":"u","requires":"no-row"}}}]}}`,
		"path with a skip state":        `{"ep":{"paths":[{"path":"a","profiles":{"nightly":{"reason":"r","unblockedBy":"u","requires":"absent:x"}}}]}}`,
		"skip with a path state":        `{"ep":{"skipped":{"nightly":{"reason":"r","unblockedBy":"u","requires":"ipsec-down"}}}}`,
		"skip with no source named":     `{"ep":{"skipped":{"nightly":{"reason":"r","unblockedBy":"u","requires":"absent:"}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "known-blocked.json")
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadKnownBlockedLedger(p); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

// intermittent is a strict boolean: a string or number must be rejected, not
// silently read as "not intermittent".
func TestLoadKnownBlockedLedgerIntermittentIsStrictBoolean(t *testing.T) {
	load := func(v string) error {
		p := filepath.Join(t.TempDir(), "known-blocked.json")
		body := `{"ep":{"paths":[{"path":"a","profiles":{"nightly":{"reason":"r","unblockedBy":"u","requires":"ipsec-down","intermittent":` + v + `}}}]}}`
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadKnownBlockedLedger(p)
		return err
	}
	for _, v := range []string{"true", "false"} {
		if err := load(v); err != nil {
			t.Errorf("intermittent %s must load: %v", v, err)
		}
	}
	for _, v := range []string{`"yes"`, `"true"`, "1", "[]"} {
		if err := load(v); err == nil {
			t.Errorf("intermittent %s must be rejected", v)
		}
	}
}

func TestKnownBlockedForProfile(t *testing.T) {
	r := func(s string) KnownBlockedReason { return KnownBlockedReason{Reason: s, UnblockedBy: "u"} }
	l := KnownBlockedLedger{
		"epB": {Skipped: map[string]KnownBlockedReason{"nightly": r("skip")}},
		"epA": {Paths: []KnownBlockedPath{
			{Path: "z", Profiles: map[string]KnownBlockedReason{"nightly": r("z-n"), "release-vm": r("z-r")}},
			{Path: "a", Profiles: map[string]KnownBlockedReason{"release-vm": r("a-r")}},
		}},
	}
	if got := l.ForProfile(""); len(got) != 0 {
		t.Errorf("an empty profile must resolve to nothing, got %v", got)
	}
	if got := l.ForProfile("unknown"); len(got) != 0 {
		t.Errorf("an unknown profile must resolve to nothing, got %v", got)
	}
	var names []string
	for _, it := range l.ForProfile("nightly") {
		names = append(names, it.Endpoint+"/"+it.Path+"/"+it.Reason)
	}
	want := []string{"epA/z/z-n", "epB//skip"}
	if !slices.Equal(names, want) {
		t.Errorf("nightly = %v, want %v", names, want)
	}
}

// The committed ledger is hand-maintained, so hold it to the same bar as the
// coverage ledger: every entry names a real endpoint, a known probe target, a
// path that coverage.json still treats as ordinary required coverage on that
// target, and carries a written reason and the box state that would unblock it.
func TestCommittedKnownBlockedLedgerIntegrity(t *testing.T) {
	ledger, err := LoadKnownBlockedLedger(knownBlockedTestPath)
	if err != nil {
		t.Fatalf("LoadKnownBlockedLedger: %v", err)
	}
	if len(ledger) == 0 {
		t.Fatal("the committed known-blocked ledger is empty")
	}
	coverage, err := LoadCoverageLedger(coverageLedgerTestPath)
	if err != nil {
		t.Fatalf("LoadCoverageLedger: %v", err)
	}
	schemas, err := AllEndpointSchemas()
	if err != nil {
		t.Fatalf("AllEndpointSchemas: %v", err)
	}
	registered := make(map[string]bool, len(schemas))
	for _, s := range schemas {
		registered[s.Endpoint] = true
	}

	// Nothing identifying: the ledger is public. Addresses, URLs and hostnames
	// have no place in a structure-only ledger.
	identifier := regexp.MustCompile(`(?i)(https?://|\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b|\.(lan|local|internal|ts\.net)\b)`)

	// Intermittent is for a path whose box state flaps, which is only the
	// nightly lab (the devel-channel box the tunnel dials into). The
	// release-channel box is permanently blocked, so a verified path there is a
	// real change and must still go stale.
	checkIntermittent := func(where, profile string, r KnownBlockedReason, isSkip bool) {
		if r.Intermittent && (isSkip || profile != ProbeProfileNightly) {
			t.Errorf("%s: intermittent is only valid on a nightly path entry", where)
		}
	}
	checkReason := func(where string, r KnownBlockedReason) {
		for field, v := range map[string]string{"reason": r.Reason, "unblockedBy": r.UnblockedBy} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is empty", where, field)
			}
			if identifier.MatchString(v) {
				t.Errorf("%s: %s carries an address, URL or hostname", where, field)
			}
		}
	}
	checkProfile := func(where, profile string) bool {
		if !slices.Contains(KnownProbeProfiles(), profile) {
			t.Errorf("%s: %q is not a known probe profile %v", where, profile, KnownProbeProfiles())
			return false
		}
		return true
	}

	for endpoint, e := range ledger {
		if !registered[endpoint] {
			t.Errorf("%s: not a known endpoint schema", endpoint)
		}
		if len(e.Paths) == 0 && len(e.Skipped) == 0 {
			t.Errorf("%s: an entry with neither paths nor skipped", endpoint)
		}
		for profile, r := range e.Skipped {
			where := endpoint + " skipped " + profile
			if checkProfile(where, profile) {
				checkReason(where, r)
				checkIntermittent(where, profile, r, true)
				if src := strings.TrimPrefix(r.Requires, RequiresAbsentPrefix); !registered[src] {
					t.Errorf("%s: requires names %q, not a known endpoint", where, src)
				}
			}
		}
		seen := map[string]bool{}
		for _, p := range e.Paths {
			if p.Path == "" {
				t.Errorf("%s: a path entry has no path", endpoint)
				continue
			}
			if seen[p.Path] {
				t.Errorf("%s: path %q is listed twice", endpoint, p.Path)
			}
			seen[p.Path] = true
			if len(p.Profiles) == 0 {
				t.Errorf("%s %q: no probe profile", endpoint, p.Path)
			}
			for profile, r := range p.Profiles {
				where := endpoint + " " + p.Path + " " + profile
				if !checkProfile(where, profile) {
					continue
				}
				checkReason(where, r)
				checkIntermittent(where, profile, r, false)
				// The entry only means something while the path is ordinary
				// required coverage on this target.
				class, entry, ok := coverage.Index(profile).Classify(endpoint, p.Path)
				switch {
				case !ok:
					t.Errorf("%s: path is not in coverage.json", where)
				case class != CoverageRequired:
					t.Errorf("%s: coverage.json classes the path %s, not required", where, class)
				case entry.Opaque:
					t.Errorf("%s: the path is opaque in coverage.json, which never warns, so it needs no entry", where)
				}
			}
		}
	}
}

// A valid ledger followed by anything else is a damaged file, not a ledger.
func TestLoadKnownBlockedLedgerRejectsTrailingData(t *testing.T) {
	const valid = `{"ep":{"skipped":{"nightly":{"reason":"r","unblockedBy":"u","requires":"absent:x"}}}}`
	load := func(body string) error {
		p := filepath.Join(t.TempDir(), "known-blocked.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadKnownBlockedLedger(p)
		return err
	}
	if err := load(valid + "\n"); err != nil {
		t.Fatalf("trailing whitespace must load: %v", err)
	}
	for _, tail := range []string{"GARBAGE", "{}", "\n[]"} {
		if err := load(valid + tail); err == nil {
			t.Errorf("trailing %q must be rejected", tail)
		}
	}
}

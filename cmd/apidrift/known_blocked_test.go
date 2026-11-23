package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

// The known-blocked ledger (opnsense/testdata/schemas/known-blocked.json) lets a
// maintainer record what the lab can never exercise so the run stops warning
// about it. These tests pin both halves of that bargain: a ledgered item is
// quiet but still reported, and everything else - including every other finding
// on a ledgered endpoint, and a ledger entry that has stopped being true - keeps
// warning.

// withTestKnownBlocked points the known-blocked lookup at a temporary ledger.
func withTestKnownBlocked(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "known-blocked.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := knownBlockedPath
	knownBlockedPath = p
	t.Cleanup(func() { knownBlockedPath = prev })
}

// withTestProfile sets the probe target for one test.
func withTestProfile(t *testing.T, profile string) {
	t.Helper()
	prev := canaryProfile
	canaryProfile = profile
	t.Cleanup(func() { canaryProfile = prev })
}

const kbCoverage = `{
  "epA": {
    "required": [
      {"path":"rows[].addr","metrics":["opnsense_x_lease_info"],"exercise":"take a lease","blocker":"no client on the testbed"},
      {"path":"rows[].mac","metrics":["opnsense_x_lease_info"],"exercise":"take a lease","blocker":"no client on the testbed"},
      {"path":"opaque.field","opaque":true,"metrics":["opnsense_x_lease_info"],"exercise":"take a lease"}
    ]
  }
}`

const kbLedger = `{
  "epA": {
    "paths": [
      {"path":"rows[].addr","profiles":{
        "nightly":{"reason":"the lab has no DHCP client","unblockedBy":"a client holding a lease","requires":"ipsec-down"},
        "release-vm":{"reason":"release box has no client at all","unblockedBy":"a client on the release box","requires":"ipsec-down"}}}
    ]
  },
  "epSkip": {
    "skipped": {"nightly":{"reason":"plugin absent from the lab","unblockedBy":"install the plugin and configure one network","requires":"absent:zerotierNetworks"}}
  }
}`

func unverified(endpoint string, paths ...string) probeResult {
	return probeResult{Endpoint: endpoint, Res: opnsense.ValidationResult{
		Unverified: paths,
		Facts:      opnsense.PayloadFacts{RowsKnown: true},
	}}
}

// phase1NoChildSA is the shape both latest reports show: a configured phase 1
// row exists, every other required path resolved, and only install-time is
// unverified.
func phase1NoChildSA() probeResult {
	return probeResult{Endpoint: "ipsecPhase1", Res: opnsense.ValidationResult{
		Unverified: []string{"rows[].install-time"},
		Facts:      opnsense.PayloadFacts{RowsKnown: true, Rows: 1, Phase1DownRows: 1},
	}}
}

// The three endpoints showing the tunnel down, so a test's ipsec-down entry is
// decided by the part of the run that test varies.
var (
	kbP1  = probeResult{Endpoint: "ipsecPhase1", Res: opnsense.ValidationResult{Facts: opnsense.PayloadFacts{RowsKnown: true, Rows: 1, Phase1DownRows: 1}}}
	kbSad = probeResult{Endpoint: "ipsecSad", Res: opnsense.ValidationResult{Facts: opnsense.PayloadFacts{RowsKnown: true}}}
	kbSpd = probeResult{Endpoint: "ipsecSpd", Res: opnsense.ValidationResult{Facts: opnsense.PayloadFacts{RowsKnown: true}}}
)

// verifiedRow is an endpoint that served a row and reported nothing about it.
func verifiedRow(endpoint string) probeResult {
	return probeResult{Endpoint: endpoint, Res: opnsense.ValidationResult{Facts: opnsense.PayloadFacts{RowsKnown: true, Rows: 1}}}
}

func TestKnownBlockedSilencesOnlyLedgeredItems(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbLedger)
	withTestProfile(t, "nightly")

	// Every case carries both ledgered confirmations (the path still unverified,
	// the endpoint still skipped) so that only the variable part decides the
	// outcome; an entry the run does not confirm is the stale case below.
	confirmed := func(extra ...probeResult) []probeResult {
		return append([]probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}}, extra...)
	}
	cases := []struct {
		name         string
		results      []probeResult
		wantWarnings bool
	}{
		{"ledgered required path and skip do not warn", confirmed(), false},
		// Guardrail: a required path that is NOT in the ledger still warns, even
		// on the endpoint that has another path ledgered.
		{"unledgered required path on a ledgered endpoint still warns", []probeResult{unverified("epA", "rows[].addr", "rows[].mac"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}}, true},
		// Guardrail: a skipped endpoint that is NOT in the ledger still warns.
		{"unledgered skipped endpoint still warns", confirmed(probeResult{Endpoint: "epOther", SkippedParam: true}), true},
		// Opaque and state-optional paths are informational whatever the ledger says.
		{"opaque path beside the ledgered one stays quiet", []probeResult{unverified("epA", "rows[].addr", "opaque.field"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drift, warnings := aggregate(tc.results)
			if drift {
				t.Error("known-blocked handling must never create breaking drift")
			}
			if warnings != tc.wantWarnings {
				t.Errorf("warnings = %v, want %v", warnings, tc.wantWarnings)
			}
		})
	}
}

// A ledgered endpoint's OTHER findings keep their severity.
func TestKnownBlockedDoesNotMaskOtherFindingsOnTheEndpoint(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbLedger)
	withTestProfile(t, "nightly")

	blocked := func(extra func(*probeResult)) []probeResult {
		r := unverified("epA", "rows[].addr")
		extra(&r)
		return []probeResult{r, kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}}
	}
	cases := []struct {
		name         string
		results      []probeResult
		wantDrift    bool
		wantWarnings bool
	}{
		{"baseline is clean", blocked(func(*probeResult) {}), false, false},
		{"breaking mismatch is drift", blocked(func(r *probeResult) {
			r.Res.Mismatches = []opnsense.Mismatch{{Path: "rows[].addr", Expected: opnsense.KindString, Got: "number"}}
		}), true, false},
		{"missing path warns", blocked(func(r *probeResult) { r.Res.Missing = []string{"rows[].gone"} }), false, true},
		{"unexpected top-level key warns", blocked(func(r *probeResult) { r.Res.UnknownTopKeys = []string{"newkey"} }), false, true},
		{"unexpected nested key warns", blocked(func(r *probeResult) { r.Res.UnknownPaths = []string{"rows[].new"} }), false, true},
		{"probe error warns", blocked(func(r *probeResult) { r.ProbeErr = "boom" }), false, true},
		{"core 404 warns", blocked(func(r *probeResult) { r.Absent = true }), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drift, warnings := aggregate(tc.results)
			if drift != tc.wantDrift || warnings != tc.wantWarnings {
				t.Errorf("aggregate = (%v,%v), want (%v,%v)", drift, warnings, tc.wantDrift, tc.wantWarnings)
			}
		})
	}
}

// Same for a ledgered skipped endpoint: a probe error is not a skip.
func TestKnownBlockedSkipDoesNotMaskProbeError(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbLedger)
	withTestProfile(t, "nightly")

	_, warnings := aggregate([]probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", ProbeErr: "boom"}})
	if !warnings {
		t.Error("a probe error on a skip-ledgered endpoint must warn")
	}
}

// Entries are scoped to the probe target. A run that names no target, or a
// different one, applies none of them.
func TestKnownBlockedIsScopedToTheProfile(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbLedger)

	results := []probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}}
	for _, tc := range []struct {
		profile      string
		wantWarnings bool
	}{
		{"nightly", false},
		{"", true},
		{"release-vm", true}, // epA's path is ledgered here, the skip is not
	} {
		withTestProfile(t, tc.profile)
		if _, w := aggregate(results); w != tc.wantWarnings {
			t.Errorf("profile %q: warnings = %v, want %v", tc.profile, w, tc.wantWarnings)
		}
	}
}

// The ledger must not rot: an entry the run shows to be untrue warns, and the
// report names it.
func TestKnownBlockedStaleEntriesWarn(t *testing.T) {
	withTestKnownBlocked(t, kbLedger)
	withTestProfile(t, "nightly")

	cases := []struct {
		name     string
		coverage string
		results  []probeResult
		wantWhy  string
	}{
		{"path is now verified", kbCoverage,
			[]probeResult{verifiedRow("epA"), {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}},
			"now verified"},
		{"path no longer required (made stateOptional)", `{"epA":{"stateOptional":[{"path":"rows[].addr","reason":"r","pruneTrigger":"p"}]}}`,
			[]probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}},
			"no longer a required"},
		{"path no longer required (now opaque)", `{"epA":{"required":[{"path":"rows[].addr","opaque":true,"metrics":["opnsense_x"],"exercise":"e"}]}}`,
			[]probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}},
			"no longer a required"},
		{"path no longer in the coverage ledger", `{}`,
			[]probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}},
			"no longer a required"},
		{"skipped endpoint is now probed", kbCoverage,
			[]probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip"}},
			"no longer skipped"},
		{"ledgered endpoint was not probed", kbCoverage,
			[]probeResult{{Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}},
			"not probed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTestCoverageLedger(t, tc.coverage)
			if _, warnings := aggregate(tc.results); !warnings {
				t.Error("a stale ledger entry must warn")
			}
			report := renderReport(tc.results, nil)
			if !strings.Contains(report, "Stale known-blocked") {
				t.Errorf("report has no stale section:\n%s", report)
			}
			if !strings.Contains(report, tc.wantWhy) {
				t.Errorf("report does not say %q:\n%s", tc.wantWhy, report)
			}
		})
	}
}

// A ledgered item is recorded, never hidden: it appears with its reason and the
// box state that would unblock it, in an informational section.
func TestKnownBlockedIsReportedNotHidden(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbLedger)
	withTestProfile(t, "nightly")

	results := []probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd, {Endpoint: "epSkip", SkippedParam: true}, {Endpoint: "zerotierNetworks", Absent: true}}
	report := renderReport(results, nil)

	for _, want := range []string{
		"Known-blocked",
		"`epA` `rows[].addr`", "the lab has no DHCP client", "a client holding a lease",
		"`epSkip`", "plugin absent from the lab", "install the plugin and configure one network",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	// It must not be listed as a warning-level required path.
	if strings.Contains(report, "Unverified paths that back metrics") {
		t.Errorf("a ledgered path must not appear in the warning-level required section:\n%s", report)
	}
	if strings.Contains(report, "Stale known-blocked") {
		t.Errorf("a true entry must not be reported stale:\n%s", report)
	}
	// The ledgered skip is not also listed under the warning-level skip section.
	if strings.Contains(report, "Skipped (parameterized") {
		t.Errorf("a ledgered skip must not appear in the skipped section:\n%s", report)
	}
}

// An unreadable ledger must fail safe: nothing is quietened.
func TestKnownBlockedUnusableLedgerWarnsAboutEverything(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestProfile(t, "nightly")

	for name, body := range map[string]string{
		"malformed JSON":    "{nope",
		"unknown field":     `{"epA":{"paths":[{"path":"rows[].addr","profile":{"nightly":{"reason":"r","unblockedBy":"u","requires":"ipsec-down"}}}]}}`,
		"no required state": `{"epA":{"paths":[{"path":"rows[].addr","profiles":{"nightly":{"reason":"r","unblockedBy":"u"}}}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			withTestKnownBlocked(t, body)
			if _, warnings := aggregate([]probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd}); !warnings {
				t.Error("an unusable known-blocked ledger must leave the warning in place")
			}
		})
	}
}

// A stale entry is news: it must break the findings fingerprint's quiet.
func TestFingerprintChangesWhenAnEntryGoesStale(t *testing.T) {
	results := []probeResult{verifiedRow("epA")}
	base := findingsFingerprintWithReview("g", results, nil, nil)
	stale := findingsFingerprintWithReview("g", results, nil, []staleFinding{{Endpoint: "epA", Path: "rows[].addr", Why: "now verified"}})
	if base == stale {
		t.Error("a stale ledger entry must change the fingerprint")
	}
	if again := findingsFingerprintWithReview("g", results, nil, []staleFinding{{Endpoint: "epA", Path: "rows[].addr", Why: "now verified"}}); again != stale {
		t.Error("the fingerprint must be stable for identical findings")
	}
}

// The committed ledger, replayed against the two latest live reports: each
// target's standing warnings (four required IPsec paths plus the skipped
// ZeroTier info endpoint) are known-blocked, so the run is clean, and removing
// any of them from the run makes the entry stale.
func TestCommittedKnownBlockedLedgerClearsTheLatestReports(t *testing.T) {
	prevCov, prevKB := coverageLedgerPath, knownBlockedPath
	coverageLedgerPath = "../../opnsense/testdata/schemas/coverage.json"
	knownBlockedPath = "../../opnsense/testdata/schemas/known-blocked.json"
	t.Cleanup(func() { coverageLedgerPath, knownBlockedPath = prevCov, prevKB })

	standing := func() []probeResult {
		return []probeResult{
			phase1NoChildSA(),
			unverified("ipsecSad", "rows[]", "rows[].satype", "rows[].spi"),
			unverified("ipsecSpd", "rows[]", "rows[].dir"),
			{Endpoint: "zerotierNetworks", Absent: true}, {Endpoint: "zerotierNetworkInfo", SkippedParam: true},
		}
	}
	for _, profile := range opnsense.KnownProbeProfiles() {
		t.Run(profile, func(t *testing.T) {
			withTestProfile(t, profile)
			if drift, warnings := aggregate(standing()); drift || warnings {
				t.Errorf("aggregate = (%v,%v), want a clean run", drift, warnings)
			}
			report := renderReport(standing(), nil)
			for _, want := range []string{"Known-blocked", "ipsecPhase1", "rows[].install-time", "rows[].satype", "rows[].spi", "rows[].dir", "zerotierNetworkInfo"} {
				if !strings.Contains(report, want) {
					t.Errorf("report missing %q:\n%s", want, report)
				}
			}
			if strings.Contains(report, "Stale known-blocked") {
				t.Errorf("no entry should be stale on the standing run:\n%s", report)
			}

			// Any one of these endpoints no longer testifying breaks the run:
			// the IPsec state is one fact across all three, and the skip needs
			// its parameter source to answer 404.
			for i, r := range standing() {
				results := standing()
				results[i] = probeResult{Endpoint: r.Endpoint}
				if _, warnings := aggregate(results); !warnings {
					t.Errorf("clearing %s must warn", r.Endpoint)
				}
			}
			// A new required path going unverified is not covered by the ledger.
			extra := append(standing(), unverified("wireguardClients", "rows[].latest-handshake"))
			if _, warnings := aggregate(extra); !warnings {
				t.Error("a required path outside the ledger must still warn")
			}
		})
	}
}

// A skipped-endpoint entry only makes sense for an endpoint the prober can skip
// for want of a live parameter, so a rename or a resolver removal fails here
// rather than leaving a silent, stale entry behind.
func TestCommittedKnownBlockedSkipsAreParameterizedEndpoints(t *testing.T) {
	ledger, err := opnsense.LoadKnownBlockedLedger("../../opnsense/testdata/schemas/known-blocked.json")
	if err != nil {
		t.Fatal(err)
	}
	for endpoint, e := range ledger {
		if len(e.Skipped) == 0 {
			continue
		}
		_, byPath := getPathParamResolvers[endpoint]
		req, hasReq := opnsense.CaptureRequestFor(opnsense.EndpointName(endpoint))
		if !byPath && (!hasReq || !req.Parameterized) {
			t.Errorf("%s: a skipped entry, but the prober has no parameter resolver for it", endpoint)
		}
	}
}

// ---- intermittent entries -------------------------------------------------

// epA's path is intermittent on nightly (a flapping box state) and an ordinary
// entry on release-vm, so the same verified run is fine on one target and stale
// on the other.
const kbIntermittentLedger = `{
  "epA": {
    "paths": [
      {"path":"rows[].addr","profiles":{
        "nightly":{"reason":"the tunnel flaps","unblockedBy":"hold the tunnel up","requires":"ipsec-down","intermittent":true},
        "release-vm":{"reason":"release box has no peer","unblockedBy":"give it a peer","requires":"ipsec-down"}}}
    ]
  }
}`

func TestKnownBlockedIntermittentVerifiedIsShownNotStale(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbIntermittentLedger)
	withTestProfile(t, "nightly")

	up := []probeResult{verifiedRow("epA")} // the path resolved, so it is not unverified
	if drift, warnings := aggregate(up); drift || warnings {
		t.Errorf("an intermittent entry verified on this run: aggregate = (%v,%v), want clean", drift, warnings)
	}
	report := renderReport(up, nil)
	for _, want := range []string{"Known-blocked", "`epA` `rows[].addr`", "verified on this run", "the tunnel flaps", "hold the tunnel up"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "Stale known-blocked") {
		t.Errorf("an intermittent verified entry must not be stale:\n%s", report)
	}

	// Down, the same entry is the ordinary blocked case and does not claim to be verified.
	down := []probeResult{unverified("epA", "rows[].addr"), kbP1, kbSad, kbSpd}
	if _, warnings := aggregate(down); warnings {
		t.Error("an intermittent entry still unverified must not warn")
	}
	if strings.Contains(renderReport(down, nil), "verified on this run") {
		t.Error("an unverified path must not be reported as verified")
	}
}

func TestKnownBlockedNonIntermittentVerifiedStillWarns(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbIntermittentLedger)
	withTestProfile(t, "release-vm")

	results := []probeResult{verifiedRow("epA")}
	if _, warnings := aggregate(results); !warnings {
		t.Error("a verified path on a non-intermittent entry must warn as stale")
	}
	if !strings.Contains(renderReport(results, nil), "Stale known-blocked") {
		t.Error("the stale section is missing")
	}
}

// Intermittent only excuses a path that is verified. Every other contradiction
// still warns, as does everything else on the endpoint.
func TestKnownBlockedIntermittentStillWarnsOnOtherConditions(t *testing.T) {
	withTestKnownBlocked(t, kbIntermittentLedger)
	withTestProfile(t, "nightly")

	cases := []struct {
		name     string
		coverage string
		results  []probeResult
		drift    bool
	}{
		{"no longer in the coverage ledger", `{}`, []probeResult{verifiedRow("epA")}, false},
		{"made stateOptional", `{"epA":{"stateOptional":[{"path":"rows[].addr","reason":"r","pruneTrigger":"p"}]}}`, []probeResult{verifiedRow("epA")}, false},
		{"now opaque", `{"epA":{"required":[{"path":"rows[].addr","opaque":true,"metrics":["opnsense_x"],"exercise":"e"}]}}`, []probeResult{verifiedRow("epA")}, false},
		{"endpoint absent", kbCoverage, []probeResult{{Endpoint: "epA", Absent: true}}, false},
		{"endpoint skipped", kbCoverage, []probeResult{{Endpoint: "epA", SkippedParam: true}}, false},
		{"endpoint not probed", kbCoverage, []probeResult{{Endpoint: "other"}}, false},
		{"probe error", kbCoverage, []probeResult{{Endpoint: "epA", ProbeErr: "boom"}}, false},
		{"missing path", kbCoverage, []probeResult{{Endpoint: "epA", Res: opnsense.ValidationResult{Missing: []string{"rows[].gone"}}}}, false},
		{"unexpected key", kbCoverage, []probeResult{{Endpoint: "epA", Res: opnsense.ValidationResult{UnknownTopKeys: []string{"newkey"}}}}, false},
		{"unexpected nested key", kbCoverage, []probeResult{{Endpoint: "epA", Res: opnsense.ValidationResult{UnknownPaths: []string{"rows[].new"}}}}, false},
		{"breaking drift", kbCoverage, []probeResult{{Endpoint: "epA", Res: opnsense.ValidationResult{Mismatches: []opnsense.Mismatch{{Path: "rows[].addr", Expected: opnsense.KindString, Got: "number"}}}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTestCoverageLedger(t, tc.coverage)
			drift, warnings := aggregate(tc.results)
			if drift != tc.drift {
				t.Errorf("drift = %v, want %v", drift, tc.drift)
			}
			if !drift && !warnings {
				t.Error("must warn")
			}
		})
	}
}

// The tunnel being up or down must not change the findings digest, or the issue
// would churn with the tunnel. Checked on the committed ledger against the real
// IPsec endpoints.
func TestFingerprintIsTheSameWhetherTheIntermittentTunnelIsUpOrDown(t *testing.T) {
	prevCov, prevKB := coverageLedgerPath, knownBlockedPath
	coverageLedgerPath = "../../opnsense/testdata/schemas/coverage.json"
	knownBlockedPath = "../../opnsense/testdata/schemas/known-blocked.json"
	t.Cleanup(func() { coverageLedgerPath, knownBlockedPath = prevCov, prevKB })
	withTestProfile(t, "nightly")

	down := []probeResult{
		phase1NoChildSA(),
		unverified("ipsecSad", "rows[]", "rows[].satype", "rows[].spi"),
		unverified("ipsecSpd", "rows[]", "rows[].dir"),
		{Endpoint: "zerotierNetworks", Absent: true}, {Endpoint: "zerotierNetworkInfo", SkippedParam: true},
	}
	up := []probeResult{
		verifiedRow("ipsecPhase1"), verifiedRow("ipsecSad"), verifiedRow("ipsecSpd"),
		{Endpoint: "zerotierNetworks", Absent: true}, {Endpoint: "zerotierNetworkInfo", SkippedParam: true},
	}
	fp := func(results []probeResult) string {
		run := analyzeRun(results)
		return findingsFingerprintWithReview("devel 1", results, run.Coverage.RequiredUnresolved, run.Stale)
	}
	for name, results := range map[string][]probeResult{"down": down, "up": up} {
		if drift, warnings := aggregate(results); drift || warnings {
			t.Fatalf("%s: aggregate = (%v,%v), want clean", name, drift, warnings)
		}
	}
	if a, b := fp(down), fp(up); a != b {
		t.Errorf("fingerprint differs between tunnel down (%s) and up (%s)", a, b)
	}
}

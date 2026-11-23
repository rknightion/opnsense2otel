package main

import (
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

// These tests run real response payloads through ValidateResponseSchema,
// aggregate and renderReport against the committed ledgers. "Unverified" alone
// must never quieten a ledger entry: the run has to show the box state the
// entry's reason names.

const (
	// Shapes OPNsense's searchPhase1Action produces: install-time stays null
	// until a child SA installs.
	p1Down        = `{"phase1desc":"d","connected":false,"ikeid":"1","name":"con1","install-time":null,"bytes-in":0,"bytes-out":0,"packets-in":0,"packets-out":0}`
	p1Up          = `{"phase1desc":"d","connected":true,"ikeid":"1","name":"con1","install-time":"5","bytes-in":1,"bytes-out":1,"packets-in":1,"packets-out":1}`
	p1UpNoInstall = `{"phase1desc":"d","connected":true,"ikeid":"1","name":"con1","install-time":null,"bytes-in":0,"bytes-out":0,"packets-in":0,"packets-out":0}`
	p1Renamed     = `{"phase1desc":"d","connected":true,"ikeid":"1","name":"con1","installed-at":"5","bytes-in":1,"bytes-out":1,"packets-in":1,"packets-out":1}`

	sadPlaceholder = `{"src":"No","dst":"SAD","satype":null,"spi":null,"nat":"ntries","id":"x","ikeid":null,"phase1desc":null,"phase2desc":null}`
	sadTail        = `"reqid":1,"state":"mature","nat":"4500","addtime_diff":42,"addtime_hard":3600,"addtime_soft":3000,"ikeid":"1","phase1desc":"p1","phase2desc":"p2","bytes_current":"1","bytes_hard":"0","bytes_soft":"0","allocated":"1","allocated_hard":"0","allocated_soft":"0"}`
	sadHealthy     = `{"src":"192.0.2.1","dst":"192.0.2.2","satype":"esp","spi":"c6524517",` + sadTail
	sadSpiRenamed  = `{"src":"192.0.2.1","dst":"192.0.2.2","satype":"esp","spi_v2":"c6524517",` + sadTail
	sadTypeRenamed = `{"src":"192.0.2.1","dst":"192.0.2.2","satype_v2":"esp","spi":"c6524517",` + sadTail
	sadSpiNull     = `{"src":"192.0.2.1","dst":"192.0.2.2","satype":"esp","spi":null,` + sadTail
	// Placeholder-shaped, but carrying a value the synthetic row never has.
	sadNearPlaceholder = `{"src":"No","dst":"SAD","satype":null,"spi":null,"nat":"ntries","reqid":1}`

	spdPolicy = `{"dir":"out"}`

	p1NoConnected   = `{"phase1desc":"d","ikeid":"2","name":"con2","install-time":null,"bytes-in":0,"bytes-out":0,"packets-in":0,"packets-out":0}`
	p1NullIkeid     = `{"phase1desc":"d","connected":false,"ikeid":null,"name":"bypass","install-time":null,"bytes-in":0,"bytes-out":0,"packets-in":0,"packets-out":0}`
	p1DownInstalled = `{"phase1desc":"d","connected":false,"ikeid":"1","name":"con1","install-time":"5","bytes-in":1,"bytes-out":1,"packets-in":1,"packets-out":1}`
	p1NoInstallKey  = `{"phase1desc":"d","connected":false,"ikeid":"2","name":"con2","bytes-in":0,"bytes-out":0,"packets-in":0,"packets-out":0}`
)

func withCommittedLedgers(t *testing.T) {
	t.Helper()
	prevCov, prevKB := coverageLedgerPath, knownBlockedPath
	coverageLedgerPath = "../../opnsense/testdata/schemas/coverage.json"
	knownBlockedPath = "../../opnsense/testdata/schemas/known-blocked.json"
	t.Cleanup(func() { coverageLedgerPath, knownBlockedPath = prevCov, prevKB })
}

func rowsPayload(rows ...string) string {
	return `{"total":1,"rowCount":1,"current":1,"rows":[` + strings.Join(rows, ",") + `]}`
}

// validated drives one payload through the real validator with the committed
// exemptions for the active profile, as probeOne does.
func validated(t *testing.T, endpoint, payload string) probeResult {
	t.Helper()
	schemas, err := opnsense.AllEndpointSchemas()
	if err != nil {
		t.Fatal(err)
	}
	exemptions, err := loadExemptions("../../opnsense/testdata/schemas/exemptions.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range schemas {
		if s.Endpoint == endpoint {
			vr, err := opnsense.ValidateResponseSchema(s, []byte(payload), exemptions[endpoint].ForProfile(canaryProfile))
			if err != nil {
				t.Fatal(err)
			}
			return probeResult{Endpoint: endpoint, Res: vr}
		}
	}
	t.Fatalf("no schema for %s", endpoint)
	return probeResult{}
}

func ipsecRun(t *testing.T, p1, sad, spd []string) []probeResult {
	t.Helper()
	return []probeResult{
		validated(t, "ipsecPhase1", rowsPayload(p1...)),
		validated(t, "ipsecSad", rowsPayload(sad...)),
		validated(t, "ipsecSpd", rowsPayload(spd...)),
		{Endpoint: "zerotierNetworks", Absent: true},
		{Endpoint: "zerotierNetworkInfo", SkippedParam: true},
	}
}

func runFingerprint(results []probeResult) string {
	run := analyzeRun(results)
	return findingsFingerprintWithReview("g", results, run.Coverage.RequiredUnresolved, run.Stale)
}

func staleFor(results []probeResult, endpoint, path string) (staleFinding, bool) {
	for _, s := range analyzeRun(results).Stale {
		if s.Endpoint == endpoint && s.Path == path {
			return s, true
		}
	}
	return staleFinding{}, false
}

func blockedFor(results []probeResult, endpoint, path string) bool {
	for _, b := range analyzeRun(results).Blocked {
		if b.Endpoint == endpoint && b.Path == path {
			return true
		}
	}
	return false
}

// The state the ledger describes, on both targets: quiet, and the same digest
// nightly produces with the tunnel up.
func TestKnownBlockedDownStateIsQuiet(t *testing.T) {
	withCommittedLedgers(t)
	for _, profile := range opnsense.KnownProbeProfiles() {
		t.Run(profile, func(t *testing.T) {
			withTestProfile(t, profile)
			for name, sad := range map[string][]string{"placeholder row": {sadPlaceholder}, "no rows": {}} {
				down := ipsecRun(t, []string{p1Down}, sad, nil)
				if drift, warnings := aggregate(down); drift || warnings {
					t.Errorf("%s: aggregate = (%v,%v), want clean\n%s", name, drift, warnings, renderReport(down, nil))
				}
			}
		})
	}
	withTestProfile(t, opnsense.ProbeProfileNightly)
	down := ipsecRun(t, []string{p1Down}, []string{sadPlaceholder}, nil)
	up := ipsecRun(t, []string{p1Up}, []string{sadHealthy}, []string{spdPolicy})
	if drift, warnings := aggregate(up); drift || warnings {
		t.Errorf("tunnel up: aggregate = (%v,%v), want clean\n%s", drift, warnings, renderReport(up, nil))
	}
	if a, b := runFingerprint(down), runFingerprint(up); a != b {
		t.Errorf("fingerprint differs between tunnel down (%s) and up (%s)", a, b)
	}
}

// Finding 1: a real SA row the placeholder rule drops is not an empty SAD.
func TestKnownBlockedDroppedRealSARowWarns(t *testing.T) {
	withCommittedLedgers(t)
	rows := map[string]string{
		"spi renamed":      sadSpiRenamed,
		"satype renamed":   sadTypeRenamed,
		"spi null":         sadSpiNull,
		"near placeholder": sadNearPlaceholder,
	}
	for _, profile := range opnsense.KnownProbeProfiles() {
		for name, row := range rows {
			t.Run(profile+"/"+name, func(t *testing.T) {
				withTestProfile(t, profile)
				results := ipsecRun(t, []string{p1Down}, []string{row}, nil)
				quiet := runFingerprint(ipsecRun(t, []string{p1Down}, []string{sadPlaceholder}, nil))
				if profile == opnsense.ProbeProfileNightly {
					// The reported failure: everything else about the tunnel is up.
					results = ipsecRun(t, []string{p1Up}, []string{row}, []string{spdPolicy})
				}
				drift, warnings := aggregate(results)
				if drift || !warnings {
					t.Errorf("aggregate = (%v,%v), want a warning and no drift", drift, warnings)
				}
				if blockedFor(results, "ipsecSad", "rows[].spi") || blockedFor(results, "ipsecSad", "rows[].satype") {
					t.Error("a dropped real row must not confirm a SAD entry")
				}
				report := renderReport(results, nil)
				for _, want := range []string{"Stale known-blocked", "Unverified paths that back metrics", "`ipsecSad` `rows[].spi`"} {
					if !strings.Contains(report, want) {
						t.Errorf("report missing %q:\n%s", want, report)
					}
				}
				if runFingerprint(results) == quiet {
					t.Error("the fingerprint must differ from the quiet tunnel-down run")
				}
			})
		}
	}
}

// Finding 2: a connected phase 1 row with no install time is not the blocked state.
func TestKnownBlockedConnectedPhase1Warns(t *testing.T) {
	withCommittedLedgers(t)
	for _, profile := range opnsense.KnownProbeProfiles() {
		t.Run(profile, func(t *testing.T) {
			withTestProfile(t, profile)
			for name, rows := range map[string][]string{
				"only row connected": {p1UpNoInstall},
				"one of two":         {p1Down, p1UpNoInstall},
			} {
				results := ipsecRun(t, rows, []string{sadPlaceholder}, nil)
				if _, warnings := aggregate(results); !warnings {
					t.Errorf("%s: a connected row with a null install-time must warn", name)
				}
				if blockedFor(results, "ipsecPhase1", "rows[].install-time") {
					t.Errorf("%s: the entry must not be confirmed", name)
				}
				if _, ok := staleFor(results, "ipsecPhase1", "rows[].install-time"); !ok {
					t.Errorf("%s: the entry must be reported stale", name)
				}
			}
		})
	}
}

// Finding 4: a renamed path is missing, and the report must not call it verified.
func TestKnownBlockedMissingPathIsNotVerified(t *testing.T) {
	withCommittedLedgers(t)
	for _, profile := range opnsense.KnownProbeProfiles() {
		t.Run(profile, func(t *testing.T) {
			withTestProfile(t, profile)
			results := ipsecRun(t, []string{p1Renamed}, []string{sadPlaceholder}, nil)
			if _, warnings := aggregate(results); !warnings {
				t.Error("a renamed path must warn")
			}
			if blockedFor(results, "ipsecPhase1", "rows[].install-time") {
				t.Error("a missing path must not be confirmed")
			}
			s, ok := staleFor(results, "ipsecPhase1", "rows[].install-time")
			if !ok {
				t.Fatal("a missing path must make the entry stale")
			}
			if !strings.Contains(s.Why, "missing") || strings.Contains(s.Why, "verified on this run") || strings.Contains(s.Why, "now verified") {
				t.Errorf("stale reason = %q, want it to say the path is missing", s.Why)
			}
			if strings.Contains(renderReport(results, nil), "verified on this run") {
				t.Error("the report must not call a missing path verified")
			}
		})
	}
}

// Finding 6: an absent endpoint cannot confirm a path entry.
func TestKnownBlockedAbsentEndpointIsStale(t *testing.T) {
	withCommittedLedgers(t)
	for _, profile := range opnsense.KnownProbeProfiles() {
		t.Run(profile, func(t *testing.T) {
			withTestProfile(t, profile)
			results := ipsecRun(t, []string{p1Down}, []string{sadPlaceholder}, nil)
			results[2] = probeResult{Endpoint: "ipsecSpd", Absent: true}
			s, ok := staleFor(results, "ipsecSpd", "rows[].dir")
			if !ok || !strings.Contains(s.Why, "absent") {
				t.Errorf("stale = %+v, %v; want an absent-endpoint stale finding", s, ok)
			}
			if blockedFor(results, "ipsecSpd", "rows[].dir") {
				t.Error("an absent endpoint must not confirm the entry")
			}
		})
	}
}

// The ZeroTier skip is excused by the plugin being absent, not by the skip alone.
func TestKnownBlockedSkipNeedsItsParameterSourceAbsent(t *testing.T) {
	withCommittedLedgers(t)
	for _, profile := range opnsense.KnownProbeProfiles() {
		t.Run(profile, func(t *testing.T) {
			withTestProfile(t, profile)
			results := ipsecRun(t, []string{p1Down}, []string{sadPlaceholder}, nil)
			// Plugin installed, no network configured: still skipped, different state.
			results[3] = validated(t, "zerotierNetworks", `{"total":0,"rowCount":0,"current":1,"rows":[]}`)
			if _, warnings := aggregate(results); !warnings {
				t.Error("a skip whose parameter source answered must warn")
			}
			if _, ok := staleFor(results, "zerotierNetworkInfo", ""); !ok {
				t.Error("the skip entry must be reported stale")
			}
		})
	}
}

// A rows array with nothing in it cannot back a "verified" claim, even if the
// path is somehow not listed as unverified.
func TestKnownBlockedZeroRowsNeverClaimVerified(t *testing.T) {
	withTestCoverageLedger(t, kbCoverage)
	withTestKnownBlocked(t, kbIntermittentLedger)
	withTestProfile(t, opnsense.ProbeProfileNightly)
	results := []probeResult{{Endpoint: "epA", Res: opnsense.ValidationResult{Facts: opnsense.PayloadFacts{RowsKnown: true}}}}
	if _, warnings := aggregate(results); !warnings {
		t.Error("must warn")
	}
	if strings.Contains(renderReport(results, nil), "verified on this run") {
		t.Error("zero rows must not be reported as verified")
	}
}

// The digest main publishes must carry stale entries, or a ledger that stopped
// being true would not break the workflow's quiet.
func TestReviewedFingerprintCarriesStaleEntries(t *testing.T) {
	withCommittedLedgers(t)
	withTestProfile(t, opnsense.ProbeProfileNightly)
	results := ipsecRun(t, []string{p1UpNoInstall}, []string{sadPlaceholder}, nil)
	run := analyzeRun(results)
	if len(run.Stale) == 0 {
		t.Fatal("the fixture must produce a stale entry")
	}
	got := reviewedFingerprint("g", results)
	if want := findingsFingerprintWithReview("g", results, run.Coverage.RequiredUnresolved, run.Stale); got != want {
		t.Errorf("fingerprint = %s, want %s", got, want)
	}
	if without := findingsFingerprintWithReview("g", results, run.Coverage.RequiredUnresolved, nil); got == without {
		t.Error("the published fingerprint ignores stale entries")
	}
}

// Findings 3 and 5: the ledger claims only what the reports and code support.
func TestCommittedKnownBlockedReasonsMatchTheEvidence(t *testing.T) {
	ledger, err := opnsense.LoadKnownBlockedLedger("../../opnsense/testdata/schemas/known-blocked.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range opnsense.KnownProbeProfiles() {
		for _, it := range ledger.ForProfile(profile) {
			text := it.Reason + " " + it.UnblockedBy
			for _, banned := range []string{"empty rows", "returned an empty", "e2e smoke", "smoke covers"} {
				if strings.Contains(text, banned) {
					t.Errorf("%s %s %s: claims %q", profile, it.Endpoint, it.Path, banned)
				}
			}
		}
	}
}

// confirmedUnverifiedIPsec lists IPsec entries the run quietened while their
// path was still unverified.
func confirmedUnverifiedIPsec(results []probeResult) []string {
	var out []string
	for _, b := range analyzeRun(results).Blocked {
		if strings.HasPrefix(b.Endpoint, "ipsec") && !b.Verified {
			out = append(out, b.Endpoint+" "+b.Path)
		}
	}
	return out
}

// A live tunnel on one endpoint contradicts "down" on the others: the state is
// one fact about the run, not three separate facts.
func TestKnownBlockedIPsecDownIsRunWide(t *testing.T) {
	withCommittedLedgers(t)
	withTestProfile(t, opnsense.ProbeProfileNightly)
	cases := []struct {
		name          string
		p1, sad, spd  []string
		wantUnblocked []string
	}{
		{"phase 1 up, SAD and SPD empty", []string{p1Up}, nil, nil, []string{"ipsecSad rows[].spi", "ipsecSad rows[].satype", "ipsecSpd rows[].dir"}},
		{"phase 1 up, SAD placeholder, SPD empty", []string{p1Up}, []string{sadPlaceholder}, nil, []string{"ipsecSad rows[].spi", "ipsecSad rows[].satype", "ipsecSpd rows[].dir"}},
		{"phase 1 down, real SAD and SPD rows", []string{p1Down}, []string{sadHealthy}, []string{spdPolicy}, []string{"ipsecPhase1 rows[].install-time"}},
		{"phase 1 down, real SAD row, SPD empty", []string{p1Down}, []string{sadHealthy}, nil, []string{"ipsecPhase1 rows[].install-time", "ipsecSpd rows[].dir"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := ipsecRun(t, tc.p1, tc.sad, tc.spd)
			drift, warnings := aggregate(results)
			if drift || !warnings {
				t.Errorf("aggregate = (%v,%v), want a warning and no drift", drift, warnings)
			}
			if got := confirmedUnverifiedIPsec(results); len(got) != 0 {
				t.Errorf("confirmed while unverified: %v", got)
			}
			for _, want := range tc.wantUnblocked {
				ep, path, _ := strings.Cut(want, " ")
				if _, ok := staleFor(results, ep, path); !ok {
					t.Errorf("%s must be reported stale", want)
				}
			}
		})
	}
	// An endpoint that cannot testify blocks every IPsec entry.
	for name, breakIt := range map[string]func([]probeResult) []probeResult{
		"SPD not probed": func(r []probeResult) []probeResult { return append(r[:2:2], r[3:]...) },
		"SAD probe error": func(r []probeResult) []probeResult {
			r[1] = probeResult{Endpoint: "ipsecSad", ProbeErr: "HTTP 500"}
			return r
		},
		"phase 1 absent": func(r []probeResult) []probeResult {
			r[0] = probeResult{Endpoint: "ipsecPhase1", Absent: true}
			return r
		},
		"SPD skipped": func(r []probeResult) []probeResult {
			r[2] = probeResult{Endpoint: "ipsecSpd", SkippedParam: true}
			return r
		},
		"SAD facts unknown": func(r []probeResult) []probeResult {
			r[1] = validated(t, "ipsecSad", `{"total":0}`)
			return r
		},
	} {
		t.Run(name, func(t *testing.T) {
			results := breakIt(ipsecRun(t, []string{p1Down}, []string{sadPlaceholder}, nil))
			if _, warnings := aggregate(results); !warnings {
				t.Error("must warn")
			}
			if got := confirmedUnverifiedIPsec(results); len(got) != 0 {
				t.Errorf("confirmed while unverified: %v", got)
			}
		})
	}
}

// Each guard in the phase 1 and SPD facts, exercised by a row the others pass.
func TestKnownBlockedMixedRowsDoNotShowTheTunnelDown(t *testing.T) {
	withCommittedLedgers(t)
	withTestProfile(t, opnsense.ProbeProfileNightly)
	for name, tc := range map[string]struct{ p1, sad, spd []string }{
		"SPD row with a null dir":          {[]string{p1Down}, []string{sadPlaceholder}, []string{`{"dir":null}`}},
		"phase 1 row with no connected":    {[]string{p1Down, p1NoConnected}, []string{sadPlaceholder}, nil},
		"phase 1 row with a null ikeid":    {[]string{p1Down, p1NullIkeid}, []string{sadPlaceholder}, nil},
		"phase 1 row with no install-time": {[]string{p1Down, p1NoInstallKey}, []string{sadPlaceholder}, nil},
		"down row with an installed child": {[]string{p1DownInstalled}, []string{sadPlaceholder}, nil},
		"no phase 1 rows":                  {nil, []string{sadPlaceholder}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			results := ipsecRun(t, tc.p1, tc.sad, tc.spd)
			if _, warnings := aggregate(results); !warnings {
				t.Error("must warn")
			}
			if got := confirmedUnverifiedIPsec(results); len(got) != 0 {
				t.Errorf("confirmed while unverified: %v", got)
			}
		})
	}
}

// A response with no rows has verified nothing, whatever the path lists say.
func TestKnownBlockedEmptyShapesNeverClaimVerified(t *testing.T) {
	withCommittedLedgers(t)
	withTestProfile(t, opnsense.ProbeProfileNightly)
	for _, payload := range []string{`[]`, `{"total":0}`} {
		for i, endpoint := range []string{"ipsecPhase1", "ipsecSad", "ipsecSpd"} {
			t.Run(endpoint+" "+payload, func(t *testing.T) {
				results := ipsecRun(t, []string{p1Up}, []string{sadHealthy}, []string{spdPolicy})
				results[i] = validated(t, endpoint, payload)
				if _, warnings := aggregate(results); !warnings {
					t.Error("must warn")
				}
				for _, b := range analyzeRun(results).Blocked {
					if b.Endpoint == endpoint {
						t.Errorf("%s %s must not be confirmed", b.Endpoint, b.Path)
					}
				}
			})
		}
	}
}

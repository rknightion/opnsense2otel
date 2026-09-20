package main

import (
	"strings"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

func resultWithMissing(endpoint string, missing ...string) probeResult {
	return probeResult{
		Endpoint: endpoint,
		Res:      opnsense.ValidationResult{Endpoint: endpoint, Missing: missing},
	}
}

func TestFindingsFingerprintIsStableAndSensitive(t *testing.T) {
	base := []probeResult{
		resultWithMissing("alpha", "rows[].a"),
		resultWithMissing("beta", "rows[].b"),
	}

	t.Run("the same findings in a different probe order hash the same", func(t *testing.T) {
		reordered := []probeResult{base[1], base[0]}
		if findingsFingerprint("release 26.7.1_1", base) != findingsFingerprint("release 26.7.1_1", reordered) {
			t.Error("fingerprint changed with probe order; the sweep order is not a finding")
		}
	})

	t.Run("a new finding changes the hash", func(t *testing.T) {
		extra := append(append([]probeResult(nil), base...), resultWithMissing("gamma", "rows[].c"))
		if findingsFingerprint("release 26.7.1_1", base) == findingsFingerprint("release 26.7.1_1", extra) {
			t.Error("fingerprint survived a new finding; real drift would go unreported")
		}
	})

	t.Run("a changed generation changes the hash", func(t *testing.T) {
		// A clean run against a NEWLY UPDATED box is new information even when
		// the findings are identical - it is the evidence the update landed.
		if findingsFingerprint("release 26.7.1_1", base) == findingsFingerprint("release 26.7.4", base) {
			t.Error("fingerprint ignored the generation; an update would post nothing")
		}
	})

	t.Run("two separate runs finding the same things hash the same", func(t *testing.T) {
		// Distinct slices, equal content - the real question is whether a
		// SECOND run over an unchanged box reproduces the first run's hash,
		// which is the whole basis for staying quiet. Hashing the identical
		// slice twice would only prove the function is not random.
		first := []probeResult{{Endpoint: "alpha"}, resultWithMissing("beta", "rows[].b")}
		second := []probeResult{{Endpoint: "alpha"}, resultWithMissing("beta", "rows[].b")}
		if findingsFingerprint("devel 27.1.a_40", first) != findingsFingerprint("devel 27.1.a_40", second) {
			t.Error("two runs with identical findings disagreed; every run would re-post")
		}
	})

	t.Run("every finding kind reaches the hash", func(t *testing.T) {
		clean := []probeResult{{Endpoint: "alpha"}}
		for name, mutated := range map[string][]probeResult{
			"missing":        {resultWithMissing("alpha", "rows[].a")},
			"unknown top":    {{Endpoint: "alpha", Res: opnsense.ValidationResult{Endpoint: "alpha", UnknownTopKeys: []string{"widget"}}}},
			"unknown nested": {{Endpoint: "alpha", Res: opnsense.ValidationResult{Endpoint: "alpha", UnknownPaths: []string{"rows[].x"}}}},
			"mismatch":       {{Endpoint: "alpha", Res: opnsense.ValidationResult{Endpoint: "alpha", Mismatches: []opnsense.Mismatch{{Path: "rows[].n"}}}}},
			"absent":         {{Endpoint: "alpha", Absent: true}},
			"probe error":    {{Endpoint: "alpha", ProbeErr: "HTTP 0"}},
		} {
			if findingsFingerprint("devel 27.1.a_40", clean) == findingsFingerprint("devel 27.1.a_40", mutated) {
				t.Errorf("%s finding did not reach the fingerprint", name)
			}
		}
	})

	t.Run("the hash is short and marker-safe", func(t *testing.T) {
		fp := findingsFingerprint("release 26.7.1_1", base)
		if len(fp) != 16 {
			t.Errorf("fingerprint %q is %d chars, want 16", fp, len(fp))
		}
		if strings.ContainsAny(fp, " -->\n") {
			t.Errorf("fingerprint %q would break the HTML comment marker", fp)
		}
	})
}

// The metric-name count and collector tally are appended to the report by the
// workflow's smoke step, AFTER this tool has run. They are exporter-side facts
// that wobble with any collector change, and folding them in would re-post a
// 200-line drift report because one metric name appeared. The workflow composes
// them into its own marker instead.
func TestFingerprintIgnoresReportProse(t *testing.T) {
	results := []probeResult{resultWithMissing("alpha", "rows[].a")}
	before := findingsFingerprint("release 26.7.1_1", results)
	results[0].Retried = true
	results[0].Status = 200
	if findingsFingerprint("release 26.7.1_1", results) != before {
		t.Error("fingerprint moved on a retry/status change; neither is a finding")
	}
}

// The baseline a run compares against must be the last FULL REPORT, never the
// last comment. If a heartbeat could be the baseline, every heartbeat would
// make the next run re-post the whole report.
func TestParseFingerprintMarker(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a full report yields its hash",
			body: "## OPNsense live-box schema canary — release 26.7.1_1\n\nProbed 202 endpoints.\n\n" + fingerprintMarker("deadbeefdeadbeef"),
			want: "deadbeefdeadbeef",
		},
		{
			name: "a heartbeat carries no marker and is never a baseline",
			body: "Live-box canary still running and still clean on release-vm. Nothing has changed since the last report.",
			want: "",
		},
		{
			name: "a human reply is not a baseline either",
			body: "Looked at this, the netbird absence is expected.",
			want: "",
		},
		{
			name: "a truncated marker is not mistaken for a hash",
			body: "## report\n" + fingerprintMarkerPrefix + "deadbeef",
			want: "",
		},
		{
			// A quoted older report inside a newer one must not win over the
			// comment's own trailing marker.
			name: "the last marker in the body wins",
			body: fingerprintMarker("0000000000000000") + "\nquoted above\n" + fingerprintMarker("1111111111111111"),
			want: "1111111111111111",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseFingerprintMarker(tc.body); got != tc.want {
				t.Errorf("parseFingerprintMarker = %q, want %q", got, tc.want)
			}
		})
	}
}

// Round-trip: whatever the tool writes, the reader must get back.
func TestFingerprintMarkerRoundTrips(t *testing.T) {
	fp := findingsFingerprint("devel 27.1.a_40", []probeResult{resultWithMissing("alpha", "rows[].a")})
	if got := parseFingerprintMarker("report body\n\n" + fingerprintMarker(fp)); got != fp {
		t.Errorf("round trip gave %q, want %q", got, fp)
	}
}

// RequiredUnresolved is the OTHER thing that sets warnings=true on an otherwise
// clean run, alongside a skipped parameterised endpoint - which is why the lab
// issues never auto-closed and re-posted the whole report every run. A required
// path becoming verified, or a new one going unverified, is real news, so it has
// to move the digest or the change would land in silence.
func TestFingerprintCoversRequiredCoverage(t *testing.T) {
	results := []probeResult{{Endpoint: "alpha"}}
	none := findingsFingerprintWithCoverage("devel 27.1.a_40", results, nil)

	one := findingsFingerprintWithCoverage("devel 27.1.a_40", results,
		[]coverageFinding{{Endpoint: "alpha", Path: "rows[].vhid"}})
	if none == one {
		t.Error("an unresolved required path did not reach the fingerprint")
	}

	// Order must not matter here either.
	pair := []coverageFinding{
		{Endpoint: "alpha", Path: "rows[].vhid"},
		{Endpoint: "beta", Path: "rows[].other"},
	}
	reversed := []coverageFinding{pair[1], pair[0]}
	if findingsFingerprintWithCoverage("devel 27.1.a_40", results, pair) !=
		findingsFingerprintWithCoverage("devel 27.1.a_40", results, reversed) {
		t.Error("coverage finding order changed the fingerprint")
	}
}

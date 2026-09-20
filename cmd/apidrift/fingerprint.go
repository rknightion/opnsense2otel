package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// findingsFingerprint digests what a run FOUND, so the workflow can tell a
// genuinely unchanged result from a re-post and stay quiet (OPN-0112).
//
// The issues are the canary's output channel, and they were drowning: ten
// consecutive byte-identical clean comments on each lab issue, because a
// standing coverage warning keeps the issue open and every run appends the
// whole report again. Real drift landing in that thread is invisible, which is
// the failure OPN-0096 already had to clean up once.
//
// WHAT IS IN THE DIGEST: the generation, and every finding as
// endpoint + kind + path. A clean run against a newly updated box therefore
// hashes differently and DOES post - the update is the news.
//
// WHAT IS DELIBERATELY OUT: probe order, HTTP status, whether a probe was
// retried, and everything the workflow's smoke step appends afterwards (the
// metric-name count, the collector tally). Those wobble for reasons that are
// not drift, and folding them in would re-post a 200-line report because one
// metric name appeared. The workflow composes the smoke verdict into its own
// marker on top of this.
func findingsFingerprint(generation string, results []probeResult) string {
	return findingsFingerprintWithCoverage(generation, results, nil)
}

// findingsFingerprintWithCoverage is findingsFingerprint plus the run's
// unresolved required-coverage paths.
//
// Those matter here for the same reason the issues drowned in the first place:
// a standing RequiredUnresolved set, and a parameterised endpoint with no live
// parameter, both set warnings=true forever, so the issue never auto-closes and
// every run appends the report again. They are stable, so they hash stable and
// the run stays quiet - but a required path becoming verified, or a new one
// going unverified, is real news and must break that quiet.
func findingsFingerprintWithCoverage(generation string, results []probeResult, required []coverageFinding) string {
	lines := make([]string, 0, len(results)+len(required))
	for _, f := range required {
		lines = append(lines, f.Endpoint+"\x00required-unresolved\x00"+f.Path)
	}
	add := func(endpoint, kind, detail string) {
		lines = append(lines, endpoint+"\x00"+kind+"\x00"+detail)
	}

	for _, r := range results {
		switch {
		case r.ProbeErr != "":
			// The message carries the status/transport reason, not a payload
			// value - probeResult.ProbeErr is built from status codes and
			// transport errors only, never a response body.
			add(r.Endpoint, "probe-error", r.ProbeErr)
			continue
		case r.Absent:
			add(r.Endpoint, "absent", "")
			continue
		case r.SkippedParam:
			add(r.Endpoint, "skipped", "")
			continue
		}
		for _, p := range r.Res.Missing {
			add(r.Endpoint, "missing", p)
		}
		for _, p := range r.Res.UnknownTopKeys {
			add(r.Endpoint, "unknown-top", p)
		}
		for _, p := range r.Res.UnknownPaths {
			add(r.Endpoint, "unknown-path", p)
		}
		for _, m := range r.Res.Mismatches {
			// Type names only, never values - same rule as the report.
			add(r.Endpoint, "mismatch", fmt.Sprintf("%s:%s->%s", m.Path, m.Expected, m.Got))
		}
	}

	// Sort so the sweep order cannot change the digest. Two runs that found the
	// same things must agree however the prober happened to order them.
	sort.Strings(lines)

	h := sha256.New()
	// Length-delimit the generation so it cannot collide with a finding line.
	fmt.Fprintf(h, "%d:%s\n", len(generation), generation)
	for _, l := range lines {
		fmt.Fprintf(h, "%d:%s\n", len(l), l)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// fingerprintMarkerPrefix is what the workflow greps an existing comment for to
// find the last FULL REPORT. A heartbeat comment carries no marker, so it can
// never become the baseline a later run compares against.
const fingerprintMarkerPrefix = "<!-- canary-fingerprint: "

// fingerprintMarker renders the hidden marker appended to a report.
func fingerprintMarker(fingerprint string) string {
	return fingerprintMarkerPrefix + fingerprint + " -->"
}

// parseFingerprintMarker pulls the hash back out of a comment body, returning
// "" when the body carries no marker - a heartbeat, or a human's reply.
func parseFingerprintMarker(body string) string {
	i := strings.LastIndex(body, fingerprintMarkerPrefix)
	if i < 0 {
		return ""
	}
	rest := body[i+len(fingerprintMarkerPrefix):]
	end := strings.Index(rest, " -->")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

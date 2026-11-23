package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

// knownBlockedPath is the committed known-blocked ledger, resolved from the repo
// root exactly like coverageLedgerPath. Overridden in tests.
var knownBlockedPath = "opnsense/testdata/schemas/known-blocked.json"

// loadKnownBlocked reads the ledger for this run. A missing file is an empty
// ledger; an unusable one (malformed JSON, a key the format does not define) is
// reported on stderr and ALSO treated as empty, so a typo makes every ledgered
// item warn again instead of silently keeping it quiet. The committed file is
// held to the strict format by a unit test in the opnsense package.
func loadKnownBlocked() opnsense.KnownBlockedLedger {
	ledger, err := opnsense.LoadKnownBlockedLedger(knownBlockedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "apidrift: known-blocked ledger %s unusable, nothing is treated as known-blocked: %v\n", knownBlockedPath, err)
		return opnsense.KnownBlockedLedger{}
	}
	return ledger
}

// blockedFinding is one ledgered item this run confirmed: the path was still
// unverified, or the endpoint was still skipped. Skipped items have no Path.
type blockedFinding struct {
	Endpoint string
	Path     string
	Skipped  bool
	// Verified is set on an intermittent entry whose path resolved on this
	// run: the state it records is currently absent.
	Verified bool
	opnsense.KnownBlockedReason
}

// staleFinding is a ledger entry this run shows to be untrue. It is
// warning-level: a ledger that quietens the run must not outlive its facts.
type staleFinding struct {
	Endpoint string
	// Path is empty for a skipped-endpoint entry.
	Path string
	Why  string
}

// runReview is everything the run says about unverified and skipped coverage
// once the known-blocked ledger has been applied. aggregate, renderReport and
// the findings fingerprint all read it, so they cannot disagree.
type runReview struct {
	// Coverage is the coverage review with known-blocked paths already removed
	// from RequiredUnresolved, so len(Coverage.RequiredUnresolved) > 0 is
	// exactly "a required path warns".
	Coverage coverageReview
	// Blocked lists the ledgered items this run confirmed. Informational.
	Blocked []blockedFinding
	// Stale lists ledger entries the run contradicts. Warning-level.
	Stale []staleFinding
}

// blockedSkip reports whether endpoint's skip is ledgered and confirmed.
func (rr runReview) blockedSkip(endpoint string) bool {
	for _, b := range rr.Blocked {
		if b.Skipped && b.Endpoint == endpoint {
			return true
		}
	}
	return false
}

// ipsecTunnelDown is the one run-wide answer to "is the IPsec tunnel down?".
// All three endpoints must independently agree: phase 1 serves at least one row
// and every row is a configured connection with nothing live, the SAD has no row
// but the exact synthetic placeholder, and the SPD has no rows. An endpoint that
// contradicts this, was not probed, or has unknown facts means no. A tunnel that
// changes state during the sweep therefore warns, which is accepted.
func ipsecTunnelDown(byEndpoint map[string]probeResult) (bool, string) {
	for _, check := range []struct {
		endpoint string
		down     func(opnsense.PayloadFacts) bool
	}{
		{"ipsecPhase1", func(f opnsense.PayloadFacts) bool { return f.Rows > 0 && f.Phase1DownRows == f.Rows }},
		{"ipsecSad", func(f opnsense.PayloadFacts) bool { return f.SADPlaceholderRows == f.Rows }},
		{"ipsecSpd", func(f opnsense.PayloadFacts) bool { return f.Rows == 0 }},
	} {
		// Facts exist only after a completed validation, so an endpoint that was
		// not probed, errored, answered 404 or was skipped has RowsKnown false.
		facts := byEndpoint[check.endpoint].Res.Facts
		if !facts.RowsKnown || !check.down(facts) {
			return false, "the entry requires phase 1, the SAD and the SPD all to show the IPsec tunnel down, and " + check.endpoint + " did not on this run"
		}
	}
	return true, ""
}

// requiredStateHeld evaluates an entry's required box state. A state it cannot
// establish does not hold. The explanation names the state only, never a
// response value.
func requiredStateHeld(requires string, byEndpoint map[string]probeResult, ipsecDown bool, ipsecWhy string) (bool, string) {
	if requires == opnsense.RequiresIPsecDown {
		return ipsecDown, ipsecWhy
	}
	if name, ok := strings.CutPrefix(requires, opnsense.RequiresAbsentPrefix); ok && name != "" {
		if src, probed := byEndpoint[name]; probed && src.Absent {
			return true, ""
		}
		return false, "the entry requires parameter source " + name + " to answer 404, and this run did not show that"
	}
	return false, "the entry names no state this run can check"
}

// reviewedFingerprint is the digest a run publishes: its findings after the
// ledger is applied, including every stale entry.
func reviewedFingerprint(generation string, results []probeResult) string {
	run := analyzeRun(results)
	return findingsFingerprintWithReview(generation, results, run.Coverage.RequiredUnresolved, run.Stale)
}

// analyzeRun applies the known-blocked ledger for canaryProfile to the run.
//
// An entry is confirmed only by positive evidence in the run: the exact path is
// in the endpoint's Unverified list, still classifies as an ordinary required
// coverage path, AND the box state the entry requires held; or the endpoint was
// actually skipped and its required state held. Unverified alone is never
// enough. (An intermittent path entry is also confirmed when the path really
// resolved, flagged Verified.) Anything else is stale, with one exception that
// already warns on its own and so needs no second finding: a probe error on the
// endpoint. A run that names no profile applies no entries at all.
func analyzeRun(results []probeResult) runReview {
	ix := loadCoverageIndex()
	rev := reviewCoverage(results, ix)
	out := runReview{Coverage: rev}

	byEndpoint := make(map[string]probeResult, len(results))
	for _, r := range results {
		if _, dup := byEndpoint[r.Endpoint]; !dup {
			byEndpoint[r.Endpoint] = r
		}
	}

	ipsecDown, ipsecWhy := ipsecTunnelDown(byEndpoint)
	blockedPath := map[[2]string]bool{}
	for _, it := range loadKnownBlocked().ForProfile(canaryProfile) {
		stale := func(why string) {
			out.Stale = append(out.Stale, staleFinding{Endpoint: it.Endpoint, Path: it.Path, Why: why})
		}
		confirm := func(verified bool) {
			out.Blocked = append(out.Blocked, blockedFinding{
				Endpoint: it.Endpoint, Path: it.Path, Skipped: it.Skipped, Verified: verified, KnownBlockedReason: it.KnownBlockedReason,
			})
		}
		r, probed := byEndpoint[it.Endpoint]
		switch {
		case !probed:
			stale("endpoint was not probed in this run, so the entry cannot be checked")
		case r.ProbeErr != "":
			// Already a warning in its own right; the entry is neither
			// confirmed nor contradicted.
		case it.Skipped:
			if !r.SkippedParam {
				stale("endpoint is no longer skipped (the run probed it), so the entry is untrue")
			} else if held, why := requiredStateHeld(it.Requires, byEndpoint, ipsecDown, ipsecWhy); held {
				confirm(false)
			} else {
				stale(why)
			}
		default:
			class, entry, ok := ix.Classify(it.Endpoint, it.Path)
			switch {
			case !ok || class != opnsense.CoverageRequired || entry.Opaque:
				stale("path is no longer a required coverage path in coverage.json, so the entry is untrue")
			case r.Absent:
				stale("endpoint is absent (404) on this box, so the path cannot be checked")
			case r.SkippedParam:
				stale("endpoint was skipped in this run, so the path cannot be checked")
			case slices.Contains(r.Res.Unverified, it.Path):
				if held, why := requiredStateHeld(it.Requires, byEndpoint, ipsecDown, ipsecWhy); held {
					confirm(false)
					blockedPath[[2]string{it.Endpoint, it.Path}] = true
				} else {
					stale(why)
				}
			case slices.Contains(r.Res.Missing, it.Path):
				stale("path is missing from the response on this run, so it is not verified and the entry does not explain it")
			case slices.ContainsFunc(r.Res.Mismatches, func(m opnsense.Mismatch) bool { return m.Path == it.Path }):
				stale("path has a type mismatch on this run, so it is not verified and the entry does not explain it")
			case !r.Res.Facts.RowsKnown || r.Res.Facts.Rows == 0:
				// Not listed as unverified, yet nothing was served to check it
				// against (a top-level [] or a body with no rows array).
				stale("response has no rows on this run, so nothing confirms the path")
			case it.Intermittent:
				// The recorded state is simply absent this run. Shown, not
				// stale, and nothing here feeds the fingerprint.
				confirm(true)
			default:
				stale("path is now verified on this run, so the entry is untrue")
			}
		}
	}

	if len(blockedPath) > 0 {
		kept := make([]coverageFinding, 0, len(rev.RequiredUnresolved))
		for _, f := range rev.RequiredUnresolved {
			if !blockedPath[[2]string{f.Endpoint, f.Path}] {
				kept = append(kept, f)
			}
		}
		out.Coverage.RequiredUnresolved = kept
	}
	return out
}

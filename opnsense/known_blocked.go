package opnsense

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// The known-blocked ledger: a reviewed record of what the lab cannot exercise.
//
// The live canary warns on two conditions that no run can ever clear when the
// box simply cannot produce the state: a required-coverage path that stays
// unverified (coverage.json, class required), and a parameterised endpoint with
// no live parameter to probe with. A warning that can never clear keeps the
// standing drift issue open forever and drowns the run that finds real drift.
//
// This ledger lets a maintainer say, per probe target, "this one is known, here
// is why, and here is the box state that would unblock it". A ledgered item is
// REPORTED in an informational section of every run - recorded, never hidden -
// and stops setting the warning state. It is not an exemption in the sense of
// exemptions.json: that file suppresses what the validator finds in a payload
// (a missing or unmodelled key), whereas nothing here is ever found in a
// payload - the path was unreachable or the endpoint was never probed - so
// there is nothing for the validator to suppress, and folding the two together
// would put skip and coverage state into a type the validator consumes.
//
// The ledger cannot rot. cmd/apidrift reports an entry as STALE, and warns, when
// the run shows the entry is no longer true: the state it requires did not hold,
// the path was verified or is missing, the path is no longer a required coverage
// path, the endpoint was probed normally instead of skipped, or the endpoint was
// not probed at all. Everything that is not
// ledgered, and every other finding on a ledgered endpoint (breaking drift, a
// missing path, an unexpected key, a probe error), keeps its existing severity.
//
// An intermittent entry is the one softening: see KnownBlockedReason.Intermittent.
//
// "Unverified" alone never confirms an entry, because a real row whose key was
// renamed can read as unverified too. Every entry names, in Requires, the box
// state its reason claims, and the run must show that state from structural
// facts (PayloadFacts) or the entry is stale and warns.
//
// An entry is scoped to named probe targets (KnownProbeProfiles). A run that
// names no profile applies none of them, so a local run is never quieter than
// CI. Paths are NORMALIZED schema paths, exactly as in coverage.json, so no
// hostname, peer identity or payload value can enter the file.

// KnownBlockedReason is one probe target's account of why an item is blocked.
type KnownBlockedReason struct {
	// Reason says what about the lab makes the item unobtainable on this target.
	Reason string `json:"reason"`
	// UnblockedBy names the box state that would clear the block, so the entry
	// can be pruned the moment someone builds it.
	UnblockedBy string `json:"unblockedBy"`
	// Intermittent marks a path whose box state flaps (the tunnel that exists
	// only while a client holds it up). A run that finds such a path VERIFIED
	// is then shown as verified rather than reported stale, so the state coming
	// and going does not reopen the drift issue. It excuses nothing else: a
	// path that is no longer required, an absent, skipped or unprobed endpoint,
	// and every other finding still warn. Valid on a path entry for a target
	// whose state really flaps; a permanently blocked target must not set it,
	// because there a verified path is a real change.
	Intermittent bool `json:"intermittent,omitempty"`
	// Requires names the box state that must be shown on this run for the entry
	// to be confirmed. Mandatory: RequiresIPsecDown on a path entry,
	// RequiresAbsentPrefix plus an endpoint name on a skipped entry.
	Requires string `json:"requires"`
}

// The closed set of states a ledger entry can require.
const (
	// RequiresIPsecDown holds when phase 1, the SAD and the SPD all independently
	// show the tunnel down on this run. It is one run-wide fact, evaluated once:
	// upstream can serve an empty SAD or SPD after a failed backend call, and a
	// down-looking phase 1 row while SAs exist, so no endpoint is trusted alone.
	RequiresIPsecDown = "ipsec-down"
	// RequiresAbsentPrefix, followed by an endpoint name, holds when that
	// endpoint answered 404 on this run.
	RequiresAbsentPrefix = "absent:"
)

// PayloadFacts is what the known-blocked check may know about a response:
// counts of rows by shape, never a value.
type PayloadFacts struct {
	// RowsKnown is true when the response has a top-level rows array.
	RowsKnown bool
	// Rows is its length, before any placeholder row is stripped.
	Rows int
	// SADPlaceholderRows counts rows that are exactly the synthetic placeholder.
	SADPlaceholderRows int
	// Phase1DownRows counts rows that are a configured connection with nothing
	// live: connected exactly false, a non-empty string ikeid, and install-time
	// present and null. Upstream forces connected false on a row with no ikeid
	// even when it holds an SA, and sets install-time once a child SA installs,
	// so a row failing any of these proves nothing about the tunnel being down.
	Phase1DownRows int
}

// payloadFactsFor derives the structural facts for one decoded response.
func payloadFactsFor(endpoint EndpointName, root any) PayloadFacts {
	obj, ok := root.(map[string]any)
	if !ok {
		return PayloadFacts{}
	}
	rows, ok := obj["rows"].([]any)
	if !ok {
		return PayloadFacts{}
	}
	facts := PayloadFacts{RowsKnown: true, Rows: len(rows)}
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		switch endpoint {
		case "ipsecSad":
			if isExactSADPlaceholder(row) {
				facts.SADPlaceholderRows++
			}
		case "ipsecPhase1":
			connected, isBool := row["connected"].(bool)
			ikeid, _ := row["ikeid"].(string)
			installTime, hasInstallTime := row["install-time"]
			if isBool && !connected && ikeid != "" && hasInstallTime && installTime == nil {
				facts.Phase1DownRows++
			}
		}
	}
	return facts
}

// isExactSADPlaceholder matches only the row upstream's list_sad.py builds from
// setkey's "No SAD entries." sentence: src "No", dst "SAD", nat "ntries", an id
// hash, and null everywhere else. It is deliberately much narrower than
// IsIPsecSADPlaceholderRow, which also drops a real row that lost its spi or
// satype, and so cannot serve as evidence that the SAD was empty.
func isExactSADPlaceholder(row map[string]any) bool {
	if row["src"] != "No" || row["dst"] != "SAD" || row["nat"] != "ntries" {
		return false
	}
	for k, v := range row {
		switch k {
		case "src", "dst", "nat":
		case "id":
			if _, ok := v.(string); !ok {
				return false
			}
		default:
			if v != nil {
				return false
			}
		}
	}
	return true
}

// validate holds every entry to the closed set of required states.
func (l KnownBlockedLedger) validate() error {
	for endpoint, e := range l {
		for profile, r := range e.Skipped {
			if name, ok := strings.CutPrefix(r.Requires, RequiresAbsentPrefix); !ok || name == "" {
				return fmt.Errorf("%s skipped %s: requires %q, want %s<endpoint>", endpoint, profile, r.Requires, RequiresAbsentPrefix)
			}
		}
		for _, p := range e.Paths {
			for profile, r := range p.Profiles {
				if r.Requires != RequiresIPsecDown {
					return fmt.Errorf("%s %s %s: requires %q is not a known path state", endpoint, p.Path, profile, r.Requires)
				}
			}
		}
	}
	return nil
}

// KnownBlockedPath is one unverified required-coverage path the lab cannot
// reach, keyed by the probe targets it applies to.
type KnownBlockedPath struct {
	Path     string                        `json:"path"`
	Profiles map[string]KnownBlockedReason `json:"profiles"`
}

// KnownBlockedEndpoint is one endpoint's ledger section. Skipped covers the
// parameterised endpoint the prober cannot resolve a live parameter for.
type KnownBlockedEndpoint struct {
	Paths   []KnownBlockedPath            `json:"paths,omitempty"`
	Skipped map[string]KnownBlockedReason `json:"skipped,omitempty"`
}

// KnownBlockedLedger is the committed
// opnsense/testdata/schemas/known-blocked.json, keyed by endpoint name.
type KnownBlockedLedger map[string]KnownBlockedEndpoint

// LoadKnownBlockedLedger reads the committed ledger. A missing file is an empty
// ledger (nothing is quietened). Malformed JSON, or a key the format does not
// define, IS an error: a typo in a hand-edited ledger must fail loudly, and the
// caller treats an unusable ledger as empty so that everything warns again.
func LoadKnownBlockedLedger(path string) (KnownBlockedLedger, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return KnownBlockedLedger{}, nil
		}
		return nil, err
	}
	out := KnownBlockedLedger{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// One JSON value and nothing after it: trailing data means a damaged file.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: trailing data after the ledger", path)
	}
	if err := out.validate(); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

// KnownBlockedItem is one ledger entry resolved for a probe target.
type KnownBlockedItem struct {
	Endpoint string
	// Path is empty for a skipped-endpoint entry.
	Path string
	// Skipped is true for a skipped-endpoint entry.
	Skipped bool
	KnownBlockedReason
}

// ForProfile lists the entries that apply to one probe target, sorted by
// endpoint, then skipped-before-paths, then path. An empty or unknown profile
// resolves to nothing.
func (l KnownBlockedLedger) ForProfile(profile string) []KnownBlockedItem {
	if profile == "" {
		return nil
	}
	var out []KnownBlockedItem
	for endpoint, e := range l {
		if r, ok := e.Skipped[profile]; ok {
			out = append(out, KnownBlockedItem{Endpoint: endpoint, Skipped: true, KnownBlockedReason: r})
		}
		for _, p := range e.Paths {
			if r, ok := p.Profiles[profile]; ok {
				out = append(out, KnownBlockedItem{Endpoint: endpoint, Path: p.Path, KnownBlockedReason: r})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Endpoint != out[j].Endpoint {
			return out[i].Endpoint < out[j].Endpoint
		}
		if out[i].Skipped != out[j].Skipped {
			return out[i].Skipped
		}
		return out[i].Path < out[j].Path
	})
	return out
}

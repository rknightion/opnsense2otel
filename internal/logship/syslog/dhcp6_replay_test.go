package syslog

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"testing"
	"time"
)

// The #1202 replay, run against the rule as committed. It feeds a day of captured-shape
// kea-dhcp6 lines through the parser and the deriver, turns the counter increments into
// the evaluation the alert performs, and measures how long that evaluation stays
// breached. A breach shorter than the rule's pending period never reaches Alerting;
// what #1202 is about is that it also should not keep OPNsenseDHCP6AllocationFailures
// in Pending for the whole day.

const allocFailRuleManifest = "../../../grafana/alerts/grafana-managed/opnsense-dhcp6-alloc-failures.json"

// allocFailRule is the part of the committed rule the replay evaluates: the window of
// its rate() and its pending period, read from the manifest so the test cannot drift
// from the rule it claims to exercise.
func allocFailRule(t *testing.T) (window, pending time.Duration) {
	t.Helper()
	raw, err := os.ReadFile(allocFailRuleManifest)
	if err != nil {
		t.Fatalf("read rule manifest: %v", err)
	}
	var doc struct {
		Spec struct {
			For         string `json:"for"`
			Expressions struct {
				A struct {
					Model struct {
						Expr string `json:"expr"`
					} `json:"model"`
				} `json:"A"`
			} `json:"expressions"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse rule manifest: %v", err)
	}
	m := regexp.MustCompile(`rate\([a-z0-9_]+\[(\d+[smh])\]\)`).FindStringSubmatch(doc.Spec.Expressions.A.Model.Expr)
	if m == nil {
		t.Fatalf("no rate window in %q", doc.Spec.Expressions.A.Model.Expr)
	}
	if window, err = time.ParseDuration(m[1]); err != nil {
		t.Fatal(err)
	}
	if pending, err = time.ParseDuration(doc.Spec.For); err != nil {
		t.Fatal(err)
	}
	return window, pending
}

type replayEvent struct {
	at   time.Time
	line string
}

// breachRuns evaluates `rate(counter[window]) > 0` once a minute over [start, end] given
// the instants at which the counter incremented, and returns the longest run of
// consecutive breached evaluations and the total number of breached ones. After the
// first increment the counter is non-decreasing, so the rate is positive exactly when
// an increment fell inside the window.
func breachRuns(increments []time.Time, start, end time.Time, window time.Duration) (longest, total time.Duration) {
	run := time.Duration(0)
	for at := start; !at.After(end); at = at.Add(time.Minute) {
		breached := false
		for _, inc := range increments {
			if inc.After(at.Add(-window)) && !inc.After(at) {
				breached = true
				break
			}
		}
		if breached {
			run += time.Minute
			total += time.Minute
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest, total
}

// replayCounts feeds events in time order through one deriver and returns when the
// alloc-fail counter incremented. tracker nil replays the pre-#1202 behaviour (count
// every cause line).
func replayCounts(t *testing.T, tr *dhcp6Tracker, events []replayEvent) []time.Time {
	t.Helper()
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.Before(events[j].at) })
	sink := &fakeSink{}
	var out []time.Time
	for _, e := range events {
		before := len(callsOf(sink, "dhcp6_alloc_fail"))
		feedDHCP6(t, sink, tr, e.at, e.line)
		if len(callsOf(sink, "dhcp6_alloc_fail")) > before {
			out = append(out, e.at)
		}
	}
	return out
}

// retransmitOffsets is the cadence one stuck transaction showed: the gap between
// retransmissions doubles from one second (RFC 8415 backoff) until it reaches half an
// hour, then it is roughly hourly for 16 hours.
func retransmitOffsets() []time.Duration {
	offs := []time.Duration{0}
	for gap := time.Second; gap < 30*time.Minute; gap *= 2 {
		offs = append(offs, offs[len(offs)-1]+gap)
	}
	for range 16 {
		offs = append(offs, offs[len(offs)-1]+time.Hour)
	}
	return offs
}

// AC5. Five Thread border routers each send an IA_PD-only SOLICIT that is refused,
// and retransmit the same tid hourly; one of them then restarts and starts a new
// transaction. The day-long refusal of those is not a lease outage.
func TestDHCP6AllocFailReplayBorderRoutersDoNotHoldThePendingState(t *testing.T) {
	window, pending := allocFailRule(t)
	start := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	end := start.Add(18 * time.Hour)

	var events []replayEvent
	routers := make([]string, 5)
	for i := range routers {
		routers[i] = fmt.Sprintf("00:01:00:01:ee:ee:ee:ee:03:00:00:00:00:%02x", i+1)
		// Staggered so each router's rate window is its own, not merged with a sibling's.
		first := start.Add(time.Duration(i*11) * time.Minute)
		tid := fmt.Sprintf("0x%x", 0x2edf40+i)
		for _, off := range retransmitOffsets() {
			events = append(events, replayEvent{first.Add(off), keaNoPools(routers[i], tid)})
		}
	}
	// One restart: router 0 comes back with a new tid six hours in and walks the whole
	// backoff ladder again.
	restart := start.Add(6 * time.Hour)
	for _, off := range retransmitOffsets() {
		events = append(events, replayEvent{restart.Add(off), keaNoPools(routers[0], "0x7c0de1")})
	}

	counted := replayCounts(t, newDHCP6Tracker(), append([]replayEvent(nil), events...))
	if len(counted) != 6 {
		t.Fatalf("counted %d failures, want 6 (five transactions and one restart): %v", len(counted), counted)
	}
	longest, total := breachRuns(counted, start, end, window)
	if longest >= pending {
		t.Fatalf("the replay holds the rule breached for %v, at or over its %v pending period", longest, pending)
	}
	if longest > window+time.Minute {
		t.Fatalf("longest breach = %v, want at most one window (%v): each failed transaction should light the window once", longest, window)
	}
	if want := 6 * window; total > want {
		t.Fatalf("total breached time = %v, want at most %v (six transactions, one window each)", total, want)
	}
	// Steady state: once every transaction has been counted, no evaluation breaches at all.
	steady := restart.Add(time.Hour)
	if l, _ := breachRuns(counted, steady, end, window); l != 0 {
		t.Fatalf("still breached for %v after every transaction was counted", l)
	}

	// The old behaviour, for contrast: counting every cause line keeps the rule breached
	// all day. (Guards against this test passing because the replay was empty.)
	old := replayCounts(t, nil, events)
	if len(old) <= 6*10 {
		t.Fatalf("the stateless replay counted only %d lines; the replay does not exercise retransmissions", len(old))
	}
	if l, tot := breachRuns(old, steady, end, window); l == 0 || tot < 3*time.Hour {
		t.Fatalf("stateless replay breached for only %v after the restart; the contrast case is not holding the rule", tot)
	}
}

// AC5, the other half: a continuous refusal of DISTINCT transactions (a full pool, a
// missing address pool: every client fails, each with its own tid) still holds the rule
// breached for longer than its pending period, so it still fires.
func TestDHCP6AllocFailReplayDistinctTransactionsStillFire(t *testing.T) {
	window, pending := allocFailRule(t)
	start := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	var events []replayEvent
	for i := 0; i < int(pending/time.Minute)+10; i++ {
		// A different client with a different tid every two minutes.
		duid := fmt.Sprintf("00:01:00:01:ff:ff:ff:ff:04:00:00:00:%02x:%02x", i>>8, i&0xff)
		events = append(events, replayEvent{start.Add(time.Duration(i) * 2 * time.Minute), keaNoPools(duid, fmt.Sprintf("0x%x", 0x100000+i))})
	}
	counted := replayCounts(t, newDHCP6Tracker(), events)
	if len(counted) != len(events) {
		t.Fatalf("counted %d of %d distinct failed transactions", len(counted), len(events))
	}
	longest, _ := breachRuns(counted, start, start.Add(time.Duration(len(events))*2*time.Minute), window)
	if longest <= pending {
		t.Fatalf("continuous refusal of distinct transactions breached for only %v, want more than the %v pending period", longest, pending)
	}
}

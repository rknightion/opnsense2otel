package syslog

import (
	"fmt"
	"strconv"
	"testing"
	"time"
)

// freshDerivation is the stateless derivation context for a test that feeds
// observeDerived one line and cares about nothing else: a new, empty DHCPv6 memory and
// a zero record time (so the tracker's clock is used). A test that needs memory ACROSS
// lines builds one tracker and one derivation of its own.
func freshDerivation() derivation { return derivation{dhcp6: newDHCP6Tracker()} }

// Synthetic client identities for the #1202 fixtures. The documentation prefix
// 2001:db8::/64 and made-up DUIDs only: this repository is public.
const (
	stuckDUIDA = "00:01:00:01:aa:aa:aa:aa:02:00:00:00:00:01"
	stuckDUIDB = "00:01:00:01:aa:aa:aa:aa:02:00:00:00:00:02"
)

var dhcp6T0 = time.Date(2026, 8, 14, 2, 23, 46, 0, time.UTC)

// keaV6Line builds one captured-shape Kea v6 line for a (DUID, tid).
func keaV6Line(logger, id, duid, tid, tail string) string {
	return "INFO  [kea-dhcp6." + logger + ".0x4b6ab5a79810] " + id + " duid=[" + duid + "], [no hwaddr info], tid=" + tid + ": " + tail
}

func keaNoPools(duid, tid string) string {
	return "WARN  [kea-dhcp6.alloc-engine.0x4b6ab5a79810] ALLOC_ENGINE_V6_ALLOC_FAIL_NO_POOLS duid=[" + duid + "], [no hwaddr info], tid=" + tid + ": no pools were available for the lease allocation"
}

func keaAdvert(duid, tid string) string {
	return keaV6Line("leases", "DHCP6_LEASE_ADVERT", duid, tid, "lease for address 2001:db8::116d and iaid=0 will be advertised")
}

func keaAlloc(duid, tid string) string {
	return keaV6Line("leases", "DHCP6_LEASE_ALLOC", duid, tid, "lease for address 2001:db8::11ba and iaid=0 has been allocated for 1800 seconds")
}

func keaRenew(duid, tid string) string {
	return keaV6Line("leases", "DHCP6_LEASE_RENEW", duid, tid, "lease for address 2001:db8::116d and iaid=0 has been allocated for 1800 seconds")
}

// feedDHCP6 parses one kea-dhcp6 line and derives it at `at`, through the given tracker.
func feedDHCP6(t *testing.T, sink *fakeSink, tr *dhcp6Tracker, at time.Time, line string) {
	t.Helper()
	rec, ok := parseDHCP(keaDHCP6Env(t, line), nil, nil)
	if !ok {
		t.Fatalf("parseDHCP(%q) ok = false", line)
	}
	observeDerived(sink, "kea-dhcp6", rec.Attributes, derivation{dhcp6: tr, at: at})
}

func callsOf(sink *fakeSink, method string) []fakeCall {
	var out []fakeCall
	for _, c := range sink.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// AC2: the three v6 lease lines carry dhcp.duid and dhcp.tid like the packet and
// alloc-fail lines already do, and a v4 lease line is left as it was.
func TestKeaDHCP6LeaseLinesCarryDUIDAndTID(t *testing.T) {
	tests := []struct {
		name, line, wantAction, wantIP string
	}{
		{"ADVERT", keaAdvert(stuckDUIDA, "0x561478"), "advertise", "2001:db8::116d"},
		{"ALLOC", keaAlloc(stuckDUIDA, "0x453c5"), "alloc", "2001:db8::11ba"},
		{"RENEW", keaRenew(stuckDUIDA, "0xc95860"), "request", "2001:db8::116d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, ok := parseDHCP(keaDHCP6Env(t, tt.line), nil, nil)
			if !ok {
				t.Fatal("parseDHCP() ok = false")
			}
			if got := rec.Attributes["dhcp.duid"]; got != stuckDUIDA {
				t.Errorf("dhcp.duid = %q, want %q", got, stuckDUIDA)
			}
			wantTID := map[string]string{"ADVERT": "0x561478", "ALLOC": "0x453c5", "RENEW": "0xc95860"}[tt.name]
			if got := rec.Attributes["dhcp.tid"]; got != wantTID {
				t.Errorf("dhcp.tid = %q, want %q", got, wantTID)
			}
			if got := rec.Attributes["dhcp.action"]; got != tt.wantAction {
				t.Errorf("dhcp.action = %q, want %q (the existing dhcp event must not change)", got, tt.wantAction)
			}
			if got := rec.Attributes["dhcp.ip"]; got != tt.wantIP {
				t.Errorf("dhcp.ip = %q, want %q", got, tt.wantIP)
			}
		})
	}

	t.Run("a v4 lease line gains no tid", func(t *testing.T) {
		env, err := ParseEnvelope([]byte("<12>1 2026-07-30T09:00:00+01:00 test-firewall kea-dhcp4 63610 - - INFO  [kea-dhcp4.leases.0x1] DHCP4_LEASE_ALLOC [hwtype=1 bc:24:11:a5:a6:34], cid=[no info], tid=0x8a1b2c3d: lease 172.16.9.100 has been allocated for 4000 seconds"), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		rec, ok := parseDHCP(env, nil, nil)
		if !ok {
			t.Fatal("parseDHCP() ok = false")
		}
		if got, has := rec.Attributes["dhcp.tid"]; has {
			t.Errorf("a DHCPv4 lease record gained dhcp.tid = %q", got)
		}
	})
}

// AC1: one SOLICIT tid refused again and again, at the observed retransmit cadence,
// is one failed transaction. Two tids from one DUID are two.
func TestDHCP6AllocFailCountsTransactionsNotRetransmissions(t *testing.T) {
	// The cadence a real stuck tid showed, in seconds after the first line, then roughly
	// hourly for 16 hours.
	offsets := []int{0, 1, 3, 7, 14, 29, 59, 115, 227, 450, 900}
	for h := 2; h <= 16; h++ {
		offsets = append(offsets, 900+(h-1)*3600)
	}

	sink := &fakeSink{}
	tr := newDHCP6Tracker()
	for _, off := range offsets {
		feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Duration(off)*time.Second), keaNoPools(stuckDUIDA, "0x2edf4e"))
	}
	assertCalls(t, callsOf(sink, "dhcp6_alloc_fail"), []fakeCall{{"dhcp6_alloc_fail", []string{keaAllocFailReasonNoPools}}})

	// A second transaction from the same DUID (a restart) is a second failure, itself
	// counted once however often it retransmits.
	for i := 0; i < 5; i++ {
		feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Duration(20000+i*60)*time.Second), keaNoPools(stuckDUIDA, "0x9a1b2c"))
	}
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != 2 {
		t.Fatalf("two tids from one DUID counted %d times, want 2", got)
	}

	// The same tid from a DIFFERENT client is a different transaction.
	feedDHCP6(t, sink, tr, dhcp6T0.Add(21000*time.Second), keaNoPools(stuckDUIDB, "0x2edf4e"))
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != 3 {
		t.Fatalf("the same tid from another DUID counted %d times in total, want 3", got)
	}
}

// The reason is part of the identity: a transaction refused for one reason and later
// for another is two failures of two kinds.
func TestDHCP6AllocFailReasonIsPartOfTheKey(t *testing.T) {
	sink := &fakeSink{}
	tr := newDHCP6Tracker()
	feedDHCP6(t, sink, tr, dhcp6T0, keaNoPools(stuckDUIDA, "0x1"))
	feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Second), "WARN  [kea-dhcp6.alloc-engine.0x4b6ab5a79810] ALLOC_ENGINE_V6_ALLOC_FAIL duid=["+stuckDUIDA+"], [no hwaddr info], tid=0x1: failed to allocate an IPv6 lease after 3 attempt(s)")
	assertCalls(t, callsOf(sink, "dhcp6_alloc_fail"), []fakeCall{
		{"dhcp6_alloc_fail", []string{keaAllocFailReasonNoPools}},
		{"dhcp6_alloc_fail", []string{keaAllocFailReasonExhausted}},
	})
}

// A remembered transaction expires 2h after its LAST sighting, and every sighting
// refreshes it, so a client retransmitting hourly stays one failure while one that went
// quiet and came back is a new one.
func TestDHCP6AllocFailMemoryExpiresTwoHoursAfterLastSighting(t *testing.T) {
	sink := &fakeSink{}
	tr := newDHCP6Tracker()
	line := keaNoPools(stuckDUIDA, "0x2edf4e")

	feedDHCP6(t, sink, tr, dhcp6T0, line)
	// Hourly sightings across five hours: each lands inside 2h of the previous one, so
	// the entry never lapses even though it is far older than 2h in total.
	for h := 1; h <= 5; h++ {
		feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Duration(h)*time.Hour), line)
	}
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != 1 {
		t.Fatalf("hourly retransmissions over 5h counted %d times, want 1", got)
	}

	// Silent for exactly 2h, then the same tid again: forgotten, so a new failure.
	feedDHCP6(t, sink, tr, dhcp6T0.Add(7*time.Hour), line)
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != 2 {
		t.Fatalf("a sighting 2h after the last counted %d times in total, want 2", got)
	}
}

// AC1: at the 4096-transaction cap, eviction keeps the memory bounded, evicts the
// least recently SEEN transaction rather than the oldest, and a new transaction is still
// counted.
func TestDHCP6AllocFailMemoryIsBoundedAndEvictsLeastRecentlySeen(t *testing.T) {
	sink := &fakeSink{}
	tr := newDHCP6Tracker()
	at := func(i int) time.Time { return dhcp6T0.Add(time.Duration(i) * time.Millisecond) }
	duid := func(i int) string { return fmt.Sprintf("00:01:00:01:bb:bb:bb:bb:%02x:%02x:00:00:00:01", i>>8, i&0xff) }

	// Fill to the cap.
	for i := 0; i < dhcp6TxMax; i++ {
		feedDHCP6(t, sink, tr, at(i), keaNoPools(duid(i), "0x1"))
	}
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != dhcp6TxMax {
		t.Fatalf("filling the memory counted %d, want %d", got, dhcp6TxMax)
	}
	// Refresh the very first transaction so it is the most recently seen, not the oldest.
	feedDHCP6(t, sink, tr, at(dhcp6TxMax), keaNoPools(duid(0), "0x1"))
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != dhcp6TxMax {
		t.Fatalf("a retransmission at the cap was counted (%d calls)", got)
	}

	// One new transaction at the cap: still counted, and memory does not grow.
	feedDHCP6(t, sink, tr, at(dhcp6TxMax+1), keaNoPools(duid(dhcp6TxMax), "0x1"))
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != dhcp6TxMax+1 {
		t.Fatalf("a new transaction at the cap counted %d times in total, want %d", got, dhcp6TxMax+1)
	}
	if got := tr.failed.len(); got != dhcp6TxMax {
		t.Fatalf("memory holds %d transactions, want the cap %d", got, dhcp6TxMax)
	}

	// The one evicted is the least recently seen (duid(1), since duid(0) was refreshed):
	// it counts again. duid(0) does not.
	feedDHCP6(t, sink, tr, at(dhcp6TxMax+2), keaNoPools(duid(0), "0x1"))
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != dhcp6TxMax+1 {
		t.Fatalf("the refreshed transaction was evicted (%d calls)", got)
	}
	feedDHCP6(t, sink, tr, at(dhcp6TxMax+3), keaNoPools(duid(1), "0x1"))
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != dhcp6TxMax+2 {
		t.Fatalf("the least recently seen transaction was not evicted (%d calls)", got)
	}
	if got := tr.failed.len(); got != dhcp6TxMax {
		t.Fatalf("memory holds %d transactions after eviction, want %d", got, dhcp6TxMax)
	}
}

// A cause line with no DUID or tid cannot be matched to a transaction, so it counts
// exactly as before rather than disappearing.
func TestDHCP6AllocFailWithoutIdentityStillCounts(t *testing.T) {
	sink := &fakeSink{}
	tr := newDHCP6Tracker()
	attrs := map[string]string{"dhcp.alloc_fail_reason": keaAllocFailReasonNoPools}
	for i := 0; i < 3; i++ {
		observeDerived(sink, "kea-dhcp6", attrs, derivation{dhcp6: tr, at: dhcp6T0})
	}
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != 3 {
		t.Fatalf("identity-less cause lines counted %d times, want 3", got)
	}
}

// The DUID and tid still never reach the sink, whichever call carries them.
func TestDHCP6SolicitStuckIdentifiersNeverReachCounterLabels(t *testing.T) {
	sink := &fakeSink{}
	tr := newDHCP6Tracker()
	feedDHCP6(t, sink, tr, dhcp6T0, keaNoPools(stuckDUIDA, "0x2edf4e"))
	for _, call := range callsOf(sink, "dhcp6_alloc_fail") {
		for _, arg := range call.args {
			if arg == stuckDUIDA || arg == "0x2edf4e" {
				t.Errorf("%q reached the alloc-fail counter", arg)
			}
		}
	}
}

// AC3 at the deriver: only a tid advertised AGAIN at least ten minutes after its first
// advertisement reaches the sink as stuck.
func TestDHCP6SolicitStuckDetection(t *testing.T) {
	stuckCalls := func(s *fakeSink) int { return len(callsOf(s, "dhcp6_solicit_stuck")) }

	t.Run("a normal exchange is never stuck", func(t *testing.T) {
		sink, tr := &fakeSink{}, newDHCP6Tracker()
		feedDHCP6(t, sink, tr, dhcp6T0, keaAdvert(stuckDUIDA, "0x561478"))
		feedDHCP6(t, sink, tr, dhcp6T0.Add(40*time.Millisecond), keaAlloc(stuckDUIDA, "0x453c5"))
		if stuckCalls(sink) != 0 {
			t.Fatalf("a SOLICIT/ADVERTISE/REQUEST exchange reported stuck: %+v", sink.calls)
		}
	})

	t.Run("quick retransmissions are never stuck", func(t *testing.T) {
		sink, tr := &fakeSink{}, newDHCP6Tracker()
		for _, off := range []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second, 14 * time.Second, 29 * time.Second, 59 * time.Second} {
			feedDHCP6(t, sink, tr, dhcp6T0.Add(off), keaAdvert(stuckDUIDA, "0x561478"))
		}
		if stuckCalls(sink) != 0 {
			t.Fatalf("sub-minute retransmissions reported stuck: %+v", sink.calls)
		}
	})

	t.Run("just under ten minutes is not stuck, ten minutes is", func(t *testing.T) {
		sink, tr := &fakeSink{}, newDHCP6Tracker()
		feedDHCP6(t, sink, tr, dhcp6T0, keaAdvert(stuckDUIDA, "0x561478"))
		feedDHCP6(t, sink, tr, dhcp6T0.Add(dhcp6StuckAfter-time.Second), keaAdvert(stuckDUIDA, "0x561478"))
		if stuckCalls(sink) != 0 {
			t.Fatal("9m59s reported stuck")
		}
		feedDHCP6(t, sink, tr, dhcp6T0.Add(dhcp6StuckAfter), keaAdvert(stuckDUIDA, "0x561478"))
		assertCalls(t, callsOf(sink, "dhcp6_solicit_stuck"), []fakeCall{{"dhcp6_solicit_stuck", []string{stuckDUIDA}}})
	})

	t.Run("the first advertisement is the clock start, not the last", func(t *testing.T) {
		// 6 minutes apart each: no pair of consecutive sightings is 10 minutes apart, but
		// the third is 12 minutes after the first.
		sink, tr := &fakeSink{}, newDHCP6Tracker()
		for i := 0; i < 3; i++ {
			feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Duration(i)*6*time.Minute), keaAdvert(stuckDUIDA, "0x561478"))
		}
		if got := stuckCalls(sink); got != 1 {
			t.Fatalf("stuck reported %d times, want 1 (the third sighting)", got)
		}
	})

	t.Run("different tids do not join up", func(t *testing.T) {
		sink, tr := &fakeSink{}, newDHCP6Tracker()
		feedDHCP6(t, sink, tr, dhcp6T0, keaAdvert(stuckDUIDA, "0x1"))
		feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Hour), keaAdvert(stuckDUIDA, "0x2"))
		if stuckCalls(sink) != 0 {
			t.Fatal("two different transactions were joined into a stuck client")
		}
	})

	t.Run("ALLOC and RENEW clear the DUID", func(t *testing.T) {
		for name, line := range map[string]string{"ALLOC": keaAlloc(stuckDUIDA, "0x453c5"), "RENEW": keaRenew(stuckDUIDA, "0xc95860")} {
			sink, tr := &fakeSink{}, newDHCP6Tracker()
			feedDHCP6(t, sink, tr, dhcp6T0, keaAdvert(stuckDUIDA, "0x561478"))
			feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Hour), keaAdvert(stuckDUIDA, "0x561478"))
			feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Hour+time.Second), line)
			assertCalls(t, append(callsOf(sink, "dhcp6_solicit_stuck"), callsOf(sink, "dhcp6_solicit_stuck_clear")...), []fakeCall{
				{"dhcp6_solicit_stuck", []string{stuckDUIDA}},
				{"dhcp6_solicit_stuck_clear", []string{stuckDUIDA}},
			})
			// The advertisement it had been repeating is forgotten with the clear.
			feedDHCP6(t, sink, tr, dhcp6T0.Add(2*time.Hour), keaAdvert(stuckDUIDA, "0x561478"))
			if got := stuckCalls(sink); got != 1 {
				t.Errorf("%s: an old tid re-stuck the client straight after it was served (%d reports)", name, got)
			}
		}
	})

	t.Run("a stuck sighting is reported on every repeat", func(t *testing.T) {
		// The store refreshes the DUID's expiry on each one; the hourly retransmit is what
		// keeps a stuck client in the count.
		sink, tr := &fakeSink{}, newDHCP6Tracker()
		for h := 0; h <= 4; h++ {
			feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Duration(h)*time.Hour), keaAdvert(stuckDUIDA, "0x561478"))
		}
		if got := stuckCalls(sink); got != 4 {
			t.Fatalf("stuck reported %d times over 4 hourly repeats, want 4", got)
		}
	})

	t.Run("the clock is used only for a record with no timestamp", func(t *testing.T) {
		sink, tr := &fakeSink{}, newDHCP6Tracker()
		now := dhcp6T0
		tr.now = func() time.Time { return now }
		feedDHCP6(t, sink, tr, time.Time{}, keaAdvert(stuckDUIDA, "0x561478"))
		now = now.Add(11 * time.Minute)
		feedDHCP6(t, sink, tr, time.Time{}, keaAdvert(stuckDUIDA, "0x561478"))
		if got := stuckCalls(sink); got != 1 {
			t.Fatalf("timestamp-less records reported stuck %d times, want 1", got)
		}
	})
}

// The advert memory is bounded and expires like the failure memory.
func TestDHCP6AdvertMemoryIsBounded(t *testing.T) {
	sink, tr := &fakeSink{}, newDHCP6Tracker()
	for i := 0; i < dhcp6TxMax+50; i++ {
		duid := "00:01:00:01:cc:cc:cc:cc:" + strconv.FormatInt(int64(0x10000+i), 16)[1:3] + ":" + strconv.FormatInt(int64(0x10000+i), 16)[3:5] + ":00:00:00:01"
		feedDHCP6(t, sink, tr, dhcp6T0.Add(time.Duration(i)*time.Millisecond), keaAdvert(duid, "0x1"))
	}
	if got := tr.adverts.len(); got != dhcp6TxMax {
		t.Fatalf("advert memory holds %d entries, want the cap %d", got, dhcp6TxMax)
	}
}

// A nil tracker (a hand-built source) keeps no memory: every cause line counts and
// nothing is ever stuck, and nothing panics.
func TestDHCP6NilTrackerKeepsNoMemory(t *testing.T) {
	sink := &fakeSink{}
	rec, ok := parseDHCP(keaDHCP6Env(t, keaNoPools(stuckDUIDA, "0x1")), nil, nil)
	if !ok {
		t.Fatal("parseDHCP() ok = false")
	}
	for i := 0; i < 2; i++ {
		observeDerived(sink, "kea-dhcp6", rec.Attributes, derivation{})
	}
	if got := len(callsOf(sink, "dhcp6_alloc_fail")); got != 2 {
		t.Fatalf("nil tracker counted %d times, want 2", got)
	}
}

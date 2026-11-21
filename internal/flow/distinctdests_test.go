package flow

import (
	"fmt"
	"net/netip"
	"testing"
	"time"
)

// destRec builds a record on interface iface (via In device) to a destination IP.
func destRec(iface, dst string) Record {
	return Record{In: Iface{Device: iface}, DstAddr: netip.MustParseAddr(dst)}
}

func TestDistinctDestsRotates(t *testing.T) {
	window := NewDistinctDests()
	startWindow := time.Unix(10000, 0)
	window.now = func() time.Time { return startWindow }
	window.Rotate(startWindow)
	for i := range 100 {
		window.Observe(destRec("lan", netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)}).String()))
	}
	if got := window.Snapshot()["lan"]; got != 100 {
		t.Errorf("100 destinations = %d, want 100", got)
	}
	window.Rotate(startWindow.Add(time.Hour))
	window.Rotate(startWindow.Add(2 * time.Hour))
	if got := window.Snapshot()["lan"]; got != 0 {
		t.Errorf("after two empty rotations = %d, want 0", got)
	}

	d := NewDistinctDests()
	start := time.Unix(10000, 0)
	now := start
	d.now = func() time.Time { return now }
	d.Observe(destRec("lan", "192.0.2.1"))
	d.Observe(destRec("lan", "192.0.2.4"))
	d.Rotate(start)
	now = start.Add(time.Hour)
	d.Rotate(now)
	d.Observe(destRec("lan", "192.0.2.1"))
	d.Observe(destRec("lan", "192.0.2.2"))
	if got := d.Snapshot()["lan"]; got != 3 {
		t.Fatalf("union = %d, want 3", got)
	}
	now = start.Add(2 * time.Hour)
	d.Rotate(now)
	if got := d.Snapshot()["lan"]; got != 2 {
		t.Fatalf("previous generation = %d, want 2", got)
	}
	now = start.Add(3 * time.Hour)
	d.Rotate(now)
	if got, ok := d.Snapshot()["lan"]; !ok || got != 0 {
		t.Fatalf("expired interface = %d, present %v; want zero and present", got, ok)
	}
	for i := range distinctDestsPerIfaceCap {
		d.Observe(destRec("lan", netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}).String()))
	}
	d.Observe(destRec("lan", "192.0.2.3"))
	if got := d.Snapshot()["lan"]; got != distinctDestsPerIfaceCap {
		t.Errorf("single window over cap = %d, want %d", got, distinctDestsPerIfaceCap)
	}
	now = start.Add(4 * time.Hour)
	d.Rotate(now)
	d.Observe(destRec("lan", "192.0.2.3"))
	if got := d.Snapshot()["lan"]; got != distinctDestsPerIfaceCap {
		t.Fatalf("union over cap = %d, want %d", got, distinctDestsPerIfaceCap)
	}
	d.Rotate(start.Add(6 * time.Hour))
	if got := d.Snapshot()["lan"]; got != 0 {
		t.Fatalf("two-hour jump = %d, want 0", got)
	}
}

func TestDistinctDestsDelayedPoll(t *testing.T) {
	start := time.Unix(10000, 0)
	for _, gap := range []time.Duration{time.Hour, 4 * time.Hour} {
		t.Run(gap.String(), func(t *testing.T) {
			d := NewDistinctDests()
			now := start
			d.now = func() time.Time { return now }
			d.Rotate(now)
			d.Observe(destRec("old", "192.0.2.1"))
			now = start.Add(gap + time.Minute)
			fresh := destRec("fresh", "192.0.2.2")
			fresh.Observed = start.Add(-100 * time.Hour)
			d.Observe(fresh)
			d.Rotate(now)
			d.Rotate(start.Add(gap + time.Hour))
			got := d.Snapshot()
			if got["fresh"] != 1 || got["old"] != 0 {
				t.Errorf("after delayed poll: %v, want fresh=1 old=0", got)
			}
		})
	}
}

func TestDistinctDestsCountsUnique(t *testing.T) {
	d := NewDistinctDests()
	d.Observe(destRec("lan", "1.1.1.1"))
	d.Observe(destRec("lan", "1.1.1.1")) // duplicate: no change
	d.Observe(destRec("lan", "2.2.2.2"))

	if got := d.Snapshot()["lan"]; got != 2 {
		t.Fatalf("distinct = %d, want 2", got)
	}
}

func TestDistinctDestsFoldsMappedV6(t *testing.T) {
	d := NewDistinctDests()
	// ::ffff:1.2.3.4 and 1.2.3.4 are the same host; Unmap must collapse them.
	d.Observe(destRec("lan", "1.2.3.4"))
	d.Observe(destRec("lan", "::ffff:1.2.3.4"))

	if got := d.Snapshot()["lan"]; got != 1 {
		t.Fatalf("mapped v6 duplicate not folded: distinct = %d, want 1", got)
	}
}

func TestDistinctDestsSeparatesInterfaces(t *testing.T) {
	d := NewDistinctDests()
	d.Observe(destRec("lan", "1.1.1.1"))
	d.Observe(destRec("wan", "1.1.1.1")) // same IP, different interface

	got := d.Snapshot()
	if got["lan"] != 1 || got["wan"] != 1 {
		t.Fatalf("per-interface counts wrong: %v", got)
	}
}

func TestDistinctDestsSkipsUnlabelled(t *testing.T) {
	d := NewDistinctDests()
	// No interface resolvable: nothing to attribute the destination to.
	d.Observe(Record{DstAddr: netip.MustParseAddr("1.1.1.1")})
	if len(d.Snapshot()) != 0 {
		t.Fatalf("record with no interface label must not be counted")
	}
}

func TestDistinctDestsCapStopsInserting(t *testing.T) {
	d := NewDistinctDests()
	// Fill exactly to the cap, then one past it.
	for i := range distinctDestsPerIfaceCap {
		d.Observe(destRec("lan", netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}).String()))
	}
	over := d.Snapshot()["lan"]
	if over != distinctDestsPerIfaceCap {
		t.Fatalf("at cap = %d, want %d", over, distinctDestsPerIfaceCap)
	}
	// A brand-new destination beyond the cap is refused; the count holds at the cap.
	d.Observe(destRec("lan", "203.0.113.99"))
	if got := d.Snapshot()["lan"]; got != distinctDestsPerIfaceCap {
		t.Fatalf("cap breached: %d, want %d", got, distinctDestsPerIfaceCap)
	}
}

// TestDistinctDestsBoundsInterfaceCardinality is #563: Zenarmor controls the
// interface/vlanid strings that key this map, and prior to the fix the outer map had
// no ceiling — one process-lifetime cell and one series per novel interface label an
// admitted sender chose to send. This drives well past the ceiling with distinct
// interface labels and asserts the live cell count never exceeds
// distinctDestsMaxInterfaces + 1 (the +1 is the single fixed OtherLabel overflow
// bucket, not another attacker-controlled key).
func TestDistinctDestsBoundsInterfaceCardinality(t *testing.T) {
	d := NewDistinctDests()

	const attackerInterfaces = distinctDestsMaxInterfaces * 2
	for i := range attackerInterfaces {
		iface := fmt.Sprintf("attacker-iface-%d", i)
		d.Observe(destRec(iface, "198.51.100.1"))
	}

	got := d.Snapshot()
	if len(got) > distinctDestsMaxInterfaces+1 {
		t.Fatalf("interface cell count = %d, want <= %d (budget + OtherLabel overflow)", len(got), distinctDestsMaxInterfaces+1)
	}
	if _, ok := got[OtherLabel]; !ok {
		t.Fatalf("overflow beyond the budget must fold into OtherLabel; got keys: %v", got)
	}

	stats := d.Stats()
	if stats.Capped == 0 {
		t.Fatalf("Stats().Capped = 0, want > 0 after driving past the interface budget")
	}
	if stats.MaxInterfaces != distinctDestsMaxInterfaces {
		t.Fatalf("Stats().MaxInterfaces = %d, want %d", stats.MaxInterfaces, distinctDestsMaxInterfaces)
	}
}

// TestDistinctDestsPreservesLegitimateCountsUnderAttack proves the fix does not
// break real counting to achieve the bound: while an attacker floods novel
// interface labels (all folding into OtherLabel), a legitimate, already-admitted
// interface's own destination count must keep advancing normally and independently.
func TestDistinctDestsPreservesLegitimateCountsUnderAttack(t *testing.T) {
	d := NewDistinctDests()

	// Admit real interfaces first, each with a couple of distinct destinations.
	d.Observe(destRec("lan", "10.0.0.1"))
	d.Observe(destRec("lan", "10.0.0.2"))
	d.Observe(destRec("wan", "1.1.1.1"))

	// Now flood past the interface budget with novel attacker-controlled labels.
	const attackerInterfaces = distinctDestsMaxInterfaces * 2
	for i := range attackerInterfaces {
		iface := fmt.Sprintf("flood-iface-%d", i)
		d.Observe(destRec(iface, "198.51.100.1"))
	}

	// More traffic on the legitimate, already-admitted interfaces must still count.
	d.Observe(destRec("lan", "10.0.0.3"))
	d.Observe(destRec("wan", "1.1.1.1")) // duplicate: must not double-count

	got := d.Snapshot()
	if got["lan"] != 3 {
		t.Fatalf("lan distinct = %d, want 3 (legitimate counting broken by the bound)", got["lan"])
	}
	if got["wan"] != 1 {
		t.Fatalf("wan distinct = %d, want 1 (legitimate counting broken by the bound)", got["wan"])
	}
	if len(got) > distinctDestsMaxInterfaces+1 {
		t.Fatalf("interface cell count = %d, want <= %d", len(got), distinctDestsMaxInterfaces+1)
	}
}

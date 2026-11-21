package syslog

import (
	"container/list"
	"sync"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/logship"
)

// DHCPv6 transaction memory (#1202).
//
// Kea logs one line per packet it handles, so a client that retransmits a SOLICIT
// under the SAME transaction id is logged again each time. RFC 8415 has a client
// whose SOLICIT was refused (or whose ADVERTISE never arrived) retransmit that tid
// with exponential backoff capped at SOL_MAX_RT (3600 s), indefinitely. Two
// different questions fall out of that, and both need a little memory the metric sink
// must not hold (it never sees a DUID or tid):
//
//   - how many transactions FAILED, as opposed to how many times Kea wrote the
//     failure down (failed below), and
//   - which clients were advertised an address and never came back to take it
//     (adverts below), the failure that leaves a client with no address while nothing
//     at all is refused.
//
// The memory is bounded two ways, like the inventories in internal/collector: a key
// budget with least-recently-seen eviction, and an expiry measured from the last
// sighting. Both are sized from SOL_MAX_RT: a live retransmission always arrives well
// inside two of them, so a remembered transaction that outlives that is one the client
// gave up on.
const (
	// dhcp6TxMax caps each transaction memory. A /64 LAN has at most a few hundred
	// DHCPv6 clients, each holding a handful of tids at a time, so this is an order of
	// magnitude of headroom, not a working-set estimate; it exists so a spoofed syslog
	// flood of random tids cannot grow the process without limit.
	dhcp6TxMax = 4096
	// dhcp6TxTTL is 2 x SOL_MAX_RT (RFC 8415 section 7.6, 3600 s). A sighting refreshes it.
	dhcp6TxTTL = 2 * time.Hour
	// dhcp6StuckAfter is how long after the FIRST advertisement of a tid the same tid
	// must be advertised again before the client counts as stuck. A normal
	// SOLICIT/ADVERTISE/REQUEST exchange is milliseconds, and the early retransmissions
	// of a client that merely lost one packet arrive within seconds, so ten minutes
	// separates "slow" from "never going to answer" with a wide margin either side. The
	// backoff ladder (1, 2, 4 ... 3600 s) passes ten minutes within the sequence's
	// first dozen retransmissions.
	dhcp6StuckAfter = 10 * time.Minute
)

// dhcp6TxKey identifies one remembered transaction. reason is empty in the adverts
// memory, which does not distinguish one.
type dhcp6TxKey struct{ duid, tid, reason string }

// dhcp6TxEntry is one remembered transaction.
type dhcp6TxEntry struct {
	key         dhcp6TxKey
	first, last time.Time
}

// dhcp6TxMemory is an LRU of transactions with a last-sighting expiry. Not safe for
// concurrent use on its own: dhcp6Tracker serialises it.
type dhcp6TxMemory struct {
	max   int
	ttl   time.Duration
	order *list.List // most recently seen at the front
	byKey map[dhcp6TxKey]*list.Element
}

func newDHCP6TxMemory(max int, ttl time.Duration) *dhcp6TxMemory {
	return &dhcp6TxMemory{max: max, ttl: ttl, order: list.New(), byKey: make(map[dhcp6TxKey]*list.Element, max)}
}

// touch records a sighting of key at `at` and returns its entry together with whether
// it was already remembered (and still live). A live entry is refreshed; an expired
// one is replaced by a fresh entry, so its first-sighting time restarts. A novel key at
// the cap evicts the least recently seen one, so a new transaction is always
// remembered.
func (m *dhcp6TxMemory) touch(key dhcp6TxKey, at time.Time) (e *dhcp6TxEntry, seen bool) {
	m.expire(at)
	if el, ok := m.byKey[key]; ok {
		e = el.Value.(*dhcp6TxEntry)
		if at.Sub(e.last) < m.ttl {
			if at.After(e.last) {
				e.last = at
			}
			m.order.MoveToFront(el)
			return e, true
		}
		// Expired but not yet swept (the sweep walks from the tail by recency, and
		// record timestamps need not be monotonic): start it afresh.
		e.first, e.last = at, at
		m.order.MoveToFront(el)
		return e, false
	}
	for m.max > 0 && m.order.Len() >= m.max {
		m.remove(m.order.Back())
	}
	e = &dhcp6TxEntry{key: key, first: at, last: at}
	m.byKey[key] = m.order.PushFront(e)
	return e, false
}

// expire sweeps entries not seen within the TTL of `at`, oldest first.
func (m *dhcp6TxMemory) expire(at time.Time) {
	for el := m.order.Back(); el != nil; el = m.order.Back() {
		if at.Sub(el.Value.(*dhcp6TxEntry).last) < m.ttl {
			return
		}
		m.remove(el)
	}
}

func (m *dhcp6TxMemory) remove(el *list.Element) {
	delete(m.byKey, el.Value.(*dhcp6TxEntry).key)
	m.order.Remove(el)
}

// dropDUID forgets every remembered transaction of one client.
func (m *dhcp6TxMemory) dropDUID(duid string) {
	for el := m.order.Front(); el != nil; {
		next := el.Next()
		if el.Value.(*dhcp6TxEntry).key.duid == duid {
			m.remove(el)
		}
		el = next
	}
}

func (m *dhcp6TxMemory) len() int { return m.order.Len() }

// dhcp6Tracker is the deriver's DHCPv6 transaction memory. One per receiver, shared by
// every listener goroutine; a nil tracker keeps no memory (every cause line counts, no
// client is ever reported stuck), which is what a hand-built test source gets.
type dhcp6Tracker struct {
	mu      sync.Mutex
	now     func() time.Time // clock for a record that carries no timestamp
	failed  *dhcp6TxMemory   // (DUID, tid, reason) of failures already counted
	adverts *dhcp6TxMemory   // (DUID, tid) of advertised addresses
}

func newDHCP6Tracker() *dhcp6Tracker {
	return &dhcp6Tracker{
		now:     time.Now,
		failed:  newDHCP6TxMemory(dhcp6TxMax, dhcp6TxTTL),
		adverts: newDHCP6TxMemory(dhcp6TxMax, dhcp6TxTTL),
	}
}

// when is the time to file a record under: its own timestamp, so a buffered or
// replayed line is judged by when the firewall wrote it, and only a record with none
// (an RFC 5424 "-") by the clock.
func (t *dhcp6Tracker) when(at time.Time) time.Time {
	if at.IsZero() {
		return t.now()
	}
	return at
}

// countFailure reports whether a cause line is the first sighting of its transaction
// and so should be counted. A retransmission of a refused SOLICIT carries the same tid,
// and Kea logs a fresh cause line for every one; counting each would report a client's
// whole retransmit history as that many failures. A line with no DUID or tid cannot be
// matched to a transaction, so it counts, as it always has.
func (t *dhcp6Tracker) countFailure(duid, tid, reason string, at time.Time) bool {
	if t == nil || duid == "" || tid == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, seen := t.failed.touch(dhcp6TxKey{duid, tid, reason}, t.when(at))
	return !seen
}

// advert records Kea advertising an address under (duid, tid) and reports whether the
// client is now stuck: this tid was first advertised at least dhcp6StuckAfter ago, so
// the client has been retransmitting the same SOLICIT since and never sent a REQUEST.
func (t *dhcp6Tracker) advert(duid, tid string, at time.Time) (stuck bool) {
	if t == nil || duid == "" || tid == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	at = t.when(at)
	e, _ := t.adverts.touch(dhcp6TxKey{duid: duid, tid: tid}, at)
	return at.Sub(e.first) >= dhcp6StuckAfter
}

// served records that the client was allocated or renewed a lease, which ends any
// pending exchange of its own: the advertised addresses it remembers were taken up.
func (t *dhcp6Tracker) served(duid string) {
	if t == nil || duid == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.adverts.dropDUID(duid)
}

// derivation is the per-record context observeDerived needs beyond the attributes: the
// receiver's DHCPv6 memory and the record's own timestamp. The zero value is valid and
// stateless.
type derivation struct {
	dhcp6 *dhcp6Tracker
	at    time.Time
}

// observeDHCP6Lease feeds the stuck-in-SOLICIT detector from one Kea v6 lease line
// (#1202). Only ADVERT, ALLOC, RENEW and REUSE matter, and only a line that named its
// client.
//
// RENEW is spelled dhcp.action=request by the parser (keaActions), the same word a v4
// DHCPREQUEST gets; the DUID is what makes it a v6 line. REUSE is what Kea writes in
// place of ALLOC or RENEW when lease caching answers from the cached lease, so it is
// just as much a served client.
func observeDHCP6Lease(sink logship.MetricSink, attrs map[string]string, d derivation) bool {
	duid := attrs["dhcp.duid"]
	if duid == "" {
		return false
	}
	switch attrs["dhcp.action"] {
	case "advertise":
		if d.dhcp6.advert(duid, attrs["dhcp.tid"], d.at) {
			return sink.ObserveDHCP6SolicitStuck(duid)
		}
	case "alloc", "request", "reuse":
		d.dhcp6.served(duid)
		return sink.ClearDHCP6SolicitStuck(duid)
	}
	return false
}

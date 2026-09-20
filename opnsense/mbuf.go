package opnsense

// mbufStatisticsData is the `mbuf-statistics` object returned by BOTH systemMbuf and
// memoryStatistics (they wrap the same `netstat -m` output). OPNsense modernised that
// payload in 26.1.11: several keys were renamed and a few dropped, while older 26.1.x /
// 25.7 boxes still send the original names. Every legacy key is therefore kept and
// resolved new-wins-else-legacy at read time — never branch on a version number.
type mbufStatisticsData struct {
	MbufCurrent int `json:"mbuf-current"`
	MbufCache   int `json:"mbuf-cache"`
	MbufTotal   int `json:"mbuf-total"`
	// There is deliberately no mbuf-max. It was exported as a ceiling (#557)
	// alongside cluster-max, but upstream removed the key in 26.1.11 and the
	// support window is now current-stable only, so it read 0 on every
	// supported box - a gauge whose own help text had to explain that its
	// value meant "no ceiling reported" rather than a ceiling. Removed with
	// its metric in OPN-0113. cluster-max and jumbo-max are still sent and
	// still exported.
	ClusterCurrent int `json:"cluster-current"`
	ClusterCache   int `json:"cluster-cache"`
	ClusterTotal   int `json:"cluster-total"`
	ClusterMax     int `json:"cluster-max"`
	MbufFails      int `json:"mbuf-failures"`
	ClusterFails   int `json:"cluster-failures"`
	PacketFails    int `json:"packet-failures"`
	MbufSleeps     int `json:"mbuf-sleeps"`
	ClusterSleeps  int `json:"cluster-sleeps"`
	PacketSleeps   int `json:"packet-sleeps"`
	// jumbop-failures / jumbop-sleeps keep the jumbop- spelling on ≥26.1.11.
	// They were never renamed, so do not "correct" them to jumbo-.
	JumbopFails  int `json:"jumbop-failures"`
	JumbopSleeps int `json:"jumbop-sleeps"`
	// Jumbo-page pool. The ≤26.1.10 spellings (jumbop-current/cache/total/max)
	// were resolved alongside these by jumboPage*() until OPN-0113 narrowed the
	// support window to current stable; measured absent on both boxes, so the
	// resolvers collapsed to direct reads. Still pointers because that is what
	// the extended-field convention below uses.
	JumboCount *int `json:"jumbo-count"`
	JumboCache *int `json:"jumbo-cache"`
	JumboTotal *int `json:"jumbo-total"`
	JumboMax   *int `json:"jumbo-max"`

	BytesInUse int64 `json:"bytes-in-use"`
	BytesTotal int64 `json:"bytes-total"`
	// BytesInCache is memory sitting in the mbuf allocator's cache -- already charged
	// to the mbuf/cluster/jumbo pools but not currently in use, so reusable without a
	// new system allocation. Upstream's netstat -m emits it in the SAME xo_emit call
	// as BytesInUse/BytesTotal above ("bytes allocated to network (current/cache/
	// total)"), unconditionally on every release (verified against FreeBSD's
	// usr.bin/netstat/mbuf.c) -- so, matching those two fields, this is a plain
	// int64, not a pointer (#579).
	BytesInCache int64 `json:"bytes-in-cache"`
	// Extended fields present in systemMbuf on OPNsense 26.1+ (both endpoints wrap the
	// same `netstat -m` output). Pointers so a nil distinguishes "key absent" (older
	// release) from "present with value 0", which decides whether the redundant
	// memoryStatistics call is needed (#137).
	Jumbo9Failures    *int `json:"jumbo9-failures"`
	Jumbo16Failures   *int `json:"jumbo16-failures"`
	Jumbo9Sleeps      *int `json:"jumbo9-sleeps"`
	Jumbo16Sleeps     *int `json:"jumbo16-sleeps"`
	SendfileSyscalls  *int `json:"sendfile-syscalls"`
	SendfileIOCount   *int `json:"sendfile-io-count"`
	SendfilePagesSent *int `json:"sendfile-pages-sent"`

	// Sfbufs allocation-pressure counters, present alongside the jumbo9/16/sendfile
	// keys above on OPNsense 26.1.11+ (#237). Pointers so nil distinguishes "key
	// absent" (older release) from "present with value 0" — same convention as the
	// jumbo9/jumbo16/sendfile fields.
	SfbufsAllocFailed *int `json:"sfbufs-alloc-failed"`
	SfbufsAllocWait   *int `json:"sfbufs-alloc-wait"`

	// Jumbo9 (9k) / Jumbo16 (16k) jumbo-cluster pool utilization, present alongside
	// the jumbo9/jumbo16 failure/sleep counters above on OPNsense 26.1+ (#579).
	// Pointers so nil distinguishes "key absent" (older release) from "present with
	// value 0" -- same convention as those counters.
	//
	// Jumbo16Limit is netstat -m's ceiling field for the 16k zone; Jumbo9Max is the
	// SAME ceiling concept for the 9k zone, but upstream spells the JSON key
	// differently per zone. Verified against FreeBSD's usr.bin/netstat/mbuf.c: both
	// fields are produced by an otherwise-identical xo_emit format string whose
	// human-readable label reads "(current/cache/total/max)" in BOTH cases --
	//   {:jumbo9-count/%ju}/{:jumbo9-cache/%ju}/{:jumbo9-total/%ju}/{:jumbo9-max/%ju}
	//   {:jumbo16-count/%ju}/{:jumbo16-cache/%ju}/{:jumbo16-total/%ju}/{:jumbo16-limit/%ju}
	// -- so this is an upstream naming inconsistency, not two different quantities;
	// both resolve to the same PoolMax["jumbo9"|"jumbo16"] entry at read time.
	Jumbo9Cache  *int `json:"jumbo9-cache"`
	Jumbo9Count  *int `json:"jumbo9-count"`
	Jumbo9Max    *int `json:"jumbo9-max"`
	Jumbo9Total  *int `json:"jumbo9-total"`
	Jumbo16Cache *int `json:"jumbo16-cache"`
	Jumbo16Count *int `json:"jumbo16-count"`
	Jumbo16Limit *int `json:"jumbo16-limit"`
	Jumbo16Total *int `json:"jumbo16-total"`

	// Packet secondary zone (m_getcl()'s pre-combined mbuf+cluster), 26.1+ only.
	// Only current/cache are ever reported: the zone borrows memory from the mbuf
	// and cluster zones rather than owning its own allocation, so netstat -m has no
	// packet-total or packet-max/-limit key to decode. Verified against upstream
	// source: the packet zone's xo_emit line carries only
	// {:packet-count/%ju}/{:packet-free/%ju}, labelled "(current/cache)" -- unlike
	// the four-value jumbo zone lines above.
	PacketCount *int `json:"packet-count"`
	PacketFree  *int `json:"packet-free"`
}

// jumboPage* read the jumbo-page pool. They stay as methods rather than
// becoming direct field reads because a nil pointer must still mean "key
// absent" and read 0, not panic.
func (s mbufStatisticsData) jumboPageCurrent() int { return firstPresentInt(s.JumboCount) }

func (s mbufStatisticsData) jumboPageCache() int { return firstPresentInt(s.JumboCache) }

func (s mbufStatisticsData) jumboPageTotal() int { return firstPresentInt(s.JumboTotal) }

func (s mbufStatisticsData) jumboPageMax() int { return firstPresentInt(s.JumboMax) }

// firstPresentInt returns the first non-nil pointer's value, or 0 when all are nil
// (i.e. none of the candidate JSON keys was sent).
func firstPresentInt(candidates ...*int) int {
	for _, c := range candidates {
		if c != nil {
			return *c
		}
	}
	return 0
}

type mbufResponse struct {
	MbufStatistics mbufStatisticsData `json:"mbuf-statistics"`
}

type memoryStatisticsData struct {
	Jumbo9Failures    int `json:"jumbo9-failures"`
	Jumbo16Failures   int `json:"jumbo16-failures"`
	Jumbo9Sleeps      int `json:"jumbo9-sleeps"`
	Jumbo16Sleeps     int `json:"jumbo16-sleeps"`
	SendfileSyscalls  int `json:"sendfile-syscalls"`
	SendfileIOCount   int `json:"sendfile-io-count"`
	SendfilePagesSent int `json:"sendfile-pages-sent"`
}

type memoryStatisticsResponse struct {
	MbufStatistics memoryStatisticsData `json:"mbuf-statistics"`
}

type MbufStatistics struct {
	MbufCurrent    int
	MbufCache      int
	MbufTotal      int
	ClusterCurrent int
	ClusterCache   int
	ClusterTotal   int
	ClusterMax     int
	// Jumbo-page pool (jumbo-* since 26.1.11; the older jumbop-* spellings on
	// ≤26.1.x left the support window in OPN-0113 and are no longer decoded).
	JumboPageCurrent int
	JumboPageCache   int
	JumboPageTotal   int
	JumboPageMax     int
	// int64: byte totals ×1024 can exceed 2^31 on large-memory boxes (#103).
	BytesInUse int64
	BytesTotal int64
	// BytesInCache: see mbufStatisticsData.BytesInCache -- decoded unconditionally,
	// same ×1024 KB->bytes conversion as BytesInUse/BytesTotal (#579).
	BytesInCache int64
	// PoolCurrent/-Cache/-Total/-Max back
	// opnsense_mbuf_pool_{current,cache,total,max}{pool=...} (#579): jumbo9, jumbo16,
	// and the packet secondary zone. A pool's key is present in a map ONLY when
	// systemMbuf carried it for that release -- e.g. "packet" never appears in
	// PoolTotal/PoolMax because upstream's packet zone borrows memory from mbuf/
	// cluster and reports no ceiling of its own. jumbo16's PoolMax entry is read
	// from netstat's jumbo16-limit key, normalised onto the same metric as jumbo9's
	// jumbo9-max (see mbufStatisticsData for the upstream naming-asymmetry
	// evidence). Distinct from mbuf/cluster pool fields above: those are
	// unconditional (every release reports them); these are 26.1+-only and may
	// legitimately be absent, so -- per convention -- absence means no map entry,
	// never a fabricated 0.
	PoolCurrent       map[string]int
	PoolCache         map[string]int
	PoolTotal         map[string]int
	PoolMax           map[string]int
	FailuresByType    map[string]int
	SleepsByType      map[string]int
	SendfileSyscalls  int
	SendfileIOCount   int
	SendfilePagesSent int
}

func (c *Client) FetchMbufStatistics() (MbufStatistics, *APICallError) {
	var resp mbufResponse
	var data MbufStatistics

	url, ok := c.endpoints["systemMbuf"]
	if !ok {
		return data, &APICallError{
			Endpoint:   "systemMbuf",
			Message:    "endpoint not found in client endpoints",
			StatusCode: 0,
		}
	}

	if err := c.do("GET", url, nil, &resp); err != nil {
		return data, err
	}

	s := resp.MbufStatistics

	data.MbufCurrent = s.MbufCurrent
	data.MbufCache = s.MbufCache
	data.MbufTotal = s.MbufTotal
	data.ClusterCurrent = s.ClusterCurrent
	data.ClusterCache = s.ClusterCache
	data.ClusterTotal = s.ClusterTotal
	data.ClusterMax = s.ClusterMax
	data.JumboPageCurrent = s.jumboPageCurrent()
	data.JumboPageCache = s.jumboPageCache()
	data.JumboPageTotal = s.jumboPageTotal()
	data.JumboPageMax = s.jumboPageMax()
	// OPNsense sources these from netstat -m, which reports the mbuf memory
	// pool in KILOBYTES. The exporter's bytes_in_use / bytes_total metrics are
	// declared in bytes, so convert here.
	data.BytesInUse = s.BytesInUse * 1024
	data.BytesTotal = s.BytesTotal * 1024
	data.BytesInCache = s.BytesInCache * 1024

	data.FailuresByType = map[string]int{
		"mbuf":    s.MbufFails,
		"cluster": s.ClusterFails,
		"packet":  s.PacketFails,
		"jumbop":  s.JumbopFails,
	}

	data.SleepsByType = map[string]int{
		"mbuf":    s.MbufSleeps,
		"cluster": s.ClusterSleeps,
		"packet":  s.PacketSleeps,
		"jumbop":  s.JumbopSleeps,
	}

	// Jumbo9/jumbo16/packet pool utilization (#579): read independently of the
	// jumbo9/jumbo16 failure/sleep presence check below, since none of these keys
	// are ever present in the memoryStatistics fallback response -- an absent
	// pointer here just means "no entry", on every release, not "fall back
	// elsewhere". Maps are always initialised (never nil) so callers can range over
	// them unconditionally; keys are added only when the box actually reported them.
	data.PoolCurrent = map[string]int{}
	data.PoolCache = map[string]int{}
	data.PoolTotal = map[string]int{}
	data.PoolMax = map[string]int{}

	if s.Jumbo9Count != nil {
		data.PoolCurrent["jumbo9"] = *s.Jumbo9Count
	}
	if s.Jumbo9Cache != nil {
		data.PoolCache["jumbo9"] = *s.Jumbo9Cache
	}
	if s.Jumbo9Total != nil {
		data.PoolTotal["jumbo9"] = *s.Jumbo9Total
	}
	if s.Jumbo9Max != nil {
		data.PoolMax["jumbo9"] = *s.Jumbo9Max
	}

	if s.Jumbo16Count != nil {
		data.PoolCurrent["jumbo16"] = *s.Jumbo16Count
	}
	if s.Jumbo16Cache != nil {
		data.PoolCache["jumbo16"] = *s.Jumbo16Cache
	}
	if s.Jumbo16Total != nil {
		data.PoolTotal["jumbo16"] = *s.Jumbo16Total
	}
	if s.Jumbo16Limit != nil {
		// jumbo16-limit IS jumbo16's ceiling, just spelled differently than
		// jumbo9-max -- see the mbufStatisticsData field comment for the upstream
		// evidence this is one quantity, not two.
		data.PoolMax["jumbo16"] = *s.Jumbo16Limit
	}

	if s.PacketCount != nil {
		data.PoolCurrent["packet"] = *s.PacketCount
	}
	if s.PacketFree != nil {
		// packet-free IS the packet zone's cache figure: netstat's own
		// human-readable text for this line reads "(current/cache)".
		data.PoolCache["packet"] = *s.PacketFree
	}
	// No packet-total / packet-max(-limit) key exists upstream -- the packet zone
	// borrows memory from mbuf/cluster rather than owning a ceiling, so PoolTotal
	// and PoolMax deliberately never get a "packet" entry.

	// On OPNsense 26.1+ the jumbo9/jumbo16/sendfile keys are already in the systemMbuf
	// response, so read them from there and skip the redundant memoryStatistics
	// round-trip entirely (#137). A nil pointer means the key was absent (older release).
	if s.Jumbo9Failures != nil || s.SendfileSyscalls != nil {
		data.FailuresByType["jumbo9"] = derefInt(s.Jumbo9Failures)
		data.FailuresByType["jumbo16"] = derefInt(s.Jumbo16Failures)
		data.SleepsByType["jumbo9"] = derefInt(s.Jumbo9Sleeps)
		data.SleepsByType["jumbo16"] = derefInt(s.Jumbo16Sleeps)
		data.SendfileSyscalls = derefInt(s.SendfileSyscalls)
		data.SendfileIOCount = derefInt(s.SendfileIOCount)
		data.SendfilePagesSent = derefInt(s.SendfilePagesSent)

		// Sfbufs allocation-pressure counters (26.1.11+, #237): gated separately
		// since they landed slightly after the jumbo9/jumbo16/sendfile keys above —
		// a nil pointer here means an older release within this same branch.
		if s.SfbufsAllocFailed != nil || s.SfbufsAllocWait != nil {
			data.FailuresByType["sfbufs"] = derefInt(s.SfbufsAllocFailed)
			data.SleepsByType["sfbufs"] = derefInt(s.SfbufsAllocWait)
		}

		return data, nil
	}

	// Fallback for older releases whose systemMbuf lacks the extended keys: fetch them
	// from the separate memoryStatistics endpoint (partial-failure tolerant).
	var memResp memoryStatisticsResponse
	memURL, ok := c.endpoints["memoryStatistics"]
	if ok {
		if memErr := c.do("GET", memURL, nil, &memResp); memErr != nil {
			c.log.Warn("memory statistics sub-call failed",
				"endpoint", "memoryStatistics",
				"error", memErr.Error(),
			)
		} else {
			ms := memResp.MbufStatistics
			data.FailuresByType["jumbo9"] = ms.Jumbo9Failures
			data.FailuresByType["jumbo16"] = ms.Jumbo16Failures
			data.SleepsByType["jumbo9"] = ms.Jumbo9Sleeps
			data.SleepsByType["jumbo16"] = ms.Jumbo16Sleeps
			data.SendfileSyscalls = ms.SendfileSyscalls
			data.SendfileIOCount = ms.SendfileIOCount
			data.SendfilePagesSent = ms.SendfilePagesSent
		}
	}

	return data, nil
}

// derefInt returns the pointed-to int, or 0 when the pointer is nil (JSON key absent).
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

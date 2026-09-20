---
id: OPN-0113
title: Decide per endpoint whether to prune the pre-26.7 legacy payload shims
status: To Do
assignee: []
created_date: '2026-09-20 11:18'
updated_date: '2026-09-20 18:59'
labels:
  - canary
  - compatibility
dependencies: []
priority: low
type: chore
ordinal: 67000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OPN-0111 narrowed the support policy to the current stable release on 2026-09-20, which fired the "prune when 26.1 leaves the support window" trigger on nine exemption entries at once: idsSettings, keaSubnets4, memoryStatistics, ndpTable, pfStatisticsByInterface, pfStatsInfo, protocolStatistics, quaggaOspfNeighbors and systemMbuf. Their notes now record that the trigger fired and point here; none of them was pruned, deliberately.

Pruning is not a ledger edit. Removing a missingOK entry alone makes the canary ENFORCE a key that no supported box sends, which turns every run into a permanent missing-path warning - the standing-noise failure OPN-0096 already had to clean up once. The real prune is deleting the legacy field from the response struct, and that drops the tolerant-reader path that lets a box on the older shape keep working at all. docs/compatibility.md says explicitly that a shape costing nothing to keep reading is usually kept, so a blanket prune would contradict the page this trigger came from.

So each entry is its own decision, weighed on what the shim actually costs: a legacy field that is decoded-only and never read into a metric costs close to nothing and can stay indefinitely, while one that forces a coalescing branch in a hot decode path or blocks a struct simplification is worth removing. Several of these are 26.1.11 renames where upstream kept both spellings briefly, and at least one pair (protocolStatistics) is noted as decoded-only with no metric impact.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every one of the nine entries carries a recorded decision - prune or keep - justified against what the shim costs and what a box on the older shape would lose
- [ ] #2 Any entry decided for pruning has both the struct field and its ledger entry removed in the same change, never the ledger entry alone
- [ ] #3 A live canary run after the change reports no new missing paths on either profile
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
ALL 31 PATHS MEASURED LIVE 2026-09-20 on both boxes (102 on opnsense-devel 27.1.a_287, 106 on opnsense 26.7.4_1). Measured rather than read off the notes, because the ledger notes have been wrong twice this week - the IPsec topology in coverage.json and the FRR coalescing direction in this very entry both turned out false.

METHOD, and it is reusable: the canary cannot answer this, because a `missingOK` path is tolerated whether or not the box sends it, and running the canary from a branch with the entries stripped is impossible - the `tailnet` environment's deployment branch policy allows `main` ONLY, so a branch run dies at startup with no jobs and `log not found`. Widening that policy for secrets that mint tailnet auth keys was not worth it. Instead the payloads were read straight off the guests, from the SAME producers the API wraps, which needs no credential:
  mbuf                    /usr/bin/netstat -m --libxo json          (actions_system.conf [show.mbuf] is this verbatim)
  protocolStatistics      /usr/bin/netstat -s --libxo json
  ndpTable                /usr/local/opnsense/scripts/interfaces/list_ndp.py
  pfStatsInfo             /usr/local/opnsense/scripts/filter/pfstatistics.py info
  pfStatisticsByInterface /usr/local/opnsense/scripts/filter/pfstatistics.py interfaces
  quaggaOspfNeighbors     vtysh -c "show ip ospf neighbor json"
  idsSettings             /conf/config.xml OPNsense->IDS->general
  keaSubnets4             the subnet4 node of mvc/app/models/OPNsense/Kea/KeaDhcpv4.xml

RESULT: 30 of the 31 legacy paths are ABSENT on both boxes, and their replacements are present. The one exception is not a legacy path at all - see pfStatsInfo below.

  memoryStatistics / systemMbuf (7 each, SAME struct): all absent. jumbo-count/cache/total/max/page-size present instead.
  protocolStatistics (6): all absent. tcp.ecn now carries received-ce/ect0/ect1-packets plus the ace-* family; syncache cookies moved to tcp.syncookies.{sent,received,failed,spurious}-cookies.
  ndpTable (2): absent - list_ndp.py emits only ip, mac, intf, manufacturer.
  quaggaOspfNeighbors (3): absent, nbrState/ifaceAddress/nbrPriority present, confirmed against a REAL Full adjacency. Worth recording separately: that adjacency is 102 <-> 106 directly over the shared TESTLAN, not via the retired guest 110, which is why OPN-0110 lost no quagga coverage.
  idsSettings (2): ids.general.ips absent; ids.general.mode present, value "pcap", on both.
  keaSubnets4 (1): absent, and structurally so - the subnet4 model node has no interface field at all, only subnet_id/subnet/pools plus ddns and option fields.
  pfStatsInfo (2): current-entries.rate absent on both tables, BUT searches.rate is PRESENT. This is the exception, and it decides the entry - see below.

THREE CORRECTIONS TO THIS TASK'S OWN FRAMING, each changing a decision:

1. THE NOTE ON quaggaOspfNeighbors IS WRONG ABOUT THE CODE. It says the collector "already coalesces new-wins-else-legacy". frr.go:554 does the opposite - `state := row.State.String(); if state == "" { state = row.NbrState.String() }` - legacy first, new as fallback. Functionally identical while only one name is ever present, but anyone reasoning about precedence from the note is reasoning from the wrong code.

2. rows[].priority IS NOT A COALESCING SHIM, IT IS DEAD CODE. Priority AND NbrPriority both have ZERO consumers anywhere outside their struct tags. So does pfStatsCounterEntry.Rate, protocolStatistics' ReceivedAcksForUnsentData, and the syncache SentCookies/ReceivdCookies pair (the one SentCookies consumer at protocol_statistics.go:849 reads the SYNCOOKIES struct, not the syncache one). Deleting these is a struct simplification available regardless of the support window, and it is not what this task was set up to decide.

3. PRUNING pfStatsInfo WOULD MAKE THINGS WORSE, and the measurement is what proves it. Rate is ONE shared field on pfStatsCounterEntry, used by current-entries, searches, inserts and removals alike. Gauges have no rate so current-entries omits it - hence exactly two missingOK paths - but searches/inserts/removals DO still send it. Removing the missingOK entries makes the canary enforce a key gauges never carry; deleting the field makes `rate` unmodelled on the six paths that DO send it, trading two tolerated absences for six extra-key warnings. KEEP, and this is now settled rather than deferred.

WHAT THE MEASUREMENT FOUND THAT THE TASK DID NOT ASK ABOUT - three legacy fields are not inert, they are actively degrading output, and each is a BREAKING change to fix:
  - ndpTable [].type feeds a LABEL on opnsense_ndp_entries (internal/collector/ndp.go:85). No supported box sends it, so that label is permanently empty on every series.
  - keaSubnets4 %interface feeds the `interface` LABEL on opnsense_kea_dhcp4_pool_size and its dhcp6 sibling (internal/collector/kea.go:262,274). Permanently empty for the same reason.
  - mbuf-max feeds opnsense_..._mbuf_max (internal/collector/mbuf.go:208). Permanently 0. Its own help text already warns "May read 0 on OPNsense >=26.1.11 ... that means no ceiling was reported, not a ceiling of zero" - a metric documenting its own uselessness rather than being removed.
These are dashboard-visible, so removing a label or a metric breaks consumers. They are Rob's call, not a shim decision.
<!-- SECTION:NOTES:END -->

---
id: OPN-0113
title: Decide per endpoint whether to prune the pre-26.7 legacy payload shims
status: Done
assignee: []
created_date: '2026-09-20 11:18'
updated_date: '2026-09-20 19:56'
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
- [x] #1 Every one of the nine entries carries a recorded decision - prune or keep - justified against what the shim costs and what a box on the older shape would lose
- [x] #2 Any entry decided for pruning has both the struct field and its ledger entry removed in the same change, never the ledger entry alone
- [x] #3 A live canary run after the change reports no new missing paths on either profile
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
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

DECISIONS RECORDED, all nine entries, Rob 2026-09-20. Two questions were put to him: what to do about the three legacy fields that were not inert, and how far the prune should go. He chose "remove all three" and "prune shims + delete the dead fields".

PRUNED, field and ledger entry together (AC2):
  idsSettings ids.general.ips           - collapsed ipsModeEnabled() to the mode selector alone. TestFetchIDS_LegacyIPSField and its fixture deleted; _Populated and _PassiveIDS already cover both branches, so no coverage was lost.
  idsSettings ids.general.mode.*        - the REVERSE shim (tolerating mode being absent on <= 26.1). mode is present on both boxes, so enforcing it is now correct and even desirable: it will catch upstream removing it.
  keaSubnets4 rows[].%interface         - and the label it fed. See BREAKING below.
  memoryStatistics + systemMbuf jumbop-*  - four firstPresentInt resolvers collapsed to direct reads.
  memoryStatistics + systemMbuf percentage, mbuf-and-cluster - decoded-only, zero consumers.
  memoryStatistics + systemMbuf mbuf-max  - and the metric it fed. See BREAKING below.
  ndpTable [].expire                    - decoded-only, never even copied into the public struct.
  ndpTable [].type                      - and the label it fed. See BREAKING below.
  pfStatisticsByInterface interfaces.*.interface - the json tag was a phantom: the field is overwritten from the MAP KEY two lines after decoding, so it is now json:"-". The label is unaffected.
  protocolStatistics ecn ce/ect0/ect1-packets - three firstPresentNum resolvers collapsed.
  protocolStatistics received-acks-for-unsent-data, syncache receivd-cookies/sent-cookies - decoded-only, zero consumers.
  quaggaOspfNeighbors rows[].state, rows[].address - the new-vs-legacy coalescing block in frr.go deleted.
  quaggaOspfNeighbors rows[].priority   - not a shim: BOTH spellings were dead.

KEPT, one entry, and the measurement is what settled it rather than deferring it again:
  pfStatsInfo current-entries.rate x2   - `rate` is ONE shared field on pfStatsCounterEntry. Gauges omit it (hence exactly two missingOK paths) but searches/inserts/removals still SEND it, measured on both boxes. Enforcing the key would be wrong; deleting the field trades two tolerated absences for extra-key warnings. The field went anyway because nothing read it, and the real keys are declared knownExtraPaths instead - which is the honest shape, not a shim.

BREAKING, and Rob authorised each one. Three legacy fields were not inert:
  opnsense_ndp_entries lost its `type` label
  opnsense_kea_dhcp4_pool_size and its dhcp6 sibling lost their `interface` label
  opnsense_mbuf_max is REMOVED, along with its dashboard series in grafana/tabs/system.py
Anyone selecting on those labels was selecting on an empty string, and anyone graphing mbuf_max was graphing a constant 0 whose own help text explained that the value meant "no ceiling reported".

A SECOND CANARY RUN WAS NEEDED, and the reason is worth keeping. Deleting a modelled field turns any key the box still sends into an UNEXPECTED key - the inverse of the missing-path problem this task is about. The first post-prune run (35532753994) reported 0 missing paths but three new extras: pfStatsInfo info.counters.*.rate and info.limit-counters.*.rate, because the six named table paths were enumerated and the two MAP-VALUED families were missed, and quaggaOspfNeighbors rows[].nbrPriority, because deleting the dead field left FRR's key unmodelled. Declared as knownExtraPaths in 955a418d; run 35533378902 then returned both profiles to exactly their pre-prune extra-key counts - 30 paths on nightly, 25 on release - with 0 missing paths, 0 breaking drift and 0 probe errors on both. Plan for that second run when pruning a modelled field.

THE FIELDAUDIT ACCEPTANCE SLOT MOVED. cmd/fieldaudit's acceptanceFindings held exactly one entry, ndpEntry.Expire, described as the case a textual scan misses and therefore the proof that the analysis is type-aware rather than grepping. This change deletes that field, so the slot was repointed to interfaceConfigEntry.Device, which has the same property: ".Device" is read in roughly 160 places across opnsense/ and internal/ (NDPEntry.Device among them) while this one cannot be read by construction, because FetchInterfaceEnumeration hand-walks the raw JSON to preserve key order (#361) and never decodes into the type. Never empty that list and never replace its entry with a field whose name is unique.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
All nine entries decided and eight pruned, with the struct field and the ledger entry removed together in every case. Measured first: 30 of the 31 legacy paths are absent on both boxes, read straight off the API's own producers rather than taken from the notes, which have been wrong twice this week.

pfStatsInfo is the single keep, and it is now settled rather than deferred - `rate` is a shared field that gauges omit and counters still send, so neither enforcing the key nor deleting the field was a win. The field went because nothing read it; the keys are declared instead.

The measurement also found what the task did not ask about: three of these legacy fields were feeding a permanently empty label or a permanently zero gauge. Rob authorised removing all three, so opnsense_ndp_entries and the Kea pool_size pair each lose a label and opnsense_mbuf_max is gone.

Verified by two live canary runs, because deleting a modelled field converts keys the box still sends into unexpected ones. The second returned both profiles to their exact pre-prune extra-key counts with 0 missing paths, 0 breaking drift and 0 probe errors.
<!-- SECTION:FINAL_SUMMARY:END -->

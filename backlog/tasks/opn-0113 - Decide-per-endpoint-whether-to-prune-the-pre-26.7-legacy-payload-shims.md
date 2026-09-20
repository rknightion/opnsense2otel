---
id: OPN-0113
title: Decide per endpoint whether to prune the pre-26.7 legacy payload shims
status: To Do
assignee: []
created_date: '2026-09-20 11:18'
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

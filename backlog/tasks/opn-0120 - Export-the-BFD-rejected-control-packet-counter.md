---
id: OPN-0120
title: Export the BFD rejected-control-packet counter
status: In Progress
assignee:
  - '@loop10'
created_date: '2026-10-08 21:56'
updated_date: '2026-10-12 20:00'
labels:
  - frr
  - feature
dependencies: []
priority: medium
ordinal: 73000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The 2026-09-20 canary triage (OPN-0119) found FRR's BFD peer counters now carry controlPacketBadInput (FRR 10.7.1 bfdd/bfdd_vty.c, incremented in bfdd/bfd_packet.c when a received control packet is rejected). It is ledgered as an opportunity in opnsense/testdata/schemas/exemptions.json under quaggaBfdCounters. It is a real fault signal (authentication or malformed-packet rejects) sitting next to the control-packet input counter the FRR collector already exports, and today a peer dropping packets for a bad-input reason is invisible.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A per-peer counter for rejected BFD control packets is exported beside the existing BFD control-packet counters, with the same labels
- [ ] #2 Older FRR builds that omit the key produce no series rather than a fabricated zero
- [ ] #3 The quaggaBfdCounters knownExtraPaths entry for controlPacketBadInput is removed and the golden schema regenerated
- [ ] #4 The BFD dashboard panel or alert catalogue shows the new counter
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Investigate existing FRR counter/metric seam, add presence-aware bad-input counter with public-client/collector compatibility tests, consume schema ledger, generate dashboard/docs, serialize full gate, return staged candidate for guarded review and root publication.
<!-- SECTION:PLAN:END -->

---
id: OPN-0120
title: Export the BFD rejected-control-packet counter
status: To Do
assignee: []
created_date: '2026-10-08 21:56'
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

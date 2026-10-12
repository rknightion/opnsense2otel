---
id: OPN-0120
title: Export the BFD rejected-control-packet counter
status: Done
assignee:
  - '@loop10'
created_date: '2026-10-08 21:56'
updated_date: '2026-10-12 21:43'
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
- [x] #1 A per-peer counter for rejected BFD control packets is exported beside the existing BFD control-packet counters, with the same labels
- [x] #2 Older FRR builds that omit the key produce no series rather than a fabricated zero
- [x] #3 The quaggaBfdCounters knownExtraPaths entry for controlPacketBadInput is removed and the golden schema regenerated
- [x] #4 The BFD dashboard panel or alert catalogue shows the new counter
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Investigate existing FRR counter/metric seam, add presence-aware bad-input counter with public-client/collector compatibility tests, consume schema ledger, generate dashboard/docs, serialize full gate, return staged candidate for guarded review and root publication.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop10: 2 implementation cycles. Presence-aware rejected-control counter uses existing peer/instance labels; older missing/null key emits no series and present zero emits counter zero. Public HTTP client/collector proof failed before implementation, parser tag-omission proof failed then exact source restored; race checks and full final gate passed. Synthetic fixtures use symbolic peers with no literal addresses, schema opportunity consumed and golden regenerated; existing control-rate dashboard exposes rate with no zero fill. Guarded security PASS and completed CodeRabbit zero findings across all 18 final paths. Root integration, exact CI, Grafana sync and deploy readback pending; not Done yet.

loop10: 2 implementation cycles, completed. Exact landed SHA passed composed full gate and ci-success after one uncharged dependency-fetch infrastructure retry. Both Grafana jobs succeeded: live GitSync verifier matched dashboards, rules prune no-op and 80 resources pushed without errors (no independent rule-content verifier claimed). Exact build observed; FRR collector success is 1. Production has zero rejected-counter series, so local real HTTP-client/collector fixtures prove emission and missing-key compatibility, not a live rejected-packet event.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added presence-aware per-peer rejected BFD control counter with unchanged control-counter labels; missing/null keys omit series, present zero emits zero. Consumed schema opportunity, regenerated golden/docs and extended rate panel without zero fill. Fail-first client/collector proof, source/composed gates, guarded review, CodeRabbit, exact CI and actual dashboard delivery verified.
<!-- SECTION:FINAL_SUMMARY:END -->

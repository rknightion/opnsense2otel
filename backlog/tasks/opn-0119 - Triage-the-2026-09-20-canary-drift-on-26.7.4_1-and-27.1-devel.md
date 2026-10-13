---
id: OPN-0119
title: Triage the 2026-09-20 canary drift on 26.7.4_1 and 27.1 devel
status: In Progress
assignee:
  - '@claude'
created_date: '2026-10-08 20:48'
updated_date: '2026-10-13 12:43'
labels:
  - canary
  - api-drift
dependencies: []
priority: medium
ordinal: 72000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub #726 (release-vm) and #727 (nightly) carry new unexpected-nested-key findings from the 2026-09-20 on-demand canary runs against 26.7.4_1 and devel 27.1.a: authUsers menu_favorites, firewallRules audit/max-pkt-rate/received-on/scrub_*, Kea leases lease_type/sort_*, Kea subnets subnet_id, NAT audit/ref, pfStatsInfo counter rate, FRR BFD/BGP fields, tailscale Peer Expired. OPN-0096 cleared the earlier set; these postdate it and none is in opnsense/testdata/schemas/exemptions.json. Two runs disagreeing on pfStatsInfo rate is a box-state tell. Verdicts follow reference/canary-triage.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every finding in the latest #726 and #727 comments has one verdict (box-state/absorb/chase/drop/opportunity) backed by an upstream source citation
- [x] #2 exemptions.json carries a knownExtraPaths or missingOK entry with a note and prune trigger for every exempted path
- [x] #3 just check passes
- [ ] #4 A fresh canary run on both profiles reports none of the triaged paths, and #726/#727 close
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
All 30 nested-key findings from the latest #726/#727 comments are opportunity (upstream additions in 26.7.2-26.7.4 and master scrub_*), each ledgered in knownExtraPaths with core/plugins/FRR/tailscale source citations and a prune trigger. pfStatsInfo rate / nbrPriority were already ledgered by 955a418d; the Kea lease flapping was empty-lease box-state. Replayed all 30 through ValidateResponseSchema: 30/30 flagged on the old ledger, 0/30 on the new, unledgered controls still flagged. Roadmap candidates: BFD controlPacketBadInput, Kea subnet_id (per-subnet stats join key), BGP receivedPrefixDup. Not pre-ledgered: SNAT/configured-DNAT rows[].audit (unobserved; the canary will flag them when a box has such a rule).

2026-10-13 direct follow-up: current lab host status reachable from operator Mac, no hold, all testbed guests stopped. This is not CI identity proof. Dispatching existing self-powered canary to test current CI path and obtain fresh dual-profile evidence; no concurrent delivery proof.

Fresh dual-profile canary run38396847166 at7d5168cda507cc87b457e6a56f8a578bebed1ac2 passed schema execution and exporter smoke; teardown verified lab off/no hold. Previously triaged paths absent. Issues726/727 remain open for six new nested fields across interfaceConfig,keaSubnets4,systemDisk; source-first triage in progress. Workflow success does not satisfy issue-close acceptance yet.

Added six exact source-backed knownExtraPaths with prune triggers. Real validator proof failed before, then accepted six additions while retaining three nearby unledgered controls. Exemption consistency and full just check passed. Fresh post-publication dual-profile canary still required.
<!-- SECTION:NOTES:END -->

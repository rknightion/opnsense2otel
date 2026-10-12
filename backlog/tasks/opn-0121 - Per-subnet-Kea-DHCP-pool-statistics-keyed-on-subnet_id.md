---
id: OPN-0121
title: Per-subnet Kea DHCP pool statistics keyed on subnet_id
status: In Progress
assignee:
  - '@loop10'
created_date: '2026-10-08 21:56'
updated_date: '2026-10-12 21:33'
labels:
  - kea
  - feature
dependencies: []
priority: medium
ordinal: 74000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The 2026-09-20 canary triage (OPN-0119) found keaSubnets4/keaSubnets6 search rows now carry subnet_id (an AutoNumberField in core KeaDhcpv4.xml/KeaDhcpv6.xml from 26.7.2). That is the key Kea's own per-subnet statistics are indexed by (subnet[N].assigned-addresses, total-addresses, declined-addresses), so it makes per-subnet pool utilisation and exhaustion possible, which the exporter cannot express today. Unverified: whether OPNsense exposes Kea's statistic-get-all through any API endpoint; establish that from upstream source before designing anything, and if no endpoint exists, record that and close this as blocked upstream. subnet_id is ledgered as an opportunity in opnsense/testdata/schemas/exemptions.json.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Whether an OPNsense API endpoint exposes Kea per-subnet statistics is established from upstream source and recorded in the task notes
- [ ] #2 If one exists: per-subnet assigned/total address gauges are exported for both families, labelled by subnet (CIDR) rather than raw subnet_id, with bounded cardinality
- [ ] #3 subnet_id leaves the knownExtraPaths ledger once it is consumed, with the golden schema regenerated
- [x] #4 If none exists: the task records the upstream gap and is closed without code
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Establish from current upstream OPNsense source whether REST exposes native Kea per-subnet statistics. If absent, return source SHA/path evidence for root notes and conditional no-code close. If present, implement within frozen files with bounded CIDR-labelled gauges, schema/dashboard generation, failing-first proof and guarded review. No live firewall/testbed calls.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop10: read-only investigation, 0 implementation attempts. Public opnsense/core at 8e6f3f34a90051ca6819e23c22e84a5afca856bf inspected through concrete routing, dispatcher, every Kea controller/base chain and actions_kea.conf. Management REST exposes configuration, aggregate lease record counts and lifecycle/status, but no native statistic-get-all/per-subnet assigned/total capacity chain. Optional separate native Kea Control Agent HTTP listener exists, disabled/loopback by default; it is not the management API auth/ACL surface and would require a separately agreed access/security design. Independent guarded evidence review PASS. No exporter/schema/dashboard code changed; both subnet_id opportunity entries retained. AC2/AC3 conditional on such endpoint are N/A, not implemented or falsely checked. Root metadata publication/final gate pending; CodeRabbit skipped read-only.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Established the upstream management-API gap from pinned source and chose the authorised no-code branch. No fallback lease-count capacity metrics or native-agent credential/configuration seam invented. Source identity: https://github.com/opnsense/core/commit/8e6f3f34a90051ca6819e23c22e84a5afca856bf
<!-- SECTION:FINAL_SUMMARY:END -->

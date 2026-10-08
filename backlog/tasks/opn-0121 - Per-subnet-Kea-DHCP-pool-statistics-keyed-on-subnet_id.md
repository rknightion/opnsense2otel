---
id: OPN-0121
title: Per-subnet Kea DHCP pool statistics keyed on subnet_id
status: To Do
assignee: []
created_date: '2026-10-08 21:56'
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
- [ ] #1 Whether an OPNsense API endpoint exposes Kea per-subnet statistics is established from upstream source and recorded in the task notes
- [ ] #2 If one exists: per-subnet assigned/total address gauges are exported for both families, labelled by subnet (CIDR) rather than raw subnet_id, with bounded cardinality
- [ ] #3 subnet_id leaves the knownExtraPaths ledger once it is consumed, with the golden schema regenerated
- [ ] #4 If none exists: the task records the upstream gap and is closed without code
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

---
id: OPN-0136
title: >-
  Export per-NIC hardware receive discards (ixl rx_discards) for physical WAN
  parents
status: To Do
assignee: []
created_date: '2026-10-15 17:16'
labels: []
dependencies: []
priority: medium
ordinal: 89000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The exporter's interface metrics cover the pppoe0 logical interface but not its physical parent NIC. On the live firewall the PPPoE parent's driver counter dev.ixl.<n>.pf.rx_discards reached about 99k in 3 days, including a burst of about 5k in 10 s, while every exported interface error and drop counter stayed at zero. PPPoE traffic is not RSS-hashed, so it all lands on a single RX queue, and these bursts are invisible in Grafana. Expose per-physical-NIC driver discard counters (and per-queue where available) for every physical interface, including PPPoE and VLAN parents, and alert on bursts. Open question: which OPNsense API or diagnostics surface exposes the driver sysctls without SSH; if none does, record that and propose the narrowest alternative.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A counter for physical-NIC driver receive discards exists per NIC, including a PPPoE parent, and matches the live sysctl value on the deployed firewall
- [ ] #2 The source surface is documented, or the absence of an API surface is recorded with the chosen alternative
- [ ] #3 A dashboard panel or alert rule surfaces discard bursts
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

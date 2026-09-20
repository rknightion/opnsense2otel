---
id: OPN-0110
title: >-
  Consolidate the testbed to three guests and run the canary profiles
  sequentially
status: To Do
assignee: []
created_date: '2026-09-20 10:43'
labels:
  - canary
  - testbed
dependencies:
  - OPN-0109
priority: medium
type: feature
ordinal: 64000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The lab is six guests on oli allocating 15 cores, 26 GB RAM and 172 GB disk: 102 nightly firewall, 106 release firewall, 110 nightly FRR/PD peer, 111 release client, 112 nightly client, 105 traffgen LXC. It is two parallel stacks because live-canary.yml probes both targets as a matrix. Rob chose on 2026-09-20 to run the profiles sequentially in one session instead, keeping 102, 106 and 105 and retiring 110, 111 and 112, saving 6 cores, 8.5 GB and 34.4 GB and halving the guests to maintain. 105 is already tri-homed - eth0 on TESTLAN from 102, eth1 on VLAN 90, eth2 on CPORTAL - so multi-homing it onto the release firewall segment is the pattern it already uses. Accepted losses, decided the same day: the 19 quagga BGP, OSPF and BFD endpoints keep being probed but verify empty rather than populated, IPv6 PD state goes the same way, and the two-host UDP pair 105 to 112 is replaced by the firewall-to-105 pair the operating model already documents. The guests stay defined and stopped until Rob confirms deletion separately - removing them from the allowlist is reversible, destroying them is not.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 One canary session probes the nightly profile then the release-vm profile against the same shared client, and each still files into its own issue
- [ ] #2 105 holds an address on both firewalls LAN segments and the readiness gate proves it before either probe starts
- [ ] #3 Guests 110, 111 and 112 are out of the power script allowlist and stay stopped, with the endpoints that lost a populated table named in the run output rather than silently reading clean
- [ ] #4 The Wave operating model doc describes the three-guest lab, the sequential run and the replacement throughput pair
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

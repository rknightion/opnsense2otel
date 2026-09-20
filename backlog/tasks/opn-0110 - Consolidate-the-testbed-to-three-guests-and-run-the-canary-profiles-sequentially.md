---
id: OPN-0110
title: >-
  Consolidate the testbed to three guests and run the canary profiles
  sequentially
status: To Do
assignee: []
created_date: '2026-09-20 10:43'
updated_date: '2026-09-20 17:10'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
MEASURED 2026-09-20 against a real three-guest run, and THE ACCEPTED-LOSSES LIST IN THE DESCRIPTION IS WRONG. Method: run 35521531188 (six guests) as the baseline, then run 35523912607 (102, 106, 105 only) after retiring the three, and set-diff the report sections rather than eyeballing a line diff of a long document. Both runs green, 202 endpoints each, and 105 held its TESTLAN lease and reached both firewalls in all four jobs - so the deltas below are the retirement and not a flake.

THE PREDICTED LOSSES DID NOT HAPPEN. Zero quagga BGP, OSPF or BFD paths moved, and neither did IPv6 PD. Those endpoints were ALREADY reading empty with 110 running, so retiring it cost nothing there. The description says they "keep being probed but verify empty rather than populated" - they were never populated.

WHAT ACTUALLY LOST ITS TABLE, all of it backing exported metrics:
  nightly, required coverage 0 -> 5:  keaLeases6 rows[].address; ipsecPhase1 rows[].install-time; ipsecSad rows[].satype; ipsecSad rows[].spi; ipsecSpd rows[].dir
  release, required coverage 1 -> 11: keaLeases4 rows[].address, .client_id, .hostname, .hwaddr, .if_descr, .mac_info; plus the same four ipsec paths
  informational unverified: +32 nightly, +24 release (ipsecPools leases[], ipsecSad rows[] and the keaLeases sub-trees)

The Kea half is explained and is what AC2 is really about: 111 and 112 were DHCP clients on the TAGGED per-firewall segments (111 on vmbr9 tag 40 = 106's, 112 on vmbr9 tag 140 = 102's), while 105's eth0 is on the UNTAGGED vmbr9 that both firewalls share. So one container on the shared segment does NOT replace two clients on the tagged ones. AC2's "both firewalls LAN segments" means giving 105 a NIC on the release firewall's segment, which is not what the first implementation did - it proved reachability across the shared segment instead, which is a weaker claim that happens to pass.

The IPsec half is NOT yet explained and should not be assumed to follow from the same cause. strongswan is running on 102; the tunnel is documented as running between the two firewalls, so retiring three guests that are not its endpoints should not have emptied it. The plausible mechanism is that 111/112 were the only source of traffic across it, so no child SA installs and the SAD/SPD read empty - but that is a hypothesis, not a measurement.

AC3's MACHINERY WORKS and is the one thing this run positively confirms: every lost path was NAMED in the report under "Unverified paths that back metrics (required coverage)" with its blocker and its exercise text, rather than reading clean. What it does NOT do is fail the run - both jobs exited 0 over eleven missing required paths.

SEPARATE DEFECT FOUND, now OPN-0114 and NOT caused by this task: both firewalls have Kea DHCPv4, Kea DHCPv6 and dnsmasq-with-a-range enabled simultaneously and race for UDP/67. Observed on one boot: 102 had both Kea daemons down with dnsmasq holding the port, 106 had kea4 up and kea6 down. This makes keaLeases coverage non-deterministic across boots independently of any client, and it is the real cause of the recurring "ct 105 eth0 has no address" warning that settle_containers attributes to ifupdown. It must be resolved before any keaLeases coverage number from this lab means anything.

DONE SO FAR (committed afd90e14): allowlist retirement with a distinct refusal for a retired id, PHASE2_VMS deleted rather than emptied because bash 3.2 aborts on "${EMPTY[@]}" under set -u, assert_client_reaches_firewalls as a hard gate, and the tests updated. The gate earned its keep on its second run by refusing a raise where eth0 never got an address.

NOT DONE, and blocked on a decision rather than on work: whether to restore the lost coverage by re-homing 105 onto the tagged segments (a NIC on vmbr9 tag 40 for release DHCPv4, and a DHCPv6 client for keaLeases6) or to accept the loss and re-ledger those entries. The description's accepted-losses list cannot be used as the answer because it describes losses that did not occur and omits the ones that did.
<!-- SECTION:NOTES:END -->

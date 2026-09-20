---
id: OPN-0110
title: >-
  Consolidate the testbed to three guests and run the canary profiles
  sequentially
status: Done
assignee: []
created_date: '2026-09-20 10:43'
updated_date: '2026-09-20 18:43'
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
- [x] #1 One canary session probes the nightly profile then the release-vm profile against the same shared client, and each still files into its own issue
- [x] #2 105 holds an address on both firewalls LAN segments and the readiness gate proves it before either probe starts
- [x] #3 Guests 110, 111 and 112 are out of the power script allowlist and stay stopped, with the endpoints that lost a populated table named in the run output rather than silently reading clean
- [x] #4 The Wave operating model doc describes the three-guest lab, the sequential run and the replacement throughput pair
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
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

WHAT THE RETIRED GUESTS ACTUALLY DID, read off their definitions before they were destroyed on 2026-09-20. Rob authorised deletion; the definitions and cloud-init snippets are archived on oli at /root/backups/retired-testbed-guests-20260920-172936 (qm-config-11{0,1,2}.txt plus 11{1,2}-user-data.yaml and 11{1,2}-network-config.yaml). Everything load-bearing is repeated here because a path on one host is not a record.

SEGMENTS - this is what re-homing 105 needs and it is not guessable from the bridge names:
  release client segment = vmbr9 tag 40   (111 sat here)
  nightly client segment = vmbr9 tag 140  (112 sat here)
  TESTLAN                = vmbr9 UNTAGGED (105 eth0, and both firewalls)
110 was NOT on a tagged segment: it held a static 172.16.9.10/24 on untagged vmbr9 with gw 172.16.9.1.

EACH CLIENT HAD TWO DHCP INTERFACES ON ITS SEGMENT, NOT ONE, and the pair is the point:
  clires - MAC pinned by a Kea host RESERVATION on the firewall, so the lease comes back reserved
  clidyn - unpinned, so the lease comes back dynamic
That pair is what exercised the reserved-vs-dynamic split in keaLeases4 rows[].is_reserved - the same field OPN-0007 is about. A single re-homed interface on 105 restores only half of it, and restores the half that was never in doubt.

BOTH client interfaces carried dhcp4-overrides use-routes:false, use-dns:false, use-domains:false. 105 already has a working default route and resolver through TESTLAN, so any DHCP interface added to it MUST carry the same overrides or the new lease will steal the route and break the container's existing paths - including the tailnet path the canary reaches it by.

110's description carried a trap that exists nowhere else in the repo, worth keeping even though its guest is gone: after any quagga config change on 102, `configctl quagga restart` is REQUIRED - reconfigure alone leaves watchfrr on the old daemon list. This is a plausible explanation for the quagga endpoints reading empty even while 110 was running, which the measurement above showed they did.

112's description and tags were WRONG in the inventory: both described it as the release client on tag 40 while it was actually the nightly client on tag 140, tagged `release`. A copy-paste from 111 that survived however long. Worth knowing if any other doc or ledger entry was written from that description rather than from the config.

FINAL POSITION, measured against the original six-guest baseline (run 35521531188) by the same set-diff method:
  nightly (#727): required coverage 0 -> 0. FULLY RESTORED, identical to the six-guest lab.
  release (#726): required coverage 1 -> 4. Four ipsec paths outstanding, and keaLeases6 rows[].address is now VERIFIED where it was a miss in the baseline - better than the lab it replaced.
So of the sixteen required paths the naive consolidation lost, twelve are back, one is newly gained, and four remain.

HOW THE TWELVE CAME BACK:
- eth3 on 105 (vmbr9 tag 40) restored all six release keaLeases4 paths. It is a DHCP CLIENT, which is the thing the container was missing - it could already ROUTE to 172.16.40.0/24 over OSPF, and that is exactly why the gap was invisible until the canary named it. Pinned in /etc/dhcp/dhclient.conf to request neither routers nor DNS; verified the default route stays via 172.16.9.1 on eth0.
- A real DHCPv6 lease on eth0 restored keaLeases6. SLAAC is NOT enough: Kea only reports what it leased, and the RA-derived /64 puts no row in the table. ct_iface_address6 matches /128 only, because matching /64 would report success for precisely the failing case.
- The four nightly ipsec paths came back on their own once 105 was restarted, which is the clue that unpicked the last item.

THE IPSEC TUNNEL IS NOT WHAT THE LEDGER SAYS, and this is the correction that matters most here. coverage.json's exercise text calls for "One site-to-site IPsec tunnel between the two testbed OPNsense boxes across the isolated bridge". There is no such tunnel. swanctl on 102 shows:
    local  'devbox'   @ 172.16.9.1[4500]
    remote 'ctclient' @ 172.16.9.100[4500]  [10.97.0.1]
That is 102 <-> 105: the traffgen container is a ROAD-WARRIOR client dialling the nightly firewall. 106 has strongswan running and no SAs at all, and never had any. So the release box's ipsec coverage was never obtainable by any arrangement of client VMs, and retiring 110/111/112 did not cause it - the baseline simply happened to catch a moment when it read differently. Restoring it means giving 105 a SECOND connection profile targeting 172.16.9.2 plus a matching tunnel and PSK on 106, which is firewall-and-client VPN configuration rather than lab plumbing, and is NOT done here.

AC1 IS AMENDED, not met as written. Rob chose on 2026-09-20 to keep two sequential jobs rather than fold the profiles into one session. Each job owning a complete raise/lower cycle is the OPN-0109 design and survives one profile dying; `max-parallel: 1` already serialises them against the single physical lab, and both profiles now run against the SAME shared traffgen and still file into their own issues, which is AC1's substance. The cost is one extra boot, about three minutes.

AC3 went further than written: 110, 111 and 112 are not merely out of the allowlist and stopped, they are DESTROYED (Rob authorised deletion on 2026-09-20). Definitions and cloud-init snippets archived on oli at /root/backups/retired-testbed-guests-20260920-172936, with everything load-bearing copied into the notes above. `qm destroy --purge` on all three, exit 0; every other guest including home assistant verified untouched afterwards.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
The lab is three guests: 102, 106 and one shared traffgen. 110, 111 and 112 are destroyed, freeing 6 cores, 8.5 GB and 34.4 GB.

The consolidation as specified would have quietly cost sixteen required-coverage paths - none of them the ones the task predicted. The predicted losses (quagga BGP/OSPF/BFD, IPv6 PD) never happened, because those endpoints were already empty with the peer running. What actually broke was Kea lease and IPsec coverage, and it broke because the retired guests were DHCP clients on the tagged per-firewall segments while the surviving container sits on the untagged shared one - it could route there, but routing does not get you a lease.

Twelve of the sixteen are restored by giving the container a release-side DHCP client and a real DHCPv6 lease, and keaLeases6 on the release box now works for the first time ever, as a side effect of fixing OPN-0114. The remaining four are IPsec on the release box, which turned out never to have been obtainable: the tunnel the ledger describes as site-to-site between the firewalls is actually the traffgen dialling the nightly box as a road-warrior client, and the release box has never had an SA.
<!-- SECTION:FINAL_SUMMARY:END -->

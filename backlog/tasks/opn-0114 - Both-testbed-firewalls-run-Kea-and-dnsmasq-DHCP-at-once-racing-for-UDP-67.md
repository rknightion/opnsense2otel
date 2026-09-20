---
id: OPN-0114
title: 'Both testbed firewalls run Kea and dnsmasq DHCP at once, racing for UDP/67'
status: Done
assignee: []
created_date: '2026-09-20 17:09'
updated_date: '2026-09-20 17:58'
labels:
  - testbed
  - canary
dependencies: []
ordinal: 68000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Guests 102 and 106 each have OPNsense Kea DHCPv4, Kea DHCPv6 AND dnsmasq-with-a-DHCP-range all enabled in /conf/config.xml at the same time. They compete for UDP/67 at boot and the winner differs per box per boot, which makes several things the canary measures non-deterministic rather than wrong.

MEASURED 2026-09-20, not inferred. On one boot 102 had kea-dhcp[v4] and kea-dhcp[v6] both NOT running with dnsmasq holding :67, while 106 on the same boot had kea-dhcp[v4] running and kea-dhcp[v6] not. Config on both reads kea4_enabled=1 kea6_enabled=1 dnsmasq_enabled=1.

THREE CONSEQUENCES, all previously read as something else:
- The traffgen container's TESTLAN lease is intermittent. The 'ct 105 eth0 has no address after 90s - re-running dhclient' warning that settle_containers exists to paper over is this race, not ifupdown giving up as its comment claims. On one raise the dhclient retry did not recover it either and eth0 stayed down: DHCPDISCOVER went unanswered because the server that owned the range had lost the port.
- keaLeases4 and keaLeases6 coverage is non-deterministic across boots. A run where Kea lost the race reports those paths unverified, which is indistinguishable in the report from a client that took no lease.
- Anything downstream of a lease - ARP, NDP, the leases tables - inherits the same intermittency.

This was surfaced by OPN-0110's consolidation measurement but is NOT caused by it: the config predates the retirement of 110/111/112 and affects both boxes.

Decide one DHCP server per box and disable the other. Whichever is chosen must be the one the exporter's Kea collectors expect, or the coverage those collectors back is unverifiable by construction.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
THE DESCRIPTION'S DIAGNOSIS WAS PARTLY WRONG and is corrected here. It said Kea and dnsmasq "compete for UDP/67" as if symmetrically, and that both being enabled is itself the misconfiguration. Neither holds. They serve DIFFERENT segments on purpose - dnsmasq owns only TESTVLAN (opt2, 172.16.20.100-199) and runs its DNS on port 5335 - so having both enabled is the intended design, not the bug. There are TWO separate faults, and one of them is not a race at all.

FAULT A, both boxes, a genuine ordering race with a precise mechanism. dnsmasq's DHCP socket is ALWAYS the wildcard *:67, because DHCP must receive broadcasts. `bind-interfaces` is already set in the generated dnsmasq.conf and governs only the DNS listener, so there is NO dnsmasq setting that avoids this - do not go looking for one. Kea binds per address. On FreeBSD the two coexist when Kea binds first, and Kea fails every bind when dnsmasq is there first:
  DHCPSRV_OPEN_SOCKET_FAIL ... failed to bind fallback socket to address 172.16.9.1, port 67 ... Address already in use
  DHCP4_OPEN_SOCKETS_FAILED maximum number of open service sockets attempts: 5, has been exhausted without success

AND THE STATUS COMMAND LIES ABOUT IT. `configctl kea status` reads the PROCESS, so a kea-dhcp4 that bound nothing still reports "running as pid N". Reproduced deliberately: restart Kea while dnsmasq holds the port and you get sockets=0 alongside "kea-dhcp[v4] is running as pid 43007". Any future check here must assert on sockstat; status is worthless for this.

FIXED in 234ecd30 by ordering rather than configuration - stop dnsmasq, restart Kea into a clear port, start dnsmasq so its wildcard lands on top. Verified on a cold raise with Kea broken first: "vm 102 kea bound NO dhcp sockets", then "recovered with 3 dhcp socket(s)", and the traffgen then took 172.16.9.100/24 on its FIRST attempt. That last part is the real prize: the recurring "ct 105 eth0 has no address after 90s - re-running dhclient" warning is this fault, NOT ifupdown giving up as settle_containers' comment claims.

FAULT B, guest 106 only, deterministic and unrelated to A. kea-dhcp6 had NEVER started on the release box:
  DHCP6_PARSER_FAIL ... subnet configuration failed: The 'id' value (0) is not within expected range: (1 - 4294967294)
Both DHCPv6 subnets generated id: 0 because their config.xml entries had no <subnet_id> node at all, while the DHCPv4 subnets on the same box had 1 and 2 and worked fine. Fixed in place on 106 by adding subnet_id 1 and 2, reloading the template and restarting Kea; kea-dhcp[v6] came up for the first time. config.xml backed up on the guest at /conf/backup/config-before-kea6-subnetid.xml.

CONSEQUENCE FOR THE LEDGER, and it matters for OPN-0110: keaLeases6 on the release box was never obtainable, so any coverage entry blaming a missing client for it was wrong about the cause. Fault B had to be fixed before "re-home a DHCPv6 client onto 105" could even be a meaningful step.

STILL OPEN: fault A's repair is a remediation on every raise, not a cure. Kea still loses the boot race about as often as it wins; the raise now notices and fixes it. A real cure would be making Kea start before dnsmasq at boot, which is an rc ordering change on the guests and was not attempted.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Two distinct faults, not the single race the description assumed.

Kea and dnsmasq genuinely collide on UDP/67 - dnsmasq's DHCP socket is always the wildcard and no setting changes that - but only in one order, and `configctl kea status` reports a Kea that bound zero sockets as running, so the failure was invisible from the box. The raise now asserts on sockstat and repairs by restarting Kea ahead of dnsmasq. Separately, the release box's DHCPv6 subnets carried no subnet_id, generated id 0, and Kea had rejected them since forever; kea-dhcp6 has now started there for the first time.

The payoff beyond DHCP itself: the traffgen's TESTLAN lease now lands on the first attempt, so the long-standing "eth0 has no address after 90s" warning was this bug rather than the ifupdown behaviour it was attributed to.
<!-- SECTION:FINAL_SUMMARY:END -->

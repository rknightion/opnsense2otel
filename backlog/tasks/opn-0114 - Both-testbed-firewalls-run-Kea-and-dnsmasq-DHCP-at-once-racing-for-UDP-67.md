---
id: OPN-0114
title: 'Both testbed firewalls run Kea and dnsmasq DHCP at once, racing for UDP/67'
status: To Do
assignee: []
created_date: '2026-09-20 17:09'
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

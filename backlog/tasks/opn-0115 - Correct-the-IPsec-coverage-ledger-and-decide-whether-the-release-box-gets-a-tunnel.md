---
id: OPN-0115
title: >-
  Correct the IPsec coverage ledger and decide whether the release box gets a
  tunnel
status: To Do
assignee: []
created_date: '2026-09-20 18:43'
labels:
  - testbed
  - canary
dependencies: []
ordinal: 69000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
coverage.json's exercise text for the ipsec entries says 'One site-to-site IPsec tunnel between the two testbed OPNsense boxes across the isolated bridge'. That is FALSE and has sent at least one investigation down the wrong path. swanctl on guest 102 shows the real topology: local 'devbox' @ 172.16.9.1 to remote 'ctclient' @ 172.16.9.100 - the traffgen CONTAINER dialling the nightly firewall as a road-warrior client. Guest 106 runs strongswan with no SAs at all and has never had any.

Consequences: ipsecPhase1 rows[].install-time, ipsecSad rows[].satype, ipsecSad rows[].spi and ipsecSpd rows[].dir are unobtainable on the release profile by construction, not because a client VM was retired. Any note blaming the release box's empty ipsec tables on missing clients is wrong about the cause.

Two things to decide, and they are separable:
1. Fix the ledger text so it describes the tunnel that exists. Cheap, and stops the next reader repeating the hunt.
2. Decide whether the release box should have a tunnel at all. Giving it one means a second connection profile on 105 targeting 172.16.9.2 plus a matching tunnel and PSK on 106. That is the only route to ipsec coverage on the release profile, and it is VPN configuration rather than lab plumbing.

Note the nightly tunnel is established by the container, so it comes back when 105 restarts - which is why these paths appear to flap between runs for no reason visible in the report.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

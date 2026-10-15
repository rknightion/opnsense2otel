---
id: OPN-0137
title: >-
  Derive PPPoE (mpd5) link-edge counters and a last-link-up timestamp from ppp
  logs
status: To Do
assignee: []
created_date: '2026-10-15 17:42'
labels: []
dependencies: []
priority: high
ordinal: 90000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The ppp parser already emits a closed ppp.event vocabulary (link_up, link_down, session_established, session_closed, session_failed, reconnecting, auth_success and others), but ppp is in nonDerivedPrograms, so no metric exists. That was deferred because Link, LCP and IPCP each report one physical flap and a naive counter triple-counts. Count each physical edge once per WAN interface, and add a ppp_last_link_up_timestamp_seconds{interface} gauge modelled on the dhcp_client lease gauges. Purpose: correlate client connection resets and loss with WAN reconnects, and allow an alert on PPPoE flaps.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 One physical PPPoE flap increments exactly one link-down and one link-up edge (fixture test from captured mpd5 lines)
- [ ] #2 A last-link-up timestamp gauge per WAN interface
- [ ] #3 Metrics reference regenerated, plus a Grafana alert or panel for reconnects
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

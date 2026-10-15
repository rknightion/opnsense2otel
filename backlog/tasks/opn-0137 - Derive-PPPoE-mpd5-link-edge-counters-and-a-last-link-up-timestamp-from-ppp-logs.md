---
id: OPN-0137
title: >-
  Derive PPPoE (mpd5) link-edge counters and a last-link-up timestamp from ppp
  logs
status: Done
assignee:
  - '@codex'
created_date: '2026-10-15 17:42'
updated_date: '2026-10-15 18:07'
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
- [x] #1 One physical PPPoE flap increments exactly one link-down and one link-up edge (fixture test from captured mpd5 lines)
- [x] #2 A last-link-up timestamp gauge per WAN interface
- [x] #3 Metrics reference regenerated, plus a Grafana alert or panel for reconnects
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Inspect source contracts and live evidence where required; implement bounded telemetry with fixtures, regenerate shared artifacts, review and run just check before landing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented bounded link counters and last-up timestamp using only mpd bundle IFACE Up/Down events, excluding Link/LCP/IPCP and inet/inet6 rc-script copies. Captured receiver fixture reproduced zero observations at base and exactly one down/up on candidate; collector snapshots preserve timestamp across scrapes. Added WAN dashboard edge rate and time-since-up panels.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shipped IFACE-only PPPoE edge counters and last-up gauge, reconnect panels and generated reference. Captured receiver flap proves exactly one down/up despite Link/LCP/IPCP and rc-script copies; store snapshots and caps tested. just gen and just check passed; CodeRabbit wording finding fixed.
<!-- SECTION:FINAL_SUMMARY:END -->

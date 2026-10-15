---
id: OPN-0138
title: Count gateway state-kill events (gateway_states_killed)
status: Done
assignee:
  - '@codex'
created_date: '2026-10-15 17:42'
updated_date: '2026-10-15 18:07'
labels: []
dependencies: []
priority: high
ordinal: 91000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
'ROUTING: killing states for unreachable gateway' is already parsed as the log event gateway_states_killed, but it arrives under the opnsense catch-all program, which is in nonDerivedPrograms, so nothing counts it. Derive a counter keyed strictly on that event, never on other lines from the catch-all program, labelled by gateway. It shows when pf flushed states on a gateway-down edge, which is a direct cause of mid-stream resets for every LAN client. It pairs with gateways_monitor_killstates and the dpinger alarm counters.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Counter increments once per kill event per gateway, from a captured line fixture
- [x] #2 No other catch-all program line is counted
- [x] #3 Metrics reference regenerated
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
Implemented bounded gateway state-kill event counter only for syslog.event=gateway_states_killed with a gateway name. Captured receiver fixture proves one flush observation and no increment for unrelated catch-all routing messages; added state-kill rate panel.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shipped bounded per-gateway flush-event counter and dashboard panel. Captured receiver fixture counts the kill event and excludes unrelated catch-all lines. Generated metrics reference updated. just gen and just check passed; review complete.
<!-- SECTION:FINAL_SUMMARY:END -->

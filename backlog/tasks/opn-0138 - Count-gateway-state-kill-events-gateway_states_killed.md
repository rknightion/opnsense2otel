---
id: OPN-0138
title: Count gateway state-kill events (gateway_states_killed)
status: To Do
assignee: []
created_date: '2026-10-15 17:42'
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
- [ ] #1 Counter increments once per kill event per gateway, from a captured line fixture
- [ ] #2 No other catch-all program line is counted
- [ ] #3 Metrics reference regenerated
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

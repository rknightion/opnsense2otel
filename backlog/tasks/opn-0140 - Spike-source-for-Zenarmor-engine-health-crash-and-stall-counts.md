---
id: OPN-0140
title: 'Spike: source for Zenarmor engine health (crash and stall counts)'
status: To Do
assignee: []
created_date: '2026-10-15 17:42'
labels: []
dependencies: []
priority: medium
ordinal: 93000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Zenarmor's check_health.sh reports crash_count and stall_count, but it is an on-box script, not a REST endpoint. Find out whether the engine logs crashes and stalls to syslog (then derive a counter from the existing receivers) or whether the values are only available on the box (then add them to the OPN-0136 textfile script). Value: engine stalls are the leading suspect for netmap ring-full drops.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Source surface identified with evidence
- [ ] #2 Follow-up implementation task filed, or the reason it is not worth it recorded
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

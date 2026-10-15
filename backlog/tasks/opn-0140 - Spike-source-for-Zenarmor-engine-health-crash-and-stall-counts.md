---
id: OPN-0140
title: 'Spike: source for Zenarmor engine health (crash and stall counts)'
status: Done
assignee:
  - '@codex'
created_date: '2026-10-15 17:42'
updated_date: '2026-10-15 18:07'
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
- [x] #1 Source surface identified with evidence
- [x] #2 Follow-up implementation task filed, or the reason it is not worth it recorded
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
Source verified read-only on 2026-10-15: installed check_health.sh lines 685-692 scans current-day main logs; current SystemUtils::getEngineCrashStallCount at lines 312-333 scans active and archive main_* and filters local Y-m-d. Both count heartbeat for / terminated with signal. HealthUtils embeds these as engine crash_count/stall_count. HealthController is loopback-only and calls check()->send(), not a read-only metrics API; Logger writes local files rather than syslog. No matching current-day engine lines were found. Do not run check_health.sh for collection: it can stop/disable services. Follow-up uses daily gauges and bounded read-only log scanning.

Follow-up filed as OPN-0141 (daily Zenarmor engine crash/stall gauges through the textfile companion), dependent on OPN-0136 (physical NIC textfile companion).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Identified installed local engine-log source and daily reset/retention semantics. Health API is check-and-send, not a safe polling endpoint; shell health check can stop services. Filed OPN-0141 (daily Zenarmor crash/stall textfile gauges) as required implementation follow-up. Durable source findings documented; just check passed.
<!-- SECTION:FINAL_SUMMARY:END -->

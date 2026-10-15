---
id: OPN-0141
title: >-
  Expose Zenarmor daily engine crash and stall counts through the textfile
  companion
status: To Do
assignee: []
created_date: '2026-10-15 17:51'
labels: []
dependencies:
  - OPN-0136
priority: medium
ordinal: 94000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OPN-0140 (Zenarmor engine health source spike) verified installed SystemUtils::getEngineCrashStallCount reads main_* logs in active and archive, counts heartbeat-for and terminated-with-signal lines containing the local calendar date. These are daily retained-log gauges, not lifetime counters. HealthController calls check()->send() and is loopback-only; it is not a safe polling endpoint. Logger writes local files. Add read-only bounded file scanning to the textfile companion without invoking the health script, changing engine state, or inferring all stalls from a health-check execution.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Daily crash and stall gauges match the installed health implementation across rotation and midnight fixtures, with missing logs distinguished from zero
- [ ] #2 Read size and file count are bounded; no log contents, paths, identities or arbitrary values become metric labels
- [ ] #3 Document freshness, retention and calendar-day semantics and prove output on the supported FreeBSD runtime
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

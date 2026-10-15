---
id: OPN-0141
title: >-
  Expose Zenarmor daily engine crash and stall counts through the textfile
  companion
status: Done
assignee:
  - '@codex'
created_date: '2026-10-15 17:51'
updated_date: '2026-10-15 18:45'
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
- [x] #1 Daily crash and stall gauges match the installed health implementation across rotation and midnight fixtures, with missing logs distinguished from zero
- [x] #2 Read size and file count are bounded; no log contents, paths, identities or arbitrary values become metric labels
- [x] #3 Document freshness, retention and calendar-day semantics and prove output on the supported FreeBSD runtime
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Ship a separate opt-in Zenarmor textfile command with bounded read-only daily log scanning and explicit collection health. Prove midnight, rotation, missing-source and publication behavior with fixtures and FreeBSD execution. Document installation/configd cron, separate node_exporter scraping, queries, freshness, limits and removal; run CodeRabbit and just check before landing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented separate opt-in Python companion and six fixture tests. Failures publish collection_success=0 and omit daily counts; publication errors retain old output. Both directories required; compressed/special/symlink sources fail closed. Defaults 128 files, 64 MiB, 4096 entries, 1 MiB lines, 10 seconds. Hardlinks deduplicated. Final script ran on deployed FreeBSD with success=1 and daily counts matching an independent read of the five retained logs. No persistent installation or cron/scrape mutation performed. User guide covers installation, configd cron, separate scrape setup, gauge queries, freshness, limits, upgrades and disable; independent documentation review passed after help-command correction.

CodeRabbit completed with one major portability finding: scandir may share directory offsets through duplicated descriptors. Fixed inventory to open a fresh descriptor relative to each pinned directory for every pass. Textfile tests and repeated final FreeBSD execution pass. Independent documentation review found only a help-command path correction, applied. No generated artifacts changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shipped the separate opt-in Zenarmor textfile companion with bounded stable daily log scanning, explicit collection health and safe atomic publication. Six fixture tests cover daily/rotation/midnight, missing and compressed inputs, limits, special files and publication. Final FreeBSD command matched direct retained-log counts. Complete installation/configd cron/scrape/query/freshness/upgrade/disable guide linked from integration docs. just check passed; CodeRabbit initial finding fixed and delta review completed with zero findings. No generated artifact changes, so just gen not required. Persistent on-box installation remains an operator opt-in.
<!-- SECTION:FINAL_SUMMARY:END -->

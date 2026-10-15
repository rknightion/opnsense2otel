---
id: OPN-0139
title: Keep per-queue interrupt and iflib thread identity in the activity collector
status: Done
assignee:
  - '@codex'
created_date: '2026-10-15 17:42'
updated_date: '2026-10-15 18:07'
labels: []
dependencies: []
priority: medium
ordinal: 92000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
activity_command_cpu_percent strips the {thread-name} suffix, so intr{irq...: <nic>:rxqN}, kernel{if_io_tqg_N} and eastpect worker threads collapse into one command. Those threads show per-RX-queue and per-CPU packet-processing load without SSH, including PPPoE traffic landing on a single RX queue and the Zenarmor worker's CPU. Start with a spike: capture one live get_activity response and confirm the thread names exist and are stable. Then keep identity for an allowlist of intr/iflib/eastpect patterns behind the existing cap or an opt-in flag.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Spike result recorded: thread names present and stable, or not
- [x] #2 If present: allowlisted threads exported with bounded cardinality; everything else aggregates as today
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
Two samples confirmed stable if_io_tqg_0..11 and named eastpect threads: configctl system diag activity json and the authenticated REST get_activity endpoint. NIC rxq interrupt threads were not observed on this host; their allowlisted support must be fixture-proven, not claimed live. REST reached through localhost on existing SSH transport for inspection only; exporter remains REST-only.

Preserves allowlisted iflib, NIC rxq and Eastpect CPU/thread identity with IRQ numbers removed; process memory stays deduplicated under original process command. Shared 128-label budget plus overflow applies across CPU/thread/memory maps. API-path red/green fixture, cap and fallback mutation checks passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Live REST/configd samples confirmed stable iflib and Eastpect thread names. Allowlisted CPU/thread labels retain identity within the shared cap, while process memory stays deduplicated. API fixture, cap/fallback tests and race suite passed. NIC rxq grammar fixture-proven, not observed live. just gen and just check passed.
<!-- SECTION:FINAL_SUMMARY:END -->

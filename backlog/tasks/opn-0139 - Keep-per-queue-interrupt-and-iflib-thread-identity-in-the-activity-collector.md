---
id: OPN-0139
title: Keep per-queue interrupt and iflib thread identity in the activity collector
status: To Do
assignee: []
created_date: '2026-10-15 17:42'
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
- [ ] #1 Spike result recorded: thread names present and stable, or not
- [ ] #2 If present: allowlisted threads exported with bounded cardinality; everything else aggregates as today
- [ ] #3 Metrics reference regenerated
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

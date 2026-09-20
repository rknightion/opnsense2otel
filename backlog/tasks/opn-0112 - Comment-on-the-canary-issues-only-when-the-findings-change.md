---
id: OPN-0112
title: Comment on the canary issues only when the findings change
status: To Do
assignee: []
created_date: '2026-09-20 10:43'
updated_date: '2026-09-20 10:59'
labels:
  - canary
  - ci
dependencies:
  - OPN-0107
priority: low
type: chore
ordinal: 66000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
live-canary.yml appends a full report to issues 727 and 726 on every run whether or not anything moved. Ten consecutive comments on each from 2026-09-06 to 2026-09-15 were byte-identical clean reports, and camden managed 27 on issue 693. A reader cannot tell a fresh clean run from a re-post, and real drift would land in a thread nobody reads any more - OPN-0096 was raised because standing noise had already made findings invisible once. The issues stay open and stay the workflow output channel per the Wave operating model; only the posting rule changes. A silent workflow is its own failure mode, so absence of a comment must not be the only signal that the canary still runs: a periodic heartbeat covers the case where the run stopped happening at all. The generation stamp from OPN-0107 is part of the comparison - a clean run against a newly updated box is new information even when the findings are identical.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A run that differs in findings or in generation posts a full report
- [ ] #2 A run whose findings and generation match the most recent full report comment posts no new comment, and heartbeat comments are never the baseline for that comparison
- [ ] #3 A heartbeat comment appears at a fixed interval even during an unbroken clean streak, so a canary that stopped running is distinguishable from one finding nothing
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CodeRabbit pre-commit review of the task text (2026-09-20) found the AC1 baseline underspecified: if the heartbeat is itself a comment, comparing a run against "the last comment" compares it against a heartbeat and re-posts a full report every time one lands. The baseline is the most recent FULL REPORT comment, and heartbeats are excluded from it. AC1 and AC3 are edited to say so.
<!-- SECTION:NOTES:END -->

---
id: OPN-0125
title: Keep OTLP log delivery through a WAN outage longer than the retry budget
status: In Progress
assignee:
  - '@codex'
created_date: '2026-10-12 19:08'
updated_date: '2026-10-13 12:22'
labels:
  - logship
  - otlp
dependencies: []
priority: low
type: enhancement
ordinal: 78000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When the production firewall rebooted on 2026-10-12 the exporter host lost its outbound route for about 8 minutes. The OTLP log sink retries a batch for roughly 2 minutes (10 attempts) and then drops it so later batches can flow; three batches (about 900 records) were dropped and OPNsenseLogShipCountedLoss fired. A firewall reboot is a routine event for this exporter's own upstream, so loss on every upgrade is a design gap. Options include a longer bounded retry budget, a bounded on-disk spool, or holding the queue while the endpoint is unreachable; the choice needs a decision on memory/disk bounds.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The chosen approach and its memory/disk bound are recorded in the task notes before implementation
- [ ] #2 A test simulating an unreachable endpoint for longer than today's retry budget but within the new bound delivers every record once the endpoint returns
- [ ] #3 Beyond the bound, loss is still counted and the existing loss alert still fires
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-10-13 owner authorized direct work on remaining follow-ups. First priority is WAN-outage resilience; buffering bounds and approach will be selected from current-path evidence before implementation.
<!-- SECTION:NOTES:END -->

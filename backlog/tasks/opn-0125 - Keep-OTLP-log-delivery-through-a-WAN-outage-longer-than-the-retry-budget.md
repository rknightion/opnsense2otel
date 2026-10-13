---
id: OPN-0125
title: Keep OTLP log delivery through a WAN outage longer than the retry budget
status: In Progress
assignee:
  - '@codex'
created_date: '2026-10-12 19:08'
updated_date: '2026-10-13 13:40'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Implement monotonic retryable-batch retention from first attempt, never reset by partial success; configure15m default and positive attempt-cap compatibility. Prove failure-first outage recovery beyond ten attempts, bound exhaustion, capacity overflow, partial ack and shutdown through real pipeline/local HTTP where practical. Document capacity and at-least-once transport limitations; regenerate config consumers; security review, CodeRabbit, just check and exact-SHA CI.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-10-13 owner authorized direct work on remaining follow-ups. First priority is WAN-outage resilience; buffering bounds and approach will be selected from current-path evidence before implementation.

2026-10-13 Rob selected 15-minute bounded memory retention. Keep existing queue128MiB estimated bytes/65,536 records plus bounded in-flight batch; no restart durability. tmpfs/local-spool idea retained as future design option, not part of this implementation. Elapsed retention becomes default ceiling; default attempts0 means elapsed-bound only, explicitly positive attempt count remains stricter. Permanent rejections, partial-ack removal, queue overflow accounting and prompt shutdown preserved.

Implemented15m monotonic elapsed retention, default attempts0 with explicit positive cap stricter, fixed deadline across partial acknowledgements, existing loss reasons. Root caught and corrected shutdown drain regression with real HTTP red/green tests: active delivery cancels, final queued records drain under stop caller deadline without retries. Package/race tests and independent security review pass. Built CLI config-check accepts15m and rejects0/negative. Generated config consumers updated; final gate/CodeRabbit underway.

Alert compatibility proof: existing LogShippingAlertTest and ClassificationCompletenessTest six tests passed. Shipped counted-loss manifest aggregates increase(logs_dropped_total[15m]) by instance/source/reason with no reason filter and zero pending delay. Existing rule evaluator transitions Normal->Alerting for ship_failed_permanent and overflow positive query results. Combined with counter tests, local metric/rule contract proven; no live page/notification claim.

Final full just check passed; security review passed. CodeRabbit full candidate and subsequent workflow-only corrections completed; no remaining major/critical findings. Existing zero-attempt test renamed to reflect approved elapsed ceiling, assertions preserved. Runtime memory policy is ready for publication and deployment readback.
<!-- SECTION:NOTES:END -->

---
id: OPN-0129
title: Design native histogram conversion for pre-bucketed flow and Unbound data
status: To Do
assignee: []
created_date: '2026-10-13 08:37'
labels: []
dependencies: []
priority: high
type: spike
ordinal: 82000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OPN-0118 (emit native exponential histograms) remains parked because the OTLP job still includes classical flow delta-ratio and Unbound recursion histograms. On 2026-10-13 Rob chose a separate conversion design before closeout rather than narrowing the all-job zero-bucket criteria. This task produces a reviewed design and implementation packet, not a runtime conversion.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The design traces both real producer-to-OTLP paths and all dashboard, alert, scrape and reset/temporality consumers at a named source revision.
- [ ] #2 The design states what information the pre-bucketed inputs retain or lose, whether a faithful exponential representation is feasible, and any approximation, error bound, sum/count, overflow and reset semantics; no synthetic raw observations or invented precision.
- [ ] #3 A frozen implementation packet names exact owned files, dependency order, compatibility decisions, discriminating fail/pass tests through the real bridge and live OTLP acceptance queries; infeasibility or a required owner tradeoff is an explicit supported result.
- [ ] #4 An independent review challenges numerical correctness and consumer compatibility, with findings resolved or owner decisions listed; OPN-0118 criteria and runtime behavior remain unchanged.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

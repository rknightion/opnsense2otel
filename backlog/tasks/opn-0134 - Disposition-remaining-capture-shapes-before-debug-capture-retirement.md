---
id: OPN-0134
title: Disposition remaining capture shapes before debug-capture retirement
status: In Progress
assignee:
  - '@codex'
created_date: '2026-10-13 12:27'
updated_date: '2026-10-13 12:43'
labels: []
dependencies: []
priority: medium
type: chore
ordinal: 87000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The complete private capture audit found 82 noncontrol syslog records still unknown after the reboot-shape fix. Retirement cannot establish a quiet baseline until these records have evidence-backed dispositions. Raw captures remain private; fixtures must be synthetic.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every noncontrol unknown record in the retained complete snapshot has an evidence-backed parser, narrow pass-through, or explicit retained-unknown disposition.
- [ ] #2 Any added grammar has synthetic positive and nearby negative proof; full private ingress replay preserves records and captures a new unknown control.
- [ ] #3 A verified rollout and fresh observation baseline are recorded without retiring capture or waiving the quiet-week requirement.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Review complete private message grammars and synthetic replacements; independently review the bounded disposition packet, implement program-scoped rules with failing positive/negative regressions, replay the complete corpus through ingress with independent unknown controls, run security review and CodeRabbit plus final gate. Root owns rollout and fresh baseline; keep retirement waiting for real quiet-week evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation adds 60 complete program-scoped rules with synthetic red/green tests. Private ingress red/green proof ships572 records:274 parsed297 known1 retained synthetic unknown; separate unknown control also ships/captures, zero ordinary corpus capture. Syslog race tests, independent security review and full just check passed. CodeRabbit and publication/live baseline remain pending.
<!-- SECTION:NOTES:END -->

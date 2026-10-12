---
id: OPN-0126
title: Isolate composed loop gates from machine-local lane worktrees
status: Done
assignee:
  - '@loop10'
created_date: '2026-10-12 20:00'
updated_date: '2026-10-12 20:56'
labels: []
dependencies: []
type: chore
ordinal: 79000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The composed full gate scanned ignored nested lane worktrees and their vendor/scratch Go files, failing formatting before checking the integrated candidate. This is a root orchestration-layout defect, not a source regression or permission to weaken the repository gate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The unchanged integrated source SHA is checked with the exact full gate in a clean detached worktree outside the main checkout.
- [x] #2 The result names SHA/tree, gate exit and preserved evidence; future loop gate placement avoids nested ignored sources without editing the gate.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Run the exact composed gate on the firmware landing in an external clean worktree; serialize the shared lint lock, record evidence and use external clean composed worktrees for later landings.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop10: orchestration repair, no production implementation attempts. Exact unchanged integrated firmware SHA gated successfully in external clean worktree; later root merged source landing and dependency updates also passed exact composed gate on the published SHA. Root will continue external composed gates and archive terminal machine-local candidates outside the main checkout before final gate. No repository gate/tool/test altered, no generated source change for this task; CodeRabbit skipped for tracker/evidence-only repair.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Fixed root gate layout: ignored nested worktrees polluted formatting scan. External clean worktree proved exact integrated source with the unchanged full gate and stable HEAD/tree/index/status; subsequent composed gate also passed.
<!-- SECTION:FINAL_SUMMARY:END -->

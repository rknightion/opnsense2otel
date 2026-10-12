---
id: OPN-0126
title: Isolate composed loop gates from machine-local lane worktrees
status: In Progress
assignee:
  - '@loop10'
created_date: '2026-10-12 20:00'
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
- [ ] #1 The unchanged integrated source SHA is checked with the exact full gate in a clean detached worktree outside the main checkout.
- [ ] #2 The result names SHA/tree, gate exit and preserved evidence; future loop gate placement avoids nested ignored sources without editing the gate.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Run the exact composed gate on the firmware landing in an external clean worktree; serialize the shared lint lock, record evidence and use external clean composed worktrees for later landings.
<!-- SECTION:PLAN:END -->

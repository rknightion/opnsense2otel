---
id: OPN-0135
title: Publish verified 5.2.0 release after resilience and live-contract proof
status: In Progress
assignee:
  - '@codex'
created_date: '2026-10-13 13:16'
labels: []
dependencies: []
priority: medium
type: chore
ordinal: 88000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner approved publishing the existing release-please candidate after WAN retention and on-demand live-contract changes pass final proof. Histogram conversion and capture retirement remain excluded.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Final release PR includes the approved landed fixes and required checks pass at its exact head.
- [ ] #2 Release is published through the existing release-please flow and expected binaries, checksums and container artifacts are verified.
- [ ] #3 Resulting deployed revision and release outcome are verified with remaining limitations recorded.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Finish approved source work and exact-SHA CI/live proof; re-read release PR head and diff; merge only verified candidate; watch release publication and verify artifacts plus deployed revision without changing credentials.
<!-- SECTION:PLAN:END -->

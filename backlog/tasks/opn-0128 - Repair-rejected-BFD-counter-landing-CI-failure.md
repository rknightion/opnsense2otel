---
id: OPN-0128
title: Repair rejected BFD counter landing CI failure
status: In Progress
assignee:
  - '@loop10'
created_date: '2026-10-12 21:24'
updated_date: '2026-10-12 21:26'
labels: []
dependencies: []
priority: high
type: bug
ordinal: 81000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The rejected BFD counter landing passed local source and guarded review, but its exact published CI run failed. Main must not close red or unexplained. Root is retrieving the failed job and assertion before choosing a source fix or authorised infrastructure retry; workflows, tool pins and gate weakening remain prohibited.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The exact failing job and cause are evidenced and classified.
- [ ] #2 A permitted repair or justified infrastructure retry yields ci-success on the owning exact SHA, without weakening checks or reverting foreign work.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Read exact-SHA failed CI evidence; choose smallest authorised source repair or infrastructure retry on new evidence; retain full proof and do not cancel publication writes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop10: no implementation attempt; exact CI failure is module ZIP transport INTERNAL_ERROR while installing existing GoReleaser pin. All other required jobs passed, no BFD assertion failed. Root initiated one authorised same-run failed-job infrastructure retry; no workflow/tool/gate/source edit and no Grafana rerun/cancellation. Dashboard live verifier matched files; rule prune no-op and push completed. Exact retry conclusion pending.
<!-- SECTION:NOTES:END -->

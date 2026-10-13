---
id: OPN-0133
title: Preserve native histogram samples in duration heatmaps
status: Done
assignee:
  - '@codex'
created_date: '2026-10-13 11:09'
updated_date: '2026-10-13 11:23'
labels: []
dependencies: []
priority: medium
type: bug
ordinal: 86000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two duration heatmaps accept native samples but their default outer max wrapper discards histogram values. Owner approved correcting the existing consumers without changing metric semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Both generated duration heatmap queries retain native histogram samples and retain classic precedence when both representations exist.
- [x] #2 Meaningful query execution proof fails on the old query and passes on the new query; generated dashboards remain instance-scoped.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Reproduce native samples being dropped by actual generated PromQL; opt the two heatmaps out of float-only deduplication; prove native/classic/dual behavior and regenerate dashboards.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Real promtool 3.15.0 evaluation of builder-produced expressions failed before fix (native-only results nil) and passed six cases after fix: two panels times native/classic/dual, instance filtering and classic precedence checked. Existing visualization suite passed 5 tests. Scratch evidence retained locally; no permanent dependency introduced. No live browser rendering claimed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed float-only deduplication from two already-aggregated duration heatmap expressions; regenerated health dashboard. Real promtool execution first failed, then all six native/classic/dual cases passed with instance scoping and classic precedence. Integrated just check passed; CodeRabbit zero issues. Live rendering not claimed.
<!-- SECTION:FINAL_SUMMARY:END -->

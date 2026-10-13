---
id: OPN-0132
title: Infer const histogram types in the generated metric catalogue
status: Done
assignee:
  - '@codex'
created_date: '2026-10-13 11:09'
updated_date: '2026-10-13 11:23'
labels: []
dependencies: []
priority: medium
type: bug
ordinal: 85000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The flow delta-ratio and Unbound recursion histogram declarations are classified as Gauge in generated documentation. Owner approved source-based type inference correction.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Both histogram families are classified as Histogram from their actual emission calls, without name-based exceptions.
- [x] #2 Regression proof fails before the fix and the real documentation generator produces correct catalogue types and consistent totals.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Reproduce emitted const histograms being inferred as Gauge; fix emission-based inference and type totals; exercise focused tests and root-run docgen.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Observed regression assertions fail for both real families and both constructor forms before implementation. go test ./scripts/docgen -count=1 -timeout=120s passed. Actual generator now reports 1048 metrics: 695 gauges, 350 counters, 3 histograms, and both affected families are Histogram.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added emission-based const histogram inference and separate histogram totals. Regression first failed for real families and both constructors, then package tests and actual generator passed. Integrated just check passed; CodeRabbit completed with zero issues.
<!-- SECTION:FINAL_SUMMARY:END -->

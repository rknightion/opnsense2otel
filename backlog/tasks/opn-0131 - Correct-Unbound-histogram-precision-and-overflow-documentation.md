---
id: OPN-0131
title: Correct Unbound histogram precision and overflow documentation
status: Done
assignee:
  - '@codex'
created_date: '2026-10-13 11:09'
updated_date: '2026-10-13 11:23'
labels: []
dependencies: []
priority: medium
type: docs
ordinal: 84000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Current source/help claims exact or half-microsecond sum reconstruction and no overflow, contradicted by the reviewed upstream evidence. Owner approved correction without runtime changes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Source comments and public help describe truncation, reconstruction and tail censoring accurately with source evidence.
- [x] #2 Generated consumers are regenerated and no metric values, names, labels or histogram behavior change.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Use study evidence to correct only source comments/help; root regenerates consumers, reviews diff and runs final gate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Corrected source comments and exported HELP for two-term truncation (<2us per observation before float error), tail censoring and reset semantics. just gen passed; only metrics catalogue and health dashboard regenerated across combined work. Runtime computations unchanged.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Corrected Unbound source comments and metric HELP for two truncations, censored tail and statistics resets. Regenerated catalogue; runtime computations unchanged. Independent review and integrated just check passed.
<!-- SECTION:FINAL_SUMMARY:END -->

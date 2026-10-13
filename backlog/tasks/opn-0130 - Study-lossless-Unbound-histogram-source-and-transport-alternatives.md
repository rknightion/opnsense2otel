---
id: OPN-0130
title: Study lossless Unbound histogram source and transport alternatives
status: Done
assignee:
  - '@codex'
created_date: '2026-10-13 11:09'
updated_date: '2026-10-13 11:23'
labels: []
dependencies: []
priority: medium
type: spike
ordinal: 83000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The reviewed histogram design demonstrates that the current aggregate cannot faithfully reconstruct exponential populations. Owner approved a bounded source and transport study; runtime conversion and acceptance narrowing are excluded.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Supported stable OPNsense and bundled Unbound source revisions are identified with immutable citations and measurement/precision/tail/reset evidence.
- [x] #2 Source-observation and transport/storage alternatives are assessed against no-loss, base2 OTLP and all-job zero-bucket requirements; capability gaps remain explicit.
- [x] #3 A reviewed recommendation and exact next implementation prerequisites are recorded without changing runtime behavior or parent acceptance.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Verify supported stable package/source identities and precision/tail/reset behavior; compare observation and transport paths against unchanged requirements; write cited reference and obtain independent review. No runtime conversion.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-10-13 owner chose direct work on main, not loop12. Study pins supported stable 26.7.6 to Unbound 1.26.1; recommends source-side mesh-reply aggregation plus explicit reset epoch. Independent review passed after qualifying NHCB storage possibility as unverified. No runtime conversion, tenant proof or parent acceptance change.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed supported-release source study with immutable citations and independent numerical review. Source-side aggregation with an explicit epoch is the prospective lossless base2 route; custom-bound transport does not satisfy base2 OTLP. Runtime implementation and parent acceptance remain unchanged.
<!-- SECTION:FINAL_SUMMARY:END -->

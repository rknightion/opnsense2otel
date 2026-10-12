---
id: OPN-0123
title: >-
  Tolerate transitional CARP and NDP response shapes seen during a firewall
  upgrade
status: Done
assignee:
  - '@loop10'
created_date: '2026-10-12 19:08'
updated_date: '2026-10-12 21:19'
labels:
  - carp
  - ndp
  - api-client
dependencies: []
priority: medium
type: bug
ordinal: 76000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
During the 2026-10-12 production upgrade to 26.7.6 the box answered api/diagnostics/interface/get_vip_status with {"total":0,"rowCount":0,"current":1,"rows":[],"carp":[]} - carp as an empty array where the decoder expects an object (carpInfo in opnsense/carp.go), failing the whole fetch with an unmarshal error - and answered api/diagnostics/interface/get_ndp with HTTP 200 and an empty body (unexpected end of JSON input). Each happened once mid-upgrade, but both recur on any reboot or service restart window.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A carp value of [] decodes as 'no CARP info' without failing the fetch, pinned by a fixture taken from the observed body shape; a non-empty object still decodes as before
- [x] #2 An empty 200 body from get_ndp is reported as a transient fetch failure (counted, no panic, no fabricated zero series) rather than a JSON decode error, pinned by a test
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Implement transitional CARP/NDP decoding within owned files; fail reproductions before fixes; staged candidate after local gate for root security review, CodeRabbit and publication.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop10: pre-mutation ownership gap, 0 implementation attempts. Empty HTTP200 NDP classification must precede shared client observer/cache. Root amended ownership for an endpoint-specific client.go check and serializes this lane after firmware landing to avoid shared-file ownership. Observer behavior tests remain in ndp_test.go.

loop10: 4 implementation cycles, final permitted candidate passed full gate. CARP restart [] decodes disabled/no-info while ordinary objects retain behavior; empty successful NDP body returns a transient failure before request-result observation/cache/publication, with actual failed-observer/no-cache proof. Named CARP object descent preserves introspection. Guarded review caught and closed a major false-pass from unnecessary Heterogeneous; strict canonical object schema already tolerates PHP empty arrays and retains invalid-container and new-key checks. New registered-validator regression failed before correction then passed. Final security PASS and completed CodeRabbit zero findings across all six final paths. Root integration, exact-SHA CI and auto-deploy readback remain pending; not Done yet.

loop10: 4 implementation cycles, completed at ceiling without extra attempts. Published source passed exact integrated gate and CI; auto-deployed build observed, CARP/NDP success both 1 after deploy. Raw index metadata changed during composed gate but exact tracked tree/cached diff unchanged and independently verified. No test/baseline weakened; major canary false-pass corrected before landing.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Tolerates observed CARP empty-array restart body and classifies empty successful NDP responses as counted transient failures before cache/publication. Named schema descent retains strict container/field drift and unknown-key discovery; actual-validator red/green proof closes the major review finding. Guarded reviews, CodeRabbit and exact source/composed/CI checks passed; live build and collector health observed.
<!-- SECTION:FINAL_SUMMARY:END -->

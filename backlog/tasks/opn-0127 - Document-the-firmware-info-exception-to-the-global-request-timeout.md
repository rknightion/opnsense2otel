---
id: OPN-0127
title: Document the firmware info exception to the global request timeout
status: Done
assignee:
  - loop11-root
created_date: '2026-10-12 20:00'
updated_date: '2026-10-13 10:00'
labels: []
dependencies: []
priority: low
type: docs
ordinal: 80000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The firmware info path now applies a request-local 60s timeout floor, but generic timeout/retry help still implies every endpoint uses the global timeout times the retry count. Guarded review identified the documentation mismatch; runtime caller deadlines remain authoritative.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Generic timeout/retry help explains the firmware info floor and caller deadline precedence; generated references are regenerated without changing runtime defaults.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Compare public help with firmware request semantics, change prose only in an isolated candidate, regenerate with just docs and final just gen, exercise built CLI help, security-review and run G, then root publish and verify exact-SHA composed G and CI.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop11: attempt 1 produced a six-file text-only candidate with G and generated/help proof green. Security review rejected the remaining elapsed-sequence bound: retry backoff and concurrency-slot wait are outside HTTP timeout allowances. Correct the wording and regenerated consumers before publication; runtime unchanged.

loop11: attempt 2; source-backed elapsed-bound wording corrected. Security delta review passes exact uncommitted tree 918f4310a79c8a4cdf688db83711bc4296346975, full patch SHA256 aea7cd45cc3ef90c52acf67fb5b5f1f4dce1856e743c4b3a667faf7cf2a7322b. Built CLI help, just docs, final just gen and exact just check passed. Six consumers are help source, troubleshooting, configuration, deployment reference, env example and Helm comments; no runtime/default/schema/dashboard/rule changes. CodeRabbit and new tests skipped for text-only change. Root acceptance granted; publication, composed G and exact-SHA CI pending.

loop11: 2 implementation attempts, 1 review-repair round; Done after reviewed source/documentation land fadd6585958b7018180399696089777c9a0b7e3a, exact-SHA composed G exit 0 and required CI run 38375900755 / ci-success job 115226799562 success. No runtime behavior change; CodeRabbit/new tests skipped text-only. No remaining documentation blocker.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Clarified the firmware request-local 60s floor, preservation of longer configured timeouts and caller deadline precedence across six public consumers. HTTP allowance is no longer described as an elapsed sequence bound; backoff and queue waits explicitly excluded. Built CLI help, just docs, final just gen and exact G observed; security delta review PASS. Land fadd6585958b7018180399696089777c9a0b7e3a and composed G / required CI 38375900755 prove the delivered change.
<!-- SECTION:FINAL_SUMMARY:END -->

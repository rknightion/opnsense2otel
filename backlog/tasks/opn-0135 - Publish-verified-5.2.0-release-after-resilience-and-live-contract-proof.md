---
id: OPN-0135
title: Publish verified 5.2.0 release after resilience and live-contract proof
status: Done
assignee:
  - '@codex'
created_date: '2026-10-13 13:16'
updated_date: '2026-10-13 14:06'
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
- [x] #1 Final release PR includes the approved landed fixes and required checks pass at its exact head.
- [x] #2 Release is published through the existing release-please flow and expected binaries, checksums and container artifacts are verified.
- [x] #3 Resulting deployed revision and release outcome are verified with remaining limitations recorded.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Finish approved source work and exact-SHA CI/live proof; re-read release PR head and diff; merge only verified candidate; watch release publication and verify artifacts plus deployed revision without changing credentials.
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Published v5.2.0 through approved release PR832, exact reviewed head614c4da6c426331aedb76a13f7b09ac89bc40046 and merge2b405722571cb16f147514c6424e6f22cc0bfe0c. Source CI38406976676 and new dual-profile live suite38406996344 passed; release-head CI38407092356 and merge CI38409061470 passed. Release workflow38409062567 succeeded with mandatory asset verification:25 assets, binaries/checksums/provenance/signature, Helm, notices and SBOMs. Darwin arm64 archive checksum verified and binary reports5.2.0. Container index sha256:18bf24d887e713d438fa7e13912de786cb96fff932cbe8ad42fd41446b836b29 has amd64/arm64. Camden observed release revision2b405722,version5.2.0,healthy,readyHTTP200; startup policy15m,attempts0,65536 records/128MiB. Capture retirement remains due after quiet-week evidence; histogram conversion parked.
<!-- SECTION:FINAL_SUMMARY:END -->

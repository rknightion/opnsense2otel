---
id: OPN-0127
title: Document the firmware info exception to the global request timeout
status: To Do
assignee: []
created_date: '2026-10-12 20:00'
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
- [ ] #1 Generic timeout/retry help explains the firmware info floor and caller deadline precedence; generated references are regenerated without changing runtime defaults.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

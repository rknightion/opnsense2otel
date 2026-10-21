---
id: OPN-0142
title: Refresh Go and certificate trust dependencies and publish release
status: In Progress
assignee: []
created_date: '2026-10-21T15:05:00Z'
updated_date: '2026-10-21T15:09:00Z'
labels: []
dependencies: []
priority: high
ordinal: 95000
---

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All Go module, build and release pins use 1.27.3 and gate passes
- [ ] #2 Current Renovate PRs are merged or have recorded blockers
- [ ] #3 Published release has verified Go and certificate trust evidence
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Aligned root and PromQL tool modules, cloud bootstrap, release workflows and both builder stages to Go 1.27.3. Verified registry index digests directly. Builder CA 20260909-r0 accepts one affected endpoint; apk upgrade to 20261020-r0 makes both reject with certificate verification errors. Runtime distroless package metadata shows ca-certificates 20250419+deb13u1. Full remote gate running. CodeRabbit skipped for dependency pins and declarative build wiring.
<!-- SECTION:NOTES:END -->

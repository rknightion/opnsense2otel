---
id: OPN-0111
title: Narrow the support policy to the current stable OPNsense release
status: To Do
assignee: []
created_date: '2026-09-20 10:43'
labels:
  - docs
  - canary
dependencies: []
priority: medium
type: chore
ordinal: 65000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
docs/compatibility.md states the policy as "the current stable OPNsense release and the previous stable: today that means 26.1.x and 25.7". Both halves are wrong as of 2026-09-20: upstream has tagged through 26.7.4, so current stable is 26.7.x and the named window is two releases out of date. The live evidence does not cover the stated window either - after OPN-0106 retires the prod profile and the lab consolidates, the only release-channel box is guest 106 tracking the current stable head, so previous-stable is verified from upstream source alone. Rob decided on 2026-09-20 to narrow the policy to the current stable main release only, rather than keep claiming a window nothing exercises. The page must claim what is actually verified and say how, because it is what an external contributor reads before filing a version bug. Check whether the page states the window in more than one place and whether any shim or exemption note justifies itself by the two-release window - the interfacesOverview release-vm entry cites stable/26.1 and was written when 26.1 was thought current, so its reasoning needs re-deriving rather than deleting.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 docs/compatibility.md states a current-stable-only policy and names no release version that upstream has superseded
- [ ] #2 The page distinguishes what a live box verifies from what is derived from upstream source
- [ ] #3 Any exemption or shim note justifying itself by the two-release window is re-derived against the narrowed policy, and the interfacesOverview release-vm entry is corrected or removed
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

---
id: OPN-0111
title: Narrow the support policy to the current stable OPNsense release
status: Done
assignee:
  - '@claude'
created_date: '2026-09-20 10:43'
updated_date: '2026-09-20 11:20'
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
- [x] #1 docs/compatibility.md states a current-stable-only policy and names no release version that upstream has superseded
- [x] #2 The page distinguishes what a live box verifies from what is derived from upstream source
- [x] #3 Any exemption or shim note justifying itself by the two-release window is re-derived against the narrowed policy, and the interfacesOverview release-vm entry is corrected or removed
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Landed in 47e40235. Three pages carried the stale claim, not one: docs/compatibility.md, docs/faq.md and README.md all named "current and previous stable". The two hits under docs/superpowers/ were left alone - that tree is gitignored scratch, confirmed at .gitignore:68.

Two things found while re-deriving, neither in the original scope. First, the page called the canary daily; that stopped being true when the schedule came off the workflow, so it now describes what the canary actually covers. Second, the shim-pruning paragraph promised that shims "get pruned" when a release ages out, which would be a bad promise to keep literally - it now says a cheap shape is usually kept, because tolerant-reader parsing is the whole reason an older box works.

AC3 came out bigger than expected. The interfacesOverview release-vm entry was removed as planned - stale reason, and redundant against a base missingOK that already covers rows[].link_typev6 - but nine MORE entries carry a "prune when 26.1 leaves the support window" trigger that this narrowing fires simultaneously: idsSettings, keaSubnets4, memoryStatistics, ndpTable, pfStatisticsByInterface, pfStatsInfo, protocolStatistics, quaggaOspfNeighbors, systemMbuf. I did not prune them and that is deliberate. Removing a missingOK entry alone does not remove a shim - it makes the canary ENFORCE a key no supported box sends, which manufactures exactly the standing-warning noise OPN-0096 had to clear once already. The real prune deletes the legacy struct field and drops best-effort reading for a box on the old shape, which contradicts the paragraph this same commit wrote. Each note now records that its trigger fired and points at OPN-0113, raised to take the nine decisions one at a time.

No CodeRabbit run: docs prose plus declarative ledger notes and one redundant entry removal, no branching logic. Validated through just check instead, exit 0.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Policy narrowed to the current stable release (26.7.x) across compatibility.md, faq.md and README.md, with a new section separating live-verified evidence from source-derived, in 47e40235. The interfacesOverview release-vm exemption is gone - stale and redundant. Nine further entries whose prune trigger this narrowing fired are deliberately NOT pruned, because removing a missingOK alone enforces a key no supported box sends and the real prune drops best-effort reading; their notes record the fired trigger and OPN-0113 carries the nine decisions. just check exit 0.
<!-- SECTION:FINAL_SUMMARY:END -->

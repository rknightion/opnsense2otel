---
id: OPN-0107
title: Stamp the OPNsense generation on both testbed canary reports
status: To Do
assignee: []
created_date: '2026-09-20 10:42'
labels:
  - canary
  - testbed
  - ci
dependencies: []
priority: high
type: feature
ordinal: 61000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
live-canary.yml never passes --generation, so the nightly and release-vm reports print a bare "## OPNsense live-box schema canary" heading while camden prod printed "- release 26.7.1_1". The consequence was found on 2026-09-20: issues 727 and 726 carried seventeen byte-identical clean comments each, and nothing in them revealed that guest 102 had been frozen on opnsense-devel 27.1.a_40 since 2026-07-25 and guest 106 on opnsense 26.7.1_1 since 2026-07-28. A clean report from a stale box is indistinguishable from a clean report from a current one, which is the precise failure the stamp exists to prevent - the comment on cmd/apidrift/report.go says so already. scripts/canary/opnsense-prod-canary.sh reads it from api/core/firmware/status product.product_version; that script is being deleted by OPN-0106, so lift the technique before it goes. The devel box reports a snapshot version such as 27.1.a_40, so the label must carry the full string and not be parsed into a release number.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Both live-canary matrix targets pass --generation, read from the box at probe time and never hardcoded
- [ ] #2 The report heading for a nightly run names the devel snapshot and for a release-vm run names the release version
- [ ] #3 A box that cannot be asked for its version produces a report that says so, rather than an unlabelled heading that reads as a normal clean run
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

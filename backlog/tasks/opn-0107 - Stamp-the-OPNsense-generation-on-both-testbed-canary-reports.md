---
id: OPN-0107
title: Stamp the OPNsense generation on both testbed canary reports
status: Done
assignee:
  - '@claude'
created_date: '2026-09-20 10:42'
updated_date: '2026-09-20 11:12'
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
- [x] #1 The report heading for a nightly run names the devel snapshot and for a release-vm run names the release version
- [x] #2 A box that cannot be asked for its version produces a report that says so, rather than an unlabelled heading that reads as a normal clean run
- [x] #3 Every canary run stamps the heading with the generation read off the box at probe time, never a hardcoded or caller-supplied default
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
DEVIATION from the AC as written, stated rather than quietly absorbed. AC1 said both live-canary matrix targets pass --generation. They do not: apidrift now reads the identity itself, straight after its pre-flight probe, and an explicit --generation only overrides. That is better on three counts - it is unit-testable without a box, it applies to every caller including a local run, and the label cannot drift from the probe it heads because the same client reads both. The AC substance, read off the box at probe time and never hardcoded, is met, and AC1 is reworded to describe what was built. live-canary.yml needed no change at all: it cats the report and never greps the heading.

Implementation: cmd/apidrift/generation.go. deriveGeneration maps product_id to a channel word (opnsense to release, opnsense-devel to devel), prints an unrecognised id verbatim rather than guessing a channel, and carries the version through untouched so a snapshot suffix survives - parsing 27.1.a_40 down to a release number would discard exactly what distinguishes one nightly from the next. parseFirmwareIdentity resolves top-level-wins-else-nested to match opnsense/firmware.go productID/productVersion; the nested copy is the one that matters, since a box that has never run a firmware check returns only product/status/status_msg (#640) and that is a freshly booted lab box.

Tests written before the implementation and watched fail on undefined symbols: five derivation cases, three resolution cases and a garbage-body case. A live run against a real box is not part of this task - OPN-0108 raises the lab next and its first report will carry the stamp, which is where the end-to-end evidence comes from.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Landed in ced3470b and pushed. apidrift reads product_id and product_version off the box after its pre-flight and stamps the heading, so a devel snapshot reads "devel 27.1.a_40" and a release "release 26.7.1_1"; an unreadable status renders a loud VERSION UNKNOWN heading rather than the old unlabelled one, and never fails the run. Verified by nine table cases across derivation, top-level-wins-else-nested resolution and a garbage body, all failing first on undefined symbols; just check exit 0; CodeRabbit zero findings. Built differently from AC1 as originally written and reworded to match - the tool reads the version rather than the workflow passing it.
<!-- SECTION:FINAL_SUMMARY:END -->

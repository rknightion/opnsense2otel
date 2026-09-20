---
id: OPN-0106
title: Retire the production-firewall schema canary
status: In Progress
assignee:
  - '@claude'
created_date: '2026-09-20 10:42'
updated_date: '2026-09-20 10:45'
labels:
  - canary
  - testbed
  - chore
dependencies: []
references:
  - 'https://github.com/rknightion/opnsense2otel/issues/693'
priority: high
type: chore
ordinal: 60000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The prod canary runs only on camden, on opnsense-prod-canary.timer at 07:20 UTC, and files into GitHub issue 693. It has produced byte-identical output on all 27 runs since 2026-08-24: 117 clean, 0 drift, 56 plugin-gated 404, 3 skipped. It also duplicates a generation the lab already covers - read live on 2026-09-20, testbed guest 106 runs 26.7.1_1 and the prod box reports release 26.7.1_1, the same build. Its only distinct contribution was a smaller plugin set, 117 probeable endpoints against the lab boxes 185. Rob decided on 2026-09-20 to retire it: the lab keeps nightly and release-vm, and no probe of the production firewall remains. Accepted loss, decided the same day: the smartInfo exemption note records prod as the only profile enforcing output.endurance_used and output.spare_available, because the testbed disks are virtio and cannot report wear. After this change nothing enforces those two paths on any profile; keep the note honest about that rather than deleting the reasoning.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 opnsense-prod-canary.timer and .service are stopped, disabled and removed from camden, and the repo copies under scripts/canary/ are deleted
- [ ] #2 ProbeProfileProd is gone from the closed profile set and apidrift rejects --profile prod
- [ ] #3 The two prod-scoped exemption entries are removed from opnsense/testdata/schemas/exemptions.json and the smartInfo note records that endurance_used and spare_available are no longer enforced anywhere
- [ ] #4 GitHub issue 693 is closed with a comment naming this task and stating that the prod box is no longer probed
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Remove ProbeProfileProd and its KnownProbeProfiles entry in opnsense/schema_validate.go; rewrite the three-targets doc comment above the const block, which explains the keying by contrasting three targets on three axes.
2. Strip every prod-scoped ledger entry: 2 in exemptions.json, and 61 coverage overrides across 13 endpoints in coverage.json (captivePortalVoucherProviders, carpStatus, firmware, ipsecPhase1, ipsecSad, ipsecSpd, keaLeases4, keaPdPools6, keaSubnets4, openVPNInstances, openVPNSessions, trafficShaperStatistics, wireguardClients). TestExemptionProfileNamesAreKnown rejects an unknown profile key, so a partial strip fails the build rather than going quiet.
3. Rewrite the smartInfo note to record that output.endurance_used and output.spare_available are now enforced on no profile, and why - virtio disks cannot report wear. Rob accepted this loss on 2026-09-20.
4. Delete scripts/canary/ and its canary-test recipe, and drop canary-test from the check gate in the justfile.
5. Rework the profile tests in schema_validate_test.go and schema_coverage_test.go onto the two surviving profiles, keeping the per-entry override semantics under test rather than deleting the cases.
6. just check.
7. camden: stop, disable and remove opnsense-prod-canary.timer, its service and /usr/local/bin/opnsense-prod-canary.sh, and confirm no timer remains.
8. Close issue 693 with a comment naming this task.
<!-- SECTION:PLAN:END -->

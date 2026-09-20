---
id: OPN-0106
title: Retire the production-firewall schema canary
status: Done
assignee:
  - '@claude'
created_date: '2026-09-20 10:42'
updated_date: '2026-09-20 11:06'
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
- [x] #1 opnsense-prod-canary.timer and .service are stopped, disabled and removed from camden, and the repo copies under scripts/canary/ are deleted
- [x] #2 ProbeProfileProd is gone from the closed profile set and apidrift rejects --profile prod
- [x] #3 The two prod-scoped exemption entries are removed from opnsense/testdata/schemas/exemptions.json and the smartInfo note records that endurance_used and spare_available are no longer enforced anywhere
- [x] #4 GitHub issue 693 is closed with a comment naming this task and stating that the prod box is no longer probed
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Verified before closing. AC1: camden reports 0 opnsense timers and 0 opnsense unit files after disable and removal, and /usr/local/bin/opnsense-prod-canary.sh is gone; all five files were copied to /root/backups/opnsense-prod-canary-20260920-110407 first, because the token-renew pair existed only on camden and in no repo. AC2: `go run ./cmd/apidrift --profile prod` exits 2 with `unknown --profile "prod", want one of [nightly release-vm]`. AC3: both ledgers grep clean for a prod key - 61 coverage overrides across 13 endpoints and 2 exemptions removed; the smartInfo pair moved to base scope with the note rewritten to record that nothing enforces endurance_used or spare_available any more. AC4: issue 693 closed as not planned with comment 5749400736. DoD: `just check` exit 0 twice, before and after the review fixes.

A camden-only unit the repo never carried turned up during teardown: opnsense-canary-token-renew.timer/.service, renewing an OpenBao token at /root/.opnsense-canary-bao-token every 05:40 UTC for the canary to mint a GitHub token. grep proved its only two consumers were that service and the canary script, so it went with them. THE TOKEN FILE IS DELIBERATELY LEFT IN PLACE: revoking it server-side is irreversible and needs the CI-SECRETS route, and it cannot be revoked once the file is gone. With no renewal it lapses on its own within the 72h role period. Rob owns the decision to revoke and remove it.

CodeRabbit ran before the commit: 5 findings, all minor, all fixed. The load-bearing one was mine - the rewritten profile-keying comment claimed the two lab boxes share a plugin set, which the live 404 lists contradict, and my first correction then claimed the two absence sets are not nested, which is also false. Checked against the 2026-09-15 comments: release-vm absent = nightly absent + netbirdServiceStatus + netbirdStatus, a strict superset. The comment now states that.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Retired in 397043c6 and pushed to main. The camden timer, its service, the token-renew pair and the canary script are removed and archived; the prod probe profile, its 2 exemptions and its 61 coverage overrides are gone; issue 693 is closed. Verified by apidrift rejecting --profile prod (exit 2), camden listing zero opnsense timers and unit files, and just check exit 0. Two things are deliberately left for Rob: the OpenBao token file on camden, which lapses within 72h unrenewed but can only be revoked while it still exists, and the loss of enforcement on output.endurance_used and output.spare_available, which he accepted on 2026-09-20 and which the smartInfo note now records as a real gap rather than a tidy-up.
<!-- SECTION:FINAL_SUMMARY:END -->

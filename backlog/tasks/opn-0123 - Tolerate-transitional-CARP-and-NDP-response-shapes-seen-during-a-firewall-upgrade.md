---
id: OPN-0123
title: >-
  Tolerate transitional CARP and NDP response shapes seen during a firewall
  upgrade
status: In Progress
assignee:
  - '@loop10'
created_date: '2026-10-12 19:08'
updated_date: '2026-10-12 19:44'
labels:
  - carp
  - ndp
  - api-client
dependencies: []
priority: medium
type: bug
ordinal: 76000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
During the 2026-10-12 production upgrade to 26.7.6 the box answered api/diagnostics/interface/get_vip_status with {"total":0,"rowCount":0,"current":1,"rows":[],"carp":[]} - carp as an empty array where the decoder expects an object (carpInfo in opnsense/carp.go), failing the whole fetch with an unmarshal error - and answered api/diagnostics/interface/get_ndp with HTTP 200 and an empty body (unexpected end of JSON input). Each happened once mid-upgrade, but both recur on any reboot or service restart window.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A carp value of [] decodes as 'no CARP info' without failing the fetch, pinned by a fixture taken from the observed body shape; a non-empty object still decodes as before
- [ ] #2 An empty 200 body from get_ndp is reported as a transient fetch failure (counted, no panic, no fabricated zero series) rather than a JSON decode error, pinned by a test
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Implement transitional CARP/NDP decoding within owned files; fail reproductions before fixes; staged candidate after local gate for root security review, CodeRabbit and publication.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop10: pre-mutation ownership gap, 0 implementation attempts. Empty HTTP200 NDP classification must precede shared client observer/cache. Root amended ownership for an endpoint-specific client.go check and serializes this lane after firmware landing to avoid shared-file ownership. Observer behavior tests remain in ndp_test.go.
<!-- SECTION:NOTES:END -->

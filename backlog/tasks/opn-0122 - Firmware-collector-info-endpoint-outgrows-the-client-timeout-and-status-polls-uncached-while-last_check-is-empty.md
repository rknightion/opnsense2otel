---
id: OPN-0122
title: >-
  Firmware collector: info endpoint outgrows the client timeout and status polls
  uncached while last_check is empty
status: In Progress
assignee:
  - '@loop10'
created_date: '2026-10-12 19:06'
updated_date: '2026-10-12 19:42'
labels:
  - firmware
  - api-client
dependencies: []
priority: high
type: bug
ordinal: 75000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Since 2026-10-07 api/core/firmware/info on the production box takes 27-35s to answer (measured directly; response shape unchanged: product_id, product_version, plugin[], package[] ~950 rows). The client's default per-request timeout is 15s with 3 retries, so the firmware collector has been at 0% success on every cold-tier poll since 2026-10-12, plugin inventory metrics are absent and OPNsenseCollectorDegraded{collector=firmware} is firing. Separately, after the 26.7.6 upgrade the box has not run an update check, so api/core/firmware/status returns an empty last_check; firmwareStatusCacheable refuses to cache that body (retired issue #724 rule), and the security-posture config snapshot (opnsense/security_posture.go via internal/logship/configsnapshot) now calls the endpoint live about every 10s instead of roughly twice a day. Both are in opnsense/firmware.go and internal/options/cache.go.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 firmware/info is fetched with a per-endpoint timeout long enough for a ~35s response without raising the global client timeout, and a test against a slow fake server that fails at the 15s default passes with the change
- [ ] #2 A firmware/status body with an empty last_check is cached for a bounded short TTL (not zero, not the full TTL), so polls of the endpoint drop from ~360/h to at most a few per hour while the box has never checked; a test pins the bound
- [ ] #3 Live proof after deploy: opnsense_exporter_scrape_collector_success{collector=firmware} is 1 on the production exporter and the api/core/firmware/status request rate is within the bound
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Implement the firmware request/cache seam; fail reproduction tests before fixes; return staged candidate after local gate for root security review, CodeRabbit, root publish and exact-SHA CI/live proof.
<!-- SECTION:PLAN:END -->

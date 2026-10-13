---
id: OPN-0117
title: Add a read-only live API contract test layer
status: In Progress
assignee:
  - '@codex'
created_date: '2026-09-25 08:05'
updated_date: '2026-10-13 13:40'
labels:
  - testing
dependencies: []
ordinal: 71000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The 2026-09-25 test audit found no integration, e2e or live build tags in this repo (0 files); all coverage is unit tests over recorded fixtures, so upstream API shape drift is caught only if a fixture happens to match reality. Add a `//go:build live` read-only contract suite modelled on tailscale2otel's internal/tsapi/contract/live/live_test.go: it hits the real API, distinguishes misconfiguration from absent resources, and runs in CI with credentials minted through OpenBao (never a PAT).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A //go:build live suite exercises the endpoints the collectors depend on, read-only
- [ ] #2 The on-demand canary runs the suite for both profiles using its existing scoped CI credentials and workflow-owned lab lifecycle; local runs skip cleanly when credentials are absent
- [ ] #3 A deliberately broken fixture or shape assumption makes the live test fail for the right reason
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Add a tagged live suite over registered endpoint contracts and existing request engine, with a stricter verdict for transport/auth/nonJSON/type errors. Only declared plugin404 and absent live parameters may skip; core404 fails. Match HTTP2 transport, preflight early, required CI configuration cannot skip. Add failure-first HTTP/config tests; run via just live-contract in existing serialized on-demand lifecycle with always teardown. No schedule/new secrets.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-10-13 Rob approved on-demand canary sessions instead of scheduled CI, reusing existing lab power and credentials. Scheduled-test and newly minted OpenBao credential requirements are explicitly replaced; timers stay disabled.

Tagged live endpoint suite plus ordinary-CI HTTP/config fixtures implemented. Reuses actual schema/request/resolver contracts; required CI cannot skip, core404 fails, declared plugin source404 is explicit absence, auth/server/transport fail. HTTP2 and30s request timeout match canary;10s preflight and20m suite bound. Workflow uses existing secrets and just live-contract; no schedule. Security review passed after resolver404 correction. Local absent-credential skip is not live proof; final canary pending.

Final full just check passed. CodeRabbit found reporting sequencing and fingerprint gaps: fixed continuation+final failure, prevented false issue close, emitted sanitized endpoint/class fingerprint. Final delta had only minor preflight-class finding, fixed and focused tagged tests passed; no remaining findings. Still requires actual new-suite dual-profile run after publication.
<!-- SECTION:NOTES:END -->

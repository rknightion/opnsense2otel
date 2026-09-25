---
id: OPN-0116
title: Prune tautological and change-detector unit tests
status: To Do
assignee: []
created_date: '2026-09-25 08:05'
labels:
  - testing
dependencies: []
ordinal: 70000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the 2026-09-25 fleet test-signal audit (sampled read-only). Delete or consolidate tautological tests (restating the implementation) and change-detector tests (pinning incidental text, markup, counts or internals). Keep parsing, state-machine, retry, security/PII, wire-contract and incident regression tests. Re-verify each candidate before deleting it; the list below comes from a sample and is not exhaustive. Candidates: internal/logship/action_test.go:64 TestActionVocabularyIsBinary (constant against its own literal); opnsense/client_test.go:100 TestNewClient_EndpointCount (magic count 204, hand-edited on every endpoint); ~19 near-identical TestXRegistered parser map-lookup checks in internal/logship/syslog/*_test.go, consolidate into one registry table test; internal/geoip/maxmind_test.go:279 TestDatabasePath (string-join accessor).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each listed candidate is deleted, consolidated or kept with a one-line reason in the notes
- [ ] #2 Other tests in the same pattern found during the work are handled the same way
- [ ] #3 The repo's check recipe passes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

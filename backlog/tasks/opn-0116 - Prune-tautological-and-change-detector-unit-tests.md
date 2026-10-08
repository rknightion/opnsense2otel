---
id: OPN-0116
title: Prune tautological and change-detector unit tests
status: Done
assignee:
  - '@claude'
created_date: '2026-09-25 08:05'
updated_date: '2026-10-08 21:21'
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
- [x] #1 Each listed candidate is deleted, consolidated or kept with a one-line reason in the notes
- [x] #2 Other tests in the same pattern found during the work are handled the same way
- [x] #3 The repo's check recipe passes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Re-verify each audit candidate, delete tautologies, consolidate the parserFor lookups into one table test seen failing once.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Deleted TestActionVocabularyIsBinary (constants vs own literals; values pinned by derive and sink tests), TestDatabasePath (string join; DatabasePath exercised by updater/fetch tests). TestNewClient_EndpointCount -> TestNewClient_UsesDefaultEndpoints (content equality kept, magic 204 dropped; TestEndpointACLCoversEveryEndpoint already guards registration). 17 TestXRegistered lookups + audit loop -> TestEveryParserProgramIsRegistered (35 programs), seen failing with ppp unregistered. Kept registration tests that pin more than a lookup: CARP/UPnP exact+enrichment, DHCP client/DHCP6C incident regressions, sudo enrichment-off, netbird negative, configctl double-count, ruleupdater subsystem, haproxy behavioural. Stale count-bump references removed from reference/adding-a-collector.md and doc-0002.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Pruned 4 audited tautological/change-detector tests and consolidated 17 per-parser registration lookups into one table-driven registry test (seen failing when a parser is unregistered). Docs no longer tell contributors to bump an endpoint count. just check green.
<!-- SECTION:FINAL_SUMMARY:END -->

---
id: OPN-0112
title: Comment on the canary issues only when the findings change
status: Done
assignee:
  - '@claude'
created_date: '2026-09-20 10:43'
updated_date: '2026-09-20 11:37'
labels:
  - canary
  - ci
dependencies:
  - OPN-0107
priority: low
type: chore
ordinal: 66000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
live-canary.yml appends a full report to issues 727 and 726 on every run whether or not anything moved. Ten consecutive comments on each from 2026-09-06 to 2026-09-15 were byte-identical clean reports, and camden managed 27 on issue 693. A reader cannot tell a fresh clean run from a re-post, and real drift would land in a thread nobody reads any more - OPN-0096 was raised because standing noise had already made findings invisible once. The issues stay open and stay the workflow output channel per the Wave operating model; only the posting rule changes. A silent workflow is its own failure mode, so absence of a comment must not be the only signal that the canary still runs: a periodic heartbeat covers the case where the run stopped happening at all. The generation stamp from OPN-0107 is part of the comparison - a clean run against a newly updated box is new information even when the findings are identical.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A run that differs in findings or in generation posts a full report
- [x] #2 A run whose findings and generation match the most recent full report comment posts no new comment, and heartbeat comments are never the baseline for that comparison
- [x] #3 A heartbeat comment appears at a fixed interval even during an unbroken clean streak, so a canary that stopped running is distinguishable from one finding nothing
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CodeRabbit pre-commit review of the task text (2026-09-20) found the AC1 baseline underspecified: if the heartbeat is itself a comment, comparing a run against "the last comment" compares it against a heartbeat and re-posts a full report every time one lands. The baseline is the most recent FULL REPORT comment, and heartbeats are excluded from it. AC1 and AC3 are edited to say so.

Landed in 82ae8739. The diagnosis in the task description was incomplete: the workflow ALREADY closed a clean issue, so the closing logic was never the problem. The real cause is that two standing conditions keep warnings=true on an otherwise clean run - a non-empty RequiredUnresolved coverage set, and zerotierNetworkInfo being a parameterised endpoint with no live parameter (aggregate, cmd/apidrift/report.go:36-58). The issue therefore never auto-closed, and every run appended the full report again.

That changed the design. Comparing report BODIES would have been brittle, because the smoke step appends a metric-name count and collector tally that move with any collector change. apidrift digests findings instead: generation, plus endpoint+kind+path for every finding, plus the unresolved required-coverage paths - which had to be folded in late, because RequiredUnresolved is one of the two things holding the issue open and a required path becoming verified is real news.

Fourteen table cases, written before the implementation and watched fail on undefined symbols.

CodeRabbit found two majors that were both genuine and both mine:

1. `gh api --paginate --jq` emits ONE JSON DOCUMENT PER PAGE. An array filter across two pages yields `[...][...]`, and `--argjson` rejects it - verified locally: `jq: invalid JSON text passed to --argjson`. Under set -euo pipefail that aborts the step, so the canary would have stopped commenting altogether once an issue passed GitHub 30-comment page boundary. These issues accumulate a comment per run by design, so it would have broken in ordinary use. Fixed by emitting one object per line and slurping with jq -s.
2. The smoke half of the comparison key was the bare failed flag, so collector A failing on one run and collector B on the next read identically and the change would have landed in silence - the exact class of miss this task exists to prevent. The smoke step now digests assertion verdicts plus failed-collector identities, excluding the counts that wobble.

A third finding caught an inconsistency I introduced in OPN-0111: the How drift is caught section still said two canaries run daily and that the live one scrapes a devel box, singular. Neither is true - the live canary is dispatch-only and probes both lab boxes. Corrected in the same commit.

Validated without a box: jq baseline extraction driven against a fixture covering a human reply, a heartbeat, a null body and an empty history; the --argjson failure reproduced and then shown fixed; shellcheck clean on both run blocks; just check exit 0. End-to-end proof needs a live run, which OPN-0109 brings.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Landed in 82ae8739. apidrift digests findings, generation and unresolved required-coverage paths; the workflow combines that with a smoke digest, writes it as a hidden marker and stays silent when it matches the last full report. Heartbeats carry a different marker so they can never become the baseline, and one goes out after HEARTBEAT_DAYS of silence so a stopped canary stays distinguishable from a quiet one. Verified by fourteen table cases written first, a jq fixture covering heartbeat/human/null/empty history, a reproduced-then-fixed --argjson pagination failure, shellcheck clean and just check exit 0. Two genuine CodeRabbit majors fixed before shipping, one of which would have silently stopped the canary commenting once an issue passed 30 comments.
<!-- SECTION:FINAL_SUMMARY:END -->

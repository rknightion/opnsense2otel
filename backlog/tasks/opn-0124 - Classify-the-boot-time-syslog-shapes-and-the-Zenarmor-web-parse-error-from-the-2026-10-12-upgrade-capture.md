---
id: OPN-0124
title: >-
  Classify the boot-time syslog shapes and the Zenarmor web parse error from the
  2026-10-12 upgrade capture
status: Done
assignee:
  - '@loop10'
created_date: '2026-10-12 19:08'
updated_date: '2026-10-12 21:02'
labels:
  - syslog
  - zenarmor
dependencies: []
references:
  - >-
    backlog/docs/doc-0004 -
    Camden-capture-parser-and-pass-through-review-OPN-0104.md
priority: medium
type: chore
ordinal: 77000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The production firewall reboot during the 26.7.6 upgrade (2026-10-12, about 09:50-10:07 UTC) put 257 unparsed syslog records into the camden debug capture: kernel 177, devd 14, opnsense 12, lldpd 12, pkg-static 10, charon 10, ppp 7, dhclient 4 and single-digit others (configd.py, daemon, eastpect, zenoverlay, pkg, dhcp6c, unbound, lldpcli), plus one Zenarmor parse_error record in the web family. Most are boot-sequence output that OPN-0104's disposition table did not see because the previous capture predates a reboot. This resets OPN-0104's week-quiet clock for retiring the capture mount. A private snapshot of the capture is kept outside the repository (path in the loop goal); never copy payload values into the repository, fixtures included - synthesise fixtures from the shape.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every program/shape in the 2026-10-12 capture has a disposition in internal/logship/syslog: a parser, a documented known pass-through, or a documented drop reason, with synthesised fixtures pinning each new rule
- [x] #2 The Zenarmor web parse_error is reproduced from a synthesised document of the same shape and either parsed or rejected with a documented reason, pinned by a test
- [x] #3 Replaying the private snapshot through the parser leaves no record classified unknown
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Classify private capture shapes with synthetic parser fixtures and redacted replay totals; fail reproductions before fixes; staged candidate after local gate for root security review, CodeRabbit and publication.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
loop10: 3 implementation cycles. Private manifest verified; local production-ingress replay ships 257 records (128 parsed, 129 documented known pass-through), zero corpus unknown; synthetic unknown positive control captured. Web parse_error reproduced as unescaped JSON control bytes; intentionally rejected with reason, raw body still ships, capture remains. New fixtures synthesized from shape, literal-address source fence corrected without changing runtime messages/assertions; guarded original and delta review PASS. CodeRabbit completed zero findings across all six original files, no repeat for trivial fixture construction delta. Exact full gate passed on corrected staged candidate. Root integration, exact-SHA CI and post-auto-deploy readback are pending; not Done yet.

loop10: attempts 3, completed. Corrected source landed, exact integrated full gate and ci-success passed; auto-deployed build observed through the granted datasource proxy. Local private replay proves classification and raw-body retention; live readback proves rollout, not a new production reboot replay.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Classified all captured reboot shapes with anchored parsers or documented pass-through: 257 local ingress records shipped, 128 parsed, 129 known, zero unknown. Synthesized Zenarmor web malformed-control-byte case intentionally rejects while retaining raw shipping/capture. Tests preserved, source literals corrected, guarded reviews and full gates passed; deployed build observed.
<!-- SECTION:FINAL_SUMMARY:END -->

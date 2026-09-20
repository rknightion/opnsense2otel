---
id: OPN-0108
title: Update both testbed firewalls to their channel head on every canary session
status: To Do
assignee: []
created_date: '2026-09-20 10:43'
labels:
  - canary
  - testbed
dependencies:
  - OPN-0107
priority: high
type: feature
ordinal: 62000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live-read on 2026-09-20: guest 102 runs opnsense-devel 27.1.a_40 installed 2026-07-25 and guest 106 runs opnsense 26.7.1_1 installed 2026-07-28, both 54-56 days stale. Upstream opnsense/core has since tagged 26.7.2, 26.7.3 and 26.7.4, so the release box is three point releases behind and the devel box has missed eight weeks of snapshots. Both channels are configured correctly - 102 carries <type>devel</type> and 106 an empty <type/> - so the channel is right and nothing was ever pulling. A frozen devel box makes the nightly profile worthless as early warning, which is the only reason it exists. Rob decided on 2026-09-20 that both boxes track their channel head. The update has to be part of the session rather than a separate timer, so currency is a property of every run instead of a thing that rots unobserved; the previous host-side timers rotted exactly this way. A broken devel snapshot must not read as drift or as a box fault, so the box needs a restore point before the update and the run needs a distinct update-failed outcome. Snapshot and rollback are Proxmox operations and belong behind a new allowlist-gated verb in scripts/testbed/opnsense-testbed-power.sh, never a direct qm call.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A canary session updates each firewall on its own configured channel before probing, and the probed version is the post-update one
- [ ] #2 Each box has a restore point taken before the update, and a box that does not pass the readiness gate afterwards is rolled back to it
- [ ] #3 A failed update or rollback is reported as its own outcome, distinguishable in the run from breaking drift and from an unreachable box
- [ ] #4 Snapshot, update and rollback run through the power script under the existing id allowlist, and scripts/testbed/testbed_power_test.py covers the new decision logic
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

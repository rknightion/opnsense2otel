---
id: OPN-0108
title: Update both testbed firewalls to their channel head on every canary session
status: Done
assignee:
  - '@claude'
created_date: '2026-09-20 10:43'
updated_date: '2026-09-20 12:58'
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
- [x] #1 A canary session updates each firewall on its own configured channel before probing, and the probed version is the post-update one
- [x] #2 Each box has a restore point taken before the update, and a box that does not pass the readiness gate afterwards is rolled back to it
- [x] #3 A failed update or rollback is reported as its own outcome, distinguishable in the run from breaking drift and from an unreachable box
- [x] #4 Snapshot, update and rollback run through the power script under the existing id allowlist, and scripts/testbed/testbed_power_test.py covers the new decision logic
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
LIVE FINDING 2026-09-20, the expensive one: `configctl firmware upgrade` SILENTLY DOES NOTHING. The action exists in actions_firmware.conf with `type:script` and empty `parameters:`, configd accepts the call, `qm guest exec` returns 0 - and then no upgrade process ever appears, no log is written, and the version does not move. It runs through `daemon -f`, so it detaches and leaves nothing to inspect. A green exit code from it proves only that configd took the message. Caught only because the box was still on 27.1.a_40 fifteen minutes later with no pkg process running and a firmware log whose newest entry was from July.

USE `opnsense-update -bkp` INSTEAD. opnsense-update(8) names it as the way to "update all currently installed components at once" - base, kernel and packages. Base and kernel need a reboot to apply, which the check reports as needs_reboot=1, so the script reboots explicitly afterwards.

BOTH the upgrade RPC and the reboot RPC have their results deliberately tolerated rather than trusted, because a base/kernel upgrade can take the box down underneath the guest agent and fail the call for a perfectly good reason. Success is decided afterwards by re-probing the box, which is the only evidence worth anything. That is also why the configctl failure was survivable rather than silently reported as a successful update.

The update verdict reads connection and repository BEFORE any package list, because a box that cannot reach its mirror reports empty lists and is otherwise indistinguishable from an up-to-date one. Calling that "current" would certify a stale box as fresh precisely when we have no idea - the exact failure this task exists to fix. That case is its own verdict, unusable, and is neither success nor an update failure.

LIVE PROOF, guest 102: pending verdict -> previous restore point rotated -> new restore point -> opnsense-update -bkp -> reboot -> serving :443 again after 48s -> re-probed -> current. opnsense-devel 27.1.a_40 -> 27.1.a_287, 247 snapshots of catch-up after 56 days frozen.

LANDED in fa672a9f. Live evidence for both boxes, one raise/lower cycle each:
  102 opnsense-devel 27.1.a_40 -> 27.1.a_287, restore point preupdate-102 rotated then taken, serving :443 again 48s after reboot, re-probed current.
  106 opnsense 26.7.1_1 -> 26.7.4_1, restore point preupdate-106 taken, serving again after 113s, re-probed current. Run ended "both firewalls are at their channel heads"; lab lowered cleanly afterwards.

AC2 rollback is NOT live-exercised: neither box failed its readiness gate, so no rollback ran. The snapshot half is proven (both restore points taken, and 102 exercised the rotate-then-take path), the restore half is not. Recorded rather than claimed.

Exit codes proven live: lab down gives 3, and a contended lock gives 6 - the die() remap, verified with a zero-wait copy while an external flock was held. That matters because cmd_update and die both call `exit`, which ends the script, so the original `cmd_update || status=$?` never observed anything and the remap was dead code. Before the subshell fix a die would have escaped as exit 1 and read as apidrift breaking drift.

Five defects found by review and fixed before shipping, four of which are the same shape - something misleading presented as evidence:
1. probe_check copied the guest previous check when a probe failed, so a pre-upgrade result could be read as the post-upgrade state.
2. needs_reboot=1 with empty package lists reported current, so a silently failed reboot would certify as a completed update.
3. A work list that was absent or wrongly shaped counted towards nothing-to-do, so a payload we did not understand could resolve to current.
4. The hold was a round 5400s against a derived worst case of 12240s, so it would have lapsed mid-update and handed the box back to the down timer.
5. guest_exec reaches die() on a malformed qm envelope and die exits the SCRIPT, so one bad envelope would have killed the run - worst case between an upgrade and its verification.

One review finding DECLINED with the reason in the code: a box that comes back serving but still reports pending updates is not rolled back. The rollback paths exist for a box that did not come back; this one is in a known state a human can read, and rolling back would discard a partial upgrade on a healthy box and risk turning a boring exit 3 into a possibly-broken lab. The restore point is kept.

NOT live-exercised and worth knowing: the probe_check rewrite (clearing the guest file before probing) landed after the successful run, so the happy path proof predates it. Its failure mode is to report unknown, which is the safe direction.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Landed in fa672a9f and pushed. Both lab firewalls are at their channel heads for the first time since July - 102 on opnsense-devel 27.1.a_287 and 106 on opnsense 26.7.4_1 - proven by a live run that re-probed each box rather than trusting the upgrade call, which matters because the first mechanism tried, configctl firmware upgrade, reports success while doing nothing at all. Verified by 67 unit tests, a live update of both boxes, and live exit-code checks including the die remap. Rollback is unproven: no box failed its readiness gate, so only the snapshot half ran.
<!-- SECTION:FINAL_SUMMARY:END -->

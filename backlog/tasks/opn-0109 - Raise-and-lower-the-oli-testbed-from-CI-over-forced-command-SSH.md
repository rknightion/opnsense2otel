---
id: OPN-0109
title: Raise and lower the oli testbed from CI over forced-command SSH
status: To Do
assignee: []
created_date: '2026-09-20 10:43'
updated_date: '2026-09-20 10:59'
labels:
  - canary
  - testbed
  - ci
dependencies: []
priority: high
type: feature
ordinal: 63000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The lab is powered by hand today: a session SSHes to oli, runs opnsense-testbed-power.sh up <hold>, dispatches live-canary.yml, then releases and takes it down. opnsense-testbed-up.timer and opnsense-testbed-canary.timer exist but are deliberately disabled because GitHub delayed their scheduled runs by 2-3.5 hours every day. Rob decided on 2026-09-20 that CI owns the power, so the lab is up only for the run. The power script header argues against this and its three objections are answered rather than ignored. It says CI power needs a Proxmox API token and a tag:ci to oli:8006 grant from a public repo - it does not, if CI gets a forced-command SSH key restricted to one verb, which grants strictly less than the DEVBOX API credentials CI already holds. It says a cancelled run leaves guests indeterminate - a bounded hold plus a short-interval watchdog resolves no-hold to down within minutes, which is better than today, where a forgotten hold idles the lab until the next 08:30 backstop. It says CI gives no warm-up - up already blocks on readiness rather than a clock, and the answer is to extend the readiness gate, not to sleep. Rob confirmed tag:ci can already reach the tailnet through OIDC federation without a stored OAuth secret, so the SSH key should be the only new long-lived credential and is worth checking whether it can be short-lived too. Rewrite the workflow and power-script headers: they currently instruct a reader not to do this.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 live-canary.yml raises the lab, waits on readiness, probes and lowers it, with the lowering step running even when the run fails or is cancelled
- [ ] #2 A watchdog powers the lab down within minutes whenever no hold is live, replacing the daily 08:30 backstop
- [ ] #3 A cancelled or failed run leaves every guest stopped, verified by inducing one
- [ ] #4 The workflow and power-script headers state the current design, and the stale do-not-do-this reasoning is replaced rather than left to contradict it
- [ ] #5 The CI credential is restricted to the testbed lifecycle and nothing else: no interactive shell, no direct qm or pct, no guest outside the existing id allowlist. Whether that is one composite session command or a small fixed verb set is the implementer choice, but the credential must not be able to run anything beyond it
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CodeRabbit pre-commit review of the task text (2026-09-20): the description said "one permitted verb", which is underspecified rather than tight. A session needs up, readiness, probe and a guaranteed down, and a literal single verb cannot span the workflow steps that have to bracket a failed or cancelled run. AC2 is rewritten to constrain what the credential MAY NOT reach, and to leave composite-command-versus-verb-set to implementation. The no-shell, no-qm, allowlist-only constraints are the part that is not negotiable.
<!-- SECTION:NOTES:END -->

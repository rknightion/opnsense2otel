---
id: OPN-0136
title: >-
  Export per-NIC hardware receive discards (ixl rx_discards) for physical WAN
  parents
status: Done
assignee:
  - '@codex'
created_date: '2026-10-15 17:16'
updated_date: '2026-10-15 18:07'
labels: []
dependencies: []
priority: medium
ordinal: 89000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The exporter's interface metrics cover the pppoe0 logical interface but not its physical parent NIC. On the live firewall the PPPoE parent's driver counter dev.ixl.<n>.pf.rx_discards reached about 99k in 3 days, including a burst of about 5k in 10 s, while every exported interface error and drop counter stayed at zero. PPPoE traffic is not RSS-hashed, so it all lands on a single RX queue, and these bursts are invisible in Grafana. Expose per-physical-NIC driver discard counters (and per-queue where available) for every physical interface, including PPPoE and VLAN parents, and alert on bursts. Open question: which OPNsense API or diagnostics surface exposes the driver sysctls without SSH; if none does, record that and propose the narrowest alternative.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A counter for physical-NIC driver receive discards exists per NIC, including a PPPoE parent, and matches the live sysctl value on the deployed firewall
- [x] #2 The source surface is documented, or the absence of an API surface is recorded with the chosen alternative
- [x] #3 A dashboard panel or alert rule surfaces discard bursts
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Inspect source contracts and live evidence where required; implement bounded telemetry with fixtures, regenerate shared artifacts, review and run just check before landing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Route decided by the owner, 2026-10-15: the documented textfile recipe becomes a supported path. A root cron script on the firewall writes .prom files to /var/tmp/node_exporter, and the os-node_exporter plugin scrapes them. The exporter keeps its no-SSH policy, so update docs/development/api-absent-telemetry.md, which currently calls NIC driver sysctl trees out of scope.

Script scope: per-NIC dev.<driver>.N.pf.rx_discards, per-RX-queue packets and irqs (rxqNN), vmstat -i interrupt counts, and dev.netmap.* as an info record. The script must survive firmware upgrades and be bounded in cardinality (NICs x queues).

First, confirm live whether interfaces_input_queue_drops_total for the PPPoE parent already tracks rx_discards; the inventory found it stays 0.

Live source correction: netstat input drops matches pf.rx_discards on the physical WAN parent. The REST traffic interface response enumerates configured interfaces, includes the PPPoE logical interface with zero drops, and omits its unassigned physical parent entirely. An assigned physical LAN NIC already reports matching REST input queue drops. Thus the gap is parent inventory visibility, not universally broken input-drop accounting.

Shipped script executed on the deployed FreeBSD firewall in an isolated temporary output directory: all four physical NIC metrics emitted and WAN-parent value matched a fresh sysctl read exactly. No production cron, package or scrape configuration changed. The node_exporter plugin is not currently verified installed; persistent scheduling and scrape enrollment remain operator setup, with the supported recipe documented.

Final hardened script rerun successfully on the live FreeBSD firewall, with four physical-NIC discard samples and exact WAN-parent sysctl match. Source staging now private under /var/tmp; descriptor-anchored os.replace rejects symlink parents and directory destinations. Independent reviewer PASS. CodeRabbit delta raised cross-filesystem staging as major; runtime suggestion declined against the documented same-filesystem contract (reviewer agreed), with live stat confirming default output and staging share a filesystem. Test output moved under /var/tmp to enforce that contract. Persistent plugin/cron/scrape configuration was not installed by this task.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shipped bounded physical NIC/queue/interrupt/netmap textfile telemetry, safe atomic publication and dashboard panels. Final script executed on the deployed FreeBSD firewall and matched driver discard counters. Installation/cron/scrape enrollment remains documented operator setup. Same-filesystem output is required; final reviewer approved that contract. just gen and just check passed.
<!-- SECTION:FINAL_SUMMARY:END -->

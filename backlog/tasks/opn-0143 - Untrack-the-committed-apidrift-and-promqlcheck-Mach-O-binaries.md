---
id: OPN-0143
title: Untrack the committed apidrift and promqlcheck Mach-O binaries
status: To Do
assignee: []
created_date: '2026-11-07T11:05:00Z'
labels: []
dependencies: []
priority: low
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two compiled arm64 Mach-O executables are tracked in Git: apidrift at the repository root (about 16.8 MB) and tools/promqlcheck/promqlcheck (about 13.2 MB, its source main.go sits beside it). They bloat every clone and cannot be reviewed. Found by the 2026-11-07 Kodus ignore sweep (HRN-0557 on the agents board), which added both paths to this repository's Kodus ignore list as a stopgap.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Neither binary is tracked on main and both paths are gitignored
- [ ] #2 Whatever builds or runs them (justfile recipes, scripts, CI) builds from source instead
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

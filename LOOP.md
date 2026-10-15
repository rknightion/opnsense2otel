# Loop: opnsense2otel
tier: guarded
gate: just check
ci-required: ci-success
release-on-push: yes
deploy-on-push: yes
grafana-stack: robknight

Public repository: no account identifier, token, IP literal or personal data in any tracked file,
`backlog/` included. `just check-public-ips` catches IP literals against
`scripts/public-ip-allowlist.json`; nothing catches the rest. Run `just` with stdin from `/dev/null`.
`just ci` adds the deployment contracts, cross-compilation and the image build.

## Credentials

- API key and secret reach the exporter through `*_FILE` variables; never put one in the repo,
  a log, argv or a fixture.
- `just bump-module-major` is the one confirm-gated recipe: never pass `--yes` or `JUST_YES=1`.
- The dashboard sync mints a token that can write only the GitSync repository. Alert rules go
  through `gcx resources push`; dashboards are never pushed through the API.
- The testbed firewalls' API credentials are on no laptop: they live only in the repository's
  `tailnet` GitHub environment. Local `just capture` and `just run` cannot reach the testbed; live
  evidence is `gh workflow run live-canary.yml --ref main`, which returns a structure verdict, never
  a payload.

## Traps

- A push to `main` touching `grafana/**` runs `grafana-sync`: dashboards go into the GitSync repo,
  rules are pushed and pruned. Renaming an alert folder needs the new folder to exist and its
  service account to hold Admin on that folder first, or the push fails with a bare 403 and deploys
  nothing. Delete the old folder afterwards.
- The vendor directory is committed: run `just sync-vendor` after any `go.mod` change.
- Never hand-edit `grafana/dashboard*.json`, the alert manifests, docgen output or text between
  `docgen:begin/end` markers; edit the builder and regenerate.
- Never reintroduce a live API fetch on the `/metrics` request path, and `internal/webui` must never
  call `Gather()` on the live registry.
- Live-box canary and live delivery proof are dispatch-only against a lab testbed that is off by
  default. The live-canary workflow owns lab raise, hold release and shutdown. Live delivery proof
  needs the main session to raise the lab before dispatch and lower it afterwards. Do not re-enable
  automatic startup or canary timers. Reports carry key paths and type names only, never payload values.
- Issue numbers below 656 are retired GitHub issues, not tasks.
- `just check` is the whole bare-toolchain gate. Every `make` target is gone: discover with
  `just --list`. Heavy CI-only legs are not a lane's job: say the change is untested against them.
- auto-rc classifies only the head commit of a push (`HEAD^..HEAD`): push the shipped-code commit
  last or on its own when a release candidate build is needed.
- A major release needs the module path moved first: `just bump-module-major major=<N>`. It also
  rewrites `backlog/` notes quoting old import paths; restore those.
- CodeRabbit past about 100 files fails with `WebSocket closed`: review the source slice in a
  scratch worktree with generated paths (`grafana/dashboard*.json`, `grafana/alerts/grafana-managed/`,
  `opnsense/testdata/schemas/`, `docs/`, `README.md`) restored to base. Every changed source file
  must appear in a completed review; a finding on an excluded file is a false positive.
- Loki cannot show a record stamped older than 3h until its ingester chunk flushes, up to two
  hours: a proof asserting its own historical record within the run never passes. Report
  visibility-pending and read `logs_shipped_total`, `logs_dropped_total{reason}` and `partialSuccess`.
- Never call `qm` or `pct` on `oli`; drive power through `opnsense-testbed-power.sh` (allowlist
  102, 106, 105). A deny-path test uses a target that is harmless when it succeeds
  (`qm stop 999999`).
- `check-public-ips` scans `backlog/`: a task about a leaked address gives `file:line` and the kind
  of address, never the literal.

## Mutexes

- One `grafana-sync` writes the stack at a time and is never cancelled mid-write.
- One live delivery proof runs at a time.
- Live canary and delivery proof share one testbed: never run them concurrently.
- The testbed (guests 102, 106 and 105 on `oli`, off by default) takes one consumer at a time. Its
  `flock` guards power operations, not API writes. `live-canary.yml` raises and lowers the lab
  itself (matrix `max-parallel: 1`). Raising it by hand is a root action:
  `ssh oli '/usr/local/bin/opnsense-testbed-power.sh up <seconds>'`, sized to the work, because a
  five-minute watchdog powers the lab off whenever no hold is live; then `release` and `down`. A
  `down` exiting 8 stopped nothing: retry. A pre-flight probe failure on a lab nobody raised is not
  a regression: open no task.
- Append-only registries, one owner per file or one wiring pass at the end:
  `internal/collector/collector.go`, `interval_tiers.go`, `internal/options/collectors.go`,
  `main.go`, `opnsense/client.go`, `contract.go`, `cache.go`, `schema_registry.go`,
  `opnsense/testdata/schemas/exemptions.json` and `grafana/build_dashboard.py`. Pinned counts
  (golden POST count, docs count, rule count) move with them.

## Lane brief

A lane never powers the testbed: it reports that it needs live evidence and stops. A change outside
your boundary is stated as the exact edit and left unmade. Generated output (docgen markers,
dashboards, alert manifests, schema goldens) is regenerated, never hand-edited. New flow logic
double-counts or mis-orients until a test says otherwise: write the test first.

# Loop: opnsense2otel
tier: guarded
gate: just check
ci-required: ci-success
release-on-push: yes
deploy-on-push: yes
receiver: https://loopwatch.m7kni.com
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
  default. The main session raises it, dispatches, then takes it down; do not re-enable its timers.
  Reports carry key paths and type names only, never payload values.
- Issue numbers below 656 are retired GitHub issues, not tasks.

## Mutexes

- One `grafana-sync` writes the stack at a time and is never cancelled mid-write.
- One live delivery proof runs at a time.

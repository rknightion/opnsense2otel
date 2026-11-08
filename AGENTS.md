# opnsense2otel

A Prometheus and OpenTelemetry exporter for OPNsense firewalls. It polls OPNsense REST APIs and
serves `/metrics`, and it also *receives* pushed telemetry - syslog, Zenarmor and NetFlow - which it
ships as OTLP logs. Both halves matter: treating it as a poll-only exporter is the usual wrong
mental model.

## Task interface

`just check` is the gate and must pass before you commit. `just ci` adds the deployment contracts,
cross-compilation and the container image. Discover the rest with `just --list`; run `just` with
stdin from `/dev/null`. Stage regenerated Grafana artifacts before `just check`: its regeneration
comparison checks the working tree against the index. GitHub API metadata validation and the full Docker/kind orchestration stay
workflow-only and have no recipe.

`just bump-module-major` is the one `[confirm]` recipe - stop and ask, never pass `--yes` or
`JUST_YES=1`.

Narrow the suite with the filter argument: `just test TestFetchGateways`.

Off the Mac, when the remote gate runner is installed (`which gaterun`): `gaterun -- just ci` (or
`just check`), paced across lanes with `gaterun-slot <lane> -- gaterun -- just ci`; `.gaterunner.yaml`
is its contract. Run one test or package locally.

## Tracking work

Work is tracked in this repository's GitHub issues; they are the only queue. Do not add a checklist
file such as `ROADMAP.md` beside them.

- A pull request body says how it relates to an issue, on a line of its own: `Closes #N` with the
  `Acceptance evidence` table from `.github/pull_request_template.md` (one row for each acceptance
  criterion of that issue), `Refs #N` when it only relates to one, or `Task: none, <reason>`.
- A pull request that changes `.github/workflows/` or `.github/actions/` carries no closing keyword:
  write `Task: none, <reason>`, with `Refs #N` beside it if you like.
- Contributors never need the project board or the tracker tooling; a maintainer handles both.
- Issues and pull requests are public, so they carry no real identifiers: no email addresses,
  handles, account ids, host or device names, addresses or coordinates. Write the shape, not the
  instance.
- Older commits, docs and tests cite legacy board ids such as `OPN-0001`. `just issue <ID>` prints
  the issue each one became; `.github/tracker/id-map.json` is the map.
- Any `#NNN` **below 656** in code comments, old docs or legacy task text is a retired GitHub issue,
  not a task: those issues were deleted, so `gh issue view <N>` 404s. Bodies and replies for the
  deleted set live in `archive/github-issues-2026-08-14.json` (redacted; `archive/README.md` has the
  placeholder mapping). Numbers from 656 to 921 are pull requests and a few live issues; imported
  issues start at 922.
- **The GitHub issue tracker stays enabled deliberately**: outside contributors file issues there
  and Renovate's dependency dashboard lives there. A maintainer triages an externally filed issue
  before it enters the queue.

## Design invariants

- **Collection is decoupled from the scrape.** A poll scheduler (`internal/collector/scheduler.go`)
  runs each of the 82 sub-collectors' `Update()` on its own volatility tier (docgen keeps that
  figure current; do not hand-edit it)
  (`internal/collector/interval_tiers.go`: fast 15s / medium 60s / slow 5m / cold 15m) into an
  in-memory snapshot (`snapshot.go`). Serving `/metrics` and the OTLP bridge both *replay* that
  snapshot; the request path makes no live API call. Never reintroduce a fetch on the request path.
- **Sub-collectors register themselves from `init()`**, appending to the global `collectorInstances`
  slice. Adding one is a file plus its `init()`, not an edit to a central list.
- **`internal/webui` must never call `Gather()` on the live registry.** The operator console renders
  only from `internal/metricsnap`, `collector.StatusTracker` and the API-client cache view; gathering
  there would trigger a firewall scrape from an unrelated page load.
- **`internal/logship` and `internal/flow` are push-based.** OPNsense, Zenarmor and NetFlow send to
  us; nothing there polls.
- Every env var is prefixed `OPN2OTEL_`, except the standard OTel SDK pair
  `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`, which the log shipper
  honours unprefixed (`docs/log-shipping.md`). The `*_FILE` secret vars also accept legacy unprefixed
  aliases (`OPS_API_KEY_FILE`, `OPS_API_SECRET_FILE`, `PYROSCOPE_AUTH_USER_FILE`,
  `PYROSCOPE_AUTH_PASSWORD_FILE`) for backwards compatibility, with the prefixed form winning.

## Recurring defects

What a review pass should look for:

- **Modelling a payload shape upstream cannot produce.** Verify against the OPNsense controller or
  script that builds the payload, and check whether the key is conditional, before writing the
  struct. Run the canary triage verdicts before reaching for `drop` or `chase`.
- **Flow correlation** (`internal/flow/`): orientation by arrival order, endpoints paired by
  position, NAT'd conversations counted twice, a window partial compared with a whole connection.
  Test-first regardless of change size.
- **Alert rules that fire on one sample.** State the metric's floor and normal range, and give
  every rule a sustain and hysteresis. A threshold below the floor can never mean anything.
- **Decoded and dropped.** `just fieldaudit` (also a unit test) reports unmarshalled struct fields
  never read; an exemption needs a written reason.
- **Public metadata.** Golden schemas are structure-only. Redact credentials at the boundary, not
  the sink.

## Conventions

- **The vendor directory is committed.** Run `just sync-vendor` after any `go.mod` change.
- Release binaries are static: `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X main.version=..."`.
  A local `just build` embeds `local-test`; GoReleaser uses the git tag.
- `version.txt` and `CHANGELOG.md` are managed by release-please from conventional commits. There is
  no "changes from upstream" list to maintain - the README carries a short hard-fork notice and
  nothing else.
- `misspell` and `revive` are on, `unused` is off, deliberately.
- Never hand-edit generated output: content between `<!-- docgen:begin/end -->` markers, the
  docgen-generated doc pages, `grafana/dashboard*.json`, or the alert manifests. Edit the builder and
  run the generator.

## Physical NIC and engine-health sources

The traffic REST endpoint enumerates configured interfaces: an unassigned PPPoE physical parent
can be absent even though its driver discard counter is nonzero. Assigned physical NICs can
already expose the same value through input queue drops. The optional node_exporter textfile
companion supplies driver and queue telemetry without adding SSH to the exporter.
Zenarmor health crash/stall figures are daily retained-log counts, not lifetime counters; its
health API invokes check-and-send, and its shell health check can stop services. Never poll either
as a read-only metric source. The opt-in `deploy/textfile/zenarmor-textfile.py` reads local logs
and emits daily gauges through node_exporter; it is independent of the flow receiver and NIC
script. Failed or bounded-out scans omit counts and publish collection health, never zero events.

## Deeper references

- `reference/adding-a-collector.md` - read before adding a collector, a `Fetch*` method or a new
  OPNsense endpoint. Holds the eight-file registration contract and the two 404 caching rules.
- `reference/canary-triage.md` - read before acting on a `cmd/apidrift` finding or editing
  `opnsense/testdata/schemas/exemptions.json`.
- `grafana/tabs/AUTHORING.md` and `grafana/README.md` - read before adding dashboard panels or
  alert rules.
- `docs/compatibility.md` - the record of data upstream removed and the support window it is kept for.
- `reference/unbound-histogram-alternatives.md` - read before changing Unbound histogram
  representation; covers source precision, censored tails, reset epochs and transport limitations.
- `reference/live-api-contracts.md` - read before running the live contract suite; it shares the
  on-demand canary's lab lifecycle and keeps missing CI configuration from passing as a skip.

## Maintainer code review

Enrolled Personal maintainers use the centrally managed `kodus-review` skill and authenticated
adapter plus a separate independent local agent review before committing code. Freeze the exact
base and candidate trees (including new files), sanitized intent and applicable central rules in
the review evidence. Pin the central rules source by full commit, selected rule IDs and rendered
SHA-256 in the local brief; verify that pin and the brief hash in the local review. Private rule
context stays local and must not be copied into this repository or assumed to reach Kodus.

Both components must pass for the same candidate identity. The independent agent inspects the
full frozen candidate and base context against the intent and pinned rules; Kodus context is
partial/unknown. Adapter exit zero alone is not combined approval. Missing, incomplete, unknown,
unsupported or oversized reviews are no verdict: resolve or split into fully covered candidates,
never omit changed files or substitute one component. Resolve verified findings and record their
dispositions; unresolved critical, high or major findings block landing. Root-authored additions
need their own exact-candidate review. Hosted reviews are supplementary and do not replace either
local component, required tests, deployment authority or human approvals.

Public contributors need no private maintainer configuration or paid review account; normal
contribution and hosted-check requirements still apply. Never submit backup repositories,
credentials, private fixtures or customer identifiers to an external reviewer.

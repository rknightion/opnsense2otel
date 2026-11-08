# Pull Request

## Description

Summarize the change and link the issue it addresses. Include relevant motivation/context and any dependencies this change requires.

Refs #<number> (or "Task: none, <reason>")

## Type of change

Delete the options that don't apply.

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation only

## How has this been tested?

Delete if not relevant. Describe the tests you ran, and how to reproduce them.

- [ ] `go test ./...`
- [ ] Manual verification against a live OPNsense box

## Checklist

- [ ] If I added or changed a collector/metric, I ran `just docs` and committed the regenerated output. I did **not** hand-edit anything between `<!-- docgen:begin/end -->` markers — those are generated files (see `docs/metrics/metrics.md`, `docs/reference/collectors.md`, `docs/configuration.md`).
- [ ] If I added or changed a metric, I added the corresponding Grafana panel and ran `just dashboard` (see `grafana/README.md`).
- [ ] If I changed `go.mod`, I ran `just sync-vendor` and committed the vendor diff.
- [ ] I commented my code, particularly in hard-to-understand areas.
- [ ] I added or updated tests that prove the fix or feature works.
- [ ] Local gate is green: `just check`. (These are exactly the bare-toolchain jobs `ci-success` requires; a Docker build job also runs in CI and can optionally be reproduced locally with `docker build .`.)

## Issue

<!-- Closes #<number> on a line of its own for each issue of this repository this change completes (Closes owner/repo#<number> means the same). Refs #<number> when it only relates to one. A change with no issue says "Task: none, <reason>" on a line of its own. -->

## Acceptance evidence

<!-- One row per acceptance criterion of every issue this change closes, copied exactly from the issue's "- [ ] ACn" list. Evidence class is code, live or browser. In Proof, put repository paths in backticks, and write anything else (a command, a read-back, a run) without them. Delete the example row. Leave the table out only when the change closes no issue. -->

| Issue | AC | AC text | Evidence class | Proof |
| --- | --- | --- | --- | --- |
| #N | AC1 | The acceptance criterion, word for word | code | `path/to/the_test.go` TestWhatItProves |

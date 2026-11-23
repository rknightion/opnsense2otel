# Triaging a schema-canary finding

Every finding from the dispatch-only live-box canary (`cmd/apidrift`,
`.github/workflows/live-canary.yml`) gets exactly one of five verdicts.

**Ask "did upstream actually change?" first.** Four of the five presuppose it did. **box-state** is
the one that does not, and it is the most common answer in practice - reach for it before the others
or you will force-fit `drop` onto a live tunnel that merely fell over.

- **box-state** - the box has nothing to report, so the key is absent. Not drift, and **never a code
  change**. A missing path proves nothing on its own: an endpoint with no IPsec SAs, no nginx cache
  node, no reporting subsystem or an empty vnstat DB legitimately omits the key. Confirm against
  upstream *source* that the key is conditional, then either exempt it with a `missingOK` entry whose
  prune trigger names the **box state** (not a release), or fix the testbed so the data exists.
  Prefer fixing the testbed when the field backs an exported metric: an exemption there blinds the
  canary to real drift on a consumed field.
- **absorb** - the payload changed representation only (a number arrived as a string, an object as an
  array of one). Flex types and `KindNumeric` usually already handle it; retype the field.
- **chase** - the data moved or was renamed. Write a tolerant reader: keep the legacy field, add the
  new one alongside it, resolve new-wins-else-legacy in an accessor. Template:
  `opnsense/health_check.go`.
- **drop** - upstream removed the data. Keep the legacy field for the length of the support window;
  the metric reads absent or zero on newer releases. Record it in `docs/compatibility.md`.
- **opportunity** - new data not modelled yet. Roadmap candidate, not a bug: exempt it via
  `knownExtraTopKeys` so the canary stops flagging it.

## Rules

- **Verify against upstream source before assigning any verdict.** Read the controller or script that
  builds the payload and check whether the key is conditional. Two canary runs disagreeing about the
  same key is a box-state tell, not intermittent drift. Guessing here is how a phantom generation
  gets modelled: `metadata.subsystems` was modelled from a release shape upstream has never
  populated, which cost two fabricated fixtures and a permanently dead branch.
- **The support window is current + previous stable OPNsense.** Never version-sniff - resolve by
  payload *shape*. Never remove a legacy field while a release that still sends it is in the window.
- **A fixture must never encode a shape upstream cannot produce.** Derive fixtures from a real
  capture (`just capture`) or from the source's own branches. A deliberately synthetic fixture
  pinning parser tolerance says so in its case comment.
- **`opnsense/testdata/schemas/exemptions.json` is the compat ledger.** Every kept-legacy path gets a
  `missingOK` entry (the prefix form `section.*` is supported) naming the generation it belongs to
  and the trigger that will let it be pruned. Unmodelled new top-level keys go in `knownExtraTopKeys`.
- Run `just schemas` after changing structs. Goldens are structure-only and must never contain
  response values.

## Known-blocked items (no verdict, a ledger entry)

A required coverage path or a skipped parameterised endpoint that the lab can never produce is not drift,
and it must not keep the drift issue open. Record it in `opnsense/testdata/schemas/known-blocked.json`
(per probe target, with a `reason` and the box state that `unblockedBy`) instead of leaving a warning
nobody can clear.

- A ledgered item moves to the informational "Known-blocked" report section and stops setting the warning
  state, so a run with nothing else wrong closes the issue. It is still named on every run.
- Entries are scoped to `nightly` and `release-vm`; a run naming no target applies none.
- Every entry names the box state its reason claims, in `requires`, and the run must show it. "Unverified"
  alone confirms nothing, because a real row with a renamed key can read as unverified too. A path entry
  takes `ipsec-down`; a skip takes `absent:<endpoint>` (that parameter source answered 404). A missing or
  unknown `requires`, or trailing data after the JSON, makes the whole ledger unusable, so everything warns.
- `ipsec-down` is one run-wide fact, evaluated once from row counts and shapes; no response value reaches a
  report, fingerprint or log. All three endpoints must agree: phase 1 serves at least one row and every row
  is a configured connection (`ikeid` set) with `connected` false and a null `install-time`, the SAD has no
  row but setkey's exact synthetic placeholder, and the SPD has no rows. If any endpoint contradicts that, was
  not probed or has unknown facts, no IPsec entry is confirmed and every unverified IPsec path warns. Upstream
  can serve an empty SAD or SPD after a failed backend call, so no endpoint is trusted alone. A tunnel that
  changes state mid-sweep warns; that is accepted.
- The ledger cannot rot. An entry the run contradicts is reported under "Stale known-blocked ledger
  entries" and **warns**: its required state did not hold, the path was verified (or is missing, or changed
  type), it is no longer a required path in `coverage.json`, the endpoint was probed instead of skipped, or
  it was not probed at all. Prune or fix the entry.
- A phase 1 row that is connected with a null `install-time` (an IKE SA with no child SA) warns on purpose:
  that state is not the one the entry records, so read it as a half-up tunnel or as drift.
- Everything else still warns on a ledgered endpoint: an unledgered required path or skip, breaking drift,
  a missing path, an unexpected key, a probe error.
- Only a path coverage.json classes as required and not opaque belongs here.
- Box state that flaps (the IPsec tunnel on the devel box) takes `"intermittent": true`, set per target and
  never on a permanently blocked one (`release-vm`). A path that resolved against at least one served row is shown as "verified on
  this run" and neither warns nor goes stale, and the findings fingerprint is the same up or down, so the
  tunnel coming and going does not reopen the issue. Every other stale condition still warns on it: no
  longer required, endpoint absent, skipped or not probed. Drift, a missing path, an unexpected key and a
  probe error warn as always.
- Never use it to quieten a path you could fix on the box: the triage rule above (fix the testbed when the
  field backs an exported metric) still comes first.

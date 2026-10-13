---
id: OPN-0129
title: Design native histogram conversion for pre-bucketed flow and Unbound data
status: Done
assignee:
  - loop11-root
created_date: '2026-10-13 08:37'
updated_date: '2026-10-13 10:00'
labels: []
dependencies: []
priority: high
type: spike
ordinal: 82000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OPN-0118 (emit native exponential histograms) remains parked because the OTLP job still includes classical flow delta-ratio and Unbound recursion histograms. On 2026-10-13 Rob chose a separate conversion design before closeout rather than narrowing the all-job zero-bucket criteria. This task produces a reviewed design and implementation packet, not a runtime conversion.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The design traces both real producer-to-OTLP paths and all dashboard, alert, scrape and reset/temporality consumers at a named source revision.
- [x] #2 The design states what information the pre-bucketed inputs retain or lose, whether a faithful exponential representation is feasible, and any approximation, error bound, sum/count, overflow and reset semantics; no synthetic raw observations or invented precision.
- [x] #3 A frozen implementation packet names exact owned files, dependency order, compatibility decisions, discriminating fail/pass tests through the real bridge and live OTLP acceptance queries; infeasibility or a required owner tradeoff is an explicit supported result.
- [x] #4 An independent review challenges numerical correctness and consumer compatibility, with findings resolved or owner decisions listed; OPN-0118 criteria and runtime behavior remain unchanged.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Produce an exact-source design packet without runtime changes; independently challenge numerical feasibility and consumer compatibility; publish the full reviewed design through the task record after integrated G.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
## 1. Status, scope and source identities

Task: **OPN-0129 (design native histogram conversion for pre-bucketed flow and Unbound data)**. Design source revision: **1f294272d0d41ceea2a2902ad32846f8745502a9** of `rknightion/opnsense2otel`. References to exporter paths and line numbers in this packet mean that immutable revision, not current HEAD. Public repository prefix:

`https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/`

Pinned deployed library versions from `go.mod`/vendor: Prometheus client_golang **v1.25.0**, client_model **v0.6.3**, common **v0.72.1**, OTel Prometheus bridge **v0.72.0**, OTel metric SDK/exporters **v1.47.0**, OTLP proto **v1.11.1**. The actual vendored code, not upstream HEAD, determines current conversion behavior.

Primary upstream sources were fetched through GitHub API (`gh api`, raw contents at pinned commits) on 2026-10-13:

- NLnetLabs/unbound: **18d8676e9c975ef4c8412265b0225f5bbd1369f7**.
- opnsense/core: **2ae9689a560d41f33d511767f27fe0608e8d01b7**.
- open-telemetry/opentelemetry-specification: **bb85bf44e2c032d9958c10b78dcb88cf36e40917**.
- prometheus/docs: **bb636b7918b0cdd4961d43cf6dd57cc9801a4148**.

These are current upstream identities, **not evidence that every supported OPNsense release ships those exact revisions**. Before implementation, compare the relevant routines in supported release versions; no live firewall or tenant was contacted by this design lane. Service/query documentation was retrieved through Firecrawl, not treated as deployment configuration proof. Full citations are in §10.

OPN-0118 (emit native exponential histograms while retaining all-job zero-bucket acceptance) remains unchanged and parked. Its all-job `_bucket` count must become zero with native histogram samples actually present; neither removing the two metrics, narrowing the job filter, accepting missing Unbound data, nor excluding their families satisfies it. Existing observed duration histograms are outside this design's runtime scope.

**Frozen design conclusion:** a generic faithful classical-map-to-exponential conversion for both inputs is not possible. Flow can instead collect native buckets from real arrivals. Unbound needs an owner decision before a deployable two-family conversion packet can be frozen. §§6–8 give exact conditional ownership, dependency order and proof contracts; they do not authorize blocked work.

## 2. Both real producer-to-OTLP paths

### 2.1 Flow delta ratio

1. Receiver lanes produce NetFlow and Zenarmor records. `main.go:897–906` wires the correlator's emit callback to `collector.Flow.ObserveDelta(r)` **before** flow-log emission. This metric measures correlated records, not raw arrivals or log-delivery success. Log truncation does not truncate the histogram.
2. `internal/collector/flow.go:ObserveDelta` forwards to `internal/flow/deltaratio.go:DeltaRatio.Observe`. It ignores records without **both** source counters. `Repairs.WindowPartial` is excluded and increments `excluded`; payload-byte fallback is still included. The interface comes from `interfaceLabel`, not a new source/interface taxonomy.
3. `ratioOf` computes `float64(NF.Bytes()) / float64(Zen.Bytes())`. Zen bytes equal to zero yield the existing finite **101** sentinel, including the both-zero case. This is *not* IEEE infinity. NF zero with nonzero Zen produces zero. Inputs are already float64-converted integers; an implementation must not promise exact rational byte ratios.
4. Under one mutex, `ratioCell` increments count, accumulates the float64 sum and one of 11 finite intervals plus overflow. Bounds are `{0.9,1,1.05,1.1,1.25,1.5,2,3,5,10,100}`. `Snapshot` builds cumulative counts and does **not** reset; overflow appears only in total count. The original values and the zero-Zen/real-large-ratio distinction are no longer recoverable from `HistogramData`.
5. `internal/collector/flow.go:collectExtras` emits `MustNewConstHistogram` with labels `(interface, opnsense_instance)`, plus the separate exclusion counter. No metric exists until an eligible record creates an interface cell.
6. The flow collector declares the fast tier (15 seconds, `internal/collector/interval_tiers.go:229`) and copies accumulated totals into the immutable collector snapshot. Effective cadence also honors the export-lane clamp, so the default single 60-second OTLP lane can clamp that declared 15-second poll to 60 seconds; use the effective interval metric rather than assume 15 seconds. It makes no API request. Histogram state lives for the process, not for a poll interval.
7. `internal/collector/collector.go:Collect/collectLane` replays the snapshot. Both HTTP scrape views and OTLP views use this replay; neither refetches nor calls `Observe`.
8. `main.go:1503–1534` supplies the registry through `metricsnap.Recorder.Tee` or disjoint `TeeLane` gatherers to `internal/telemetry/provider.go:Start`. The optional fast export lane contains flow; the base lane excludes it. This must stay disjoint.
9. The real bridge is `vendor/go.opentelemetry.io/contrib/bridges/prometheus/producer.go`. The current DTO has only classic buckets, so `convertHistogram` creates `metricdata.Histogram[float64]`, cumulative temporality. `convertBuckets` subtracts adjacent cumulative values and adds the implicit overflow population `Count-lastFinite`.
10. Both vendored HTTP and gRPC metric exporters' `internal/transform/metricdata.go` encode that data as an OTLP explicit histogram. The downstream OTLP-to-Prometheus receiver exposes classic suffix series. No OTel SDK histogram view or histogram-aggregation environment variable reconstructs observations in this external producer path.

### 2.2 Unbound recursion histogram

There is no `opnsense/unbound.go` at this revision. The actual client is **`opnsense/unbound_dns.go`**.

1. Unbound `services/mesh.c:1639–1641` increments `replies_sent`, adds the wait to `replies_sum_wait`, and calls `timehist_insert` consecutively. The histogram and mean describe the same population; query totals/cache-miss totals are not interchangeable denominators.
2. `util/timehist.c:dosetup/timestwo` creates 40 buckets. Upper bounds in seconds are `2^k / 1,000,000` for `k=0..19` (1us through 524288us), then `2^j` for `j=0..19` (1s through 524288s). The transition is **0.524288s to 1s**, not 1.048576s. `timehist_insert` selects the first upper-inclusive bound; values exceeding every bound are added to the **last** bucket.
3. `daemon/remote.c:print_hist` prints all per-bucket counts, not cumulative counts; printing is gated by extended statistics. `print_stats/timeval_divide` prints the truncated mean to six decimal places. The total wait itself is not printed.
4. OPNsense `src/opnsense/scripts/unbound/wrapper.py:82–94` invokes `stats_noreset`, reshaping flat histogram keys into `{from:[sec,usec], to:[sec,usec], value:string}` objects. `DiagnosticsController.php:statsAction` wraps available data in `status:"ok"`; unavailable stats return failed status. The local endpoint registry (`opnsense/client.go:125`) maps this to GET `api/unbound/diagnostics/stats`.
5. `FetchUnboundOverview` decodes presence, uptime and mean. `buildUnboundRecursionHistogram` sorts upper bounds, requires a start at zero and tiled intervals, clamps negative bucket counts to zero, accumulates uint64 counts, and sets `Sum=avgSeconds*float64(Count)`. Current code does not validate every integer/range/overflow case or exact upstream shape. It does not retain original `from`/`to` pairs beyond constructing the cumulative map.
6. `internal/collector/unbound_dns.go:Update` uses the histogram's **own** `Present` flag, not the sibling extended-statistics flag, to emit `MustNewConstHistogram`, with only `opnsense_instance`. Stats-unavailable success emits service state only; histogram-absent success drops histogram output instead of fabricating zero buckets. Defaults on newer releases can legitimately omit it.
7. Unbound is absent from the nondefault tier map, so it polls at the medium default (60 seconds). The same snapshot/replay/recorder/bridge/exporter pipeline in flow steps 7–10 emits a cumulative explicit OTLP histogram. With split exports it belongs only to the base lane.

### 2.3 Shared conversion and failure/reset behavior

- `snapshotStore.put` replaces successful-empty or partial-nonempty outputs, but retains last-good data after an error with no metrics. It records poll, snapshot and success clocks separately. Native conversion must preserve these rules; replayed stale cumulative histograms are not new measurements.
- The bridge chooses classic versus exponential **once per family from the first metric**: `isExponentialHistogram` checks nonzero zero threshold/count or any positive/negative span. A schema field alone is insufficient. Empty native histograms require the client constructor's length-zero-span discriminator or a positive threshold. Mixing native and classic DTOs among interfaces in the same family is unsafe.
- Native DTO schema equals OTLP scale. Positive Prometheus index `k` maps to OTLP offset `k-1`. Sparse spans/deltas become **dense** OTLP bucket arrays, inserting zero counts through gaps. Sparse map size is not the payload-size bound.
- The bridge always supplies cumulative temporality. Created timestamp overrides its package-level `processStartTime`; without it the apparent start belongs to the exporter process, not Unbound's source epoch. SDK reader/exporter temporality selectors do not reaggregate these external producer points.
- Existing flow counters reset on exporter restart, not snapshot/collect. Unbound counters can reset on resolver restart or an administrative `stats`/clear, including without an uptime reset. The existing source has no explicit statistics-reset epoch.
- A conversion must not synthesize a new start timestamp every poll. Resolver uptime can establish a tentative boot epoch but cannot prove an intervening statistics clear. Poll-to-poll count/bucket decreases detect some resets, not a reset followed by enough traffic to exceed the previous totals. Complete reset detection from these fields alone is impossible.
- Created timestamps and reset policy need consistency tests through the real receiver. An owner may preserve the existing bridge fallback behavior and its limitation, but must not label it source-exact or delta temporality.

## 3. Consumer inventory at the named revision

Repository-wide searches included base names, suffixes and general histogram functions; a negative match is explicitly bounded to this checkout, not external users.

| Consumer | Current dependency | Required compatibility action |
|---|---|---|
| `grafana/tabs/flow.py:171,225–260` | `has_flow_delta_ratio` sentinel uses `_bucket`; p50/p90/p99 aggregate `(opnsense_instance,le,interface)`; heatmap reads bucket rates | Sentinel must recognize both forms. Quantiles and heatmap need classic-or-native branches with interface/instance preserved. Keep excluded rate line and the payload/window-partial explanation. |
| `grafana/tabs/dns_unbound.py:192–217` | p50/p90/p99 and heatmap read `_bucket`, grouped `(opnsense_instance,le)` | Dual-form query support; preserve absence contract. Do not show an all-zero histogram when extended statistics are off. |
| `grafana/build_dashboard.py:51–62` | Both bases are coverage exceptions because suffixes alone are queried | Update obsolete explanation/coverage exception only when native branches actually reference the bases; never weaken the coverage gate. |
| `grafana/build_dashboard.py:1175–1188,1344–1360` | Existing API/server duration queries already use classic-or-native, including heatmap `or on` identity matching | Useful pattern, not sufficient proof of new metric compatibility. Do not alter observed duration histograms or their tests. |
| `grafana/builder.py:_query/heatmap/sentinel`, `grafana/sentinel_contract.py` | Prometheus outer query wrapping, heatmap range format and instance-scoped presence contract | Check actual generated expressions and browser data frames. Histogram samples cannot be passed through arbitrary float-only `max` aggregation. |
| `grafana/dashboard.json`, `grafana/sentinel-contract.json` | Generated copies of the above | Regenerate from builders, never hand-edit. Companion dashboards should be checked for unintended changes. |
| `grafana/alerts/build_rules.py`, generated Grafana-managed manifests | **No direct consumer of either family found**, and no quantile-based alert found for them | No new alert or recording rule needed. Existing DNS mean/median/health rules, flow repair/coverage rules and their runbooks remain unchanged. Do not invent an existing percentile alert. |
| `grafana/annotations.py:603` | Delta ratio is deliberately not an annotation | Preserve this rationale; no event detector is added for histogram buckets. `internal/annotations` has no matching-family consumer. |
| `internal/server/metrics.go`, `promhttp`, vendored common `expfmt` | HTTP scrape views; text/OpenMetrics classic rendering | Preserve every old finite `_bucket`, `+Inf`, `_sum`, `_count` and label set on `/metrics`. Native-only DTOs would still produce an implicit `+Inf`/sum/count in text, but **lose finite buckets**. Test actual HTTP responses, not DTO presence alone. Protobuf/native negotiation needs a separate expectation. |
| `internal/collector/snapshot.go`, scheduler, collector lane views | Immutable metrics, last-good retention, medium/fast lane ownership | Dual metrics must be immutable snapshots, not live pointers changing after poll; gather cannot fetch source APIs or observe values. |
| `internal/metricsnap/recorder.go`, `budget.go` | Captures full DTOs; merged family set, budget counts `len(mf.Metric)` | Preserve full native fields. Budget is logical series-per-family/label set, not classic wire-bucket cardinality. Do not claim the local budget proves downstream savings. |
| `internal/webui/status.go` and metrics pages | Recorded metrics only; mean duration uses sum/count for exporter latency families | No direct flow/Unbound histogram computation found. Generic DTO display/capture should not drop native fields or claim new source precision; no live Gather is introduced. |
| `scripts/docgen/main.go`, `selfmetrics.go` | Static declaration/type extraction | Both const families currently appear as **Gauge** in the generated metric catalogue although emitted DTOs are histograms. If introducing a dual-metric constructor, teach emission type inference about it and regenerate; do not add an exception to hide the mismatch. |
| User docs | Current config says const families stay classic; flow docs explain accounting basis; Unbound help claims exact/half-us sum and no overflow | Correct claims with citations and bound. Document OTLP versus scrape forms and interpolation changes. Generic external suffix consumers are unknown and require release notes, not a guarantee of zero usage. |
| Existing native/collector/client tests | Duration bridge shape and classic bucket retention; source exclusions/bucket sums; Unbound presence, tiling, sum reconstruction | Retain all existing assertions except ones changed by an approved behavior decision. New proofs in §7 must exercise the actual new families. |

Relevant durable documents (immutable absolute public references):

- [Configuration, histogram contract](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/docs/configuration.md#histograms-are-exported-as-native-base2-exponential-histograms-over-otlp) — the source text is around lines 278–291; the heading link may not be a generated anchor.
- [Flow semantics](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/docs/flow.md#what-source_byte_delta_ratio-actually-measures).
- [Generated metric catalogue](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/docs/metrics/metrics.md), rows for the two bases.
- [Compatibility](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/docs/compatibility.md), Unbound extended-statistics availability.
- [Dashboard authoring contract](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/grafana/tabs/AUTHORING.md) and [Grafana assets contract](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/grafana/README.md).

## 4. Information retained, mathematical feasibility and error bounds

### 4.1 Meaning of faithful conversion

For this decision, faithful means retaining the count, available sum and source cumulative counts at **every original bound**, without assigning unavailable within-interval positions or creating observations. Exponential output's conventional quantile interpolation is already an estimate, even when bucket counts are exact. A coarsening may preserve count/sum and certain CDF checkpoints, but must name which source bounds it discards. A finer scale is not higher source accuracy when derived from coarse buckets.

OTLP positive bucket `i` at scale `s` covers `(2^(i*2^-s), 2^((i+1)*2^-s)]`. Prometheus uses upper-bound index `i+1`. Zero threshold is arbitrary but supplies only one contiguous interval around zero. A classical interval can be moved exactly into one target interval only if the entire source interval fits; otherwise its mass must be split, and the split is not identifiable.

### 4.2 Flow: exact old-map conversion is not available; direct observation is

Retained: integer cumulative bucket populations, total count, float64 accumulated sum, interface attribution. Lost: zero population versus other values in the first bucket, values within each interval, maximum/minimum, distinctions between 101 sentinel and real ratios above 100, per-record times and accounting bases.

Exact float64 non-identifiability witness inside classical `(1.1,1.25]`: integer-byte populations A `{72/64,79/64}` versus B `{75/64,76/64}` produce ratios `{1.125,1.234375}` and `{1.171875,1.1875}`. Every ratio and serially accumulated sum is exactly representable: both sums equal `0x1.2e00000000000p+1` (2.359375), with identical count and **all** classical buckets. At schema 3, boundary `2^(2/8)=1.189207115...` splits A but not B. A converter that reads the exact retained float64 sum still cannot distinguish them. The earlier decimal witness `{1.12,1.24}` versus `{1.18,1.18}` is rejected: actual sums differ by one ULP, so a tolerance would not establish equal converter inputs.

Most finite source bounds (e.g. 1.05, 1.1, 1.25, 3, 5, 10, 100) are not standard exponential boundaries at any supported scale. Zero threshold can align one bound, not restore the missing splits. The first interval touches zero and the overflow interval is broad, so generic midpoint/upper-bound placement has no global finite relative error guarantee.

**Feasible prospective route:** update a native accumulator in the existing `DeltaRatio.Observe` critical section from the **same real `ratioOf` result**, in parallel with the classic accumulator. This is not synthetic observation replay. It changes the retained state going forward; no state is reconstructed retroactively. Exporter restart is already the accumulator epoch, so deployment begins a new cumulative sequence without a state migration.

Preserve the existing 101 sentinel unless the owner separately changes it. Native output will place that finite sentinel in a finite exponential bucket, not an infinity bucket; consumers must not interpret it as a measured 101-fold discrepancy. Native tail quantiles will differ from classic overflow interpolation (which returns the last finite bound when the quantile lies in the highest `+Inf` bucket). This is a documented compatibility change, not an accuracy claim about the sentinel.

For actual finite ratios the base2 bucket membership is exact to the float64 observation mapping. With fixed schema 3, adjacent bounds differ by `2^(1/8)=1.0905077...`; a quantile remains somewhere in its bucket, with up to that width of bucket uncertainty, **not** a guaranteed 9% error against every population or against the classic interpolated percentile. For a nonzero integer NF/Z ratio the float64 range is roughly `[2^-64,2^64]`; zero is separate and 101 lies within it. A fixed-schema-3 native accumulator has roughly 1025 possible positive indices and a similarly bounded dense span per interface, rather than an unbounded arbitrary-double range. Existing interface cardinality is not newly bounded by this design.

### 4.3 Unbound: base2 in microseconds is not base2 in seconds

The 20 microsecond upper bounds are rational values with an uncancelled factor of 5 in the denominator when converted to seconds. They cannot equal `2^(n/2^s)`: if such a power is rational it must be a power of two with integer exponent, whereas these values are not dyadic rationals. This is a structural mismatch, not float64 rounding noise; scale 8, 20 or a midpoint cannot fix it.

The fixed second bounds `1,2,...,2^19` align with schema 0. The `0.524288→1` transition can be represented with a zero threshold of 0.524288 and a partially overlapping positive bucket whose occupied region starts above that threshold, but only by merging **all first 20 source buckets into zero**. This preserves count and some coarse checkpoints while losing essentially the useful subsecond latency distribution. It is not an acceptable default masquerading as faithful conversion.

Numerical witness respecting integer-microsecond durations: inside `(8us,16us]`, A `{9us,16us}` and B `{12us,13us}` have identical count and total wait (25us), therefore identical printed mean and source buckets. At schema 0 in seconds the boundary `2^-16s=15.2587890625us` splits A but not B. The target population is not recoverable even with the total wait known exactly.

**Overflow/censoring:** `timehist_insert` deposits any wait above 524288s in the last source bucket `(262144s,524288s]`. Therefore the observed final population is actually `(262144s,+∞)` subject to implementation duration limits; the current client's assertion that nothing can exceed the top finite bound is false. A source final count does not determine how many lie at 524288, beyond it, or much farther out. Assigning all to a finite native bucket introduces an unsupported upper bound; assigning them to the native infinity bucket invents infinite observations. No raw durations or maximum are exposed. The ordinary native `+Inf` bucket is not a general arbitrary-threshold overflow bucket.

Possible exact aggregation into a very broad zero bucket would discard the shape; a shift to microsecond units changes the metric's units/name and does not repair the transition or censored tail. Custom boundaries (NHCB schema -53) preserve classical interval information but are **not OTLP exponential histograms**; the pinned const-native API accepts only schemas -4..8. The pinned bridge has no faithful NHCB-to-OTLP representation. Mimir's current NHCB guide describes scrape conversion and experimental remote-write 2.0, not a drop-in change to this application's OTLP bridge.

### 4.4 Sum/count and reset semantics

Flow: preserve existing float64 sum accumulation and classic uint64 count, including 101 sentinel contributions and zero contributions. Native and classic must observe the identical eligible population exactly once. Do not compute the native sum from bucket representatives. Existing upstream uint64 byte-counter arithmetic and float conversion limits remain inherited limitations. The preferred client_golang native accumulator below has a narrower total-count domain: `0 <= N < 2^63`, because its hot-index bit shares the uint64 count word. At N=2^63, `Write` masks the visible count to zero and its cooldown wait can stall against a nonzero cold count. It cannot promise unrestricted uint64 lifetime parity. No independent native reset, silent refusal, or classic-count change is approved to handle that boundary. Q2 must select an observable, parity-preserving beyond-domain policy or another implementation before runtime work; the current flow-only instrument proposal is conditional on that decision.

Unbound: `Sum=printedMean*histogramCount` preserves the currently available reconstruction, not an exact upstream sum. For normalized nonnegative `sum.tv_sec=a`, `sum.tv_usec=b`, `N>0`, the retrieved `timeval_divide` computes:

`printedMeanUs = floor(a/N)*1e6 + floor(b/N) + floor((a mod N)*1e6/N)`.

There are two truncated fractional-microsecond terms. Consequently `0 <= trueSum - N*printedMean < 2*N microseconds` before float64 parsing/multiplication error. Example: `N=3`, total wait `2.000002s`, mean `0.666666s`; reconstructed sum `1.999998s` loses **4us**, or **1.333us per observation**. This disproves both a half-us and a one-us blanket bound. Implementation release comparison must establish whether the retrieved routine matches supported releases.

The reconstruction error grows with cumulative N and can change between snapshots. Reconstructed sum can decrease without a histogram count reset when truncation error changes. Thus `rate(_sum)/rate(_count)` is not guaranteed to be an exact duration mean or even immune to apparent sum resets. The error in the difference of two reconstructed sums is bounded conservatively by `2*(N1+N2)us`, plus floating-point error; the relative error over a low-traffic window can be arbitrarily large. Existing `recursion_time_avg_seconds` is the printed mean gauge and remains useful, but must not be substituted for a window mean.

Preserve source counts, not query totals. Validate monotone cumulative counts and arithmetic overflow before unsigned subtraction or conversion to signed native bucket counts. The vendored `NewConstNativeHistogram` uses signed int64 population summation; it does not provide general arbitrary-uint64 overflow safety. Negative counts, invalid durations, duplicate/reversed bounds, nonfinite mean and count overflow must not become plausible native data. Refusal should be observable and should not silently revert one interface or poll to classic OTLP output. Exact refusal metric/log ownership is Q6.

### 4.5 Approximation choices: bounded statements, not approval

No approximation is selected by this packet. These are decision alternatives and their limits:

- **Upper-bound bucket placement:** choose a target bucket from each source upper bound, preserving integer mass and the independently reconstructed sum. No repeated/fractional `Observe`. For a finite source interval `(L,R]`, the population is still only known to lie there. If rounded target upper bound is `U`, `R<=U<base*R`; the enlarged support is at most `(L,U]`. That `base` bound is rounding of R, **not** an error bound for each original value or a quantile. All mass may be on the other side of an internal target boundary; CDF error can reach the entire source interval population. The first interval's relative value error and censored tail error are unbounded without additional source support assumptions.
- **Uniform/log-uniform splitting:** fractional masses must be rounded and are not observations. These models can move every observation to the wrong side of a threshold and have no distribution-free accuracy guarantee. Larger scale advertises invented precision. Rejected absent explicit owner acceptance of a modelled metric with a different contract/name.
- **Interval-envelope coarsening:** merge source intervals only where fully contained by target buckets, widen zero threshold as needed, and refuse any straddling interval. It can preserve certain CDF checkpoints without invented splits but cannot retain the complete Unbound shape or faithfully place its censored tail. Schema-0/0.524288s zero threshold destroys the very latency range the DNS panels display.
- **Keep explicit source histogram but store NHCB downstream:** potentially preserves the available classical shape and reconstructed sum (not the uncensored raw tail). It requires a supported receiver/transport/storage path, and the all-job zero-bucket/base2 requirement must be checked literally. No receiver capability or tenant setting is proven here. This is a separate infrastructure decision, not a vendor hack.
- **Change upstream to report native buckets, raw durations or exact aggregate state:** potentially faithful but owns code outside this repository and must handle OPNsense upgrades, source reset epochs and raw-event sampling/loss. DNS per-query logs are not established as the same recursion-wait population; do not substitute them without proof.

## 5. Compatibility contract for any approved implementation

1. Keep base metric names and existing label sets if semantics are retained. Changing units, sentinel treatment, source population or modelled approximation requires explicit naming/versioning approval, not just a new histogram type.
2. Preserve classic scrape buckets, sum and count exactly. Native DTO fields can coexist with classic fields in **one** metric per label set. Registering two histogram metrics with the same name/labels is a duplicate, not dual representation.
3. A proposed collector-owned immutable dual metric can call the classic const metric's `Write`, then attach native fields copied from a snapshot, and return one DTO. It must not retain pointers to mutable producer maps/client histogram DTOs or overwrite classic buckets/count/sum/labels. New files and constructor names require docgen type recognition.
4. The pinned bridge will choose native fields and omit classical bounds from its OTLP point. No bridge fork, SDK View, synthetic raw observations or `go.mod` change is needed for the feasible flow path. All members of a family must use the same representation including empty interfaces.
5. Classic-or-native percentiles: calculate classic `histogram_quantile(q,sum by(instance,le[,interface])(rate(H_bucket[W])))` and native `histogram_quantile(q,sum by(instance[,interface])(rate(H[W])))`, then combine results with `or on(opnsense_instance[,interface])`. Prefer classic when both forms exist unless the owner chooses otherwise. Never add both distributions together or allow two copies per interface.
6. Heatmaps need histogram-valued native expressions, not `histogram_count` scalar data. Combine classic bucket-rate and native histogram-rate branches with `or on(opnsense_instance[,interface])`, then prove Grafana's data source decodes the native frame with the actual generated wrapping. `Builder._query`'s float-oriented outer `max without(job,service_instance_id,service_name,service_version)` **ignores native histogram samples**, per the official aggregation specification. This is a pre-existing source-level defect in duration heatmap native branches too; deployed UI impact was not queried here. For the proposed flow heatmap, reuse the **existing** `Builder.heatmap(..., dedupe=True)` parameter and its existing forwarding through `_rich_queries` to `_query`; pass `dedupe=False` only at this already instance/interface-aggregated histogram-valued call site. No builder API or forwarding change is required at this source revision. Both branches must remove transport dimensions in their own sum-by clauses before union. Do not remove the wrapper from every query. Existing duration heatmap corrections require separately authorized scope.
7. Sentinel presence must match base **or** `_bucket` series, scoped to selected `opnsense_instance`, and stay true for an emitted zero-count histogram. Missing producer data stays missing; histogram presence is not an observation-count predicate. Existing `b.sentinel` supports structured `name_regex`, **not a raw custom query**: use `name_regex="opnsense_flow_source_byte_delta_ratio(_bucket)?"`, preserving its injected instance scoping. No new sentinel API is needed.
8. Changing bucket representation changes interpolation from linear classical to exponential native within nonzero exponential buckets. There is no general promise that p50/p90/p99 lines stay numerically identical even for exact source data. Zero bucket uses linear interpolation. Document this separately from conversion loss.
9. Historical dashboards must work for pre-upgrade classic-only, post-upgrade native-only OTLP, classic scrape, both forms, two appliance instances, no-data, and mixed old/new time windows. Scalar `or` precedence, heatmap precedence and instance separation must be verified with actual responses, not source substring checks.
10. Do not share the observed-duration **one-hour reset option** with a dual lifetime flow accumulator. Any reset of one representation only corrupts sum/count parity. Do not infer Unbound's created epoch by recomputing it every poll.

## 6. Frozen dependency order and exact conditional ownership

**Stage 0 — required before implementation:** independent reviewer-high challenges §§2–5; owner resolves Q1–Q6. If Q1 chooses no lossy representation and no upstream/transport change, the frozen result is **infeasible for Unbound with present input**, not an incomplete guessed implementation. OPN-0118 (emit native exponential histograms while retaining all-job zero-bucket acceptance) stays parked. Do not launch a two-family implementation worker on this packet without an appended decision and branch-specific file manifest.

**Stage 1 — only if a flow-only improvement is separately authorized:** preferred feasibility candidate is a parallel native-only **client_golang histogram** per existing `ratioCell`, updated at real observation time under the existing DeltaRatio lock. Use the library's tested float64 mapping instead of a fresh log/ceil implementation near powers of two. A native-only internal instrument is not separately registered; its immutable DTO is attached by the collector dual metric. Factor 1.1/schema 3; fixed zero threshold small enough to exclude every positive byte ratio; no bucket-triggered/timer reset; no max-bucket reset/widening behavior. Its positive/dense span is bounded by the actual uint64-ratio domain. The alternative 160-bucket auto-downscaling policy is not silently selected because it changes resolution across interfaces/time. Creation epoch is stable across snapshots. Supported client accumulation domain is N<2^63; Q2 must settle the beyond-domain behavior or select another implementation before this instrument is runtime-ready. This introduces a Prometheus-client import to `internal/flow`; dependency is already in the module but architecture/memory/range choice requires Q2.

Exact source ownership for this conditional flow candidate:

- Modify `internal/flow/deltaratio.go`: parallel actual-observation native state and snapshot data, stable epoch; keep current classic state, sentinel and exclusions.
- Modify `internal/flow/deltaratio_test.go` and `internal/flow/deltaratio_basis_test.go`: retain all existing semantics; add boundary/extreme/zero/native parity/concurrency/immutable snapshot proofs. No replacement of classic tests.
- Add `internal/collector/dual_histogram.go` and `internal/collector/dual_histogram_test.go`: single immutable classic+native DTO seam, classic/native count+sum equality checked; no live mutable DTO aliases.
- Modify `internal/collector/flow.go`: one dual metric per interface; same desc/labels/presence and snapshot path.
- Modify `internal/collector/flow_test.go`; add `internal/collector/const_native_histogram_test.go`: actual FlowStore→collector→registry→real bridge contract, not a fake replacement producer.
- Modify `internal/collector/native_histogram.go` comments to distinguish the new flow path from unresolved Unbound. Do not change the observed histogram constants/options.
- Modify `scripts/docgen/main.go` and `scripts/docgen/type_resolution_test.go` **only if the emission type inference needs the new seam**; regenerate the catalogue instead of declaring histogram DTOs gauges. Validate actual generated row types.
- Modify `grafana/tabs/flow.py` and `grafana/build_dashboard.py` for native-valued heatmap call-site selection and actual base-name coverage in §5; reuse the existing `dedupe=False` API without modifying `grafana/builder.py`. Add `grafana/tests/test_native_histogram_consumers.py` and update `grafana/tests/test_sentinel_scoping.py` with generated-expression/selection contract tests, not weakened old assertions or redundant builder-API tests.
- Update configuration and flow docs cited in §3, and their generated metric catalogue outputs via generator. Regenerate `grafana/dashboard.json` and `grafana/sentinel-contract.json` (and include other dashboard outputs only if the generator actually changes them).
- Do **not** modify `internal/server/native_histogram_test.go`, telemetry bridge vendor, transport selection, `go.mod`, `go.sum`, alert/rule builders, Unbound runtime or tracker files for this flow-only branch.

**Stage 2 — Unbound decision-dependent, no approved runtime ownership yet:**

- An approved aggregate approximation would need `opnsense/unbound_dns.go`, `opnsense/unbound_dns_test.go`, `internal/collector/unbound_dns.go`, `internal/collector/unbound_dns_test.go`, the shared dual seam/tests, and `internal/collector/const_native_histogram_test.go`. It must keep source integer `from/to` and noncumulative counts available until validation, distinguish source censoring from output support, carry refusal/precision/reset policy explicitly, and preserve classic output independently. These files are a conditional surface list, **not a frozen numerical algorithm**.
- Its consumer stage owns `grafana/tabs/dns_unbound.py`, the new native-consumer tests, and the same generator outputs; docs own configuration/compatibility/metric help corrections and any naming/versioning decision.
- An upstream/native-source or receiver/NHCB decision instead needs a new cross-repository/infra ownership manifest. No exact infra file can be named from this checkout alone. Returning this missing ownership is required, not permission to edit deployment files by guess.
- The sum-error/no-overflow comment corrections in `opnsense/unbound_dns.go` and `internal/collector/unbound_dns.go`, plus schema exemption explanatory text, should be separated from conversion behavior if owner authorizes a documentation-correction task. Exemption behavior is not changed by this design; canary rules apply before any exemption edit.

**Stage 3 — integration:** generate docs/dashboards (`just gen`), run new focused discriminating proofs, then `just check`. The root runs integrated publication G for the design packet; future runtime gate commands are not claimed run here. Runtime CodeRabbit review applies to the future implementation lane under its tier. No source or generated file was changed by this design lane.

**Stage 4 — deploy/query acceptance:** root/operator only, after separately authorized deployment. Record exact candidate SHA, build identity, receiver capability and time windows; execute §8 without accepting empty results as native success. A flow-only change does not close all-job acceptance while Unbound remains explicit.

## 7. Discriminating fail/pass proof packet

No new test was written or run in this design. Each future test below must first be observed failing on the unconverted named source for its intended reason; tests for newly discovered defects should fail on the defect, not merely on missing test scaffolding.

| Proof | Red condition on current/incorrect candidate | Required green evidence |
|---|---|---|
| Actual flow family through bridge | Current FlowStore collector produces `metricdata.Histogram[float64]` | Same family/attrs produces exactly one cumulative `ExponentialHistogram[float64]` per interface after a real `ObserveDelta`; exact sum/count and bucket positions checked. |
| Actual Unbound API fixture→collector→bridge | Existing client/collector emits explicit histogram; fabricated upper-bound mapping fails the equal-input witnesses | Approved decision's shape and arithmetic asserted through real HTTP fixture decoding and the real bridge; or explicit infeasibility remains, with no green conversion claim. |
| Observation rather than rebucketing | Real byte populations `{72/64,79/64}` and `{75/64,76/64}` with exactly equal classic count, float64 sum and every old bucket produce identical native buckets under bad map conversion | Native buckets differ at the schema-3 split; classic snapshots, including bit-exact sum `0x1.2e00000000000p+1`, remain equal. Derive ratios and serial sums from byte inputs; never weaken equality to a tolerance or print a literal as proof. |
| Boundary and zero mapping | Off-by-one map places exact 1 or power of two in next bucket; zero treated as positive | Exact 1 gives Prom index 0/OTLP offset -1 at scale 3; just below/above boundaries and extreme ratios mapped correctly. Zero is zero count; 101 finite sentinel and actual >100 retain count/sum. |
| Source exclusions/coverage | Counting single-source or window-partial records, or excluding payload-basis records | Existing `TestDeltaRatioIgnoresSingleSource`, basis/window-partial tests stay green; native and classic same eligible count, excluded counter unchanged. |
| Immutable snapshot/replay | Calling Gather twice increments counts or mutates previously captured DTO | Observe, snapshot A, observe more, snapshot B; A stays unchanged. Repeated HTTP/bridge gathers do not change population or hit fixture API. Race test concurrently observes/polls/gathers. |
| Sum/count parity | Hourly/bucket-limit reset or separate populations creates native/classic mismatch; preferred client crosses its N<2^63 domain unchecked | Within approved domain, all-zero, steady increments, process restart and large-range native observations keep both count/sum aligned; epoch stable within life, new at restart. Separately exercise the owner-selected beyond-domain policy without driving 2^63 events or hanging Write; no independent reset or silent loss counts as parity. |
| Empty native detection/family homogeneity | A zero-count native is classified classic; first interface hides others' native fields | Bridge returns exponential even for empty snapshots; every emitted member in a family has native discriminator. Absent family still absent. |
| Sparse→dense and index arithmetic | Large gaps encoded with wrong offset/delta or counts shift by one | Verify converted dense array offsets/counts at both extreme occupied indices, zero-filled gaps, no negative counts and reasonable bounded payload length. |
| Scrape compatibility | Native-only replacement loses old finite buckets | Drive actual `/metrics` handler in text and OpenMetrics negotiation on fixture-backed polling; exact old finite buckets, +Inf, sum, count, labels remain. Protobuf native scrape tests assert dual DTO semantics separately. |
| Unbound true shape/censoring | Assuming 1.048576s transition or finite-only last support | Fixture/proof reproduces 40 exact integer-bound pairs with 0.524288→1 transition; last-source bucket's censored support handled per approved policy, never invented as an exact finite tail. |
| Unbound arithmetic/absence/reset | Half-us assertion, negative clamp silently accepted as faithful native, count overflow wrap, newly synthesized zero, created timestamp recomputed each poll | N=3/2.000002s witness pins <2us conservative bound; missing histogram, disabled extended stats and failed stats envelope remain absent; validation refusals explicit; external clear/restart/recovery limitations documented and tested. |
| Dashboard identity/selection | Current flow sentinel hides native-only family, quantiles empty, native heatmap dropped by outer `max`, or both branches duplicate | Classic-only/native-only/both/no-data/multiple-instance fixtures exercise **generated** queries; real PromQL engine and real Grafana/browser evidence show p50/p90/p99, two heatmaps and sentinel state. Parser/unit tests alone are not sufficient. |
| Actual wire serialization | SDK struct shape is green but transport body is explicit or count lost | Bounded local HTTP OTLP receiver decodes real exported protobuf and checks `exponential_histogram`, count, sum, scale, zero threshold, positive offsets and cumulative temporality for actual collector families. gRPC encode path checked likewise if supported in implementation acceptance. |
| All-job closure | Native count green while bucket families remain or Unbound is disabled | Live job-scoped bucket count zero AND expected families/instances contain native samples with real traffic/population, build identity and sustained post-upgrade windows. |

Existing proof references: `internal/server/native_histogram_test.go:TestRequestDurationIsDualHistogram` exercises the real bridge for an observed duration family, **not these two const families**. `internal/telemetry/provider_test.go:TestBridgeParity` and error-isolation tests prove other aspects but do not assert native const shape. `internal/collector/unbound_dns_test.go:TestUnboundDNSCollector_RecursionHistogram/Absent`, `opnsense/unbound_dns_test.go:TestFetchUnboundOverview_RecursionHistogram/Absent/NonContiguous`, and flow/basis tests must remain intact. Proposed Unbound tests may change histogram expectations only after an explicit approved intended behavior is named.

Local runtime surface required in the future implementation lane: fixture-backed API poll + collector snapshot + actual metrics handler + actual bridge/exporter. This repository already has the bridge/server/client seams; use them instead of building an unasked live tenant harness. Browser/live query proof is distinct and root-owned. No new harness task is needed for this design-only lane; if implementation cannot cheaply exercise the real UI/frame path, return that as a follow-up harness requirement rather than claiming the Python tests are browser evidence.

## 8. Live OTLP acceptance query packet (not executed)

Use authorized configured `gcx` context and datasource, without persisting context names, datasource UIDs, tenant/account identifiers or credentials in public task text. CLI syntax was introspected with `gcx metrics query --help`; expressions below are positional, JSON output supported. Evaluate at fixed `T` and fixed range endpoints for reproducibility. Wrap process execution in the operator's normal bounded timeout. Record implementation SHA and deployed build proof alongside redacted query output. `service_version` is not guaranteed on metric resource identity; use exporter build_info/deployment identity, not a guessed label.

All-job literal closure:

```sh
gcx metrics query 'count({job="opnsense2otel",__name__=~".+_bucket"}) or vector(0)' --time "$T" -o json
gcx metrics query 'count by (__name__) ({job="opnsense2otel",__name__=~".+_bucket"})' --time "$T" -o json
gcx metrics query 'count by (__name__,opnsense_instance) (histogram_count({job="opnsense2otel"}))' --time "$T" -o json
```

The first must return scalar-valued zero, the second no bucket-family results, the third **nonempty native histogram-derived** results. `or vector(0)` is used only for absent bucket count; never zero-fill native presence. Generic job must remain exactly the owner-specified OTLP job. Check an active source instance; disabled Unbound does not prove its conversion.

For each `H` equal to `opnsense_flow_source_byte_delta_ratio` or `opnsense_unbound_dns_recursion_time_seconds`, expand explicitly:

```sh
gcx metrics query 'opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"}' --time "$T" -o json
gcx metrics query 'opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"}' --time "$T" -o json
gcx metrics query 'histogram_count(opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"})' --time "$T" -o json
gcx metrics query 'histogram_sum(opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"})' --time "$T" -o json
gcx metrics query 'histogram_count(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"})' --time "$T" -o json
gcx metrics query 'histogram_sum(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"})' --time "$T" -o json
```

Direct API JSON must contain native histogram sample objects (not only float-valued samples), with finite/nonnegative count and expected labels. Sum must follow the sentinel/reconstruction contract, not equal a midpoint-derived fake sum. Compare cumulative count/sum against a simultaneous source snapshot/scrape accounting for snapshot cadence, not two unrelated evaluation times.

Population and reset/temporality checks:

```promql
histogram_count(rate(opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"}[10m]))
histogram_count(rate(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"}[10m]))
histogram_sum(rate(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"}[10m]))
resets(opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"}[1h])
resets(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"}[1h])
rate(opnsense_flow_source_byte_delta_excluded_total{job="opnsense2otel"}[10m])
```

Require at least two distinct fresh export timestamps after complete poll warm-up and enough real eligible events to exercise both populations. No-data is not a successful zero rate. Verify no unintended periodic/native-only resets; distinguish exporter restart, resolver restart, statistics clear and schema/zero-threshold evolution. Do not intentionally restart a live resolver without separate authorization; controlled fixture/local reset is the implementation proof.

Native dashboard calculations:

```promql
histogram_quantile(0.5, sum by (opnsense_instance,interface) (rate(opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"}[10m])))
histogram_quantile(0.9, sum by (opnsense_instance,interface) (rate(opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"}[10m])))
histogram_quantile(0.99, sum by (opnsense_instance,interface) (rate(opnsense_flow_source_byte_delta_ratio{job="opnsense2otel"}[10m])))
histogram_quantile(0.5, sum by (opnsense_instance) (rate(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"}[10m])))
histogram_quantile(0.9, sum by (opnsense_instance) (rate(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"}[10m])))
histogram_quantile(0.99, sum by (opnsense_instance) (rate(opnsense_unbound_dns_recursion_time_seconds{job="opnsense2otel"}[10m])))
```

These are simple engine tests; additionally execute the **actual generated dashboard expressions**, including `or` matching and outer wrappers, against native-only and both-form datasets. Load Grafana 13+ locally/through the separately authorized read-only UI path and retain heatmap frames/screenshots, sentinel behavior and multi-instance separation. A PromQL parser success, an HTTP 200, a native series count or Grafana import success alone proves neither histogram arithmetic nor rendering.

Historical migration: query fixed all-job windows spanning pre/post deployment, then entirely post-deployment after the old sample lookback has expired. Preserve old classic data and mixed-window queries. Do not count historical `_bucket` series via a broad series-discovery endpoint as if they are fresh samples; conversely do not use disappearance of instant buckets to claim all historical data was migrated. Acceptance evaluates the literal requested current job and proves new native population.

## 9. Owner questions and independent reviewer-high challenge

**Q1 — blocking, Unbound representation:** Is any distribution loss approved, and with what exact retained checkpoints, maximum admissible quantile/CDF error, first-bucket and censored-tail semantics, naming/units, and invalid-input policy? If not, choose upstream/source-state work or a verified transport/storage alternative. No supplied inputs permit the originally implied faithful full exponential conversion. Neither current all-job zero-bucket criteria nor metric availability may be silently weakened.

**Q2 — flow native state policy:** Authorize parallel real-observation state and the fixed-schema-3 (~1025-bin per-interface maximum dense span) policy, including adding the existing Prometheus dependency to `internal/flow`, or require another reviewed aggregate implementation? The preferred client only supports cumulative total N<2^63; choose an observable parity-preserving beyond-domain policy or another implementation before runtime authorization. This theoretical boundary is not silently resolved by reset, refusal, or changing the classic uint64 contract. The current observed-duration 160-bucket/one-hour reset policy must not be reused unchanged. Existing interface cardinality remains a separate inherited concern.

**Q3 — compatibility:** Keep classic scrape shape and metric names, keep 101 zero-Zen sentinel, and accept native interpolation differences? Changing these semantics is a distinct owner choice. Which representation wins when a datasource contains both forms? Proposal is classic precedence for backward compatibility, without double-counting.

**Q4 — reset/temporality:** Preserve the existing bridge start-time fallback for Unbound and explicitly document undetectable administrative resets, or add stable tracked source epoch/refusal logic? Uptime is not a statistics epoch. Exact restart/clear proof needs upstream evidence or controlled source test; it cannot be invented from monotone counts.

**Q5 — upstream/support evidence:** Which supported Unbound/OPNsense release revisions must be checked before relying on the shape, overflow and two-truncation facts? If considering NHCB, who owns verified receiver/transport capability and how does it satisfy the base2 OTLP requirement rather than only lower cardinality? Current service documentation is not a tenant feature attestation.

**Q6 — invalid-source refusal and documentation defects:** Authorize a narrow separate correction for the unsupported half-us/no-overflow comments and catalogue Gauge typing, and select how bad histogram inputs are made observable? Returning no histogram must not become a silent way to obtain all-job zero buckets. No tracker or schema-exemption behavior is changed by this packet.

Independent reviewer-high must challenge, at minimum:

1. Rational-boundary nonalignment and the equal-count/sum CDF counterexamples, including real uint64-ratio realizability and upper-inclusive mapping.
2. Unbound source routine, 40-bound transition, overflow censoring, same-population proof and the **two-floor** sum-error derivation. Compare supported releases; reject an unsupported extrapolation from master.
3. Whether any purported exact coarsening preserves useful subsecond checkpoints; quantify discarded information rather than describing it as merely lower resolution.
4. Zero-Zen sentinel's native-tail interpretation versus classic +Inf percentile behavior; no new measured precision.
5. Client histogram snapshot immutability, native dense span bounds, aggregate count range, fixed epoch and classic/native population parity; no resetting only one representation.
6. First-metric family detection and empty native discriminator through **the real vendored bridge**.
7. All consumers in §3, particularly native heatmaps passing through `max without`, `_bucket` sentinel absence, instance deduplication and historical dual-form precedence.
8. Evidence separation: proposed tests/live queries are not executed proof, current docs are not source truth, no disabled metric or narrowed job counts as all-job closure.

Initial independent reviewer-high findings have been received and corrected in this packet: F1 replaced the unequal float64 decimal witness with bit-exact dyadic byte inputs; F2 reuses the existing heatmap dedupe API and removes unnecessary builder ownership; F3 names the preferred client's N<2^63 accumulation domain and leaves beyond-domain policy explicitly unresolved under Q2. Delta review remains required before AC4 completion. These corrections do not select an owner representation or authorize runtime work.

## 10. Primary citations and reproducible numerical inventory

- [Unbound bucket setup and overflow insert](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/util/timehist.c#L53-L145).
- [Unbound comparison and mean division](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/util/timeval_func.c#L74-L113). Note `<=` inside a function named smaller; do not infer exclusive upper bounds from its name.
- [Unbound same-population updates](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/services/mesh.c#L1639-L1641), and [statistics clear](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/services/mesh.c#L2384-L2396).
- [Unbound mean printing](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/daemon/remote.c#L873-L878), [histogram printing](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/daemon/remote.c#L1017-L1042), [extended-statistics gate](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/daemon/remote.c#L1266-L1272), [stats versus stats_noreset](https://github.com/NLnetLabs/unbound/blob/18d8676e9c975ef4c8412265b0225f5bbd1369f7/daemon/remote.c#L4065-L4073).
- [OPNsense stats wrapper](https://github.com/opnsense/core/blob/2ae9689a560d41f33d511767f27fe0608e8d01b7/src/opnsense/scripts/unbound/wrapper.py#L82-L94) and [API envelope](https://github.com/opnsense/core/blob/2ae9689a560d41f33d511767f27fe0608e8d01b7/src/opnsense/mvc/app/controllers/OPNsense/Unbound/Api/DiagnosticsController.php#L45-L55).
- [OTel metrics data model, ExponentialHistogram](https://github.com/open-telemetry/opentelemetry-specification/blob/bb85bf44e2c032d9958c10b78dcb88cf36e40917/specification/metrics/data-model.md#exponentialhistogram): scale/base, upper-inclusive mapping, dense buckets, zero threshold and exact downscaling apply to already known exponential populations, not arbitrary classical buckets.
- [Prometheus native histogram specification](https://github.com/prometheus/docs/blob/bb636b7918b0cdd4961d43cf6dd57cc9801a4148/docs/specs/native_histograms.md): standard versus custom schemas, zero bucket, overflow, sparse encoding and OTel correspondence.
- [Actual pinned bridge](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/vendor/go.opentelemetry.io/contrib/bridges/prometheus/producer.go#L85-L337): first-family detection, temporality/start-time, offset-minus-one and sparse-to-dense conversion.
- [Actual const-native constructor and bounds](https://github.com/rknightion/opnsense2otel/blob/1f294272d0d41ceea2a2902ad32846f8745502a9/vendor/github.com/prometheus/client_golang/prometheus/histogram.go#L1877-L2045): signed counts, schema range, empty-native marker. Same file's `limitBuckets/maybeReset` shows why the one-hour observed-histogram reset policy is not compatible with independent lifetime classic state.
- [Prometheus query functions](https://prometheus.io/docs/prometheus/latest/querying/functions/), retrieved fresh through Firecrawl 2026-10-13: native/classic quantile aggregation, exponential versus linear interpolation, mixed samples and reset handling. Scrape identity `01a13475-7608-7774-8186-f23c3e14bdda`.
- [Prometheus aggregation operators](https://prometheus.io/docs/prometheus/latest/querying/operators/#aggregation-operators), retrieved through Firecrawl 2026-10-13: `min/max` operate only on float samples and ignore histogram inputs with an info annotation. Scrape identity `01a13481-3bbf-71d6-b28e-f78d32446eee`. This directly supports the generated native heatmap wrapper finding.
- [Mimir native overview](https://grafana.com/docs/mimir/latest/send/native-histograms/), [exponential buckets](https://grafana.com/docs/mimir/latest/send/native-histograms/_exponential_buckets/), [custom buckets](https://grafana.com/docs/mimir/latest/send/native-histograms/_custom_buckets/), retrieved through Firecrawl 2026-10-13. The custom guide states experimental remote-write 2.0 dependency and scrape conversion; it does not establish compatibility with this direct OTLP path. Scrape identities `01a13475-6fb5-72ca-894a-202f153cc199`, `01a13476-f2e4-7251-a3b6-3a91269cbf60`, `01a13476-eb94-75d3-8033-0cc034b06f31`.
- [Grafana heatmap documentation](https://grafana.com/docs/grafana/latest/panels-visualizations/visualizations/heatmap/), retrieved through Firecrawl 2026-10-13, scrape identity `01a13475-7ff2-719b-8600-fa36a5ad5416`. General visualizer documentation does not prove generated native frames render; browser evidence remains required.

Bounded scratch calculations reproduced the 40-bound sequence, nonalignment at scales 0/3/8, the Unbound CDF witness and two-truncation sum witness. Independent review rejected the original flow scratch output because it printed a literal sum without asserting actual float64 equality. The corrected evidence derives ratios and serial sums from integer bytes, observes the old witness failing, then asserts exact count/sum/all-classic-bucket equality and distinct exponential CDFs for the replacement witness. Corrected numerical inventory:

```text
unbound bounds: count 40 micro-last 8192/15625 transition 1 last 524288
scale 0 micro boundaries nonaligned 20
scale 3 micro boundaries nonaligned 20
scale 8 micro boundaries nonaligned 20
rejected decimal witness: A sum 0x1.2e147ae147ae2p+1 B sum 0x1.2e147ae147ae1p+1 exact equality false
corrected byte witness: A [72/64,79/64] B [75/64,76/64] exact sum both 0x1.2e00000000000p+1
corrected classic count/sum/all-bucket equality true; schema3 boundary 1.189207115002721 splits only A
unbound schema0 witness boundary us 15.2587890625 A [9, 16] B [12, 13] same sum us 25
timeval_divide witness N=3 sum=2.000002s avg us 666666 reconstruction loss us 4 loss per sample us 1.3333333333333333
flow uint64-byte-ratio schema3 positive-index range inclusive -512..512 width 1025; OTLP first offset -513
preferred native client cumulative-total domain N<2^63, not unrestricted uint64
coarse mapping first20 -> zero_threshold 0.524288 native upper-index 0..19 seconds powers, last censored
```

This is arithmetic/source evidence, not runtime bridge, browser or deployed OTLP evidence. The rational nonalignment argument does not depend on floating-point integer-log checks from scratch output.

## 11. Acceptance/proof inventory and residual risks

- **AC1: packet supplied.** Both named-revision producer→snapshot→bridge→wire paths and repository dashboard/alert/scrape/reset consumers traced. External dashboards, supported-release variants and active receiver capabilities are not claimed discovered.
- **AC2: packet supplied.** Retained/lost data, faithful infeasibility, feasible prospective flow capture, conditional approximation bounds, actual sum/count, censoring and reset limits are explicit. No synthetic observations or invented source precision selected.
- **AC3: supported infeasibility branch supplied.** Conditional exact flow ownership/order and fail/pass through the real bridge/local wire/live-query packet are specified. A two-family numerical implementation is intentionally not approved or frozen until Q1 selects a supported route; upstream/infra ownership remains missing if that route is chosen.
- **AC4: initial independent review and corrections supplied; delta verdict pending.** Numerical/consumer findings F1–F3 and their concrete dispositions are stated above; Q1–Q6 remain owner decisions rather than approved runtime policy. Root must obtain a clean delta verdict before persisting the design as reviewed/complete.

Residual risks: no supported-release source comparison yet; no live receiver/tenant setting attestation; no implementation, runtime gate, bridge/wire fixture run or UI proof; existing source has censoring and rounded/truncated sum limitations; native heatmap query wrappers may drop histogram samples; arbitrary external `_bucket` consumers unknown; flow memory per interface and interface cardinality need explicit policy; literal all-job closure remains blocked by Unbound. These are deliberately not papered over by a green parser/type check or disabled metric.

## Independent review closure (supersedes the packet's pending-review status labels)

Reviewed packet SHA256: 375b7d9f94234115b67a770ce016fe5099f0c09b85b1361fe9e351caf1861d45. Independent reviewer-high delta verdict: PASS for publication as a supported design-only infeasibility result, not runtime-ready. Review source is exporter 1f294272d0d41ceea2a2902ad32846f8745502a9. F1 (major) is resolved by bit-exact dyadic byte inputs and observed old-witness failure; F2 (minor) by reuse of the existing heatmap dedupe API without builder changes; F3 (minor theoretical) by the explicit N<2^63 native-client domain and unresolved beyond-domain choice. No new defect in the repaired delta. AC4 review is complete for design publication; earlier pending labels describe the pre-delta state and do not govern this closure.

Q1-Q6 remain open for future owner-authorized runtime work. No approximation, upstream/transport change, reset/range/refusal policy, parent criteria change or runtime implementation is approved by publication. OPN-0118 (native histogram closeout with unchanged all-job zero-bucket criteria) remains parked. CodeRabbit and new tests deliberately skipped for design-only work; mathematical diagnostics are not exporter/bridge/browser/live proof.

loop11: zero runtime implementation attempts; one design correction/review-repair round. Full reviewed packet persisted here through Backlog CLI, not left as an ignored-artifact link. Root acceptance granted; integrated task-publication G and exact-SHA CI pending.

loop11: zero runtime implementation attempts, one design correction/review-repair round. Done as supported reviewed design-only infeasibility result after full packet publication 228722564e802019106f93402941c8e9f730cba4, composed G exit 0, required CI run 38377466529 / ci-success job 115233175728 success. No generated artifact changed by design; conditional just gen DoD is not applicable. Q1-Q6 remain future owner decisions, not an uncompleted design or authorized runtime packet. OPN-0118 (native histogram closeout with unchanged all-job zero-bucket criteria) stays parked and unchanged.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Full reviewed design is persisted in this task at source 1f294272d0d41ceea2a2902ad32846f8745502a9; design SHA256 375b7d9f94234115b67a770ce016fe5099f0c09b85b1361fe9e351caf1861d45. Flow can prospectively collect native state from actual eligible observations, but faithful full Unbound exponential conversion is unsupported by present pre-bucketed inputs and censored tail. Independent high delta review PASS after exact float64 witness/API/count-domain corrections. Conditional owned-file/dependency/consumer/real-bridge/wire/scrape/live query proof packet and six future owner decisions are explicit. Publication 228722564e802019106f93402941c8e9f730cba4 passed exact composed G and required CI 38377466529. No conversion implemented or parent criteria changed; CodeRabbit/new tests skipped design-only.
<!-- SECTION:FINAL_SUMMARY:END -->

# Unbound histogram source and transport alternatives

Study date: 2026-10-13. OPN-0130 (lossless Unbound source and transport study) and
OPN-0131 (correct Unbound precision and overflow documentation).

The existing API cannot supply a lossless base2 exponential histogram. The
strongest prospective route is to aggregate real durations inside Unbound at its
existing mesh-reply accounting point, before the information is discarded, and
expose that aggregate with an explicit reset epoch. This requires an upstream or
maintained package change. It is not an exporter-only conversion.

No runtime, metric value, label, population or acceptance requirement changes are
approved by this study. OPN-0118 (native exponential histograms with all-job zero
classic buckets) remains blocked for Unbound. “No loss” here means no additional
loss of the measured source population or available aggregate; it does not promise
raw-observation recovery from a histogram or exact real-number arithmetic.

## Supported release evidence

The repository support policy targets current stable 26.7.x. These are release
sources, not a claim about a contacted firewall's installed packages:

| Source | Immutable identity and evidence |
| --- | --- |
| OPNsense 26.7.6, released 2026-10-08, with documented 26.7.6_3 hotfix | [Release notes at changelog eac84e0](https://github.com/opnsense/changelog/blob/eac84e074c9c27a6d048eb39577fac75dcc3340f/community/26.7/26.7.6) |
| Release core | [26.7.6 wrapper at a842fe4](https://github.com/opnsense/core/blob/a842fe48c21288d11a422cb944678e8eb308becf/src/opnsense/scripts/unbound/wrapper.py#L82-L96): reads `stats_noreset` and reshapes histogram keys without restoring observations |
| Release ports | [26.7.6 package recipe at ab7b6ff](https://github.com/opnsense/ports/blob/ab7b6ff7b905cacc3fc4cc7f1633c20da3cbcca4/dns/unbound/Makefile#L1-L4): `DISTVERSION=1.26.1` |
| Bundled Unbound source | Release tag `release-1.26.1` resolves to [13b6717](https://github.com/NLnetLabs/unbound/tree/13b6717f1716810523ffb60f971a6c9c5e8ce127); the release port's [files directory](https://github.com/opnsense/ports/tree/ab7b6ff7b905cacc3fc4cc7f1633c20da3cbcca4/dns/unbound/files) has service/message files and no histogram patch |

Current stable-branch core `f1a83f72b144469fd97839aba2419ad130d19043`
and current ports `4811f42640bbfe1b3459d3cd955205a1f81bac88` were also
resolved, but the conclusions above use the release pins. Earlier 26.7 patch
packages and older best-effort releases were not exhaustively compared.

## What is measured and lost

- **Population and precision.** [mesh_send_reply](https://github.com/NLnetLabs/unbound/blob/13b6717f1716810523ffb60f971a6c9c5e8ce127/services/mesh.c#L1633-L1660)
  subtracts `start_time` from `now_tv`, then increments replies, accumulates wait,
  and inserts that same microsecond-resolution duration. This is the reply-wait
  population, not interchangeable with all DNS queries, cache misses or upstream
  packet round trips. Insertion occurs outside the extended-statistics guard.
- **Bounds and tail.** [timehist setup and insertion](https://github.com/NLnetLabs/unbound/blob/13b6717f1716810523ffb60f971a6c9c5e8ce127/util/timehist.c#L52-L131)
  produce 1, 2, …, 524288 microseconds, then 1, 2, …, 524288 seconds.
  The comparison is upper-inclusive. The final fallback says “dump in last bucket”.
  Durations above the final finite bound are therefore indistinguishable from
  ordinary last-bucket durations; equal finite/+Inf counts do not prove no overflow.
- **Sum.** [timeval_divide](https://github.com/NLnetLabs/unbound/blob/13b6717f1716810523ffb60f971a6c9c5e8ce127/util/timeval_func.c#L76-L103)
  separately truncates the microsecond part and the remainder of the seconds
  division. [print_stats](https://github.com/NLnetLabs/unbound/blob/13b6717f1716810523ffb60f971a6c9c5e8ce127/daemon/remote.c#L872-L883)
  prints the result to six places. For positive count N, nonnegative durations
  and no integer overflow, multiplying that mean by N loses less than 2N
  microseconds, before float64 error. Example: total 2.000002 seconds / 3 prints
  0.666666; reconstruction is 1.999998 seconds, four microseconds short.
  Neither exact reconstruction nor a half-microsecond rounding bound is valid.
- **Epoch.** The wrapper uses nonreset reads, but
  [statistics collection](https://github.com/NLnetLabs/unbound/blob/13b6717f1716810523ffb60f971a6c9c5e8ce127/daemon/stats.c#L376-L378)
  may clear statistics on reset requests when cumulative statistics are disabled;
  [mesh_stats_clear](https://github.com/NLnetLabs/unbound/blob/13b6717f1716810523ffb60f971a6c9c5e8ce127/services/mesh.c#L2385-L2396)
  clears count, sum and histogram. Uptime alone is not a statistics epoch. A
  decrease detects some resets, but misses a reset followed by enough new traffic.

The microsecond bounds are not seconds-based OTLP base2 boundaries. For example,
9us+16us and 12us+13us have the same count, sum and source 8–16us bucket population,
but split differently around the scale-0 boundary at 15.2587890625us. No parser,
transport or SDK view can recover which population occurred. Quantile interpolation
is an estimate even when histogram counts are measured correctly.

## Alternatives against the unchanged requirements

| Alternative | No additional source loss | Base2 OTLP | All-job zero classic buckets |
| --- | --- | --- | --- |
| Keep explicit OTLP | Preserves the available aggregate, including its existing limitations | No | Current receiver path produces suffix series; does not close acceptance |
| NHCB / Remote Write 2.0 storage | Can preserve arbitrary source bounds/counts and reconstructed sum | No: NHCB is a custom-bound histogram | Unverified storage possibility requiring receiver and query proof; still fails base2 OTLP |
| Convert API buckets to exponential using midpoints, upper bounds or invented samples | No: unknown within-bucket positions and censored tail | Encodable, but populations invented | Even if zero, not an acceptable proof |
| Merge small buckets into one zero region / coarsen | Discards original CDF checkpoints | Encodable | Does not meet no-loss acceptance |
| Observe real durations at the mesh accounting point | Prospective measured population can be retained in parallel aggregates; cannot repair historical data | Yes, with source-side base2 aggregation | Plausible only after end-to-end implementation and live proof for every job |
| Stream existing reply logs or dnstap | Per-event information exists, but completeness, population equivalence and transport loss are unproved | A new consumer can aggregate observations | Requires the same end-to-end proof; not a drop-in replacement |

The [pinned OTel compatibility specification](https://github.com/open-telemetry/opentelemetry-specification/blob/bb85bf44e2c032d9958c10b78dcb88cf36e40917/specification/compatibility/prometheus_and_openmetrics.md#native-histograms)
requires NHCB to become an OTLP **Histogram**, not an ExponentialHistogram.
Its [reverse NHCB mapping](https://github.com/open-telemetry/opentelemetry-specification/blob/bb85bf44e2c032d9958c10b78dcb88cf36e40917/specification/compatibility/prometheus_and_openmetrics.md#histograms-as-prometheus-nhcb)
preserves explicit bounds under schema -53. Firecrawl developer search surfaced
[merged specification PR 5091](https://github.com/open-telemetry/opentelemetry-specification/pull/5091);
the pinned source, including its Development status markers, governs these claims.
Specification capability does not prove the vendored bridge, a receiver release,
a tenant feature flag, storage or Grafana rendering supports that route.

Existing reply logging is particularly relevant: the mesh accounting point calls
`log_reply_info` with the same duration and cache flag 0 immediately after accounting.
However, enabling logging does not establish lossless delivery, distinguish every
other logging call's population, or supply an epoch. [dnstap source](https://github.com/NLnetLabs/unbound/blob/13b6717f1716810523ffb60f971a6c9c5e8ce127/dnstap/dnstap.c#L474-L655)
records client query/response and resolver exchange events; resolver exchanges can
include retries and referrals. Correlating those events is not proof of equality
with mesh reply waits. The inspected port lists DNSTAP as an option but not in its
default option set; installed build capability was not queried.

## Recommended next implementation prerequisites

1. Approve and name an upstream/package owner and delivery mechanism for Unbound
   instrumentation. Freeze the exact supported package and OPNsense API ownership;
   exporter ownership alone cannot make these new data available.
2. Specify additive source aggregation at the same accounting point: measured
   duration, parallel classic and base2 aggregates, count/sum representation,
   zero policy, scale/memory bound, long-tail treatment and explicit reset epoch.
   Preserve existing classic output and source population; specify overflow and
   precision domains instead of promising unbounded exact arithmetic.
3. Expose an atomic, versioned snapshot through the OPNsense wrapper/API. Specify
   absent/unsupported behavior and a restart/statistics-clear epoch that cannot be
   confused with ordinary polls. No synthetic observation reconstruction.
4. Before exporter integration, prove real-source aggregation against a local
   fixture including zero, exact boundaries, the existing transition, beyond-tail
   durations, restart and manual clear. Compare counts and sum on both paths and
   cross the actual API/bridge/OTLP wire boundary; unit DTO presence is insufficient.
5. Freeze exporter, dashboard, compatibility and rollout ownership. Retain finite
   classic scrape buckets where required while sending genuine exponential OTLP.
   Observe prospective flow events separately; replacing Unbound alone does not
   close the two-family or all-job acceptance.
6. With separately authorized stack and deployment scope, attest receiver/native
   settings and query all required jobs: genuine native samples present, classic
   bucket count zero, retained population and dashboard results. No credential,
   production capture or deployment authority is implied by this study.

No live firewall, tenant, package installation or browser was contacted. No new
behavioral tests were added for these documentation-only corrections. Source reads
and arithmetic establish the limitation; generated help validation and repository
gates belong to the integration pass. This study does not recommission the parked
conversion or weaken its acceptance criteria.

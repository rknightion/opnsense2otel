---
id: OPN-0118
title: Emit native (exponential) histograms instead of classical buckets
status: In Progress
assignee:
  - '@claude'
created_date: '2026-09-15 13:00'
updated_date: '2026-10-08 21:21'
labels:
  - observability
  - cardinality
dependencies: []
priority: high
type: chore
ordinal: 59000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
opnsense2otel pushes 2,621 classical histogram bucket series into the m7kni stack (stack 1217581, Mimir tenant 2359401) across 4 _bucket families, measured 2026-09-15 with job="opnsense2otel".

  2448  opnsense_exporter_api_request_duration_seconds_bucket
    96  opnsense_flow_source_byte_delta_ratio_bucket
    41  opnsense_unbound_dns_recursion_time_seconds_bucket
    36  opnsense_exporter_server_metrics_request_duration_seconds_bucket

opnsense_exporter_api_request_duration_seconds_bucket alone is 2,448 series of self-observability: this exporter's own OPNsense API client latency. It has zero dashboard, zero query and zero alert-rule usage in the last usage window, so it is currently the most expensive thing in this repo that nobody reads.

Two independent decisions, and both are worth taking:

1. Native histograms. A classical histogram costs one series per bucket plus _sum plus _count; a base2 exponential histogram is one series at better resolution. This exporter pushes OTLP to the gateway, so it is an SDK change only, no Alloy and no remote-write protocol change. Cheapest route with no code change:
     OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION=base2_exponential_bucket_histogram
   Explicit route is a metric.View with metric.AggregationBase2ExponentialHistogram{MaxSize: 160, MaxScale: 20}.

2. Reconsider the per-endpoint split on api_request_duration. 2,448 series for one latency histogram means the endpoint or path dimension is high cardinality. Even as a native histogram that stays 1 series per endpoint value. Check what splits it and whether a templated endpoint name would bound it.

Note the earlier assumption that this came from the Alloy opnsense-exporter-717 pipeline scraping localhost:9098 is wrong: the series carry job="opnsense2otel" and service_name="opnsense2otel", so they arrive by OTLP push from this exporter.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Histogram instruments emit base2 exponential histograms by default, verified via gcx metrics query returning no _bucket families for job=opnsense2otel
- [x] #2 The dimension splitting opnsense_exporter_api_request_duration_seconds is identified and either bounded or justified in writing
- [x] #3 The aggregation choice is documented in the repo config reference, with the env var named
- [ ] #4 Post-change bucket series for job=opnsense2otel is 0 and native histogram series are present
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add native-histogram options (factor 1.1, 160 buckets, 1h reset) alongside the classic buckets on the two observed histograms, so /metrics is unchanged and the OTLP Prometheus bridge emits base2 exponential histograms. 2. Leave the two const histograms (unbound recursion, flow delta ratio) classic: pre-bucketed data. 3. Make the health-dashboard p95/heatmap queries classic-or-native. 4. Document in docs/configuration.md. 5. Contract test through the real bridge, seen failing without the options.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC2: the split is endpoint x opnsense_instance. endpoint is the client's registered api/* path TEMPLATE (requestObserverAtPath passes the template, never a parameterised path), pre-seeded for every registered endpoint, so it is bounded by the endpoint registry (~200 per instance). 2,448 = ~175 endpoints x 14 (11 buckets + +Inf + _sum + _count): the cost was the bucket multiplier, not the label. As a native histogram it becomes ~175 series. Justified, not reduced. The env var OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION the task proposed does NOT apply: it only affects OTel SDK instruments, and every histogram here is a Prometheus client histogram bridged as pre-aggregated data. AC1/AC4 literal 'no _bucket families' is unreachable for the two const histograms (137 series); they stay classic by design.

Contract test internal/server/native_histogram_test.go goes through the real Prometheus bridge (seen failing as metricdata.Histogram without the options). Health dashboard p95/heatmap panels query classic-or-native (heatmap uses or on(instance,label) so a datasource holding both forms is not double-drawn); promqlcheck validates them. AC1/AC4 need a deployed build and a query against the m7kni stack, so the task stays In Progress until then.
<!-- SECTION:NOTES:END -->

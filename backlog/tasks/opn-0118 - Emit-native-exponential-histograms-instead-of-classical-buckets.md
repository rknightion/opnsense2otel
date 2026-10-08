---
id: OPN-0118
title: Emit native (exponential) histograms instead of classical buckets
status: To Do
assignee: []
created_date: '2026-09-15 13:00'
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
- [ ] #2 The dimension splitting opnsense_exporter_api_request_duration_seconds is identified and either bounded or justified in writing
- [ ] #3 The aggregation choice is documented in the repo config reference, with the env var named
- [ ] #4 Post-change bucket series for job=opnsense2otel is 0 and native histogram series are present
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 just check
- [ ] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

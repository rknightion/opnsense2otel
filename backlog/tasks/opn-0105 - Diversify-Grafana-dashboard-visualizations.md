---
id: OPN-0105
title: Diversify Grafana dashboard visualizations
status: Done
assignee:
  - '@codex'
created_date: '2026-09-19 09:56'
updated_date: '2026-09-19 10:59'
labels: []
dependencies: []
references:
  - 'https://grafana.com/grafana/plugins/search/?type=datasource%2Cpanel'
  - >-
    https://grafana.com/docs/grafana-cloud/observe-and-act/monitor-infrastructure/integrations/integration-reference/integration-ktranslate-netflow/#dashboards
ordinal: 59000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The generated operational and self-observability dashboards cover the available telemetry but rely heavily on time series, stats, and tables. Flow records in particular encode relationships that those panels obscure, while several other data shapes could be made easier to interpret with purpose-built Grafana visualizations. Audit the dashboards and use catalogue panel types only where they improve an operator decision without weakening the existing portability, cardinality, identity, or empty-state contracts.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Flow records include a relationship view that makes traffic paths and relative volume visible from the existing bounded log schema.
- [x] #2 The operational and self-observability dashboards are audited for data-appropriate visualization improvements, and each adopted change preserves instance identity, cardinality bounds, conditional rendering, and query semantics.
- [x] #3 Any external panel-plugin dependency is documented by exact plugin ID, has a harmless missing-plugin experience, and is verified against a live Grafana Cloud stack.
- [x] #4 Generated dashboard artifacts pass schema validation and the repository gate, and representative changed panels are visually verified with real data.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 just check
- [x] #2 just gen (if any generated artifact changed) and the diff committed
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the v2 dashboard builder with reusable, tested helpers for the selected built-in and catalogue visualizations, preserving datasource, identity, transformation, and link contracts.
2. Redesign Flow Volume with complementary Sankey, geomap, treemap or categorical views over bounded Prometheus and Loki queries while retaining existing exact-value and forensic panels.
3. Audit every operational domain and the health dashboard, adopting data-shape-led bar charts, heatmaps, histograms, geomaps, treemaps, polystats, relationship views, or other catalogue panels wherever they improve an operator decision.
4. Regenerate assets, document exact plugin dependencies and missing-plugin behavior, then run targeted tests, schema validation, CodeRabbit review, and just check.
5. Publish scratch dashboard identities to m7kni, enable any selected catalogue panels there when required, inspect representative dark and light renders with real data, iterate, and only then update the canonical generated dashboards.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Design choice approved by Rob: comprehensive additive redesign (Approach A), with no numerical cap on new panels. Existing diagnostic and exact-value panels remain available as fallbacks; visual diversity is constrained by data fit, query cost, cardinality, and operational usefulness rather than panel count.

Architecture approved by Rob: reusable v2 builder helpers; built-ins used broadly; third-party catalogue panels used additively; existing exact-value and diagnostic panels retained; plugin absence must not remove the underlying data path; all existing identity, cardinality, datasource, sentinel and query-window contracts remain binding.

Implemented reusable v2 helpers for bar chart, heatmap, histogram, geomap, XY chart, Sankey, Treemap, Polystat and Node Graph. Added Flow Volume bar, geomap, Sankey and Treemap panels plus heatmaps for API latency, metrics-handler latency, DNS recursion latency and flow source-delta ratio. Live m7kni validation returned zero failures; dark and light Grafana renders used real Prometheus and Loki data. CodeRabbit's duplicate major heatmap-format finding was fixed via a RED-to-GREEN regression test. Final just check passed, including 432 Grafana tests and generated consistency.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Diversified both generated dashboards with data-shape-appropriate built-in and catalogue panels while retaining diagnostic fallbacks, bounded queries and appliance identity. Verified generated v2 manifests against live m7kni, visually rendered representative panels in both themes with real data, fixed CodeRabbit's heatmap format finding, and passed just check.
<!-- SECTION:FINAL_SUMMARY:END -->

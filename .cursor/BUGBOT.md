# OPNsense exporter review guide

## What this is

Operators run this exporter alongside OPNsense firewalls. It serves Prometheus metrics, sends OpenTelemetry metrics and logs over OTLP, and receives syslog and flow records.

## Invariants

- Keep collection off the scrape path. The scheduler publishes snapshots for `/metrics` and OTLP metrics. The web console also reads snapshots and cached state. Its device tab has separate, time-bounded reads.
- Preserve metric names, units and label meanings. Keep label cardinality bounded. Keep credentials and raw payloads out of diagnostics and public fixtures.
- Make fixes to generated dashboards, alerts and reference docs in their source generators. Review the generators and behavior tests when generated output changes.

## Changes to scrutinize

- In `opnsense/client.go`, check that a stale connection can redial and that the retry still respects the request's context. For new payload fields, check the upstream shape and whether the field is optional.
- In `internal/logship/` and `internal/annotations/`, check recovery after a failed capture rotation, either direction of year rollover for yearless syslog timestamps, and deduplication across annotation readback. Extra configured tags must preserve event identity.
- In `internal/flow/`, check direction and NAT identity before summing observations. Keep the correlated windows comparable so a partial record cannot double-count a connection.
- In `grafana/alerts/build_rules.py`, match health and freshness gates to the originating scrape target as well as the firewall. Check the behavior during a reboot, when samples are stale, and when the condition persists past the alert's hold period.

## Findings already covered by CI

Formatting, configured lint rules, metric naming and workflow lint already have automated checks. Focus review on reachable behavior changes and cross-component contracts that those checks cannot establish.

## Verification

`just check` runs the Go race suite, bounded fuzz targets, public-data checks, generated-doc checks, Grafana tests and dependency vulnerability scanning. Native CI also exercises deployment contracts, cross-builds, image and chart validation. Structure-only fixtures establish the recorded shape. Claims about a live firewall need separate live evidence.

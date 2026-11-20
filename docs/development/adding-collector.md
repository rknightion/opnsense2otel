---
title: Adding a Collector
description: Add an opnsense2otel collector across the API client, collector registry, metric definitions, tests, and documentation.
tags:
  - OPNsense
---

# Adding a Collector

This guide follows the registration contract using the temperature collector as a concrete example. The maintained reference is `reference/adding-a-collector.md`.

## Step 1: Implement and register the collector

Implement `CollectorInstance` in `internal/collector/` and append the instance in `init()`. There is no central registration list. Use `buildPrometheusDesc` so docgen can discover descriptors. The interface requires this signature:

`Update(ctx context.Context, client *opnsense.Client, ch chan<- prometheus.Metric) *opnsense.APICallError`

The following is `internal/collector/temperature.go`; its `init`, `Register`, `Describe`, and `Update` show the complete pattern. `TestTemperatureCollector_Update` checks its behaviour.

```go
package collector

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

type temperatureCollector struct {
	log *slog.Logger

	celsius *prometheus.Desc

	subsystem string
	instance  string
}

func init() {
	collectorInstances = append(collectorInstances, &temperatureCollector{
		subsystem: TemperatureSubsystem,
	})
}

func (c *temperatureCollector) Name() string {
	return c.subsystem
}

func (c *temperatureCollector) Register(namespace, instanceLabel string, log *slog.Logger) {
	c.log = log
	c.instance = instanceLabel
	c.log.Debug("Registering collector", "collector", c.Name())

	c.celsius = buildPrometheusDesc(c.subsystem, "celsius",
		"Temperature reading in Celsius",
		[]string{"device", "type", "device_seq"},
	)
}

func (c *temperatureCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.celsius
}

func (c *temperatureCollector) Update(ctx context.Context, client *opnsense.Client, ch chan<- prometheus.Metric) *opnsense.APICallError {
	readings, err := client.FetchTemperatures()
	if err != nil {
		return err
	}

	for _, r := range readings {
		ch <- prometheus.MustNewConstMetric(
			c.celsius,
			prometheus.GaugeValue,
			r.Celsius,
			r.Device,
			r.Type,
			strconv.Itoa(r.DeviceSeq),
			c.instance,
		)
	}

	return nil
}
```

## Step 2: Assign a poll tier

Add a non-medium collector to `collectorTiers` in `internal/collector/interval_tiers.go`. An absent entry means the 60 second medium default, as for temperature. Operators can override the interval using the collector poll interval override. There is no test that requires an explicit tier: select one based on data volatility.

## Step 3: Add the API method and endpoint

Add the response types and fetch method in `opnsense/`, and register the endpoint in `defaultEndpoints()` in `opnsense/client.go`. Add its ACL entry in `opnsense/acl.go`: `TestEndpointACLCoversEveryEndpoint` checks coverage, and `TestNewClient_UsesDefaultEndpoints` checks the client's defaults. For a plugin-gated endpoint, return empty data and nil on a 404, as `FetchACMECertificates` does. Temperature is a plain GET and is not plugin-gated. Its `opnsense/temperature.go` is:

```go
package opnsense

type temperatureReading struct {
	Device         string `json:"device"`
	DeviceSeq      int    `json:"device_seq"`
	Temperature    string `json:"temperature"`
	TypeTranslated string `json:"type_translated"`
	Type           string `json:"type"`
}

type TemperatureReading struct {
	Device    string
	DeviceSeq int
	Type      string
	Celsius   float64
}

func (c *Client) FetchTemperatures() ([]TemperatureReading, *APICallError) {
	var resp []temperatureReading

	url, ok := c.endpoints["systemTemperature"]
	if !ok {
		return nil, &APICallError{
			Endpoint:   "systemTemperature",
			Message:    "endpoint not found in client endpoints",
			StatusCode: 0,
		}
	}

	if err := c.do("GET", url, nil, &resp); err != nil {
		return nil, err
	}

	readings := make([]TemperatureReading, 0, len(resp))
	for _, r := range resp {
		readings = append(readings, TemperatureReading{
			Device:    r.Device,
			DeviceSeq: r.DeviceSeq,
			Type:      r.Type,
			Celsius:   safeParseFloat(r.Temperature),
		})
	}

	return readings, nil
}
```

## Step 4: Register POST endpoints

For POST requests only, add the endpoint key to `postEndpoints` in `opnsense/contract.go` and update the golden POST count in `opnsense/contract_test.go`; `TestContractManifestCoversAllEndpoints` checks the manifest. GET endpoints are derived automatically, so temperature needs no POST entry.

## Step 5: Cache plugin absence

For a plugin-gated GET or POST, add its endpoint to `NegativeCacheable404Endpoints()` in `opnsense/cache.go`, subject to the distinction below. Never add a core endpoint: a cached health-check 404 could hide recovery. `TestPluginGatedEndpoints_ExcludesCoreEndpoints` checks this protection. Temperature is not plugin-gated and needs no entry.

## Step 6: Register the schema

Add the response struct to `schemaRegistry` in `opnsense/schema_registry.go` and run `just schemas`; `TestSchemaRegistryComplete` checks coverage. POST endpoints also need a request body in `captureRequests` in `opnsense/capture_requests.go`. These feed the on-demand, dispatch-only live schema canary. Golden schemas contain structure only, never response values.

## Step 7: Add the subsystem and option

Add the subsystem constant, `Without<Subsystem>Collector()` option and `SubsystemDisplayNames` entry in `internal/collector/collector.go`. Temperature's corresponding entries are in that file. `TestSubsystemDisplayNamesComplete` checks display-name coverage; `TestEveryCollectorHasDisableSwitch` checks registered collectors against switches.

## Step 8: Add the switch

Add the flag, `CollectorsDisableSwitch` field, switch entry and `CollectorFlags` binding in `internal/options/collectors.go`. Use a default-on disable flag for low-cardinality collectors; reserve a default-off enable flag for extra API cost or high cardinality. `TestCollectorFlagsCoverAllSwitchFlags` and `TestEveryCollectorHasDisableSwitch` check the bindings.

## Step 9: Wire the switch

Wire the switch in `main.go` to the collector's `Without` option. `TestEveryDisableSwitchWiredInMain` checks that switch fields are wired.

## Two 404 lists

`NegativeCacheable404Endpoints()` asks whether a 404 may be cached; `PluginGatedEndpoints()` asks whether it means a plugin is absent. The latter is a superset used by the canary and ACLs. When a query string prevents the cache key matching the request, list the endpoint only in `PluginGatedEndpoints()`; otherwise the canary treats the missing plugin as a vanished core route.

## What may be cached

Successful bodies may be cached only for GETs with wholly slow-moving payloads. Counters, rates and live status would freeze; POST bodies must never be cached because requests can differ. A plugin-absence 404 can be cached for any method, even when successful data is live. See `opnsense/cache.go` for the policy.

## Testing

Add collector and API tests, then run:

```bash
just test TestTemperatureCollector_Update
just test TestFetchTemperatures
```

## Docs and dashboard

Run `just docs` and `just docs-check`. Do not hand-edit generated pages or docgen regions. Every catalogue metric must appear on the Grafana dashboard: add panels in `grafana/tabs/` following `grafana/tabs/AUTHORING.md`, then run `just dashboard`. It fails if a catalogue metric is missing. Add alert and recording rules through `grafana/alerts/build_rules.py` and run `just rules`; `just grafana-check` gates the generated artifacts.

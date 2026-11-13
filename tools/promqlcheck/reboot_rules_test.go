package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/prometheus/promql/promqltest"
)

func shippedAlertExpr(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "grafana", "alerts", "grafana-managed", name+".json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m struct {
		Spec struct {
			Expressions map[string]struct {
				Model struct {
					Expr string `json:"expr"`
				} `json:"model"`
			} `json:"expressions"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	expr := m.Spec.Expressions["A"].Model.Expr
	if expr == "" {
		t.Fatalf("manifest %s has no A expression", name)
	}
	return expr
}

// firewallState is what the exporter reports for one synthetic firewall, held
// constant for 30m and evaluated at the end, where time() is 1800. bootAt empty
// leaves the boot timestamp absent.
type firewallState struct {
	up         int
	bootAt     string
	snapshotAt int
}

func (s firewallState) load(collector string, interval int, series string) string {
	sel := fmt.Sprintf(`{collector=%q,opnsense_instance="fw"}`, collector)
	out := "load 1m\n"
	out += fmt.Sprintf("  opnsense_up{opnsense_instance=\"fw\"} %dx30\n", s.up)
	if s.bootAt != "" {
		out += fmt.Sprintf("  opnsense_system_boot_timestamp_seconds{opnsense_instance=\"fw\"} %sx30\n", s.bootAt)
	}
	out += fmt.Sprintf("  opnsense_exporter_collector_snapshot_timestamp_seconds%s %dx30\n", sel, s.snapshotAt)
	out += fmt.Sprintf("  opnsense_exporter_collector_poll_interval_seconds%s %dx30\n", sel, interval)
	out += "  " + series + " 0x30\n"
	return out
}

// The service and gateway down rules must only see a stopped service or a down
// gateway in fresh data from a reachable firewall that is past its boot window.
// Retained boot-time zeros, an unreachable box and a box that has just booted all
// remove the series; a missing boot timestamp must not silence the alert.
func TestDownRulesIgnoreRetainedAndBootTimeData(t *testing.T) {
	rules := []struct {
		name, collector string
		interval        int
		series, want    string
	}{
		{"opnsense-service-down", "services", 300,
			`opnsense_services_status{name="cron",description="Cron",opnsense_instance="fw"}`,
			`opnsense_services_status{name="cron",description="Cron",opnsense_instance="fw"} 0`},
		{"opnsense-gateway-down", "gateways", 15,
			`opnsense_gateways_status{name="WAN_DHCP",default_gateway="true",opnsense_instance="fw"}`,
			`opnsense_gateways_status{name="WAN_DHCP",default_gateway="true",opnsense_instance="fw"} 0`},
		{"opnsense-gw-down-failover", "gateways", 15,
			`opnsense_gateways_status{name="WAN_PPPOE",default_gateway="false",opnsense_instance="fw"}`,
			`opnsense_gateways_status{name="WAN_PPPOE",default_gateway="false",opnsense_instance="fw"} 0`},
	}
	states := []struct {
		name  string
		state firewallState
		fires bool
	}{
		{"fresh data on a settled box", firewallState{up: 1, bootAt: "0", snapshotAt: 1790}, true},
		{"boot timestamp absent", firewallState{up: 1, snapshotAt: 1790}, true},
		{"retained snapshot from before the poll stopped", firewallState{up: 1, bootAt: "0", snapshotAt: 0}, false},
		{"firewall API unreachable", firewallState{up: 0, bootAt: "0", snapshotAt: 1790}, false},
		{"booted five minutes ago", firewallState{up: 1, bootAt: "1500", snapshotAt: 1790}, false},
	}
	for _, r := range rules {
		expr := shippedAlertExpr(t, r.name)
		for _, s := range states {
			t.Run(r.name+"/"+s.name, func(t *testing.T) {
				script := s.state.load(r.collector, r.interval, r.series) + "\neval instant at 30m " + expr + "\n"
				if s.fires {
					script += "  " + r.want + "\n"
				}
				promqltest.RunTest(t, script, promqltest.NewTestEngine(t, false, 0, promqltest.DefaultMaxSamplesPerQuery))
			})
		}
	}
}

// A firewall that is unreachable or has just booted must not raise one stale-data
// alert per collector; a reachable box whose collector stopped refreshing still must.
func TestCollectorDataStaleDefersToExporterDownAndBootWindow(t *testing.T) {
	expr := shippedAlertExpr(t, "opnsense-collector-data-stale")
	states := []struct {
		name  string
		state firewallState
		want  string
	}{
		{"stale on a reachable settled box", firewallState{up: 1, bootAt: "0", snapshotAt: 0}, `{collector="pf_stats",opnsense_instance="fw"} 112`},
		{"stale while the API is unreachable", firewallState{up: 0, bootAt: "0", snapshotAt: 0}, ""},
		{"stale just after a boot", firewallState{up: 1, bootAt: "1500", snapshotAt: 0}, ""},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			script := s.state.load("pf_stats", 15, `opnsense_pf_states{opnsense_instance="fw"}`) + "\neval instant at 30m " + expr + "\n"
			if s.want != "" {
				script += "  " + s.want + "\n"
			}
			promqltest.RunTest(t, script, promqltest.NewTestEngine(t, false, 0, promqltest.DefaultMaxSamplesPerQuery))
		})
	}
}

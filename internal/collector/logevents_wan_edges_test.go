package collector

import (
	"github.com/prometheus/common/promslog"
	"testing"
)

func TestLogEventsCollector_WANEdgesAndTimestampPersist(t *testing.T) {
	c := &logEventsCollector{store: newTestLogEventStore(t), subsystem: LogEventsSubsystem}
	c.Register(namespace, "opnsense.example.com", promslog.NewNopLogger())
	c.store.ObservePPPLink("opt7", "link_up", 100)
	c.store.ObservePPPLink("opt7", "link_down", 101)
	c.store.ObservePPPLink("opt7", "link_up", 200)
	c.store.ObserveGatewayStatesKilled("WAN_TEST")
	c.store.ObserveGatewayStatesKilled("WAN_TEST")
	for scrape := 0; scrape < 2; scrape++ {
		seen := 0
		for _, m := range collectMetrics(t, c, nil) {
			labels := getMetricLabels(m)
			switch {
			case hasFqName(m, "opnsense_log_events_ppp_link_total"):
				seen++
				want := float64(1)
				if labels["event"] == "link_up" {
					want = 2
				}
				if len(labels) != 3 || labels["interface"] != "opt7" || getMetricValue(m) != want {
					t.Errorf("edge labels/value = %v/%v", labels, getMetricValue(m))
				}
			case hasFqName(m, "opnsense_log_events_ppp_last_link_up_timestamp_seconds"):
				seen++
				if !metricIsGauge(t, m) || len(labels) != 2 || labels["interface"] != "opt7" || getMetricValue(m) != 200 {
					t.Errorf("last-up labels/value = %v/%v", labels, getMetricValue(m))
				}
			case hasFqName(m, "opnsense_log_events_gateway_states_killed_total"):
				seen++
				if len(labels) != 2 || labels["gateway"] != "WAN_TEST" || getMetricValue(m) != 2 {
					t.Errorf("kill labels/value = %v/%v", labels, getMetricValue(m))
				}
			}
		}
		if seen != 4 {
			t.Fatalf("scrape %d emitted %d WAN metric rows, want 4", scrape, seen)
		}
	}
}

func TestLogEventStore_WANMetricBudgets(t *testing.T) {
	s := newTestLogEventStore(t)
	s.SetMaxKeys(1)
	for _, iface := range []string{"opt7", "opt8"} {
		s.ObservePPPLink(iface, "link_up", 100)
		s.ObserveGatewayStatesKilled(iface)
	}
	s.ObservePPPLink("opt7", "link_up", 200)
	s.sync()
	if len(s.ppp.m) != 1 || len(s.pppLastUp.m) != 1 || len(s.gatewayKills.m) != 1 {
		t.Fatal("new WAN families exceeded max keys")
	}
	if s.pppLastUp.m["opt7"] != 200 {
		t.Fatal("existing last-up gauge did not update at capacity")
	}
	if _, overflow := s.ppp.snapshot(); overflow != 1 {
		t.Fatalf("PPP overflow=%v", overflow)
	}
	if _, overflow := s.pppLastUp.snapshot(); overflow != 1 {
		t.Fatalf("last-up overflow=%v", overflow)
	}
	if _, overflow := s.gatewayKills.snapshot(); overflow != 1 {
		t.Fatalf("kill overflow=%v", overflow)
	}
}

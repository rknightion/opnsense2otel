package metricsnap_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/promslog"
	"github.com/rknightion/opnsense2otel/v5/internal/metricsnap"
	"github.com/rknightion/opnsense2otel/v5/internal/server"
)

type syntheticViews struct{}

func (syntheticViews) EnabledCollectorNames() []string { return []string{"base", "fast"} }
func (syntheticViews) ScrapeView(_ context.Context, include map[string]bool) prometheus.Collector {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "fixture_collector_success", Help: "synthetic collector health"}, []string{"collector"})
	for name, value := range map[string]float64{"base": 11, "fast": 22} {
		if include == nil || include[name] {
			g.WithLabelValues(name).Set(value)
		}
	}
	return g
}

func schedulerValues(t *testing.T, families []*dto.MetricFamily) map[string]float64 {
	t.Helper()
	values := make(map[string]float64)
	for _, family := range families {
		if family.GetName() != "fixture_collector_success" {
			t.Fatalf("unexpected captured family %q", family.GetName())
		}
		for _, metric := range family.GetMetric() {
			if len(metric.GetLabel()) != 1 || metric.Label[0].GetName() != "collector" {
				t.Fatalf("unexpected labels: %v", metric.GetLabel())
			}
			values[metric.Label[0].GetValue()] = metric.GetGauge().GetValue()
		}
	}
	return values
}

func TestRecorder_HTTPFilteredAndSelfIsolation(t *testing.T) {
	r := metricsnap.New()
	r.ConfigureSplitLanes("base", "fast")
	var observed []int
	r.ConfigureSeriesBudget(metricsnap.SeriesBudget{Observed: func(total int) { observed = append(observed, total) }})
	self := prometheus.NewRegistry()
	selfGauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "fixture_self_metric", Help: "synthetic self metric"})
	selfGauge.Set(7)
	self.MustRegister(selfGauge)
	handler := server.NewMetricsHandler(syntheticViews{}, self, promslog.NewNopLogger(), r, nil)
	request := func(path string) string {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d: %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	body := request("/metrics")
	if !strings.Contains(body, "fixture_self_metric 7") || !strings.Contains(body, `fixture_collector_success{collector="base"} 11`) || !strings.Contains(body, `fixture_collector_success{collector="fast"} 22`) {
		t.Fatalf("unfiltered output: %s", body)
	}
	if got := schedulerValues(t, r.Capture().Families); !reflect.DeepEqual(got, map[string]float64{"base": 11, "fast": 22}) {
		t.Fatalf("full capture: %v", got)
	}

	for _, lane := range []struct {
		name  string
		value float64
	}{{"base", 31}, {"fast", 42}} {
		reg := prometheus.NewRegistry()
		g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "fixture_collector_success", Help: "synthetic collector health"}, []string{"collector"})
		g.WithLabelValues(lane.name).Set(lane.value)
		reg.MustRegister(g)
		raw, err := r.TeeLane(lane.name, reg).Gather()
		if err != nil {
			t.Fatal(err)
		}
		if got := schedulerValues(t, raw); !reflect.DeepEqual(got, map[string]float64{lane.name: lane.value}) {
			t.Fatalf("raw split gather %s: %v", lane.name, got)
		}
	}
	before := r.Capture()
	if got := schedulerValues(t, before.Families); !reflect.DeepEqual(got, map[string]float64{"base": 31, "fast": 42}) {
		t.Fatalf("split capture: %v", got)
	}
	if !reflect.DeepEqual(observed, []int{2, 2}) {
		t.Fatalf("initial complete budgets: %v", observed)
	}

	selfGauge.Set(8)
	body = request("/metrics?collect%5B%5D=base")
	if !strings.Contains(body, "fixture_self_metric 8") || !strings.Contains(body, `fixture_collector_success{collector="base"} 11`) || strings.Contains(body, `collector="fast"`) {
		t.Fatalf("filtered output: %s", body)
	}
	if _, err := self.Gather(); err != nil {
		t.Fatal(err)
	}
	after := r.Capture()
	if !after.At.Equal(before.At) || after.Partial != before.Partial || !reflect.DeepEqual(schedulerValues(t, after.Families), map[string]float64{"base": 31, "fast": 42}) || !reflect.DeepEqual(observed, []int{2, 2}) {
		t.Fatalf("filtered or self gather changed capture/budget: %+v; %v", after, observed)
	}

	body = request("/metrics")
	if !strings.Contains(body, "fixture_self_metric 8") {
		t.Fatalf("self metric absent from full output: %s", body)
	}
	if got := schedulerValues(t, r.Capture().Families); !reflect.DeepEqual(got, map[string]float64{"base": 11, "fast": 22}) {
		t.Fatalf("full takeover: %v", got)
	}
	if !reflect.DeepEqual(observed, []int{2, 2, 2}) {
		t.Fatalf("budget included self metric or missed publication: %v", observed)
	}
}

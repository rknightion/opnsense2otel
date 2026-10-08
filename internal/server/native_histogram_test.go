package server

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	prometheusbridge "go.opentelemetry.io/contrib/bridges/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestRequestDurationIsDualHistogram pins OPN-0118: the /metrics request histogram
// reaches OTLP as ONE base2 exponential histogram per label set (not a series per
// bucket), while the text exposition still carries the classic _bucket series that
// Prometheus scrapers and the dashboards' le-based queries read.
func TestRequestDurationIsDualHistogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newHandlerMetrics(reg)
	m.requestDuration.WithLabelValues(requestStatusOK).Observe(0.042)

	producer := prometheusbridge.NewMetricProducer(prometheusbridge.WithGatherer(reg))
	reader := sdkmetric.NewManualReader(sdkmetric.WithProducer(producer))
	defer reader.Shutdown(context.Background()) //nolint:errcheck
	_ = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var found bool
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			if md.Name != metricNameRequestDuration {
				continue
			}
			found = true
			if _, ok := md.Data.(metricdata.ExponentialHistogram[float64]); !ok {
				t.Fatalf("OTLP %s is %T, want ExponentialHistogram[float64]", md.Name, md.Data)
			}
		}
	}
	if !found {
		t.Fatalf("%s missing from OTLP output", metricNameRequestDuration)
	}

	// The text exposition renders the classic buckets from these fields; a native-only
	// histogram has none and would drop every _bucket series from /metrics.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricNameRequestDuration {
			continue
		}
		if got := len(mf.GetMetric()[0].GetHistogram().GetBucket()); got != len(prometheus.DefBuckets) {
			t.Fatalf("classic buckets = %d, want %d", got, len(prometheus.DefBuckets))
		}
		return
	}
	t.Fatalf("%s missing from gather", metricNameRequestDuration)
}

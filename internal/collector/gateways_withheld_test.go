package collector

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const gatewayValueSentinel = "SYNTHETIC-GATEWAY-VALUE-SENTINEL"

// An unparseable threshold or priority is skipped with a log that names the
// gateway and the field, and never the value the firewall sent.
func TestGatewaysCollectorParseLogsWithholdTheValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"total":1,"rowCount":1,"current":1,"rows":[{
			"disabled":false,"name":"WAN_GW","descr":"d","interface":"igb0","ipprotocol":"inet",
			"gateway":"192.0.2.1","defaultgw":true,"fargw":"","monitor_disable":"0","monitor_noroute":"0",
			"monitor":"192.0.2.2","force_down":"0","priority":"` + gatewayValueSentinel + `","weight":"1",
			"latencylow":"` + gatewayValueSentinel + `","current_latencylow":"","latencyhigh":"","current_latencyhigh":"",
			"losslow":"","current_losslow":"","losshigh":"","current_losshigh":"","interval":"","current_interval":"",
			"time_period":"","current_time_period":"","loss_interval":"","current_loss_interval":"",
			"data_length":"","current_data_length":"","uuid":"abc-123","if":"wan","attribute":1,"dynamic":false,
			"virtual":false,"upstream":true,"interface_descr":"WAN","status":"Online","delay":"1.2 ms",
			"stddev":"0.3 ms","loss":"0.0 %","label_class":"success"}]}`))
	}))
	defer server.Close()
	client := newCollectorTestClient(t, server)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := &gatewaysCollector{subsystem: GatewaysSubsystem}
	c.Register(namespace, "test", logger)
	collectMetrics(t, c, client)

	got := logs.String()
	if strings.Contains(got, gatewayValueSentinel) {
		t.Errorf("log holds the upstream value: %s", got)
	}
	for _, want := range []string{"skipping gateway threshold metric: unparseable value", "skipping gateway priority metric: unparseable value", "WAN_GW", "latencylow"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lost %q: %s", want, got)
		}
	}
}

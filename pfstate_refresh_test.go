package main

import (
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/flow"
	"github.com/rknightion/opnsense2otel/v5/internal/options"
	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

func TestPFStateRefreshSingleAttemptAndStretch(t *testing.T) {
	var requests atomic.Int32
	var slow atomic.Bool
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if slow.Load() {
			<-release
			return
		}
		_, _ = io.WriteString(w, `{"total":1,"rows":[{"proto":"tcp","direction":"out","src_addr":"192.0.2.1","src_port":"1234","dst_addr":"198.51.100.1","dst_port":"443","route-to":"192.0.2.2@wan","nat_addr":"10.0.0.1","nat_port":"1234"}]}`)
	}))
	defer server.Close()
	defer close(release)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := opnsense.NewClient(options.OPNSenseConfig{Protocol: "http", Host: strings.TrimPrefix(server.URL, "http://"), Timeout: 50 * time.Millisecond}, "test", logger)
	if err != nil {
		t.Fatal(err)
	}
	repairer := &flow.Repairer{}
	refresher := &pfStateRefresher{fetch: client.FetchFirewallStates, repairer: repairer, logger: logger}
	refresher.refreshOnce()
	routes, nats := repairer.RouteTable(), repairer.NATTable()
	if routes == nil || nats == nil {
		t.Fatal("successful refresh did not publish both tables")
	}
	slow.Store(true)
	requests.Store(0)
	refresher.refreshOnce()
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
	if repairer.RouteTable() != routes || repairer.NATTable() != nats || refresher.previous != routes || refresher.previousNAT != nats {
		t.Error("failed refresh replaced previous tables")
	}
	for _, tc := range []struct{ last, want time.Duration }{
		{time.Second, time.Minute}, {6 * time.Second, time.Minute},
		{12 * time.Second, 2 * time.Minute}, {90 * time.Second, 15 * time.Minute},
		{time.Duration(math.MaxInt64), 15 * time.Minute},
	} {
		if got := nextPFStateRefreshInterval(tc.last); got != tc.want {
			t.Errorf("interval after %v = %v, want %v", tc.last, got, tc.want)
		}
	}
}

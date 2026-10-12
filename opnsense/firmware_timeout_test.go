package opnsense

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/options"
)

// Exercise the real HTTP boundary at the observed upper response time, not a
// scaled-down timeout or a mocked transport.
func TestFirmwareInfoSlowResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(35 * time.Second):
			_, _ = io.WriteString(w, `{"product_id":"opnsense","product_version":"26.7","package":[{"name":"base"}],"plugin":[{"name":"os-example","version":"1","installed":"1"}]}`)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	client, err := NewClient(options.OPNSenseConfig{Protocol: "http", Host: strings.TrimPrefix(server.URL, "http://"), MaxRetries: 1}, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.httpClient.CloseIdleConnections()
	if client.httpClient.Timeout != 15*time.Second {
		t.Fatalf("global timeout = %s, want 15s", client.httpClient.Timeout)
	}
	start := time.Now()
	if _, apiErr := client.FetchFirmwareStatus(); apiErr == nil || !strings.Contains(apiErr.Message, "deadline exceeded") {
		t.Fatalf("default endpoint must time out: %v", apiErr)
	}
	t.Logf("default endpoint timed out after %s", time.Since(start))
	start = time.Now()
	info, apiErr := client.FetchFirmwareInfo()
	if apiErr != nil {
		t.Fatalf("35s firmware/info response must succeed: %v", apiErr)
	}
	if info.ProductID != "opnsense" || len(info.InstalledPlugins) != 1 || info.InstalledPlugins[0].Name != "os-example" {
		t.Fatalf("unexpected firmware inventory: %+v", info)
	}
	if client.httpClient.Timeout != 15*time.Second {
		t.Fatalf("firmware fetch mutated global timeout: %s", client.httpClient.Timeout)
	}
	t.Logf("firmware/info decoded installed plugin after %s; global timeout unchanged", time.Since(start))
}

func TestFirmwareInfoHonorsCallerContext(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	defer server.Close()
	client.httpClient.Timeout = 15 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.WithContext(ctx).FetchFirmwareInfo(); err == nil || !strings.Contains(err.Message, "deadline exceeded") {
		t.Fatalf("caller deadline must bound firmware/info: %v", err)
	}
	if client.httpClient.Timeout != 15*time.Second {
		t.Fatal("global timeout changed")
	}
}

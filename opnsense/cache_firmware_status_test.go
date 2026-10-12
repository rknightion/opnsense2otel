package opnsense

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestFirmwareStatusEmptyLastCheckHourlyBound(t *testing.T) {
	for _, body := range []string{
		`{"last_check":"","product":{"product_id":"opnsense"},"status":"none"}`,
		`{"product":{"product_id":"opnsense"},"status":"none"}`,
		`{"last_check":"   ","product":{"product_id":"opnsense"},"status":"none"}`,
	} {
		t.Run(body, func(t *testing.T) {
			var requests atomic.Int64
			server, client := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = io.WriteString(w, body) })
			defer server.Close()
			client.SetEndpointCacheTTL("firmware", 12*time.Hour)
			clock := withFakeClock(t, client)
			// Simulate the real config-snapshot consumer's 10s polling for an hour.
			for i := 0; i < 360; i++ {
				status, err := client.FetchFirmwareStatus()
				if err != nil {
					t.Fatal(err)
				}
				if status.CheckPresent || status.ProductId != "opnsense" {
					t.Fatalf("unexpected unchecked status: %+v", status)
				}
				clock.Advance(10 * time.Second)
			}
			if got := requests.Load(); got != 4 {
				t.Fatalf("empty last_check: got %d requests/hour, want 4 (15m TTL)", got)
			}
			snap := client.CacheSnapshot()
			if len(snap) != 1 || snap[0].TTL != 15*time.Minute {
				t.Fatalf("effective TTL must be 15m, got %+v", snap)
			}
			if _, err := client.FetchFirmwareStatus(); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 5 {
				t.Fatal("entry must expire at the 15m boundary")
			}
			t.Logf("360 polls over [0,1h): 4 upstream requests; effective TTL 15m")
		})
	}
}

func TestFirmwareStatusMalformedResponseNotCached(t *testing.T) {
	for _, body := range []string{`not json`, `{"last_check":42}`} {
		t.Run(body, func(t *testing.T) {
			var requests atomic.Int64
			server, client := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, body)
			})
			defer server.Close()
			client.SetEndpointCacheTTL("firmware", 12*time.Hour)
			for i := 0; i < 2; i++ {
				if _, err := client.FetchFirmwareStatus(); err == nil {
					t.Fatal("malformed firmware response must fail decoding")
				}
			}
			if requests.Load() != 2 || len(client.CacheSnapshot()) != 0 {
				t.Fatal("malformed responses must never be cached")
			}
		})
	}
}

func TestFirmwareStatusEmptyLastCheckRespectsConfiguredTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, 5 * time.Minute} {
		t.Run(ttl.String(), func(t *testing.T) {
			var requests atomic.Int64
			server, client := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, `{"last_check":""}`)
			})
			defer server.Close()
			client.SetEndpointCacheTTL("firmware", ttl)
			clock := withFakeClock(t, client)
			for i := 0; i < 2; i++ {
				if _, err := client.FetchFirmwareStatus(); err != nil {
					t.Fatal(err)
				}
			}
			if ttl == 0 {
				if requests.Load() != 2 || len(client.CacheSnapshot()) != 0 {
					t.Fatal("zero TTL must disable caching")
				}
				return
			}
			if requests.Load() != 1 {
				t.Fatal("positive short configured TTL must cache")
			}
			snap := client.CacheSnapshot()
			if len(snap) != 1 || snap[0].TTL != ttl {
				t.Fatalf("configured TTL must cap unchecked TTL: %+v", snap)
			}
			clock.Advance(ttl)
			if _, err := client.FetchFirmwareStatus(); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 2 {
				t.Fatal("configured shorter TTL must expire")
			}
		})
	}
}

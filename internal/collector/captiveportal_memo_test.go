package collector

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/promslog"
	"github.com/rknightion/opnsense2otel/v5/internal/options"
	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

type voucherFixture struct {
	inventory, groups, vouchers, sessions int
	empty, partial                        bool
	server                                *httptest.Server
	client                                *opnsense.Client
}

func newVoucherFixture(t *testing.T) *voucherFixture {
	t.Helper()
	f := &voucherFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/captiveportal/session/zones", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `["Guest"]`) })
	mux.HandleFunc("/api/captiveportal/session/search", func(w http.ResponseWriter, _ *http.Request) {
		f.sessions++
		fmt.Fprintf(w, `{"total":%d,"rows":[]}`, f.sessions)
	})
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) {
		f.inventory++
		if f.empty {
			fmt.Fprint(w, `[]`)
		} else {
			fmt.Fprint(w, `["provider"]`)
		}
	})
	mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider", func(w http.ResponseWriter, _ *http.Request) {
		f.groups++
		if f.partial {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `["group"]`)
	})
	mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/provider/group", func(w http.ResponseWriter, _ *http.Request) {
		f.vouchers++
		fmt.Fprint(w, `[{"state":"unused","expirytime":1900000000}]`)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	f.client = newCollectorTestClient(t, f.server)
	f.client.SetEndpointCacheTTL("captivePortalVoucherProviders", time.Hour)
	return f
}
func voucherPoll(t *testing.T, c *captivePortalCollector, f *voucherFixture, expected float64) {
	t.Helper()
	metrics := collectMetrics(t, c, f.client.WithContext(context.Background()))
	got := map[string]float64{}
	session := false
	for _, m := range metrics {
		labels := getMetricLabels(m)
		if hasFqName(m, "opnsense_captiveportal_vouchers") {
			if len(labels) != 4 || labels["provider"] != "provider" || labels["group"] != "group" || labels["opnsense_instance"] != "test" {
				t.Fatalf("wrong voucher labels: %v", labels)
			}
			got["state:"+labels["state"]] = getMetricValue(m)
		}
		if hasFqName(m, "opnsense_captiveportal_voucher_group_next_expiry_timestamp_seconds") {
			if !reflect.DeepEqual(labels, map[string]string{"provider": "provider", "group": "group", "opnsense_instance": "test"}) {
				t.Fatalf("wrong expiry labels: %v", labels)
			}
			got["expiry"] = getMetricValue(m)
		}
		if hasFqName(m, "opnsense_captiveportal_sessions") && getMetricValue(m) == float64(f.sessions) {
			session = true
		}
	}
	want := map[string]float64{}
	if expected == 1 {
		want = map[string]float64{"state:unused": 1, "state:valid": 0, "state:expired": 0, "expiry": 1900000000}
	}
	if !reflect.DeepEqual(got, want) || !session {
		t.Fatalf("voucher series=%v, want %v; current session metric=%v", got, want, session)
	}
}
func TestCaptivePortalVouchersMemoised(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newCollector := func() *captivePortalCollector {
		c := &captivePortalCollector{subsystem: CaptivePortalSubsystem, now: func() time.Time { return now }}
		c.Register(namespace, "test", promslog.NewNopLogger())
		return c
	}
	t.Run("expiry and sessions", func(t *testing.T) {
		f := newVoucherFixture(t)
		c := newCollector()
		voucherPoll(t, c, f, 1)
		now = now.Add(time.Minute)
		voucherPoll(t, c, f, 1)
		if f.inventory != 1 || f.groups != 1 || f.vouchers != 1 || f.sessions != 2 {
			t.Fatalf("reads providers=%d groups=%d vouchers=%d sessions=%d, want 1,1,1,2", f.inventory, f.groups, f.vouchers, f.sessions)
		}
		now = now.Add(4 * time.Minute)
		voucherPoll(t, c, f, 1)
		if f.inventory != 2 || f.groups != 2 || f.vouchers != 2 || f.sessions != 3 {
			t.Fatalf("after expiry: providers=%d groups=%d vouchers=%d sessions=%d", f.inventory, f.groups, f.vouchers, f.sessions)
		}
	})
	t.Run("partial results are not cached", func(t *testing.T) {
		f := newVoucherFixture(t)
		c := newCollector()
		f.partial = true
		voucherPoll(t, c, f, 0)
		f.partial = false
		voucherPoll(t, c, f, 1)
		if f.inventory != 2 {
			t.Fatalf("partial inventory reads=%d, want 2", f.inventory)
		}
	})
	t.Run("empty inventory", func(t *testing.T) {
		f := newVoucherFixture(t)
		c := newCollector()
		f.empty = true
		voucherPoll(t, c, f, 0)
		f.empty = false
		voucherPoll(t, c, f, 0)
		if f.inventory != 1 {
			t.Fatalf("empty inventory reads=%d, want 1", f.inventory)
		}
		now = now.Add(5 * time.Minute)
		voucherPoll(t, c, f, 1)
		if f.inventory != 2 {
			t.Fatalf("expired empty inventory reads=%d, want 2", f.inventory)
		}
	})
	t.Run("rebind", func(t *testing.T) {
		first := newVoucherFixture(t)
		second := newVoucherFixture(t)
		second.empty = true
		c := newCollector()
		voucherPoll(t, c, first, 1)
		voucherPoll(t, c, first, 1)
		if first.inventory != 1 {
			t.Fatalf("first inventory was not warm: %d", first.inventory)
		}
		voucherPoll(t, c, second, 0)
		if second.inventory != 1 {
			t.Fatalf("rebound inventory reads=%d, want 1", second.inventory)
		}
	})
}

func TestVoucherInventoryRepairControls(t *testing.T) {
	t.Run("zero ttl fetches live", func(t *testing.T) {
		f := newVoucherFixture(t)
		c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
		c.Register(namespace, "test", promslog.NewNopLogger())
		voucherPoll(t, c, f, 1)
		voucherPoll(t, c, f, 1)
		if f.inventory != 1 {
			t.Fatalf("expected warm cache, got %d reads", f.inventory)
		}
		f.client.SetEndpointCacheTTL("captivePortalVoucherProviders", 0)
		voucherPoll(t, c, f, 1)
		if f.inventory != 2 {
			t.Fatalf("disabled cache: inventory reads %d, want 2", f.inventory)
		}
	})
	for _, tc := range []struct {
		name, bad string
		states    map[string]float64
	}{
		{"null row", `[{"state":"unused"},null]`, map[string]float64{"unused": 1, "valid": 0, "expired": 0, "": 1}},
		{"object state", `[{"state":"unused"},{"state":{"invalid":true}}]`, map[string]float64{"unused": 1, "valid": 0, "expired": 0, `{"invalid":true}`: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.bad
			reads := 0
			mux := captivePortalCollectorMux(t)
			mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `["provider"]`) })
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `["group"]`) })
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/provider/group", func(w http.ResponseWriter, _ *http.Request) { reads++; fmt.Fprint(w, body) })
			server := httptest.NewServer(mux)
			defer server.Close()
			client := newCollectorTestClient(t, server)
			client.SetEndpointCacheTTL("captivePortalVoucherProviders", time.Hour)
			c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
			c.Register(namespace, "test", promslog.NewNopLogger())
			first := collectMetrics(t, c, client)
			count := 0.0
			states := map[string]float64{}
			for _, m := range first {
				if hasFqName(m, "opnsense_captiveportal_vouchers") {
					states[getMetricLabels(m)["state"]] = getMetricValue(m)
				}
				if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "unused" {
					count = getMetricValue(m)
				}
			}
			if count != 1 || !reflect.DeepEqual(states, tc.states) {
				t.Fatalf("partial voucher states=%v, want the unmemoised emission %v", states, tc.states)
			}
			body = `[{"state":"unused"},{"state":"unused"}]`
			second := collectMetrics(t, c, client)
			count = 0
			for _, m := range second {
				if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "unused" {
					count = getMetricValue(m)
				}
			}
			if count != 2 || reads != 2 {
				t.Fatalf("recovery: unused=%v reads=%d, want 2,2", count, reads)
			}
		})
	}
	for _, bad := range []string{`null`, `[null]`} {
		t.Run("malformed provider body recovers/"+bad, func(t *testing.T) {
			body := bad
			reads := 0
			mux := captivePortalCollectorMux(t)
			mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) { reads++; fmt.Fprint(w, body) })
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `[]`) })
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `["group"]`) })
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/provider/group", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `[{"state":"unused"}]`) })
			server := httptest.NewServer(mux)
			defer server.Close()
			client := newCollectorTestClient(t, server)
			client.SetEndpointCacheTTL("captivePortalVoucherProviders", time.Hour)
			c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
			c.Register(namespace, "test", promslog.NewNopLogger())
			collectMetrics(t, c, client)
			body = `["provider"]`
			metrics := collectMetrics(t, c, client)
			found := false
			for _, m := range metrics {
				if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "unused" && getMetricValue(m) == 1 {
					found = true
				}
			}
			if !found || reads != 2 {
				t.Fatalf("recovery: voucher=%v reads=%d, want true,2", found, reads)
			}
		})
	}
}

func TestVoucherInventoryMixedPayloads(t *testing.T) {
	for _, tc := range []struct{ name, providers, groups, rows string }{
		{"provider sibling", `["provider",null]`, `["group"]`, `[{"state":"unused","expirytime":0}]`},
		{"group sibling", `["provider"]`, `["group",null]`, `[{"state":"unused","expirytime":0}]`},
		{"object expiry", `["provider"]`, `["group"]`, `[{"state":"unused","expirytime":{}}]`},
		{"unsupported expiry", `["provider"]`, `["group"]`, `[{"state":"unused","expirytime":"invalid"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers, groups, rows := tc.providers, tc.groups, tc.rows
			reads := 0
			mux := captivePortalCollectorMux(t)
			mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) { reads++; fmt.Fprint(w, providers) })
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, groups) })
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/provider/group", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, rows) })
			server := httptest.NewServer(mux)
			defer server.Close()
			client := newCollectorTestClient(t, server)
			client.SetEndpointCacheTTL("captivePortalVoucherProviders", 30*time.Minute)
			c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
			c.Register(namespace, "test", promslog.NewNopLogger())
			unused := func() float64 {
				metrics := collectMetrics(t, c, client)
				for _, m := range metrics {
					if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "unused" {
						return getMetricValue(m)
					}
				}
				return -1
			}
			if n := unused(); n != 1 {
				t.Fatalf("partial valid row count=%v, want 1", n)
			}
			providers = `["provider"]`
			groups = `["group"]`
			rows = `[{"state":"unused","expirytime":0},{"state":"unused","expirytime":0}]`
			if n := unused(); n != 2 || reads != 2 {
				t.Fatalf("recovery count=%v provider reads=%d, want 2,2", n, reads)
			}
		})
	}
}

func TestVoucherExpiryParsingRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, first string
		wantExpiry  float64
		recover     bool
	}{
		{"escaped numeric", `[{"state":"unused","expirytime":"\u0031\u0032\u0033","validity":3600}]`, 0, true},
		{"numeric", `[{"state":"unused","expirytime":123,"validity":3600}]`, 123, false},
		{"fractional string", `[{"state":"unused","expirytime":"123.5","validity":"3600"}]`, 123, false},
		{"zero", `[{"state":"unused","expirytime":0,"validity":3600}]`, 0, false},
		{"null", `[{"state":"unused","expirytime":null,"validity":3600}]`, 0, false},
		{"empty array", `[{"state":"unused","expirytime":[],"validity":3600}]`, 0, false},
		{"empty string", `[{"state":"unused","expirytime":"","validity":3600}]`, 0, false},
		{"out of range", `[{"state":"unused","expirytime":1e25,"validity":3600}]`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.first
			providerReads, voucherReads := 0, 0
			mux := captivePortalCollectorMux(t)
			mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) { providerReads++; fmt.Fprint(w, `["provider"]`) })
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `["group"]`) })
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/provider/group", func(w http.ResponseWriter, _ *http.Request) { voucherReads++; fmt.Fprint(w, body) })
			server := httptest.NewServer(mux)
			defer server.Close()
			client := newCollectorTestClient(t, server)
			client.SetEndpointCacheTTL("captivePortalVoucherProviders", 30*time.Minute)
			c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
			c.Register(namespace, "test", promslog.NewNopLogger())
			poll := func() (float64, float64) {
				count, expiry := -1.0, 0.0
				for _, m := range collectMetrics(t, c, client) {
					if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "unused" {
						count = getMetricValue(m)
					}
					if hasFqName(m, "opnsense_captiveportal_voucher_group_next_expiry_timestamp_seconds") {
						expiry = getMetricValue(m)
					}
				}
				return count, expiry
			}
			if count, expiry := poll(); count != 1 || expiry != tc.wantExpiry {
				t.Fatalf("first count=%v expiry=%v, want 1,%v", count, expiry, tc.wantExpiry)
			}
			if tc.recover {
				body = `[{"state":"unused","expirytime":123,"validity":3600},{"state":"unused","expirytime":null}]`
			}
			count, expiry := poll()
			wantCount := 1.0
			wantReads := 1
			wantExpiry := tc.wantExpiry
			if tc.recover {
				wantCount = 2
				wantReads = 2
				wantExpiry = 123
			}
			if count != wantCount || expiry != wantExpiry || providerReads != wantReads || voucherReads != wantReads {
				t.Fatalf("second count=%v expiry=%v provider=%d voucher=%d, want %v,%v,%d,%d", count, expiry, providerReads, voucherReads, wantCount, wantExpiry, wantReads, wantReads)
			}
		})
	}
}

// TestVoucherExpiryBoundaryCacheAdmission drives the real collector: a value one
// past the signed 64-bit range leaves the inventory incomplete and uncached,
// while the range's own endpoints stay cacheable.
func TestVoucherExpiryBoundaryCacheAdmission(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("boundary literals are for a 64-bit int; release targets are amd64 and arm64")
	}
	for _, tc := range []struct {
		name, expiry string
		wantReads    int
		wantSeries   bool
		wantValue    float64
	}{
		{"below minimum", "-9223372036854775809", 2, false, 0},
		{"below minimum string", `"-9223372036854775809"`, 2, false, 0},
		{"minimum", "-9223372036854775808", 1, false, 0},
		{"minimum string", `"-9223372036854775808"`, 1, false, 0},
		{"above maximum", "9223372036854775808", 2, false, 0},
		{"maximum", "9223372036854775807", 1, true, 9223372036854775807},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providerReads, voucherReads := 0, 0
			mux := captivePortalCollectorMux(t)
			mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) {
				providerReads++
				fmt.Fprint(w, `["provider"]`)
			})
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider", func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `["group"]`)
			})
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/provider/group", func(w http.ResponseWriter, _ *http.Request) {
				voucherReads++
				fmt.Fprintf(w, `[{"state":"unused","expirytime":%s,"validity":3600}]`, tc.expiry)
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := newCollectorTestClient(t, server)
			client.SetEndpointCacheTTL("captivePortalVoucherProviders", 30*time.Minute)
			c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
			c.Register(namespace, "test", promslog.NewNopLogger())
			for poll := 1; poll <= 2; poll++ {
				count, hasSeries, value := -1.0, false, 0.0
				for _, m := range collectMetrics(t, c, client) {
					if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "unused" {
						count = getMetricValue(m)
					}
					if hasFqName(m, "opnsense_captiveportal_voucher_group_next_expiry_timestamp_seconds") {
						hasSeries, value = true, getMetricValue(m)
					}
				}
				if count != 1 || hasSeries != tc.wantSeries || value != tc.wantValue {
					t.Fatalf("poll %d: unused=%v next expiry present=%v value=%v, want 1,%v,%v", poll, count, hasSeries, value, tc.wantSeries, tc.wantValue)
				}
			}
			if providerReads != tc.wantReads || voucherReads != tc.wantReads {
				t.Fatalf("reads provider=%d voucher=%d, want %d: an out-of-range expiry must stay incomplete and uncached, an in-range one cached", providerReads, voucherReads, tc.wantReads)
			}
		})
	}
}

func TestVoucherMemoClockGuards(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newClocked := func(now *time.Time) *captivePortalCollector {
		c := &captivePortalCollector{subsystem: CaptivePortalSubsystem, now: func() time.Time { return *now }}
		c.Register(namespace, "test", promslog.NewNopLogger())
		return c
	}
	t.Run("backward clock refetches", func(t *testing.T) {
		now := start
		f := newVoucherFixture(t)
		c := newClocked(&now)
		voucherPoll(t, c, f, 1)
		now = start.Add(-time.Minute)
		voucherPoll(t, c, f, 1)
		if f.inventory != 2 {
			t.Fatalf("inventory reads=%d, want 2: an entry from the future is not fresh", f.inventory)
		}
	})
	t.Run("expired entry is invalidated by a failed refresh", func(t *testing.T) {
		now := start
		f := newVoucherFixture(t)
		c := newClocked(&now)
		voucherPoll(t, c, f, 1)
		now = start.Add(5 * time.Minute)
		f.partial = true
		voucherPoll(t, c, f, 0)
		f.partial = false
		now = start.Add(time.Minute)
		voucherPoll(t, c, f, 1)
		if f.inventory != 3 {
			t.Fatalf("inventory reads=%d, want 3: a stale entry must not revive after a partial refresh", f.inventory)
		}
	})
}

func TestVoucherCollectorWarningOmitsResponseBody(t *testing.T) {
	const code = "SENTINEL-VOUCHER-CODE"
	mux := captivePortalCollectorMux(t)
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"username":"`+code+`"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)
	var logs bytes.Buffer
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	collectMetrics(t, c, client)
	if !bytes.Contains(logs.Bytes(), []byte("failed to fetch captive portal vouchers")) {
		t.Fatalf("expected the collector warning, got: %s", logs.String())
	}
	if bytes.Contains(logs.Bytes(), []byte(code)) {
		t.Fatalf("voucher code reached the collector warning: %s", logs.String())
	}
}

func TestVoucherCollectorWarningOmitsNonHTTPBytes(t *testing.T) {
	const code = "SENTINEL-VOUCHER-CODE"
	mux := captivePortalCollectorMux(t)
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		fmt.Fprint(conn, code+"\r\n\r\n")
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := opnsense.NewClient(options.OPNSenseConfig{
		Protocol: "http", Host: strings.TrimPrefix(server.URL, "http://"),
		APIKey: "test", APISecret: "test", MaxRetries: 1,
	}, "test", promslog.NewNopLogger())
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	collectMetrics(t, c, &client)
	warning := ""
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "failed to fetch captive portal vouchers") {
			warning = line
		}
	}
	if warning == "" {
		t.Fatalf("expected the collector warning, got: %s", logs.String())
	}
	if strings.Contains(warning, code) {
		t.Fatalf("received bytes reached the collector warning: %s", warning)
	}
	if !strings.Contains(warning, "list_providers") || !strings.Contains(warning, "failed status code: 0") {
		t.Fatalf("warning lost its endpoint or status: %s", warning)
	}
}

func TestVoucherFeatureAbsentIsNotMemoised(t *testing.T) {
	providerReads := 0
	mux := captivePortalCollectorMux(t)
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, r *http.Request) {
		providerReads++
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)
	client.SetEndpointCacheTTL("captivePortalVoucherProviders", 30*time.Minute)
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())
	collectMetrics(t, c, client)
	collectMetrics(t, c, client)
	if providerReads != 2 {
		t.Fatalf("provider reads=%d, want 2: a feature-absent result is not memoised", providerReads)
	}
}

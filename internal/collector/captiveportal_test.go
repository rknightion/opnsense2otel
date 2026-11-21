package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/promslog"
)

func captivePortalCollectorMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	// Real API shape for sequential-from-0 zoneids: a JSON array, not an object (#73).
	mux.HandleFunc("/api/captiveportal/session/zones", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`["Guest WiFi", "Lab"]`))
	})
	mux.HandleFunc("/api/captiveportal/session/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total": 3, "rowCount": 3, "current": 1, "rows": [
		  {"sessionId": "abc", "zoneid": "0"},
		  {"sessionId": "def", "zoneid": "0"},
		  {"sessionId": "ghi", "zoneid": "1"}]}`))
	})
	mux.HandleFunc("/api/captiveportal/service/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"running"}`))
	})
	return mux
}

func TestCaptivePortalCollector_Update_Normal(t *testing.T) {
	server := httptest.NewServer(captivePortalCollectorMux(t))
	defer server.Close()
	client := newCollectorTestClient(t, server)

	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())

	metrics := collectMetrics(t, c, client)

	// zones (1) + sessions (1) + zone_sessions × 2 (2) + service_running (1) = 5
	expected := 5
	if len(metrics) != expected {
		t.Errorf("expected %d metrics, got %d", expected, len(metrics))
	}

	var foundZonesTotal, foundSessionsTotal, foundServiceRunning bool
	zoneSessionCounts := map[string]float64{}
	for _, m := range metrics {
		desc := m.Desc().String()
		labels := getMetricLabels(m)
		val := getMetricValue(m)
		switch {
		case hasFqName(m, "opnsense_captiveportal_zones"):
			foundZonesTotal = true
			if val != 2 {
				t.Errorf("zones expected 2, got %v", val)
			}
		case hasFqName(m, "opnsense_captiveportal_sessions"):
			foundSessionsTotal = true
			if val != 3 {
				t.Errorf("sessions expected 3, got %v", val)
			}
		case strings.Contains(desc, "captiveportal_zone_sessions"):
			zoneSessionCounts[labels["zone_id"]] = val
		case strings.Contains(desc, "captiveportal_service_running"):
			foundServiceRunning = true
			if val != 1 {
				t.Errorf("service_running expected 1, got %v", val)
			}
		}
	}

	if !foundZonesTotal {
		t.Error("missing zones metric")
	}
	if !foundSessionsTotal {
		t.Error("missing sessions metric")
	}
	if !foundServiceRunning {
		t.Error("missing service_running metric")
	}
	if zoneSessionCounts["0"] != 2 {
		t.Errorf("zone 0 sessions expected 2, got %v", zoneSessionCounts["0"])
	}
	if zoneSessionCounts["1"] != 1 {
		t.Errorf("zone 1 sessions expected 1, got %v", zoneSessionCounts["1"])
	}
}

// captivePortalVoucherMux extends captivePortalCollectorMux with a single
// voucher provider/group so a test can layer on top of the base session
// handlers and add voucher rows.
func captivePortalVoucherMux(t *testing.T, mux *http.ServeMux, providers, groups, vouchers string) {
	t.Helper()
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(providers))
	})
	mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/TestVoucherServer", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(groups))
	})
	mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/TestVoucherServer/TestGroup1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(vouchers))
	})
}

func TestCaptivePortalCollector_Update_Vouchers(t *testing.T) {
	mux := captivePortalCollectorMux(t)
	captivePortalVoucherMux(t, mux,
		`["TestVoucherServer"]`,
		`["TestGroup1"]`,
		// username deliberately present in the fixture to prove it never
		// reaches a metric label (sensitivity guard, tracker #227).
		`[
			{"username":"J#qfpf","validity":0,"expirytime":0,"state":"expired"},
			{"username":"ut22)*","validity":3600,"expirytime":0,"state":"expired"},
			{"username":"!kIbam","validity":3600,"expirytime":0,"state":"unused"},
			{"username":"TM,*hh","validity":3600,"expirytime":0,"state":"unused"},
			{"username":"RgfX0t","validity":3600,"expirytime":0,"state":"unused"}
		]`)

	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)

	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())

	metrics := collectMetrics(t, c, client)

	voucherCounts := map[string]float64{}
	var sawNextExpiry bool
	for _, m := range metrics {
		desc := m.Desc().String()
		labels := getMetricLabels(m)
		val := getMetricValue(m)
		if strings.Contains(desc, "captiveportal_vouchers") {
			if labels["provider"] != "TestVoucherServer" || labels["group"] != "TestGroup1" {
				t.Errorf("unexpected voucher labels: %+v", labels)
			}
			for k := range labels {
				if strings.Contains(strings.ToLower(k), "username") || strings.Contains(strings.ToLower(k), "code") {
					t.Fatalf("voucher metric leaked a code/username-shaped label: %+v", labels)
				}
			}
			voucherCounts[labels["state"]] = val
		}
		if hasFqName(m, "opnsense_captiveportal_voucher_group_next_expiry_timestamp_seconds") {
			sawNextExpiry = true
		}
	}

	if voucherCounts["expired"] != 2 {
		t.Errorf("expired count = %v, want 2", voucherCounts["expired"])
	}
	if voucherCounts["unused"] != 3 {
		t.Errorf("unused count = %v, want 3", voucherCounts["unused"])
	}
	if voucherCounts["valid"] != 0 {
		t.Errorf("valid count = %v, want 0 (zero-filled, no valid vouchers)", voucherCounts["valid"])
	}
	if sawNextExpiry {
		t.Error("expirytime is 0 on every row; next_expiry_seconds should not be emitted")
	}
}

func TestCaptivePortalCollector_Update_VouchersNoProvider(t *testing.T) {
	// Core feature present, but no voucher-type auth server configured: the
	// list_providers call succeeds with an empty array, so no voucher series
	// are emitted at all — this must not be confused with feature-absent.
	mux := captivePortalCollectorMux(t)
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)

	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())

	metrics := collectMetrics(t, c, client)
	for _, m := range metrics {
		if strings.Contains(m.Desc().String(), "captiveportal_vouchers") ||
			hasFqName(m, "opnsense_captiveportal_voucher_group_next_expiry_timestamp_seconds") {
			t.Errorf("unexpected voucher metric with no provider configured: %s", m.Desc().String())
		}
	}
}

func TestCaptivePortalCollector_Update_Unconfigured(t *testing.T) {
	// Core feature: present but unconfigured. Totals still emitted so the
	// dashboard sentinel query (zones > 0) can correctly hide the tab.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/captiveportal/session/zones", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/api/captiveportal/session/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total": 0, "rowCount": 0, "current": 1, "rows": []}`))
	})
	mux.HandleFunc("/api/captiveportal/service/status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"disabled"}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)

	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())

	metrics := collectMetrics(t, c, client)

	// zones (0) + sessions (0) + service_running = 3
	expected := 3
	if len(metrics) != expected {
		t.Errorf("expected %d metrics (unconfigured), got %d", expected, len(metrics))
	}

	for _, m := range metrics {
		desc := m.Desc().String()
		val := getMetricValue(m)
		if hasFqName(m, "opnsense_captiveportal_zones") && val != 0 {
			t.Errorf("zones expected 0, got %v", val)
		}
		if hasFqName(m, "opnsense_captiveportal_sessions") && val != 0 {
			t.Errorf("sessions expected 0, got %v", val)
		}
		if strings.Contains(desc, "captiveportal_service_running") && val != 0 {
			t.Errorf("service_running expected 0 (disabled), got %v", val)
		}
	}
}

func TestCaptivePortalCollector_Update_FeatureAbsent(t *testing.T) {
	// 404 on zones → Present=false → fully silent, no service probe.
	mux := http.NewServeMux() // no handlers, all 404
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)

	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())

	metrics := collectMetrics(t, c, client)
	if len(metrics) != 0 {
		t.Errorf("expected 0 metrics when feature absent, got %d", len(metrics))
	}
}

func TestCaptivePortalVouchersMemoised(t *testing.T) {
	mux := captivePortalCollectorMux(t)
	calls := 0
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`[]`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())
	collectMetrics(t, c, client.WithContext(context.Background()))
	collectMetrics(t, c, client.WithContext(context.Background()))
	if calls != 1 {
		t.Fatalf("provider requests = %d, want 1 for complete empty inventory", calls)
	}
}

func TestCaptivePortalVoucherMalformedRetries(t *testing.T) {
	mux := captivePortalCollectorMux(t)
	calls := 0
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte(`null`))
			return
		}
		w.Write([]byte(`[]`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())
	collectMetrics(t, c, client)
	collectMetrics(t, c, client)
	collectMetrics(t, c, client)
	if calls != 2 {
		t.Fatalf("provider calls = %d, want malformed retry then cache", calls)
	}
}

func TestCaptivePortalVoucherPartialRetryAndExpiry(t *testing.T) {
	calls := map[string]int{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/api/captiveportal/session/zones":
			w.Write([]byte(`["Guest"]`))
		case "/api/captiveportal/session/search":
			w.Write([]byte(`{"total":1,"rows":[{"zoneid":"0"}]}`))
		case "/api/captiveportal/service/status":
			w.Write([]byte(`{"status":"running"}`))
		case "/api/captiveportal/voucher/list_providers":
			w.Write([]byte(`["p"]`))
		case "/api/captiveportal/voucher/list_voucher_groups/p":
			w.Write([]byte(`["good","bad"]`))
		case "/api/captiveportal/voucher/list_vouchers/p/good":
			w.Write([]byte(`[{"state":"unused","expirytime":1234}]`))
		case "/api/captiveportal/voucher/list_vouchers/p/bad":
			if calls[r.URL.Path] == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write([]byte(`[{"state":"valid"}]`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())
	now := time.Now()
	c.voucherNow = func() time.Time { return now }
	assertGroups := func(wantBad bool) {
		t.Helper()
		metrics := collectMetrics(t, c, client)
		good, bad, expiry := false, false, false
		for _, m := range metrics {
			labels := getMetricLabels(m)
			if hasFqName(m, "opnsense_captiveportal_vouchers") && labels["provider"] == "p" {
				if labels["group"] == "good" && labels["state"] == "unused" && getMetricValue(m) == 1 {
					good = true
				}
				if labels["group"] == "bad" && labels["state"] == "valid" && getMetricValue(m) == 1 {
					bad = true
				}
			}
			if hasFqName(m, "opnsense_captiveportal_voucher_group_next_expiry_timestamp_seconds") && labels["group"] == "good" && getMetricValue(m) == 1234 {
				expiry = true
			}
		}
		if !good || !expiry || bad != wantBad {
			t.Fatalf("good=%v expiry=%v bad=%v want bad=%v", good, expiry, bad, wantBad)
		}
	}
	assertGroups(false)
	assertGroups(true)
	assertGroups(true)
	provider := "/api/captiveportal/voucher/list_providers"
	if calls[provider] != 2 {
		t.Fatalf("provider calls = %d, want retry then cache", calls[provider])
	}
	now = now.Add(5*time.Minute - time.Nanosecond)
	assertGroups(true)
	if calls[provider] != 2 {
		t.Fatalf("provider calls near expiry = %d, want cache hit", calls[provider])
	}
	now = now.Add(time.Nanosecond)
	assertGroups(true)
	if calls[provider] != 3 {
		t.Fatalf("provider calls after expiry = %d", calls[provider])
	}
	for _, path := range []string{"/api/captiveportal/session/zones", "/api/captiveportal/session/search", "/api/captiveportal/service/status"} {
		if calls[path] != 5 {
			t.Errorf("%s calls = %d, want 5", path, calls[path])
		}
	}
}

func TestCaptivePortalVouchersRebind(t *testing.T) {
	makeServer := func(provider string) *httptest.Server {
		mux := captivePortalCollectorMux(t)
		mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`["` + provider + `"]`)) })
		mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/"+provider, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`["g"]`)) })
		mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/"+provider+"/g", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`[{"state":"valid"}]`)) })
		return httptest.NewServer(mux)
	}
	first, second := makeServer("first"), makeServer("second")
	defer first.Close()
	defer second.Close()
	for _, tc := range []struct {
		server   *httptest.Server
		provider string
	}{{first, "first"}, {second, "second"}} {
		client := newCollectorTestClient(t, tc.server)
		parent, err := New(client, promslog.NewNopLogger(), tc.provider)
		if err != nil {
			t.Fatal(err)
		}
		var portal *captivePortalCollector
		for _, sub := range parent.collectors {
			if p, ok := sub.(*captivePortalCollector); ok {
				portal = p
				break
			}
		}
		if portal == nil {
			t.Fatal("missing captive portal collector")
		}
		found := false
		for _, m := range collectMetrics(t, portal, client.WithContext(context.Background())) {
			if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "valid" {
				labels := getMetricLabels(m)
				if labels["provider"] != tc.provider || labels["opnsense_instance"] != tc.provider {
					t.Fatalf("wrong binding: %v", labels)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("missing valid voucher")
		}
	}
}

func TestCaptivePortalVoucherMalformedElements(t *testing.T) {
	for _, fixture := range []struct {
		name, rows string
		valid      bool
	}{
		{"null", `[null]`, false},
		{"mixed", `[{"state":"unused"},null,{"state":{"bad":1}}]`, true},
		{"object-only", `[{"state":{"bad":1}}]`, false},
		{"mixed-object", `[{"state":"unused"},{"state":{"bad":1}}]`, true},
		{"mixed-array", `[{"state":"unused"},{"state":["bad"]}]`, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			mux := captivePortalCollectorMux(t)
			calls := 0
			mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`["p"]`)) })
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/p", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`["g"]`)) })
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/p/g", func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					w.Write([]byte(fixture.rows))
					return
				}
				w.Write([]byte(`[{"state":"valid"}]`))
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			client := newCollectorTestClient(t, server)
			c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
			c.Register(namespace, "test", promslog.NewNopLogger())
			metrics := collectMetrics(t, c, client)
			found := false
			for _, m := range metrics {
				if !hasFqName(m, "opnsense_captiveportal_vouchers") {
					continue
				}
				state := getMetricLabels(m)["state"]
				if state != "valid" && state != "unused" && state != "expired" {
					t.Errorf("malformed row emitted unexpected state %q", state)
				}
				if state != "unused" && getMetricValue(m) != 0 {
					t.Errorf("unexpected positive state %q: %v", state, getMetricValue(m))
				}
				if state == "unused" && getMetricValue(m) == 1 {
					found = true
				}
			}
			if found != fixture.valid {
				t.Errorf("valid sibling emitted = %v, want %v", found, fixture.valid)
			}
			metrics = collectMetrics(t, c, client)
			if calls != 2 {
				t.Errorf("voucher calls = %d, want retry", calls)
			}
			found = false
			for _, m := range metrics {
				if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "valid" && getMetricValue(m) == 1 {
					found = true
				}
			}
			if !found {
				t.Error("retry did not emit valid row")
			}
		})
	}
}

func TestCaptivePortalVoucherScalarStates(t *testing.T) {
	mux := captivePortalCollectorMux(t)
	calls := 0
	mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`["p"]`)) })
	mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/p", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`["g"]`)) })
	mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/p/g", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte(`[{"state":"future"},{"state":42},{"state":true}]`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := newCollectorTestClient(t, server)
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	c.Register(namespace, "test", promslog.NewNopLogger())
	for i := 0; i < 2; i++ {
		seen := map[string]float64{}
		for _, m := range collectMetrics(t, c, client) {
			if hasFqName(m, "opnsense_captiveportal_vouchers") {
				seen[getMetricLabels(m)["state"]] = getMetricValue(m)
			}
		}
		for _, state := range []string{"future", "42", "true"} {
			if seen[state] != 1 {
				t.Errorf("poll %d scalar %q count = %v", i+1, state, seen[state])
			}
		}
	}
	if calls != 1 {
		t.Errorf("complete scalar inventory fetched %d times", calls)
	}
}

func TestCaptivePortalVoucherMalformedNames(t *testing.T) {
	for _, level := range []string{"providers", "groups"} {
		t.Run(level, func(t *testing.T) {
			mux := captivePortalCollectorMux(t)
			calls := 0
			mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if level == "providers" && calls == 1 {
					w.Write([]byte(`["p",null]`))
					return
				}
				w.Write([]byte(`["p"]`))
			})
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/p", func(w http.ResponseWriter, _ *http.Request) {
				if level == "groups" && calls == 1 {
					w.Write([]byte(`["g",null]`))
					return
				}
				w.Write([]byte(`["g"]`))
			})
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/p/g", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`[{"state":"valid"}]`)) })
			mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`[]`)) })
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/p/", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`[]`)) })
			server := httptest.NewServer(mux)
			defer server.Close()
			client := newCollectorTestClient(t, server)
			c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
			c.Register(namespace, "test", promslog.NewNopLogger())
			for i := 0; i < 2; i++ {
				found := false
				for _, m := range collectMetrics(t, c, client) {
					if hasFqName(m, "opnsense_captiveportal_vouchers") && getMetricLabels(m)["state"] == "valid" && getMetricValue(m) == 1 {
						found = true
					}
				}
				if !found {
					t.Errorf("poll %d lost valid sibling", i+1)
				}
			}
			if calls != 2 {
				t.Errorf("provider calls = %d, want retry", calls)
			}
		})
	}
}

func TestCaptivePortalCollector_Name(t *testing.T) {
	c := &captivePortalCollector{subsystem: CaptivePortalSubsystem}
	if c.Name() != CaptivePortalSubsystem {
		t.Errorf("expected %s, got %s", CaptivePortalSubsystem, c.Name())
	}
}

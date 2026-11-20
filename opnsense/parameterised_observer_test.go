package opnsense

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func assertRegisteredObservations(t *testing.T, client *Client, obs *fakeObserver, want int) {
	t.Helper()
	registered := make(map[string]bool)
	for _, path := range client.Endpoints() {
		registered[string(path)] = true
	}
	calls := obs.snapshot()
	if len(calls) != want {
		t.Errorf("observed %d requests, want %d", len(calls), want)
	}
	for _, call := range calls {
		if !registered[call.endpoint] {
			t.Errorf("observed unregistered endpoint %q", call.endpoint)
		}
	}
}

type retainingResultObserver struct {
	endpointAtCallback string
	err                *APICallError
}

func (o *retainingResultObserver) ObserveAPIRequest(string, int, time.Duration) {}

func (o *retainingResultObserver) ObserveAPIRequestResult(_ string, _ int, _ time.Duration, err *APICallError) {
	o.err = err
	if err != nil {
		o.endpointAtCallback = err.Endpoint
	}
}

func TestTrafficTopDoesNotMutateObservedError(t *testing.T) {
	server, mux, client := newTestClientWithMux(t)
	defer server.Close()
	obs := &retainingResultObserver{}
	client.SetRequestObserver(obs)
	mux.HandleFunc("/api/diagnostics/traffic/top/id-a,id-b", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "synthetic failure", http.StatusBadRequest)
	})

	_, err := client.FetchTrafficTop([]string{"id-a", "id-b"})
	if err == nil || obs.err == nil {
		t.Fatalf("expected returned and observed errors: returned=%v observed=%v", err, obs.err)
	}
	wantOriginal := string(client.Endpoints()["trafficTop"]) + "/id-a,id-b"
	if obs.endpointAtCallback != wantOriginal || obs.err.Endpoint != obs.endpointAtCallback {
		t.Errorf("observed error endpoint changed: at callback %q, after return %q, want %q", obs.endpointAtCallback, obs.err.Endpoint, wantOriginal)
	}
	if err == obs.err {
		t.Error("returned error aliases published error")
	}
	if err.Endpoint != string(client.Endpoints()["trafficTop"]) || err.StatusCode != obs.err.StatusCode || err.Message != obs.err.Message {
		t.Errorf("returned error = %+v, observed = %+v", err, obs.err)
	}
}

func TestParameterisedFetchesObserveRegisteredPath(t *testing.T) {
	t.Run("vouchers", func(t *testing.T) {
		server, mux, client := newTestClientWithMux(t)
		defer server.Close()
		obs := &fakeObserver{}
		client.SetRequestObserver(obs)
		mux.HandleFunc("/api/captiveportal/voucher/list_providers", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `["provider-a","provider-b"]`)
		})
		mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider-a", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `["group-a","group-b"]`)
		})
		mux.HandleFunc("/api/captiveportal/voucher/list_voucher_groups/provider-b", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `["group-c"]`)
		})
		for _, path := range []string{"provider-a/group-a", "provider-a/group-b", "provider-b/group-c"} {
			mux.HandleFunc("/api/captiveportal/voucher/list_vouchers/"+path, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `[{"state":"unused"}]`)
			})
		}
		data, err := client.FetchCaptivePortalVouchers()
		if err != nil || len(data.Groups) != 3 {
			t.Fatalf("vouchers: %+v, error: %v", data, err)
		}
		assertRegisteredObservations(t, client, obs, 6)
	})
	t.Run("vnstat", func(t *testing.T) {
		server, mux, client := newTestClientWithMux(t)
		defer server.Close()
		obs := &fakeObserver{}
		client.SetRequestObserver(obs)
		mux.HandleFunc("/api/vnstat/service/interface_list", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"interfaces":["device-a","device-b"]}`)
		})
		mux.HandleFunc("/api/vnstat/service/get_json_data", func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("iface") {
			case "device-a", "device-b":
				fmt.Fprint(w, `{"interfaces":[{"traffic":{"total":{"rx":1}}}]}`)
			default:
				http.Error(w, "unexpected interface", http.StatusBadRequest)
			}
		})
		data, err := client.FetchVnstat()
		if err != nil || len(data.Interfaces) != 2 {
			t.Fatalf("vnstat: %+v, error: %v", data, err)
		}
		assertRegisteredObservations(t, client, obs, 3)
	})
	t.Run("traffic top", func(t *testing.T) {
		server, mux, client := newTestClientWithMux(t)
		defer server.Close()
		obs := &fakeObserver{}
		client.SetRequestObserver(obs)
		mux.HandleFunc("/api/diagnostics/traffic/top/id-a,id-b", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"id-a":{"status":"ok"},"id-b":{"status":"ok"}}`)
		})
		data, err := client.FetchTrafficTop([]string{"id-b", "id-a"})
		if err != nil || len(data.Interfaces) != 2 {
			t.Fatalf("traffic top: %+v, error: %v", data, err)
		}
		assertRegisteredObservations(t, client, obs, 1)
	})
	t.Run("traffic top error", func(t *testing.T) {
		server, mux, client := newTestClientWithMux(t)
		defer server.Close()
		obs := &fakeObserver{}
		client.SetRequestObserver(obs)
		mux.HandleFunc("/api/diagnostics/traffic/top/id-a,id-b", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "synthetic failure", http.StatusBadRequest)
		})
		_, err := client.FetchTrafficTop([]string{"id-a", "id-b"})
		if err == nil {
			t.Fatal("expected traffic top error")
		}
		if err.Endpoint != string(client.Endpoints()["trafficTop"]) {
			t.Errorf("error endpoint = %q, want registered traffic top path", err.Endpoint)
		}
		assertRegisteredObservations(t, client, obs, 1)
	})
}

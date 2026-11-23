package opnsense

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	httpBodyMarker = "SYNTHETIC-UPSTREAM-BODY-MARKER"
	// numericBodyMarker overflows the uint8 target below, which makes
	// encoding/json quote the literal in its error text.
	numericBodyMarker = "987654321"
	// floatBodyMarker overflows a float64 field in the same way.
	floatBodyMarker = "1e999"
)

func newBodyMarkerClient(t *testing.T, status int, body string) (*Client, *lockedLogBuffer, *atomic.Int64) {
	t.Helper()
	standardLogs := captureStandardLog(t)
	hits := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	logs := &lockedLogBuffer{}
	client := newFailureLogClient(t, "http", strings.TrimPrefix(server.URL, "http://"), logs)
	t.Cleanup(func() {
		if got := standardLogs.String(); strings.Contains(got, httpBodyMarker) {
			t.Errorf("standard log holds the upstream body: %s", got)
		}
	})
	return client, logs, hits
}

// assertBodyWithheld logs the error the two ways collectors do, so the check
// covers LogValue as well as Error.
func assertBodyWithheld(t *testing.T, apiErr *APICallError, logs *lockedLogBuffer, endpoint string, status int, markers ...string) {
	t.Helper()
	if apiErr == nil {
		t.Fatal("expected an error")
	}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	logger.Warn("call failed", "err", apiErr)
	logger.Warn("call failed", "err", apiErr.Error())
	captured := logs.String()
	for _, marker := range markers {
		for surface, got := range map[string]string{"Error()": apiErr.Error(), "Message": apiErr.Message, "logs": captured} {
			if strings.Contains(got, marker) {
				t.Errorf("%s holds upstream content %q: %s", surface, marker, got)
			}
		}
	}
	if apiErr.StatusCode != status {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, status)
	}
	if apiErr.Endpoint != endpoint {
		t.Errorf("Endpoint = %q, want %q", apiErr.Endpoint, endpoint)
	}
	if strings.Contains(apiErr.Message, endpoint) {
		t.Errorf("Message must be a fixed reason without the request path: %s", apiErr.Message)
	}
	wantStatus := fmt.Sprintf("HTTP %d", status)
	if !strings.Contains(apiErr.Message, wantStatus) {
		t.Errorf("Message lost the status %q: %s", wantStatus, apiErr.Message)
	}
	for surface, got := range map[string]string{"Error()": apiErr.Error(), "logs": captured} {
		if !strings.Contains(got, endpoint) {
			t.Errorf("%s lost the endpoint %q: %s", surface, endpoint, got)
		}
		if !strings.Contains(got, fmt.Sprintf("failed status code: %d", status)) {
			t.Errorf("%s lost the status %d: %s", surface, status, got)
		}
	}
}

func TestHTTPErrorBodyWithheldFromNon2xx(t *testing.T) {
	const endpoint = "api/test/failure"
	for _, status := range []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusInternalServerError,
	} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client, logs, _ := newBodyMarkerClient(t, status, `{"message":"`+httpBodyMarker+`"}`)
			var out map[string]any
			apiErr := client.do(http.MethodGet, endpoint, nil, &out)
			assertBodyWithheld(t, apiErr, logs, endpoint, status, httpBodyMarker)
		})
	}
}

func TestHTTPErrorBodyWithheldFromCachedReplay(t *testing.T) {
	client, logs, hits := newBodyMarkerClient(t, http.StatusNotFound, `{"message":"`+httpBodyMarker+`"}`)
	client.SetEndpointAbsentTTL("crowdsecAlerts", time.Hour)
	path := client.endpoints["crowdsecAlerts"]

	var out map[string]any
	live := client.do(http.MethodGet, path, nil, &out)
	assertBodyWithheld(t, live, logs, string(path), http.StatusNotFound, httpBodyMarker)

	replayed := client.do(http.MethodGet, path, nil, &out)
	assertBodyWithheld(t, replayed, logs, string(path), http.StatusNotFound, httpBodyMarker)
	if got := hits.Load(); got != 1 {
		t.Fatalf("server saw %d requests, want 1: the second call must be a cache replay", got)
	}
	if *live != *replayed {
		t.Errorf("replay differs from the live 404: live %+v, replay %+v", *live, *replayed)
	}
	entry, ok := client.cache.get(path)
	if !ok {
		t.Fatal("the 404 is no longer cached")
	}
	if len(entry.body) != 0 {
		t.Errorf("cache retained %d bytes of an error body", len(entry.body))
	}
}

func TestHTTPErrorBodyWithheldFromUnparseable200(t *testing.T) {
	const endpoint = "api/test/decode"
	type target struct {
		Count uint8 `json:"count"`
	}
	for name, tc := range map[string]struct {
		body    string
		marker  string
		partial bool
	}{
		"not json":       {body: httpBodyMarker, marker: httpBodyMarker},
		"truncated json": {body: `{"note":"` + httpBodyMarker + `",`, marker: httpBodyMarker},
		"quoted literal": {body: `{"count":` + numericBodyMarker + `,"note":"` + httpBodyMarker + `"}`, marker: numericBodyMarker, partial: true},
	} {
		t.Run(name, func(t *testing.T) {
			client, logs, _ := newBodyMarkerClient(t, http.StatusOK, tc.body)
			var out target
			apiErr := client.do(http.MethodGet, endpoint, nil, &out)
			assertBodyWithheld(t, apiErr, logs, endpoint, http.StatusOK, tc.marker, httpBodyMarker)
			if apiErr.PartialDecode != tc.partial {
				t.Errorf("PartialDecode = %v, want %v", apiErr.PartialDecode, tc.partial)
			}
		})
	}
}

// The endpoint decoders that run after a successful fetch report through the
// same error type, so their decoder text is held to the same rule.
func TestDecodeFailureWithholdsDecoderText(t *testing.T) {
	for name, tc := range map[string]struct {
		body   string
		marker string
		fetch  func(*Client) *APICallError
	}{
		"interface enumeration": {
			body: `{"` + httpBodyMarker + `":{},"` + httpBodyMarker + `":{}}`, marker: httpBodyMarker,
			fetch: func(c *Client) *APICallError { _, err := c.FetchInterfaceEnumeration(); return err },
		},
		"config snapshot": {
			body:   `{"rows":[{"uuid":"` + httpBodyMarker + `"},{"uuid":"` + httpBodyMarker + `"}]}`,
			marker: httpBodyMarker,
			fetch:  func(c *Client) *APICallError { _, err := c.FetchFirewallConfigSnapshots(); return err },
		},
		"bpf entries": {
			body:   `{"bpf-statistics":{"bpf-entry":[{"received-packets":` + floatBodyMarker + `}]}}`,
			marker: floatBodyMarker,
			fetch:  func(c *Client) *APICallError { _, err := c.FetchBPFStatistics(); return err },
		},
		"ospf overview": {
			body:   `{"response":{"spfLastExecutedMsecs":` + floatBodyMarker + `}}`,
			marker: floatBodyMarker,
			fetch:  func(c *Client) *APICallError { _, err := c.FetchFRROSPF(); return err },
		},
		"netbird status": {
			body: `{"peers":7}`, marker: "cannot unmarshal",
			fetch: func(c *Client) *APICallError { _, err := c.FetchNetbirdStatus(); return err },
		},
		"firewall rule ids": {
			body: `{"items":7}`, marker: "cannot unmarshal",
			fetch: func(c *Client) *APICallError { _, err := c.FetchFirewallRuleIDs(); return err },
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, _, _ := newBodyMarkerClient(t, http.StatusOK, tc.body)
			apiErr := tc.fetch(client)
			if apiErr == nil {
				t.Fatal("expected a decode failure, so this run proves nothing")
			}
			if got := apiErr.Error(); strings.Contains(got, tc.marker) {
				t.Errorf("Error() holds decoder text %q: %s", tc.marker, got)
			}
			if apiErr.Endpoint == "" {
				t.Error("endpoint lost")
			}
		})
	}
}

// Each decode site logs on its own line, so each is checked on its own line.
func TestCrowdSecDecodeLogsWithholdDecoderText(t *testing.T) {
	fetchStatus := func(c *Client) *APICallError {
		_, err := c.FetchCrowdSecStatusWithHub(true)
		return err
	}
	for name, tc := range map[string]struct {
		line  string
		want  int
		fetch func(*Client) *APICallError
	}{
		"alerts": {
			line: "failed to decode alerts rows", want: 1,
			fetch: func(c *Client) *APICallError { _, err := c.FetchCrowdSecAlerts(); return err },
		},
		"decisions": {
			line: "failed to decode decisions rows", want: 1,
			fetch: func(c *Client) *APICallError { _, err := c.FetchCrowdSecDecisions(); return err },
		},
		"bouncers":  {line: "failed to decode bouncers rows", want: 1, fetch: fetchStatus},
		"machines":  {line: "failed to decode machines rows", want: 1, fetch: fetchStatus},
		"hub items": {line: "failed to decode hub item rows", want: 6, fetch: fetchStatus},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/search") {
					_, _ = w.Write([]byte(`{"total":1,"rows":7,"note":"` + httpBodyMarker + `"}`))
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)
			logs := &lockedLogBuffer{}
			client := newFailureLogClient(t, "http", strings.TrimPrefix(server.URL, "http://"), logs)
			if err := tc.fetch(client); err != nil {
				t.Fatalf("fetch: %v", err)
			}
			matched := 0
			for _, line := range strings.Split(logs.String(), "\n") {
				if !strings.Contains(line, tc.line) {
					continue
				}
				matched++
				if strings.Contains(line, "cannot unmarshal") || strings.Contains(line, httpBodyMarker) {
					t.Errorf("log holds decoder text: %s", line)
				}
			}
			if matched != tc.want {
				t.Fatalf("saw %d %q lines, want %d, so this run proves nothing: %s", matched, tc.line, tc.want, logs.String())
			}
		})
	}
}

// FetchTrafficTop puts interface identifiers in the request path and reports only
// the base route as Endpoint, so Message must not carry the path back in.
func TestTrafficTopErrorOmitsInterfaceIdentifiers(t *testing.T) {
	identifiers := []string{"synthetic-if-a", "synthetic-if-b"}
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"http 500":        {status: http.StatusInternalServerError, body: httpBodyMarker},
		"unparseable 200": {status: http.StatusOK, body: httpBodyMarker},
	} {
		t.Run(name, func(t *testing.T) {
			client, _, _ := newBodyMarkerClient(t, tc.status, tc.body)
			_, apiErr := client.FetchTrafficTop(identifiers)
			if apiErr == nil {
				t.Fatal("expected an error")
			}
			for _, identifier := range identifiers {
				if strings.Contains(apiErr.Message, identifier) {
					t.Errorf("Message holds %q: %s", identifier, apiErr.Message)
				}
				if strings.Contains(apiErr.Error(), identifier) {
					t.Errorf("Error() holds %q: %s", identifier, apiErr.Error())
				}
			}
			base := string(client.endpoints["trafficTop"])
			if apiErr.Endpoint != base || !strings.Contains(apiErr.Error(), base) {
				t.Errorf("endpoint = %q, want %q in the field and Error(): %s", apiErr.Endpoint, base, apiErr.Error())
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
		})
	}
}

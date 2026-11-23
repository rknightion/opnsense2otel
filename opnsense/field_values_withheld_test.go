package opnsense

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/options"
)

// fieldSentinel stands in for an upstream field value. The tests below prove it
// never reaches an error or a log, while the field or endpoint it came from does.
const fieldSentinel = "SYNTHETIC-FIELD-VALUE-SENTINEL"

// overflowSentinel is a digit string no integer type holds, so strconv quotes
// it in its own error text.
const overflowSentinel = "99999999999999999999999"

// debugLogger writes every level without timestamps, so a numeric sentinel
// cannot collide with a clock value.
func debugLogger(w *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func assertWithheld(t *testing.T, surface, got string, sentinels ...string) {
	t.Helper()
	for _, sentinel := range sentinels {
		if strings.Contains(got, sentinel) {
			t.Errorf("%s holds the upstream value %q: %s", surface, sentinel, got)
		}
	}
}

func assertKept(t *testing.T, surface, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("%s lost %q: %s", surface, want, got)
		}
	}
}

func TestParseStringToIntWithholdsTheValue(t *testing.T) {
	const endpoint = "api/test/endpoint"
	for name, tc := range map[string]struct {
		value  string
		reason string
	}{
		"not a number":   {value: fieldSentinel, reason: "invalid syntax"},
		"out of range":   {value: overflowSentinel, reason: "value out of range"},
		"embedded space": {value: "1 " + fieldSentinel, reason: "invalid syntax"},
	} {
		t.Run(name, func(t *testing.T) {
			_, apiErr := parseStringToInt(tc.value, endpoint)
			if apiErr == nil {
				t.Fatal("expected a parse failure")
			}
			var logs bytes.Buffer
			logger := debugLogger(&logs)
			logger.Warn("parse failed", "err", apiErr)
			logger.Warn("parse failed", "err", apiErr.Error())
			for surface, got := range map[string]string{"Error()": apiErr.Error(), "Message": apiErr.Message, "logs": logs.String()} {
				assertWithheld(t, surface, got, fieldSentinel, overflowSentinel)
			}
			if apiErr.Endpoint != endpoint {
				t.Errorf("Endpoint = %q, want %q", apiErr.Endpoint, endpoint)
			}
			assertKept(t, "Message", apiErr.Message, tc.reason)
		})
	}
}

func TestCronEntryParseLogWithholdsTheValue(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"rows":[{"uuid":"u","enabled":%q,"minutes":"0","hours":"0","days":"*","months":"*","weekdays":"*","description":"d","command":"c","origin":"cron"}],"rowCount":1,"total":1,"current":1}`, fieldSentinel)
	})
	defer server.Close()
	var logs bytes.Buffer
	client.log = debugLogger(&logs)

	if _, err := client.FetchCronTable(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertWithheld(t, "logs", logs.String(), fieldSentinel)
	assertKept(t, "logs", logs.String(), "unable to parse cron entry status")
}

func TestUnboundStatusWithheldFromError(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"status":%q}`, fieldSentinel)
	})
	defer server.Close()
	var logs bytes.Buffer
	client.log = debugLogger(&logs)

	_, apiErr := client.FetchUnboundLocalData()
	if apiErr == nil {
		t.Fatal("expected the unreachable status to fail the fetch")
	}
	logger := debugLogger(&logs)
	logger.Warn("fetch failed", "err", apiErr)
	for surface, got := range map[string]string{"Error()": apiErr.Error(), "Message": apiErr.Message, "logs": logs.String()} {
		assertWithheld(t, surface, got, fieldSentinel)
	}
	assertKept(t, "Message", apiErr.Message, "unbound diagnostics status", "unbound-control unreachable")
	if !strings.HasPrefix(apiErr.Endpoint, "unbound") {
		t.Errorf("Endpoint = %q, want the unbound endpoint name", apiErr.Endpoint)
	}
}

func TestInterfaceIndexDeviceNameWithheldFromError(t *testing.T) {
	c := indexClient(t, `{"statistics":{
	 "a":{"name":"`+fieldSentinel+`","network":"<Link#1111>"},
	 "b":{"name":"`+fieldSentinel+`","network":"<Link#2222>"}}}`, http.StatusOK)
	_, apiErr := c.FetchInterfaceIndexes()
	if apiErr == nil {
		t.Fatal("accepted a device with two different kernel indexes; want an error")
	}
	for surface, got := range map[string]string{"Error()": apiErr.Error(), "Message": apiErr.Message} {
		assertWithheld(t, surface, got, fieldSentinel, "1111", "2222")
	}
	if apiErr.Endpoint == "" {
		t.Error("endpoint lost")
	}
}

// Regression pin, not a new guard: this path already passes on the base,
// because the enumeration error goes through decodeFailureReason. It stays so
// the duplicate-device text cannot start quoting a device name.
func TestInterfaceEnumerationDeviceNameWithheldFromError(t *testing.T) {
	server, client := newTestClientWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{%q:{},%q:{}}`, fieldSentinel, fieldSentinel)
	})
	defer server.Close()
	_, apiErr := client.FetchInterfaceEnumeration()
	if apiErr == nil {
		t.Fatal("expected a duplicate device to fail the enumeration")
	}
	assertWithheld(t, "Error()", apiErr.Error(), fieldSentinel)
	if apiErr.Endpoint == "" {
		t.Error("endpoint lost")
	}
}

func TestDynDNSMtimeParseLogWithholdsTheValue(t *testing.T) {
	server, mux, client := newTestClientWithMux(t)
	defer server.Close()
	mux.HandleFunc("/api/dyndns/accounts/searchItem", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"total":1,"rowCount":1,"current":1,"rows":[{"uuid":"u","enabled":"1","service":"s","hostnames":"h","zone":"","interface":"i","description":"d","current_ip":"","current_mtime":%q}]}`, fieldSentinel)
	})
	var logs bytes.Buffer
	client.log = debugLogger(&logs)

	if _, err := client.FetchDynDNSAccounts(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertWithheld(t, "logs", logs.String(), fieldSentinel)
	assertKept(t, "logs", logs.String(), "current_mtime")
}

func TestActivityParseLogsWithholdTheValues(t *testing.T) {
	for name, tc := range map[string]struct {
		header string
		want   string
	}{
		"threads total":     {header: overflowSentinel + " threads: 1 running", want: "failed to parse threads total"},
		"thread state":      {header: "5 threads: " + overflowSentinel + " running", want: "failed to parse thread-state count"},
		"no state segments": {header: "5 threads: " + fieldSentinel, want: "no thread-state segments parsed"},
	} {
		t.Run(name, func(t *testing.T) {
			server, client := newTestClientWithServer(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"headers":[%q],"details":[]}`, tc.header)
			})
			defer server.Close()
			var logs bytes.Buffer
			client.log = debugLogger(&logs)

			if _, err := client.FetchActivity(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertWithheld(t, "logs", logs.String(), fieldSentinel, overflowSentinel)
			assertKept(t, "logs", logs.String(), tc.want)
		})
	}
}

func TestDHCPPoolParseLogsWithholdTheEntry(t *testing.T) {
	// The range separator is a hyphen, so the no-separator case needs a
	// sentinel without one.
	unhyphenated := strings.ReplaceAll(fieldSentinel, "-", "")
	for name, tc := range map[string]struct {
		entry string
		want  string
	}{
		"cidr":     {entry: fieldSentinel + "/99", want: "unparseable CIDR pool entry"},
		"no range": {entry: unhyphenated, want: "unparseable pool entry (no range separator)"},
		"range":    {entry: fieldSentinel + " - " + fieldSentinel, want: "unparseable or inverted pool range"},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			c := &Client{log: debugLogger(&logs)}
			if got := c.poolSpecSize(tc.entry); got != 0 {
				t.Fatalf("poolSpecSize = %v, want 0", got)
			}
			assertWithheld(t, "logs", logs.String(), fieldSentinel, unhyphenated)
			assertKept(t, "logs", logs.String(), tc.want)
		})
	}
}

func TestKeaPDPoolLengthsWithheldFromLog(t *testing.T) {
	var logs bytes.Buffer
	c := &Client{log: debugLogger(&logs)}
	if got := c.pdPoolCapacity(4321, 8765); got != 0 {
		t.Fatalf("pdPoolCapacity = %v, want 0", got)
	}
	assertWithheld(t, "logs", logs.String(), "4321", "8765")
	assertKept(t, "logs", logs.String(), "kea: invalid pd pool prefix lengths")
}

func TestFloatParseLogWithholdsTheValue(t *testing.T) {
	for name, tc := range map[string]*regexp.Regexp{
		"pattern mismatch": regexp.MustCompile(`^\d+$`),
		"parse failure":    regexp.MustCompile(`.+`),
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			if got := parseStringToFloatWithReplace(fieldSentinel, tc, "%", "loss", debugLogger(&logs)); got != -1.0 {
				t.Fatalf("got %v, want -1", got)
			}
			assertWithheld(t, "logs", logs.String(), fieldSentinel)
			assertKept(t, "logs", logs.String(), "loss")
		})
	}
}

func TestTailscaleParseLogsWithholdTheValues(t *testing.T) {
	server, mux, client := newTestClientWithMux(t)
	defer server.Close()
	mux.HandleFunc("/api/tailscale/status/status", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"Version":"1","BackendState":"Running","AuthURL":"",
			"Self":{"HostName":"fw","KeyExpiry":%q},
			"Peer":{"k1":{"HostName":%q},"k2":{"HostName":%q}}}`, fieldSentinel, fieldSentinel, fieldSentinel)
	})
	var logs bytes.Buffer
	client.log = debugLogger(&logs)

	if _, err := client.FetchTailscaleStatus(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertWithheld(t, "logs", logs.String(), fieldSentinel)
	assertKept(t, "logs", logs.String(), "Self.KeyExpiry", "duplicate name")
}

// The warnings for a failed or retried request name the registered route. A
// parameterised segment, such as the interface identifiers of the traffic-top
// route, stays out of them, and out of the debug line that precedes them.
func TestRetryAndTransportWarningsLogTheBaseRoute(t *testing.T) {
	for name, serve := range map[string]func(t *testing.T) string{
		"retryable status": func(t *testing.T) string {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)
			return strings.TrimPrefix(server.URL, "http://")
		},
		"transport failure": func(t *testing.T) string {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			addr := ln.Addr().String()
			_ = ln.Close()
			return addr
		},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			client, err := NewClient(options.OPNSenseConfig{
				Protocol: "http", Host: serve(t), APIKey: "test-key", APISecret: "test-secret",
				Timeout: 2 * time.Second, MaxRetries: 2,
			}, "test", debugLogger(&logs))
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			base := string(client.endpoints["trafficTop"])

			_, apiErr := client.FetchTrafficTop([]string{fieldSentinel + "-A", fieldSentinel + "-B"})
			if apiErr == nil {
				t.Fatal("expected the request to fail")
			}
			assertWithheld(t, "logs", logs.String(), fieldSentinel)
			assertWithheld(t, "Error()", apiErr.Error(), fieldSentinel)
			assertKept(t, "logs", logs.String(), "endpoint="+base, "attempt=1")
			if apiErr.Endpoint != base {
				t.Errorf("Endpoint = %q, want the base route %q", apiErr.Endpoint, base)
			}
		})
	}
}

func TestRouteForLog(t *testing.T) {
	c := &Client{endpoints: defaultEndpoints()}
	base := string(c.endpoints["trafficTop"])
	vnstat := string(c.endpoints["vnstatGetJsonData"])
	for name, tc := range map[string]struct{ path, want string }{
		"exact":              {base, base},
		"path segment":       {base + "/" + fieldSentinel, base},
		"comma list":         {base + "/a,b", base},
		"query":              {vnstat + "?interface=" + fieldSentinel, vnstat},
		"not a segment edge": {base + fieldSentinel, base + fieldSentinel},
		"unregistered":       {"api/unknown/route?x=" + fieldSentinel, "api/unknown/route"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := c.routeForLog(EndpointPath(tc.path)); got != tc.want {
				t.Errorf("routeForLog(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

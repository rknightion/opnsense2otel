package annotations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const grafanaBodySentinel = "SYNTHETIC-GRAFANA-BODY-SENTINEL"

// A rejected write reports the status and nothing of the response body, which
// can quote the annotation text, a token or a dashboard name. The retry hint
// and the permanent-or-transient classification are unchanged.
func TestWriteErrorWithholdsTheResponseBody(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"` + grafanaBodySentinel + `"}`))
			}))
			t.Cleanup(origin.Close)

			err := clientFor(origin).post(context.Background(), Event{Kind: "test", Text: "text"})
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if strings.Contains(err.Error(), grafanaBodySentinel) {
				t.Errorf("error holds the response body: %s", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Errorf("error lost the status %d: %s", status, err)
			}
			var httpErr *httpError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected an *httpError, got %T", err)
			}
			if httpErr.StatusCode != status {
				t.Errorf("StatusCode = %d, want %d", httpErr.StatusCode, status)
			}
			if httpErr.RetryAfter != 7*time.Second {
				t.Errorf("RetryAfter = %s, want 7s", httpErr.RetryAfter)
			}
			wantPermanent := status == http.StatusBadRequest || status == http.StatusForbidden
			if httpErr.permanent() != wantPermanent {
				t.Errorf("permanent() = %v, want %v", httpErr.permanent(), wantPermanent)
			}
		})
	}
}

// The list read decodes Grafana's reply; a number the decoder rejects is quoted
// in encoding/json's own error text.
func TestExistingDecodeFailureWithholdsTheResponseBody(t *testing.T) {
	const literal = "99999999999999999999999"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"time":` + literal + `,"tags":["` + grafanaBodySentinel + `"]}]`))
	}))
	t.Cleanup(origin.Close)

	_, err := clientFor(origin).existing(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected a decode failure")
	}
	for _, sentinel := range []string{literal, grafanaBodySentinel} {
		if strings.Contains(err.Error(), sentinel) {
			t.Errorf("error holds the response body %q: %s", sentinel, err)
		}
	}
	if !strings.Contains(err.Error(), "decode annotation list") {
		t.Errorf("error lost what failed: %s", err)
	}
}

// nonHTTPReplies are what a configured URL can answer with when it is not
// Grafana. net/http quotes both in its own error text.
var nonHTTPReplies = map[string]string{
	"status line": "PROBE-SENTINEL-REPLY-LINE\r\n\r\n",
	"header":      "HTTP/1.1 200 OK\r\nPROBE-SENTINEL-HEADER-NO-COLON\r\n\r\n",
}

// serveNonHTTP answers every connection with raw bytes and closes it.
func serveNonHTTP(t *testing.T, raw string) *httptest.Server {
	t.Helper()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = conn.Write([]byte(raw))
		_ = conn.Close()
	}))
	t.Cleanup(origin.Close)
	return origin
}

func assertTransportWithheld(t *testing.T, surface string, err error, origin *httptest.Server) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected a transport failure", surface)
	}
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		t.Errorf("%s: a reply that is not HTTP must not look like an HTTP status: %v", surface, err)
	}
	host := strings.TrimPrefix(origin.URL, "http://")
	for _, withheld := range []string{"PROBE-SENTINEL", host} {
		if strings.Contains(err.Error(), withheld) {
			t.Errorf("%s: error holds %q: %s", surface, withheld, err)
		}
	}
	if !strings.Contains(err.Error(), "grafana request failed") {
		t.Errorf("%s: error lost what failed: %s", surface, err)
	}
}

// net/http quotes a malformed status line or header in its own error, and names
// the request URL. Neither reaches the error the watcher logs.
func TestTransportErrorWithholdsTheUpstreamReply(t *testing.T) {
	for name, raw := range nonHTTPReplies {
		t.Run(name, func(t *testing.T) {
			origin := serveNonHTTP(t, raw)
			c := clientFor(origin)
			assertTransportWithheld(t, "post", c.post(context.Background(), Event{Kind: "test", Text: "text"}), origin)
			_, err := c.existing(context.Background(), time.Now())
			assertTransportWithheld(t, "existing", err, origin)
		})
	}
}

// A transport failure is not a verdict on the body, so the watcher must keep
// the event unseen and send it again once Grafana answers, and must log nothing
// the server sent.
func TestWatcherRetriesATransportFailureWithoutLoggingTheReply(t *testing.T) {
	for name, raw := range nonHTTPReplies {
		t.Run(name, func(t *testing.T) {
			f := newFakeGrafana(t)
			now := time.Now()
			changed := now.Add(-90 * time.Minute).Truncate(time.Second)
			w := newTestWatcher(t, f, now, series{
				name:   "opnsense_system_config_last_change_timestamp_seconds",
				labels: map[string]string{instanceLabel: "fw-a"},
				value:  float64(changed.Unix()),
			})
			var logs bytes.Buffer
			w.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			healthy := w.client

			// The startup read goes through the broken transport first: it is
			// logged without the reply and the watcher carries on.
			w.client = clientFor(serveNonHTTP(t, raw))
			w.reconcile(context.Background())
			if !strings.Contains(logs.String(), "annotation reconciliation failed") {
				t.Errorf("the failed reconciliation was not logged: %s", logs.String())
			}
			if strings.Contains(logs.String(), "PROBE-SENTINEL") {
				t.Errorf("reconciliation log holds the upstream reply: %s", logs.String())
			}

			// Then a healthy read seeds the baseline, and the post fails.
			w.client = healthy
			w.reconcile(context.Background())
			w.client = clientFor(serveNonHTTP(t, raw))
			w.tick(context.Background())
			if got := len(f.writes()); got != 0 {
				t.Fatalf("wrote %d annotations through a broken transport", got)
			}
			if !strings.Contains(logs.String(), "annotation post failed") {
				t.Errorf("the failed post was not logged: %s", logs.String())
			}

			w.client = healthy
			w.tick(context.Background())
			if got := len(f.writes()); got != 1 {
				t.Fatalf("the event was not retried once Grafana answered: %d writes", got)
			}
			if strings.Contains(logs.String(), "PROBE-SENTINEL") {
				t.Errorf("log holds the upstream reply: %s", logs.String())
			}
		})
	}
}

func TestTransportErrorKeepsCancellationAndDeadlineRecognisable(t *testing.T) {
	release := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(origin.Close)
	t.Cleanup(func() { close(release) })
	c := clientFor(origin)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.post(canceled, Event{Kind: "test", Text: "text"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("post after cancel: errors.Is(Canceled) = false for %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a cancellation reads as a deadline: %v", err)
	}

	expired, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	_, err = c.existing(expired, time.Now())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("existing past its deadline: errors.Is(DeadlineExceeded) = false for %v", err)
	}
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		t.Errorf("a deadline must not look like an HTTP status: %v", err)
	}
}

// A refused connection names the host and port in net/http's text; the fixed
// reason does not.
func TestTransportErrorForARefusedConnectionNamesNoAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	c := newClient(Config{URL: "http://" + addr, Token: "test-token"})
	got := c.post(context.Background(), Event{Kind: "test", Text: "text"})
	if got == nil {
		t.Fatal("expected a connection failure")
	}
	if strings.Contains(got.Error(), addr) {
		t.Errorf("error names the configured address: %s", got)
	}
}

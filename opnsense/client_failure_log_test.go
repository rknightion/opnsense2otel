package opnsense

import (
	"bytes"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/options"
)

const failureLogMarker = "SYNTHETIC-VOUCHER-MARKER"

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureStandardLog must run before NewClient, as a real process's output is
// in place before its clients are built.
func captureStandardLog(t *testing.T) *lockedLogBuffer {
	t.Helper()
	captured := &lockedLogBuffer{}
	previous := log.Writer()
	log.SetOutput(captured)
	t.Cleanup(func() { log.SetOutput(previous) })
	return captured
}

func newFailureLogClient(t *testing.T, protocol, host string, logs io.Writer) *Client {
	t.Helper()
	client, err := NewClient(options.OPNSenseConfig{
		Protocol:   protocol,
		Host:       host,
		APIKey:     "test-key",
		APISecret:  "test-secret",
		Insecure:   protocol == "https",
		Timeout:    2 * time.Second,
		MaxRetries: 1,
	}, "test", slog.New(slog.NewTextHandler(logs, nil)))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return &client
}

// serveRawReply answers every connection with reply, whatever was requested.
func serveRawReply(t *testing.T, reply string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 4096)
				_, _ = conn.Read(buf)
				_, _ = io.WriteString(conn, reply)
			}()
		}
	}()
	return ln.Addr().String()
}

func TestClientFailureLogsOmitNonHTTPReply(t *testing.T) {
	const endpoint = "api/test/failure"
	for name, reply := range map[string]string{
		"status line": failureLogMarker + " is not http\r\n\r\n",
		"status code": "HTTP/1.1 " + failureLogMarker + " nope\r\n\r\n",
		"header line": "HTTP/1.1 200 OK\r\n" + failureLogMarker + "\r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			standard := captureStandardLog(t)
			var clientLogs lockedLogBuffer
			client := newFailureLogClient(t, "http", serveRawReply(t, reply), &clientLogs)

			var out map[string]any
			apiErr := client.do(http.MethodGet, endpoint, nil, &out)
			if apiErr == nil {
				t.Fatal("expected a non-HTTP reply to fail")
			}
			for surface, text := range map[string]string{
				"client log":     clientLogs.String(),
				"standard log":   standard.String(),
				"returned error": apiErr.Error(),
			} {
				if strings.Contains(text, failureLogMarker) {
					t.Errorf("%s holds upstream content: %s", surface, text)
				}
			}
			logged := clientLogs.String()
			if !strings.Contains(logged, "endpoint="+endpoint) || !strings.Contains(logged, "code=0") {
				t.Errorf("failure log must name endpoint and status, got: %s", logged)
			}
			if apiErr.Endpoint != endpoint || apiErr.StatusCode != 0 {
				t.Errorf("returned error lost endpoint or status: %#v", apiErr)
			}
		})
	}
}

// The transport logs bytes that arrive on an idle connection straight to the
// standard logger, outside any error this client handles.
func TestClientFailureLogsOmitUnsolicitedIdleBytes(t *testing.T) {
	standard := captureStandardLog(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	replied := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}")
		<-replied
		// Let the transport return the connection to its idle pool first.
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(conn, failureLogMarker)
		_, _ = conn.Read(buf)
	}()

	var clientLogs lockedLogBuffer
	client := newFailureLogClient(t, "http", ln.Addr().String(), &clientLogs)
	var out map[string]any
	if apiErr := client.do(http.MethodGet, "api/test/idle", nil, &out); apiErr != nil {
		t.Fatalf("first request: %v", apiErr)
	}
	close(replied)

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(standard.String(), "Unsolicited response") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := standard.String()
	if !strings.Contains(got, "Unsolicited response") {
		t.Fatalf("transport never logged the idle bytes, so this run proves nothing: %q", got)
	}
	if strings.Contains(got, failureLogMarker) {
		t.Errorf("standard log holds upstream content: %s", got)
	}
}

// Withholding content must not cost the diagnosis of a wrong protocol setting.
func TestClientFailureKeepsPlainHTTPDiagnosis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	var clientLogs lockedLogBuffer
	client := newFailureLogClient(t, "https", strings.TrimPrefix(server.URL, "http://"), &clientLogs)
	var out map[string]any
	apiErr := client.do(http.MethodGet, "api/test/plain", nil, &out)
	if apiErr == nil || !strings.Contains(apiErr.Message, "HTTP response to HTTPS client") {
		t.Fatalf("expected the protocol mismatch to stay visible, got %#v", apiErr)
	}
}

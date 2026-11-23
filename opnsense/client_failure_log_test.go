package opnsense

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/internal/cpustream"
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
		"status line": failureLogMarker + "\r\n\r\n",
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

// The CPU stream dials outside do(), and internal/cpustream logs whatever error
// the opener returns.
func TestCPUStreamFailureLogOmitsNonHTTPReply(t *testing.T) {
	for name, reply := range map[string]string{
		"status line": failureLogMarker + "\r\n\r\n",
		"status code": "HTTP/1.1 " + failureLogMarker + " nope\r\n\r\n",
		"header line": "HTTP/1.1 200 OK\r\n" + failureLogMarker + "\r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			var clientLogs, streamLogs lockedLogBuffer
			client := newFailureLogClient(t, "http", serveRawReply(t, reply), &clientLogs)
			stream := cpustream.New(client.OpenCPUStream, cpustream.Config{
				Logger:     slog.New(slog.NewTextHandler(&streamLogs, nil)),
				MinBackoff: 20 * time.Millisecond,
				MaxBackoff: 40 * time.Millisecond,
			})
			stream.Start(context.Background())
			deadline := time.Now().Add(3 * time.Second)
			for !strings.Contains(streamLogs.String(), "disconnected") && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			stream.Close()

			got := streamLogs.String()
			if !strings.Contains(got, "disconnected") {
				t.Fatalf("stream never logged the failure, so this run proves nothing: %q", got)
			}
			if strings.Contains(got, failureLogMarker) {
				t.Errorf("stream log holds upstream content: %s", got)
			}
		})
	}
}

// Certificate error text quotes names the peer chose, so the reason is built
// from the error's type alone.
func TestClientFailureOmitsPeerCertificateText(t *testing.T) {
	markerName := strings.ToLower(failureLogMarker) + ".example"
	now := time.Now()
	for name, tc := range map[string]struct {
		dnsNames []string
		notAfter time.Time
		trusted  bool
		want     string
	}{
		"hostname mismatch": {[]string{markerName}, now.Add(time.Hour), true, "TLS certificate does not match"},
		"unknown authority": {[]string{"localhost", markerName}, now.Add(time.Hour), false, "TLS certificate is signed by an unknown authority"},
		"expired":           {[]string{"localhost", markerName}, now.Add(-time.Minute), true, "TLS certificate has expired"},
	} {
		t.Run(name, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatalf("generate key: %v", err)
			}
			template := &x509.Certificate{
				SerialNumber:          big.NewInt(1),
				Subject:               pkix.Name{CommonName: failureLogMarker},
				NotBefore:             now.Add(-time.Hour),
				NotAfter:              tc.notAfter,
				DNSNames:              tc.dnsNames,
				IsCA:                  true,
				BasicConstraintsValid: true,
				KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
			}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if err != nil {
				t.Fatalf("create certificate: %v", err)
			}
			leaf, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatalf("parse certificate: %v", err)
			}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
			server.StartTLS()
			t.Cleanup(server.Close)
			_, port, err := net.SplitHostPort(server.Listener.Addr().String())
			if err != nil {
				t.Fatalf("split address: %v", err)
			}

			var clientLogs lockedLogBuffer
			client := newFailureLogClient(t, "https", "localhost:"+port, &clientLogs)
			roots := x509.NewCertPool()
			if tc.trusted {
				roots.AddCert(leaf)
			} else {
				// A root with the same subject but another key makes the
				// verifier quote this candidate's name in its error.
				otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatalf("generate key: %v", err)
				}
				otherDER, err := x509.CreateCertificate(rand.Reader, template, template, &otherKey.PublicKey, otherKey)
				if err != nil {
					t.Fatalf("create certificate: %v", err)
				}
				other, err := x509.ParseCertificate(otherDER)
				if err != nil {
					t.Fatalf("parse certificate: %v", err)
				}
				roots.AddCert(other)
			}
			transport := client.httpClient.Transport.(*http.Transport)
			transport.TLSClientConfig.InsecureSkipVerify = false
			transport.TLSClientConfig.RootCAs = roots

			var out map[string]any
			apiErr := client.do(http.MethodGet, "api/test/cert", nil, &out)
			if apiErr == nil {
				t.Fatal("expected certificate verification to fail")
			}
			for surface, text := range map[string]string{
				"client log":     clientLogs.String(),
				"returned error": apiErr.Error(),
			} {
				if strings.Contains(strings.ToLower(text), strings.ToLower(failureLogMarker)) {
					t.Errorf("%s holds peer certificate text: %s", surface, text)
				}
			}
			if !strings.Contains(clientLogs.String(), tc.want) {
				t.Errorf("failure log must keep the reason %q, got: %s", tc.want, clientLogs.String())
			}
		})
	}
}

// A well-formed HTTP reply with a broken gzip body is not an HTTP failure.
func TestClientFailureNamesCorruptGzip(t *testing.T) {
	var valid bytes.Buffer
	writer := gzip.NewWriter(&valid)
	if _, err := writer.Write([]byte(`{"ok":true}`)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	badChecksum := bytes.Clone(valid.Bytes())
	badChecksum[len(badChecksum)-5] ^= 0xff
	// A valid ten-byte header followed by the reserved deflate block type.
	badDeflate := append(bytes.Clone(valid.Bytes()[:10]), 0xff, 0xff, 0xff, 0xff)

	for name, body := range map[string][]byte{
		"header":   []byte(failureLogMarker + failureLogMarker),
		"deflate":  badDeflate,
		"checksum": badChecksum,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = w.Write(body)
			}))
			t.Cleanup(server.Close)

			var clientLogs lockedLogBuffer
			client := newFailureLogClient(t, "http", strings.TrimPrefix(server.URL, "http://"), &clientLogs)
			var out map[string]any
			apiErr := client.do(http.MethodGet, "api/test/gzip", nil, &out)
			if apiErr == nil {
				t.Fatal("expected a corrupt gzip body to fail")
			}
			if !strings.Contains(apiErr.Message, "corrupt gzip data") {
				t.Errorf("reason must name the corrupt gzip body, got: %q", apiErr.Message)
			}
			if apiErr.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", apiErr.StatusCode)
			}
			if strings.Contains(apiErr.Error()+clientLogs.String(), failureLogMarker) {
				t.Errorf("failure holds upstream content: %s %s", apiErr.Error(), clientLogs.String())
			}
		})
	}
}

// watchCPUStreamFailure runs the real stream loop until it logs a disconnect and
// returns everything it logged.
func watchCPUStreamFailure(t *testing.T, protocol, addr string) string {
	t.Helper()
	var clientLogs, streamLogs lockedLogBuffer
	client := newFailureLogClient(t, protocol, addr, &clientLogs)
	stream := cpustream.New(client.OpenCPUStream, cpustream.Config{
		Logger:     slog.New(slog.NewTextHandler(&streamLogs, nil)),
		MinBackoff: 20 * time.Millisecond,
		MaxBackoff: 40 * time.Millisecond,
	})
	stream.Start(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(streamLogs.String(), "disconnected") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	stream.Close()
	got := streamLogs.String()
	if !strings.Contains(got, "disconnected") {
		t.Fatalf("stream never logged the failure, so this run proves nothing: %q", got)
	}
	return got
}

func writeHTTP2Frame(w io.Writer, frameType, flags byte, streamID uint32, payload []byte) error {
	frame := make([]byte, 9, 9+len(payload))
	frame[0], frame[1], frame[2] = byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
	frame[3], frame[4] = frameType, flags
	binary.BigEndian.PutUint32(frame[5:], streamID)
	_, err := w.Write(append(frame, payload...))
	return err
}

// serveHTTP2GoAway answers a request with 200 and one SSE frame, then sends
// GOAWAY carrying the marker as debug data and closes the connection.
func serveHTTP2GoAway(t *testing.T, code uint32) string {
	t.Helper()
	const (
		frameData      = 0
		frameHeaders   = 1
		frameSettings  = 4
		frameGoAway    = 7
		flagAck        = 1
		flagEndHeader  = 4
		hpackStatus200 = 0x88
	)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		NextProtos:   []string{"h2"},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	serve := func(conn net.Conn) {
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
		if _, err := io.ReadFull(conn, make([]byte, 24)); err != nil {
			return
		}
		_ = writeHTTP2Frame(conn, frameSettings, 0, 0, nil)
		for {
			header := make([]byte, 9)
			if _, err := io.ReadFull(conn, header); err != nil {
				return
			}
			size := int64(header[0])<<16 | int64(header[1])<<8 | int64(header[2])
			if _, err := io.CopyN(io.Discard, conn, size); err != nil {
				return
			}
			switch header[3] {
			case frameSettings:
				if header[4]&flagAck == 0 {
					_ = writeHTTP2Frame(conn, frameSettings, flagAck, 0, nil)
				}
			case frameHeaders:
				id := binary.BigEndian.Uint32(header[5:]) & 0x7fffffff
				_ = writeHTTP2Frame(conn, frameHeaders, flagEndHeader, id, []byte{hpackStatus200})
				_ = writeHTTP2Frame(conn, frameData, 0, id, []byte("data: {}\n\n"))
				payload := make([]byte, 8, 8+len(failureLogMarker))
				binary.BigEndian.PutUint32(payload, id)
				binary.BigEndian.PutUint32(payload[4:], code)
				_ = writeHTTP2Frame(conn, frameGoAway, 0, 0, append(payload, failureLogMarker...))
				time.Sleep(100 * time.Millisecond)
				return
			}
		}
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(conn)
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split address: %v", err)
	}
	return "localhost:" + port
}

// An error from an already-open body reaches the same log as a dial error, and
// net/http quotes received bytes in it.
func TestCPUStreamFailureLogOmitsBodyReadError(t *testing.T) {
	const head = "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n"
	const chunk = "a\r\ndata: {}\n\n\r\n"
	for name, open := range map[string]func(*testing.T) (string, string){
		"chunked trailer": func(t *testing.T) (string, string) {
			return "http", serveRawReply(t, head+chunk+"0\r\n"+failureLogMarker+"\r\n\r\n")
		},
		"goaway without error": func(t *testing.T) (string, string) { return "https", serveHTTP2GoAway(t, 0) },
		"goaway with error":    func(t *testing.T) (string, string) { return "https", serveHTTP2GoAway(t, 2) },
	} {
		t.Run(name, func(t *testing.T) {
			protocol, addr := open(t)
			got := watchCPUStreamFailure(t, protocol, addr)
			if strings.Contains(got, failureLogMarker) {
				t.Errorf("stream log holds upstream content: %s", got)
			}
			if !strings.Contains(got, "cpu usage stream read failed") {
				t.Errorf("failure did not come from the open body: %s", got)
			}
		})
	}
}

type failingReadCloser struct {
	n   int
	err error
}

func (r failingReadCloser) Read([]byte) (int, error) { return r.n, r.err }
func (failingReadCloser) Close() error               { return nil }

func TestStreamBodyReadKeepsErrorClass(t *testing.T) {
	quoted := func(err error) error { return fmt.Errorf("%s: %w", failureLogMarker, err) }
	for name, tc := range map[string]struct {
		err     error
		is      error
		wantEOF bool
	}{
		"clean end":      {err: io.EOF, wantEOF: true},
		"wrapped end":    {err: quoted(io.EOF), wantEOF: true},
		"canceled":       {err: quoted(context.Canceled), is: context.Canceled},
		"deadline":       {err: quoted(context.DeadlineExceeded), is: context.DeadlineExceeded},
		"unexpected end": {err: quoted(io.ErrUnexpectedEOF), is: io.ErrUnexpectedEOF},
		"unclassified":   {err: errors.New(failureLogMarker)},
	} {
		t.Run(name, func(t *testing.T) {
			body := &streamBody{ReadCloser: failingReadCloser{n: 3, err: tc.err}, cancel: func() {}}
			n, err := body.Read(make([]byte, 8))
			if n != 3 {
				t.Errorf("n = %d, want the 3 bytes read alongside the error", n)
			}
			if tc.wantEOF {
				if err != io.EOF {
					t.Fatalf("err = %v, want bare io.EOF", err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), failureLogMarker) {
				t.Fatalf("err must exist without upstream text, got: %v", err)
			}
			if errors.Is(err, io.EOF) {
				t.Errorf("a failed read must not look like a clean end: %v", err)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("err = %v, want errors.Is %v", err, tc.is)
			}
		})
	}
}

// The real transport must still report cancellation and deadlines as such.
func TestOpenCPUStreamReadKeepsContextErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "data: {}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(server.Close)

	for name, tc := range map[string]struct {
		open func() (context.Context, context.CancelFunc)
		stop bool
		want error
	}{
		"canceled": {func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, true, context.Canceled},
		"deadline": {func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 150*time.Millisecond)
		}, false, context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			var logs lockedLogBuffer
			client := newFailureLogClient(t, "http", strings.TrimPrefix(server.URL, "http://"), &logs)
			ctx, cancel := tc.open()
			defer cancel()
			body, err := client.OpenCPUStream(ctx)
			if err != nil {
				t.Fatalf("OpenCPUStream: %v", err)
			}
			defer body.Close()
			if _, err := body.Read(make([]byte, 64)); err != nil {
				t.Fatalf("first read: %v", err)
			}
			if tc.stop {
				cancel()
			}
			_, err = io.Copy(io.Discard, body)
			if !errors.Is(err, tc.want) {
				t.Fatalf("read error = %v, want errors.Is %v", err, tc.want)
			}
		})
	}
}

// A finished exchange with an incomplete gzip body is a bad body; a connection
// lost part way through is still reported as a lost connection.
func TestClientFailureSeparatesIncompleteGzipFromLostConnection(t *testing.T) {
	var valid bytes.Buffer
	writer := gzip.NewWriter(&valid)
	if _, err := writer.Write([]byte(strings.Repeat("{\"ok\":true}", 20))); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	for name, body := range map[string][]byte{
		"empty":       {},
		"part header": valid.Bytes()[:6],
		"header only": valid.Bytes()[:10],
		"part data":   valid.Bytes()[:12],
	} {
		t.Run("complete reply/"+name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				_, _ = w.Write(body)
			}))
			t.Cleanup(server.Close)
			var logs lockedLogBuffer
			client := newFailureLogClient(t, "http", strings.TrimPrefix(server.URL, "http://"), &logs)
			var out map[string]any
			apiErr := client.do(http.MethodGet, "api/test/gzip", nil, &out)
			if apiErr == nil {
				t.Fatal("expected an incomplete gzip body to fail")
			}
			if !strings.Contains(apiErr.Message, "incomplete gzip data") {
				t.Errorf("reason = %q, want incomplete gzip data", apiErr.Message)
			}
			if apiErr.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", apiErr.StatusCode)
			}
		})
		if len(body) == 0 {
			continue
		}
		t.Run("lost connection/"+name, func(t *testing.T) {
			reply := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: %d\r\n\r\n%s", valid.Len(), body)
			var logs lockedLogBuffer
			client := newFailureLogClient(t, "http", serveRawReply(t, reply), &logs)
			var out map[string]any
			apiErr := client.do(http.MethodGet, "api/test/gzip", nil, &out)
			if apiErr == nil {
				t.Fatal("expected a cut connection to fail")
			}
			if !strings.Contains(apiErr.Message, "upstream closed the connection") {
				t.Errorf("reason = %q, want the connection loss", apiErr.Message)
			}
		})
	}
}

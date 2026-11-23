package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"go/ast"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/exporter-toolkit/web"
	"github.com/rknightion/opnsense2otel/v5/internal/logship"
)

// writeSelfSignedTLSConfig writes a throwaway certificate and a web config file
// that enables TLS with it, and returns the config file path.
func writeSelfSignedTLSConfig(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "web.yml")
	cfg := "tls_server_config:\n  cert_file: " + certPath + "\n  key_file: " + keyPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// lockedBuffer is a bytes.Buffer safe for the server goroutine to write while the
// test reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestMetricsServerTLSHandshakeFailureStaysOutOfShippingQueue is the regression for
// an unauthenticated peer evicting firewall records: exporter-toolkit sends
// net/http's server errors to the application logger when http.Server.ErrorLog is
// nil, and with --logs.self.enabled that logger feeds the bounded log queue. A
// plaintext request to a TLS metrics port must reach stderr only.
func TestMetricsServerTLSHandshakeFailureStaysOutOfShippingQueue(t *testing.T) {
	stderr := &lockedBuffer{}
	baseLogger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	selfLog := logship.NewSelfLogHandler(baseLogger.Handler())
	var mu sync.Mutex
	var queued []logship.Entry
	selfLog.Bind(func(e logship.Entry) bool {
		mu.Lock()
		defer mu.Unlock()
		// web.Serve announces "Listening on" at info; only server errors
		// (warn) are the subject here.
		if e.Record.Severity != logship.SeverityWarn {
			return true
		}
		queued = append(queued, e)
		return true
	})
	// The application logger exactly as main wires it under --logs.self.enabled.
	appLogger := slog.New(selfLog)

	srv := newMetricsServer(http.NotFoundHandler(), baseLogger)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := writeSelfSignedTLSConfig(t)
	addrs := []string{l.Addr().String()}
	systemd := false
	flags := &web.FlagConfig{WebListenAddresses: &addrs, WebSystemdSocket: &systemd, WebConfigFile: &cfgPath}
	done := make(chan error, 1)
	go func() { done <- web.Serve(l, srv, flags, appLogger) }()
	t.Cleanup(func() {
		_ = srv.Close()
		<-done
	})

	// Plaintext HTTP to the TLS port: the server logs a failed handshake.
	for i := 0; i < 3; i++ {
		c, err := net.DialTimeout("tcp", l.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.Write([]byte("GET /metrics HTTP/1.1\r\nHost: x\r\n\r\n"))
		buf := make([]byte, 256)
		_, _ = c.Read(buf)
		_ = c.Close()
	}

	// The handshake error is logged after the peer closes; wait for it to land
	// on stderr, which proves the error path ran before asserting on the queue.
	deadline := time.Now().Add(5 * time.Second)
	for stderr.String() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if stderr.String() == "" {
		t.Fatal("the failed handshakes were not logged to stderr at all; the test is not exercising the server error path")
	}
	time.Sleep(100 * time.Millisecond) // let any straggling record settle

	mu.Lock()
	defer mu.Unlock()
	if len(queued) != 0 {
		t.Fatalf("%d failed-handshake records entered the shipping queue; want 0 (stderr only). first: %+v", len(queued), queued[0])
	}
}

// TestMainBuildsMetricsServerFromBaseLogger guards the wiring that the seam test
// cannot reach, because main() calls os.Exit: the metrics server must be built
// with baseLogger (stderr only), never logger (which is the self-log wrapper when
// --logs.self.enabled is set).
func TestMainBuildsMetricsServerFromBaseLogger(t *testing.T) {
	found := false
	ast.Inspect(mainFuncBody(t), func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "newMetricsServer" {
			return true
		}
		found = true
		if len(call.Args) != 2 {
			t.Errorf("newMetricsServer called with %d args, want 2", len(call.Args))
			return true
		}
		if id, ok := call.Args[1].(*ast.Ident); !ok || id.Name != "baseLogger" {
			t.Errorf("newMetricsServer must be given baseLogger (stderr only), not %T %v", call.Args[1], call.Args[1])
		}
		return true
	})
	if !found {
		t.Fatal("main() does not build the metrics server through newMetricsServer")
	}
}

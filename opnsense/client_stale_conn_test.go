package opnsense

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/promslog"
	"github.com/rknightion/opnsense2otel/v5/internal/options"
)

// silentDropProxy forwards TCP to target until drop is called. From then on it
// swallows every byte on the connections it already holds, in both directions,
// without closing them, while new connections still forward normally. That is
// what a rebooted firewall looks like to a pooled connection: pf has no state for
// it and silently drops its packets, so the client sees neither data nor a reset.
type silentDropProxy struct {
	ln     net.Listener
	target string

	mu      sync.Mutex
	dropGen int
	conns   []net.Conn
}

func newSilentDropProxy(t *testing.T, target string) *silentDropProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := &silentDropProxy{ln: ln, target: target}
	go p.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, c := range p.conns {
			_ = c.Close()
		}
	})
	return p
}

func (p *silentDropProxy) serve() {
	for {
		down, err := p.ln.Accept()
		if err != nil {
			return
		}
		up, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = down.Close()
			continue
		}
		p.mu.Lock()
		gen := p.dropGen
		p.conns = append(p.conns, down, up)
		p.mu.Unlock()
		go p.pipe(up, down, gen)
		go p.pipe(down, up, gen)
	}
}

func (p *silentDropProxy) pipe(dst, src net.Conn, gen int) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		p.mu.Lock()
		dropped := p.dropGen != gen
		p.mu.Unlock()
		if dropped {
			continue
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (p *silentDropProxy) drop() {
	p.mu.Lock()
	p.dropGen++
	p.mu.Unlock()
}

// A pooled HTTP/2 connection to a firewall that has since rebooted must not keep
// swallowing requests: one API call, within its normal retry budget, has to get
// through on a fresh connection. The server advertises more concurrent streams
// than the call has attempts, so reusing the dead connection until its streams
// run out cannot pass.
func TestClient_RecoversFromSilentlyDeadHTTP2Connection(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			http.Error(w, "want HTTP/2", http.StatusHTTPVersionNotSupported)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	srv.EnableHTTP2 = true
	srv.Config.HTTP2 = &http.HTTP2Config{MaxConcurrentStreams: 100}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	proxy := newSilentDropProxy(t, srv.Listener.Addr().String())
	client, err := NewClient(options.OPNSenseConfig{
		Protocol:  "https",
		Host:      proxy.ln.Addr().String(),
		APIKey:    "k",
		APISecret: "s",
		Insecure:  true,
		Timeout:   600 * time.Millisecond,
	}, "test", promslog.NewNopLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var out map[string]any
	if apiErr := client.do("GET", "api/test/x", nil, &out); apiErr != nil {
		t.Fatalf("request before the drop: %v", apiErr)
	}

	proxy.drop()

	out = nil
	if apiErr := client.do("GET", "api/test/x", nil, &out); apiErr != nil {
		t.Fatalf("request after the connection went silent did not recover: %v", apiErr)
	}
	if out["ok"] != true {
		t.Fatalf("response after recovery = %v, want ok=true", out)
	}
}

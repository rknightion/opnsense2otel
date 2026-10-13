//go:build live

package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

// TestLiveEndpoints makes unsuccessful contract execution fail CI; the existing
// canary separately reports additive fields and optional box-state warnings.
func TestLiveEndpoints(t *testing.T) {
	failures := map[string]string{}
	t.Cleanup(func() {
		resultPath := os.Getenv("OPN2OTEL_LIVE_RESULT")
		if resultPath == "" {
			return
		}
		summary, digest := liveFailureSummary(failures)
		if err := os.WriteFile(resultPath, []byte(summary), 0600); err != nil {
			t.Error("contract result unavailable")
			return
		}
		output := os.Getenv("GITHUB_OUTPUT")
		if output == "" {
			return
		}
		f, err := os.OpenFile(output, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("contract fingerprint unavailable")
			return
		}
		_, writeErr := fmt.Fprintf(f, "fingerprint=%s\n", digest)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			t.Error("contract fingerprint unavailable")
		}
	})
	switch liveConfiguration(os.Getenv) {
	case "skip":
		t.Skip("live credentials absent")
	case "invalid":
		failures["configuration"] = "invalid"
		t.Fatal("live configuration incomplete or invalid")
	}
	schemas, err := opnsense.AllEndpointSchemas()
	if err != nil {
		failures["registry"] = "unavailable"
		t.Fatal("schema registry unavailable")
	}
	const ledger = "../../opnsense/testdata/schemas/exemptions.json"
	if _, err = os.Stat(ledger); err != nil {
		failures["ledger"] = "unavailable"
		t.Fatal("exemption ledger unavailable")
	}
	exemptions, err := loadExemptions(ledger)
	if err != nil {
		failures["ledger"] = "invalid"
		t.Fatal("exemption ledger invalid")
	}
	transport := &http.Transport{ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}} //nolint:gosec // existing self-signed lab profiles
	defer transport.CloseIdleConnections()
	p := &prober{baseURL: "https://" + os.Getenv("DEVBOX_HOST"), key: os.Getenv("OPN2OTEL_OPS_API_KEY"), secret: os.Getenv("OPN2OTEL_OPS_API_SECRET"), notes: io.Discard, client: &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	// Match the canary endpoint timeout, including slow firmware responses.
	// Fail before visiting every endpoint when the lab/authentication is unavailable.
	// The preflight alone uses a shorter bound (two attempts plus retryPause).
	p.client.Timeout = 10 * time.Second
	preflight := false
	for _, s := range schemas {
		if s.Endpoint == "systemTime" {
			preflight = true
			r := p.probeOne(s, exemptions[s.Endpoint].ForProfile(os.Getenv("CANARY_PROFILE")))
			if verdict := liveVerdict(r); verdict != "pass" {
				failures["preflight"] = verdict
				t.Fatal("core preflight failed")
			}
			break
		}
	}
	if !preflight {
		failures["preflight"] = "unavailable"
		t.Fatal("core preflight contract unavailable")
	}
	p.client.Timeout = 30 * time.Second
	for _, s := range schemas {
		t.Run(s.Endpoint, func(t *testing.T) {
			r := p.probeOne(s, exemptions[s.Endpoint].ForProfile(os.Getenv("CANARY_PROFILE")))
			switch verdict := liveVerdict(r); verdict {
			case "skip":
				t.Skip("plugin or parameter resource absent")
			case "pass":
			default:
				failures[s.Endpoint] = verdict
				t.Fatalf("contract failure class=%s", verdict)
			}
		})
	}
}

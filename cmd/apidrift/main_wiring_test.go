package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

const runMainEnv = "APIDRIFT_TEST_RUN_MAIN"

// TestMain lets a test run the real main in a child process.
func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// The fingerprint main publishes, in the report marker and in GITHUB_OUTPUT,
// must include stale ledger entries. A box answering 404 everywhere makes every
// IPsec entry stale without any fixture standing in for main's own wiring.
func TestMainPublishesStaleEntriesInItsFingerprint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "api/core/system/status") {
			_, _ = io.WriteString(w, "{}")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	report, ghOutput := filepath.Join(dir, "report.md"), filepath.Join(dir, "github-output")
	cmd := exec.Command(os.Args[0], "--base-url="+srv.URL, "--api-key=k", "--api-secret=s",
		"--generation=g", "--profile=nightly", "--out="+report)
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), runMainEnv+"=1", "GITHUB_OUTPUT="+ghOutput)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("main: %v\n%s", err, out)
	}

	withCommittedLedgers(t)
	withTestProfile(t, opnsense.ProbeProfileNightly)
	schemas, err := opnsense.AllEndpointSchemas()
	if err != nil {
		t.Fatal(err)
	}
	exemptions, err := loadExemptions("../../opnsense/testdata/schemas/exemptions.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, ex := range exemptions {
		exemptions[name] = ex.ForProfile(opnsense.ProbeProfileNightly)
	}
	p := &prober{client: srv.Client(), baseURL: srv.URL, key: "k", secret: "s", notes: io.Discard, pause: func(time.Duration) {}}
	results := p.probeAll(schemas, exemptions)
	run := analyzeRun(results)
	if len(run.Stale) == 0 {
		t.Fatal("the fixture must produce stale entries")
	}
	want := findingsFingerprintWithReview("g", results, run.Coverage.RequiredUnresolved, run.Stale)
	if without := findingsFingerprintWithReview("g", results, run.Coverage.RequiredUnresolved, nil); want == without {
		t.Fatal("the stale entries must change the digest for this test to mean anything")
	}

	written, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseFingerprintMarker(string(written)); got != want {
		t.Errorf("report marker = %q, want %q", got, want)
	}
	outputs, err := os.ReadFile(ghOutput)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(outputs), "fingerprint="+want+"\n") {
		t.Errorf("GITHUB_OUTPUT does not carry fingerprint %s:\n%s", want, outputs)
	}
}

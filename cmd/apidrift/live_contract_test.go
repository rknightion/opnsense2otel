package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rknightion/opnsense2otel/v5/opnsense"
)

func liveVerdict(r probeResult) string {
	if r.Absent {
		if pluginGatedSet()[r.Endpoint] {
			return "skip"
		}
		return "core-absent"
	}
	if r.ProbeErr != "" {
		if r.Status != 0 && (r.Status < 200 || r.Status >= 300) {
			return "http"
		}
		return "probe"
	}
	if r.SkippedParam {
		return "skip"
	}
	if len(r.Res.Mismatches) > 0 {
		return "shape"
	}
	return "pass"
}
func liveConfiguration(get func(string) string) string {
	names := []string{"DEVBOX_HOST", "OPN2OTEL_OPS_API_KEY", "OPN2OTEL_OPS_API_SECRET", "CANARY_PROFILE"}
	count := 0
	for _, name := range names {
		if get(name) != "" {
			count++
		}
	}
	if count == 0 && get("OPN2OTEL_LIVE_REQUIRED") == "" {
		return "skip"
	}
	if count != len(names) || !slices.Contains(opnsense.KnownProbeProfiles(), get("CANARY_PROFILE")) {
		return "invalid"
	}
	u, err := url.Parse("https://" + get("DEVBOX_HOST"))
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "invalid"
	}
	return "run"
}

func TestLiveContractVerdicts(t *testing.T) {
	plugin := opnsense.PluginGatedEndpoints()[0]
	for _, tc := range []struct {
		name, endpoint, body, want string
		code                       int
	}{
		{"clean", "core", `{"rows":[],"total":0}`, "pass", 200},
		{"broken shape", "core", `{"rows":[],"total":{}}`, "shape", 200},
		{"core absent", "core", `{}`, "core-absent", 404},
		{"plugin absent", string(plugin), `{}`, "skip", 404},
		{"authentication", "core", `{}`, "http", 401},
		{"non JSON", "core", `sensitive-body`, "probe", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProber(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			p.notes = io.Discard
			r := p.probeOne(testSchema(tc.endpoint, "/contract"), opnsense.SchemaExemption{})
			if got := liveVerdict(r); got != tc.want {
				t.Fatalf("verdict=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestLiveConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"absent", nil, "skip"},
		{"partial", map[string]string{"DEVBOX_HOST": "example.invalid"}, "invalid"},
		{"required absent", map[string]string{"OPN2OTEL_LIVE_REQUIRED": "true"}, "invalid"},
		{"complete", map[string]string{"DEVBOX_HOST": "example.invalid", "OPN2OTEL_OPS_API_KEY": "fixture", "OPN2OTEL_OPS_API_SECRET": "fixture", "CANARY_PROFILE": "nightly"}, "run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := liveConfiguration(func(k string) string { return tc.env[k] }); got != tc.want {
				t.Fatalf("configuration=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestLiveResolverSourceAbsence(t *testing.T) {
	for _, endpoint := range []string{"smartInfo", "ipsecPhase2", "backupDiff"} {
		for _, code := range []int{0, 404, 401, 403, 500} {
			t.Run(fmt.Sprintf("%s/%d", endpoint, code), func(t *testing.T) {
				p := newTestProber(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if code == 0 {
						hijackAndCorrupt(w)
						return
					}
					w.WriteHeader(code)
				}))
				p.notes = io.Discard
				p.pause = func(time.Duration) {}
				s := opnsense.EndpointSchema{Endpoint: endpoint, Path: "/unused", TopLevelKind: opnsense.KindObject}
				r := p.probeOne(s, opnsense.SchemaExemption{})
				got := liveVerdict(r)
				if code == 404 && endpoint != "backupDiff" {
					if got != "skip" {
						t.Fatalf("declared plugin source absence verdict=%s", got)
					}
				} else if got == "skip" || got == "pass" {
					t.Fatalf("source failure incorrectly accepted: %s", got)
				}
			})
		}
	}
}

func TestLiveFailureFingerprint(t *testing.T) {
	a := map[string]string{"systemTime": "probe", "systemDisk": "shape"}
	b := map[string]string{"systemTime": "probe", "systemDisk": "shape"}
	c := map[string]string{"systemResources": "probe", "systemDisk": "shape"}
	textA, hashA := liveFailureSummary(a)
	_, hashB := liveFailureSummary(b)
	_, hashC := liveFailureSummary(c)
	if hashA != hashB || hashA == hashC {
		t.Fatal("failure identity fingerprint is not stable and discriminating")
	}
	if textA != "systemDisk shape\nsystemTime probe\n" {
		t.Fatalf("unexpected sanitized summary: %q", textA)
	}
	sanitized, _ := liveFailureSummary(map[string]string{"secret-host": "body-secret", "systemTime": "payload-secret"})
	if sanitized != "unknown invalid\n" {
		t.Fatal("untrusted values were not suppressed")
	}
}
func liveFailureSummary(failures map[string]string) (string, string) {
	allowed := map[string]bool{"configuration": true, "registry": true, "ledger": true, "preflight": true}
	for name := range opnsense.ContractManifest() {
		allowed[string(name)] = true
	}
	classes := map[string]bool{"invalid": true, "unavailable": true, "probe": true, "http": true, "shape": true, "core-absent": true}
	unique := map[string]bool{}
	for name, class := range failures {
		if !allowed[name] || !classes[class] {
			unique["unknown invalid"] = true
		} else {
			unique[name+" "+class] = true
		}
	}
	lines := make([]string, 0, len(unique))
	for line := range unique {
		lines = append(lines, line)
	}
	slices.Sort(lines)
	summary := strings.Join(lines, "\n")
	if summary != "" {
		summary += "\n"
	}
	return summary, fmt.Sprintf("%x", sha256.Sum256([]byte(summary)))
}

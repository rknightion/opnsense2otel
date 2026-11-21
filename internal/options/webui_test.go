package options

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kingpin/v2"
)

func TestWebUIIsOptInByDefault(t *testing.T) {
	for _, flag := range kingpin.CommandLine.Model().Flags {
		if flag.Name == "web.ui-enabled" {
			if got := strings.Join(flag.Default, ","); got != "false" {
				t.Fatalf("web.ui-enabled default = %q, want false", got)
			}
			return
		}
	}
	t.Fatal("web.ui-enabled flag not registered")
}

// isolateEffectiveConfigFiles keeps local secret configuration out of fixtures.
func isolateEffectiveConfigFiles(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPN2OTEL_OPS_API_KEY_FILE", "OPS_API_KEY_FILE",
		"OPN2OTEL_OPS_API_SECRET_FILE", "OPS_API_SECRET_FILE",
		"OPN2OTEL_PYROSCOPE_AUTH_USER_FILE", "PYROSCOPE_AUTH_USER_FILE",
		"OPN2OTEL_PYROSCOPE_AUTH_PASSWORD_FILE", "PYROSCOPE_AUTH_PASSWORD_FILE",
		"OPN2OTEL_ANNOTATIONS_TOKEN_FILE", "OPN2OTEL_GEOIP_DOWNLOAD_LICENSE_KEY_FILE",
		"OPN2OTEL_OTLP_HEADERS_FILE", "OPN2OTEL_LOGS_ZENARMOR_AUTH_PASSWORD_FILE",
	} {
		t.Setenv(key, "")
	}
}

// TestBuildEffectiveConfig_SecretsRedacted checks presence-only fields in the
// builder and URL sanitization on the full EffectiveConfig path.
func TestBuildEffectiveConfig_SecretsRedacted(t *testing.T) {
	isolateEffectiveConfigFiles(t)
	in := configInputs{host: "fw.example", apiKeySet: true, apiSecretSet: true, otlpHeadersSet: true, zenarmorAuthSet: true}
	found := false
	for _, sec := range buildEffectiveConfig(in) {
		for _, it := range sec.Items {
			if it.Secret && it.Value != redacted && it.Value != "—" {
				t.Fatalf("secret %q not redacted: %q", it.Key, it.Value)
			}
			if it.Key == "Zenarmor Auth" {
				found = true
				if !it.Secret || it.Value != redacted {
					t.Errorf("Zenarmor Auth not redacted: %+v", it)
				}
			}
		}
	}
	if !found {
		t.Fatal("Zenarmor Auth row missing")
	}
	credentialURL := url.URL{
		Scheme: "https", Host: "service.example", Path: "/path",
		User: url.UserPassword("sampleuser", "samplepass"), RawQuery: "token=sampletoken&x=1",
	}
	flags := []*string{otlpEndpoint, pyroscopeServerAddress, annotationsGrafanaURL}
	for _, flag := range flags {
		old := *flag
		t.Cleanup(func() { *flag = old })
		*flag = credentialURL.String()
	}
	*otlpEndpoint = "\t " + credentialURL.String() + " \t"
	wantRows := map[string]string{"Telemetry (OTLP)": "Endpoint", "Pyroscope": "Server", "Annotations": "Grafana URL"}
	seen := 0
	for _, sec := range EffectiveConfig() {
		for _, item := range sec.Items {
			for _, secret := range []string{"sampleuser", "samplepass", "sampletoken"} {
				if strings.Contains(item.Value, secret) {
					t.Errorf("%s/%s exposes synthetic credential %s", sec.Title, item.Key, secret)
				}
			}
			if wantRows[sec.Title] == item.Key {
				seen++
				if !strings.Contains(item.Value, "service.example") {
					t.Errorf("%s/%s lost host: %q", sec.Title, item.Key, item.Value)
				}
			}
		}
	}
	if seen != len(wantRows) {
		t.Errorf("found %d URL rows, want %d", seen, len(wantRows))
	}
	plain := "https://service.example/a%2Fb?z=1&x=hello%20world"
	for _, flag := range flags {
		*flag = plain
	}
	*otlpEndpoint = "\t " + plain + " \t"
	for _, sec := range EffectiveConfig() {
		for _, item := range sec.Items {
			want := plain
			if sec.Title == "Telemetry (OTLP)" {
				want = *otlpEndpoint
			}
			if wantRows[sec.Title] == item.Key && item.Value != want {
				t.Errorf("%s/%s = %q, want unchanged %q", sec.Title, item.Key, item.Value, want)
			}
		}
	}
}

func TestEffectiveConfig_FileSecrets(t *testing.T) {
	isolateEffectiveConfigFiles(t)
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("placeholder\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPN2OTEL_LOGS_ZENARMOR_AUTH_PASSWORD_FILE", path)
	t.Setenv("OPN2OTEL_OTLP_HEADERS_FILE", path)
	oldUser, oldPassword, oldHeaders := *logsZenarmorAuthUser, *logsZenarmorAuthPassword, *otlpHeaders
	t.Cleanup(func() {
		*logsZenarmorAuthUser, *logsZenarmorAuthPassword, *otlpHeaders = oldUser, oldPassword, oldHeaders
	})
	*logsZenarmorAuthUser, *logsZenarmorAuthPassword, *otlpHeaders = "user", "", ""
	check := func(want string) {
		t.Helper()
		found := map[string]bool{}
		for _, section := range EffectiveConfig() {
			for _, item := range section.Items {
				if item.Key == "Zenarmor Auth" || (section.Title == "Telemetry (OTLP)" && item.Key == "Headers") {
					found[item.Key] = true
					if !item.Secret || item.Value != want {
						t.Errorf("%s = %+v, want %s", item.Key, item, want)
					}
				}
			}
		}
		if !found["Zenarmor Auth"] || !found["Headers"] {
			t.Errorf("missing secret rows: %v", found)
		}
	}
	check(redacted)
	t.Setenv("OPN2OTEL_LOGS_ZENARMOR_AUTH_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("OPN2OTEL_OTLP_HEADERS_FILE", filepath.Join(t.TempDir(), "missing"))
	check(unset)
}

func TestEffectiveConfig_ZenarmorWhitespacePassword(t *testing.T) {
	isolateEffectiveConfigFiles(t)
	withZenarmorFlags(t, func() {
		*logsZenarmorEnabled, *logsZenarmorListenHTTP = true, ":9200"
		*logsZenarmorAuthUser, *logsZenarmorAuthPassword = "user", " \t "
		cfg, enabled, err := LogsZenarmor()
		if err != nil || !enabled || cfg.AuthPassword == "" {
			t.Fatalf("expected startup to configure authentication: enabled=%v err=%v", enabled, err)
		}
		for _, section := range EffectiveConfig() {
			for _, item := range section.Items {
				if item.Key == "Zenarmor Auth" {
					if !item.Secret || item.Value != redacted {
						t.Errorf("Zenarmor Auth should show redacted presence, got %+v", item)
					}
					return
				}
			}
		}
		t.Error("Zenarmor Auth row missing")
	})
}

// TestBuildEffectiveConfig_PresenceFalseRendersDash confirms an unset secret
// renders as the em-dash placeholder, not empty/blank.
func TestBuildEffectiveConfig_PresenceFalseRendersDash(t *testing.T) {
	in := configInputs{host: "fw.example"}
	for _, sec := range buildEffectiveConfig(in) {
		for _, it := range sec.Items {
			if it.Secret && it.Value != "—" {
				t.Fatalf("unset secret %q should render as em-dash, got %q", it.Key, it.Value)
			}
		}
	}
}

// TestBuildEffectiveConfig_PlainValuesPassThrough checks a couple of ordinary
// (non-secret) fields render their real value verbatim, and blank ones render "—".
func TestBuildEffectiveConfig_PlainValuesPassThrough(t *testing.T) {
	in := configInputs{
		host:        "fw.example",
		metricsPath: "/metrics",
		insecure:    true,
	}
	out := buildEffectiveConfig(in)
	var gotHost, gotInsecure, gotMetrics string
	for _, sec := range out {
		for _, it := range sec.Items {
			switch it.Key {
			case "Host":
				gotHost = it.Value
			case "Insecure TLS":
				gotInsecure = it.Value
			case "Metrics Path":
				gotMetrics = it.Value
			}
		}
	}
	if gotHost != "fw.example" {
		t.Fatalf("Host = %q", gotHost)
	}
	if gotInsecure != "on" {
		t.Fatalf("Insecure TLS = %q", gotInsecure)
	}
	if gotMetrics != "/metrics" {
		t.Fatalf("Metrics Path = %q", gotMetrics)
	}
}

// TestBuildEffectiveConfig_CollectorsPopulated asserts the Collectors section is
// present and non-empty, and every item there is a plain on/off item (never Secret).
func TestBuildEffectiveConfig_CollectorsPopulated(t *testing.T) {
	in := configInputs{collectors: []ConfigItem{{Key: "ARP", Value: "on"}, {Key: "Netflow", Value: "off"}}}
	out := buildEffectiveConfig(in)
	found := false
	for _, sec := range out {
		if sec.Title != "Collectors" {
			continue
		}
		found = true
		if len(sec.Items) != 2 {
			t.Fatalf("want 2 collector items, got %d", len(sec.Items))
		}
		for _, it := range sec.Items {
			if it.Secret {
				t.Fatalf("collector item %q must not be Secret", it.Key)
			}
		}
	}
	if !found {
		t.Fatal("Collectors section missing")
	}
}

func TestPrettifyFieldName(t *testing.T) {
	cases := map[string]string{
		"ARP":               "ARP",
		"Cron":              "Cron",
		"Wireguard":         "Wireguard",
		"IPsec":             "IPsec",
		"IPsecLeaseDetails": "IPsec Lease Details",
		"FirewallNATCounts": "Firewall NAT Counts",
		"OpenVPNDetails":    "Open VPN Details",
		"TrafficShaper":     "Traffic Shaper",
		"UnboundQStats":     "Unbound QStats",
	}
	for in, want := range cases {
		if got := prettifyFieldName(in); got != want {
			t.Errorf("prettifyFieldName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildEffectiveConfig_AnnotationsLogsFlowPresent covers #518: --config.check
// and the /config page previously printed nothing for annotations, log shipping
// or flow even when all three were fully configured. Assert the sections exist
// and carry the specific facts the issue calls out as security/operationally
// relevant (allowlist count, not just on/off; token/lookback/max-per-cycle).
func TestBuildEffectiveConfig_AnnotationsLogsFlowPresent(t *testing.T) {
	in := configInputs{
		host: "fw.example",

		annotationsEnabled:     true,
		annotationsURL:         "https://grafana.example",
		annotationsTokenSet:    true,
		annotationsLookback:    "24h0m0s",
		annotationsInterval:    "1m0s",
		annotationsMaxPerCycle: 20,

		logsEnabled: true,
		logsSink:    "otlp",

		syslogEnabled:        true,
		syslogListen:         "udp :5514, tcp :5514",
		syslogTLS:            false,
		syslogAllowlistCount: 3,
		syslogDebugCapture:   true,

		zenarmorEnabled:        true,
		zenarmorListen:         ":9200",
		zenarmorTLS:            true,
		zenarmorAllowlistCount: 0,
		zenarmorDebugCapture:   false,

		logsDebugCaptureDir: "/var/log/capture",

		flowEnabled:   true,
		flowCorrelate: true,
		flowLogMode:   "per_flow",

		netflowEnabled:        true,
		netflowListen:         ":2055",
		netflowAllowlistCount: 1,
		netflowDebugCapture:   "unidentified",
	}
	sections := buildEffectiveConfig(in)

	byTitle := map[string]ConfigSection{}
	for _, sec := range sections {
		byTitle[sec.Title] = sec
	}

	for _, want := range []string{"Annotations", "Log shipping", "Flow"} {
		if _, ok := byTitle[want]; !ok {
			t.Fatalf("missing section %q", want)
		}
	}

	items := func(title string) map[string]ConfigItem {
		m := map[string]ConfigItem{}
		for _, it := range byTitle[title].Items {
			m[it.Key] = it
		}
		return m
	}

	ann := items("Annotations")
	if ann["Enabled"].Value != "on" {
		t.Errorf("Annotations Enabled = %q, want on", ann["Enabled"].Value)
	}
	if ann["Grafana URL"].Value != "https://grafana.example" {
		t.Errorf("Grafana URL = %q", ann["Grafana URL"].Value)
	}
	if !ann["Token"].Secret || ann["Token"].Value != redacted {
		t.Errorf("Token = %+v, want redacted secret", ann["Token"])
	}
	if ann["Lookback"].Value != "24h0m0s" {
		t.Errorf("Lookback = %q", ann["Lookback"].Value)
	}
	if ann["Max Per Cycle"].Value != "20" {
		t.Errorf("Max Per Cycle = %q", ann["Max Per Cycle"].Value)
	}

	logs := items("Log shipping")
	if logs["Syslog Allowed Peers"].Value != "set (3 prefixes)" {
		t.Errorf("Syslog Allowed Peers = %q", logs["Syslog Allowed Peers"].Value)
	}
	if logs["Zenarmor Allowed Peers"].Value != "open (no allowlist)" {
		t.Errorf("Zenarmor Allowed Peers = %q", logs["Zenarmor Allowed Peers"].Value)
	}
	if logs["Syslog Debug Capture"].Value != "on" {
		t.Errorf("Syslog Debug Capture = %q", logs["Syslog Debug Capture"].Value)
	}
	if logs["Debug Capture Dir"].Value != "/var/log/capture" {
		t.Errorf("Debug Capture Dir = %q", logs["Debug Capture Dir"].Value)
	}
	if logs["Zenarmor TLS"].Value != "on" {
		t.Errorf("Zenarmor TLS = %q", logs["Zenarmor TLS"].Value)
	}

	flow := items("Flow")
	if flow["NetFlow Allowed Peers"].Value != "set (1 prefix)" {
		t.Errorf("NetFlow Allowed Peers = %q", flow["NetFlow Allowed Peers"].Value)
	}
	if flow["NetFlow Debug Capture"].Value != "unidentified" {
		t.Errorf("NetFlow Debug Capture = %q", flow["NetFlow Debug Capture"].Value)
	}
}

func TestCollectorConfigItems_HaveDisplay(t *testing.T) {
	items := collectorConfigItems()
	if len(items) == 0 {
		t.Fatal("no collector items")
	}
	for _, it := range items {
		if it.Key == "" || it.Display == "" {
			t.Fatalf("collector item missing Key/Display: %+v", it)
		}
	}
}

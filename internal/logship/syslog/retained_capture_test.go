package syslog

import (
	"net"
	"strings"
	"testing"
)

// All fixtures are invented. These messages previously entered unknown capture;
// new rules must retain shipping bodies and cannot create derived metrics.
func TestRetainedCaptureDispositions(t *testing.T) {
	cases := []struct{ program, message, status, reason string }{
		{"configd.py", "Starting daemon.", "known", "config_daemon_lifecycle"},
		{"root", "Bogons V6 file updated: 8 addresses changed.", "known", "bogon_update_progress"},
		{"miniupnpd", "Failed to get IP for interface example0", "parsed", "upnp_interface_address_failed"},
		{"miniupnpd", "PCP: Invalid PCP v2 MAP message.", "parsed", "upnp_pcp_invalid"},
		{"unbound", "[3:0] error: SERVFAIL <example.invalid. A IN>: all the configured stub or forward servers failed, at zone . upstream server timeout", "parsed", "resolver_upstream_failed"},
		{"dhcp6c", "transmit failed: Can't assign requested address", "parsed", "dhcp6_transmit_failed"},
		{"sasldblistusers2", "DIGEST-MD5 common mech free", "known", "sasl_library_trace"},
		{"saslpasswd2", "DIGEST-MD5 common mech free", "known", "sasl_library_trace"},
		{"saslpasswd2", "_plug_EVP_MD_CTX_new()", "known", "sasl_library_trace"},
		{"saslpasswd2", "Setpass for OTP successful", "parsed", "sasl_otp_password_set"},
		{"charon", "01[DMN] SIGTERM received, shutting down", "known", "ipsec_daemon_lifecycle"},
		{"ppp", "caught fatal signal TERM", "parsed", "ppp_signal_received"},
		{"ppp", "[example] IFACE: Close event", "parsed", "ppp_interface_close_requested"},
		{"ppp", "[example] LCP: no reply to 2 echo request(s)", "parsed", "ppp_echo_reply_missing"},
		{"ppp", "[example] LCP: peer not responding to echo requests", "parsed", "ppp_peer_not_responding"},
		{"ppp", "[example] LCP: SendTerminateReq #9", "known", "ppp_negotiation_trace"},
		{"ppp", "[example] can't remove hook example-hook from node \"example-node\": No such file or directory", "parsed", "ppp_hook_remove_failed"},
		{"ppp", "[example] PPPoE connection timeout after 7 seconds", "parsed", "pppoe_connection_timeout"},
		{"kernel", "<5>[9] Limiting ICMPv6 destination unreachable output from 20 to 10 packets/sec", "parsed", "icmp6_unreachable_rate_limited"},
		{"kernel", "[9] Waiting (max 60 seconds) for system process `vnlru' to stop... done", "known", "shutdown_progress"},
		{"kernel", "[9] Syncing disks, vnodes remaining... 2 0 ", "known", "shutdown_progress"},
		{"kernel", "[9] All buffers synced.", "known", "shutdown_progress"},
		{"kernel", "[9] uhub1: detached", "known", "device_detached"},
		{"kernel", "<6>[9] Configuring loopback interface...done.", "known", "boot_configuration_progress"},
		{"kernel", "[9] example0: Disabling FW LLDP agent failed: error: I40E_ERR_ADMIN_QUEUE_ERROR, I40E_AQ_RC_ERANGE", "parsed", "firmware_lldp_disable_failed"},
		{"opnsense", "/usr/local/etc/rc.bootup: Device example0 required for example1, configuring now", "parsed", "interface_configuration_started"},
		{"opnsense", "/usr/local/etc/rc.bootup: Unable to configure nonexistent interface example1 (example0)", "parsed", "interface_configuration_failed"},
		{"opnsense", "AcmeClient: renew certificate: example.invalid", "parsed", "certificate_renewal_requested"},
		{"lldpd", "unable to disable LLDP in firmware for example0 via dev.example.1.fw_lldp: Invalid argument", "parsed", "lldp_firmware_disable_failed"},
		{"lighttpd", "(/example/mod_openssl.c.12) SSL: addr:" + net.IPv4(192, 0, 2, 1).String() + " ssl_err:1 error:0A000102:SSL routines::unsupported protocol", "parsed", "tls_protocol_unsupported"},
		{"ddclient", "Account sample-account [example - example]  no global IP address detected, check config if warning persists", "parsed", "dynamic_dns_address_missing"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Renew: example.invalid", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Renewing: 'example.invalid'", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Renewing using Le_API=https://ca.example.invalid/directory", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Current renewal time: 2026-01-01T01:00:00Z", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] ARI suggestedWindow: 2026-01-01T01:00:00Z to 2026-01-02T01:00:00Z", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Updating renewal time picked from ARI window: 2026-01-01T01:00:00Z", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] ARI suggested renewal has passed (2026-01-01T01:00:00Z), proceeding with renewal.", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Single domain='example.invalid'", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Getting webroot for domain='example.invalid'", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Adding record", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Added, OK", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] The TXT record has been successfully added.", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Sleeping for 60 seconds to wait for the the TXT records to take effect", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Verifying: example.invalid", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Removing DNS records.", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Successfully removed", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Let's finalize the order.", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Le_OrderFinalize='https://ca.example.invalid/order/sample'", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Downloading cert.", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Le_LinkCert='https://ca.example.invalid/cert/sample'", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Your cert is in: /example/cert.pem", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Your cert key is in: /example/key.pem", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] The intermediate CA cert is in: /example/ca.pem", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] And the full-chain cert is in: /example/chain.pem", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Next renewal time picked from ARI window: 2026-02-01T01:00:00Z", "known", "certificate_automation_progress"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Installing key to: /example/key.pem", "known", "certificate_automation_progress"},
		{"config", "AcmeClient: updated ACME example.invalid certificate: example.invalid (abcdef12)", "parsed", "certificate_updated"},
		{"config", "AcmeClient: running automations for certificate: example.invalid", "parsed", "certificate_automation_requested"},
		{"config", "AcmeClient: running automation (configd): restart-example", "parsed", "certificate_action_requested"},
	}
	for _, tc := range cases {
		t.Run(tc.program+"/"+tc.reason+"/"+tc.message, func(t *testing.T) {
			env := Envelope{Program: tc.program, Message: tc.message}
			rec, parsed := buildRecord(env, nil, nil)
			if parsed != (tc.status == "parsed") {
				t.Errorf("parsed=%v; want status=%s", parsed, tc.status)
			}
			key := "syslog.event"
			if tc.status == "known" {
				key = "syslog.parse_reason"
			}
			assertAttrs(t, rec, map[string]string{"syslog.parse_status": tc.status, key: tc.reason})
			if rec.Body != tc.message {
				t.Error("shipping body changed")
			}
			if observeDerived(&fakeSink{}, tc.program, rec.Attributes) {
				t.Error("diagnostic derived a metric")
			}
			wrong, _ := buildRecord(Envelope{Program: "unfamiliar-program", Message: tc.message}, nil, nil)
			if wrong.Attributes["syslog.parse_status"] != "unknown" {
				t.Error("claimed another program's message")
			}
			if tc.status == "known" {
				near, _ := buildRecord(Envelope{Program: tc.program, Message: tc.message + " failed: permission denied"}, nil, nil)
				if near.Attributes["syslog.parse_status"] != "unknown" {
					t.Error("failure suffix hidden as progress")
				}
			}
			for _, field := range []string{"code.location", "ddclient.account", "acme.service", "certificate.id"} {
				if _, ok := rec.Attributes[field]; ok {
					t.Errorf("unnecessary identifier field %s", field)
				}
			}
		})
	}
}

func TestRetainedCaptureDiagnosticFields(t *testing.T) {
	rec, _ := buildRecord(Envelope{Program: "ppp", Message: "[example] PPPoE connection timeout after 7 seconds"}, nil, nil)
	assertAttrs(t, rec, map[string]string{"ppp.link": "example", "pppoe.timeout_seconds": "7", "syslog.event": "pppoe_connection_timeout"})
	rec, _ = buildRecord(Envelope{Program: "unbound", Message: "[1:0] error: SERVFAIL <example.invalid. HTTPS IN>: all the configured stub or forward servers failed, at zone . no server to query nameserver addresses not usable have no nameserver names"}, nil, nil)
	assertAttrs(t, rec, map[string]string{"dns.query_name": "example.invalid.", "dns.query_type": "HTTPS", "dns.zone": ".", "error.message": "no server to query nameserver addresses not usable have no nameserver names"})
	rec, _ = buildRecord(Envelope{Program: "saslpasswd2", Message: "Setpass for OTP successful"}, nil, nil)
	assertAttrs(t, rec, map[string]string{"syslog.event": "sasl_otp_password_set", "sasl.mechanism": "OTP"})
}

func TestRetainedCaptureRejectsMalformedDiagnostics(t *testing.T) {
	cases := []struct{ program, message string }{
		{"unbound", "[1:0] error: SERVFAIL <example.invalid. A IN>: unrelated failure"},
		{"unbound", "[1:0] error: SERVFAIL <example.invalid. A IN>: all the configured stub or forward servers failed, at zone . upstream server timeout\nnew unclassified event"},
		{"lighttpd", "(/example/mod_openssl.c.12) SSL: addr:invalid-address ssl_err:1 error:0A000102:SSL routines::unsupported protocol"},
		{"acme.sh", "[invalid timestamp] Added, OK"},
		{"acme.sh", "[Mon Jan 1 01:00:00 UTC 2024] Unexpected challenge failure"},
		{"saslpasswd2", "Setpass for OTP failed"},
	}
	for _, tc := range cases {
		t.Run(tc.program+"/"+strings.ReplaceAll(tc.message, "\n", " "), func(t *testing.T) {
			r, parsed := buildRecord(Envelope{Program: tc.program, Message: tc.message}, nil, nil)
			if parsed || r.Attributes["syslog.parse_status"] != "unknown" {
				t.Error("malformed diagnostic claimed")
			}
		})
	}
}

package syslog

import (
	"net"
	"net/netip"
	"testing"

	"github.com/rknightion/opnsense2otel/v5/internal/logship"
)

// All values are invented. The table pins complete reboot grammars, not capture
// normalisation keys. Events are log-only; known rows still ship unchanged.
func TestUpgradeBootDispositions(t *testing.T) {
	cases := []struct{ program, message, status, outcome string }{
		{"pkg-static", "sample reinstalled: 1.0 -> 1.1", "parsed", "package_reinstalled"},
		{"pkg", "sample reinstalled: 1.0 -> 1.1", "parsed", "package_reinstalled"},
		{"pkg-static", "sample-1.0 installed", "parsed", "package_installed"},
		{"pkg", "sample-1.0 installed", "parsed", "package_installed"},
		{"pkg-static", "sample-1.0 deinstalled", "parsed", "package_deinstalled"},
		{"pkg", "sample-1.0 deinstalled", "parsed", "package_deinstalled"},
		{"configd.py", "Stopping daemon.", "known", "config_daemon_lifecycle"},
		{"configd.py", "unable to sendback response for 00000000-0000-4000-8000-000000000001, message was Traceback (most recent call last):   File \"/example.py\", line 1, in run FileNotFoundError: [Errno 2] No such file or directory: '/example'", "parsed", "config_response_failed"},
		{"zenoverlay", "zenoverlay instance zenoverlay (zen1) started", "known", "overlay_lifecycle"},
		{"opnsense", "/usr/local/etc/rc.bootup: The command <ifconfig example0> returned exit code 1 and the output was \"ifconfig: interface example0 does not exist\"", "parsed", "interface_command_failed"},
		{"opnsense", "/usr/local/etc/rc.bootup: Device vlan1 required by EXAMPLE(opt1), configuring now", "parsed", "interface_configuration_started"},
		{"opnsense", "/usr/local/etc/rc.bootup: Device lo0 required by (loopback)(), configuring now", "parsed", "interface_configuration_started"},
		{"opnsense", "/usr/local/etc/rc.bootup: IP renewal deferred during boot on 'example0'", "known", "boot_renewal_deferred"},
		{"opnsense", "/usr/local/etc/rc.newwanip: IP renewal starting (new: " + net.IPv4(192, 0, 2, 1).String() + ", old: , interface: opt1, device: example0, force: yes)", "parsed", "interface_address_renewal_started"},
		{"dhclient", "dhclient-script: New IP Address (example0): " + net.IPv4(192, 0, 2, 1).String(), "parsed", "dhcp_script_address_configuration"},
		{"dhclient", "dhclient-script: New Subnet Mask (example0): " + net.IPv4(255, 255, 255, 0).String(), "parsed", "dhcp_script_address_configuration"},
		{"dhclient", "dhclient-script: New Broadcast Address (example0): " + net.IPv4(192, 0, 2, 255).String(), "parsed", "dhcp_script_address_configuration"},
		{"dhclient", "dhclient-script: New Routers (example0): " + net.IPv4(192, 0, 2, 2).String(), "parsed", "dhcp_script_address_configuration"},
		{"dhcp6c", "rtsold_script: starting dhcp6c", "known", "dhcp_helper_start"},
		{"ppp", "Multi-link PPP daemon for FreeBSD", "known", "ppp_daemon_lifecycle"},
		{"ppp", "process 42 started, version 1.0", "known", "ppp_daemon_lifecycle"},
		{"ppp", "web: web is not running", "known", "ppp_daemon_lifecycle"},
		{"ppp", " ", "known", "ppp_daemon_lifecycle"},
		{"ppp", "[opt1] Bundle: Interface ng1 created", "parsed", "ppp_interface_created"},
		{"ppp", "[undefined] GetSystemIfaceMTU: SIOCGIFMTU failed: Device not configured", "parsed", "ppp_interface_mtu_failed"},
		{"ppp", "[opt1_link1] Link: OPEN event", "parsed", "ppp_link_open_requested"},
		{"devd", "Processing event '!system=ETHERNET subsystem=vlan1 type=IFATTACH'", "known", "device_node_notice"},
		{"devd", "Processing event '!system=GEOM subsystem=DEV type=CREATE cdev=md1'", "known", "device_node_notice"},
		{"devd", "Processing event '!system=DEVFS subsystem=CDEV type=MEDIACHANGE cdev=md1'", "known", "device_node_notice"},
		{"devd", "Processing event '!system=GEOM subsystem=DEV type=DESTROY cdev=ufsid/1234'", "known", "device_node_notice"},
		{"devd", "Processing event '!system=VFS subsystem=FS type=MOUNT mount-point=\"/example\" mount-dev=\"tmpfs\" mount-type=\"tmpfs\" fsid=0xabcd owner=0 flags=\"local;noexec;\"'", "known", "filesystem_mount_notice"},
		{"devd", "Executing '/usr/sbin/automount -c'", "known", "device_dispatch_trace"},
		{"daemon", "daemon: /example/file: No such file or directory", "parsed", "daemon_file_missing"},
		{"daemon", "00[LIB] no files found matching '/example/*.conf'", "known", "ipsec_optional_include_empty"},
		{"charon", "00[DMN] Starting IKE charon daemon (strongSwan 1.0, FreeBSD 1.0, amd64)", "known", "ipsec_daemon_configuration"},
		{"charon", "00[LIB] providers loaded by OpenSSL: default legacy", "known", "ipsec_daemon_configuration"},
		{"charon", "00[CFG] using '/example/resolv.conf' to install DNS servers", "known", "ipsec_daemon_configuration"},
		{"charon", "00[CFG] RADIUS server 'example' misses address, skipped", "parsed", "ipsec_radius_configuration_skipped"},
		{"charon", "00[CFG] loaded 0 RADIUS server configurations", "known", "ipsec_daemon_configuration"},
		{"charon", "00[LIB] loaded plugins: charon example-plugin", "known", "ipsec_daemon_configuration"},
		{"charon", "00[JOB] spawning 4 worker threads", "known", "ipsec_daemon_configuration"},
		{"charon", "00[CFG] loaded certificate 'C=XX, O=Example, CN=Example CA'", "known", "ipsec_credential_inventory"},
		{"charon", "00[CFG] loaded certificate 'C=XX, O=Example's CA, CN=Example'", "known", "ipsec_credential_inventory"},
		{"charon", "00[CFG] loaded IKE shared key with id 'ike-example' for: 'example-user'", "known", "ipsec_credential_inventory"},
		{"lldpd", "could not open either /etc/os-release or /usr/lib/os-release", "parsed", "lldp_os_information_unavailable"},
		{"lldpd", "lsb_release information not available", "known", "lldp_daemon_configuration"},
		{"lldpd", "no libcap support, running monitor as root", "known", "lldp_daemon_configuration"},
		{"lldpd", "/example/localtime copied to chroot", "known", "lldp_daemon_configuration"},
		{"lldpd", "protocol LLDP enabled", "known", "lldp_daemon_configuration"},
		{"lldpd", "protocol CDPv1 enabled", "known", "lldp_daemon_configuration"},
		{"lldpd", "protocol CDPv2 enabled", "known", "lldp_daemon_configuration"},
		{"lldpd", "protocol SONMP disabled", "known", "lldp_daemon_configuration"},
		{"lldpd", "protocol EDP enabled", "known", "lldp_daemon_configuration"},
		{"lldpd", "protocol FDP enabled", "known", "lldp_daemon_configuration"},
		{"lldpd", "libevent 1.0 initialized with kqueue method", "known", "lldp_daemon_configuration"},
		{"lldpd", "enable SNMP subagent", "known", "lldp_daemon_configuration"},
		{"lldpd", "ixl driver detected for ixl1, disabling LLDP in firmware", "known", "lldp_daemon_configuration"},
		{"lldpcli", "lldpd should resume operations", "known", "lldp_daemon_lifecycle"},
		{"eastpect", "nm1::example0:1: permanently promiscuous mode enabled", "known", "packet_capture_interface_mode"},
		{"eastpect", "nm1::example0^: permanently promiscuous mode enabled", "known", "packet_capture_interface_mode"},
		{"unbound", "[1:0] info: [25%]=0 median[50%]=0 [75%]=0", "known", "resolver_stats_dump"},
	}
	for _, tc := range cases {
		t.Run(tc.program+"/"+tc.message, func(t *testing.T) {
			rec, parsed := buildRecord(Envelope{Program: tc.program, Message: tc.message}, nil, nil)
			if parsed != (tc.status == "parsed") {
				t.Fatalf("parsed=%v want status=%s", parsed, tc.status)
			}
			assertAttrs(t, rec, map[string]string{"syslog.parse_status": tc.status})
			key := "syslog.event"
			if tc.status == "known" {
				key = "syslog.parse_reason"
			}
			assertAttrs(t, rec, map[string]string{key: tc.outcome})
			if rec.Body != tc.message {
				t.Fatal("body changed")
			}
			if observeDerived(&fakeSink{}, tc.program, rec.Attributes) {
				t.Fatal("boot record derived a metric")
			}
			// Nearby failures must not be silently classified known. Parsed diagnostic
			// text can contain arbitrary suffixes, but always remains a visible event.
			if tc.status == "known" {
				near, _ := buildRecord(Envelope{Program: tc.program, Message: tc.message + " failed: permission denied"}, nil, nil)
				if near.Attributes["syslog.parse_status"] != "unknown" {
					t.Fatal("failure suffix accepted")
				}
			}
		})
	}
}

func TestUpgradeConsoleProgress(t *testing.T) {
	messages := []string{
		"certctl: Modified 2 trust store links.", "certctl: Modified 1 trust store link.", "Writing trust bundles...done.", "Setting hostname: example.invalid",
		"Generating /etc/example.conf...done.", "Configuring system logging...done.", "Configuring hardware interfaces...done.", "Configuring tailscale interface...done.", "Configuring zenoverlay interface...done.", "Configuring LAN interface...done.", "Configuring OpenSSH...done.",
		"Configuring firewall.......done.", "Configuring firewall..", "Configuring EXAMPLE interface...", "Starting web GUI...done.", "Starting router advertisement service...done.", "Starting Dnsmasq...done.", "Starting Unbound DNS...done.", "Starting UPnP IGD & PCP/NAT-PMP service...done.", "Starting NTP service...done.",
		"Setting up routes...done.", "Setting up gateway monitor...done.", "Syncing OpenVPN settings...done.", "Sync KEA DHCP config...done.", "Configuring IPsec VPN...done.", ".....done.", ">>> Invoking start script 'example'", "Reconfiguring IPv4 on example0", "Reconfiguring IPv6 on example0", "setup example0", "setup example0 [egress only]",
		"Starting kea.", "Starting tailscaled.", "Starting alloy.", "Starting senpai.", "Starting eastpect.", "Starting ddclient_opn.", "Starting elasticsearch.", "Starting snmpd.", "Starting acme_http_challenge.", "Starting lldpd.", "Starting unbound_exporter.",
		"INFO/example: Starting /usr/local/sbin/kea-dhcp4 -c /example/kea.conf", "/dev/md1: 32.0MB (65536 sectors) block size 4096, fragment size 512", "\tusing 2 cylinder groups of 16.0MB, 4096 blks, 128 inodes.", "\twith soft updates", "super-block backups (for fsck_ffs -b #) at:", " 12, 24, 36, 48", "Service `sysctl' has been restarted.", "load feed example_feed [2 addresses added.]", "Root file system: zroot/example", "Mon Jan 1 12:34:56 UTC 2024", "*** example.invalid: OPNsense 1.0 (amd64) ***", " EXAMPLE (example0) -> v4/DHCP4: " + net.IPv4(192, 0, 2, 1).String() + "/24", " EXAMPLE (pppoe1) -> v4/PPPoE: " + net.IPv4(192, 0, 2, 1).String() + "/32", " EXAMPLE (example0) -> v6: " + netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}).String() + "/64", "                     v6/DHCP6: " + netip.AddrFrom16([16]byte{0xfe, 0x80, 15: 1}).String() + "%example0/64", "                     v6/t6: " + netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 2}).String() + "/64", "OK",
	}
	for _, msg := range messages {
		for _, prefix := range []string{"", "<6>[1] "} {
			t.Run(prefix+msg, func(t *testing.T) {
				env := Envelope{Program: "kernel", Message: prefix + msg}
				rec, parsed := buildRecord(env, nil, nil)
				if parsed {
					t.Fatal("progress claimed as parsed")
				}
				assertAttrs(t, rec, map[string]string{"syslog.parse_status": "known", "syslog.parse_reason": "boot_configuration_progress"})
				if rec.Body != env.Message {
					t.Fatal("body changed")
				}
				env.Message += " failed: permission denied"
				near, _ := buildRecord(env, nil, nil)
				if near.Attributes["syslog.parse_status"] != "unknown" {
					t.Fatal("failure suffix accepted")
				}
			})
		}
	}
}

func TestUpgradeDiagnosticsExtractMeaning(t *testing.T) {
	cases := []struct {
		message string
		attrs   map[string]string
	}{
		{"example0: permanently promiscuous mode enabled", map[string]string{"syslog.event": "interface_promiscuous_enabled", "interface": "example0"}},
		{"sonewconn: pcb 0xabc (" + net.IPv4(192, 0, 2, 1).String() + ":443 (proto 6)): Listen queue overflow: 12 already in queue awaiting acceptance (2 occurrences), euid 0, rgid 0, jail 0", map[string]string{"syslog.event": "listen_queue_overflow", "socket.pending": "12", "socket.overflow_occurrences": "2"}},
		{"Some peers are advertising routes but --accept-routes is false", map[string]string{"syslog.event": "advertised_routes_ignored"}},
		{"warning: ignoring JAVA_HOME=/example/jdk; using ES_JAVA_HOME", map[string]string{"syslog.event": "java_home_ignored", "java.home.ignored": "/example/jdk"}},
		{"1.234 [ 1] iflib_netmap_config txr 2 rxr 3 txd 128 rxd 256 rbufsz 2048", map[string]string{"syslog.event": "netmap_interface_configuration", "netmap.tx_rings": "2", "netmap.rx_rings": "3", "netmap.tx_descriptors": "128", "netmap.rx_descriptors": "256", "netmap.receive_buffer_size": "2048"}},
	}
	for _, tc := range cases {
		t.Run(tc.message, func(t *testing.T) {
			rec, parsed := buildRecord(Envelope{Program: "kernel", Message: "<6>[1] " + tc.message}, nil, nil)
			if !parsed {
				t.Fatal("diagnostic not parsed")
			}
			assertAttrs(t, rec, tc.attrs)
			if observeDerived(&fakeSink{}, "kernel", rec.Attributes) {
				t.Fatal("diagnostic derived a metric")
			}
		})
	}
}

func TestUpgradeElasticsearchEnvelope(t *testing.T) {
	for _, tc := range []struct {
		level    string
		severity logship.Severity
	}{{"INFO ", logship.SeverityInfo}, {"WARN ", logship.SeverityWarn}, {"ERROR", logship.SeverityError}, {"DEBUG", logship.SeverityDebug}, {"TRACE", logship.SeverityDebug}} {
		msg := "<6>[1] [2024-01-01T12:34:56,789][" + tc.level + "][o.e.example.Logger   ] [example.invalid] diagnostic with [details]"
		rec, parsed := buildRecord(Envelope{Program: "kernel", Message: msg}, nil, nil)
		if !parsed {
			t.Fatal("console envelope not parsed")
		}
		assertAttrs(t, rec, map[string]string{"syslog.event": "elasticsearch_log", "log.logger": "o.e.example.Logger", "elasticsearch.node": "example.invalid", "elasticsearch.message": "diagnostic with [details]"})
		if rec.Severity != tc.severity {
			t.Fatalf("severity=%v want %v", rec.Severity, tc.severity)
		}
		if rec.Body != msg {
			t.Fatal("body changed")
		}
	}
	for _, msg := range []string{
		"[2024-01-01T12:34:56,789][FATAL][o.e.example.Logger] [example] diagnostic",
		"[2024-01-01T12:34:56,789][INFO ][o.e.example.Logger] diagnostic",
		"panic: boot failed", "Starting unfamiliar_service.", "Setting hostname: example.invalid failed",
	} {
		rec, _ := buildRecord(Envelope{Program: "kernel", Message: msg}, nil, nil)
		if rec.Attributes["syslog.parse_status"] != "unknown" {
			t.Fatal("unrecognised grammar accepted")
		}
	}
}

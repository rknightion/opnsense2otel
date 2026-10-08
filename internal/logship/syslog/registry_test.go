package syslog

import "testing"

// TestEveryParserProgramIsRegistered is the single registry check: each program
// name a parser claims must dispatch to some parser. A parser whose init()
// registration is dropped or mistyped ships its lines as generic records with no
// structured attributes, and nothing else fails loudly, so this table is the guard.
//
// Registration-shape contracts (exact vs prefix, body enrichment, subsystem) stay
// next to the parser they belong to; this only proves the program names resolve.
func TestEveryParserProgramIsRegistered(t *testing.T) {
	programs := []string{
		"acme.sh", "opnsense",
		"audit", "configd.py",
		"charon",
		"cron", "/usr/sbin/cron",
		"dhcpd", "dnsmasq", "dnsmasq-dhcp", "kea-dhcp4", "kea-dhcp6", "dhcrelay", "DhcpLFC",
		"dpinger",
		"firewall",
		"kernel",
		"miniupnpd",
		"openvpn", "openvpn_server1", "openvpn_server40", "openvpn_client2",
		"ppp",
		"radvd",
		"rule-updater.py",
		"sshd", "sshd-session",
		"syslog-ng",
		"tailscaled",
		"unbound",
		"wireguard",
	}
	for _, program := range programs {
		t.Run(program, func(t *testing.T) {
			if _, ok := parserFor(program); !ok {
				t.Errorf("no parser registered for program %q", program)
			}
		})
	}
}

package syslog

// Additional retained-capture dispositions. These events are log-only; requests,
// diagnostics and library traces do not establish a tunnel or login outcome.
// Private corpus values never belong here: patterns describe complete grammars.
func init() {
	knownMessage("configd.py", "config_daemon_lifecycle", `Starting daemon\.`)
	knownMessage("root", "bogon_update_progress", `Bogons V[46] file updated: \d+ addresses changed\.`)
	capturedEvent("miniupnpd", "upnp_interface_address_failed", `Failed to get IP for interface ([\w.-]+)`, "interface")
	capturedEvent("miniupnpd", "upnp_pcp_invalid", `PCP: Invalid PCP v(\d+) (MAP) message\.`, "pcp.version", "pcp.opcode")
	capturedEvent("unbound", "resolver_upstream_failed", `\[\d+:\d+\] error: SERVFAIL <([^\s<>]+) ([A-Z][A-Z0-9]*) (IN)>: all the configured stub or forward servers failed, at zone (\S+) (upstream server timeout|no server to query nameserver addresses not usable have no nameserver names)`, "dns.query_name", "dns.query_type", "dns.query_class", "dns.zone", "error.message")
	capturedEvent("dhcp6c", "dhcp6_transmit_failed", `transmit failed: (Can't assign requested address)`, "error.message")
	knownMessage("sasldblistusers2", "sasl_library_trace", `DIGEST-MD5 common mech free`)
	knownMessage("saslpasswd2", "sasl_library_trace", `DIGEST-MD5 common mech free`)
	knownMessage("saslpasswd2", "sasl_library_trace", `_plug_EVP_MD_CTX_(?:new|free)\(\)`)
	capturedEvent("saslpasswd2", "sasl_otp_password_set", `Setpass for (OTP) successful`, "sasl.mechanism")
	knownMessage("charon", "ipsec_daemon_lifecycle", `\d+\[DMN\] SIGTERM received, shutting down`)
	capturedEvent("ppp", "ppp_signal_received", `caught fatal signal (TERM)`, "process.signal")
	capturedEvent("ppp", "ppp_interface_close_requested", `\[([\w.-]+)\] IFACE: Close event`, "ppp.bundle")
	capturedEvent("ppp", "ppp_echo_reply_missing", `\[([\w.-]+)\] LCP: no reply to (\d+) echo request\(s\)`, "ppp.link", "ppp.echo_requests")
	capturedEvent("ppp", "ppp_peer_not_responding", `\[([\w.-]+)\] LCP: peer not responding to echo requests`, "ppp.link")
	knownMessage("ppp", "ppp_negotiation_trace", `\[[\w.-]+\] LCP: SendTerminateReq #\d+`)
	capturedEvent("ppp", "ppp_hook_remove_failed", `\[([\w.-]+)\] can't remove hook ([\w.-]+) from node "([^"\r\n]+)": (No such file or directory)`, "ppp.link", "ppp.hook", "ppp.node", "error.message")
	capturedEvent("ppp", "pppoe_connection_timeout", `\[([\w.-]+)\] PPPoE connection timeout after (\d+) seconds`, "ppp.link", "pppoe.timeout_seconds")
	capturedEvent("kernel", "icmp6_unreachable_rate_limited", `(?:<\d+>)?(?:\[\d+\] )?Limiting ICMPv6 destination unreachable output from (\d+) to (\d+) packets/sec`, "icmp6.rate.previous", "icmp6.rate.current")
	knownMessage("kernel", "shutdown_progress", "(?:<\\d+>)?(?:\\[\\d+\\] )?Waiting \\(max \\d+ seconds\\) for system process `(?:vnlru|syncer)' to stop\\.\\.\\. (?:done)?")
	knownMessage("kernel", "shutdown_progress", `(?:<\d+>)?(?:\[\d+\] )?Syncing disks, vnodes remaining\.\.\. \d+(?: \d+)* ?`)
	knownMessage("kernel", "shutdown_progress", `(?:<\d+>)?(?:\[\d+\] )?(?:done|All buffers synced\.|Uptime: (?:\d+d)?(?:\d+h)?(?:\d+m)?\d+s)`)
	knownMessage("kernel", "device_detached", `(?:<\d+>)?(?:\[\d+\] )?uhub\d+: detached`)
	knownMessage("kernel", "boot_configuration_progress", `(?:<\d+>)?(?:\[\d+\] )?(?:Configuring (?:loopback interface|LAGG interfaces)\.\.\.done\.|Setting up routes\.\.\.|Configuring firewall\.|\.{6}done\.)`)
	capturedEvent("kernel", "firmware_lldp_disable_failed", `(?:<\d+>)?(?:\[\d+\] )?([\w.-]+): Disabling FW LLDP agent failed: error: (I40E_ERR_ADMIN_QUEUE_ERROR), (I40E_AQ_RC_ERANGE)`, "interface", "error.code", "firmware.error_code")
	capturedEvent("opnsense", "interface_configuration_started", `/usr/local/etc/rc\.[\w.]+: Device ([\w.-]+) required for ([\w.-]+), configuring now`, "interface", "interface.name")
	capturedEvent("opnsense", "interface_configuration_failed", `/usr/local/etc/rc\.[\w.]+: Unable to configure nonexistent interface ([\w.-]+) \(([\w.-]+)\)`, "interface.name", "interface")
	capturedEvent("opnsense", "certificate_renewal_requested", `AcmeClient: renew certificate: ([\w*.-]+)`, "certificate.name")
	capturedEvent("lldpd", "lldp_firmware_disable_failed", `unable to disable LLDP in firmware for ([\w.-]+) via (dev\.[\w.]+\.fw_lldp): (Invalid argument)`, "interface", "sysctl.name", "error.message")
	// Empty field names deliberately omit incidental paths or account/certificate identifiers.
	capturedEvent("lighttpd", "tls_protocol_unsupported", `\((/[\w/.-]+/mod_openssl\.c\.\d+)\) SSL: addr:(\S+) ssl_err:(\d+) error:([0-9A-Fa-f]+):SSL routines::(unsupported protocol)`, "", "src.ip", "tls.error_number", "tls.error_code", "error.message")
	// Empty field names deliberately omit incidental paths or account/certificate identifiers.
	capturedEvent("ddclient", "dynamic_dns_address_missing", `Account (\S+) \[([\w.-]+) - ([\w.-]+)\]  (no global IP address detected, check config if warning persists)`, "", "ddclient.service", "ddclient.protocol", "error.message")
	// Each ACME progress body is complete. Errors and unfamiliar output stay unknown.
	const acmeTimestamp = `\[(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) +\d{1,2} \d{2}:\d{2}:\d{2} [A-Z]{2,5} \d{4}\] `
	for _, pattern := range []string{
		`Renew: [\w*.-]+`,
		`Renewing: '[\w*.-]+'`,
		`Renewing using Le_API=https://[A-Za-z0-9.-]+(?::\d+)?/[A-Za-z0-9_./?=&%+-]*`,
		`Current renewal time: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`,
		`ARI suggestedWindow: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z to \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`,
		`Updating renewal time picked from ARI window: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`,
		`ARI suggested renewal has passed \(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\), proceeding with renewal\.`,
		`Single domain='[\w*.-]+'`,
		`Getting webroot for domain='[\w*.-]+'`,
		`Adding record`,
		`Added, OK`,
		`The TXT record has been successfully added\.`,
		`Sleeping for \d+ seconds to wait for the the TXT records to take effect`,
		`Verifying: [\w*.-]+`,
		`Removing DNS records\.`,
		`Successfully removed`,
		`Let's finalize the order\.`,
		`Le_OrderFinalize='https://[A-Za-z0-9.-]+(?::\d+)?/[A-Za-z0-9_./?=&%+-]*'`,
		`Downloading cert\.`,
		`Le_LinkCert='https://[A-Za-z0-9.-]+(?::\d+)?/[A-Za-z0-9_./?=&%+-]*'`,
		`Your cert is in: /[\w/.-]+`,
		`Your cert key is in: /[\w/.-]+`,
		`The intermediate CA cert is in: /[\w/.-]+`,
		`And the full-chain cert is in: /[\w/.-]+`,
		`Next renewal time picked from ARI window: \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`,
		`Installing key to: /[\w/.-]+`,
	} {
		knownMessage("acme.sh", "certificate_automation_progress", acmeTimestamp+pattern)
	}
	// Empty field names deliberately omit incidental paths or account/certificate identifiers.
	capturedEvent("config", "certificate_updated", `AcmeClient: updated ACME ([\w.-]+) certificate: ([\w*.-]+) \(([0-9a-f]+)\)`, "", "certificate.name", "")
	capturedEvent("config", "certificate_automation_requested", `AcmeClient: running automations for certificate: ([\w*.-]+)`, "certificate.name")
	capturedEvent("config", "certificate_action_requested", `AcmeClient: running automation \((configd)\): ([\w.-]+)`, "automation.backend", "automation.action")
}

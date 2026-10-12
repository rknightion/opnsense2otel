package syslog

// Reboot console and daemon dispositions. Known rules describe complete progress
// messages only; explicit failures are log-only captured events, never counters.
// Variable configuration names are identifiers, not a pass-through of arbitrary
// trailing text. No rule exempts an entire program or severity.
func init() {
	for _, p := range []string{"pkg", "pkg-static"} {
		capturedEvent(p, "package_reinstalled", `([\w.+-]+) reinstalled: ([\w.+,~-]+) -> ([\w.+,~-]+)`, "package.name", "package.version.previous", "package.version")
		capturedEvent(p, "package_installed", `([\w.+-]+)-(\d[\w.+,~]*) installed`, "package.name", "package.version")
		capturedEvent(p, "package_deinstalled", `([\w.+-]+)-(\d[\w.+,~]*) deinstalled`, "package.name", "package.version")
	}
	knownMessage("configd.py", "config_daemon_lifecycle", `Stopping daemon\.`)
	// Tracebacks remain visible as failures, including the complete diagnostic.
	capturedEvent("configd.py", "config_response_failed", `unable to sendback response for ([\da-fA-F-]+), message was (Traceback \(most recent call last\):[^\r\n]+)`, "config.request", "error.message")
	knownMessage("zenoverlay", "overlay_lifecycle", `zenoverlay instance zenoverlay \(zen\d+\) started`)
	capturedEvent("opnsense", "interface_command_failed", opnsenseScript+`The command <([^>\r\n]+)> returned exit code (\d+) and the output was "([^\r\n]+)"`, "process.command", "process.exit_code", "error.message")
	capturedEvent("opnsense", "interface_configuration_started", opnsenseScript+`Device ([\w.-]+) required by ([\w.-]+)\(([\w.-]*)\), configuring now`, "interface", "interface.name", "interface.id")
	capturedEvent("opnsense", "interface_configuration_started", opnsenseScript+`Device ([\w.-]+) required by \((loopback)\)\(\), configuring now`, "interface", "interface.name")
	knownMessage("opnsense", "boot_renewal_deferred", opnsenseScript+`IP renewal deferred during boot on '[\w.-]+'`)
	capturedEvent("opnsense", "interface_address_renewal_started", opnsenseScript+`IP renewal starting \(new: (\S+), old: , interface: ([\w.-]+), device: ([\w.-]+), force: (yes|no)\)`, "interface.address", "interface.name", "interface", "interface.force")
	capturedEvent("dhclient", "dhcp_script_address_configuration", `dhclient-script: New (IP Address|Subnet Mask|Broadcast Address|Routers) \(([\w.-]+)\): ([\d.]+)`, "dhcp.script_field", "interface", "interface.address")
	knownMessage("dhcp6c", "dhcp_helper_start", `rtsold_script: starting dhcp6c`)
	knownMessage("ppp", "ppp_daemon_lifecycle", `(?:Multi-link PPP daemon for FreeBSD|process \d+ started, version [\w.-]+|web: web is not running|)`)
	capturedEvent("ppp", "ppp_interface_created", `\[([\w.-]+)\] Bundle: Interface ([\w.-]+) created`, "ppp.bundle", "interface")
	capturedEvent("ppp", "ppp_interface_mtu_failed", `\[([\w.-]+)\] GetSystemIfaceMTU: SIOCGIFMTU failed: (Device not configured)`, "ppp.link", "error.message")
	capturedEvent("ppp", "ppp_link_open_requested", `\[([\w.-]+)\] Link: OPEN event`, "ppp.link")
	knownMessage("devd", "device_node_notice", `Processing event '!system=ETHERNET subsystem=[\w.-]+ type=IFATTACH'`)
	knownMessage("devd", "device_node_notice", `Processing event '!system=(?:GEOM subsystem=DEV|DEVFS subsystem=CDEV) type=(?:CREATE|DESTROY|MEDIACHANGE) cdev=[\w./-]+'`)
	knownMessage("devd", "filesystem_mount_notice", `Processing event '!system=VFS subsystem=FS type=MOUNT mount-point="/[\w/.-]*" mount-dev="[\w/.-]+" mount-type="(?:devfs|nullfs|ufs|tmpfs)" fsid=0x[\da-f]+ owner=\d+ flags="(?:local|multilabel|read-only|soft-updates|noexec)(?:;(?:local|multilabel|read-only|soft-updates|noexec))*;"'`)
	knownMessage("devd", "device_dispatch_trace", `Executing '/usr/sbin/automount -c'`)
	capturedEvent("daemon", "daemon_file_missing", `daemon: (/[\w/.-]+): (No such file or directory)`, "file.path", "error.message")
	knownMessage("daemon", "ipsec_optional_include_empty", `\d+\[LIB\] no files found matching '/[\w/.*-]+'`)
	knownMessage("charon", "ipsec_daemon_configuration", `\d+\[DMN\] Starting IKE charon daemon \(strongSwan [\w.-]+, FreeBSD [\w.-]+, amd64\)`)
	knownMessage("charon", "ipsec_daemon_configuration", `\d+\[LIB\] providers loaded by OpenSSL: default legacy`)
	knownMessage("charon", "ipsec_daemon_configuration", `\d+\[CFG\] using '/[\w/.-]+' to install DNS servers`)
	capturedEvent("charon", "ipsec_radius_configuration_skipped", `\d+\[CFG\] RADIUS server '([^'\r\n]+)' misses address, skipped`, "radius.server")
	knownMessage("charon", "ipsec_daemon_configuration", `\d+\[CFG\] loaded \d+ RADIUS server configurations`)
	knownMessage("charon", "ipsec_daemon_configuration", `\d+\[LIB\] loaded plugins: [\w-]+(?: [\w-]+)*`)
	knownMessage("charon", "ipsec_daemon_configuration", `\d+\[JOB\] spawning \d+ worker threads`)
	// Certificate subjects and shared-key IDs are inventory, not authentication
	// outcomes. Do not extract identity/key identifiers into structured attributes.
	knownMessage("charon", "ipsec_credential_inventory", `\d+\[CFG\] loaded certificate '[^\r\n]+'`)
	knownMessage("charon", "ipsec_credential_inventory", `\d+\[CFG\] loaded IKE shared key with id 'ike-[^'\r\n]+' for: '[^'\r\n]+'`)
	capturedEvent("lldpd", "lldp_os_information_unavailable", `could not open either (/etc/[\w.-]+) or (/usr/lib/[\w.-]+)`, "file.path", "file.alternative_path")
	knownMessage("lldpd", "lldp_daemon_configuration", `(?:lsb_release information not available|no libcap support, running monitor as root|/[\w/.-]+ copied to chroot|protocol (?:LLDP|CDPv[12]|SONMP|EDP|FDP) (?:enabled|disabled)|libevent [\w.-]+ initialized with kqueue method|enable SNMP subagent|ixl driver detected for ixl\d+, disabling LLDP in firmware)`)
	knownMessage("lldpcli", "lldp_daemon_lifecycle", `lldpd should resume operations`)
	knownMessage("eastpect", "packet_capture_interface_mode", `nm\d+::[\w.-]+(?::\d+|\^): permanently promiscuous mode enabled`)
	knownMessage("unbound", "resolver_stats_dump", `\[\d+:\d+\] info: \[25%\]=\d+(?:\.\d+)? median\[50%\]=\d+(?:\.\d+)? \[75%\]=\d+(?:\.\d+)?`)
	initUpgradeConsoleRules()
}

func initUpgradeConsoleRules() {
	const prefix = `(?:<\d+>)?(?:\[\d+\] )?`
	for _, pattern := range []string{
		`certctl: Modified \d+ trust store links?\.`,
		`Writing trust bundles\.\.\.done\.`,
		`Setting hostname: [\w.-]+`,
		`Generating /etc/[\w.-]+\.\.\.done\.`,
		`Configuring (?:system logging|hardware interfaces|tailscale interface|zenoverlay interface|LAN interface|OpenSSH)\.\.\.done\.`,
		`Configuring firewall\.{2,7}(?:done\.)?`,
		`Configuring [\w.-]+ interface\.\.\.`,
		`Starting (?:web GUI|router advertisement service|Dnsmasq|Unbound DNS|UPnP IGD & PCP/NAT-PMP service|NTP service)\.\.\.done\.`,
		`Setting up (?:routes|gateway monitor)\.\.\.done\.`,
		`Syncing OpenVPN settings\.\.\.done\.`,
		`Sync KEA DHCP config\.\.\.done\.`,
		`Configuring IPsec VPN\.\.\.done\.`,
		`\.{5}done\.`,
		`>>> Invoking start script '[\w.-]+'`,
		`Reconfiguring IPv[46] on [\w.-]+`,
		`setup [\w.-]+(?: \[egress only\])?`,
		`Starting (?:kea|tailscaled|alloy|senpai|eastpect|ddclient_opn|elasticsearch|snmpd|acme_http_challenge|lldpd|unbound_exporter)\.`,
		`INFO/[\w.-]+: Starting /usr/local/sbin/kea-dhcp[46] -c /[\w/.-]+`,
		`/dev/[\w]+: \d+\.\d+MB \(\d+ sectors\) block size \d+, fragment size \d+`,
		`\tusing \d+ cylinder groups of \d+\.\d+MB, \d+ blks, \d+ inodes\.`,
		`\twith soft updates`,
		`super-block backups \(for fsck_ffs -b #\) at:`,
		` \d+(?:, \d+)+`,
		`Service \x60sysctl' has been restarted\.`,
		`load feed [\w.-]+ \[\d+ addresses added\.\]`,
		`Root file system: zroot/[\w/-]+`,
		`(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) +\d+ \d{2}:\d{2}:\d{2} [A-Z]+ \d{4}`,
		`\*\*\* [\w.-]+: OPNsense [\w.+-]+ \(amd64\) \*\*\*`,
		` +[\w.-]+ \([\w.-]+\) +-> v[46](?:/(?:DHCP[46]|SLAAC|PPPoE))?: [\da-fA-F:.]+(?:%[\w.-]+)?/\d+`,
		` +v6/(?:DHCP6|SLAAC|t6): [\da-fA-F:.]+(?:%[\w.-]+)?/\d+`,
		`OK`,
	} {
		knownMessage("kernel", "boot_configuration_progress", prefix+pattern)
	}
	// Warnings and diagnostics are kept structured and visible, not inventory.
	capturedEvent("kernel", "interface_promiscuous_enabled", prefix+`([\w.-]+): permanently promiscuous mode enabled`, "interface")
	capturedEvent("kernel", "listen_queue_overflow", prefix+`sonewconn: pcb (0x[\da-f]+) \(([^\r\n]+)\): Listen queue overflow: (\d+) already in queue awaiting acceptance \((\d+) occurrences\), euid (\d+), rgid (\d+), jail (\d+)`, "socket.pcb", "socket.description", "socket.pending", "socket.overflow_occurrences", "process.euid", "process.rgid", "process.jail_id")
	capturedEvent("kernel", "advertised_routes_ignored", prefix+`(Some peers are advertising routes but --accept-routes is false)`, "error.message")
	capturedEvent("kernel", "java_home_ignored", prefix+`warning: ignoring JAVA_HOME=(/[\w/.-]+); using ES_JAVA_HOME`, "java.home.ignored")
	capturedEvent("kernel", "netmap_interface_configuration", prefix+`\d+\.\d+ \[ *\d+\] iflib_netmap_config +txr (\d+) rxr (\d+) txd (\d+) rxd (\d+) rbufsz (\d+)`, "netmap.tx_rings", "netmap.rx_rings", "netmap.tx_descriptors", "netmap.rx_descriptors", "netmap.receive_buffer_size")
	// Elasticsearch's complete console envelope is structured, not an INFO-level
	// exemption. All messages (including ERROR) retain logger, level and diagnostic.
	capturedEvent("kernel", "elasticsearch_log", prefix+`\[(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2},\d{3})\]\[(INFO |WARN |ERROR|DEBUG|TRACE)\]\[([\w.$]+) *\] \[([^\]\r\n]+)\] ([^\r\n]+)`, "elasticsearch.timestamp", "elasticsearch.level", "log.logger", "elasticsearch.node", "elasticsearch.message")
}

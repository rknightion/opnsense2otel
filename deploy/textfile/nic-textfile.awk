# A deliberately narrow driver contract, not a dump of arbitrary sysctl keys.
function metric(name, labels, value) { print name "{" labels "} " value }
function escape(value) { gsub(/\\/, "\\\\", value); gsub(/"/, "\\\"", value); return value }
function fail(message) { print message > "/dev/stderr"; bad = 1 }
BEGIN {
    prefix = "opnsense_textfile_"
    config["admode"] = config["fwd"] = config["no_pendintr"] = 1
    config["no_timestamp"] = config["generic_hwcsum"] = config["iflib_crcstrip"] = 1
    counters["iflib_rx_miss"] = counters["iflib_rx_miss_bufs"] = 1
}
FILENAME == ARGV[1] {
    colon = index($0, ":")
    if (!colon) next
    key = substr($0, 1, colon - 1)
    value = substr($0, colon + 1)
    sub(/^[ \t]+/, "", value); sub(/[ \t]+$/, "", value)
    n = split(key, part, ".")
    if (n >= 5 && part[1] == "dev" && part[2] ~ /^[a-z][a-z0-9_]*$/ && part[3] ~ /^[0-9]+$/ && part[4] == "pf") {
        device = part[2] part[3]
        if (n == 5 && part[5] == "rx_discards") {
            if (value !~ /^[0-9]+$/) { fail("Invalid NIC discard counter"); next }
            if (!(device in discards) && ++nic_count > 64) { fail("NIC cardinality limit exceeded"); next }
            discards[device] = value; driver[device] = part[2]
        } else if (n == 6 && part[5] ~ /^rxq[0-9][0-9][0-9]*$/ && (part[6] == "packets" || part[6] == "irqs")) {
            queue = substr(part[5], 4)
            if (queue + 0 >= 128) next
            if (value !~ /^[0-9]+$/) { fail("Invalid RX queue counter"); next }
            queues[device SUBSEP queue SUBSEP part[6]] = value
        }
    }
    if (n == 3 && part[1] == "dev" && part[2] == "netmap" && (part[3] in config || part[3] in counters)) {
        if (value !~ /^[0-9]+$/) { fail("Invalid netmap value"); next }
        netmap[part[3]] = value
    }
    next
}
FILENAME == ARGV[2] {
    if ($1 == "interrupt" || $1 == "Total" || NF == 0) next
    if (NF < 3 || $(NF-1) !~ /^[0-9]+$/ || $NF !~ /^[0-9]+$/) { fail("Invalid vmstat interrupt row"); next }
    count = $(NF-1)
    label = $1
    for (i = 2; i < NF-1; i++) label = label " " $i
    if (label in interrupts) { fail("Duplicate vmstat interrupt row"); next }
    if (++interrupt_count > 1024) { fail("Interrupt cardinality limit exceeded"); next }
    interrupts[label] = count
}
END {
    if (bad || interrupt_count == 0) exit 1
    print "# HELP " prefix "nic_receive_discards_total Driver physical NIC receive discards; absent on unsupported drivers."
    print "# TYPE " prefix "nic_receive_discards_total counter"
    for (device in discards) metric(prefix "nic_receive_discards_total", "device=\"" device "\",driver=\"" driver[device] "\"", discards[device])
    print "# TYPE " prefix "nic_rx_queue_packets_total counter"
    print "# TYPE " prefix "nic_rx_queue_interrupts_total counter"
    for (key in queues) {
        split(key, parts, SUBSEP); device = parts[1]
        if (!(device in discards)) continue
        name = parts[3] == "packets" ? "nic_rx_queue_packets_total" : "nic_rx_queue_interrupts_total"
        metric(prefix name, "device=\"" device "\",driver=\"" driver[device] "\",queue=\"" parts[2] "\"", queues[key])
    }
    print "# TYPE " prefix "interrupts_total counter"
    for (label in interrupts) metric(prefix "interrupts_total", "interrupt=\"" escape(label) "\"", interrupts[label])
    print "# TYPE " prefix "netmap_info gauge"
    print "# TYPE " prefix "netmap_iflib_rx_miss_total counter"
    print "# TYPE " prefix "netmap_iflib_rx_miss_bufs_total counter"
    for (key in netmap) {
        if (key in config) metric(prefix "netmap_info", "setting=\"" key "\",value=\"" netmap[key] "\"", 1)
        else print prefix "netmap_" key "_total " netmap[key]
    }
    print "# TYPE " prefix "last_success_timestamp_seconds gauge"
    print prefix "last_success_timestamp_seconds " now
}

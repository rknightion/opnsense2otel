---
title: API-Absent Telemetry
description: Why the exporter will never ship an SSH channel, and how to cover the handful of signals only available over shell
tags:
  - OPNsense
  - development
---

# API-Absent Telemetry

A recurring request is an SSH-based channel for the small set of signals OPNsense's REST API
does not expose at all, chiefly ZFS pool health and CARP demotion state. This was spiked
end-to-end in [#225](https://github.com/rknightion/opnsense2otel/issues/225) against a real
26.7-devel box. **Verdict: no.** This page is the permanent record of why, and the recipe to get
these signals without the exporter ever holding a shell credential.

## Why the exporter will never dial SSH

The blocker is not parser fragility, it is OPNsense's privilege model: **there is no unprivileged
shell tier to grant a credential into.**

`/usr/local/etc/inc/auth.inc` decides a user's login shell with one line:

```php
$user_shell = $is_admin && !empty($user['shell']) ? $user['shell'] : '/usr/sbin/nologin';
```

Any account without the full `page-all` admin privilege is force-set to shell `/usr/sbin/nologin`
and group `nobody`, regardless of what shell the GUI's user editor claims to offer. There is no
separate "shell access" privilege distinct from full admin. On top of that, the generated `sshd_config`
(`plugins.inc.d/openssh.inc:171`) hardcodes `AllowGroups wheel` unconditionally, so even a
`nologin`-shelled account that somehow reached sshd would be refused at the group check.

The practical consequence: **every credential capable of running one SSH command against an
OPNsense box is a full-admin, effectively root-equivalent credential.** A dedicated, read-only,
non-wheel SSH user (the shape of thing a scraper credential should be) does not exist in stock
OPNsense. Building one would mean unmanaged `sshd_config.d` drop-ins and a boot-hook user
creation outside `config.xml`, which is not upgrade-safe and not something this project will ship
as a default for every install.

Given that, embedding an SSH client in the exporter would trade a scoped API-key credential for a
root-equivalent one, add a reconnect/session state machine, and add parsers with no contract-canary
safety net (see [API Landmines](api-landmines.md) for what that safety net normally buys us). All
that to unlock two metrics. Not a good trade. Closed as a documentation deliverable instead.

## Use os-node_exporter for OS-level telemetry

Most of what an SSH channel would have chased is already covered by installing the
**os-node_exporter** plugin on the firewall itself and scraping it as a second Prometheus target
(the exporter and node_exporter are complementary, not overlapping; see
[Integration & Dashboards](../integration-dashboards.md)). Its GUI collector toggles
(Services → Node Exporter) map straight onto the metrics families the spike was chasing:

| Toggle | Covers |
| --- | --- |
| `devstat` | Per-disk I/O: read/write bytes, ops, busy time (the same data `iostat -x` would give an SSH-based collector, without a fixed-column parser to keep in sync with FreeBSD releases). |
| `interrupts` | Per-CPU interrupt counters. |
| `zfs` | ZFS ARC size and hit/miss counters (`kstat.zfs.misc.arcstats.*`). |
| `cpu`, `meminfo`, `loadavg`, `filesystem`, `netdev`, `time`, `ntp` | Standard OS-level coverage; not the reason for this spike but worth enabling alongside the above. |

node_exporter does **not** cover ZFS pool health (degraded/faulted vdevs) or CARP demotion state.
Those two were the API-absent signals confirmed by the original spike. Physical NIC driver
telemetry has a supported textfile path below as well.

## Textfile-collector recipe: ZFS pool health + CARP demotion

The os-node_exporter plugin ships node_exporter's textfile collector always on, pointed at a fixed
directory, `/var/tmp/node_exporter` (owned `nobody:nobody`; confirmed against the plugin's
[startup template](https://github.com/opnsense/plugins/blob/master/sysutils/node_exporter/src/opnsense/service/templates/OPNsense/NodeExporter/node_exporter)
and reports from users who hit it). There is no GUI setting for the directory, and none is needed:
any readable `.prom` file dropped there gets picked up on the next scrape automatically.

A root cron job writing one file covers both signals with zero new credentials and zero exporter
code:

```sh
#!/bin/sh
# /usr/local/bin/zfs_carp_textfile.sh
# Root cron: emit ZFS pool health + CARP demotion as a node_exporter textfile metric.
set -eu
DIR=/var/tmp/node_exporter
TMP=$(mktemp "${DIR}/.zfs_carp.prom.XXXXXX")
POOLS=$(zpool list -H -o name 2>/dev/null | wc -l | tr -d ' ')
HEALTHY=0
zpool status -x 2>/dev/null | grep -qx 'all pools are healthy' && HEALTHY=1
DEMOTION=$(sysctl -n net.inet.carp.demotion 2>/dev/null || echo 0)
{
  printf '# TYPE opnsense_textfile_zpool_count gauge\nopnsense_textfile_zpool_count %s\n' "${POOLS}"
  printf '# TYPE opnsense_textfile_zpool_healthy gauge\nopnsense_textfile_zpool_healthy %s\n' "${HEALTHY}"
  printf '# TYPE opnsense_textfile_carp_demotion gauge\nopnsense_textfile_carp_demotion %s\n' "${DEMOTION}"
} > "${TMP}"
chmod 644 "${TMP}"
mv -f "${TMP}" "${DIR}/zfs_carp.prom"
```

Add it via **Services → Cron** (do not hand-edit `/etc/crontab`: OPNsense regenerates it from
`config.xml` on every boot and config write, silently discarding a manual entry) at whatever
interval matches your scrape cadence; every 5 minutes is generally enough for pool health and
CARP demotion, neither of which flaps on a healthy box.

Two things make this safe to run unattended:

- **Atomic write:** node_exporter's textfile collector rejects a file mid-write (a truncated
  `.prom` file is invalid exposition format), so the script writes to a `mktemp` sibling in the
  *same* directory and `mv`s it into place: a rename within one filesystem is atomic, so
  node_exporter only ever sees the file fully formed or not at all.
- **Zero-pools handled correctly:** `zpool list -H -o name` against a box with no pools returns
  empty stdout with exit code 0, so the `wc -l` count is then `0`, which is correct, not an
  error. This is a real gotcha to know if you build your own variant against `zpool list -j`:
  with zero pools it *also* returns empty stdout (not `{}`, nothing at all), while `zpool status
  -j` returns valid, parseable JSON even with zero pools (`{"pools":{}}`). The two `-j` forms are
  not consistent with each other on the empty case. The script above sidesteps the whole question
  by staying on the plain-text forms, which need no JSON parser on the box at all.

## Supported physical NIC textfile telemetry

The shipped [nic-textfile.sh](../../deploy/textfile/nic-textfile.sh) runtime script reads
`sysctl dev` and `vmstat -i` locally as root and atomically publishes `nic.prom` in
`/var/tmp/node_exporter`. Its adjacent [AWK parser](../../deploy/textfile/nic-textfile.awk)
is required, along with OPNsense's `/usr/local/bin/python3`. The exporter still holds only its
API credential; it never opens SSH.

Collection stages in a private directory under sticky `/var/tmp`, outside the potentially
writable textfile directory. Python opens the destination with `O_DIRECTORY|O_NOFOLLOW` and
replaces `nic.prom` relative to that directory descriptor. A destination symlink is replaced
without following it, a directory destination is rejected, and a substituted parent symlink is
never traversed. `/var/tmp` and the output directory must share a filesystem: a cross-filesystem
rename fails and retains the previous output, with no non-atomic copy fallback.

This path covers driver-specific queue counters which the REST interface traffic response does
not expose. It does not mean every physical interface drop is absent from the REST API: compare
`opnsense_interfaces_input_queue_drops_total` for the physical parent separately. The ixl
contract was confirmed with `dev.ixl.N.pf.rx_discards`, `pf.rxqNN.packets` and `pf.rxqNN.irqs`.
The live comparison found the unassigned PPPoE parent omitted from the REST response,
while its textfile discard value matched the driver sysctl exactly. An assigned physical NIC
already exposed matching REST input queue drops. Other drivers are included only when they expose those exact suffixes. Unsupported drivers and
absent queue counters produce no samples, rather than fabricated zeroes.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `opnsense_textfile_nic_receive_discards_total` | `device`, `driver` | Physical NIC driver receive discards, including PPPoE/VLAN parent NICs. |
| `opnsense_textfile_nic_rx_queue_packets_total` | `device`, `driver`, `queue` | Received packets per driver RX queue. |
| `opnsense_textfile_nic_rx_queue_interrupts_total` | `device`, `driver`, `queue` | Interrupts per driver RX queue. |
| `opnsense_textfile_interrupts_total` | `interrupt` | Cumulative vmstat interrupt count; the displayed rate is discarded. |
| `opnsense_textfile_netmap_info` | `setting`, `value` | Numeric configuration for `admode`, `fwd`, `no_pendintr`, `no_timestamp`, `generic_hwcsum`, `iflib_crcstrip`. |
| `opnsense_textfile_netmap_iflib_rx_miss_total` | none | Netmap iflib receive misses. |
| `opnsense_textfile_netmap_iflib_rx_miss_bufs_total` | none | Netmap iflib receive buffer misses. |
| `opnsense_textfile_last_success_timestamp_seconds` | none | Unix time of the last complete collection. |

Changing netmap counters never appear in labels. The small configuration allowlist avoids a
blanket `dev.netmap.*` label dump; unknown keys are ignored. Limits are 64 supported NICs,
queue indices 0 through 127 per NIC, and 1024 interrupt sources. NIC/interrupt overflow or malformed
selected numeric values aborts publication. Queue indices above 127 are intentionally omitted.
Counters can reset on reboot or driver reload; use `rate` or `increase`, not subtraction.

Install both files together under `/conf/opnsense2otel/`, owned by root and not writable by other
users, and make the shell script executable. Keep an off-box copy of these files: an ordinary
`config.xml` backup does not contain arbitrary custom scripts. `/var/tmp/node_exporter` is output,
not the installation location. Install `os-node_exporter` first so it creates that directory.
Run `/conf/opnsense2otel/nic-textfile.sh` manually once and inspect `nic.prom` before scheduling.

Create `/usr/local/opnsense/service/conf/actions.d/actions_opn2otel.conf`, owned by root,
with this fixed command (no caller-supplied arguments):

```ini
[nic]
command:/conf/opnsense2otel/nic-textfile.sh
parameters:
type:script
message:Collect physical NIC textfile telemetry
description:Collect physical NIC textfile telemetry
```

Reload configd with `service configd restart`, then select **Collect physical NIC textfile
telemetry** in **System → Settings → Cron**. The action executes as root through configd;
the generated cron entry itself need not run as root. The [configd action contract](https://docs.opnsense.org/development/backend/configd.html)
uses `description` to make an action selectable in the GUI. A GUI Cron entry is stored in `config.xml`; editing `/etc/crontab` directly is not
persistent. Custom configd registrations are separately installed files and must be backed up and
reinstalled if an upgrade replaces them. Keeping the runtime in `/conf` avoids installing it into
a package-owned script location, but does not replace that backup and post-upgrade verification.
Run the script once and check the freshness metric after each firmware upgrade.

Run at least every minute for this hardware telemetry. Cron's minute resolution cannot reliably
show the shape of a ten-second burst, but a cumulative discard counter retains its volume. Use
`increase(opnsense_textfile_nic_receive_discards_total[5m])` for burst volume and
`rate(opnsense_textfile_nic_rx_queue_packets_total[5m])` grouped by device and queue to identify
traffic concentrated on one RX queue. Scrape node_exporter as a separate target, with the same
`opnsense_instance` label used for this firewall's exporter target, so dashboard filtering can
join the two sources without copying credentials.

A failed source command leaves the previous complete file intact and returns nonzero. Alert on
`time() - opnsense_textfile_last_success_timestamp_seconds > 180` at a one-minute collection
cadence, plus `absent(opnsense_textfile_last_success_timestamp_seconds)` for a never-published
file. The old counters alone cannot distinguish a failed cron job from an idle NIC. The script
never resets a failed read to zero. Overlapping cron runs should be avoided; a minute schedule
is sufficient for these local commands.

Fixture verification: `just textfile-test` runs the shipped command with
mocked FreeBSD subprocesses and verifies successful publication and preservation on read failure.

## Optional Zenarmor daily engine health

This is an **optional on-firewall textfile collector**, separate from both opnsense2otel and
its Zenarmor flow receiver. It is not enabled by upgrading the exporter, enabling flow shipping,
or installing the NIC script. Install and schedule it only if you want engine crash/stall counts.
It uses no API keys, SSH credentials or network calls. The exporter itself still never uses SSH.

The [zenarmor-textfile.py](../../deploy/textfile/zenarmor-textfile.py) command reads plain
`main_*` logs under `/usr/local/zenarmor/log/active` and `/usr/local/zenarmor/log/archive`.
It follows the installed plugin's `SystemUtils::getEngineCrashStallCount()` convention: count
lines containing the firewall's local `YYYY-MM-DD` date and `terminated with signal` (crashes)
or `heartbeat for` (stalls). A line containing both phrases contributes once to each gauge.
These are observed log counts, not a diagnosis that the engine is currently unhealthy.

Do not invoke `check_health.sh` or the Health API to collect these values. The script can stop
and disable services, and the loopback-only API invokes `check()->send()`. This companion only
reads files and writes its own textfile output.

### Install and schedule

1. Install and enable **os-node_exporter** on the firewall. Verify its textfile collector reads
   `/var/tmp/node_exporter`. Restrict access to the node_exporter listener to your monitoring
   system; do not expose it to the Internet.
2. From a checked-out release of this repository, copy `deploy/textfile/zenarmor-textfile.py`
   to `/conf/opnsense2otel/zenarmor-textfile.py` on the firewall. Create the directory if needed;
   it and the script must be root-owned and not writable by other users. Python 3 at
   `/usr/local/bin/python3` is required; no additional Python packages are needed.
3. As root, run:

   ```sh
   /usr/local/bin/python3 /conf/opnsense2otel/zenarmor-textfile.py
   cat /var/tmp/node_exporter/zenarmor.prom
   ```

   Expect `opnsense_textfile_zenarmor_collection_success 1` and both daily gauges. Zero counts
   are valid only after a successful scan of available logs. A failed scan exits nonzero and
   publishes collection health without count gauges; diagnose it before scheduling.
4. Add the following section to
   `/usr/local/opnsense/service/conf/actions.d/actions_opn2otel.conf`. Preserve an existing
   `[nic]` section if you installed NIC telemetry; the two actions are independent.

   ```ini
   [zenarmor]
   command:/usr/local/bin/python3 /conf/opnsense2otel/zenarmor-textfile.py
   parameters:
   type:script
   message:Collect Zenarmor daily engine health
   description:Collect Zenarmor daily engine health
   ```

5. Run `service configd restart`, then `configctl opn2otel zenarmor` to verify the registered
   action. In **System → Settings → Cron**, select **Collect Zenarmor daily engine health**
   and schedule it every minute. Configd executes the action as root; do not edit the generated
   crontab. Ensure there is only one scheduled instance of this action.

Keep off-box copies of the script and configd action. A `config.xml` backup includes the GUI
schedule but does not include these custom files. After a firmware or Zenarmor upgrade, verify
that the action remains installed, the log format still matches, and collection health is fresh.
Reinstall custom files if needed. This is not a packaged OPNsense plugin with automatic upgrades.

### Scrape and query

Scrape **node_exporter**, not opnsense2otel's `/metrics`, to receive these metrics. Use your
existing Prometheus-compatible scraper and send the samples to the same metrics backend if you
want to correlate them with exporter data. For example, replace these placeholders with your
node_exporter address and the exact `opnsense_instance` value on the exporter's existing series:

```yaml
scrape_configs:
  - job_name: opnsense-node
    scrape_interval: 30s
    static_configs:
      - targets: ['FIREWALL_NODE_EXPORTER_HOST:9100']
        labels:
          opnsense_instance: 'MATCH_EXISTING_EXPORTER_INSTANCE'
```

Use the listener port configured on your firewall. Add this target to an existing job if it is
already scraped; do not duplicate it. No new opnsense2otel flags or OTLP credentials are required.
Verify these samples at node_exporter's endpoint and then in your backend:

| Metric | Meaning |
| --- | --- |
| `opnsense_textfile_zenarmor_crashes_today` | Matching crash lines for the current local day. |
| `opnsense_textfile_zenarmor_stalls_today` | Matching stall lines for the current local day. |
| `opnsense_textfile_zenarmor_collection_success` | 1 for a complete scan; 0 for unavailable, changing or over-limit inputs. |
| `opnsense_textfile_zenarmor_collection_timestamp_seconds` | Time of the published collection attempt, including failures. |
| `opnsense_textfile_zenarmor_day_start_timestamp_seconds` | Local midnight for a successful scan's calendar day. |

All five metrics are gauges with no source-derived labels. The scrape job adds instance labels.
Query the daily counts directly, for example:

```promql
opnsense_textfile_zenarmor_crashes_today{opnsense_instance="MATCH_EXISTING_EXPORTER_INSTANCE"}
opnsense_textfile_zenarmor_stalls_today{opnsense_instance="MATCH_EXISTING_EXPORTER_INSTANCE"}
```

**Do not use `rate()` or `increase()` on these daily gauges.** Counts reset at local midnight,
can decrease if logs are removed, and are not lifetime event totals. Polling near midnight can
miss the previous day's final events. Rotation normally preserves counts while files remain in
active/archive; hard links to the same file are counted once, but copied log contents can be counted twice, as in the plugin's grep-based
implementation. A selected compressed log makes the scan unavailable; compressed logs are not silently omitted. These metrics are not a durable event history.
No Zenarmor-health dashboard panel or alert is automatically installed by this optional script;
use these queries in your own panels.

Monitor collection separately from counts. For a one-minute schedule, inspect:

```promql
opnsense_textfile_zenarmor_collection_success == 0
time() - opnsense_textfile_zenarmor_collection_timestamp_seconds > 180
absent(opnsense_textfile_zenarmor_collection_success{opnsense_instance="MATCH_EXISTING_EXPORTER_INSTANCE"})
```

Also monitor node_exporter target `up` and `node_textfile_scrape_error`. A publication failure
leaves the previous file intact, so freshness catches failures that cannot publish success=0.
An exporter restart alone does not refresh this file. Check the day-start gauge around midnight
if a stale previous-day count could be mistaken for today's value.

### Limits, failures and disabling

The scan is bounded by file count, total bytes, directory entries, line length and elapsed time.
Defaults are 128 matching files, 64 MiB total input, 4096 directory entries across both
directories, 1 MiB per line and 10 seconds elapsed. `--max-files` and `--max-bytes` accept
explicit positive limits; the other caps are fixed. Run `/usr/local/bin/python3 /conf/opnsense2otel/zenarmor-textfile.py --help` for usage.
Both active and archive directories must be readable and at least one matching log must exist. Missing logs,
unreadable files, detected rotation during a scan and exceeded limits are unavailable data,
not zero events. Retry on the next schedule; do not silently truncate the scan to force success.
Only plain regular files are accepted; selected symlinks and special files are rejected.

Publication uses private staging under `/var/tmp` and an atomic replacement anchored to an
opened output-directory descriptor. The output directory must share a filesystem with
`/var/tmp`; cross-filesystem publication fails safely. An output-directory symlink is rejected.
The default output is `/var/tmp/node_exporter/zenarmor.prom`. `--log-root` and `--output-dir`
allow explicit alternative locations; they do not grant extra permissions.

To disable this functionality, disable its GUI Cron entry first. Move `zenarmor.prom` to a
backup location outside the textfile directory so node_exporter stops serving stale samples.
The NIC companion and normal exporter collection continue independently. Keep or archive the
script and configd action for future re-enablement.

## Deliberately out of scope

- **`dev.cpu.N.freq`** is a real-hardware-only signal: there is no cpufreq driver under
  virtualization (confirmed absent on a KVM-hosted devel box), so it would read absent on a
  large fraction of installs and isn't worth a collector or a textfile metric either.

## See also

- [API Landmines](api-landmines.md) - the same "verified against source, not guessed at" treatment
  for endpoints inside the OPNsense REST API itself.
- [Compatibility](../compatibility.md#how-drift-is-caught) - the automated canaries that keep the
  in-API surface honest; there is no equivalent canary for this page's shell commands, so treat
  the recipe as a starting point to adapt, not a pinned contract.

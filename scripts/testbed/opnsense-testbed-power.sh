#!/usr/bin/env bash
# OPNsense testbed power scheduler for `oli` (#625).
#
# The six testbed guests are only needed for on-demand live validation, but
# they would otherwise run 24/7 costing ~0.38 of a core and 24 GB of allocated
# RAM. The main session raises them with `up <hold>` when it needs a live API
# surface and takes them down after; opnsense-testbed-down.timer is the daily
# backstop for a lab left running.
#
# WHO RAISES THE LAB: a session does, by hand, and OPN-0109 has NOT changed that
# yet. The intent is for live-canary.yml to own the cycle over a restricted CI
# credential, and the work is parked on a live finding rather than a design
# argument: TAILSCALE SSH INTERCEPTS PORT 22 ON THIS HOST, so sshd's
# authorized_keys never runs for a connection over the tailnet. A forced
# command is simply bypassed - verified 2026-09-20, `ssh -v` reports
# `Authenticated to ... using "none"` and lands in a full shell with the
# `command="...",restrict` line present and correct. Tailscale SSH has no
# per-command restriction, so it cannot be the boundary CI needs. Do not wire
# CI to this host over :22 believing a forced command will hold it.
#
# The three objections this header used to raise against CI-owned power still
# have answers, and they are worth keeping because none of them is the blocker:
# a forced command needs only :22 and grants far less than DEVBOX_API_KEY, which
# CI already holds; a cancelled run is bounded by the hold plus the watchdog,
# not left indeterminate; and `up` blocks on readiness rather than a clock, so a
# CI cycle is gated the same way a manual one is. The real cost of CI-owned
# power is thinner tables on a box that has been up for minutes rather than
# hours, and that is worth naming honestly.
#
# ORDERING IS LOAD-BEARING. Every guest except the two firewalls depends on a
# firewall for DHCP, routing and DNS, so the firewalls start first and stop
# last. Start a client before its firewall and it comes up with no lease — the
# canary then reads an empty box and files it as drift.
set -euo pipefail

# ---------------------------------------------------------------------------
# Inventory. Phase 1 must be READY before phase 2 starts.
#
# THE ALLOWLIST IS A SAFETY BOUNDARY, NOT A CONVENIENCE. oli also runs 100
# (home assistant), 101 (the CI runners), 103 (unifi-os), 104 (winsrv) and 107
# (postgres). Powering any of those off is a real outage, so every qm/pct call
# below is gated on assert_allowed and the ids are hardcoded — never derived
# from `qm list` or an argument.
# ---------------------------------------------------------------------------
PHASE1_VMS=(102 106)            # firewalls: nightly, release
PHASE2_CTS=(105)                # traffgen LXC, multi-homed onto both firewalls

# RETIRED BY OPN-0110 and deliberately NOT an allowlist entry. 110 was the
# nightly FRR/PD peer, 111 the release client, 112 the nightly client. The lab
# was two parallel stacks because the canary probed both targets as a matrix;
# it now runs the profiles sequentially against the one traffgen container, so
# the second client of each pair had nothing left to do. Retiring them frees 6
# cores, 8.5 GB RAM and 34.4 GB of disk.
#
# The ids live here rather than in a comment so that re-adding one is a
# deliberate edit to the allowlist and not a plausible-looking line in a list
# that already mentions them. is_allowed does NOT read this array: while these
# ids are here, the script refuses to touch those guests at all, which is the
# point - they stay DEFINED and STOPPED on oli until Rob confirms deletion
# separately, and a script that could still stop them could still start them.
#
# What was lost is recorded where it can be read from a run rather than from
# this comment: the affected coverage entries in
# opnsense/testdata/schemas/coverage.json carry a RETIRED note, so the canary
# names each one in its report instead of reading clean.
RETIRED_VMS=(110 111 112)

# Readiness gate. oli reaches both boxes' webConfigurator from its 10.0.0.6 LAN
# address (the MGMT_SRC alias covers 10.0.0.0/24), verified 200 on both.
READY_URLS=(https://10.0.90.111/ https://10.0.90.119/)

HOLD_FILE=/var/lib/opnsense-testbed/hold
# The hold file says "someone intends to keep the lab up until T". That is a
# lifecycle expiry, NOT mutual exclusion: `down` can read the hold as free and
# then be overtaken by an `update` that takes one a moment later, and both then
# proceed. This lock is the exclusion, and every lifecycle verb takes it.
LOCK_FILE=/var/lib/opnsense-testbed/lock
DEFAULT_HOLD_SECONDS=28800      # 8h — one working day, then it lapses by itself
READY_TIMEOUT=600               # 10 min; the canary dispatch waits on this unit
SHUTDOWN_TIMEOUT=180            # per guest, before falling back to a hard stop
ADDRESS_TIMEOUT=90              # per container interface, before remediating
GUEST_EXEC_TIMEOUT=300          # qm guest-agent command timeout

# DHCP interfaces the traffgen container must hold before the lab counts as up.
# eth0 is its TESTLAN address (leased by VM 102, and the jump path to both
# firewalls); eth1 is VLAN 90. eth2 is CPORTAL, whose captive portal means a
# lease there is not required and not waited for.
CT_REQUIRED_IFACES=(eth0 eth1)

# Both firewalls' TESTLAN addresses. 102 and 106 each put an UNTAGGED interface
# on vmbr9, so TESTLAN is ONE broadcast domain shared by both boxes rather than
# a segment per firewall - which is what makes a single traffgen container able
# to serve both profiles, and why OPN-0110 could retire the second client
# instead of re-homing it.
#
# The gate below asserts 105 can actually reach both. Holding a lease on eth0
# is NOT the same claim: a lease proves Kea answered, and the canary's failure
# mode here is the box being up and unreachable, which reads as an empty table
# rather than an error. Since the profiles now run one after the other against
# this one container, an unreachable firewall would let the FIRST profile probe
# happily and silently hollow out the second.
FIREWALL_TESTLAN_ADDRS=(172.16.9.1 172.16.9.2)

log() { printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"; }
die() { log "ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Pure decision logic. Kept free of Proxmox, network and filesystem writes so
# scripts/testbed/testbed_power_test.py can drive it through --decide-only
# below without a host, root or qm.
# ---------------------------------------------------------------------------

# is_allowed exits 0 only for an EXACT id match. A substring or glob match here
# would be catastrophic in both directions: "10" must not match guest 100, and
# "1020" must not be mistaken for 102.
is_allowed() {
  local candidate=${1-} id
  for id in "${PHASE1_VMS[@]}" "${PHASE2_CTS[@]}"; do
    [ "$candidate" = "$id" ] && return 0
  done
  return 1
}

# is_retired exits 0 for a guest OPN-0110 took out of the lab. Purely so the
# refusal can say WHICH kind of no it is: a retired id is a guest that still
# exists on oli and that this script must not touch, which is a different
# situation from a typo or from guest 100, and a caller that hits it is usually
# running an older command line rather than making a mistake.
is_retired() {
  local candidate=${1-} id
  for id in "${RETIRED_VMS[@]}"; do
    [ "$candidate" = "$id" ] && return 0
  done
  return 1
}

# guest_order prints the ids for a direction, in the order they must be acted
# on. down is the exact reverse of up.
guest_order() {
  local direction=$1 up=("${PHASE1_VMS[@]}" "${PHASE2_CTS[@]}")
  case "$direction" in
    up) printf '%s\n' "${up[@]}" ;;
    down)
      local i
      for ((i = ${#up[@]} - 1; i >= 0; i--)); do printf '%s\n' "${up[$i]}"; done
      ;;
    *) die "guest_order: unknown direction '$direction'" ;;
  esac
}

# hold_is_live decides whether a scheduled shutdown must be skipped.
#
# A malformed or empty hold file FAILS OPEN to "free" on purpose. The two
# failure modes are not symmetric: a stuck-on hold silently disables the whole
# feature and nobody notices for months, whereas a shutdown during ad-hoc work
# is immediately obvious and `testbed up` undoes it in ~3 minutes. So garbage
# means proceed, loudly.
hold_is_live() {
  local file=$1 now=$2 expiry
  case "$now" in '' | *[!0-9]*) die "hold_is_live: '$now' is not an epoch" ;; esac
  [ -f "$file" ] || return 1
  expiry=$(head -n1 "$file" 2>/dev/null | tr -d '[:space:]')
  case "$expiry" in
    '' | *[!0-9]*)
      log "WARNING: hold file '$file' is malformed ('$expiry'); ignoring it" >&2
      return 1
      ;;
  esac
  # Expiry exactly now counts as over, not still running.
  [ "$now" -lt "$expiry" ]
}

# update_verdict reads a firmware check result and says what to do with it.
#
# The input is /tmp/pkg_upgrade.json, which `configctl firmware probe` writes.
# That file is the only machine-readable view of a check: `configctl firmware
# status` prints a human changelog, and the JSON API needs credentials that
# deliberately do not exist on this host.
#
# Verdicts:
#   pending  — packages to install, upgrade, downgrade, reinstall or remove
#   current  — the box is already at its channel head
#   unusable — the check could not reach the mirror, so ABSENCE OF PENDING
#              WORK PROVES NOTHING. This is the case that must never be read as
#              "current": a box with no route to the mirror reports empty
#              package lists and looks exactly like an up-to-date one.
#
# Reads connection/repository first for that reason, before looking at any list.
update_verdict() {
  local file=$1
  [ -f "$file" ] || { echo unusable; return; }
  python3 - "$file" <<'PYEOF'
import json, sys

try:
    with open(sys.argv[1]) as handle:
        check = json.load(handle)
except (OSError, ValueError):
    print("unusable")
    sys.exit(0)

# A failed fetch still writes the file, with these set to something other than
# "ok". Empty package lists under a failed fetch mean "we could not look", not
# "nothing to do".
if check.get("connection") != "ok" or check.get("repository") != "ok":
    print("unusable")
    sys.exit(0)

work = (
    "new_packages",
    "upgrade_packages",
    "downgrade_packages",
    "reinstall_packages",
    "remove_packages",
)

# A field that is absent, or not the list it should be, means this payload is
# not the check we think it is - and treating a missing list as an empty one
# would count it towards "nothing to do". Every road to "current" has to be
# paved with fields we actually read.
for key in work:
    if not isinstance(check.get(key), list):
        print("unusable")
        sys.exit(0)

if any(check.get(k) for k in work):
    print("pending")
    sys.exit(0)

# A box with nothing left to install but a pending reboot is NOT at its channel
# head: base and kernel only take effect on boot, so it is still RUNNING the old
# ones. Calling that "current" is how a reboot that silently failed gets
# certified as a completed update - the box keeps serving, the package lists are
# empty, and nothing else in the run would notice.
if str(check.get("needs_reboot", "")).strip().lower() in {"1", "true", "yes"}:
    print("pending")
    sys.exit(0)

print("current")
PYEOF
}

# check_identity prints "<product_id> <product_version>" from a check result, so
# a caller can log what a box was before and after an upgrade. Prints nothing
# when the file is missing or unreadable; a caller must treat that as unknown
# rather than as a version.
check_identity() {
  local file=$1
  [ -f "$file" ] || return 0
  python3 - "$file" <<'PYEOF'
import json, sys

try:
    with open(sys.argv[1]) as handle:
        check = json.load(handle)
except (OSError, ValueError):
    sys.exit(0)

pid = str(check.get("product_id") or "").strip()
version = str(check.get("product_version") or "").strip()
if version:
    print(f"{pid} {version}".strip())
PYEOF
}

# snapshot_name is the pre-update restore point for one guest. ONE name per
# guest, reused every run: Proxmox refuses a duplicate, so the update path
# deletes the previous one first. A timestamped name would silently accumulate
# a snapshot per run until the store filled, and nothing would report it.
snapshot_name() {
  echo "preupdate-$1"
}

# --- Test seam. MUST stay above anything that reads state or touches a guest.
# Production invocations never pass --decide-only.
if [ "${1-}" = "--decide-only" ]; then
  case "${2-}" in
    allowed) if is_allowed "${3-}"; then echo yes; else echo no; fi ;;
    order)   guest_order "${3-}" | tr '\n' ' ' | sed 's/ $//' && echo ;;
    hold)    if hold_is_live "${3-}" "${4-}"; then echo held; else echo free; fi ;;
    verdict) update_verdict "${3-}" ;;
    identity) check_identity "${3-}" ;;
    snapname) snapshot_name "${3-}" ;;
    *)       die "--decide-only: unknown question '${2-}'" ;;
  esac
  exit 0
fi

# ---------------------------------------------------------------------------
# Proxmox interaction
# ---------------------------------------------------------------------------

assert_allowed() {
  is_allowed "$1" && return 0
  if is_retired "$1"; then
    die "refusing to touch guest $1 — retired from the lab by OPN-0110 (it is still defined on oli, and deliberately out of reach here)"
  fi
  die "refusing to touch guest $1 — not in the testbed allowlist"
}

# guest_kind resolves vm-vs-container from the hardcoded lists, NOT by probing
# Proxmox, so a typo can never make the script guess at a prod guest.
guest_kind() {
  local id=$1 ct
  for ct in "${PHASE2_CTS[@]}"; do
    [ "$id" = "$ct" ] && { echo ct; return; }
  done
  echo vm
}

# decode_qm_guest_exec validates and decodes the JSON envelope returned by
# `qm guest exec`. A successful qm process only means that the guest-agent RPC
# completed; the guest command's exitcode is inside this envelope. Keep the
# decoded streams in files so command output remains byte-for-byte intact,
# including missing or multiple trailing newlines. QGA omits empty streams;
# those absent fields become empty output, while present fields must be strings.
decode_qm_guest_exec() {
  local envelope=$1 guest_stdout=$2 guest_stderr=$3 guest_exit=$4

  if ! jq -e '
    def valid_exit:
      if type == "number" then (. == floor and . >= 0 and . <= 255) else false end;
    if type != "object" then false
    else
      has("exitcode")
      and (.exitcode | valid_exit)
      and ((has("out-data") | not) or (.["out-data"] | type == "string"))
      and ((has("err-data") | not) or (.["err-data"] | type == "string"))
    end
  ' "$envelope" >/dev/null 2>&1; then
    return 1
  fi
  if ! jq -j 'if has("out-data") then .["out-data"] else "" end' \
      "$envelope" >"$guest_stdout" 2>/dev/null; then
    return 1
  fi
  if ! jq -j 'if has("err-data") then .["err-data"] else "" end' \
      "$envelope" >"$guest_stderr" 2>/dev/null; then
    return 1
  fi
  if ! jq -er '.exitcode | tostring' "$envelope" >"$guest_exit" 2>/dev/null; then
    return 1
  fi
}

# with_lock re-execs this script under flock(1) so the lifecycle verbs cannot
# overlap. THREE ATTEMPTS AT THIS, and the first two both failed in ways worth
# recording:
#
# 1. `exec 9>lock; flock 9` held the lock on an inherited fd. `qm start` leaves
#    a kvm process per guest that lives as long as the guest, and every one
#    inherited fd 9 - so the lock stayed held for the entire time the lab was
#    up and NOTHING could take it down again. Seen live 2026-09-20: lsof showed
#    fd 9 on the lock file in five kvm processes hours after the script exited.
#    `exec {var}>` was tested on this host and is inherited too; bash cannot
#    mark a redirection close-on-exec.
#
# 2. A PID file fixed the inheritance but carried a TOCTOU window: two callers
#    can both read the same stale PID, both delete it, and the second delete
#    can remove a lock the first has just legitimately taken.
#
# 3. This. `flock --close` keeps the lock in the flock PROCESS and closes the
#    descriptor before exec'ing the command, so no child - however long-lived -
#    can inherit it. The kernel owns the mutual exclusion, and it is released
#    when flock exits however the run ends, including a kill.
#
# -E 8 makes a conflict exit 8, which `down` reports rather than swallowing.
#
# The re-entry marker is an ARGV FLAG, not an environment variable. An earlier
# version used OPNSENSE_TESTBED_LOCKED=1 in the environment, which anyone with a
# shell could export once and then silently run every later invocation with no
# lock at all - and nothing in the output would say so. A flag has to be passed
# deliberately on each call, and it shows up in `ps`.
#
# This is a safety interlock, not a privilege boundary: every caller here is
# already root, so someone determined to skip the lock can simply not use it.
# What matters is that it cannot be disabled BY ACCIDENT, which the ambient
# variable made easy.
with_lock() {
  local wait_seconds=$1
  shift
  # Already inside the lock: run the verb directly rather than nesting.
  [ "${LOCK_REENTERED:-0}" = 1 ] && return 0
  mkdir -p "$(dirname "$LOCK_FILE")"
  exec flock --close --exclusive --wait "$wait_seconds" --conflict-exit-code 8 \
    "$LOCK_FILE" "$0" --locked "$@"
}

cleanup_qm_guest_exec() {
  local directory=$1
  rm -f "$directory/envelope.json" "$directory/qm.stderr" \
    "$directory/guest.stdout" "$directory/guest.stderr" "$directory/guest.exit"
  rmdir "$directory" 2>/dev/null || true
}

# qm_guest_exec returns the guest command's exit code, not qm's RPC status.
# qm's stderr is retained separately so a valid guest err-data stream is
# returned plainly on stderr. A missing, malformed, or timed-out envelope is a
# hard failure and never gets interpreted as a successful guest command.
qm_guest_exec() {
  local id=$1
  shift
  local directory envelope qm_stderr guest_stdout guest_stderr guest_exit
  local qm_status guest_status

  directory=$(mktemp -d "${TMPDIR:-/tmp}/opnsense-qm-exec.XXXXXX") \
    || die "exec: could not create a temporary directory"
  envelope=$directory/envelope.json
  qm_stderr=$directory/qm.stderr
  guest_stdout=$directory/guest.stdout
  guest_stderr=$directory/guest.stderr
  guest_exit=$directory/guest.exit

  if qm guest exec "$id" --timeout "$GUEST_EXEC_TIMEOUT" -- "$@" \
      >"$envelope" 2>"$qm_stderr"; then
    qm_status=0
  else
    qm_status=$?
  fi
  if [ "$qm_status" -ne 0 ]; then
    log "ERROR: exec: qm guest exec for guest $id failed (status $qm_status); no valid JSON envelope" >&2
    cat "$qm_stderr" >&2
    cleanup_qm_guest_exec "$directory"
    return "$qm_status"
  fi

  if ! decode_qm_guest_exec "$envelope" "$guest_stdout" "$guest_stderr" "$guest_exit"; then
    if [ -s "$qm_stderr" ]; then cat "$qm_stderr" >&2; fi
    cleanup_qm_guest_exec "$directory"
    die "exec: guest $id returned no valid JSON envelope (malformed, missing, or timed-out)"
  fi

  cat "$guest_stdout"
  cat "$guest_stderr" >&2
  guest_status=$(cat "$guest_exit")
  cleanup_qm_guest_exec "$directory"
  case "$guest_status" in
    ''|*[!0-9]*) die "exec: guest $id returned an invalid exit code" ;;
  esac
  return "$guest_status"
}

# guest_exec is the one command route for an allowlisted guest. Containers
# expose pct's plain stream and status directly. VMs expose qm's JSON envelope
# only to this decoder, so callers always see guest stdout/stderr and status.
guest_exec() {
  local id=$1
  shift
  if [ "$(guest_kind "$id")" = ct ]; then
    pct exec "$id" -- "$@"
  else
    qm_guest_exec "$id" "$@"
  fi
}

cmd_exec() {
  local id
  [ "$#" -ge 1 ] || die "exec: guest id is required"
  id=$1
  assert_allowed "$id"
  shift
  [ "${1-}" = "--" ] || die "exec: expected '--' before command"
  shift
  [ "$#" -gt 0 ] || die "exec: command is required"
  guest_exec "$id" "$@"
}

cmd_put() {
  local id local_path remote_path
  [ "$#" -ge 1 ] || die "put: guest id is required"
  id=$1
  assert_allowed "$id"
  shift
  [ "$#" -eq 2 ] || die "put: expected <local-path> <remote-path>"
  local_path=$1
  remote_path=$2
  if [ "$(guest_kind "$id")" != ct ]; then
    die "put: refusing VM $id — qm has no guest file-write; use 'exec $id -- fetch -o <tmp> <release-url>' and verify the in-guest sha256 against release checksums.txt"
  fi
  pct push "$id" "$local_path" "$remote_path"
}

guest_state() {
  local id=$1
  if [ "$(guest_kind "$id")" = ct ]; then
    pct status "$id" 2>/dev/null | awk '{print $2}'
  else
    qm status "$id" 2>/dev/null | awk '{print $2}'
  fi
}

start_guest() {
  local id=$1 kind
  assert_allowed "$id"
  kind=$(guest_kind "$id")
  if [ "$(guest_state "$id")" = running ]; then
    log "guest $id already running — nothing to do"
    return 0
  fi
  log "starting $kind $id"
  if [ "$kind" = ct ]; then pct start "$id"; else qm start "$id"; fi
}

# stop_guest shuts down gracefully and only escalates on timeout. OPNsense
# needs a clean shutdown — a hard stop risks the config filesystem, and these
# boxes carry hand-built state that is tedious to rebuild.
stop_guest() {
  local id=$1 kind
  assert_allowed "$id"
  kind=$(guest_kind "$id")
  if [ "$(guest_state "$id")" != running ]; then
    log "guest $id already stopped — nothing to do"
    return 0
  fi
  log "shutting down $kind $id (timeout ${SHUTDOWN_TIMEOUT}s)"
  if [ "$kind" = ct ]; then
    pct shutdown "$id" --timeout "$SHUTDOWN_TIMEOUT" || true
  else
    qm shutdown "$id" --timeout "$SHUTDOWN_TIMEOUT" || true
  fi
  if [ "$(guest_state "$id")" = running ]; then
    log "WARNING: $kind $id ignored ACPI shutdown; forcing stop"
    if [ "$kind" = ct ]; then pct stop "$id"; else qm stop "$id"; fi
  fi
}

# wait_ready blocks until both firewalls serve their webConfigurator. This is
# the gate that matters: a canary that probes a half-booted box files bogus
# drift, which is worse than a late start.
wait_ready() {
  local started=$SECONDS deadline=$((SECONDS + READY_TIMEOUT)) url pending
  log "waiting for firewalls to serve :443 (timeout ${READY_TIMEOUT}s)"
  while [ "$SECONDS" -lt "$deadline" ]; do
    pending=0
    for url in "${READY_URLS[@]}"; do
      curl -sk -o /dev/null -m 6 "$url" || pending=1
    done
    if [ "$pending" -eq 0 ]; then
      log "both firewalls ready after $((SECONDS - started))s"
      return 0
    fi
    sleep 10
  done
  die "firewalls did not serve :443 within ${READY_TIMEOUT}s — the canary will probe a cold box"
}

# ct_iface_address prints a container interface's IPv4 address, empty if none.
ct_iface_address() {
  pct exec "$1" -- ip -4 -br addr show "$2" 2>/dev/null | awk '{print $3}'
}

# settle_containers makes sure the traffgen LXC actually holds its leases.
#
# WHY THIS EXISTS (found by the #625 acceptance test, not theorised): the
# firewalls serve :443 about 31s into boot, but Kea binds UDP/67 later. The VM
# guests ride systemd-networkd/netplan, which retries DHCP indefinitely and
# self-heals. The container rides ifupdown, whose dhclient gives up
# PERMANENTLY after its boot-time attempt — so 105 came up with no address on
# any NIC and no routes, silently. Every collector fed by the traffgen (ARP,
# NDP, states, netflow, unbound, leases) would then read empty and the canary
# would file it as drift.
#
# Waiting longer before starting phase 2 does not fix this, because the race is
# against a signal we cannot observe remotely. Asserting the outcome does.
settle_containers() {
  local id iface addr waited
  for id in "${PHASE2_CTS[@]}"; do
    assert_allowed "$id"
    for iface in "${CT_REQUIRED_IFACES[@]}"; do
      waited=0
      while [ "$waited" -lt "$ADDRESS_TIMEOUT" ]; do
        addr=$(ct_iface_address "$id" "$iface")
        [ -n "$addr" ] && break
        sleep 10
        waited=$((waited + 10))
      done
      if [ -n "$addr" ]; then
        log "ct $id $iface holds $addr"
        continue
      fi
      # dhclient has already given up; a plain retry of the same lease request
      # is what recovers it.
      log "WARNING: ct $id $iface has no address after ${waited}s — re-running dhclient"
      pct exec "$id" -- dhclient -1 "$iface" >/dev/null 2>&1 || true
      addr=$(ct_iface_address "$id" "$iface")
      if [ -n "$addr" ]; then
        log "ct $id $iface recovered with $addr"
      else
        log "ERROR: ct $id $iface still has no address — the traffgen is not feeding the box" >&2
      fi
    done
    assert_client_reaches_firewalls "$id"
    restart_address_dependent_services "$id"
  done
}

# assert_client_reaches_firewalls proves the shared traffgen can talk to BOTH
# boxes before either profile is probed (OPN-0110 AC2).
#
# This is a hard failure, not a warning. The whole consolidation rests on one
# container serving two firewalls in sequence, so a container that reaches only
# one of them produces a full clean report for the profile it can reach and a
# hollow one for the profile it cannot - and a hollow report is the single
# outcome this canary must never produce quietly, because "no rows" and "the
# field was removed upstream" look identical downstream.
assert_client_reaches_firewalls() {
  local id=$1 addr
  assert_allowed "$id"
  for addr in "${FIREWALL_TESTLAN_ADDRS[@]}"; do
    if pct exec "$id" -- ping -c 2 -W 2 "$addr" >/dev/null 2>&1; then
      log "ct $id reaches firewall $addr on TESTLAN"
    else
      die "ct $id cannot reach firewall $addr on TESTLAN — the shared traffgen feeds both profiles, so probing now would report one of them as empty rather than as broken"
    fi
  done
}

# restart_address_dependent_services re-starts the container units that need an
# address at START time, now that settle_containers has guaranteed one.
#
# WHY: shaper-keepalive@ holds a dummynet flow open so trafficShaperStatistics
# items[].flows[] is populated when the canary polls — without it those paths
# read unverified, because dnctl only reports LIVE flows. It starts at boot,
# long before DHCP completes, and was observed crash-looping TWENTY times on
# "Network is unreachable" before the address arrived. systemd's Restart= does
# converge it, so this is not repairing a broken unit — it is removing the race,
# so the flow is established when `up` returns rather than up to a restart
# window later. That margin was ample either way back when the canary ran on a
# 47-minute lead behind a GitHub cron; since #654 there is no lead at all — the
# dispatch fires the moment `up` returns — so "established when `up` returns" is
# now load-bearing rather than a nicety. Making it deterministic also means a
# genuinely broken keepalive shows up here as a failure instead of hiding behind
# a lucky retry.
restart_address_dependent_services() {
  local id=$1 unit
  assert_allowed "$id"
  while read -r unit; do
    [ -n "$unit" ] || continue
    log "restarting $unit on ct $id"
    pct exec "$id" -- systemctl restart "$unit" >/dev/null 2>&1 \
      || log "WARNING: could not restart $unit on ct $id"
  done < <(pct exec "$id" -- systemctl list-units 'shaper-keepalive@*' \
             --state=loaded --no-legend --plain 2>/dev/null | awk '{print $1}')
}

# ensure_dhcp_sockets guarantees Kea is actually SERVING DHCP, not merely
# running (OPN-0114).
#
# THE STATUS COMMAND LIES. `configctl kea status` reports "kea-dhcp[v4] is
# running as pid N" by looking at the PROCESS, and says nothing about sockets.
# A kea-dhcp4 that failed to bind every one of its addresses is still a live
# process, so status reads healthy while the box answers no DHCP at all. This
# check therefore asserts on SOCKSTAT, never on status.
#
# THE CAUSE, measured on both boxes: dnsmasq serves one test VLAN and, because
# DHCP must receive broadcasts, its DHCP socket is ALWAYS the wildcard *:67 -
# `bind-interfaces` is already set and governs only the DNS listener, so there
# is no dnsmasq setting that avoids this. Kea binds per address. On FreeBSD the
# two coexist happily when Kea binds FIRST, and Kea fails every bind when
# dnsmasq gets there first:
#   DHCPSRV_OPEN_SOCKET_FAIL ... address 172.16.9.1, port 67 ... Address already in use
#   DHCP4_OPEN_SOCKETS_FAILED maximum number of open service sockets attempts: 5
# Boot order decides which happens, which is why the traffgen's TESTLAN lease
# was intermittent and why keaLeases coverage moved between runs for no reason
# visible in the report.
#
# The repair is ordering, not configuration: stop dnsmasq, restart Kea so it
# binds into a clear port, then start dnsmasq so its wildcard lands on top.
# Verified live on guest 102 - three per-address binds plus dnsmasq's wildcard,
# all present together.
ensure_dhcp_sockets() {
  local id bound
  for id in "${PHASE1_VMS[@]}"; do
    assert_allowed "$id"
    bound=$(kea_socket_count "$id")
    if [ "$bound" -gt 0 ]; then
      log "vm $id kea holds $bound dhcp socket(s)"
      continue
    fi
    log "vm $id kea bound NO dhcp sockets — restarting it ahead of dnsmasq"
    guest_exec "$id" /bin/sh -c \
      '/usr/local/sbin/configctl dnsmasq stop >/dev/null 2>&1; sleep 2; /usr/local/sbin/configctl kea restart >/dev/null 2>&1; sleep 8; /usr/local/sbin/configctl dnsmasq start >/dev/null 2>&1; sleep 3' \
      >/dev/null 2>&1 || true
    bound=$(kea_socket_count "$id")
    if [ "$bound" -gt 0 ]; then
      log "vm $id kea recovered with $bound dhcp socket(s)"
    else
      # Not fatal: the canary is still worth running, and every kea* path will
      # report as unverified coverage rather than as drift. Failing the raise
      # here would cost the whole run over one collector family.
      log "WARNING: vm $id kea still holds no dhcp sockets — expect every kea lease and subnet path to read unverified" >&2
    fi
  done
}

# kea_socket_count prints how many UDP/67 sockets kea-dhcp4 actually holds.
# Zero is the failure this whole function exists to detect, and it is invisible
# to `configctl kea status`.
kea_socket_count() {
  local id=$1 out
  assert_allowed "$id"
  out=$(guest_exec "$id" /bin/sh -c "sockstat -4 -l | grep -c 'kea-dhcp4.*:67'" 2>/dev/null) || out=0
  out=$(printf '%s' "$out" | tr -dc '0-9')
  printf '%s' "${out:-0}"
}

# warm_firmware_check makes each firewall store an update check.
#
# WHY (found by the #625 acceptance test): OPNsense's firmware check result does
# not survive a reboot. A box that has not checked since boot serves the bare
# three-key envelope, so `firmware` `product.product_check.upgrade_needs_reboot`
# reads as unverified — and that path is REQUIRED coverage, because it is the
# fallback arm of firmwareStatusResponse.upgradeNeedsReboot(). On a box that
# omits the top-level pointer the metric is computed entirely from it.
#
# Under the old always-on regime the box had checked at some point and the path
# was covered. Without this step the daily power cycle would file a canary
# warning issue EVERY morning — turning a clean signal into permanent noise,
# which is exactly how a canary stops being read.
warm_firmware_check() {
  local id
  for id in "${PHASE1_VMS[@]}"; do
    assert_allowed "$id"
    log "running firmware check on vm $id (restores product_check coverage)"
    # Fire-and-tolerate: this is warm-up, not a gate. It runs `pkg update`, so
    # it needs working DNS and WAN — both boxes are acceptDNS=0 precisely
    # because MagicDNS breaks pkg and hangs firmware/info forever.
    if ! qm guest exec "$id" --timeout 240 -- \
        /usr/local/sbin/configctl firmware check >/dev/null 2>&1; then
      log "WARNING: firmware check on vm $id did not complete — expect a product_check coverage warning"
    fi
  done
}

# ---------------------------------------------------------------------------
# Firmware update (OPN-0108)
#
# The boxes were read live on 2026-09-20 and neither had been updated since
# late July - 54 and 56 days - because nothing had ever pulled. "nightly" was a
# name, not a fact, which makes the nightly profile worthless as early warning.
# The update belongs in the session rather than on its own timer for exactly
# that reason: a timer is what rotted unobserved.
# ---------------------------------------------------------------------------

# An upgrade fetches ~350 MiB, installs ~126 packages and reboots, so it needs a
# far longer gate than a cold boot does.
UPDATE_READY_TIMEOUT=1800
# One `opnsense-update -bkp` call: fetch ~350 MiB, then install ~126 packages
# plus base and kernel. Generous, because the cost of being too short is a
# needless rollback of an upgrade that was going to work.
UPDATE_EXEC_TIMEOUT=2400
# How long to allow for the box to START rebooting after the upgrade is
# triggered. `configctl firmware upgrade` daemonises and returns immediately, so
# polling readiness straight away would see the box that has not gone down YET
# and call the upgrade finished.
UPDATE_REBOOT_GRACE=420

# ready_url_for maps a firewall id to its webConfigurator URL. READY_URLS is
# positional against PHASE1_VMS, so this asserts the pairing rather than
# trusting an index to stay aligned as the inventory changes.
ready_url_for() {
  local id=$1 i
  [ "${#PHASE1_VMS[@]}" -eq "${#READY_URLS[@]}" ] \
    || die "ready_url_for: PHASE1_VMS and READY_URLS are out of step"
  for i in "${!PHASE1_VMS[@]}"; do
    if [ "${PHASE1_VMS[$i]}" = "$id" ]; then
      printf '%s\n' "${READY_URLS[$i]}"
      return 0
    fi
  done
  return 1
}

box_serves() {
  curl -sk -o /dev/null -m 6 "$1"
}

# wait_box_ready blocks until ONE firewall serves :443 again.
wait_box_ready() {
  local id=$1 timeout=$2 url started=$SECONDS deadline
  deadline=$((SECONDS + timeout))
  url=$(ready_url_for "$id") || die "wait_box_ready: no ready URL for guest $id"
  while [ "$SECONDS" -lt "$deadline" ]; do
    if box_serves "$url"; then
      log "vm $id serving :443 again after $((SECONDS - started))s"
      return 0
    fi
    sleep 10
  done
  return 1
}

# snapshot_guest takes the pre-update restore point, replacing the previous one.
snapshot_guest() {
  local id=$1 name
  assert_allowed "$id"
  name=$(snapshot_name "$id")
  # Proxmox refuses a duplicate name, and a stale restore point is worse than
  # none - it would roll back to a state two updates old.
  if qm listsnapshot "$id" 2>/dev/null | awk '{print $2}' | grep -qx "$name"; then
    log "removing the previous restore point $name on vm $id"
    qm delsnapshot "$id" "$name" >/dev/null \
      || die "could not remove the stale restore point $name on vm $id"
  fi
  log "taking restore point $name on vm $id"
  qm snapshot "$id" "$name" --description "pre-update, opnsense-testbed-power.sh" >/dev/null \
    || die "could not snapshot vm $id — refusing to update a box with no way back"
}

# rollback_guest restores the pre-update snapshot and brings the box back.
rollback_guest() {
  local id=$1 name
  name=$(snapshot_name "$id")
  log "ERROR: rolling vm $id back to $name" >&2
  if ! qm rollback "$id" "$name" >/dev/null 2>&1; then
    log "ERROR: rollback of vm $id FAILED — the box needs hands" >&2
    return 1
  fi
  start_guest "$id"
  if ! wait_box_ready "$id" "$READY_TIMEOUT"; then
    log "ERROR: vm $id did not come back after rollback — the box needs hands" >&2
    return 1
  fi
  log "vm $id rolled back and serving again"
  return 0
}

# probe_check runs a SYNCHRONOUS firmware check and copies the result off the
# box. `configctl firmware check` daemonises; `probe` is the same launcher call
# in the foreground, which is what makes the result readable by the time this
# returns.
probe_check() {
  local id=$1 dest=$2

  # Empty the destination FIRST, so every failure below leaves the caller with
  # nothing rather than with whatever was there before.
  : >"$dest"

  # Delete the guest's previous result before probing. Without this, a probe
  # that fails leaves the PREVIOUS check on the box and the copy below reads it
  # as though it were fresh - so a pre-upgrade file could be presented as the
  # post-upgrade state, which is precisely the stale-evidence failure this whole
  # task exists to remove. An empty result reads as `unusable`, which is the
  # honest answer when the probe did not run.
  # SUBSHELLS ARE LOAD-BEARING. guest_exec reaches die() on a malformed, missing
  # or timed-out qm envelope, and die exits the SCRIPT - `|| return 0` cannot
  # catch that. Without the subshell, one bad envelope here kills the whole
  # update run, and the worst moment for that is between an upgrade and its
  # verification: a box left upgraded, unverified, and nobody told. Contained,
  # the same envelope just yields an empty result, which reads as unusable.
  if ! ( guest_exec "$id" /bin/rm -f /tmp/pkg_upgrade.json >/dev/null 2>&1 ); then
    log "WARNING: could not clear the previous firmware check on vm $id — reporting currency as unknown"
    return 0
  fi

  if ! qm guest exec "$id" --timeout 300 -- \
      /usr/local/sbin/configctl firmware probe >/dev/null 2>&1; then
    log "WARNING: firmware probe on vm $id did not complete — reporting currency as unknown"
    return 0
  fi

  ( guest_exec "$id" /bin/cat /tmp/pkg_upgrade.json ) >"$dest" 2>/dev/null || : >"$dest"
}

# update_guest brings ONE firewall to its channel head.
#
# EXIT STATUS IS THE OUTCOME, and the four are deliberately not collapsed -
# cmd_update maps them to distinct run exits so "we could not check", "the
# upgrade failed but the box is fine" and "the box may be broken" never read as
# the same event:
#
#   0  at its channel head (already, or after a successful upgrade)
#   1  update failed, and the box was rolled back and is serving again
#   2  the check was unusable BEFORE any upgrade, so currency is UNKNOWN and the
#      box was NOT touched
#   3  ROLLBACK FAILED - the box may be broken and needs hands
#   6  the box WAS upgraded and rebooted but could not be verified afterwards -
#      currency UNKNOWN and someone has to look
update_guest() {
  local id=$1 work before after verdict url
  assert_allowed "$id"
  work=$(mktemp "${TMPDIR:-/tmp}/opnsense-check-XXXXXX.json")
  url=$(ready_url_for "$id") || die "update_guest: no ready URL for guest $id"

  probe_check "$id" "$work"
  verdict=$(update_verdict "$work")
  before=$(check_identity "$work")

  case "$verdict" in
    current)
      log "vm $id is already at its channel head (${before:-version unknown})"
      rm -f "$work"
      return 0
      ;;
    unusable)
      # NOT an update failure and NOT success. The box could not reach its
      # mirror, so "no pending packages" proves nothing - reporting it as
      # current is how a stale box gets certified fresh. Nothing was touched, so
      # this must not read like a failed upgrade either.
      log "ERROR: vm $id firmware check unusable (no mirror route?) — currency UNKNOWN, not confirmed" >&2
      rm -f "$work"
      return 2
      ;;
  esac

  log "vm $id has pending updates (running ${before:-version unknown}) — upgrading"
  snapshot_guest "$id"

  # `opnsense-update -bkp` — base, kernel and packages, which opnsense-update(8)
  # names as the way to "update all currently installed components at once".
  #
  # NOT `configctl firmware upgrade`, which was tried first and SILENTLY DID
  # NOTHING: the action exists and configd accepts it, the call returns 0, and
  # then no upgrade process ever appears, no log is written and the version does
  # not move. It daemonises through `daemon -f`, so there is nothing left to
  # inspect and nothing that reports the failure. A green exit code from it
  # proves only that configd took the message.
  #
  # The RPC's own result is deliberately tolerated rather than trusted. A
  # base/kernel upgrade can take the box down underneath the guest agent, which
  # fails the call for a good reason. Success is decided further down by
  # re-probing the box, which is the only evidence that means anything.
  log "vm $id: running opnsense-update -bkp (this takes a while)"
  if ! qm guest exec "$id" --timeout "$UPDATE_EXEC_TIMEOUT" -- \
      /usr/local/sbin/opnsense-update -bkp >/dev/null 2>&1; then
    log "vm $id: the upgrade call did not return cleanly — continuing to the readiness gate, which decides"
  fi

  # Base and kernel only take effect after a reboot, and the check reports
  # needs_reboot=1 for exactly that reason. Tolerated the same way: the box
  # going down IS the reboot, so the RPC failing here is the expected case.
  log "vm $id: rebooting to apply base and kernel"
  qm guest exec "$id" --timeout 60 -- /sbin/shutdown -r now >/dev/null 2>&1 || true

  # Wait for the box to actually go down before trusting a readiness probe. A
  # box that never goes down may simply not have needed a reboot, so this is a
  # grace period and not a gate.
  local deadline=$((SECONDS + UPDATE_REBOOT_GRACE))
  while [ "$SECONDS" -lt "$deadline" ]; do
    box_serves "$url" || break
    sleep 10
  done

  if ! wait_box_ready "$id" "$UPDATE_READY_TIMEOUT"; then
    log "ERROR: vm $id did not come back within ${UPDATE_READY_TIMEOUT}s after the upgrade" >&2
    rm -f "$work"
    # A rollback that ALSO fails is a different, worse event than an upgrade
    # that failed cleanly: one box is serving, the other may not be.
    rollback_guest "$id" || return 3
    return 1
  fi

  # Prove it took, rather than assuming a box that boots was upgraded.
  probe_check "$id" "$work"
  after=$(check_identity "$work")
  verdict=$(update_verdict "$work")
  rm -f "$work"

  case "$verdict" in
    current)
      log "vm $id updated: ${before:-unknown} -> ${after:-unknown}"
      return 0
      ;;
    unusable)
      # Distinct from the pre-update unusable above: this box WAS upgraded and
      # rebooted, we just cannot confirm what it is now running. Someone has to
      # look, where an untouched box is a non-event.
      log "ERROR: vm $id came back but its post-update check is unusable — currency UNKNOWN after an upgrade" >&2
      return 6
      ;;
    *)
      # DELIBERATELY NOT ROLLED BACK, though review suggested it. The rollback
      # paths above exist for a box that did NOT come back; this one is serving,
      # so its state is known and a human can read it. Rolling back here would
      # discard a partial upgrade in favour of an older one on a healthy box,
      # and would risk a rollback failure - turning a boring exit 3 into a
      # possibly-broken lab. The restore point is still there for whoever looks.
      log "ERROR: vm $id still reports pending updates after upgrading (now ${after:-unknown}); box is serving, restore point $(snapshot_name "$id") kept" >&2
      return 1
      ;;
  esac
}

# ensure_hold guarantees a hold of at least this many seconds from now,
# EXTENDING an existing one and never shortening it. Shortening would let this
# call cut short a hold a longer-running caller is relying on.
ensure_hold() {
  local seconds=$1 wanted now existing=0
  now=$(date -u +%s)
  wanted=$((now + seconds))
  if [ -f "$HOLD_FILE" ]; then
    existing=$(head -n1 "$HOLD_FILE" 2>/dev/null | tr -d '[:space:]')
    case "$existing" in '' | *[!0-9]*) existing=0 ;; esac
  fi
  if [ "$existing" -ge "$wanted" ]; then
    return 0
  fi
  mkdir -p "$(dirname "$HOLD_FILE")"
  printf '%s\n' "$wanted" > "$HOLD_FILE"
  log "hold extended to $(date -u -d "@$wanted" '+%Y-%m-%dT%H:%M:%SZ') for the update"
}

# DERIVED, not guessed. A hold shorter than the work it protects is worse than
# none: it lapses mid-upgrade and hands the box straight back to the down timer,
# which is the exact failure the hold exists to prevent. 5400 was picked as a
# round number and was less than half the real worst case.
#
# Per box, worst case: the upgrade call, the reboot grace, the readiness gate, a
# rollback's own readiness gate, and two probes.
update_hold_seconds() {
  local per_box
  per_box=$((UPDATE_EXEC_TIMEOUT + UPDATE_REBOOT_GRACE + UPDATE_READY_TIMEOUT \
             + READY_TIMEOUT + 2 * GUEST_EXEC_TIMEOUT))
  # Every firewall in sequence, plus ten minutes of margin.
  echo $((per_box * ${#PHASE1_VMS[@]} + 600))
}

# cmd_update brings both firewalls to their channel heads, in place, one at a
# time.
#
# Exit 0 both at head; 3 an upgrade failed with the box still serving, or a
# firewall was not running so nothing was attempted on it; 4 currency
# is UNKNOWN for a box, before or after an upgrade, with the log saying which;
# 5 a rollback FAILED and the lab needs hands; 6 the update aborted early, which
# is how the dispatch below remaps a die(). All of them are distinct from
# apidrift's 1 (breaking drift) and 2 (probe error), so an update problem can
# never be read as drift or as an unreachable box.
cmd_update() {
  local id status broken=0 failed=0 unknown=0 touched_unknown=0 not_running=0

  # TAKE A HOLD FIRST. The down timer is otherwise free to fire in the middle of
  # an upgrade and pull the power on a box that is part-way through writing a
  # new base and kernel - the one moment in this script's life when a hard stop
  # can actually break a guest. `down` skips entirely while a hold is live, so
  # the existing mechanism is the lock; it just was not being taken here.
  #
  # Deliberately NOT released at the end: the caller owns the lifecycle, and CI
  # drops the hold as part of its teardown. A hold left behind lapses on its own.
  #
  # The lock is taken BEFORE the hold, so a `down` cannot read the hold as free
  # and then race this call into taking one.
  with_lock 300 update
  ensure_hold "$(update_hold_seconds)"

  for id in "${PHASE1_VMS[@]}"; do
    if [ "$(guest_state "$id")" != running ]; then
      log "ERROR: vm $id is not running — raise the lab before updating" >&2
      not_running=1
      continue
    fi
    status=0
    update_guest "$id" || status=$?
    case "$status" in
      0) ;;
      2) unknown=1 ;;
      6) unknown=1; touched_unknown=1 ;;
      3)
        # A rollback that failed leaves ONE box possibly broken. Touching the
        # other one now can only widen the damage, and the run already has to
        # stop for hands - so stop here rather than upgrading into a lab that is
        # already in a state nobody has looked at.
        broken=1
        log "ERROR: stopping before the remaining firewalls — the lab needs hands first" >&2
        break
        ;;
      *) failed=1 ;;
    esac
  done

  # Worst outcome wins, and each has its own exit so a caller can tell them
  # apart. All are distinct from apidrift's 1 (breaking drift) and 2 (probe
  # error), so an update problem never reads as drift.
  if [ "$broken" -ne 0 ]; then
    log "ERROR: a rollback FAILED — a firewall may be broken and needs hands" >&2
    exit 5
  fi
  # Log EVERY category that occurred before choosing an exit. A run that hit two
  # different problems must not report only the one that happened to win.
  if [ "$not_running" -ne 0 ]; then
    log "ERROR: a firewall was not running, so nothing was updated on it" >&2
  fi
  if [ "$failed" -ne 0 ]; then
    log "ERROR: an upgrade failed; the box is serving, but not at its channel head" >&2
  fi
  if [ "$touched_unknown" -ne 0 ]; then
    log "ERROR: a firewall was UPGRADED and then could not be verified — currency UNKNOWN, check it" >&2
  elif [ "$unknown" -ne 0 ]; then
    log "ERROR: a firmware check was unusable before any upgrade — currency UNKNOWN, box NOT touched" >&2
  fi

  # An upgraded box nobody can identify outranks one that failed cleanly and is
  # still serving a KNOWN build: the second is a bad state someone understands,
  # the first is a box whose contents are a guess.
  if [ "$touched_unknown" -ne 0 ]; then
    exit 4
  fi
  if [ "$failed" -ne 0 ] || [ "$not_running" -ne 0 ]; then
    exit 3
  fi
  if [ "$unknown" -ne 0 ]; then
    exit 4
  fi
  log "both firewalls are at their channel heads"
}

# ---------------------------------------------------------------------------
# Subcommands
# ---------------------------------------------------------------------------

cmd_up() {
  local hold_seconds=${1-}
  # Queue rather than refuse: a raise that loses a race to a finishing teardown
  # should wait for it, not fail the run.
  with_lock 300 up "$@"
  log "bringing the testbed up"
  for id in "${PHASE1_VMS[@]}"; do start_guest "$id"; done
  wait_ready
  # BEFORE phase 2, not after. settle_containers is the step that waits on the
  # traffgen's leases, so a Kea with no sockets makes it burn its whole
  # ADDRESS_TIMEOUT and then report the container unaddressed - which is the
  # exact symptom this repair exists to remove. Repairing Kea afterwards fixes
  # the firewall and leaves the container with no address, so the order here is
  # the whole point of the step rather than a detail of it.
  ensure_dhcp_sockets
  # Phase 2 only after the firewalls serve AND can actually hand out a lease, so
  # every dependent guest finds a DHCP server and a default route on its first
  # attempt.
  for id in "${PHASE2_CTS[@]}"; do start_guest "$id"; done
  settle_containers
  warm_firmware_check
  if [ -n "$hold_seconds" ]; then
    cmd_hold "$hold_seconds"
  fi
  log "testbed up"
}

cmd_down() {
  local now expiry
  # Do not queue. This is the verb the scheduled down timer fires, and a timer
  # that blocks for twenty minutes behind a running update would pile up one
  # waiting instance per tick. Stepping aside costs nothing: the next firing
  # tries again, and the hold would have made it a no-op anyway.
  # DO NOT QUEUE, and EXIT 8 ON CONFLICT rather than 0. A `down` that stepped
  # aside has NOT taken the lab down, and reporting that as success is how the
  # first CI-driven run tore nothing down and said it had: the teardown step
  # went green while six guests kept running.
  #
  # flock's -E 8 produces that exit directly. The watchdog timer tolerates it
  # via SuccessExitStatus, because for a timer "busy, will retry in five
  # minutes" really is fine; for a caller that asked for a teardown and needs to
  # know whether it happened - CI above all - it stays a failure.
  with_lock 0 down
  now=$(date +%s)
  if hold_is_live "$HOLD_FILE" "$now"; then
    expiry=$(head -n1 "$HOLD_FILE")
    log "hold active until $(date -u -d "@$expiry" '+%Y-%m-%dT%H:%M:%SZ') — skipping shutdown"
    return 0
  fi
  log "taking the testbed down"
  # Reverse order: dependents first, firewalls last, so nothing loses its
  # gateway mid-shutdown and hangs.
  while read -r id; do stop_guest "$id"; done < <(guest_order down)
  log "testbed down"
}

cmd_hold() {
  local seconds=${1:-$DEFAULT_HOLD_SECONDS} expiry
  case "$seconds" in ''|*[!0-9]*) die "hold: '$seconds' is not a number of seconds" ;; esac
  expiry=$(( $(date +%s) + seconds ))
  mkdir -p "$(dirname "$HOLD_FILE")"
  echo "$expiry" > "$HOLD_FILE"
  log "hold set until $(date -u -d "@$expiry" '+%Y-%m-%dT%H:%M:%SZ')"
}

cmd_release() {
  rm -f "$HOLD_FILE"
  log "hold released — the next scheduled down will run"
}

cmd_status() {
  local now id state
  now=$(date +%s)
  if hold_is_live "$HOLD_FILE" "$now"; then
    printf 'hold:   active until %s\n' \
      "$(date -u -d "@$(head -n1 "$HOLD_FILE")" '+%Y-%m-%dT%H:%M:%SZ')"
  else
    printf 'hold:   none\n'
  fi
  while read -r id; do
    state=$(guest_state "$id")
    printf 'guest %s (%s): %s\n' "$id" "$(guest_kind "$id")" "${state:-unknown}"
  done < <(guest_order up)
}

usage() {
  cat <<'EOF'
Usage: opnsense-testbed-power.sh <command>

  up [seconds]   Start the testbed in dependency order and block until both
                 firewalls serve :443. With [seconds], also set a hold.
  update         Bring both firewalls to their channel heads in place, taking a
                 restore point first and rolling back a box that does not come
                 back. Exit 0 both at head, 3 an upgrade failed or a firewall
                 was not running,
                 4 currency UNKNOWN for a box (before or after an upgrade - the
                 log says which), 5 a rollback FAILED and the lab needs hands,
                 6 the update aborted early. Never 1 or 2, which belong to
                 apidrift's drift and probe-error verdicts.
  down           Gracefully stop the testbed, unless a hold is active. Exit 8
                 means another operation held the lock so nothing was stopped -
                 a retry, not a success.
  hold [seconds] Suppress the scheduled shutdown (default 8h). Auto-expires.
  release        Clear an active hold.
  status         Show hold state and every guest's power state.
  exec <id> -- <command...>
                 Run a command in an allowlisted guest and return its status.
  put <id> <local-path> <remote-path>
                 Push a file to an allowlisted container (VMs have no put route).
EOF
}

# Strip the internal re-entry flag before dispatching. Set only by with_lock's
# own re-exec, which is already inside flock.
LOCK_REENTERED=0
if [ "${1-}" = "--locked" ]; then
  LOCK_REENTERED=1
  shift
fi

case "${1-}" in
  up)      shift; cmd_up "${1-}" ;;
  update)
    # die() exits 1, and cmd_update's whole contract is that its failures are
    # NEVER apidrift's 1 (breaking drift) or 2 (probe error). An abort inside
    # snapshot_guest, ready_url_for or assert_allowed would otherwise surface as
    # exit 1 and a caller would read a refused snapshot as live API drift.
    #
    # cmd_update itself only ever exits 0, 3, 4 or 5, so a 1 or 2 arriving here
    # can only be an abort, and 6 says exactly that: the update stopped early
    # for an operational reason and the log says which.
    #
    # SUBSHELL, or none of this works: cmd_update and die() both call `exit`,
    # which ends the SCRIPT, so `||` never observes a status and the remap below
    # is dead code. Running it in a subshell turns those exits into a status
    # this dispatch can actually see. The exit 3 observed on a stopped lab
    # before this fix was cmd_update exiting the script directly with the right
    # number by luck; a die() would have escaped as 1 and read as drift.
    update_status=0
    ( cmd_update ) || update_status=$?
    case "$update_status" in
      1 | 2) exit 6 ;;
      *) exit "$update_status" ;;
    esac
    ;;
  down)    cmd_down ;;
  hold)    shift; cmd_hold "${1-}" ;;
  release) cmd_release ;;
  status)  cmd_status ;;
  exec)    shift; cmd_exec "$@" ;;
  put)     shift; cmd_put "$@" ;;
  -h|--help|help) usage ;;
  *)       usage >&2; exit 2 ;;
esac

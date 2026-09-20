#!/usr/bin/env bash
# The only thing CI can do on `oli` (OPN-0109).
#
# THIS FILE IS A LOGIN SHELL, not an authorized_keys forced command. The first
# design used `command="..."` in authorized_keys and it does not bind here:
# TAILSCALE SSH TERMINATES PORT 22 ON THIS HOST, so sshd never consults
# authorized_keys for a tailnet connection and the forced command is simply
# skipped. Verified 2026-09-20 - `ssh -v` reports `Authenticated to ... using
# "none"` and lands in a full root shell with the forced-command line present
# and correctly formed.
#
# So the restriction is rebuilt where we still own it. The tailnet policy's ssh
# rule pins `users: ["opn-ci"]`, which is the one thing the ACL CAN enforce -
# it has no command-level control, but it does decide which local account a
# connection becomes. That account's login shell is this script, so every
# session arrives here whatever the client asked for.
#
# Invoked three ways, all handled below:
#   <shell> -c "up 3600"   a non-interactive SSH command (what CI sends)
#   <shell>                an interactive session (refused - there is no shell)
#   SSH_ORIGINAL_COMMAND   a forced command, if this is ever moved to an sshd
#                          that actually honours one
#
# THE ALLOWLIST IS THE SECURITY BOUNDARY. Below it, operations run as root via
# a single no-wildcard sudoers entry naming this script, so this file is trusted
# with its own argv and must never pass an unvalidated string to a shell or to
# the power script.
set -euo pipefail

POWER=/usr/local/bin/opnsense-testbed-power.sh
SELF=/usr/local/bin/opnsense-testbed-ci.sh

# The longest hold CI may take. A run that dies without calling `down` leaves
# the lab up until its hold lapses, so this bounds a crashed workflow to one
# hold rather than to the next scheduled backstop.
MAX_HOLD_SECONDS=5400

deny() {
  printf 'opnsense-testbed-ci: refusing %s\n' "$1" >&2
  exit 2
}

# --- Work out what was actually asked for.
#
# `escalated` marks the one re-entry this script makes into itself under sudo.
# Without it, a sudo that returns WITHOUT elevating sends the second invocation
# straight back down the escalation branch and the script re-execs itself
# forever - a fork bomb reachable from a remote session, which is not a thing a
# security boundary should contain. The guard makes re-entry structurally
# once-only rather than dependent on sudo behaving.
request=""
escalated=0
case "${1-}" in
  --privileged)
    escalated=1
    shift
    request="$*"
    ;;
  -c)
    [ "$#" -eq 2 ] || deny "a -c with ${#} arguments"
    request=$2
    ;;
  '')
    request=${SSH_ORIGINAL_COMMAND-}
    ;;
  *)
    request="$*"
    ;;
esac

# No verb takes a free-form argument except `up <seconds>`, and that one is
# numeric and clamped, so splitting on whitespace is safe here and only here.
read -r -a argv <<<"$request"
verb=${argv[0]-}
hold=${argv[1]-}

case "$verb" in
  up)
    case "$hold" in
      '' | *[!0-9]*) deny "up with a non-numeric hold '${hold}'" ;;
    esac
    [ "$hold" -gt 0 ] || deny "up with a zero hold — an unheld lab is taken down underneath the run"
    [ "${#argv[@]}" -le 2 ] || deny "up with extra arguments"
    if [ "$hold" -gt "$MAX_HOLD_SECONDS" ]; then
      printf 'opnsense-testbed-ci: clamping hold %s to %s\n' "$hold" "$MAX_HOLD_SECONDS" >&2
      hold=$MAX_HOLD_SECONDS
    fi
    ;;
  update | status | down)
    [ "${#argv[@]}" -eq 1 ] || deny "$verb with arguments"
    ;;
  '')
    deny "an interactive session — this account has no shell"
    ;;
  *)
    deny "the unknown verb '${verb}'"
    ;;
esac

# --- Escalate exactly once, with the verb already validated.
#
# sudoers names this script with NO wildcard, so sudo permits any argv and the
# validation above is what actually constrains it. That is why the re-entry
# re-validates rather than trusting its own caller: the argv arriving on the
# privileged side is only as trustworthy as whoever invoked sudo.
#
# Falling through WITHOUT root is deliberate. If sudo is missing or refuses,
# the power script runs as opn-ci and fails on Proxmox permissions - a clear
# error, where looping or silently doing nothing would both be worse.
if [ "$escalated" -eq 0 ] && [ "$(id -u)" -ne 0 ]; then
  if [ "$verb" = up ]; then
    exec sudo -n "$SELF" --privileged up "$hold"
  fi
  exec sudo -n "$SELF" --privileged "$verb"
fi

case "$verb" in
  up)     exec "$POWER" up "$hold" ;;
  update) exec "$POWER" update ;;
  status) exec "$POWER" status ;;
  down)
    # `down` refuses while a hold is live, and CI is the holder, so CI has to be
    # able to drop its own hold or the lab stays up until the hold lapses -
    # which is the whole thing CI-owned power exists to avoid.
    #
    # A failed release is REPORTED rather than swallowed. `cmd_release` only
    # removes a file, so failing means something is wrong with the hold
    # directory, and the `down` that follows will then refuse and leave six
    # guests running. Silence there reads as a successful teardown.
    if ! "$POWER" release >&2; then
      printf 'opnsense-testbed-ci: WARNING — could not release the hold; the down below will refuse while it is live\n' >&2
    fi
    exec "$POWER" down
    ;;
esac

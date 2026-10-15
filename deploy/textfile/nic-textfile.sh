#!/bin/sh
# Root cron runtime artifact; FreeBSD sysctl/vmstat are read-only sources.
set -eu
PATH=/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/sbin:/usr/local/bin
export PATH LC_ALL=C
DIR=${1:-/var/tmp/node_exporter}
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# The plugin owns this directory. Do not change its ownership or permissions.
[ -d "$DIR" ] || { echo "Missing textfile directory: $DIR" >&2; exit 1; }
# Stage outside the nobody-owned collector directory, under the sticky root-owned parent.
WORK=$(mktemp -d /var/tmp/opn2otel-nic.XXXXXX)
trap 'exit 1' HUP INT TERM
trap 'rm -f "$WORK/dev" "$WORK/interrupts" "$WORK/result"; rmdir "$WORK"' EXIT
sysctl dev > "$WORK/dev"
vmstat -i > "$WORK/interrupts"
awk -v now="$(date +%s)" -f "$HERE/nic-textfile.awk" "$WORK/dev" "$WORK/interrupts" > "$WORK/result"
chmod 644 "$WORK/result"
# FreeBSD base Python is used by OPNsense. No copy fallback: EXDEV must fail closed.
/usr/local/bin/python3 - "$WORK/result" "$DIR" <<'PYTHON'
import os
import sys

# Anchor the destination before publishing; never traverse a replacement parent symlink.
directory = os.open(sys.argv[2], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
try:
    os.replace(sys.argv[1], "nic.prom", dst_dir_fd=directory)
finally:
    os.close(directory)
PYTHON

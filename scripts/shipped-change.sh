#!/usr/bin/env bash
# Decide whether a commit changes an input to a shipped artifact (image, chart,
# binaries). Prints "true" or "false" on stdout; the reason goes to stderr.
#
#   scripts/shipped-change.sh <sha> [<base>]
#
# With <base>, every commit in <base>..<sha> counts (a push range); without it,
# only <sha> against its parent. An empty or all-zero <base> means a new branch
# and falls back to the parent comparison.
#
# auto-rc.yml cuts a release candidate and release-please.yml publishes the
# :main edge image only when this prints "true". Production tracks :main, so a
# false "true" restarts the exporter and drops in-flight log records.
#
# The path list is a positive allowlist: new repository metadata stays
# release-neutral until it is deliberately identified as a shipped input.
#
# The release workflows are on the list because a real edit to them changes how
# artifacts are built. A commit that only moves their rknightion/.github pins
# (Renovate bumps them on every shared-workflow release) is not shipped: the
# next real change picks the new pin up.
set -euo pipefail

sha=${1:?usage: shipped-change.sh <sha> [<base>]}
base=${2:-}
[[ $base =~ ^0+$ ]] && base=""

release_workflows=" .github/workflows/auto-rc.yml .github/workflows/publish.yml .github/workflows/release-please.yml "

# A changed line that is nothing but a SHA-pinned rknightion/.github call.
pin_line='^[+-][[:space:]]*(-[[:space:]]+)?uses:[[:space:]]+rknightion/\.github/[^@[:space:]]+@[0-9a-f]{40}([[:space:]]+#.*)?$'

# A commit git cannot read must fail the job, never print "false" and skip a
# release. Paths go through a file so a failing git command trips set -e;
# process substitution would hide its exit status.
git rev-parse --verify -q "${sha}^{commit}" >/dev/null || {
  echo "shipped-change: unknown commit ${sha}" >&2
  exit 2
}
paths_file=$(mktemp)
trap 'rm -f "$paths_file"' EXIT

if [ -n "$base" ] && git rev-parse --verify -q "${base}^{commit}" >/dev/null; then
  git diff --name-only --no-renames -z "$base" "$sha" >"$paths_file"
elif git rev-parse --verify -q "${sha}^" >/dev/null; then
  base="${sha}^"
  # --no-renames keeps both sides of a move, so moving a shipped path out of
  # the allowlist still counts as shipped.
  git diff --name-only --no-renames -z "$base" "$sha" >"$paths_file"
else
  # A root commit has no parent. Treat every path as changed; this is also the
  # fail-safe when the parent is unavailable.
  base=""
  git ls-tree --name-only -r -z "$sha" >"$paths_file"
fi
mapfile -d '' -t paths <"$paths_file"

# True when every changed line in <path> is a pin line, and the removed and
# added lines name the same targets once the SHA and its comment are dropped.
only_pin_bumps() {
  local path=$1 lines
  [ -n "$base" ] || return 1
  lines=$(git diff --no-renames --unified=0 "$base" "$sha" -- "$path" |
    grep -E '^[+-]' | grep -Ev '^(\+\+\+|---) ') || return 1
  [ -n "$lines" ] || return 1
  grep -Evq "$pin_line" <<<"$lines" && return 1
  targets() { grep -E "^[$1]" <<<"$lines" | sed -E 's/^[+-][[:space:]]*(-[[:space:]]+)?uses:[[:space:]]+([^@]+)@.*/\2/' | sort; }
  [ "$(targets -)" = "$(targets +)" ]
}

for path in "${paths[@]}"; do
  case "$path" in
    *.go | go.mod | go.sum | vendor/* | grafana/* | charts/* | deploy/* | \
      justfile | Dockerfile | .dockerignore | .goreleaser.yml | LICENSE | \
      internal/webui/oui_data.txt | internal/webui/templates/* | \
      scripts/notices.sh | scripts/notices.tsv.tmpl | scripts/shipped-change.sh | \
      .github/workflows/auto-rc.yml | .github/workflows/publish.yml | \
      .github/workflows/release-please.yml)
      if [[ $release_workflows == *" $path "* ]] && only_pin_bumps "$path"; then
        echo "Release-neutral: ${path} only moves rknightion/.github pins" >&2
        continue
      fi
      echo "Shipped-artifact input changed: ${path}" >&2
      echo true
      exit 0
      ;;
  esac
done

echo "No shipped-artifact input changed" >&2
echo false

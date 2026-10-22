#!/usr/bin/env bash
# Regression checks for scripts/shipped-change.sh against a throwaway git repo.
set -euo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/shipped-change.sh"
repo=$(mktemp -d)
trap 'rm -rf "$repo"' EXIT
cd "$repo"
git init -q
git config user.email test@example.invalid
git config user.name test
git config commit.gpgsign false

old=0f4847f833f096c1f03284481d7e6b19c2f81c30
new=693f703185f03c93249fd10e450c3f217ef6e137
mkdir -p .github/workflows
cat >.github/workflows/publish.yml <<EOF
jobs:
  image:
    uses: rknightion/.github/.github/workflows/container-publish.yml@${old} # v1.25.8
    steps:
      - uses: rknightion/.github/.github/actions/next-rc-tag@${old} # main
      - uses: actions/checkout@${old} # v7.0.1
EOF
echo 'package main' >main.go
echo '# notes' >README.md
git add -A && git commit -qm root

failures=0
expect() {
  local want=$1 label=$2 got
  got=$("$script" HEAD ${3:+"$3"} 2>/dev/null)
  if [ "$got" != "$want" ]; then
    printf 'FAIL %s: want %s, got %s\n' "$label" "$want" "$got" >&2
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "$label"
  fi
}

expect true "root commit is shipped"

sed -i.bak "s/${old} # v1.25.8/${new} # v1.25.19/; s/next-rc-tag@${old}/next-rc-tag@${new}/" \
  .github/workflows/publish.yml && rm .github/workflows/publish.yml.bak
git commit -qam "bump shared pins"
expect false "rknightion/.github pin bump in a release workflow"

sed -i.bak "s/checkout@${old}/checkout@${new}/" .github/workflows/publish.yml && rm .github/workflows/publish.yml.bak
git commit -qam "bump third-party action"
expect true "third-party action bump in a release workflow"

echo '    timeout-minutes: 30' >>.github/workflows/publish.yml
git commit -qam "real workflow edit"
expect true "real edit to a release workflow"

echo 'more' >>README.md
git commit -qam docs
expect false "docs-only change"

echo '// x' >>main.go
git commit -qam code
expect true "Go change"

sed -i.bak "s/${new} # v1.25.19/${old} # v1.25.8/" .github/workflows/publish.yml && rm .github/workflows/publish.yml.bak
echo '// y' >>main.go
git commit -qam "pin bump plus code"
expect true "pin bump together with a Go change"

sed -i.bak "s#workflows/container-publish.yml@${old}#workflows/other-publish.yml@${new}#" \
  .github/workflows/publish.yml && rm .github/workflows/publish.yml.bak
git commit -qam "retarget shared pin"
expect true "pin moved to a different rknightion/.github target"

before=$(git rev-parse HEAD)
echo '// z' >>main.go
git commit -qam "code in a push"
echo 'again' >>README.md
git commit -qam "docs on top"
expect false "last commit alone is docs-only"
expect true "push range with an earlier Go change" "$before"
expect false "all-zero before (new branch) falls back to the parent" 0000000000000000000000000000000000000000

if "$script" deadbeefdeadbeefdeadbeefdeadbeefdeadbeef >/dev/null 2>&1; then
  printf 'FAIL unknown commit: want a non-zero exit\n' >&2
  failures=$((failures + 1))
else
  printf 'ok   unknown commit fails the job\n'
fi

[ "$failures" -eq 0 ] || exit 1

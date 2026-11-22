#!/usr/bin/env bash
# Select a known successful ancestor, otherwise preserve the push's before SHA.
set -euo pipefail
head=${1:?usage: edge-diff-base.sh <head> <before> [<successful>]}
before=${2-}
successful=${3-}

if [[ $successful =~ ^[0-9a-fA-F]{40}$ ]] && [[ ! $successful =~ ^0+$ ]] &&
  git rev-parse --verify -q "${successful}^{commit}" >/dev/null &&
  git merge-base --is-ancestor "$successful" "$head"; then
  printf '%s\n' "$successful"
else
  echo '::warning::No usable successful release-please run commit; falling back to event before' >&2
  printf '%s\n' "$before"
fi

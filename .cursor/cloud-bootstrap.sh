#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local GOWORK=off GOPROXY=https://proxy.golang.org

# Follow the CI pin that Renovate updates, and fail if its format changes.
lint_version=$(sed -nE '/# renovate: datasource=github-releases depName=golangci\/golangci-lint$/ {
    n
    s/^[[:space:]]+version: (v[0-9]+\.[0-9]+\.[0-9]+)$/\1/p
}' .github/workflows/ci.yml)
if [[ ! $lint_version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf '%s\n' 'Could not read the golangci-lint pin in .github/workflows/ci.yml' >&2
    exit 1
fi

just _tool-govulncheck
GOBIN="$PWD/.tools" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$lint_version"

go build -mod=vendor ./...
go test -mod=vendor -race -c -o /dev/null ./...

go -C tools/promqlcheck mod download
go -C tools/promqlcheck build -mod=readonly -o /dev/null .
go -C tools/promqlcheck test -mod=readonly -c -o /dev/null ./...

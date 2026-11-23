#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local GOWORK=off GOPROXY=https://proxy.golang.org

just _tool-govulncheck
just _tool-golangci-lint

go build -mod=vendor ./...
go test -mod=vendor -race -c -o /dev/null ./...

go -C tools/promqlcheck mod download
go -C tools/promqlcheck build -mod=readonly -o /dev/null .
go -C tools/promqlcheck test -mod=readonly -c -o /dev/null ./...

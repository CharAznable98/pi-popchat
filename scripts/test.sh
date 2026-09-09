#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=15.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=15.0"
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run build
go test -race . ./internal/...

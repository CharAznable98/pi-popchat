#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=15.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=15.0"
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.18 generate bindings -ts -i -d frontend/bindings .

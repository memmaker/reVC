#!/bin/sh
# build.sh OUT.wasm: the goplay import (cmd/vc) for the web, with ../gamefiles embedded
set -e
cd "$(dirname "$0")"
rm -rf cmd/vc/gamefiles && cp -R ../gamefiles cmd/vc/gamefiles
GOOS=js GOARCH=wasm go build -ldflags=-s -o "${1:?OUT.wasm}" ./cmd/vc

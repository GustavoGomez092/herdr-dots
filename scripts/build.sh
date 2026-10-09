#!/bin/sh
# Herdr runs this as the manifest's [[build]] step, in the plugin root.
set -eu
mkdir -p bin
if ! command -v go >/dev/null 2>&1; then
	echo "herdr-dots: Go toolchain (>= 1.25) is required to build" >&2
	exit 1
fi
exec go build -o bin/herdr-dots ./cmd/herdr-dots

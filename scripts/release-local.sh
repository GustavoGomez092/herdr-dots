#!/bin/sh
# Builds the six release archives and dist/SHA256SUMS locally, with the same
# names and flags as .goreleaser.yaml, for testing the install path when
# GoReleaser is not installed (make snapshot). It never uploads anything.
# HERDR_DOTS_TARGETS="darwin/arm64 linux/amd64" builds fewer targets.
set -eu
cd "$(dirname "$0")/.."
NAME=herdr-dots
VERSION=$(tr -d ' \r\n' < internal/version/VERSION)
COMMIT=$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
rm -rf dist
mkdir -p dist
TARGETS=${HERDR_DOTS_TARGETS:-darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64}
for target in $TARGETS; do
	os=${target%/*}
	arch=${target#*/}
	base="${NAME}_${VERSION}_${os}_${arch}"
	exe=$NAME
	if [ "$os" = windows ]; then
		exe=$NAME.exe
	fi
	mkdir -p "dist/$base"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X herdr-dots/internal/version.Source=release -X herdr-dots/internal/version.Commit=$COMMIT" \
		-o "dist/$base/$exe" ./cmd/herdr-dots
	if [ "$os" = windows ]; then
		(cd "dist/$base" && zip -q -X "../$base.zip" "$exe")
	else
		tar -czf "dist/$base.tar.gz" -C "dist/$base" "$exe"
	fi
	rm -rf "dist/${base:?}"
done
cd dist
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum "${NAME}"_* > SHA256SUMS
else
	shasum -a 256 "${NAME}"_* > SHA256SUMS
fi
cat SHA256SUMS

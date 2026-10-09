#!/bin/sh
# Builds the six release archives and dist/SHA256SUMS locally, with the same
# names and flags as .goreleaser.yaml, for testing the install path when
# GoReleaser is not installed (make snapshot). It never uploads anything.
# HERDR_DOTS_TARGETS="darwin/arm64 linux/amd64" builds fewer targets.
# Archives are reproducible: fixed mtime (SOURCE_DATE_EPOCH, else the last
# commit time), owner 0, no gzip timestamp, one file per archive.
set -eu
cd "$(dirname "$0")/.."
NAME=herdr-dots
VERSION=$(tr -d ' \r\n' < internal/version/VERSION)
COMMIT=$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
EPOCH=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct HEAD 2>/dev/null || echo 0)}
STAMP=$(TZ=UTC date -r "$EPOCH" +%Y%m%d%H%M.%S 2>/dev/null || TZ=UTC date -d "@$EPOCH" +%Y%m%d%H%M.%S)
if tar --version 2>/dev/null | grep -q GNU; then
	OWNER="--owner=0 --group=0 --numeric-owner --sort=name --mtime=@$EPOCH"
else
	OWNER="--uid 0 --gid 0 --numeric-owner"
fi
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
	TZ=UTC touch -t "$STAMP" "dist/$base/$exe"
	if [ "$os" = windows ]; then
		(cd "dist/$base" && TZ=UTC zip -q -X "../$base.zip" "$exe")
	else
		# OWNER is a list of flags and must split.
		# shellcheck disable=SC2086
		tar $OWNER -cf - -C "dist/$base" "$exe" | gzip -n > "dist/$base.tar.gz"
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

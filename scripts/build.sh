#!/bin/sh
# Herdr runs this as the manifest's [[build]] step on macOS and Linux, from
# the plugin root, during `herdr plugin install`. It installs bin/herdr-dots:
#   1. the prebuilt release binary for this OS and CPU, downloaded from the
#      GitHub release and checked against release/SHA256SUMS. That file is
#      part of the repository Herdr cloned, so the checksums come from the
#      tree you chose to install, never from the download server;
#   2. otherwise a source build with Go 1.25 or newer;
#   3. otherwise it fails with the reason, leaving nothing half-installed.
# HERDR_PLUGIN_BUILD_OFFLINE=1 skips the download.
set -eu
cd "$(dirname "$0")/.."
NAME=herdr-dots
VERSION=$(tr -d ' \r\n' < internal/version/VERSION)

say() { echo "$NAME: $*" >&2; }

# platform prints <os>_<arch> in release asset terms.
platform() {
	if [ -n "${HERDR_DOTS_PLATFORM:-}" ]; then
		echo "$HERDR_DOTS_PLATFORM"
		return 0
	fi
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	case "$os" in
	darwin | linux) ;;
	*) return 1 ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) return 1 ;;
	esac
	echo "${os}_${arch}"
}

# repo prints owner/repo: release/REPO when set, else the clone's GitHub origin.
repo() {
	r=""
	if [ -f release/REPO ]; then
		r=$(tr -d ' \r\n' < release/REPO)
	fi
	if [ -z "$r" ] && command -v git >/dev/null 2>&1; then
		url=$(git config --get remote.origin.url 2>/dev/null || true)
		r=$(printf '%s\n' "$url" | sed -n -e 's#^https://github\.com/##p' -e 's#^git@github\.com:##p' | sed 's#\.git$##')
	fi
	if printf '%s\n' "$r" | grep -Eq '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$'; then
		echo "$r"
		return 0
	fi
	return 1
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		return 1
	fi
}

# fetch URL FILE downloads over HTTPS only (tests point
# HERDR_DOTS_RELEASE_BASE at a local http server).
fetch() {
	proto='=https'
	if [ -n "${HERDR_DOTS_RELEASE_BASE:-}" ]; then
		proto='=http,https'
	fi
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto "$proto" --retry 2 --connect-timeout 15 --max-time 300 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -T 60 -O "$2" "$1"
	else
		say "neither curl nor wget is installed"
		return 1
	fi
}

install_release() {
	if [ "${HERDR_PLUGIN_BUILD_OFFLINE:-}" = 1 ]; then
		say "HERDR_PLUGIN_BUILD_OFFLINE=1: skipping the download"
		return 1
	fi
	if ! plat=$(platform); then
		say "no prebuilt binary for $(uname -s) $(uname -m)"
		return 1
	fi
	asset="${NAME}_${VERSION}_${plat}.tar.gz"
	want=""
	if [ -f release/SHA256SUMS ]; then
		want=$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1; exit }' release/SHA256SUMS)
	fi
	if [ -z "$want" ]; then
		say "release/SHA256SUMS has no checksum for $asset"
		return 1
	fi
	if ! r=$(repo); then
		say "unknown GitHub repository: put owner/repo in release/REPO"
		return 1
	fi
	url="${HERDR_DOTS_RELEASE_BASE:-https://github.com}/$r/releases/download/v$VERSION/$asset"
	say "downloading $url"
	if ! fetch "$url" "$tmp/$asset"; then
		say "download failed"
		return 1
	fi
	if ! got=$(sha256 "$tmp/$asset"); then
		say "no sha256sum or shasum to check the download"
		return 1
	fi
	if [ "$got" != "$want" ]; then
		say "checksum mismatch for $asset (got $got, want $want): not installing it"
		return 1
	fi
	mkdir -p "$tmp/x"
	if ! tar -xzf "$tmp/$asset" -C "$tmp/x" "$NAME" 2>/dev/null || [ ! -f "$tmp/x/$NAME" ] || [ -L "$tmp/x/$NAME" ]; then
		say "$asset has no $NAME binary"
		return 1
	fi
	chmod 755 "$tmp/x/$NAME"
	mkdir -p bin/release
	rm -f "bin/release/$asset.sigstore.json"
	if fetch "$url.sigstore.json" "$tmp/$asset.sigstore.json" 2>/dev/null; then
		mv "$tmp/$asset.sigstore.json" "bin/release/$asset.sigstore.json"
	fi
	mv "$tmp/$asset" "bin/release/$asset"
	mv "$tmp/x/$NAME" "bin/$NAME"
	printf 'source=release\nversion=%s\nrepo=%s\nasset=%s\nsha256=%s\n' "$VERSION" "$r" "$asset" "$got" > bin/INSTALL_SOURCE
	return 0
}

build_source() {
	say "building $VERSION from source with $(go version)"
	if ! go build -trimpath -ldflags "-X herdr-dots/internal/version.Source=source" -o "$tmp/$NAME" ./cmd/herdr-dots; then
		return 1
	fi
	mv "$tmp/$NAME" "bin/$NAME"
	printf 'source=source\nversion=%s\n' "$VERSION" > bin/INSTALL_SOURCE
	return 0
}

mkdir -p bin
tmp=$(mktemp -d "bin/.install.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

if install_release; then
	say "installed the verified $VERSION release binary"
	exit 0
fi
if command -v go >/dev/null 2>&1; then
	if build_source; then
		say "built $VERSION from source"
		exit 0
	fi
	say "the source build failed (see above); bin/ is unchanged"
	exit 1
fi
say "could not install: no verified prebuilt binary (see above) and no Go toolchain."
say "Install Go 1.25 or newer (https://go.dev/dl/) and reinstall the plugin, or download"
say "${NAME}_${VERSION}_<os>_<arch>.tar.gz from the GitHub release, check it against"
say "release/SHA256SUMS and put the $NAME binary in bin/."
exit 1

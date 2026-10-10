#!/bin/sh
# The release checks (controller ruling P10-R12). internal/version/VERSION is
# the exact version the binaries report: 1.0.0-rc.1 for a candidate of the
# first release, 1.0.0 for the final one.
#
#   release-kind.sh candidate VERSION
#       A manual workflow_dispatch run of release.yml on main (no tag):
#       checks VERSION's form before the archives, SHA256SUMS and ASSET_ENV
#       are built and proposed in the checksum pull request.
#   release-kind.sh publish VERSION TAG RELEASE_DIR
#       A pushed tag: TAG must be v<VERSION>, RELEASE_DIR/ASSET_ENV must pin
#       HERDR_DOTS_VERSION=<VERSION> (once), and RELEASE_DIR/SHA256SUMS must
#       list only that version's archives: the tag sits on the merge commit
#       of a candidate of exactly this VERSION.
#
# Both print "prerelease" for X.Y.Z-rc.<digits> and "final" for X.Y.Z. Any
# other VERSION (1.0.0-rc.1x, 1.0.0-beta-rc.1, ...) or a failed check prints
# an error and exits 1. Tested by scripts/releasekind_test.go.
set -eu
usage="usage: release-kind.sh candidate VERSION | publish VERSION TAG RELEASE_DIR"
fail() {
	echo "$*" >&2
	exit 1
}
step=${1:-}
version=${2:-}
nl='
'
case $version in
*"$nl"*) fail "VERSION holds a line break" ;;
esac
if printf '%s\n' "$version" | grep -Eqx '[0-9]+\.[0-9]+\.[0-9]+'; then
	kind=final
elif printf '%s\n' "$version" | grep -Eqx '[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+'; then
	kind=prerelease
else
	fail "VERSION '$version' is neither X.Y.Z nor X.Y.Z-rc.<digits>"
fi
case $step in
candidate)
	[ $# -eq 2 ] || fail "$usage"
	;;
publish)
	[ $# -eq 4 ] || fail "$usage"
	tag=$3
	dir=$4
	[ "$tag" = "v$version" ] || fail "tag '$tag' does not match VERSION $version (want v$version)"
	[ -s "$dir/ASSET_ENV" ] || fail "$dir/ASSET_ENV is missing or empty: merge the candidate's checksum pull request first"
	pins=$(grep -c '^HERDR_DOTS_VERSION=' "$dir/ASSET_ENV" || true)
	if [ "$pins" -ne 1 ] || ! grep -Fqx "HERDR_DOTS_VERSION=$version" "$dir/ASSET_ENV"; then
		fail "$dir/ASSET_ENV does not pin HERDR_DOTS_VERSION=$version: tag the merge commit of this version's candidate"
	fi
	[ -s "$dir/SHA256SUMS" ] || fail "$dir/SHA256SUMS is missing or empty: merge the candidate's checksum pull request first"
	if ! awk -v p="herdr-dots_${version}_" 'NF != 2 || index($2, p) != 1 { bad = 1 } END { exit bad }' "$dir/SHA256SUMS"; then
		fail "$dir/SHA256SUMS lists archives of another version (want herdr-dots_${version}_*)"
	fi
	;;
*) fail "$usage" ;;
esac
echo "$kind"

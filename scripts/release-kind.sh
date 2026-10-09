#!/bin/sh
# Classifies a release tag against internal/version/VERSION.
# Usage: release-kind.sh <VERSION> <TAG>
# Prints "candidate" when TAG is v<VERSION> and VERSION ends in -rc.N,
# "final" when TAG is v<VERSION> and VERSION has no suffix. Anything else
# (a mismatch, another suffix) prints an error and exits 1.
# Tested by internal/version/releasekind_test.go; release.yml calls it.
set -eu
version=${1:?usage: release-kind.sh VERSION TAG}
tag=${2:?usage: release-kind.sh VERSION TAG}
if [ "$tag" != "v$version" ]; then
	echo "tag $tag does not match VERSION $version (want v$version)" >&2
	exit 1
fi
case "$version" in
*-rc.[0-9]*) echo candidate ;;
*[!0-9.]*)
	echo "VERSION $version has an unsupported suffix" >&2
	exit 1
	;;
*) echo final ;;
esac

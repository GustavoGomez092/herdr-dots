#!/bin/sh
# Runs every native Go fuzz target (func FuzzXxx) in the module for a fixed
# time each: 30s by default (pull requests), 10m in the release job:
#   sh scripts/fuzz.sh 10m
# Package patterns after the time run only those packages' targets (the
# release job's matrix groups; TestReleaseFuzzMatrixCoversEveryTarget checks
# that the groups together cover every target):
#   sh scripts/fuzz.sh 10m ./internal/memory/ ./internal/transcript/
# A crash stops the run; Go saves the input under testdata/fuzz/<Target>/ in
# that package. Commit it with the fix so it stays a regression test.
# A package whose tests don't compile fails the run (it would otherwise hide
# its targets), and so does finding fewer than MIN_TARGETS targets in a full
# run (raise it when you add one), or none in a subset.
set -eu
FUZZTIME="${1:-30s}"
MIN_TARGETS=26
if [ $# -gt 0 ]; then
	shift
fi
min=1
if [ $# -eq 0 ]; then
	set -- ./...
	min=$MIN_TARGETS
fi
cd "$(dirname "$0")/.."
pkgs=$(go list "$@")
targets=""
n=0
for pkg in $pkgs; do
	# An assignment from a failing command substitution fails under set -e.
	list=$(go test -list '^Fuzz' "$pkg")
	for target in $(printf '%s\n' "$list" | grep '^Fuzz' || true); do
		targets="$targets $pkg:$target"
		n=$((n + 1))
	done
done
if [ "$n" -lt "$min" ]; then
	echo "found $n fuzz targets, expected at least $min" >&2
	exit 1
fi
for pt in $targets; do
	pkg=${pt%%:*}
	target=${pt#*:}
	echo "== $pkg $target ($FUZZTIME)"
	go test "$pkg" -run '^$' -fuzz "^${target}\$" -fuzztime "$FUZZTIME"
done
echo "fuzzed $n targets for $FUZZTIME each"

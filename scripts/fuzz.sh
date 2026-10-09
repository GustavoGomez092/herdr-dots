#!/bin/sh
# Runs every native Go fuzz target (func FuzzXxx) in the module for a fixed
# time each: 30s by default (pull requests), 10m in the release job:
#   sh scripts/fuzz.sh 10m
# A crash stops the run; Go saves the input under testdata/fuzz/<Target>/ in
# that package. Commit it with the fix so it stays a regression test.
# A package whose tests don't compile fails the run (it would otherwise hide
# its targets), and so does finding fewer than MIN_TARGETS targets: raise it
# when you add one.
set -eu
FUZZTIME="${1:-30s}"
MIN_TARGETS=26
cd "$(dirname "$0")/.."
pkgs=$(go list ./...)
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
if [ "$n" -lt "$MIN_TARGETS" ]; then
	echo "found $n fuzz targets, expected at least $MIN_TARGETS" >&2
	exit 1
fi
for pt in $targets; do
	pkg=${pt%%:*}
	target=${pt#*:}
	echo "== $pkg $target ($FUZZTIME)"
	go test "$pkg" -run '^$' -fuzz "^${target}\$" -fuzztime "$FUZZTIME"
done
echo "fuzzed $n targets for $FUZZTIME each"

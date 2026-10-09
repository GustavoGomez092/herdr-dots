#!/bin/sh
# Runs every native Go fuzz target (func FuzzXxx) in the module for a fixed
# time each: 30s by default (pull requests), 10m in the release job:
#   sh scripts/fuzz.sh 10m
# A crash stops the run; Go saves the input under testdata/fuzz/<Target>/ in
# that package. Commit it with the fix so it stays a regression test.
set -eu
FUZZTIME="${1:-30s}"
cd "$(dirname "$0")/.."
n=0
for pkg in $(go list ./...); do
	for target in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
		echo "== $pkg $target ($FUZZTIME)"
		go test "$pkg" -run '^$' -fuzz "^${target}\$" -fuzztime "$FUZZTIME"
		n=$((n + 1))
	done
done
echo "fuzzed $n targets for $FUZZTIME each"

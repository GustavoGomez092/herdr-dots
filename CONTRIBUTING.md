# Contributing

## Build and test

    make build                 # bin/herdr-dots (download-verified or source)
    make test                  # go test -race ./...
    make vet
    make cross                 # Linux and Windows builds, Windows vet, compiled Windows tests
    make fuzz FUZZTIME=30s     # every fuzz target
    make soak SOAK_DURATION=15m
    make vulncheck
    make snapshot              # the six release archives in dist/, never published

Run every `go` command with `GOTOOLCHAIN=local`. Tests use temp dirs only:
never your real Herdr session, state dir, `~/.claude*` or `~/.codex`.
For live testing use a separate session (`herdr --session dots-test`) and
never `herdr plugin link` in your everyday session (links are global).

## Rules the code follows

- **Memory is the core.** Memory text goes only in the user prompt. Models can
  write dot findings and notes; project memory they write waits for approval;
  they never write dismissed, preference or global entries. Only outcome and
  user findings suppress repeats. Secrets are scanned.
- Model text goes through `textutil.SanitizeModelText` before it is shown,
  stored or sent.
- Engine locks: paneLock → e.mu → stores, and dmu → e.mu/stores. No file, git
  or network I/O under those locks; background work goes through `e.spawn`.
- UIs act on what is on screen: select by id, show the full text before an
  approval.
- No `runtime.GOOS` branches in product code: OS differences live in
  `internal/platform` or `//go:build` files.
- Every git call passes `-c core.hooksPath=<null device>` and
  `-c core.fsmonitor=false` (a test enforces it).

## Persisted files and schema versions

Every whole-file JSON or TOML store is a kind in `internal/migrate/kinds.go`.
Its version is the top-level `version` key; a missing key means 1.

- Adding a file: add a kind (and its `paths.Paths` getter to `KindPaths`), or
  list the getter in `Unversioned` with the reason.
- Adding optional fields: no version change.
- Changing the meaning or shape of a field: bump the kind's `Current`,
  register a `Migration` from the old version (it must set `version` in its
  output), and add a fixture test. A state-root kind needs a `Lock` before it
  can be migrated.
- Append-only JSONL logs (feed, memory ledger, history) only ever gain
  optional fields; a breaking change uses a new file name.

## Releases

`internal/version/VERSION` is the exact version the binaries report. A
release candidate is a manual run of `.github/workflows/release.yml` on
`main` (Run workflow, no tag): it tests, fuzzes, runs govulncheck and the
end-to-end install test, builds the archives and opens a pull request with
`release/SHA256SUMS` and `release/ASSET_ENV`. Publishing is the tag
`v<VERSION>` pushed on that pull request's merge commit: the workflow
rebuilds, refuses archives that differ from the committed checksums, signs
them and drafts the release (a prerelease for `X.Y.Z-rc.<digits>`).
`scripts/release-kind.sh` holds the checks; any other VERSION suffix is
refused. Publishing is the owner's step: see
`docs/release/ready-to-publish.md`.

## Commits

Small commits with a type prefix (`feat:`, `fix:`, `test:`, `docs:`,
`build:`). Never commit secrets, `bin/` or `dist/`.

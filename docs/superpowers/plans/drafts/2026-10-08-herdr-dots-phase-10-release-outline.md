# Herdr Dots Phase 10: Rename, Hardening and Marketplace Release (1.0), Implementation Plan Outline

**Status: DRAFT, pending user review**

> **This is an outline, not an executable plan.** Earlier phases' code doesn't exist yet. When Phase 10 starts, write the full step-by-step plan with superpowers:writing-plans against the real code, then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Rename the product away from OpenAI's "Dots", migrate existing installs losslessly, version and fuzz every persisted format, meet the resource budgets, ship verified prebuilt binaries, publish on the Herdr marketplace, and pass the feature-complete acceptance run that alone allows tagging `1.0.0`.

**Architecture:** A one-time `rename` copier plus a per-file `migrate` registry run at engine start. Legacy delivery headers stay parseable. GoReleaser builds six targets; the `[[build]]` scripts download a release asset and verify it against checksums committed in the repo, falling back to `go build`. CI adds fuzzing, `govulncheck` and a soak test. Docs, the threat model and a final security review complete the release.

**Tech Stack:** Go 1.25; GoReleaser; cosign (keyless); `govulncheck`; GitHub Actions; existing dependencies.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-10-release-design.md`

## Global Constraints

- "Feature complete" and `1.0.0` may be used **only** after the acceptance run (Task 8) passes on macOS, Linux and Windows. Until then, versions stay `0.9.x` and docs say "release candidate".
- The README roadmap keeps Phase 2 (dot memory) marked **VITAL** in its history table.
- The name, plugin id and per-agent noun come from the user's answer to spec O1/O2. Tasks use `<newid>` until then.
- Migration copies, never moves. Old dirs stay untouched. A failed copy rolls back.
- Never run an unverified downloaded binary.
- No telemetry.
- `go test -race ./...`, `go vet ./...` and `govulncheck ./...` on all CI OSes before every commit; fuzz targets 10 minutes each in the release job.

## Review Focus

1. **A user upgrading with an engine still running under the old id.** Expect the new engine to coexist (different socket and lock under different dirs), migration to read the old files consistently (copy under the old memory locks), and no corruption. (Task 3)
2. **A `config.toml` whose keybindings mix `herdr-dots.*` with the user's own entries and comments.** Expect `setup-keys --migrate` to rewrite only the plugin's commands, keep everything else byte-identical, and back up first. (Task 3)
3. **An offline install** (no network during `[[build]]`) on a machine without Go. Expect a clear, actionable error and nothing half-installed in `bin/`. (Task 5)
4. **A state file from 0.3 (no `version`) next to one from a future 1.2.** Expect the first migrated and backed up, the second read-only with a notice, and the engine running. (Task 2)
5. **Hours of heavy use with big memory and history.** Expect snapshot broadcasts to stay ≤ 256 KB and ≤ 10/s, and RSS < 80 MB. (Task 6)

---

## File Structure

```
go.mod                               module rename
cmd/<newid>/                         binary rename (+ migrate.go, uninstall.go, verifyrelease.go)
internal/migrate/migrate.go          registry, Run, backups
internal/migrate/v*.go               per-file migrations
internal/rename/rename.go            FindLegacy, CopyTree, markers, rollback
internal/transcript/turn.go          new header, legacy parsing
internal/keys/keys.go                new ids, Migrate
internal/mcpinstall/install.go       new server name, PlanMigrate
internal/paths/paths.go              LegacyDirs
internal/engine/engine.go            snapshot throttling and trimming
internal/ui/style/style.go           NO_COLOR
herdr-plugin.toml                    new id, actions, platform commands
scripts/build.sh, scripts/build.ps1  download → verify → install, else go build
release/SHA256SUMS                   committed checksums
.goreleaser.yaml
.github/workflows/release.yml, ci.yml (fuzz, govulncheck, soak)
internal/**/fuzz_test.go             fuzz targets
test/soak/soak_test.go               //go:build soak
docs/security/threat-model.md
docs/guide/*.md
README.md, SECURITY.md, CHANGELOG.md, CONTRIBUTING.md, LICENSE
docs/superpowers/notes/1.0-acceptance.md
```

---

## Tasks

### Task 1: Name check and verification (V1–V4)

**Files:** Create `docs/superpowers/notes/phase-10-verification.md`.
**Interfaces:** Findings only, plus the user's final name choice recorded verbatim.
**Tests:** None. STOP until the user has chosen the name (O1) and license (O3).

### Task 2: Schema versions and the migration framework

**Files:** Create `internal/migrate/*.go`. Modify every persisted struct (`state` attachments/usage/feed/daily, `queue`, `memory` files, `history`, `config`, `dots`, `credentials`, `trusted.json`, `mcp-install.json`). Test: `internal/migrate/migrate_test.go` with fixture dirs `internal/migrate/testdata/v0.1 … v0.9`.
**Interfaces:**
- Produces: `type Migration struct{ File string; From, To int; Apply func([]byte) ([]byte, error) }`; `Register(m Migration)`; `type Report struct{ Migrated, ReadOnly []string; Backups []string }`; `Run(p paths.Paths, logf func(string, ...any)) (Report, error)`; `Version(data []byte, format string) (int, error)`; `Version int` fields on all persisted structs (missing → 1).
**Tests:** every fixture version migrates to current; a second run is a no-op; a newer version → read-only with a report entry; a failed migration restores the backup; a corrupt file is left untouched and reported.

### Task 3: Rename and migration of installs

**Files:** Create `internal/rename/rename.go`, `cmd/<newid>/migrate.go`. Modify `go.mod` and all imports, `herdr-plugin.toml`, `internal/transcript/turn.go`, `internal/keys/keys.go`, `internal/mcpinstall/install.go`, `internal/paths/paths.go`. Test: `internal/rename/rename_test.go`, extend `transcript`, `keys`, `mcpinstall` tests.
**Interfaces:**
- Produces: `rename.FindLegacy(oldID string, lookup func(id, kind string) (string, error)) (config, state string, ok bool)`; `rename.CopyTree(src, dst string, skip func(rel string) bool) (int, error)`; `rename.Migrate(oldID string, p paths.Paths) (rename.Result, error)`; `type Result struct{ Files, Dots, Projects int; Skipped []string }`; `(paths.Paths) LegacyDirs(oldID string) (string, string, bool)`; `transcript.DotPrefix` → new header, `FromDots` parses old and new; `keys.Migrate(data []byte, oldID, newID string) ([]byte, int)`; `mcpinstall.PlanMigrate(oldName, newName string) (Plan, error)`; `runMigrate(args []string) error` (`--from`, `--yes`); `setup-keys --migrate`, `setup-mcp --migrate`.
**Tests:** copy skips session runtime files (`engine.lock`, `dots.sock`, `engine.log`); modes preserved (Unix) and the DACL preserved (Windows build); rollback on an injected copy error; the copy holds the old memory locks while copying memory dirs; `FromDots` legacy and new headers (golden); `keys.Migrate` rewrites only plugin commands (golden `config.toml` with comments); the MCP migrate plan removes old and adds new.

### Task 4: Fuzzing, `govulncheck`, threat model

**Files:** Create `fuzz_test.go` files in `internal/backend`, `internal/transcript`, `internal/memory`, `internal/dotpack`, `internal/schedule`, `internal/ipc`, `internal/keys`; create `docs/security/threat-model.md`; modify `.github/workflows/ci.yml`. Test: the fuzz targets themselves.
**Interfaces:**
- Produces: fuzz targets `FuzzParseReply`, `FuzzParseMemoryOps`, `FuzzParseClaude`, `FuzzParseCodex`, `FuzzFromDots`, `FuzzMemoryLoad`, `FuzzDotpackParse`, `FuzzScheduleParse`, `FuzzIPCDecode`, `FuzzKeysExisting`; a CI job running each for 10 minutes on release tags and 30 s on PRs; `govulncheck` in CI.
**Tests:** each fuzz target seeds from existing unit-test fixtures; invariants (no panic; parse → render → parse stable where applicable; `ParseReply` never returns a relevant verdict with an empty message). The threat model lists every threat with its mitigation and the test that pins it.

### Task 5: Release engineering and the build scripts

**Files:** Create `.goreleaser.yaml`, `.github/workflows/release.yml`, `release/SHA256SUMS`, `cmd/<newid>/verifyrelease.go`. Modify `scripts/build.sh`, `scripts/build.ps1`, `herdr-plugin.toml`. Test: `scripts/build_test.go` (drives the scripts against `httptest` servers in temp dirs), `cmd/<newid>/verifyrelease_test.go`.
**Interfaces:**
- Produces: six release targets named `<newid>_<ver>_<os>_<arch>.(tar.gz|zip)`; build script behavior per spec §6.1 (env `HERDR_PLUGIN_BUILD_OFFLINE=1` forces the source build); `runVerifyRelease(args []string) error` (optional cosign verification when `cosign` is on PATH).
**Tests:** a verified download installs `bin/<newid>`; a checksum mismatch → source fallback and the downloaded file deleted; no network and no Go → a non-zero exit with instructions and an empty `bin/`; Windows script parity (CI Windows job); the `--version` output shows `release` vs `source`.

### Task 6: Performance hardening and the soak test

**Files:** Modify `internal/engine/engine.go` (broadcast throttling and snapshot trimming), `internal/ui/style/style.go` (`NO_COLOR`). Create `test/soak/soak_test.go` (`//go:build soak`). Test: `internal/engine/broadcast_test.go`, the soak test.
**Interfaces:**
- Produces: snapshot broadcasts coalesced to ≤ 10/s; `view.Snapshot` trimmed (feed ≤ 50 entries per pane, cards ≤ 100 per pane) to ≤ 256 KB; `style.NoColor() bool`.
**Tests:** 1,000 state changes in 1 s → ≤ 11 broadcasts and the final state delivered; snapshot size bound with a large fixture; soak: 8 h (CI nightly; 15 min on PRs, scaled) with 12 scripted dots × 4 fake panes: CPU < 1% idle average, RSS < 80 MB, no goroutine growth.

### Task 7: Docs, uninstall, marketplace listing

**Files:** Create `docs/guide/*.md`, `SECURITY.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `LICENSE`, `cmd/<newid>/uninstall.go`. Modify `README.md`. Test: `cmd/<newid>/uninstall_test.go`; a docs link check in CI.
**Interfaces:**
- Produces: `runUninstallExtras(args []string) error` (`--purge`, `--yes`); README sections (overview, install from the marketplace, quick start, privacy and no telemetry, safety model, the roadmap history with Phase 2 **VITAL**); the GitHub topic `herdr-plugin` set on the public repo (a manual step recorded in the notes).
**Tests:** uninstall removes the MCP entries and keybinding block with backups and leaves data; `--purge` needs typed confirmation; the link checker passes.

### Task 8: Feature-complete acceptance run and the 1.0.0 tag

**Files:** Create `docs/superpowers/notes/1.0-acceptance.md`. Modify `internal/version/version.go` (`1.0.0`), `CHANGELOG.md`.
**Interfaces:** Consumes everything.
**Tests:** every item of the roadmap's feature-complete checklist executed on macOS, Linux and Windows with evidence; a final security review (the `security-review` skill plus a human read) with findings fixed; `go test -race ./...`, `go vet ./...`, `govulncheck ./...` green on all OSes. Only after every item passes: set `1.0.0`, tag, release, and use the words "feature complete" in the changelog and README for the first time.

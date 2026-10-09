# Herdr Dots Phase 10: Release Hardening (1.0.0-rc.1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden Herdr Dots for its first public release and stop at a written "ready to publish" checklist: a schema-version and data-migration framework, fuzz targets for every parser, throttled and size-bounded snapshots proven by a soak test, checksum-verified prebuilt binaries with a source-build fallback, `verify-release` and `uninstall-extras`, a triage of every deferred finding, a threat model and security review, user docs and the feature-complete acceptance checklist, shipped as `1.0.0-rc.1`.

**Architecture:** `internal/migrate` knows every persisted file kind (`Default()`), reads each file's top-level `version` (missing = 1), upgrades old files through registered migrations under the right lock with a backup, and marks files a newer binary wrote read-only through a single gate in `fsutil.WriteFileAtomic`. The engine coalesces snapshot broadcasts to at most 10 a second and `view.Trim` keeps each snapshot under 256 KB without ever cutting a card. GoReleaser builds six archives; `scripts/build.sh` and `build.ps1` download the archive for the host, verify it against `release/SHA256SUMS` from the cloned tree, and otherwise build from source. Nothing is pushed, tagged, listed or published: those are user gates.

**Tech Stack:** Go 1.25 (existing dependencies only, no new module). Native Go fuzzing. GoReleaser v2 config (not run locally unless installed), cosign keyless (in the release workflow only), `govulncheck` via `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4`, GitHub Actions (written, never run here), POSIX sh and PowerShell for the install scripts, `actionlint` and `shellcheck` (installed here) for linting.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-10-release-design.md`, approved with the amendments in `docs/superpowers/decisions/2026-10-08-phase-decisions.md`, which win where they differ: ruling 1 keeps the name "Herdr Dots" (`herdr-dots`) and asks for a data-migration framework instead of a rename; ruling 16 adds no license; the release-gate amendments forbid every outward-facing action, put the migration framework, fuzzing, threat model, soak test and acceptance checklist in scope, stop at a "ready to publish" checklist, and leave the three-OS acceptance run, the live provider checks and the Codex live tests to the user. This plan replaces the outline `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-10-release-outline.md`.

**Preconditions (check before Task 1; stop and report if any fails):**
- The base is `feat/dots-v1` with Phases 2–9 merged (reconciled against `dffabbf`, "merge: P9 Task 12 README, roadmap, notes, 0.9.0"), plus Phase 9's final fix wave if it has landed. These exist and every name this plan uses was checked there: `internal/council`, `internal/credentials`, `internal/pricing`, `internal/secrets` (Phase 9's shared scanner), `internal/backend/toolloop`, `internal/mcpinstall`, `internal/mcpserver`, `cmd/herdr-dots/setupmcp.go` (`runSetupMCP`), `internal/platform` with its portability guard (Phase 6), `internal/worktree` and `internal/patchstore` (Phase 7), `internal/schedule`, `state.OpenDaily`, `queue.OpenInbox` (Phase 8), `internal/history`, `internal/dotpack`, `internal/projectdots` (Phase 9). `internal/version/version.go` says `const Version = "0.9.0"` and Phase 9's `TestVersionIsPhaseNine` pins it.
- Phase 9's final fix wave landed on `feat/dots-v1` as `0205616` while this pass ran (it touches `internal/dots/store.go`, `project.go`, `internal/memory/store.go`, `internal/ui/memui/model.go`, `internal/dotpack/dotpack.go` and the README, and adds no getter, persisted file or version pin). The final plan text was replayed on `0205616` too: every diff applied and the full verification set passed (decision 41). Later changes (Phase 9's residual rounds) may move lines in files this plan edits (`internal/engine/engine.go`, `cmd/herdr-dots/main.go`, `cmd/herdr-dots/engine.go`, `internal/memory/store.go`, `internal/dots/store.go`, `internal/ui/memui/model.go`, `README.md`). Every diff carries its exact context; if `git apply` refuses a hunk, make the same edit by hand and run `gofmt -w` on the file. If it adds a `paths.Paths` getter, a persisted file, a version pin or a deferred finding, Tasks 2, 5 and 9 say where it goes.
- `GOTOOLCHAIN=local go test -race ./... && GOTOOLCHAIN=local go vet ./...` passes on the base (darwin), and `make cross` (Phase 6) passes (both verified at `dffabbf`).

## Global Constraints

Phase 10 constraints:
- **Version `1.0.0-rc.1`, never `1.0.0`.** `internal/version/VERSION`, `herdr-plugin.toml` and every test that pins the version say `1.0.0-rc.1`. The `1.0.0` version, its tag, the license and publishing are user gates.
- **No outward-facing action, ever:** no `git push`, no tag (local or remote), no GitHub topic, no marketplace listing, no `gh` command that writes, no GoReleaser publish. `goreleaser` runs only as `goreleaser release --snapshot --clean --skip=publish,sign` (or `goreleaser build --snapshot`), only if `command -v goreleaser` finds it; never install it, never run `goreleaser release` without `--snapshot`.
- **No license.** No `LICENSE` file is added (ruling 16). Docs say "License: not chosen yet" and the ready-to-publish checklist lists it as a blocker.
- **The name stays "Herdr Dots"** (plugin id, module, binary `herdr-dots`), ruling 1. The `[◆ Name dot]` delivery header and `transcript.FromDots` are unchanged.
- **Wording.** The product may be called "feature complete on paper" only in Task 13's README and acceptance notes, and always with the caveats: unverified on Linux and Windows hardware, live providers and Codex not verified in the acceptance run, license and publishing pending. Nothing may say released, ready, done or finished. Until Task 13, docs say "release candidate, not released".
- **Migration safety:** a file is only ever rewritten atomically, after its original is saved as `<file>.bak-v<old>` (mode 0600), and only under the lock that guards it; a failed migration leaves the file untouched and read-only; a file a newer version wrote is never written (`fsutil.ErrNewerVersion`).
- **Install safety:** `scripts/build.sh` and `build.ps1` never run or keep an archive that failed the checksum from `release/SHA256SUMS` in the cloned tree; a failed install leaves `bin/` without a half-installed binary.
- **Snapshot budgets (spec F9):** at most 10 broadcasts a second, at most 256 KB per snapshot, at most 100 cards and 50 feed entries per pane; a card is shown whole or not at all.
- **No telemetry** (spec F10): nothing phones home; the only network traffic the plugin itself makes is the install-time download and the backends the developer configures.
- Every task's verification runs, from the repo root, "The verification gate" below: `gofmt -l internal cmd scripts` (prints nothing; not `gofmt -l .`, which walks into `.claude/worktrees` and `.worktrees` checkouts), `go vet` (also with `-tags soak` for the engine), `go test -race`, the Linux build, the Windows build and vet, and `GOOS=windows go test -c` for the packages the task touched (Phase 6 made Windows build clean).
- **Phase 6's portability guard** (`internal/platform/portability_test.go`): product code starts every child process with `platform.Command`/`CommandContext`, sets `cmd.Env = platform.ChildEnv(...)`, and has no `runtime.GOOS` branch. Tests that check modes skip or use `platformtest.AssertPrivateFile` on Windows, and test paths are built with `filepath.Join` so they are absolute on every OS.

All earlier Global Constraints still apply (v1 through Phase 9):
- **Phase 2 memory is VITAL.** Memory text goes only in the user prompt; model writes follow the op rules (project scope is proposed until approved; models never write dismissed, preference or global entries, never edit user entries or other dots' entries); only outcome and user findings suppress repeats; secrets are scanned. Phase 10 changes to memory code (Tasks 3 and 9) must keep `go test ./internal/memory/ ./internal/engine/` green, including the Phase 2 VITAL tests.
- Model text is sanitized (`textutil.SanitizeModelText`) before display, storage or delivery.
- Engine lock order: paneLock → e.mu → stores, and dmu → e.mu/stores. No store, git or network I/O under these locks. Background work goes through `e.spawn` (Engine.Wait). Task 4's broadcast lock is a leaf lock.
- UI rule: never act on an unseen selection. Select by id, show the full text before any approval (Task 10 enforces it for memui's approve screen in tiny popups).
- Config reloads at runtime; safety switches fail closed on a malformed config.
- No `runtime.GOOS` branches in product code (Phase 6's portability guard); OS differences live in `internal/platform` or `//go:build` files. Test code may skip by OS.
- Live steps: only `herdr --session dots-test`; never `herdr plugin link` (it is global); never type Claude slash commands into panes; snapshot and diff `~/.claude/settings.json` around every live run; never touch `~/.config/herdr/config.toml` (read only) or the user's sessions.
- Codex is not installed here: Codex paths stay "documented-format, not live-verified".
- Tests never touch the real Herdr session, `STATE_DIR`, `CONFIG_DIR`, `~/.claude*` or `~/.codex`; use `t.TempDir()`. Run every `go` command with `GOTOOLCHAIN=local`.
- State files `0600`, directories `0700`, atomic writes; keys never reach logs, the feed, snapshots, IPC, memory, argv or error strings.

## Plan decisions

No human was available; every judgment call is recorded here. Each can be reversed.

1. **No rename, so no `internal/rename`, no legacy-header parsing, no `setup-keys --migrate` or `setup-mcp --migrate` (ruling 1).** The spec's F1–F4 and its §4.1 renames (module, binary, manifest id, `DotPrefix`, `PlanMigrate`, `LegacyDirs`) are dropped. What ruling 1 keeps is the data-migration framework (Task 2): a later rename becomes one more registered migration plus a copier, without touching the stores. The "migrated from" notice and `migrate --from` are dropped with the rename; `herdr-dots migrate` (Task 7) runs schema migrations instead.
2. **Missing `version` = 1, and 1.0 writers are unchanged.** Spec F7 says every file "gains `version`". Every kind is at version 1 in 1.0 and "missing means 1", so the framework reads versions without any store writing a new key. This keeps every 1.0 file byte-identical to what 0.9 wrote (so a downgrade to 0.9 still loads), keeps Phases 3–9's structs untouched in one task (no merge conflicts across nine phases), and avoids a real hazard: `attachments.json` and `usage.json` are JSON objects keyed by pane id, where a top-level `version` key would be decoded as a pane and break the loaders. The first kind that reaches version 2 writes its key in the migration that creates it (and `Probe` reads a numeric top-level `version`, which a pane-keyed map can never hold). Files that already carry `version` (memory, `mcp-install.json`, Phase 8's `schedule.json`/`daily.json`) keep it. (Refined by decision 24, reconciled: `trusted.json` carries `version` too, and `credentials.toml` and `inbox.json` join the map-shaped and versionless files.)
3. **Read-only mode is one gate in `fsutil.WriteFileAtomic`.** Every state and config write goes through it (verified for v1–Phase 2; Phases 3–9 follow the same rule). `migrate.Run` calls `fsutil.ProtectNewer(path)` for a newer file and `WriteFileAtomic` then returns `fsutil.ErrNewerVersion`; the store keeps working in memory and logs the failed save. Append-only JSONL logs (feed, memory ledger, Phase 9 history) are not kinds: their records only gain optional fields, and a breaking change must use a new file name (rule written in `CONTRIBUTING.md`). (Reconciled: verified at `dffabbf` for every writer of a kind, Phases 3–9 included; see decision 24.)
4. **Who migrates what.** The engine runs `Run` at start, holding its session lock, over config, shared state and its own session dir. State-root kinds are shared by every session's engine, so a migration can be registered only for a state-root kind that declares its lock (memory's `memory.lock`; Phase 8's `daily.json` declares none yet, so it can't be migrated until it does: `Set.Validate` enforces this). Processes that write outside the engine (`manage`, `keys`, `dots`, `setup-mcp`, `uninstall-extras`) run a read-only `DryRun` first so they too refuse to overwrite newer files; memory kinds are skipped there because the memory store already refuses newer files itself (`memory.ErrVersion`). `herdr-dots migrate` skips session dirs whose engine is running. (Refined by decision 24, reconciled: `daily.json`, `inbox.json` and `trusted.json` now declare their `<file>.lock`; `memory` and `mcp` are left out of the writing commands, decision 37.)
5. **Legacy fixtures are the formats the code really wrote.** `legacy_test.go` holds 0.1, 0.2 and 0.3 files byte-exact from the v1/Phase 2/Phase 3 code and loads them through the real loaders after `Run`. Files Phases 4–9 introduced are produced by those phases' own writers in `writers_test.go` (`credentials.File.Set`, `mcpinstall.SaveRecords`, `TrustStore.Trust`, `state.Daily.Add`, `memory.Store.ClaimSchedule`, `history.Store.Append`), which is exactly what 0.4–0.9 write because Phase 10 changes no writer. `TestEveryPathsFileIsClassified` makes every `paths.Paths` getter either a kind or an explicitly unversioned path, so a later phase can't add a file silently. (Refined by decision 24, reconciled: `writers_test.go` uses the merged writers and adds `patchstore.Put`, `Inbox.Put` and `SeeDots`.)
6. **Fuzzing found two problems while planning; one is a product bug.** A short run of the new targets found that `memory.ExportTOML` writes `ref = "d:"` for an entry that has no id yet, which `ImportTOML` then rejects (crasher `FuzzImportTOML/8a748f204ecc8c6f`, committed as a regression input; fixed in Task 3). It also showed that `ParseStance` maps invalid UTF-8 to U+FFFD (the fuzz invariant counts runes, not bytes). Task 3 runs every target for 60 s and fixes what it finds in the same task, committing each crasher under `testdata/fuzz/`. (Reconciled: the `ExportTOML` bug is still present at `dffabbf`; the reconcile pass's own fuzz run found a third problem, decision 26.)
7. **Fuzz targets beyond the spec's list:** `council.ParseDigest`/`ParseStance` (chair output reaches the queue), `config.Load` (the kill switches must fail closed on every input), the dot file loader (dots are shared files in Phase 9), `memory.ScanSecrets`, the scope-file loader, the Codex config block editor (`mcpinstall`, it edits the developer's `~/.codex/config.toml`) and the credentials file reader. `IPC request decoding` is fuzzed at `ipc.serveConn` over `net.Pipe`, which covers every command's JSON decode path. (Extended by decision 26, reconciled.)
8. **Snapshot trimming never cuts text.** The spec suggested trimming; cutting a card body would show the developer one text and send another, which breaks the UI rule. `view.Trim` keeps whole cards in queue order (most severe, then oldest), drops feeds to 5 entries first, then drops the last card of the fullest pane one at a time, down to one card per pane. `Snapshot()` trims outside `e.mu`.
9. **Broadcast throttle:** leading edge plus one trailing broadcast. The first change after a quiet 100 ms goes out at once; later changes in the window coalesce into one trailing broadcast (spawned through `e.spawn`) that carries the final state. Measured while planning: 1,000 changes in 1 s give at most 11 broadcasts.
10. **The soak test lives in `internal/engine` (`//go:build soak && (darwin || linux)`),** not `test/soak/`: it reuses the engine test rig (fake Herdr server, fake backend, fake readers) instead of duplicating it. CPU and peak RSS come from `getrusage`. A 45 s smoke while planning gave 14.4 MB peak RSS, 0.58 % idle CPU and 19 KB snapshots. GitHub-hosted jobs stop at 6 hours, so CI runs 15 minutes on pushes and 5 hours nightly; the 8-hour run happens locally in Task 13 (or, if the session can't wait, it becomes a user gate with the longest completed run recorded). (Reconciled numbers at `dffabbf`, decision 28.)
11. **`NO_COLOR` needs no code.** lipgloss v1.1.0 picks its color profile with termenv's `EnvColorProfile` (`renderer.go:74`), which returns the no-color profile when `NO_COLOR` is set (`termenv.go:69`); bold, faint and reverse stay, so selection and severity stay visible. The spec's `style.NoColor()` is dropped; the README documents `NO_COLOR`.
12. **Version source of truth is `internal/version/VERSION`,** embedded with `go:embed`, so the install scripts read the same value. `version.Source` (`release`/`source`) and `version.Commit` come from `-ldflags -X`; without them `CommitID()` reads Go's VCS stamp. `herdr-dots version` and `--version` print `herdr-dots 1.0.0-rc.1 (source, commit 1a2b3c4d5e6f)`.
13. **Where the repository comes from.** Herdr installs with `git clone` (herdr.dev/docs/plugins), so `build.sh` reads `owner/repo` from `release/REPO` or, when that file is empty, from the clone's GitHub `origin` remote. Both files under `release/` are committed empty in 1.0.0-rc.1: no release exists, so every install builds from source; the release workflow fills `SHA256SUMS`, and the developer fills `REPO` if Herdr's clone has no usable origin.
14. **Release flow (written, never run):** a `v<VERSION>-rc.N` tag runs the three-OS tests, a 10-minute fuzz pass per target and a GoReleaser build with nothing published, and opens a pull request that commits `release/SHA256SUMS` and `release/ASSET_ENV` (version, asset commit, timestamp). A `v<VERSION>` tag on the merged commit rebuilds from the pinned inputs, refuses to continue unless the archives are byte-identical to the committed checksums, signs them with cosign (keyless) and creates a **draft** release. The spec's "rc commits SHA256SUMS" can't put the checksums in the rc commit itself; the PR plus reproducible rebuild keeps the spec's guarantee (the tree a user installs carries the checksums of the assets they download). Reproducibility (`-trimpath`, `-buildvcs=false`, pinned `mod_timestamp` and archive `mtime`) is unverified without GoReleaser; if it doesn't hold, the publish job fails closed at its `diff`.
15. **Six targets, as the spec says,** including `windows/arm64`, which Phase 6 left unsupported. It is cross-compiled and published like the others; the README marks it "built, not tested".
16. **`build.ps1` and `build.sh` run the same steps,** and their tests share fixtures. The PowerShell tests run wherever `pwsh` or `powershell` exists (CI's ubuntu and windows runners); here they skip. Phase 6's `TestWindowsBuildScriptBuildsTheExe` pinned strings of the old script; Task 6 updates it. (Reconciled: the update is now a diff, Task 6 Step 4.)
17. **`release-local.sh`** builds the same six archives and `SHA256SUMS` with plain `go build`, `tar` and `zip` when GoReleaser isn't installed, so the install path is tested end to end with real binaries (`HERDR_DOTS_E2E_RELEASE=1`, verified while planning for darwin/arm64 and linux/amd64).
18. **`verify-release`** re-hashes the kept archive (`bin/release/<asset>`) against `release/SHA256SUMS`, checks that `bin/herdr-dots` is the binary inside it, and runs `cosign verify-blob` with the repository's release-workflow identity when cosign and a `.sigstore.json` bundle exist. Without cosign it says so; it never downloads anything.
19. **`uninstall-extras`** removes the MCP entries through Phase 5's `setup-mcp --remove` (its own preview, confirmation and backups) and the keybindings through a new `keys.RemoveBlock`, which deletes only `[[keys.command]]` tables whose command is `herdr-dots.*` and keeps every other byte (a setup-keys round trip is byte-identical). `--purge` deletes the config and state dirs only after the developer types `purge herdr-dots`, even with `--yes`, and never while an engine runs.
20. **Triage of deferred findings (Tasks 9 and 10).** Folded: the four notable Phase 2 items (git config execution in hostile repos, zero-width secret evasion, the nested-repo `FindProject` prefix match, the memui approve screen in tiny popups), plus Unicode bidi controls in model text (an approval-screen spoof), `DeleteDot` leaving ledger excerpts (the Phase 2 completion note scheduled it for "a later phase"; none took it), dot slugs that could name files outside the dots dir, a feed file that kept a loose mode, and two panics (`TruncateRunes(n<=0)`, `Feed.Recent(n<0)`). Everything else is a documented known issue with its reason. The rule for items Phases 3–9 add to their ledgers before Phase 10 runs is in Task 9. (Refined by decisions 29–32, reconciled: several of these were already fixed by Phases 6–8, and every ledger, v1 to Phase 9, is triaged.)
21. **Git calls get a guard test.** Beyond fixing `memory.ExecGit` (`core.fsmonitor=false`, `core.pager=cat`, `protocol.allow=never`, `GIT_NO_LAZY_FETCH=1`), `TestEveryGitCallDisablesRepositoryPrograms` fails if any product file that starts git (including Phase 7's `internal/worktree`) lacks `core.fsmonitor=false` and `core.hooksPath=`. (Refined by decision 29, reconciled: `ExecGit` already has every flag but `core.pager=cat`, which a non-terminal git never uses; the guard test stays as a pin.)
22. **The spec's manage-popup footer ("Outriders 1.0.0 · MCP ✓ · keys ✓") is dropped:** it belonged to the rename, Phase 5 already shows the MCP status there, and `--version` reports the version. The footer is a churn-heavy file shared by Phases 3–9.
23. **Security review:** Task 11 runs the Claude Code `security-review` skill over the whole tree (not just a diff) and writes the human-read notes; findings rated High or Critical are fixed in Task 11, the rest go to the known issues. `govulncheck` runs through `go run`; if the module can't be downloaded (offline), that is recorded and becomes a user gate.
24. **Every persisted file, with its version (reconciled).** The inventory in Task 2's Interfaces block was read from the code at `dffabbf`. Kinds: config, dot, credentials, prices, mcp-install, trust (`trusted.json.lock`, self-guarded by `projectdots.ErrNewer`), memory-scope, memory-meta, schedule (all `memory.lock`, self-guarded by `memory.ErrVersion`), daily (`daily.json.lock`, self-guarded by `state.ErrDailyVersion`), inbox (`inbox.json.lock`), attachments, queue, usage, patch-meta. `Appended` names the three JSONL logs (feed, memory ledger, history) and `External` the formats read from outside the plugin's directories (`.dotpack.toml` format 1, the memory TOML export version 1, repository project dots). `TestEveryPathsFileIsClassified` covers every getter, `InboxFile` included.
25. **Keys never reach a report (reconciled).** `credentials.toml` is a `Secret` kind: a parse error quotes the offending line, so its `Corrupt`/`Failed` text is replaced (`TestBrokenCredentialsAreReportedWithoutTheirText`). Backups are written with `fsutil.WriteFileAtomic(…, 0o600)`, so they get the owner-only DACL on Windows too, and the test checks them with `platformtest.AssertPrivateFile`.
26. **Twenty-four fuzz targets (reconciled).** Added for the parsers the merged phases introduced: `FuzzParseCLIOutput` (claude and codex CLI output), `FuzzDecodeStrict` (`dots.DecodeStrict` and `DecodeProject`), `FuzzDiffHeaders` and `FuzzUnquoteC` (Phase 7's diff header parser), `FuzzDiffViewParse` (the patch viewer). `FuzzDotpackParse` uses the real `Export(d, mem, ExportOptions)` and `Prepare` (there is no `ClampPack`; idempotence is "clamping a prepared dot again changes nothing", `Clamp.Fact`). `FuzzCredentialsFile` checks `Last4` as `"…"` plus four bytes; `FuzzImportTOML` also compares `Source` and `From`. A 10 s run found that `FuzzCodexBlock`'s invariant was wrong (crasher `a763424096dd21b2`, kept); then every target passed 30 s. Not fuzzed, on purpose: the MCP server's JSON-RPC (the go-sdk decodes it into typed tool inputs), `herdr.ParseInvocationContext` (`encoding/json` into a flat struct), `worktree.parseRaw`/`parseNumstat` (git's own output) and the scrollback reader (a plain-text tail).
27. **The first-`{`-to-last-`}` extraction stays (reconciled; ruling P8-R7).** `ParseVerdict`, `ParseReply` and `ParseMemoryOps` still take that span. It fails closed (two objects never parse as one, so a wrong verdict is never accepted, only a malformed one), the fuzz targets check every accepted reply is sanitized and capped, and the triage lists it as a known issue; the string-aware extractor P8-R7 promised did not land in Phase 8 and is follow-up work, not a release blocker.
28. **Throttle and soak against the merged engine (reconciled).** `broadcast` resets `pending` when `e.spawn` refuses after `Wait`, so a stopping engine never wedges the throttle. The merged engine already caps each pane's feed at 20, so `MaxFeedPerPane` (50) is a backstop. The 60 s soak at `dffabbf`: 240 runs, 57 broadcasts, biggest snapshot 23 KB, peak RSS 21.8 MB, idle CPU 0.58 %. All engine, UI, IPC and MCP tests pass with the throttle.
29. **Already fixed, so only pinned (reconciled).** Phases 6–8 gave every git child `core.fsmonitor=false`, the null hooks path, `protocol.allow=never`, `GIT_ALLOW_PROTOCOL=none` and `GIT_NO_LAZY_FETCH=1`; `SanitizeModelText` drops bidi controls; Phase 7 Task 0 made `DeleteDot` scrub ledger lines, asides and orphaned proposals. Task 9 drops the old `ExecGit`, `gitEnv`, `sanitize.go`, `bidi_test.go` and `DeleteDot` edits and keeps `TestExecGitRunsNothingTheRepositoryConfigures` and `TestEveryGitCallDisablesRepositoryPrograms` as pins (both pass at `dffabbf`).
30. **The invisible-character fix goes in `internal/secrets` (reconciled).** Phase 9 moved the patterns to `secrets.Scan`, which `memory.ScanSecrets`, `dots.Encode`, dot packs and history call, so the fix there covers every caller. `TestFindProjectPrefersTheNestedRepository` names its projects so the outer one sorts first (`Store.projects()` sorts by name), or it would pass on the old code by luck.
31. **Slug checks are file-name safety, not `Slugify`'s form (reconciled).** `Slugify(slug) == slug` would stop the manage popup from editing a dot file the developer named by hand (`My_Dot.toml`). `validSlug` refuses what can name another file (empty, `/`, `\`, `:`, NUL, a non-local name per `filepath.IsLocal`, which also refuses Windows device names). Project dots never reach `Load`/`Save`/`Delete`.
32. **Two flaky tests, fixed in their tests (reconciled).** `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies`: `startStub`'s stop returned before `ipc.Serve` closed its listener, and a closing Unix listener unlinks its path, so it could delete the next stub's socket; the stop now waits for `Serve`. The Phase 7 draft tests: the rig's `eventually` gave git-heavy drafts 3 s; it now allows 15 s. Both ran green under `-race` with load (`-count=50`; drafts `-count=6` beside two other packages).
33. **Version pins and manifest (reconciled).** One test pins the version at `dffabbf` (`TestVersionIsPhaseNine`, renamed `TestVersionIsTheReleaseCandidateInTheBinary`); the manifest edit is a diff; `keys.Defaults()` has four bindings (Phase 3 added council), which `TestRemoveBlockKeepsEverythingElseByteForByte` now counts. A binary without Go's VCS stamp prints `commit unknown`.
34. **Live provider checks are user gates (reconciled).** Task 13 no longer runs `go test -tags live` or the provider part of the macOS smoke (a card from a Claude pane, then `suppressed`); both join the ready-to-publish checklist with the API backends and Codex. The memui part of the smoke stays, with a proposal made by `memory import`.
35. **`verify-release` starts cosign through `internal/platform` (reconciled).** Phase 6's portability guard failed on `exec.Command`; it now uses `platform.Command`, `platform.ChildEnv` and `platform.HideWindow`.
36. **Tests that run on Windows CI (reconciled).** `TestUninstallRemovesMCPAndKeysWithABackupAndKeepsData` checks the config's mode only off Windows; `TestSafeToPurgeRefusesHomeRootAndRelativePaths` builds its paths with `filepath.Join` under a temp dir (`/Users/me` is not absolute on Windows); the migrate backup check uses `platformtest.AssertPrivateFile`.
37. **Writing commands (reconciled).** `manage`, `keys`, `dots`, `setup-mcp` and `uninstall-extras` run the read-only check first. `memory` and `mcp` write only memory files, which refuse newer versions themselves, and the MCP server's start must stay fast, so they are left out.
38. **The verification gate (reconciled).** `gofmt -l internal cmd scripts` (Task 6 adds Go files under `scripts/`; `gofmt -l .` walks into other checkouts under `.claude/worktrees`), `go vet -tags soak ./internal/engine/` as well, and `GOOS=windows go test -c` for each task's packages.
39. **The README moves by its real headings (reconciled).** Task 12's table maps the README as it is at `dffabbf` (`### Backends and keys`, `### Dots MCP server: …`, `### Proactive dots (0.8.0)`, …). The detailed `## Privacy and safety` bullets move verbatim to `docs/guide/privacy-and-safety.md` (the README keeps the short list and links it), and `## Known gaps in this scaffold` merges into `## Known issues`. The wording checks exclude three technical phrases moved verbatim (`Patch ready`, "a finished run", Herdr reporting `done`).
40. **The roadmap edit is Phase 10's own row only (reconciled).** Task 13 replaces the version and increment cells of the Phase 10 row; the checklist stays unchecked.
41. **Replayed on Phase 9's final fix wave as well (reconciled).** `feat/dots-v1` moved to `0205616` ("merge: Phase 9 final fix wave") during this pass. Tasks 1–10 replayed from the final text onto `0205616` apply cleanly and pass `gofmt`, `vet` (and `-tags soak`), the race suite, the Linux and Windows builds and 48 Windows test binaries; `FuzzDecodeStrict`, `FuzzDotFile`, `FuzzDotpackParse` and `FuzzImportTOML` ran 15 s each against the wave's new TOML pre-scan. The wave's three "Parked → Phase 10" lines are in the triage notes.

## Confirmed while planning (2026-10-08)

| # | Question | Finding | Evidence | Assumption holds |
|---|---|---|---|---|
| V1 | Can a plugin find another plugin id's dirs? | Not needed: the name is kept (ruling 1) | decisions doc ruling 1 | n/a |
| V2 | Is the chosen name free (GitHub, marketplace, trademark)? | Not checked here: naming and publishing are the developer's decisions. "Herdr Dots" overlaps OpenAI's "Dots" (spec §2); recorded as a publishing risk in the ready-to-publish checklist | spec §2, ruling 1 | user gate |
| V3 | Network during `[[build]]`; where Herdr installs from | `plugin install` clones with `git` (default branch, or `--ref`), runs build steps after confirmation, gives them no runtime env, documents no network restriction; `plugin link` runs no build; there is no `plugin update` (reinstall) | herdr.dev/docs/plugins (fetched 2026-10-08) | yes: the clone carries `release/SHA256SUMS` and an `origin` remote; tests force offline with `HERDR_PLUGIN_BUILD_OFFLINE=1` |
| V4 | Per-platform `[[build]]` | Item-level `platforms` on `[[build]]` work; Phase 6 already splits `sh scripts/build.sh` / `powershell ... scripts/build.ps1` | Phase 6 plan, decision 2 (Herdr source) | yes |
| T1 | Tools on this machine | `actionlint`, `shellcheck`, `zip` and `ruby` installed; `goreleaser`, `cosign`, `govulncheck`, `pwsh`, `powershell` and `codex` not installed; Go 1.25.7, Herdr 0.8.2 (rechecked 2026-10-09) | `command -v` | goreleaser snapshot replaced by `release-local.sh`; PowerShell tests skip here |
| T2 | Compile check (reconciled 2026-10-09) | Every task was replayed in wave order on `dffabbf` (Phases 2–9 merged) from this plan's text; after each wave `gofmt -l internal cmd scripts`, `go vet ./...` (and `-tags soak`), `go test -race ./...`, the Linux build, the Windows build and vet, and `GOOS=windows go test -c` for the wave's packages passed; `make cross` built 48 Windows test binaries; the 60 s soak, `make snapshot`, the release e2e install, the `migrate --dry-run` scratch run, `actionlint` and `shellcheck` ran; every fuzz target ran 10 s and 30 s | scratch worktree, Go 1.25.7 | yes |

## Review Focus

The five inputs the spec implies but its tests don't exercise, most likely to bite a person first. Each has a test in its owning task.

1. **A file from a newer herdr-dots next to current ones** (a downgrade after trying 1.1). Expect it read, never rewritten by the engine or by `manage`/`setup-mcp`, a toast naming it, and everything else working. Tests: `TestFutureQueueIsReadButNeverRewritten`, `TestNewerFileIsReadOnlyAndNeverRewritten` (Task 2), `TestWritingCommandsProtectNewerFiles`, `TestMigrateCommandReportsAndSkipsRunningSessions` (Task 7).
2. **An install with no network, or a tampered download, on a machine without Go.** Expect a clear message that says what to do, nothing half-installed in `bin/`, and never a binary that failed its checksum. Tests: `TestOfflineWithoutGoFailsWithInstructionsAndAnEmptyBin`, `TestUnreachableServerWithoutGoFailsCleanly`, `TestChecksumMismatchFallsBackToASourceBuild`, `TestArchiveWithoutTheBinaryIsRejected`, `TestPowerShellRefusesAMismatchAndFailsWithoutGo` (Task 6).
3. **A Herdr `config.toml` that mixes herdr-dots bindings with the developer's own tables and comments.** Expect `uninstall-extras` to remove only herdr-dots bindings, keep every other byte, back up first, and say which bindings it couldn't edit. Tests: `TestRemoveBlockKeepsEverythingElseByteForByte`, `TestRemoveBlockCountsBindingsItCannotRemove`, `TestUninstallRemovesMCPAndKeysWithABackupAndKeepsData` (Task 8).
4. **A hostile repository** (a clone whose `.git/config` sets `core.fsmonitor`/`core.pager`, a model-written patch with a forged header, or memory text with zero-width or bidi characters). Expect no program from the repository to run, no symlink created, secrets still caught, and screen text that reads as stored. Tests: `TestExecGitRunsNothingTheRepositoryConfigures`, `TestEveryGitCallDisablesRepositoryPrograms`, `TestInvisibleCharactersDoNotHideASecret` (Task 9), `FuzzDiffHeaders`, `FuzzDiffViewParse` (Task 3), and Phase 2's `TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners`.
5. **Hours of heavy use: a burst of state changes, 150 cards on one pane, huge card bodies.** Expect at most 10 broadcasts a second with the final state delivered, snapshots under 256 KB, no card text cut, and flat memory. Tests: `TestBroadcastsAreCoalescedAndTheLastStateArrives`, `TestTrimStaysUnderTheByteBudgetWithoutCuttingACard`, `TestTrimKeepsOneCardPerPaneEvenOverBudget`, `TestSoak` (Task 4, run long in Task 13).

---

## Parallel execution: dependencies and waves

Each task runs in its own git worktree branched from the wave's base (the base branch with Phases 2–9 merged, plus every earlier wave). Merge every task of a wave, in task order, before starting the next wave. Tasks in the same wave never touch the same file.

| Task | Title | Depends on | Wave |
|---|---|---|---|
| 1 | Verification notes, baseline, tool check, user gates | — | 1 |
| 2 | Migration framework and the read-only gate | — | 1 |
| 3 | Fuzz targets, fuzz script, fixes for what they find | — | 1 |
| 4 | Snapshot throttling and trimming, soak test | — | 1 |
| 5 | Version 1.0.0-rc.1, build info, GoReleaser, workflows, Makefile | — | 1 |
| 6 | Install scripts (verify, fall back) and `verify-release` | Task 5 | 2 |
| 7 | Migrations at startup, read-only protection, `migrate` command | Task 2 | 2 |
| 8 | `uninstall-extras` and `keys.RemoveBlock` | Task 3 | 2 |
| 9 | Hardening triage A: secrets, nested projects, git pins (and the triage notes) | Task 3 | 2 |
| 10 | Hardening triage B: memui approve fit, slugs, feed, panics, flaky tests | Task 3 | 2 |
| 11 | Threat model, security review, `SECURITY.md` | Tasks 6–10 | 3 |
| 12 | User docs: README, guide, CHANGELOG, CONTRIBUTING | Tasks 6–10 | 3 |
| 13 | Acceptance checklist, soak run, ready-to-publish checklist | all | 4 |

**Wave schedule:** W1 = {1, 2, 3, 4, 5} → W2 = {6, 7, 8, 9, 10} → W3 = {11, 12} → W4 = {13}.

**Files per task** (disjoint within each wave):
- Task 1: `docs/superpowers/notes/2026-10-08-phase-10-verification.md`.
- Task 2: `internal/migrate/migrate.go`, `kinds.go`, `migrate_test.go`, `legacy_test.go`, `writers_test.go`; `internal/fsutil/readonly.go`, `readonly_test.go`, `atomic.go`.
- Task 3: `internal/backend/fuzz_test.go`, `internal/transcript/fuzz_test.go`, `internal/memory/fuzz_test.go`, `internal/memory/tomlio.go`, `internal/memory/testdata/fuzz/FuzzImportTOML/8a748f204ecc8c6f`, `internal/council/fuzz_test.go`, `internal/keys/fuzz_test.go`, `internal/config/fuzz_test.go`, `internal/dots/fuzz_test.go`, `internal/ipc/fuzz_test.go`, `internal/schedule/fuzz_test.go`, `internal/dotpack/fuzz_test.go`, `internal/mcpinstall/fuzz_test.go`, `internal/mcpinstall/testdata/fuzz/FuzzCodexBlock/a763424096dd21b2`, `internal/credentials/fuzz_test.go`, `internal/worktree/fuzz_test.go`, `internal/ui/diff/fuzz_test.go`, `scripts/fuzz.sh`; any parser file a crash points at, with its crasher under that package's `testdata/fuzz/` (never a file another Wave 1 task owns).
- Task 4: `internal/engine/broadcast.go`, `broadcast_test.go`, `engine.go`, `soak_test.go`, `soak_darwin_test.go`, `soak_linux_test.go`; `internal/view/trim.go`, `trim_test.go`.
- Task 5: `internal/version/VERSION`, `version.go`, `version_test.go`; `cmd/herdr-dots/main.go`, `cmd/herdr-dots/memwire_test.go` (the one version pin at `dffabbf`); `herdr-plugin.toml`; `.goreleaser.yaml`; `.github/workflows/ci.yml`, `release.yml`; `Makefile`; `release/SHA256SUMS`, `release/REPO`; `scripts/release-local.sh`; `.gitignore`.
- Task 6: `scripts/build.sh`, `build.ps1`, `build_test.go`, `build_ps1_test.go`, `release_test.go`; `cmd/herdr-dots/verifyrelease.go`, `verifyrelease_test.go`, `platform_manifest_test.go`.
- Task 7: `cmd/herdr-dots/migrate.go`, `migrate_test.go`, `main.go`, `engine.go`.
- Task 8: `internal/keys/remove.go`, `remove_test.go`; `cmd/herdr-dots/uninstall.go`, `uninstall_test.go`.
- Task 9: `internal/secrets/secrets.go`; `internal/memory/store.go`, `hardening_test.go`, `gitcalls_test.go`; `docs/superpowers/notes/2026-10-08-phase-10-triage.md`.
- Task 10: `internal/ui/memui/model.go`, `model_test.go`; `internal/delivery/delivery.go`, `truncate_test.go`; `internal/dots/store.go`, `slug_test.go`; `internal/state/feed.go`, `feedmode_test.go`; `internal/mcpserver/engine_test.go`; `internal/engine/engine_test.go`.
- Task 11: `docs/security/threat-model.md`, `docs/security/2026-10-08-security-review.md`, `SECURITY.md`; product files a High or Critical finding points at (never a docs file Task 12 owns).
- Task 12: `README.md`, `docs/guide/*.md`, `CHANGELOG.md`, `CONTRIBUTING.md`.
- Task 13: `docs/superpowers/notes/1.0-acceptance.md`, `docs/release/ready-to-publish.md`, `README.md` (status block only), `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, the Task 1 notes file (wrap-up section).

**Gate after Wave 1:** after merging Tasks 1–5, "The verification gate" passes for every package, and `GOTOOLCHAIN=local go test -run '^Fuzz' ./...` (seed corpora only) passes.

**Gate after Wave 2:** `make cross` passes (48 Windows test binaries at the reconcile pass); `GOTOOLCHAIN=local go test -race ./...` passes (Phase 6's portability guard included); `actionlint .github/workflows/*.yml` prints nothing; `shellcheck -s sh scripts/*.sh` prints nothing.

**Test-name hygiene:** helpers added here, by package. `migrate` (new package): `widgetSet`, `noLock`, `writeFile`, `readFile`, `legacyTree`, `snapshotTree`, `writerTree`, consts `legacyDot`, var `legacy`; product `memoryLock`, `siblingLock`, `quiet`, `withLock`, `backupPath`, `checkVersion`. `fsutil`: none. `backend`: `hasControl`, `seedReplies`, `checkOps`. `transcript`: `checkTurns`. `memory`: `char` (Task 9), no new helper in Task 3. `council`, `keys`, `config`, `dots`, `schedule`, `dotpack`, `mcpinstall`, `credentials`, `worktree`, `ui/diff`: none (Task 3); `keys` product `blockHeader`, `tableHeaderRe`, `commandTableRe`, `ourCommandRe` (Task 8); `dots` product `ErrBadSlug`, `validSlug` (Task 10); `secrets` product `stripInvisible`, `scan` (Task 9). `ipc`: `fuzzHandler`. `engine`: `soakEnv`, `cpuTime`, `peakRSS`, consts `soakMaxIdleCPU`, `soakMaxRSS`, `soakDots`, `soakPanes`, `rssUnit`, `minBroadcastGap` (product). `view`: `bigPane`, `jsonSize`. `version`: none. `scripts` (new package, tests only): `skipOnWindows`, `toolDir`, `tarGz`, `sum`, `pluginRoot`, `release`, `goEnv`, `runBuild`, `runInstalled`, `readInstallSource`, `binLeftovers`, `powershell`, `zipBytes`, `psRoot`, `runPS`, consts `testVersion`, `testRepo`, `testPlat`, `testAsset`, `fakeBinary`, `winAsset`, var `unixTools`. `main` (cmd/herdr-dots): `vrTarGz`, `vrRoot`, `vrRun` (Task 6); `migratePaths`, `writeTestFile` (Task 7); `uninstallRig`, `newUninstallRig`, const `herdrConfig` (Task 8). Product names in `main`: Task 6 `verifyIO`, `vrAssetRe`, `vrRepoRe`, `vrSigIssuer`, `vrReadRecord`, `vrChecksum`, `vrFileSHA256`, `vrMemberSHA256`, `vrExists`; Task 7 `startupMigrations`, `readOnlyNotice`, `writingCommands`, `protectNewerFiles`, `sessionDirRe`, `migrateCmd`; Task 8 `uninstallIO`, `uninstallSessionRe`, `purgeConfirm`, `removeKeyBlock`, `purgeDirs`, `uninstallConfirm`, `safeToPurge`, `runningEngines`; Task 5 `printVersion`. `delivery`, `state`, `mcpserver`: none. The reconcile pass compiled every one of these names into its package at `dffabbf` with no collision; if Phase 9's final fix wave adds one, run `grep -rn "func <name>\b\|<name> =" <package dir>` and add the suffix `P10` everywhere the task uses it.

### Shared files with Phases 3–9

Phases 3–9 are merged, so these files carry their changes. Every diff in this plan was regenerated by the reconcile pass against `dffabbf` (Wave 2 diffs against `dffabbf` plus Wave 1), so it applies with `git apply` as written. If Phase 9's final fix wave moves a line, make the same edit by hand: every hunk shows the exact lines to remove and add.

| File | Changed by | Phase 10 task | What Phase 10 changes |
|---|---|---|---|
| `internal/engine/engine.go` | 3–9 | 4 | adds the `bc throttle` field, renames `Snapshot` to `buildSnapshot`, removes `broadcast` (moved to `broadcast.go`) |
| `internal/engine/engine_test.go` | 1–9 | 10 | `eventually` waits up to 15 s |
| `cmd/herdr-dots/main.go` | 3, 4, 5, 7, 9 | 5, 7 | version entries (5); one `protectNewerFiles` line in `dispatch` (7) |
| `cmd/herdr-dots/memwire_test.go` | 2, 9 | 5 | the version pin |
| `cmd/herdr-dots/engine.go` | 3–9 | 7 | two insertions in `serveEngine` |
| `cmd/herdr-dots/platform_manifest_test.go` | 6 | 6 | `TestWindowsBuildScriptBuildsTheExe` pins the new script |
| `internal/fsutil/atomic.go` | 6 | 2 | one check at the top of `WriteFileAtomic` |
| `internal/secrets/secrets.go` | 9 | 9 | `Scan` also scans without invisible characters |
| `internal/memory/store.go` | 2–9 | 9 | `FindProject` deepest match |
| `internal/memory/tomlio.go` | 2, 9 | 3 | `ExportTOML` omits an empty ref |
| `internal/mcpserver/engine_test.go` | 5, 6 | 10 | `startStub` waits for `Serve` |
| `internal/ui/memui/model.go`, `model_test.go` | 2, 5, 9 | 10 | approve screen fit |
| `internal/dots/store.go` | 4, 9 | 10 | slug validation |
| `internal/state/feed.go` | 7, 8 | 10 | mode tightening, `Recent(n<=0)` |
| `herdr-plugin.toml` | 3, 5, 6, 7 | 5 | version, comment, description |
| `scripts/build.sh`, `scripts/build.ps1` | 6 | 6 | replaced whole |
| `.github/workflows/ci.yml` | 6 | 5 | replaced whole (keeps Phase 6's matrix) |
| `Makefile` | 6 | 5 | new targets |
| `README.md` | every phase | 12, 13 | restructured (12), status block (13) |
| `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md` | — | 13 | the Phase 10 row's version and increment cells |

## File Structure

```
internal/migrate/migrate.go          (new)  Kind, Migration, Set, Roots, Options, Report, Probe, Validate, Run (backups, locks, read-only marks)
internal/migrate/kinds.go            (new)  Default(): every persisted file kind in 1.0; Unversioned; KindPaths; Appended; External
internal/fsutil/readonly.go          (new)  ErrNewerVersion, ProtectNewer, Protected, ResetProtected, CheckWritable
internal/fsutil/atomic.go            (mod)  WriteFileAtomic refuses protected paths
internal/*/fuzz_test.go              (new)  24 fuzz targets in 14 packages (Task 3)
internal/memory/tomlio.go            (mod)  ExportTOML: no ref for an entry without an id
scripts/fuzz.sh                      (new)  run every fuzz target for N each
internal/engine/broadcast.go         (new)  throttle, Snapshot (trimmed), broadcast (coalesced), sendSnapshot
internal/engine/engine.go            (mod)  bc field; buildSnapshot; old broadcast removed
internal/engine/soak*_test.go        (new)  //go:build soak: 12 dots x 4 panes, CPU/RSS/snapshot/goroutine budgets
internal/view/trim.go                (new)  MaxSnapshotBytes, MaxCardsPerPane, MaxFeedPerPane, Trim
internal/version/VERSION             (new)  1.0.0-rc.1
internal/version/version.go          (mod)  embedded Version; Source, Commit, CommitID, String
cmd/herdr-dots/main.go               (mod)  version/--version via printVersion; protectNewerFiles in dispatch
herdr-plugin.toml                    (mod)  version 1.0.0-rc.1
.goreleaser.yaml                     (new)  six archives, SHA256SUMS, pinned inputs
.github/workflows/ci.yml             (mod)  + vulncheck, fuzz, soak jobs; Windows build runs build.ps1
.github/workflows/release.yml        (new)  rc candidate (checksum PR) and final publish (verify, sign, draft)
release/SHA256SUMS, release/REPO     (new)  empty in rc.1
scripts/release-local.sh             (new)  the six archives without GoReleaser
scripts/build.sh, build.ps1          (mod)  download -> verify -> install, else go build, else clear error
scripts/*_test.go                    (new)  package scripts: install-script tests, PowerShell parity, release e2e, GoReleaser naming
cmd/herdr-dots/verifyrelease.go      (new)  verify-release
cmd/herdr-dots/migrate.go            (new)  startupMigrations, readOnlyNotice, protectNewerFiles, migrate command
cmd/herdr-dots/engine.go             (mod)  migrations before stores open; read-only toast
internal/keys/remove.go              (new)  RemoveBlock
cmd/herdr-dots/uninstall.go          (new)  uninstall-extras [--purge] [--yes]
internal/secrets/secrets.go          (mod)  scan without invisible characters too
internal/memory/store.go             (mod)  FindProject deepest root
internal/memory/gitcalls_test.go     (new)  guard: every git call disables repository programs
internal/memory/hardening_test.go    (new)  invisible-character secrets, hostile-repo git, nested projects
internal/ui/memui/model.go           (mod)  approve only when the whole proposal fits
internal/mcpserver/engine_test.go    (mod)  startStub waits for Serve (flaky test)
internal/engine/engine_test.go       (mod)  eventually waits up to 15 s (draft tests under load)
internal/delivery/delivery.go        (mod)  TruncateRunes(n<=0)
internal/dots/store.go               (mod)  ErrBadSlug: Load/Save/Delete refuse non-file-name slugs
internal/state/feed.go               (mod)  owner-only feed file; Recent(n<=0)
docs/security/threat-model.md        (new)  threats -> mitigations -> tests
docs/security/2026-10-08-security-review.md (new) review findings and dispositions
SECURITY.md, CHANGELOG.md, CONTRIBUTING.md (new)
docs/guide/*.md                      (new)  per-feature user guide
README.md                            (mod)  overview, install, quick start, privacy, safety, known issues, status
docs/superpowers/notes/*.md          (new)  verification, triage, 1.0 acceptance
docs/release/ready-to-publish.md     (new)  the developer's checklist from here to a published 1.0.0
```

**Conventions for code steps.** New files are given in full. Changes to existing files are unified diffs: save the block to a file (for example `/tmp/p10-task.patch`) and run `git apply /tmp/p10-task.patch` from the repo root. If a hunk doesn't apply because Phase 9's final fix wave moved a neighboring line, make the same edit by hand (every hunk shows the exact lines), then run `gofmt -w` on the touched files. Every code block in Tasks 1–10 was replayed from this text on `dffabbf` (Phases 2–9 merged) by the reconcile pass, compiled and tested (see "Confirmed while planning", T2).

**The verification gate** (each task's last test step spells it out):

```bash
gofmt -l internal cmd scripts
GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go vet -tags soak ./internal/engine/
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./...
mkdir -p "${TMPDIR:-/tmp}/p10-wintest" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p10-wintest/" <the task's packages>; rm -rf "${TMPDIR:-/tmp}/p10-wintest"
```

Expected: `gofmt` prints nothing; the others succeed. (`gofmt -l .` would walk into the checkouts under `.claude/worktrees` and `.worktrees`.)

---

### Task 1: Verification notes, baseline, tool check, user gates

**Depends on:** none (Wave 1).

**Files:**
- Create: `docs/superpowers/notes/2026-10-08-phase-10-verification.md`

**Interfaces:**
- Consumes: nothing.
- Produces: the notes file with the sections `## V1–V4`, `## Tools on this machine`, `## Baseline at the start of Phase 10`, `## User gates` and an empty `## Phase 10 wrap-up` (Task 13 fills it). Task 13 reads the tool list to decide whether to run GoReleaser.

- [ ] **Step 1: Check the tools**

Run: `for t in goreleaser cosign govulncheck actionlint shellcheck pwsh powershell codex herdr claude git go; do printf '%s: ' "$t"; command -v "$t" || echo 'not found'; done; go version; git --version; herdr --version`
Expected: a path or `not found` per tool. While planning: `actionlint`, `shellcheck`, `herdr` (0.8.2), `claude`, `git` (2.52.0) and `go` (1.25.7) found; `goreleaser`, `cosign`, `govulncheck`, `pwsh`, `powershell` and `codex` not found. Do not install anything.

- [ ] **Step 2: Record the baseline**

Run: `GOTOOLCHAIN=local go test -race ./... 2>&1 | tail -40; GOTOOLCHAIN=local go vet ./... && echo vet-ok; grep -n 'Version' internal/version/version.go; grep -n '^version' herdr-plugin.toml`
Expected: every package `ok`, `vet-ok`, version `0.9.0` in both places. If anything fails, stop and report (Preconditions).

- [ ] **Step 3: Write the notes**

Create `docs/superpowers/notes/2026-10-08-phase-10-verification.md` with this content, replacing each `RESULT:` line with the output of Steps 1 and 2 (paths under the home directory shortened to `~`; never paste user names):

```markdown
# Phase 10 (release hardening) verification notes

Phase 10 ships `1.0.0-rc.1`. It builds everything up to a release and stops at
`docs/release/ready-to-publish.md`. Nothing here was pushed, tagged, listed or
published (release-gate amendments).

## V1–V4

| # | Question | Finding | Holds |
|---|---|---|---|
| V1 | Can a plugin find another plugin id's config/state dirs? | Not needed: the name stays "Herdr Dots" (decisions ruling 1); the migration framework (`internal/migrate`) makes a later rename one more migration | n/a |
| V2 | Is the name free (GitHub repo, marketplace, Homebrew/Scoop, trademark)? | Not checked: naming and publishing are the developer's decisions. "Herdr Dots" overlaps OpenAI's "Dots" (spec §2); listed in the ready-to-publish checklist | user gate |
| V3 | Can `[[build]]` reach the network during `herdr plugin install`? Where does Herdr install from? | herdr.dev/docs/plugins (2026-10-08): install clones with `git` (default branch, or `--ref`), shows a preview, runs build steps after confirmation, gives them no runtime env, and documents no network restriction. `plugin link` runs no build. There is no `plugin update`: reinstall | yes |
| V4 | Does install honor per-platform `[[build]]` items? | Yes: item-level `platforms` (Phase 6 decision 2, from Herdr's source) | yes |

## Tools on this machine

RESULT: (Step 1 output)

GoReleaser is used only as `goreleaser release --snapshot --clean --skip=publish,sign`
when installed; otherwise `scripts/release-local.sh` builds the same archives.

## Baseline at the start of Phase 10

RESULT: (Step 2 output, trimmed to the package lines and the two version lines)

## User gates

Only the developer can do these; Phase 10 prepares each one.

1. Choose a license and add `LICENSE` (ruling 16). No license is chosen.
2. Decide the name for publishing (keep "Herdr Dots" or rename; the migration framework supports a rename).
3. Run the feature-complete acceptance run on Linux and on Windows (Herdr Windows beta) hardware, and on macOS with a real Herdr install.
4. Run the live provider checks: the Claude live tests and the provider part of the macOS smoke, the API backends with your keys, then install Codex and run the Codex live tests (`go test -tags live ./internal/backend/ ./internal/council/`).
5. Create the GitHub repository, push, and let CI run on three OSes.
6. Tag `v1.0.0-rc.N`, merge the checksum PR, set `internal/version/VERSION` to `1.0.0` when the acceptance run passes, tag `v1.0.0`, publish the draft release.
7. Add the `herdr-plugin` topic and check the listing on herdr.dev/plugins.
8. Install from the marketplace without Go on all six targets.

## Phase 10 wrap-up

(Task 13)
```

- [ ] **Step 4: Commit (never push)**

```bash
git add docs/superpowers/notes/2026-10-08-phase-10-verification.md
git commit -m "docs: Phase 10 verification notes, baseline and user gates"
```

---

### Task 2: Migration framework and the read-only gate

**Depends on:** none (Wave 1).

**Files:**
- Create: `internal/migrate/migrate.go`, `internal/migrate/kinds.go`, `internal/migrate/migrate_test.go`, `internal/migrate/legacy_test.go`, `internal/migrate/writers_test.go`, `internal/fsutil/readonly.go`, `internal/fsutil/readonly_test.go`
- Modify: `internal/fsutil/atomic.go` (top of `WriteFileAtomic`)

**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`; `lockfile.Acquire(path string, timeout time.Duration) (*Lock, error)`, `(*Lock).Release()`, `lockfile.TryLock(path string) (*Lock, error)`; `paths.Paths{ConfigDir, StateDir, SessionDir}`, `paths.New` and the getter methods (reflection); the loaders `config.Load`, `dots.LoadAll`, `dots.DecodeStrict`, `queue.Open`, `state.OpenAttachments`, `state.OpenUsage`, `state.OpenFeed`, `memory.Open`/`List`/`ImportTOML`, `dotpack.Parse`/`Format`/`ErrFormat` (tests only); the Phase 4–9 writers, tests only: `credentials.File.Set`, `mcpinstall.SaveRecords`/`RecordPath`/`Record`/`ClientClaude`, `patchstore.Open`/`Put`/`Update`/`Meta`/`StatusReady`, `state.OpenDaily`/`Daily.Add`, `queue.OpenInbox`/`Inbox.Put`, `memory.Store.SeeDots`/`ClaimSchedule`, `history.Open`/`Append`/`Record`/`TypeRun`/`KindReview`, `state.OutcomePass`, `(*projectdots.TrustStore).Trust(project, rel, path string, sum [32]byte, caps []string)`, `projectdots.Sum`.
- Produces (package `fsutil`): `var ErrNewerVersion error`; `func ProtectNewer(path string)`; `func Protected(path string) bool`; `func ResetProtected()`; `func CheckWritable(path string) error`; `WriteFileAtomic` returns an error wrapping `ErrNewerVersion` for a protected path.
- Produces (package `migrate`): `type Format string` (`JSON`, `TOML`); `type Root int` (`ConfigRoot`, `StateRoot`, `SessionRoot`); `type Kind struct{ Name string; Root Root; Globs []string; Format Format; Current int; Lock func(file string) string; SelfGuarded bool; Secret bool }`; `type Migration struct{ Kind string; From int; Apply func([]byte) ([]byte, error) }`; `type Set struct{ Kinds []Kind; Migrations []Migration }`; `type Roots struct{ Config, State, Session string }`; `func RootsOf(p paths.Paths) Roots`; `type Options struct{ DryRun bool; LockTimeout time.Duration; Logf func(string, ...any) }`; `type Report struct{ Migrated, Pending, ReadOnly, Failed, Corrupt, Backups []string }` with `Empty() bool` and `Summary() string`; `func Probe(data []byte, f Format) (int, error)`; `func (Set) Validate() error`; `func (Set) Run(r Roots, o Options) (Report, error)`; `func Default() Set`; `var Unversioned map[string]string`; `var KindPaths map[string]string`; `var Appended map[string]string` (the append-only JSONL logs); `var External map[string]string` (formats read from outside the plugin's directories).

Every persisted file at `dffabbf`, with its version (verified by reading the code; Plan decision 24):

| File | Kind / class | Version today | Writer and guard |
|---|---|---|---|
| `CONFIG_DIR/config.toml` | `config` | none (= 1) | the developer; `config.Load` fails closed on a bad file |
| `CONFIG_DIR/dots/*.toml` | `dot` | none (= 1) | `dots.Save` (`fsutil.WriteFileAtomic`) |
| `CONFIG_DIR/credentials.toml` | `credentials` (Secret) | none (= 1); a table per provider, so it can never hold a top-level `version` | `credentials.File.Set`/`Remove` |
| `CONFIG_DIR/prices.toml` | `prices` | none (= 1) | the developer |
| `CONFIG_DIR/mcp-install.json` | `mcp-install` | `"version": 1` | `mcpinstall.SaveRecords` |
| `CONFIG_DIR/trusted.json` | `trust` (lock `trusted.json.lock`, self-guarded) | `"version": 1` | `TrustStore.Trust`/`Revoke*`; `projectdots.ErrNewer` |
| `STATE_DIR/memory/<p>/project.json`, `dots/<slug>.json`, `_global/dots/<slug>.json` | `memory-scope` (lock `memory.lock`, self-guarded) | `"version": 1` | `memory.Store`; `memory.ErrVersion` |
| `STATE_DIR/memory/<p>/meta.json` | `memory-meta` (same) | `"version": 1` | `SeeProject`, `SeeDots` |
| `STATE_DIR/memory/<p>/schedule.json` | `schedule` (same) | `"version": 1` | `ClaimSchedule` |
| `STATE_DIR/memory/<p>/ledger.jsonl` | `Appended` | per line | `RecordOutcome` (append) |
| `STATE_DIR/daily.json` | `daily` (lock `daily.json.lock`, self-guarded) | `"version": 1` | `Daily.Add`; `state.ErrDailyVersion` |
| `STATE_DIR/inbox.json` | `inbox` (lock `inbox.json.lock`) | none (= 1) | `Inbox.Put`/claims |
| `STATE_DIR/history/<slug>.jsonl` | `Appended` (`HistoryDir`) | per line | `history.Store.Append` |
| `SESSION_DIR/attachments.json`, `queue.json`, `usage.json` | `attachments`, `queue`, `usage` | none (= 1); attachments and usage are pane-keyed maps | the engine |
| `SESSION_DIR/patches/<id>.json` | `patch-meta` | none (= 1) | `patchstore.Put`/`Update` (the `.patch` files are git output, not a format of ours) |
| `SESSION_DIR/feed.jsonl` | `Appended` (`FeedFile`) | per line | `Feed.Append` |
| `SESSION_DIR/dotspanes.json`, `engine.lock`, `engine.log`, the socket, `CONFIG_DIR/.state-dir` | `Unversioned` | — | runtime files |
| `*.dotpack.toml` (export files) | `External` | `[pack] format = 1` | `dotpack.Export`; `dotpack.Parse` refuses another format |
| memory export `.toml` | `External` | `version = 1` | `memory.ExportTOML`; `ImportTOML` refuses a newer version |
| `<repo>/.herdr-dots/dots/*.toml` | `External` | none | the repository; `dots.DecodeStrict` refuses unknown keys |

- [ ] **Step 1: Write the failing tests**

Create `internal/fsutil/readonly_test.go`:

```go
package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedFileRefusesAtomicWritesAndKeepsItsBytes(t *testing.T) {
	t.Cleanup(ResetProtected)
	dir := t.TempDir()
	p := filepath.Join(dir, "queue.json")
	if err := WriteFileAtomic(p, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ProtectNewer(filepath.Join(dir, ".", "queue.json")) // any spelling of the path
	err := WriteFileAtomic(p, []byte(`{}`), 0o600)
	if !errors.Is(err, ErrNewerVersion) {
		t.Fatalf("write to a protected file: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != `{"version":2}` {
		t.Fatalf("protected file changed: %s", b)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "other.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("other files stay writable: %v", err)
	}
	ResetProtected()
	if err := CheckWritable(p); err != nil {
		t.Fatalf("after reset: %v", err)
	}
}
```

Create `internal/migrate/migrate_test.go`:

```go
package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/platform/platformtest"
)

// widgetSet is a test-only kind at version 3 with two JSON migrations: v1
// renames "name" to "title", v2 adds "tags".
func widgetSet(lock func(string) string) Set {
	step := func(to int, fn func(map[string]any)) func([]byte) ([]byte, error) {
		return func(b []byte) ([]byte, error) {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			fn(m)
			m["version"] = to
			return json.Marshal(m)
		}
	}
	return Set{
		Kinds: []Kind{{Name: "widget", Root: StateRoot, Globs: []string{"widgets/*.json"}, Format: JSON, Current: 3, Lock: lock}},
		Migrations: []Migration{
			{Kind: "widget", From: 1, Apply: step(2, func(m map[string]any) { m["title"] = m["name"]; delete(m, "name") })},
			{Kind: "widget", From: 2, Apply: step(3, func(m map[string]any) { m["tags"] = []string{} })},
		},
	}
}

func noLock(string) string { return "" }

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProbeReadsVersionsAndDefaultsToOne(t *testing.T) {
	cases := []struct {
		data string
		f    Format
		want int
		bad  bool
	}{
		{`{"next":3,"cards":[]}`, JSON, 1, false},
		{`{"version":2,"cards":[]}`, JSON, 2, false},
		{`{"w1:p1":{"pane_id":"w1:p1"}}`, JSON, 1, false}, // map-shaped file
		{`{"version":{"pane_id":"x"}}`, JSON, 1, false},   // a pane named "version"
		{`null`, JSON, 1, false},                          // an empty store
		{`{"version":0}`, JSON, 0, true},                  // never written by herdr-dots
		{`[1,2]`, JSON, 0, true},                          // not an object
		{``, JSON, 0, true},                               // truncated
		{"name = \"Security\"\n", TOML, 1, false},         // a v1 dot
		{"version = 4\nname = \"x\"\n", TOML, 4, false},   // a future dot
		{"version = \"one\"\n", TOML, 1, false},           // not a number
		{"name = \n", TOML, 0, true},                      // malformed
	}
	for _, c := range cases {
		got, err := Probe([]byte(c.data), c.f)
		if (err != nil) != c.bad || (!c.bad && got != c.want) {
			t.Errorf("Probe(%q, %s) = %d, %v; want %d (error %v)", c.data, c.f, got, err, c.want, c.bad)
		}
	}
}

func TestRunUpgradesOldFilesBacksThemUpAndIsIdempotent(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	old := filepath.Join(state, "widgets", "a.json")
	mid := filepath.Join(state, "widgets", "b.json")
	cur := filepath.Join(state, "widgets", "c.json")
	writeFile(t, old, `{"name":"alpha"}`)
	writeFile(t, mid, `{"version":2,"title":"beta"}`)
	writeFile(t, cur, `{"version":3,"title":"gamma","tags":["x"]}`)
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }

	rep, err := widgetSet(noLock).Run(Roots{State: state}, Options{Logf: logf})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Migrated) != 2 || len(rep.Backups) != 2 || len(rep.ReadOnly)+len(rep.Failed)+len(rep.Corrupt) != 0 {
		t.Fatalf("report %+v", rep)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(readFile(t, old)), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != float64(3) || got["title"] != "alpha" || got["name"] != nil || got["tags"] == nil {
		t.Fatalf("a.json after migration: %v", got)
	}
	if b := readFile(t, old+".bak-v1"); b != `{"name":"alpha"}` {
		t.Fatalf("backup holds %q", b)
	}
	platformtest.AssertPrivateFile(t, old+".bak-v1") // 0600, or an owner-only DACL on Windows
	if b := readFile(t, cur); b != `{"version":3,"title":"gamma","tags":["x"]}` {
		t.Fatalf("a current file was rewritten: %s", b)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "upgraded from version 1 to 3") {
		t.Fatalf("logs: %v", logs)
	}

	again, err := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if err != nil || !again.Empty() || len(again.Backups) != 0 {
		t.Fatalf("second run: %+v %v", again, err)
	}
}

func TestNewerFileIsReadOnlyAndNeverRewritten(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	future := filepath.Join(state, "widgets", "f.json")
	writeFile(t, future, `{"version":9,"title":"from 2.0"}`)
	rep, err := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ReadOnly) != 1 || rep.ReadOnly[0] != future || len(rep.Migrated) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if err := fsutil.WriteFileAtomic(future, []byte(`{}`), 0o600); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("a newer file must stay read-only: %v", err)
	}
	if readFile(t, future) != `{"version":9,"title":"from 2.0"}` {
		t.Fatal("newer file changed")
	}
}

func TestFailedMigrationLeavesTheFileAndMarksItReadOnly(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	s := widgetSet(noLock)
	s.Migrations[1].Apply = func([]byte) ([]byte, error) { return nil, errors.New("boom") }
	rep, err := s.Run(Roots{State: state}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) != 1 || !strings.Contains(rep.Failed[0], "version 2 to 3: boom") || len(rep.ReadOnly) != 1 || len(rep.Backups) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if readFile(t, f) != `{"name":"alpha"}` {
		t.Fatal("a failed migration changed the file")
	}
	if _, err := os.Stat(f + ".bak-v1"); !os.IsNotExist(err) {
		t.Fatal("no backup is left behind when nothing was rewritten")
	}
	if !fsutil.Protected(f) {
		t.Fatal("the engine must not write a file it could not migrate")
	}
}

func TestMigrationThatForgetsTheVersionFails(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	s := widgetSet(noLock)
	s.Migrations[0].Apply = func(b []byte) ([]byte, error) { return b, nil }
	rep, _ := s.Run(Roots{State: state}, Options{})
	if len(rep.Failed) != 1 || !strings.Contains(rep.Failed[0], "produced version 1") {
		t.Fatalf("report %+v", rep)
	}
}

func TestCorruptFileIsReportedAndUntouched(t *testing.T) {
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":`)
	rep, _ := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if len(rep.Corrupt) != 1 || len(rep.Migrated)+len(rep.ReadOnly) != 0 {
		t.Fatalf("report %+v", rep)
	}
	if readFile(t, f) != `{"name":` {
		t.Fatal("corrupt file changed")
	}
}

func TestDryRunReportsPendingWithoutWriting(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	rep, _ := widgetSet(noLock).Run(Roots{State: state}, Options{DryRun: true})
	if len(rep.Pending) != 1 || len(rep.Migrated) != 0 || readFile(t, f) != `{"name":"alpha"}` {
		t.Fatalf("report %+v", rep)
	}
}

func TestLockedFileWaitsThenFailsWithoutChange(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	lockPath := filepath.Join(state, "widgets", "widgets.lock")
	held, err := lockfile.TryLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	s := widgetSet(func(string) string { return lockPath })
	start := time.Now()
	rep, _ := s.Run(Roots{State: state}, Options{LockTimeout: 150 * time.Millisecond})
	if time.Since(start) < 150*time.Millisecond || len(rep.Failed) != 1 || !strings.Contains(rep.Failed[0], "lock") {
		t.Fatalf("report %+v after %v", rep, time.Since(start))
	}
	if readFile(t, f) != `{"name":"alpha"}` {
		t.Fatal("file changed without its lock")
	}
	_ = held.Release()
	fsutil.ResetProtected()
	rep, _ = s.Run(Roots{State: state}, Options{})
	if len(rep.Migrated) != 1 {
		t.Fatalf("after the lock is free: %+v", rep)
	}
}

func TestValidateRefusesIncompleteOrUnsafeChains(t *testing.T) {
	gap := widgetSet(noLock)
	gap.Migrations = gap.Migrations[1:]
	unlocked := widgetSet(nil)
	dup := widgetSet(noLock)
	dup.Migrations = append(dup.Migrations, dup.Migrations[0])
	beyond := widgetSet(noLock)
	beyond.Migrations = append(beyond.Migrations, Migration{Kind: "widget", From: 3, Apply: func(b []byte) ([]byte, error) { return b, nil }})
	for name, s := range map[string]Set{"gap": gap, "unlocked shared state": unlocked, "duplicate": dup, "beyond current": beyond} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
		if _, err := s.Run(Roots{State: t.TempDir()}, Options{}); err == nil {
			t.Errorf("%s: Run accepted it", name)
		}
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default: %v", err)
	}
}

func TestBackupNeverOverwritesAnEarlierBackup(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	state := t.TempDir()
	f := filepath.Join(state, "widgets", "a.json")
	writeFile(t, f, `{"name":"alpha"}`)
	writeFile(t, f+".bak-v1", "older backup")
	rep, _ := widgetSet(noLock).Run(Roots{State: state}, Options{})
	if len(rep.Backups) != 1 || rep.Backups[0] != f+".bak-v1-2" {
		t.Fatalf("backups %v", rep.Backups)
	}
	if readFile(t, f+".bak-v1") != "older backup" || !bytes.Equal([]byte(readFile(t, f+".bak-v1-2")), []byte(`{"name":"alpha"}`)) {
		t.Fatal("backups mixed up")
	}
}
```

Create `internal/migrate/legacy_test.go` (the 0.1–0.3 files are byte-exact formats from the v1, Phase 2 and Phase 3 code):

```go
package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

const legacyDot = `name = "Security"
glyph = "◆"
color = "red"
purpose = """
Flag authentication flaws and secrets in code."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = []
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
`

// legacy holds files exactly as each released 0.x version wrote them, by
// root ("config", "state", "session") and path. Each version keeps the files
// of the versions before it.
var legacy = map[string]map[string]string{
	"0.1": {
		"config/config.toml":        "default_backend = \"claude-cli\"\ndefault_model = \"sonnet\"\ninject_enabled = true\n",
		"config/dots/security.toml": legacyDot,
		"session/attachments.json":  `{"w1:p1":{"pane_id":"w1:p1","agent":"claude","session_kind":"id","session_value":"abc","cwd":"/repo","source":"transcript","force_scrollback":false,"dots":[{"slug":"security","last_seen_turn":4,"paused":false,"cooldown_left":0,"failures":0}]}}`,
		"session/queue.json":        `{"next":2,"cards":[{"id":"c1","dot":"security","dot_name":"Security","glyph":"◆","pane_id":"w1:p1","turn":4,"severity":"warn","body":"Token logged in auth.go:12.","mode":"queue","approved":false,"status":"pending","created_at":"2026-10-01T10:00:00Z"}]}`,
		"session/usage.json":        `{"w1:p1":{"security":{"calls":3,"input_tokens":1200,"output_tokens":90}}}`,
		"session/feed.jsonl":        `{"time":"2026-10-01T10:00:00Z","pane_id":"w1:p1","dot":"security","turn":4,"outcome":"flagged","detail":"warn"}` + "\n",
	},
	"0.2": {
		"config/config.toml":                                 "default_backend = \"claude-cli\"\nmemory_enabled = true\nmemory_retention_days = 180\n",
		"state/memory/g-0123456789abcdef/meta.json":          `{"version":1,"id":"g-0123456789abcdef","name":"repo","roots":["/repo"],"created_at":"2026-10-02T10:00:00Z"}`,
		"state/memory/g-0123456789abcdef/project.json":       `{"version":1,"next":1,"entries":[{"id":"m1","scope":"project","kind":"decision","text":"Auth tokens live in httpOnly cookies.","status":"active","source":"user","pinned":true,"created_at":"2026-10-02T10:00:00Z","updated_at":"2026-10-02T10:00:00Z","uses":0,"evidence":{}}]}`,
		"state/memory/g-0123456789abcdef/dots/security.json": `{"version":1,"next":1,"entries":[{"id":"m1","scope":"dot","dot":"security","kind":"dismissed","text":"CSRF on /admin is intentionally off.","status":"active","source":"outcome","pinned":false,"created_at":"2026-10-02T10:00:00Z","updated_at":"2026-10-02T10:00:00Z","uses":0,"evidence":{"card":"c1"}}]}`,
		"state/memory/g-0123456789abcdef/ledger.jsonl":       `{"at":"2026-10-02T10:00:00Z","dot":"security","card":"c1","outcome":"dismissed","body":"CSRF on /admin."}` + "\n",
		"state/memory/_global/dots/security.json":            `{"version":1,"next":1,"entries":[{"id":"m1","scope":"global","dot":"security","kind":"preference","text":"Keep findings to two sentences.","status":"active","source":"user","pinned":false,"created_at":"2026-10-02T10:00:00Z","updated_at":"2026-10-02T10:00:00Z","uses":0,"evidence":{}}]}`,
		"session/attachments.json":                           `{"w1:p1":{"pane_id":"w1:p1","agent":"claude","session_kind":"id","session_value":"abc","cwd":"/repo","source":"transcript","force_scrollback":false,"dots":[{"slug":"security","last_seen_turn":4,"paused":false,"cooldown_left":0,"failures":0,"memory_seen_turn":4,"memory_passes":1}],"project_id":"g-0123456789abcdef","project_name":"repo"}}`,
	},
	"0.3": {
		"config/config.toml": "default_backend = \"claude-cli\"\n\n[council]\nrounds = 2\nmax_members = 4\nmerge = \"chair\"\n",
		"session/queue.json": `{"next":3,"cards":[{"id":"c2","dot":"council","dot_name":"Council","glyph":"⊕","pane_id":"w1:p1","turn":5,"severity":"critical","body":"1. Token logged (Security).","mode":"queue","approved":false,"status":"pending","created_at":"2026-10-03T10:00:00Z","kind":"council","question":"Is this safe?","parts":[{"dot":"security","dot_name":"Security","glyph":"◆","severity":"critical","body":"Token logged."},{"dot":"frontend","dot_name":"Frontend","glyph":"▲","failed":true,"body":"timed out"}]}]}`,
	},
}

// legacyTree writes version v and every version before it into a temp tree.
func legacyTree(t *testing.T, v string) Roots {
	t.Helper()
	base := t.TempDir()
	r := Roots{Config: filepath.Join(base, "config"), State: filepath.Join(base, "state"), Session: filepath.Join(base, "session")}
	var versions []string
	for k := range legacy {
		if k <= v {
			versions = append(versions, k)
		}
	}
	sort.Strings(versions)
	for _, ver := range versions {
		for rel, body := range legacy[ver] {
			writeFile(t, filepath.Join(base, filepath.FromSlash(rel)), body)
		}
	}
	return r
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasSuffix(p, ".lock") {
			b, _ := os.ReadFile(p)
			out[p] = string(b)
		}
		return nil
	})
	return out
}

// Every released format is already current in 1.0: Run rewrites nothing, and
// every file still loads through its own package afterwards.
func TestEveryReleasedVersionIsCurrentAndStillLoads(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	for _, v := range []string{"0.1", "0.2", "0.3"} {
		t.Run(v, func(t *testing.T) {
			r := legacyTree(t, v)
			before := snapshotTree(t, filepath.Dir(r.Config))
			rep, err := Default().Run(r, Options{})
			if err != nil || !rep.Empty() || len(rep.Backups) != 0 {
				t.Fatalf("report %+v %v", rep, err)
			}
			if after := snapshotTree(t, filepath.Dir(r.Config)); !reflect.DeepEqual(before, after) {
				t.Fatal("Run changed a released file")
			}
			if _, err := config.Load(filepath.Join(r.Config, "config.toml")); err != nil {
				t.Fatalf("config: %v", err)
			}
			if ds, bad := dots.LoadAll(filepath.Join(r.Config, "dots")); len(ds) != 1 || len(bad) != 0 {
				t.Fatalf("dots: %v %v", ds, bad)
			}
			q, err := queue.Open(filepath.Join(r.Session, "queue.json"))
			if err != nil || len(q.All()) != 1 {
				t.Fatalf("queue: %v", err)
			}
			if a, err := state.OpenAttachments(filepath.Join(r.Session, "attachments.json")); err != nil || len(a.List()) != 1 {
				t.Fatalf("attachments: %v", err)
			}
			if u, err := state.OpenUsage(filepath.Join(r.Session, "usage.json")); err != nil || u.Calls("w1:p1", "security") != 3 {
				t.Fatalf("usage: %v", err)
			}
			if f, err := state.OpenFeed(filepath.Join(r.Session, "feed.jsonl")); err != nil || len(f.Recent("w1:p1", 5)) != 1 {
				t.Fatalf("feed: %v", err)
			}
			if v >= "0.2" {
				st := memory.Open(filepath.Join(r.State, "memory"), memory.DefaultLimits())
				es, err := st.List("g-0123456789abcdef", memory.Filter{})
				if err != nil || len(es) < 2 {
					t.Fatalf("memory: %d entries, %v", len(es), err)
				}
			}
		})
	}
}

// A store a future version wrote stays readable and is never overwritten.
func TestFutureQueueIsReadButNeverRewritten(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r := legacyTree(t, "0.3")
	qf := filepath.Join(r.Session, "queue.json")
	future := `{"version":2,"next":3,"cards":[],"lanes":["a"]}`
	writeFile(t, qf, future)
	rep, err := Default().Run(r, Options{})
	if err != nil || len(rep.ReadOnly) != 1 || rep.ReadOnly[0] != qf {
		t.Fatalf("report %+v %v", rep, err)
	}
	q, err := queue.Open(qf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Add(queue.Card{Dot: "security", PaneID: "w1:p1", Body: "x", Status: queue.StatusPending}); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("a write to the future queue must fail with ErrNewerVersion, got %v", err)
	}
	if readFile(t, qf) != future {
		t.Fatal("future queue changed")
	}
}

// Every file a paths.Paths method names is either a kind or deliberately
// unversioned. A phase that adds a file must classify it here.
func TestEveryPathsFileIsClassified(t *testing.T) {
	kinds := map[string]bool{}
	for _, k := range Default().Kinds {
		kinds[k.Name] = true
	}
	for name, kind := range KindPaths {
		if !kinds[kind] {
			t.Errorf("KindPaths[%s] = %q, which is not a kind", name, kind)
		}
	}
	typ := reflect.TypeOf(paths.Paths{})
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if m.Type.NumIn() != 1 || m.Type.NumOut() != 1 || m.Type.Out(0).Kind() != reflect.String {
			continue // only path getters: func (Paths) X() string
		}
		if _, ok := KindPaths[m.Name]; ok {
			continue
		}
		if _, ok := Unversioned[m.Name]; ok {
			continue
		}
		t.Errorf("paths.Paths.%s is neither a migrate kind (KindPaths) nor listed in Unversioned", m.Name)
	}
}
```

Create `internal/migrate/writers_test.go` (files Phases 4–9 write, produced by their own merged writers; a broken `credentials.toml` is reported without its text; the external formats refuse newer versions):

```go
package migrate

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/history"
	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// writerTree writes every file Phases 4–9 added, with the writers that
// wrote them in 0.4–0.9. Phase 10 changes none of those writers, so what they
// produce today is what an existing install has on disk.
func writerTree(t *testing.T) (Roots, paths.Paths) {
	t.Helper()
	base := t.TempDir()
	p := paths.New(filepath.Join(base, "config"), filepath.Join(base, "state"), "/tmp/herdr-test.sock")
	r := RootsOf(p)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	const proj = "g-0123456789abcdef"

	cf := credentials.File{Path: p.CredentialsFile(), Env: func(string) string { return "" }}
	if err := cf.Set("anthropic", "sk-ant-api03-"+strings.Repeat("aB3d", 12)); err != nil {
		t.Fatalf("credentials.toml (Phase 4): %v", err)
	}
	if err := mcpinstall.SaveRecords(mcpinstall.RecordPath(p.ConfigDir), []mcpinstall.Record{{
		Client: mcpinstall.ClientClaude, Scope: "user", Command: []string{"/x/bin/herdr-dots", "mcp"}, At: now}}); err != nil {
		t.Fatalf("mcp-install.json (Phase 5): %v", err)
	}
	ps := patchstore.Open(p.PatchesDir())
	if err := ps.Put(patchstore.Meta{ID: "0123456789abcdef", Dot: "security", Pane: "w1:p1", Repo: "/repo", Status: patchstore.StatusReady}); err != nil {
		t.Fatalf("patches/<id>.json (Phase 7): %v", err)
	}
	d, err := state.OpenDaily(p.DailyFile())
	if err != nil {
		t.Fatalf("daily.json (Phase 8): %v", err)
	}
	if err := d.Add("security", "research", 0.01, now); err != nil {
		t.Fatalf("daily.json (Phase 8): %v", err)
	}
	if err := queue.OpenInbox(p.InboxFile()).Put(queue.Card{Project: proj, Dot: "security", DotName: "Security",
		Severity: "warn", Body: "Scheduled review: token logged.", Mode: "queue", Origin: "scheduled"}); err != nil {
		t.Fatalf("inbox.json (Phase 8): %v", err)
	}
	st := memory.Open(p.MemoryDir(), memory.DefaultLimits())
	if err := st.SeeDots(proj, []string{"security"}, now); err != nil {
		t.Fatalf("meta.json (Phase 8): %v", err)
	}
	if _, err := st.ClaimSchedule(proj, "security", now, now); err != nil {
		t.Fatalf("schedule.json (Phase 8): %v", err)
	}
	if err := history.Open(p.HistoryDir()).Append(history.Record{T: history.TypeRun, At: now, Dot: "security",
		Kind: history.KindReview, Outcome: state.OutcomePass}); err != nil {
		t.Fatalf("history (Phase 9): %v", err)
	}
	if err := (&projectdots.TrustStore{Path: p.TrustFile()}).Trust(proj, ".herdr-dots/dots/review.toml",
		"/repo/.herdr-dots/dots/review.toml", projectdots.Sum([]byte("name = \"Review\"\n")), nil); err != nil {
		t.Fatalf("trusted.json (Phase 9): %v", err)
	}
	return r, p
}

// Everything Phases 4–9 write is current in 1.0, and the JSONL history is
// not a kind at all.
func TestFilesWrittenByPhases4To9AreCurrent(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r, _ := writerTree(t)
	rep, err := Default().Run(r, Options{})
	if err != nil || !rep.Empty() || len(rep.Backups) != 0 {
		t.Fatalf("files written by Phases 4–9 must be current: %+v %v", rep, err)
	}
}

// Every Phase 4–9 file a newer herdr-dots wrote is kept read-only, and its
// own writer then fails with ErrNewerVersion instead of dropping what it
// doesn't know.
func TestNewerPhase4To9FilesStayReadOnly(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r, p := writerTree(t)
	inbox := `{"version":2,"next":1,"cards":[],"lanes":["a"]}`
	patch := `{"version":2,"id":"0123456789abcdef","dot":"security","pane":"w1:p1","repo":"/repo","files":0,"bytes":0,"status":"ready","created_at":"2026-10-08T12:00:00Z","updated_at":"2026-10-08T12:00:00Z"}`
	writeFile(t, p.InboxFile(), inbox)
	writeFile(t, filepath.Join(p.PatchesDir(), "0123456789abcdef.json"), patch)
	writeFile(t, mcpinstall.RecordPath(p.ConfigDir), `{"version":3,"installs":[]}`)
	rep, err := Default().Run(r, Options{})
	if err != nil || len(rep.ReadOnly) != 3 || len(rep.Migrated) != 0 {
		t.Fatalf("report %+v %v", rep, err)
	}
	err = queue.OpenInbox(p.InboxFile()).Put(queue.Card{Project: "g-0123456789abcdef", Dot: "security", Body: "x", Mode: "queue"})
	if !errors.Is(err, fsutil.ErrNewerVersion) || readFile(t, p.InboxFile()) != inbox {
		t.Fatalf("a newer inbox must not be rewritten: %v", err)
	}
	if _, err := patchstore.Open(p.PatchesDir()).Update("0123456789abcdef", func(m *patchstore.Meta) { m.Stat = "x" }); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("a newer patch record must not be rewritten: %v", err)
	}
	if err := mcpinstall.SaveRecords(mcpinstall.RecordPath(p.ConfigDir), nil); !errors.Is(err, fsutil.ErrNewerVersion) {
		t.Fatalf("newer mcp-install.json must not be rewritten: %v", err)
	}
}

// credentials.toml holds keys: a broken one is reported without the parse
// error, which would quote the line (and the key) into engine.log.
func TestBrokenCredentialsAreReportedWithoutTheirText(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	r, p := writerTree(t)
	key := "sk-ant-api03-" + strings.Repeat("Zq9x", 12)
	writeFile(t, p.CredentialsFile(), "[anthropic]\napi_key = \""+key+"\n")
	rep, _ := Default().Run(r, Options{})
	if len(rep.Corrupt) != 1 || strings.Contains(rep.Summary(), "Zq9x") || !strings.Contains(rep.Corrupt[0], "details withheld") {
		t.Fatalf("report %q", rep.Summary())
	}
}

// Formats herdr-dots reads from outside its directories check their own
// version: a newer pack or memory export is refused, never half-read.
func TestExternalFormatsRefuseNewerVersions(t *testing.T) {
	if len(External) == 0 || len(Appended) == 0 {
		t.Fatal("External and Appended document the formats Run does not cover")
	}
	if _, err := dotpack.Parse([]byte("[pack]\nformat = 2\n")); !errors.Is(err, dotpack.ErrFormat) {
		t.Fatalf("a format-2 pack: %v", err)
	}
	if _, err := memory.ImportTOML([]byte("version = 2\n")); !errors.Is(err, memory.ErrVersion) {
		t.Fatalf("a version-2 memory export: %v", err)
	}
	if _, err := dots.DecodeStrict([]byte("version = 2\nname = \"X\"\n")); err == nil {
		t.Fatal("a project dot with a key this version doesn't know must be refused")
	}
	if dotpack.Format != 1 {
		t.Fatalf("dotpack.Format = %d: register the change in External and CHANGELOG.md", dotpack.Format)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/fsutil/ ./internal/migrate/ 2>&1 | tail -5`
Expected: FAIL to build: `undefined: ResetProtected` (fsutil) and `no non-test Go files in .../internal/migrate` or `undefined: Set`.

- [ ] **Step 3: The read-only gate**

Create `internal/fsutil/readonly.go`:

```go
package fsutil

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

// ErrNewerVersion refuses a write to a file that a newer herdr-dots wrote. Its
// schema version is higher than this binary knows, so rewriting it would drop
// data the newer version relies on. internal/migrate marks such files when a
// process starts; this process then only reads them.
var ErrNewerVersion = errors.New("written by a newer herdr-dots: kept read-only")

var (
	roMu sync.RWMutex
	ro   = map[string]bool{}
)

func roKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// ProtectNewer makes path read-only for the rest of this process.
func ProtectNewer(path string) {
	roMu.Lock()
	defer roMu.Unlock()
	ro[roKey(path)] = true
}

// Protected reports whether ProtectNewer marked path.
func Protected(path string) bool {
	roMu.RLock()
	defer roMu.RUnlock()
	return ro[roKey(path)]
}

// ResetProtected forgets every mark. Tests use it.
func ResetProtected() {
	roMu.Lock()
	defer roMu.Unlock()
	ro = map[string]bool{}
}

// CheckWritable returns an error wrapping ErrNewerVersion for a protected
// path. WriteFileAtomic calls it; code that appends to a file calls it itself.
func CheckWritable(path string) error {
	if Protected(path) {
		return fmt.Errorf("%s: %w", filepath.Base(path), ErrNewerVersion)
	}
	return nil
}
```

Apply to `internal/fsutil/atomic.go` (the check goes first, before the Windows DACL and rename-retry code Phase 6 added; the doc comment gains one sentence):

```diff
--- a/internal/fsutil/atomic.go
+++ b/internal/fsutil/atomic.go
@@ -15,7 +15,11 @@ var restrictFn = restrict
 // also gets an owner-only DACL on Windows, applied to the temp file before any
 // byte is written, and a write whose privacy can't be set fails. On Windows
 // the rename is retried briefly while another process has the old file open.
+// A file a newer herdr-dots wrote (see ProtectNewer) is never replaced.
 func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
+	if err := CheckWritable(path); err != nil {
+		return err
+	}
 	dir := filepath.Dir(path)
 	if err := os.MkdirAll(dir, 0o700); err != nil {
 		return err
```


- [ ] **Step 4: The framework and the kinds**

Create `internal/migrate/migrate.go`:

```go
// Package migrate versions herdr-dots's persisted files and upgrades old ones.
//
// Every whole-file JSON or TOML state and config file has a schema version: a
// top-level "version" key, where a missing key means version 1. In 1.0 every
// kind is at version 1, so nothing is rewritten yet; the framework is here so
// a later format change (or a rename of the plugin) can upgrade files without
// losing data. Run, at engine start:
//
//   - leaves files at the current version alone;
//   - upgrades older files through registered migrations, one version at a
//     time, after backing the original up as <file>.bak-v<old>;
//   - marks files a newer binary wrote read-only for this process
//     (fsutil.ProtectNewer), so they are read but never overwritten;
//   - reports corrupt files and leaves them to their own loaders.
//
// Append-only JSONL logs (the feed, the memory ledger, history) are not
// versioned per file: their records only ever gain optional fields, and a
// breaking change must use a new file name (see CONTRIBUTING.md).
package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

// Format is how a kind of file is encoded.
type Format string

const (
	JSON Format = "json"
	TOML Format = "toml"
)

// Root is the directory a kind's glob is relative to.
type Root int

const (
	ConfigRoot  Root = iota // the plugin config dir (user-editable)
	StateRoot               // the plugin state dir, shared by every Herdr session
	SessionRoot             // one Herdr session's state subdirectory
)

// Kind is one kind of persisted file.
type Kind struct {
	Name    string // "queue", "memory-scope", ...
	Root    Root
	Globs   []string // filepath.Glob patterns under Root, slash-separated
	Format  Format
	Current int // the newest version this binary reads and writes
	// Lock returns the lock file that guards file, or "" for none. State-root
	// files are shared by every session's engine, so a migration can only be
	// registered for a state-root kind that has a lock.
	Lock func(file string) string
	// SelfGuarded kinds refuse newer versions in their own loader (memory,
	// schedule, daily, trust), so a read-only check (Options.DryRun) can skip
	// them.
	SelfGuarded bool
	// Secret kinds hold keys (credentials.toml). A TOML or JSON parse error
	// quotes the offending line, so their errors are never reported or logged.
	Secret bool
}

// Migration upgrades one kind of file from version From to From+1. Apply
// gets the whole file and must return it with "version" set to From+1.
type Migration struct {
	Kind  string
	From  int
	Apply func(data []byte) ([]byte, error)
}

// Set is the kinds and migrations one binary knows.
type Set struct {
	Kinds      []Kind
	Migrations []Migration
}

// Roots are the three directories kinds live under. An empty root skips its
// kinds (the migrate command runs one session at a time).
type Roots struct{ Config, State, Session string }

// RootsOf is the roots for one engine: config, state and its own session.
func RootsOf(p paths.Paths) Roots {
	return Roots{Config: p.ConfigDir, State: p.StateDir, Session: p.SessionDir}
}

// Options tune Run.
type Options struct {
	// DryRun reads versions only: newer files are still marked read-only,
	// files that need a migration are reported as Pending, nothing is written.
	DryRun      bool
	LockTimeout time.Duration // per locked file; default 2s
	Logf        func(string, ...any)
}

// Report says what Run found. Paths are absolute.
type Report struct {
	Migrated []string // upgraded to the current version
	Pending  []string // would be upgraded (DryRun)
	ReadOnly []string // written by a newer version, or a failed migration
	Failed   []string // "<path>: <why>"; the file was left as it was
	Corrupt  []string // "<path>: <why>"; unreadable version, left to its loader
	Backups  []string // originals saved before a rewrite
}

// Empty reports whether Run found nothing to say.
func (r Report) Empty() bool {
	return len(r.Migrated)+len(r.Pending)+len(r.ReadOnly)+len(r.Failed)+len(r.Corrupt) == 0
}

// Summary is one log line.
func (r Report) Summary() string {
	if r.Empty() {
		return "all files current"
	}
	var parts []string
	add := func(label string, items []string) {
		if len(items) > 0 {
			parts = append(parts, fmt.Sprintf("%s %d (%s)", label, len(items), strings.Join(items, "; ")))
		}
	}
	add("migrated", r.Migrated)
	add("pending", r.Pending)
	add("read-only", r.ReadOnly)
	add("failed", r.Failed)
	add("corrupt", r.Corrupt)
	return strings.Join(parts, ", ")
}

// Probe returns a file's schema version: its top-level "version" key, or 1
// when there is none. A "version" that is not a whole number (a map-shaped
// file keyed by pane id can't hold one) also means 1.
func Probe(data []byte, f Format) (int, error) {
	switch f {
	case JSON:
		var top map[string]json.RawMessage
		if err := json.Unmarshal(data, &top); err != nil {
			return 0, err
		}
		raw, ok := top["version"]
		if !ok {
			return 1, nil
		}
		var n json.Number
		if json.Unmarshal(raw, &n) != nil {
			return 1, nil
		}
		i, err := n.Int64()
		if err != nil {
			return 1, nil
		}
		return checkVersion(i)
	case TOML:
		var top map[string]any
		if _, err := toml.Decode(string(data), &top); err != nil {
			return 0, err
		}
		i, ok := top["version"].(int64)
		if !ok {
			return 1, nil
		}
		return checkVersion(i)
	}
	return 0, fmt.Errorf("unknown format %q", f)
}

func checkVersion(i int64) (int, error) {
	if i < 1 || i > 1<<20 {
		return 0, fmt.Errorf("version %d out of range", i)
	}
	return int(i), nil
}

// Validate checks that every migration chain is complete and safe.
func (s Set) Validate() error {
	kinds := map[string]Kind{}
	for _, k := range s.Kinds {
		if k.Name == "" || k.Current < 1 || len(k.Globs) == 0 {
			return fmt.Errorf("kind %q: needs a name, globs and a current version", k.Name)
		}
		if _, dup := kinds[k.Name]; dup {
			return fmt.Errorf("kind %q declared twice", k.Name)
		}
		kinds[k.Name] = k
	}
	steps := map[string]map[int]bool{}
	for _, m := range s.Migrations {
		k, ok := kinds[m.Kind]
		if !ok {
			return fmt.Errorf("migration for unknown kind %q", m.Kind)
		}
		if m.From < 1 || m.From >= k.Current || m.Apply == nil {
			return fmt.Errorf("kind %q: migration from %d is outside 1..%d", m.Kind, m.From, k.Current-1)
		}
		if k.Root == StateRoot && k.Lock == nil {
			return fmt.Errorf("kind %q: shared state needs a lock before it can be migrated", m.Kind)
		}
		if steps[m.Kind] == nil {
			steps[m.Kind] = map[int]bool{}
		}
		if steps[m.Kind][m.From] {
			return fmt.Errorf("kind %q: two migrations from %d", m.Kind, m.From)
		}
		steps[m.Kind][m.From] = true
	}
	for name, k := range kinds {
		for from := 1; from < k.Current; from++ {
			if !steps[name][from] {
				return fmt.Errorf("kind %q: no migration from %d to %d", name, from, from+1)
			}
		}
	}
	return nil
}

// Run checks every file of every kind under r. It returns an error only when
// the set itself is invalid; per-file problems go in the report.
func (s Set) Run(r Roots, o Options) (Report, error) {
	var rep Report
	if err := s.Validate(); err != nil {
		return rep, err
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = 2 * time.Second
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	for _, k := range s.Kinds {
		if o.DryRun && k.SelfGuarded {
			continue
		}
		root := map[Root]string{ConfigRoot: r.Config, StateRoot: r.State, SessionRoot: r.Session}[k.Root]
		if root == "" {
			continue
		}
		var files []string
		for _, g := range k.Globs {
			m, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(g)))
			if err != nil {
				return rep, fmt.Errorf("kind %q: %w", k.Name, err)
			}
			files = append(files, m...)
		}
		sort.Strings(files)
		for _, f := range files {
			s.one(k, f, o, &rep)
		}
	}
	return rep, nil
}

func (s Set) chain(kind string, from, to int) []Migration {
	var out []Migration
	for v := from; v < to; v++ {
		for _, m := range s.Migrations {
			if m.Kind == kind && m.From == v {
				out = append(out, m)
			}
		}
	}
	return out
}

// one handles a single file.
func (s Set) one(k Kind, file string, o Options, rep *Report) {
	data, err := os.ReadFile(file)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", file, err))
		}
		return
	}
	v, err := Probe(data, k.Format)
	switch {
	case err != nil:
		rep.Corrupt = append(rep.Corrupt, fmt.Sprintf("%s: %v", file, quiet(k, err)))
		return
	case v == k.Current:
		return
	case v > k.Current:
		fsutil.ProtectNewer(file)
		rep.ReadOnly = append(rep.ReadOnly, file)
		o.Logf("migrate: %s has version %d, this binary knows %d: read-only", file, v, k.Current)
		return
	case o.DryRun:
		rep.Pending = append(rep.Pending, file)
		return
	}
	upgrade := func() error { return s.upgrade(k, file, rep) }
	if k.Lock != nil {
		if lp := k.Lock(file); lp != "" {
			upgrade = withLock(lp, o.LockTimeout, upgrade)
		}
	}
	if err := upgrade(); err != nil {
		err = quiet(k, err)
		fsutil.ProtectNewer(file)
		rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", file, err))
		rep.ReadOnly = append(rep.ReadOnly, file)
		o.Logf("migrate: %s: %v (left as it was, read-only)", file, err)
		return
	}
	o.Logf("migrate: %s upgraded from version %d to %d", file, v, k.Current)
}

// quiet replaces the error of a Secret kind: its text may quote a key.
func quiet(k Kind, err error) error {
	if k.Secret {
		return errors.New("unreadable (details withheld: the file holds keys)")
	}
	return err
}

func withLock(lockPath string, timeout time.Duration, fn func() error) func() error {
	return func() error {
		l, err := lockfile.Acquire(lockPath, timeout)
		if err != nil {
			return fmt.Errorf("lock %s: %w", filepath.Base(lockPath), err)
		}
		defer l.Release()
		return fn()
	}
}

// upgrade re-reads file (another process may have upgraded it meanwhile),
// applies every step, checks the result, backs the original up and replaces
// the file atomically. On any error the file is untouched.
func (s Set) upgrade(k Kind, file string, rep *Report) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	st, err := os.Stat(file)
	if err != nil {
		return err
	}
	v, err := Probe(data, k.Format)
	if err != nil {
		return err
	}
	if v >= k.Current {
		return nil
	}
	out := data
	for _, m := range s.chain(k.Name, v, k.Current) {
		if out, err = m.Apply(out); err != nil {
			return fmt.Errorf("version %d to %d: %w", m.From, m.From+1, err)
		}
		got, err := Probe(out, k.Format)
		if err != nil || got != m.From+1 {
			return fmt.Errorf("version %d to %d produced version %d (%v)", m.From, m.From+1, got, err)
		}
	}
	backup, err := backupPath(file, v)
	if err != nil {
		return err
	}
	// The backup is as private as the file (an owner-only DACL on Windows):
	// credentials.toml and memory are backed up too.
	if err := fsutil.WriteFileAtomic(backup, data, 0o600); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(file, out, st.Mode().Perm()); err != nil {
		_ = os.Remove(backup)
		return err
	}
	rep.Backups = append(rep.Backups, backup)
	rep.Migrated = append(rep.Migrated, file)
	return nil
}

// backupPath is <file>.bak-v<version>, or with -2, -3 ... when that exists.
func backupPath(file string, v int) (string, error) {
	base := fmt.Sprintf("%s.bak-v%d", file, v)
	for i := 1; i < 100; i++ {
		p := base
		if i > 1 {
			p = fmt.Sprintf("%s-%d", base, i)
		}
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			return p, nil
		}
	}
	return "", fmt.Errorf("too many backups of %s", filepath.Base(file))
}
```

Create `internal/migrate/kinds.go`:

```go
package migrate

import "path/filepath"

// memoryLock is the lock that guards a memory file: memory.lock in its
// project (or _global) directory, one level up for files under dots/.
func memoryLock(file string) string {
	dir := filepath.Dir(file)
	if filepath.Base(dir) == "dots" {
		dir = filepath.Dir(dir)
	}
	return filepath.Join(dir, "memory.lock")
}

// siblingLock is <file>.lock, the lock daily.json, inbox.json and
// trusted.json are written under.
func siblingLock(file string) string { return file + ".lock" }

// Default is every persisted file kind herdr-dots 1.0 knows. Every kind is
// at version 1 and there are no migrations yet. Kinds a phase adds must be
// listed here: TestEveryPathsFileIsClassified fails until they are.
func Default() Set {
	return Set{Kinds: []Kind{
		// Config dir (the developer edits these; atomic replace keeps them safe).
		{Name: "config", Root: ConfigRoot, Globs: []string{"config.toml"}, Format: TOML, Current: 1},
		{Name: "dot", Root: ConfigRoot, Globs: []string{"dots/*.toml"}, Format: TOML, Current: 1},
		// credentials.toml is a table per provider: it can never hold a
		// top-level version key, so it stays at 1 until its format changes
		// shape. It holds keys, so a parse error's text is never reported.
		{Name: "credentials", Root: ConfigRoot, Globs: []string{"credentials.toml"}, Format: TOML, Current: 1, Secret: true},
		{Name: "prices", Root: ConfigRoot, Globs: []string{"prices.toml"}, Format: TOML, Current: 1},
		{Name: "mcp-install", Root: ConfigRoot, Globs: []string{"mcp-install.json"}, Format: JSON, Current: 1},
		{Name: "trust", Root: ConfigRoot, Globs: []string{"trusted.json"}, Format: JSON, Current: 1,
			Lock: siblingLock, SelfGuarded: true}, // projectdots.ErrNewer

		// State dir, shared by every session.
		{Name: "memory-scope", Root: StateRoot, Globs: []string{"memory/*/project.json", "memory/*/dots/*.json"},
			Format: JSON, Current: 1, Lock: memoryLock, SelfGuarded: true}, // memory.ErrVersion
		{Name: "memory-meta", Root: StateRoot, Globs: []string{"memory/*/meta.json"},
			Format: JSON, Current: 1, Lock: memoryLock, SelfGuarded: true},
		{Name: "schedule", Root: StateRoot, Globs: []string{"memory/*/schedule.json"},
			Format: JSON, Current: 1, Lock: memoryLock, SelfGuarded: true},
		{Name: "daily", Root: StateRoot, Globs: []string{"daily.json"}, Format: JSON, Current: 1,
			Lock: siblingLock, SelfGuarded: true}, // state.ErrDailyVersion
		{Name: "inbox", Root: StateRoot, Globs: []string{"inbox.json"}, Format: JSON, Current: 1, Lock: siblingLock},

		// One session's directory (the engine that runs Run holds its lock).
		{Name: "attachments", Root: SessionRoot, Globs: []string{"attachments.json"}, Format: JSON, Current: 1},
		{Name: "queue", Root: SessionRoot, Globs: []string{"queue.json"}, Format: JSON, Current: 1},
		{Name: "usage", Root: SessionRoot, Globs: []string{"usage.json"}, Format: JSON, Current: 1},
		{Name: "patch-meta", Root: SessionRoot, Globs: []string{"patches/*.json"}, Format: JSON, Current: 1},
	}}
}

// Unversioned lists the persisted paths that are deliberately not kinds, by
// paths.Paths method name, with the reason. TestEveryPathsFileIsClassified
// accepts a method only if it is here or maps to a kind in Default.
var Unversioned = map[string]string{
	"EngineLock":       "runtime lock, rewritten by every engine",
	"EngineLog":        "log text",
	"EngineSock":       "socket (Unix)",
	"EngineEndpoint":   "socket or named pipe",
	"FeedFile":         "append-only JSONL: records only gain optional fields",
	"DotsPanesFile":    "runtime cache of open Dots panes, rebuilt on demand",
	"StatePointerFile": "one line of text: the state dir",
	"HistoryDir":       "append-only JSONL per dot: records only gain optional fields",
	"WorktreesDir":     "transient git worktrees, owned by git and pruned",
}

// KindPaths maps paths.Paths methods to the kind that covers what they
// return.
var KindPaths = map[string]string{
	"ConfigFile":      "config",
	"DotsDir":         "dot",
	"CredentialsFile": "credentials",
	"PricesFile":      "prices",
	"TrustFile":       "trust",
	"MemoryDir":       "memory-scope",
	"DailyFile":       "daily",
	"InboxFile":       "inbox",
	"AttachmentsFile": "attachments",
	"QueueFile":       "queue",
	"UsageFile":       "usage",
	"PatchesDir":      "patch-meta",
}

// Appended lists the append-only JSONL logs. They are not kinds: each line
// is one record that only ever gains optional fields, and a breaking change
// must use a new file name (CONTRIBUTING.md).
var Appended = map[string]string{
	"feed.jsonl":            "session feed (state.Feed)",
	"memory/*/ledger.jsonl": "card outcomes a dot learned from (memory.Store ledger)",
	"history/*.jsonl":       "per-dot run history (history.Store)",
}

// External lists formats herdr-dots reads from files outside its own
// directories. Run never touches them; each reader checks its own version
// and refuses a newer one.
var External = map[string]string{
	".dotpack.toml":           "dot pack, top-level format = 1; dotpack.Parse refuses a newer format (ErrFormat)",
	"memory export (.toml)":   "herdr-dots memory export, version = 1; memory.ImportTOML refuses a newer version (ErrVersion)",
	".herdr-dots/dots/*.toml": "project dot files in a repository; dots.DecodeStrict refuses keys it doesn't know",
}
```

- [ ] **Step 5: Classify every `paths.Paths` getter the merged phases added**

Run: `GOTOOLCHAIN=local go test ./internal/migrate/ -run TestEveryPathsFileIsClassified -v`
Expected: PASS (at `dffabbf` the getters are `DotsDir`, `ConfigFile`, `EngineLock`, `EngineLog`, `AttachmentsFile`, `QueueFile`, `FeedFile`, `UsageFile`, `DotsPanesFile`, `HistoryDir`, `TrustFile`, `WorktreesDir`, `PatchesDir`, `CredentialsFile`, `PricesFile`, `DailyFile`, `InboxFile`, `MemoryDir`, `StatePointerFile`, `EngineEndpoint` and, on Unix, `EngineSock`; every one is classified above). If it fails naming a method, Phase 9's final fix wave added a persisted path: add a kind to `Default()` (and the method to `KindPaths`) when it is a whole-file JSON or TOML store, or add the method to `Unversioned` with the reason (append-only JSONL, runtime lock, cache, socket). A state-root kind without a lock gets no `Lock` (it can't be migrated until it declares one).

- [ ] **Step 6: Run the tests**

Run: `GOTOOLCHAIN=local go test -race ./internal/fsutil/ ./internal/migrate/ -v 2>&1 | grep -E '^(=== RUN|--- |ok|FAIL|PASS)' | grep -v '=== RUN'`
Expected: PASS for `TestProtectedFileRefusesAtomicWritesAndKeepsItsBytes`, `TestProbeReadsVersionsAndDefaultsToOne`, `TestRunUpgradesOldFilesBacksThemUpAndIsIdempotent`, `TestNewerFileIsReadOnlyAndNeverRewritten`, `TestFailedMigrationLeavesTheFileAndMarksItReadOnly`, `TestMigrationThatForgetsTheVersionFails`, `TestCorruptFileIsReportedAndUntouched`, `TestDryRunReportsPendingWithoutWriting`, `TestLockedFileWaitsThenFailsWithoutChange`, `TestValidateRefusesIncompleteOrUnsafeChains`, `TestBackupNeverOverwritesAnEarlierBackup`, `TestEveryReleasedVersionIsCurrentAndStillLoads` (subtests 0.1, 0.2, 0.3), `TestFutureQueueIsReadButNeverRewritten`, `TestEveryPathsFileIsClassified`, `TestFilesWrittenByPhases4To9AreCurrent`, `TestNewerPhase4To9FilesStayReadOnly`, `TestBrokenCredentialsAreReportedWithoutTheirText`, `TestExternalFormatsRefuseNewerVersions` (all verified at `dffabbf` by the reconcile pass).

If `TestFilesWrittenByPhases4To9AreCurrent` fails after Phase 9's final fix wave because a Phase 4–9 file is reported `Corrupt` (not a JSON object, not TOML), make that kind's `Format` match the file or move its getter to `Unversioned` with the reason.

- [ ] **Step 7: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed (every existing test still passes: no writer changed).

- [ ] **Step 8: Commit (never push)**

```bash
git add internal/migrate internal/fsutil/readonly.go internal/fsutil/readonly_test.go internal/fsutil/atomic.go
git commit -m "feat: schema versions and data-migration framework with a read-only gate for newer files (Phase 10)"
```

---

### Task 3: Fuzz targets, fuzz script, fixes for what they find

**Depends on:** none (Wave 1).

**Files:**
- Create: `internal/backend/fuzz_test.go`, `internal/transcript/fuzz_test.go`, `internal/memory/fuzz_test.go`, `internal/memory/testdata/fuzz/FuzzImportTOML/8a748f204ecc8c6f`, `internal/council/fuzz_test.go`, `internal/keys/fuzz_test.go`, `internal/config/fuzz_test.go`, `internal/dots/fuzz_test.go`, `internal/ipc/fuzz_test.go`, `internal/schedule/fuzz_test.go`, `internal/dotpack/fuzz_test.go`, `internal/mcpinstall/fuzz_test.go`, `internal/mcpinstall/testdata/fuzz/FuzzCodexBlock/a763424096dd21b2`, `internal/credentials/fuzz_test.go`, `internal/worktree/fuzz_test.go`, `internal/ui/diff/fuzz_test.go`, `scripts/fuzz.sh`
- Modify: `internal/memory/tomlio.go` (`ExportTOML`); any parser a fuzz crash points at

**Interfaces:**
- Consumes (all verified at `dffabbf`): `backend.ParseVerdict`, `ParseReply`, `ParseMemoryOps`, `ParseClaudeOutput`, `ParseCodexOutput`, `Result`, `MaxReviewOps`, `MaxPassOps`, `MemoryOp`; `transcript.ParseClaude`, `ParseCodex`, `FromDots`, `DotPrefix`, `Format`, the test fixtures `claudeSample`, `codexSample`; `memory.ExportTOML`, `ImportTOML`, `ScanSecrets`, `Open`, `List`, `ErrCorrupt`, `ErrVersion`, the test helpers `tomlFixture`, `idNum`; `council.ParseDigest`, `ParseStance`, `Stance*`; `keys.Existing`, `Plan`, `Block`, `Defaults`; `config.Load`; `dots.LoadAll`, `Load`, `Save`, `templateFS`, `DecodeStrict`, `DecodeProject`, `ProjectFile{Base, Path, Rel, Data}`, `IsProjectSlug`, `OriginProject`, `OriginImport`, `ClampDot`, `Clamp.Fact`, `ModeInject`; `ipc.serveConn(ctx, conn, h)` (unexported, same package), `ipc.CmdPing`; Phase 8 `schedule.Parse`, `Spec.Next`, `Spec.Prev`; Phase 9 `dotpack.Export(d dots.Dot, mem []memory.Entry, o ExportOptions)`, `ExportOptions{Note, Version, Now}`, `Parse`, `Prepare(p Pack, keep []string) (dots.Dot, []dots.Clamp, error)`, `memory.ImportKinds`; Phase 5 `mcpinstall.CodexBlock`, `UpsertCodexBlock`, `RemoveCodexBlock`, `BlockBegin`, `BlockEnd`; Phase 4 `credentials.File.List`, `Status.Last4` (`"…"` plus the last four bytes); Phase 7 `worktree.diffHeaders`, `unquoteC`, `gitModes`, `symlinkMode` (unexported, same package) and `ui/diff.Parse`, `File`.
- Produces: fuzz targets `FuzzParseVerdict`, `FuzzParseReply`, `FuzzParseMemoryOps`, `FuzzParseCLIOutput`, `FuzzParseClaude`, `FuzzParseCodex`, `FuzzFromDots`, `FuzzImportTOML`, `FuzzScopeFileLoad`, `FuzzScanSecrets`, `FuzzParseDigest`, `FuzzParseStance`, `FuzzKeysExisting`, `FuzzConfigLoad`, `FuzzDotFile`, `FuzzDecodeStrict`, `FuzzIPCDecode`, `FuzzScheduleParse`, `FuzzDotpackParse`, `FuzzCodexBlock`, `FuzzCredentialsFile`, `FuzzDiffHeaders`, `FuzzUnquoteC`, `FuzzDiffViewParse` (24); `scripts/fuzz.sh [duration]` (default `30s`; Task 5's CI uses it, the release job runs `10m`). `ExportTOML` writes no `ref` for an entry whose `ID` is empty.

- [ ] **Step 1: Write the fuzz targets**

Create `internal/backend/fuzz_test.go`:

```go
package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// hasControl reports a control character other than newline and tab: model
// text must be sanitized before anything renders or stores it.
func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }) >= 0
}

// seedReplies adds the recorded live replies (Phase 2 fixtures) to the corpus.
func seedReplies(f *testing.F) {
	files, _ := filepath.Glob("../memory/testdata/replies/*.txt")
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	for _, s := range []string{
		`{"relevant":true,"severity":"warn","message":"Token logged."}`,
		"```json\n{\"relevant\":false}\n```",
		`{"relevant":true,"message":"x","remember":[{"op":"add","scope":"dot","kind":"note","text":"t"}]}`,
		`{"remember":null}`, `{"relevant":true,"message":"\u001b[2Jgone"}`, `}{`, ``,
	} {
		f.Add(s)
	}
}

func FuzzParseVerdict(f *testing.F) {
	seedReplies(f)
	f.Fuzz(func(t *testing.T, text string) {
		v, err := ParseVerdict(text)
		if err != nil {
			return
		}
		if v.Relevant && strings.TrimSpace(v.Message) == "" {
			t.Fatalf("relevant verdict with an empty message from %q", text)
		}
		if hasControl(v.Message) {
			t.Fatalf("unsanitized message %q", v.Message)
		}
		if v.Severity != "critical" && v.Severity != "warn" && v.Severity != "info" {
			t.Fatalf("severity %q", v.Severity)
		}
	})
}

func FuzzParseReply(f *testing.F) {
	seedReplies(f)
	f.Fuzz(func(t *testing.T, text string) {
		v, ops, err := ParseReply(text)
		if err != nil {
			if len(ops) != 0 {
				t.Fatal("ops returned with an error")
			}
			return
		}
		if v.Relevant && strings.TrimSpace(v.Message) == "" {
			t.Fatalf("relevant verdict with an empty message from %q", text)
		}
		if len(ops) > MaxReviewOps {
			t.Fatalf("%d ops, the cap is %d", len(ops), MaxReviewOps)
		}
		checkOps(t, ops)
	})
}

func FuzzParseMemoryOps(f *testing.F) {
	seedReplies(f)
	f.Fuzz(func(t *testing.T, text string) {
		ops, err := ParseMemoryOps(text)
		if err != nil {
			return
		}
		if len(ops) > MaxPassOps {
			t.Fatalf("%d ops, the cap is %d", len(ops), MaxPassOps)
		}
		checkOps(t, ops)
	})
}

func checkOps(t *testing.T, ops []MemoryOp) {
	t.Helper()
	for _, op := range ops {
		switch op.Op {
		case "add":
			if op.Text == "" {
				t.Fatalf("add without text: %+v", op)
			}
		case "update":
			if op.ID == "" || op.Text == "" {
				t.Fatalf("update without id or text: %+v", op)
			}
		case "forget":
			if op.ID == "" {
				t.Fatalf("forget without id: %+v", op)
			}
		default:
			t.Fatalf("op %q survived cleaning", op.Op)
		}
	}
}

// FuzzParseCLIOutput feeds arbitrary bytes to the parsers of the claude and
// codex CLIs' output (JSON with model text inside). An accepted review has a
// sanitized message and only clean memory ops.
func FuzzParseCLIOutput(f *testing.F) {
	f.Add([]byte(`{"is_error":false,"result":"{\"relevant\":true,\"severity\":\"warn\",\"message\":\"x\"}","total_cost_usd":0.01,"usage":{"input_tokens":3,"output_tokens":2}}`))
	f.Add([]byte(`{"is_error":true,"result":"\u001b[2Jboom","total_cost_usd":-1}`))
	f.Add([]byte(`{"type":"item.completed","item":{"type":"agent_message","text":"{\"relevant\":false}"}}` + "\n" + `{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`))
	f.Add([]byte(`{"type":"turn.failed","error":{"message":"rate limited"}}`))
	f.Fuzz(func(t *testing.T, out []byte) {
		for name, parse := range map[string]func([]byte) (Result, error){"claude": ParseClaudeOutput, "codex": ParseCodexOutput} {
			res, err := parse(out)
			if err != nil {
				continue
			}
			if hasControl(res.Verdict.Message) {
				t.Fatalf("%s: unsanitized message %q", name, res.Verdict.Message)
			}
			if len(res.Remember) > MaxReviewOps {
				t.Fatalf("%s: %d ops, the cap is %d", name, len(res.Remember), MaxReviewOps)
			}
			checkOps(t, res.Remember)
		}
	})
}
```

Create `internal/transcript/fuzz_test.go`:

```go
package transcript

import (
	"os"
	"strings"
	"testing"
)

func checkTurns(t *testing.T, turns []Turn) {
	t.Helper()
	for i, tr := range turns {
		if tr.Index != i+1 {
			t.Fatalf("turn %d has index %d", i+1, tr.Index)
		}
		for _, d := range tr.FromDots {
			if d == "" {
				t.Fatalf("empty dot name in %+v", tr.FromDots)
			}
		}
	}
	_ = Format(turns) // must not panic on any parsed turn
}

func FuzzParseClaude(f *testing.F) {
	f.Add(claudeSample)
	if b, err := os.ReadFile("testdata/claude_real.jsonl"); err == nil {
		f.Add(string(b))
	}
	f.Add(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"x"}]}}`)
	f.Fuzz(func(t *testing.T, data string) {
		turns, err := ParseClaude(strings.NewReader(data))
		if err == nil {
			checkTurns(t, turns)
		}
	})
}

func FuzzParseCodex(f *testing.F) {
	f.Add(codexSample)
	f.Add(`{"type":"event_msg","payload":{"type":"user_message","message":"[◆ Security dot] fix"}}`)
	f.Fuzz(func(t *testing.T, data string) {
		turns, err := ParseCodex(strings.NewReader(data))
		if err == nil {
			checkTurns(t, turns)
		}
	})
}

func FuzzFromDots(f *testing.F) {
	for _, s := range []string{
		DotPrefix("◆", "Security") + " JWT in localStorage.\n\n" + DotPrefix("▲", "Frontend") + " skeleton",
		"[◆ Security dot]", "x [◆ Security dot] mid-line", "[◆  dot]", "[ dot]", "[◆ A dot] [◆ B dot]\n[◆ A dot]",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		names := FromDots(text)
		seen := map[string]bool{}
		for _, n := range names {
			if n == "" || seen[n] {
				t.Fatalf("FromDots(%q) = %q: empty or repeated name", text, names)
			}
			seen[n] = true
			// The header the engine writes for that name parses back to it.
			if got := FromDots(DotPrefix("◆", n)); len(got) != 1 || got[0] != n {
				t.Fatalf("round trip of %q gave %q", n, got)
			}
		}
	})
}
```

Create `internal/memory/fuzz_test.go`:

```go
package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func FuzzImportTOML(f *testing.F) {
	if data, err := ExportTOML(tomlFixture()); err == nil {
		f.Add(string(data))
	}
	f.Add("version = 1\n[[entry]]\nref = \"p:m1\"\nkind = \"note\"\ntext = \"x\"\n")
	f.Add("version = 99\n")
	f.Fuzz(func(t *testing.T, data string) {
		es, err := ImportTOML([]byte(data))
		if err != nil {
			return
		}
		for _, e := range es {
			if e.Scope != ScopeProject && e.Scope != ScopeDot && e.Scope != ScopeGlobal {
				t.Fatalf("imported scope %q", e.Scope)
			}
		}
		// An import exports and imports again without loss.
		out, err := ExportTOML(es)
		if err != nil {
			t.Fatalf("re-export: %v", err)
		}
		back, err := ImportTOML(out)
		if err != nil {
			t.Fatalf("re-import of our own export: %v\n%s", err, out)
		}
		if len(back) != len(es) {
			t.Fatalf("round trip: %d entries became %d", len(es), len(back))
		}
		for i := range es {
			a, b := es[i], back[i]
			if a.ID != b.ID || a.Scope != b.Scope || a.Dot != b.Dot || a.Kind != b.Kind || a.Status != b.Status ||
				a.Pinned != b.Pinned || a.Text != b.Text || a.Source != b.Source || a.From != b.From || strings.Join(a.Paths, "\x00") != strings.Join(b.Paths, "\x00") {
				t.Fatalf("round trip changed entry %d: %+v -> %+v", i+1, a, b)
			}
		}
	})
}

// FuzzScopeFileLoad feeds arbitrary bytes to the scope-file loader through the
// store: it must report ErrCorrupt or ErrVersion, or return clean entries.
func FuzzScopeFileLoad(f *testing.F) {
	f.Add(`{"version":1,"next":1,"entries":[{"id":"m1","scope":"project","kind":"note","text":"x","status":"active","source":"user"}]}`)
	f.Add(`{"version":2,"entries":[]}`)
	f.Add(`{"entries":[{"text":"\u001b]52;c;aGk=\u0007hand edit"}]}`)
	f.Add(`{"version":1,"next":-5,"entries":[{"id":"m-3"},{"id":""}]}`)
	f.Fuzz(func(t *testing.T, data string) {
		s := Open(filepath.Join(t.TempDir(), "memory"), DefaultLimits())
		dir := filepath.Join(s.Dir, "g-0123456789abcdef")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "project.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		es, err := s.List("g-0123456789abcdef", Filter{Scope: ScopeProject})
		if err != nil {
			if !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrVersion) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			return
		}
		for _, e := range es {
			if strings.IndexFunc(e.Text, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }) >= 0 {
				t.Fatalf("hand-edited text not sanitized: %q", e.Text)
			}
			if idNum(e.ID) == 0 {
				t.Fatalf("entry without a usable id: %+v", e)
			}
		}
	})
}

func FuzzScanSecrets(f *testing.F) {
	for _, s := range []string{
		"AKIAABCDEFGHIJKLMNOP", "password = hunter2hunter2", "Bearer abcdefghijklmnop1234", "plain prose",
		"sk-ant-" + strings.Repeat("a1B2", 8), "https://user:pass@example.com/x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		found, kind := ScanSecrets(text)
		if found == (kind == "") {
			t.Fatalf("ScanSecrets(%q) = %v, %q", text, found, kind)
		}
	})
}
```

Create `internal/council/fuzz_test.go`:

```go
package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzParseDigest(f *testing.F) {
	files, _ := filepath.Glob("testdata/chair/*.txt")
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("## Must fix\n- Token logged (Security)\n## Not heard from\n- Frontend")
	f.Add("## Should fix\n- unattributed point")
	f.Add("- (")
	f.Fuzz(func(t *testing.T, message string) {
		body, verified := ParseDigest(message)
		if strings.IndexFunc(body, func(r rune) bool { return r != '\n' && r != '\t' && unicode.IsControl(r) }) >= 0 {
			t.Fatalf("digest body not sanitized: %q", body)
		}
		if verified && !strings.Contains(body, "## ") {
			t.Fatalf("verified digest without a section header: %q", body)
		}
	})
}

func FuzzParseStance(f *testing.F) {
	for _, s := range []string{"AGREE: yes", "**OBJECT:** no", "add: more", "ADD", "", "ÄGREE: x", "`AGREE:`"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, message string) {
		stance, rest := ParseStance(message)
		switch stance {
		case "":
			if rest != strings.TrimSpace(message) {
				t.Fatalf("no stance must keep the message: %q -> %q", message, rest)
			}
		case StanceAgree, StanceObject, StanceAdd:
			if utf8.RuneCountInString(rest) > utf8.RuneCountInString(message) {
				t.Fatalf("rest longer than the message: %q -> %q", message, rest)
			}
		default:
			t.Fatalf("unknown stance %q", stance)
		}
	})
}
```

Create `internal/keys/fuzz_test.go`:

```go
package keys

import "testing"

func FuzzKeysExisting(f *testing.F) {
	f.Add("[keys]\nprefix = \"ctrl+b\"\nsplit = \"prefix+v\"\n")
	f.Add(Block(Defaults()))
	f.Add("[[keys.command]]\nkey = \"prefix+d\"\ncommand = \"herdr-dots.manage\"\n[[keys.command]]\nkey = 3\n")
	f.Add("keys = 5\n")
	f.Add("[keys]\ncommand = [{ key = \"prefix+x\", command = \"a\" }, 7]\n")
	f.Fuzz(func(t *testing.T, data string) {
		m, err := Existing([]byte(data))
		if err != nil {
			return
		}
		for k := range m {
			if k == "" {
				t.Fatalf("empty key bound in %q", data)
			}
		}
		// Planning against any parsed config never panics and never adds a
		// key that is already taken.
		add, _, _ := Plan(Defaults(), m)
		for _, b := range add {
			if _, taken := m[b.Key]; taken {
				t.Fatalf("Plan adds %s, which %q already binds", b.Key, data)
			}
		}
	})
}
```

Create `internal/config/fuzz_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzConfigLoad checks the kill-switch rule on every input: a file that
// fails to load turns inject and memory off, and a loaded file is clamped.
func FuzzConfigLoad(f *testing.F) {
	f.Add("default_backend = \"claude-cli\"\ninject_enabled = true\nmemory_enabled = true\n")
	f.Add("max_parallel_runs = -4\nrun_timeout_seconds = 1\nmemory_max_entries = 99999999\n[council]\nrounds = 9\nmax_members = -1\n")
	f.Add("inject_enabled = \"yes\"\n")
	f.Add("[[council]]\n")
	f.Fuzz(func(t *testing.T, data string) {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(p)
		if err != nil {
			if c.InjectEnabled || c.MemoryEnabled {
				t.Fatalf("a config that failed to load left a kill switch on: %+v", c)
			}
			return
		}
		if c.MaxParallelRuns < 1 || c.RunTimeoutSeconds < 10 || c.SessionBudgetCalls < 1 || c.TurnDebounceMs < 0 ||
			c.MemoryMaxEntries < 10 || c.MemoryMaxEntries > 10000 || c.Council.Rounds < 1 || c.Council.Rounds > 2 ||
			c.Council.MaxMembers < 2 || c.Council.MaxMembers > 16 {
			t.Fatalf("value out of range after Load: %+v", c)
		}
	})
}
```

Create `internal/dots/fuzz_test.go`:

```go
package dots

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzDotFile loads arbitrary dot files: each is either reported with its
// file name or loads as a dot that validates and saves back.
func FuzzDotFile(f *testing.F) {
	for _, name := range []string{"security", "frontend", "test-writer"} {
		if b, err := templateFS.ReadFile("templates/" + name + ".toml"); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("name = \"X\"\n[delivery]\nmode = \"inject\"\n[budget]\nmax_calls_per_session = -1\n")
	f.Add("name = \"\"\n")
	f.Add("name = \"A\"\n[backend]\nargs = [\"--dangerously-skip-permissions\"]\n")
	f.Fuzz(func(t *testing.T, data string) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "fuzz.toml"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		ds, bad := LoadAll(dir)
		if len(ds)+len(bad) != 1 {
			t.Fatalf("one file gave %d dots and %d errors", len(ds), len(bad))
		}
		if len(bad) == 1 {
			if bad[0].File != "fuzz.toml" || bad[0].Err == nil {
				t.Fatalf("load error without its file: %+v", bad[0])
			}
			return
		}
		d := ds[0]
		if err := d.Validate(); err != nil {
			t.Fatalf("LoadAll returned an invalid dot: %v", err)
		}
		out := t.TempDir()
		if err := Save(out, d, false); err != nil {
			t.Fatalf("a loaded dot must save: %v", err)
		}
		back, err := Load(out, d.Slug)
		if err != nil || back.Name != d.Name || back.Purpose != d.Purpose {
			t.Fatalf("save/load changed the dot: %+v vs %+v (%v)", back, d, err)
		}
	})
}

// FuzzDecodeStrict feeds arbitrary bytes to the strict decoder that reads
// repository project dots (files other people commit). A file it accepts
// never claims an origin of its own, and DecodeProject turns it into a
// project dot under a project-namespaced slug that validates.
func FuzzDecodeStrict(f *testing.F) {
	for _, name := range []string{"security", "frontend", "backend"} {
		if b, err := templateFS.ReadFile("templates/" + name + ".toml"); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("name = \"X\"\npurpose = \"p\"\norigin = \"user\"\n")
	f.Add("name = \"X\"\npurpose = \"p\"\n[capabilities]\nwrite = true\nfuture = 1\n")
	f.Add("name = \"X\"\npurpose = \"p\"\n[delivery]\nmode = \"inject\"\n")
	f.Add("version = 2\n")
	f.Fuzz(func(t *testing.T, data string) {
		d, err := DecodeStrict([]byte(data))
		if err != nil {
			return
		}
		if d.Origin != "" {
			t.Fatalf("DecodeStrict trusted the file's origin %q", d.Origin)
		}
		pf := ProjectFile{Base: "review", Path: "/repo/.herdr-dots/dots/review.toml", Rel: ".herdr-dots/dots/review.toml", Data: []byte(data)}
		pd, err := DecodeProject(pf, "g-0123456789abcdef")
		if err != nil {
			return
		}
		if pd.Origin != OriginProject || !IsProjectSlug(pd.Slug) || pd.SourcePath != pf.Path {
			t.Fatalf("project dot %q: origin %q, source %q", pd.Slug, pd.Origin, pd.SourcePath)
		}
		if err := pd.Validate(); err != nil {
			t.Fatalf("DecodeProject returned an invalid dot: %v", err)
		}
	})
}
```

Create `internal/ipc/fuzz_test.go`:

```go
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"herdr-dots/internal/view"
)

// fuzzHandler decodes every argument shape the engine uses and fails
// unknown commands, like the engine does.
type fuzzHandler struct{}

func (fuzzHandler) Handle(_ context.Context, cmd string, raw json.RawMessage) error {
	var v struct {
		PaneID string   `json:"pane_id"`
		Slugs  []string `json:"slugs"`
		IDs    []string `json:"ids"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
	}
	if cmd == CmdPing {
		return nil
	}
	return errors.New("unknown command")
}
func (fuzzHandler) Snapshot() view.Snapshot { return view.Snapshot{} }
func (fuzzHandler) Watch() (<-chan view.Snapshot, func()) {
	return make(chan view.Snapshot), func() {}
}

// FuzzIPCDecode feeds arbitrary bytes to one server connection. The server
// must answer or hang up, never panic or block once the client is done.
func FuzzIPCDecode(f *testing.F) {
	f.Add(`{"id":1,"cmd":"ping"}` + "\n")
	f.Add(`{"id":2,"cmd":"attach","args":{"pane_id":"w1:p1","slugs":["security"]}}` + "\n" + `{"id":3,"cmd":"bogus"}` + "\n")
	f.Add(`{"id":4,"cmd":"watch"}`)
	f.Add(`{"id":"x","cmd":7}`)
	f.Add(`{"id":5,"cmd":"send","args":"not an object"}`)
	f.Fuzz(func(t *testing.T, data string) {
		srv, cli := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { serveConn(ctx, srv, fuzzHandler{}); close(done) }()
		go func() { _, _ = io.Copy(io.Discard, cli) }() // read every reply
		_ = cli.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write([]byte(data))
		_ = cli.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("server connection did not end for input %q", data)
		}
	})
}
```

Create `internal/schedule/fuzz_test.go` (Phase 8's `schedule.Parse`; an impossible schedule may return a zero time):

```go
package schedule

import (
	"testing"
	"time"
)

func FuzzScheduleParse(f *testing.F) {
	for _, s := range []string{"0 9 * * 1-5", "*/15 * * * *", "0 0 31 2 *", "61 * * * *", "", "* * * *", "0 0 * * 7", "5,10-12/2 * * * *"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		spec, err := Parse(expr)
		if err != nil {
			return
		}
		base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		if next := spec.Next(base); !next.IsZero() && !next.After(base) {
			t.Fatalf("Next(%v) = %v for %q: not after", base, next, expr)
		}
		if prev := spec.Prev(base); !prev.IsZero() && !prev.Before(base) {
			t.Fatalf("Prev(%v) = %v for %q: not before", base, prev, expr)
		}
	})
}
```

Create `internal/dotpack/fuzz_test.go` (Phase 9's `dotpack.Parse`, which already pre-scans for TOML bombs; preparing a pack clamps every risky capability and clamping again changes nothing):

```go
package dotpack

import (
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
)

// FuzzDotpackParse feeds arbitrary bytes to the pack parser, whose input
// comes from other people (a file or an https download). It must return
// quickly (the TOML bomb pre-scan), and a pack it accepts must only carry
// what may travel; preparing it clamps every risky capability, and clamping
// the prepared dot again changes nothing.
func FuzzDotpackParse(f *testing.F) {
	d := dots.New("Security")
	d.Purpose = "Flag secrets."
	if b, err := Export(d, nil, ExportOptions{Note: "seed", Version: "1.0.0-rc.1", Now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}); err == nil {
		f.Add(string(b))
	}
	f.Add("[pack]\nformat = 1\n[dot]\nname = \"x\"\npurpose = \"p\"\n")
	f.Add("[pack]\nformat = 99\n")
	f.Add("[pack]\nformat = 1\n[dot]\nname = \"W\"\npurpose = \"p\"\n[dot.capabilities]\nwrite = true\n[dot.delivery]\nmode = \"inject\"\n")
	f.Add("[pack]\nformat = 1\n[dot]\nname = \"M\"\npurpose = \"p\"\n[[memory]]\nscope = \"global\"\nkind = \"note\"\ntext = \"x\"\n")
	f.Add("a = " + strings.Repeat("[", 4000))
	f.Fuzz(func(t *testing.T, data string) {
		p, err := Parse([]byte(data))
		if err != nil {
			return
		}
		if p.Dot.Origin != dots.OriginImport {
			t.Fatalf("a parsed pack's dot has origin %q", p.Dot.Origin)
		}
		for _, e := range p.Memory {
			if (e.Scope != memory.ScopeDot && e.Scope != memory.ScopeProject) || !slices.Contains(memory.ImportKinds, e.Kind) {
				t.Fatalf("pack memory that may not travel: %+v", e)
			}
		}
		prepared, _, err := Prepare(p, nil)
		if err != nil {
			return
		}
		if prepared.Origin != dots.OriginImport || prepared.Capabilities.Write || prepared.Delivery.Mode == dots.ModeInject {
			t.Fatalf("Prepare kept a risky capability: %+v", prepared)
		}
		if _, again := dots.ClampDot(prepared, nil); slices.ContainsFunc(again, func(c dots.Clamp) bool { return !c.Fact }) {
			t.Fatalf("clamping a prepared dot changed it again: %+v", again)
		}
	})
}
```

Create `internal/mcpinstall/fuzz_test.go` (Phase 5's Codex block editor works on the developer's `~/.codex/config.toml`):

```go
package mcpinstall

import (
	"strings"
	"testing"
)

// FuzzCodexBlock edits arbitrary Codex config files the way setup-mcp edits
// the developer's ~/.codex/config.toml: installing twice is a no-op, and
// removing after an install leaves no marker line of ours. (The developer's
// own text may quote the marker inside a longer line; that line is theirs
// and stays: crasher a763424096dd21b2.)
func FuzzCodexBlock(f *testing.F) {
	f.Add("")
	f.Add("model = \"o3\"\n\n[mcp_servers.other]\ncommand = \"x\"\n")
	f.Add(CodexBlock("/x/bin/herdr-dots", "/c", "/s"))
	f.Add("[mcp_servers.herdr-dots]\ncommand = \"old\"\n")
	f.Add(BlockBegin + "\nbroken")
	f.Fuzz(func(t *testing.T, before string) {
		after, err := UpsertCodexBlock(before, "/x/bin/herdr-dots", "/c", "/s")
		if err != nil {
			return // refused as unsafe: nothing was written
		}
		again, err := UpsertCodexBlock(after, "/x/bin/herdr-dots", "/c", "/s")
		if err != nil || again != after {
			t.Fatalf("installing twice is not a no-op (%v):\n%s\n---\n%s", err, after, again)
		}
		removed, found, err := RemoveCodexBlock(after)
		if err != nil || !found {
			t.Fatalf("remove after install: found=%v err=%v\n%s", found, err, removed)
		}
		for _, l := range strings.Split(removed, "\n") {
			if m := strings.TrimSpace(l); m == BlockBegin || m == BlockEnd {
				t.Fatalf("remove after install left a marker line:\n%s", removed)
			}
		}
	})
}
```

Create `internal/credentials/fuzz_test.go` (Phase 4's file reader never shows more than the last four characters of a key; `Last4` is `"…"` plus four bytes):

```go
package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzCredentialsFile(f *testing.F) {
	f.Add("[anthropic]\napi_key = \"sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n")
	f.Add("[openai]\napi_key = 7\n")
	f.Add("[[anthropic]]\n")
	f.Fuzz(func(t *testing.T, data string) {
		p := filepath.Join(t.TempDir(), "credentials.toml")
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		sts, err := File{Path: p, Env: func(string) string { return "" }}.List()
		if err != nil {
			return
		}
		for _, s := range sts {
			// Last4 is "" or "…" plus the key's last four bytes, never more.
			if rest, ok := strings.CutPrefix(s.Last4, "…"); s.Last4 != "" && (!ok || len(rest) > 4) {
				t.Fatalf("List shows %q for %s: more than the last four characters", s.Last4, s.Provider)
			}
		}
	})
}
```

Create `internal/mcpinstall/testdata/fuzz/FuzzCodexBlock/a763424096dd21b2` (found by the reconcile pass's 10-second run: the developer's own comment quoting the marker text inside a longer line; it stays theirs, which is why the invariant compares whole marker lines):

```text
go test fuzz v1
string("# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)00")
```

Create `internal/worktree/fuzz_test.go` (Phase 7's diff header parser guards every drafted patch, which is model output):

```go
package worktree

import (
	"strings"
	"testing"
)

// FuzzDiffHeaders feeds arbitrary bytes to the diff header parser that
// guards every drafted patch (Phase 7): a patch is model output. It must
// never panic, and a diff it accepts never creates or changes a symlink and
// only states modes git prints.
func FuzzDiffHeaders(f *testing.F) {
	for _, s := range []string{
		"diff --git a/x.go b/x.go\nindex 1111111..2222222 100644\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n",
		"diff --git a/n b/n\nnew file mode 100755\nindex 0000000..1111111\n--- /dev/null\n+++ b/n\n@@ -0,0 +1 @@\n+x\n",
		"diff --git a/l b/l\nnew file mode 120000\nindex 0000000..1111111\n",
		"diff --git \"a/q\\\"q.txt\" \"b/q\\\"q.txt\"\nindex 1111111..2222222 100644\n",
		"diff --git a/a b/b\nrename from a\nrename to b\n",
		"diff --git a/x b/x\nold mode 100644\nnew mode 100755\n@@ -1 +1 @@\nnew mode 120000\n",
		"--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n",
		"diff --git a/x b/x\n+++ b/x\nnew mode 120000\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, diff []byte) {
		modes, renames, err := diffHeaders(diff)
		if err != nil || renames {
			return
		}
		if len(modes) == 0 {
			t.Fatal("an accepted diff states no path")
		}
		for path, m := range modes {
			if path == "" {
				t.Fatalf("empty path in %q", diff)
			}
			if m.new == symlinkMode {
				t.Fatalf("%s becomes a symlink in an accepted diff: %q", path, diff)
			}
			for _, mode := range []string{m.old, m.new} {
				if mode != "000000" && !gitModes[mode] {
					t.Fatalf("%s has mode %q, which git never prints", path, mode)
				}
			}
		}
	})
}

// FuzzUnquoteC checks git's C-quoting reader: a name without quotes,
// backslashes or control bytes reads back as itself, and nothing panics.
func FuzzUnquoteC(f *testing.F) {
	for _, s := range []string{`"a/b c"`, `"\303\251"`, `"\"x\""`, `"\777"`, `"unterminated`, `"a\`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _, _ = unquoteC(s)
		if strings.ContainsAny(s, "\"\\") || strings.IndexFunc(s, func(r rune) bool { return r < 0x20 }) >= 0 {
			return
		}
		name, rest, ok := unquoteC(`"` + s + `" tail`)
		if !ok || name != s || rest != " tail" {
			t.Fatalf("unquoteC(%q) = %q, %q, %v", s, name, rest, ok)
		}
	})
}
```

Create `internal/ui/diff/fuzz_test.go` (the patch viewer shows model-written diffs):

```go
package diff

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzDiffViewParse feeds arbitrary patches to the viewer's parser: the
// patch text comes from a model. One file per "diff --git " line, hunk
// indexes inside the file, and no line it keeps for the screen carries a
// control, tab, newline or format character.
func FuzzDiffViewParse(f *testing.F) {
	f.Add([]byte("diff --git a/x.go b/x.go\nindex 1111111..2222222 100755\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n"))
	f.Add([]byte("diff --git a/l b/l\nnew file mode 120000\n@@ -0,0 +1 @@\n+\x1b]52;c;aGk=\x07target\n"))
	f.Add([]byte("diff --git a/b.bin b/b.bin\nGIT binary patch\nliteral 3\nKcmV+\n"))
	f.Add([]byte("noise\ndiff --git a/‮q b/‮q\n"))
	f.Fuzz(func(t *testing.T, patch []byte) {
		files := Parse(patch)
		want := 0
		for _, l := range strings.Split(strings.TrimRight(string(patch), "\n"), "\n") {
			if strings.HasPrefix(l, "diff --git ") {
				want++
			}
		}
		if len(files) != want {
			t.Fatalf("%d files for %d \"diff --git\" lines", len(files), want)
		}
		unsafe := func(r rune) bool {
			return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
		}
		for _, fl := range files {
			if strings.IndexFunc(fl.Path, unsafe) >= 0 {
				t.Fatalf("path %q reaches the screen raw", fl.Path)
			}
			for _, h := range fl.Hunks {
				if h < 0 || h >= len(fl.Lines) {
					t.Fatalf("hunk index %d outside %d lines", h, len(fl.Lines))
				}
			}
			for _, l := range fl.Lines {
				if strings.IndexFunc(l, unsafe) >= 0 {
					t.Fatalf("line %q reaches the screen raw", l)
				}
			}
		}
	})
}
```

Create `scripts/fuzz.sh`:

```sh
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
```

- [ ] **Step 2: Run the seed corpora (fuzz targets run as tests)**

Run: `GOTOOLCHAIN=local go test -run '^Fuzz' ./internal/... 2>&1 | grep -v '^ok' | head -20`
Expected: no output (every package `ok`; verified at `dffabbf` by the reconcile pass, before the Step 3 crasher exists).

- [ ] **Step 3: Fuzz `ImportTOML` and see the known failure**

Run: `GOTOOLCHAIN=local go test ./internal/memory/ -run XXX -fuzz FuzzImportTOML -fuzztime 20s 2>&1 | tail -8`
Expected: FAIL within seconds, `re-import of our own export: entry 1: invalid memory request: memory id "d:" must look like p:m3, d:m9 or g:m2`, and a new file under `internal/memory/testdata/fuzz/FuzzImportTOML/`. (An imported block without `ref` has no id; `ExportTOML` wrote `ref = "d:"` for it, which nothing can import.) Delete the generated file, then keep the input found while planning (it still fails at `dffabbf`) as the regression input.

Create `internal/memory/testdata/fuzz/FuzzImportTOML/8a748f204ecc8c6f` (exact name and content):

```text
go test fuzz v1
string("[[entrY]]\nsCope=\"dot\"\ndot=\"0\"\nkind=\"dismissed\"")
```

- [ ] **Step 4: Fix `ExportTOML`**

Apply to `internal/memory/tomlio.go`:

```diff
--- a/internal/memory/tomlio.go
+++ b/internal/memory/tomlio.go
@@ -44,7 +44,11 @@ func ExportTOML(entries []Entry) ([]byte, error) {
 		if paths == nil {
 			paths = []string{}
 		}
-		doc.Entry = append(doc.Entry, tomlEntry{Ref: e.Ref().String(), Scope: string(e.Scope), Dot: e.Dot,
+		ref := ""
+		if e.ID != "" { // an entry not stored yet has no ref (a "d:" ref would not import)
+			ref = e.Ref().String()
+		}
+		doc.Entry = append(doc.Entry, tomlEntry{Ref: ref, Scope: string(e.Scope), Dot: e.Dot,
 			Kind: string(e.Kind), Status: string(e.Status), Source: string(e.Source), Pinned: e.Pinned, Paths: paths, Text: e.Text, From: e.From})
 	}
 	var buf bytes.Buffer
```

Run: `GOTOOLCHAIN=local go test ./internal/memory/ ./cmd/herdr-dots/ -run 'FuzzImportTOML|TOML|Export|Diff' -v 2>&1 | grep -E '^(--- FAIL|ok|FAIL)|8a748f204ecc8c6f'`
Expected: `--- PASS: FuzzImportTOML/8a748f204ecc8c6f` and both packages `ok` (the memory CLI's export and edit still round-trip).

- [ ] **Step 5: Fuzz every target for 60 seconds and fix what breaks**

Run each target alone (Go fuzzes one target per invocation):

```bash
for t in FuzzParseVerdict FuzzParseReply FuzzParseMemoryOps FuzzParseCLIOutput; do GOTOOLCHAIN=local go test ./internal/backend/ -run XXX -fuzz "^$t\$" -fuzztime 60s || break; done
for t in FuzzParseClaude FuzzParseCodex FuzzFromDots; do GOTOOLCHAIN=local go test ./internal/transcript/ -run XXX -fuzz "^$t\$" -fuzztime 60s || break; done
for t in FuzzImportTOML FuzzScopeFileLoad FuzzScanSecrets; do GOTOOLCHAIN=local go test ./internal/memory/ -run XXX -fuzz "^$t\$" -fuzztime 60s || break; done
for t in FuzzParseDigest FuzzParseStance; do GOTOOLCHAIN=local go test ./internal/council/ -run XXX -fuzz "^$t\$" -fuzztime 60s || break; done
GOTOOLCHAIN=local go test ./internal/keys/ -run XXX -fuzz FuzzKeysExisting -fuzztime 60s
GOTOOLCHAIN=local go test ./internal/config/ -run XXX -fuzz FuzzConfigLoad -fuzztime 60s
for t in FuzzDotFile FuzzDecodeStrict; do GOTOOLCHAIN=local go test ./internal/dots/ -run XXX -fuzz "^$t\$" -fuzztime 60s || break; done
GOTOOLCHAIN=local go test ./internal/ipc/ -run XXX -fuzz FuzzIPCDecode -fuzztime 60s
GOTOOLCHAIN=local go test ./internal/schedule/ -run XXX -fuzz FuzzScheduleParse -fuzztime 60s
GOTOOLCHAIN=local go test ./internal/dotpack/ -run XXX -fuzz FuzzDotpackParse -fuzztime 60s
GOTOOLCHAIN=local go test ./internal/mcpinstall/ -run XXX -fuzz FuzzCodexBlock -fuzztime 60s
GOTOOLCHAIN=local go test ./internal/credentials/ -run XXX -fuzz FuzzCredentialsFile -fuzztime 60s
for t in FuzzDiffHeaders FuzzUnquoteC; do GOTOOLCHAIN=local go test ./internal/worktree/ -run XXX -fuzz "^$t\$" -fuzztime 60s || break; done
GOTOOLCHAIN=local go test ./internal/ui/diff/ -run XXX -fuzz FuzzDiffViewParse -fuzztime 60s
```

Expected: `PASS` for each. The reconcile pass ran every target for 10 s and then 30 s at `dffabbf` with the Step 4 fix: all passed except `FuzzCodexBlock`, whose crasher `a763424096dd21b2` showed that the invariant was wrong (the developer's own comment may quote the marker text inside a longer line); the invariant in Step 1 is the corrected one and the crasher is committed as a regression input. For every failure: read the crasher (`cat <pkg>/testdata/fuzz/<Target>/<id>`), decide whether the parser or the invariant is wrong (an invariant may only be relaxed when the spec allows the behavior, and the reason goes in the commit message), fix it with the smallest change in that package, keep the crasher file as a regression input, and rerun that target. Never touch a file Tasks 2, 4 or 5 own. A target that slows to `0/sec` for over a minute is a hang: treat it like a crash (Go reports it at the end of `-fuzztime` as a failure).

- [ ] **Step 6: Lint the script**

Run: `shellcheck -s sh scripts/fuzz.sh && echo shellcheck-ok`
Expected: `shellcheck-ok`.

- [ ] **Step 7: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 8: Commit (never push)**

```bash
git add internal/backend/fuzz_test.go internal/transcript/fuzz_test.go internal/memory/fuzz_test.go internal/memory/tomlio.go internal/memory/testdata/fuzz internal/council/fuzz_test.go internal/keys/fuzz_test.go internal/config/fuzz_test.go internal/dots/fuzz_test.go internal/ipc/fuzz_test.go internal/schedule/fuzz_test.go internal/dotpack/fuzz_test.go internal/mcpinstall/fuzz_test.go internal/mcpinstall/testdata/fuzz internal/credentials/fuzz_test.go internal/worktree/fuzz_test.go internal/ui/diff/fuzz_test.go scripts/fuzz.sh
git add -u   # only the parser files a crash pointed at
git commit -m "test: fuzz targets for every parser; ExportTOML writes no ref for an unsaved entry (Phase 10)"
```

---

### Task 4: Snapshot throttling and trimming, soak test

**Depends on:** none (Wave 1).

**Files:**
- Create: `internal/view/trim.go`, `internal/view/trim_test.go`, `internal/engine/broadcast.go`, `internal/engine/broadcast_test.go`, `internal/engine/soak_test.go`, `internal/engine/soak_darwin_test.go`, `internal/engine/soak_linux_test.go`
- Modify: `internal/engine/engine.go` (struct field, `Snapshot` renamed, old `broadcast` removed)

**Interfaces:**
- Consumes: `view.Snapshot`, `view.Pane{Cards []queue.Card; Feed []state.FeedEntry}` (at `dffabbf` the engine already caps each pane's feed at 20 via `Feed.Recent(pane, 20)`; panes also carry `Drafts`, `Council` and the library, which Trim leaves alone); the engine's `e.mu`, `e.watchers`, `e.spawn(f func()) bool` (false after `Wait`); `(*Engine).TogglePauseAll()`; the engine test rig (`newRig`, `newRigWith`, `dot`, `critical`, `fakeReader`, `fakeBackend.set(vs ...backend.Verdict)` (the last verdict repeats), `fakeBackend.n()`, `rig.srv`, `rig.be`, `rig.q`, `rig.setReader`, `rig.mu`, `rig.readers`, `Deps.PollInterval`).
- Produces (package `view`): `const MaxSnapshotBytes = 256 << 10`, `MaxCardsPerPane = 100`, `MaxFeedPerPane = 50`; `func Trim(s Snapshot, maxBytes int) Snapshot` (never modifies `s`, never cuts a card).
- Produces (package `engine`): `const minBroadcastGap = 100 * time.Millisecond`; `type throttle struct{ mu sync.Mutex; pending bool; last time.Time }`; `Engine.bc throttle`; `(*Engine) Snapshot() view.Snapshot` is now trimmed (same signature; `ipc.Handler` unchanged); `(*Engine) buildSnapshot() view.Snapshot` (the old body); `(*Engine) broadcast()` coalesces; `(*Engine) sendSnapshot()`; the soak test `TestSoak` (`-tags soak`, env `SOAK_DURATION`, default 15m, and `SOAK_IDLE`).

- [ ] **Step 1: Write the failing tests**

Create `internal/view/trim_test.go`:

```go
package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

func bigPane(id string, cards, feed, bodyLen int) Pane {
	p := Pane{PaneID: id}
	for i := 0; i < cards; i++ {
		p.Cards = append(p.Cards, queue.Card{ID: fmt.Sprintf("%s-c%03d", id, i), PaneID: id, Status: queue.StatusPending,
			Body: strings.Repeat("x", bodyLen), CreatedAt: time.Unix(int64(i), 0)})
	}
	for i := 0; i < feed; i++ {
		p.Feed = append(p.Feed, state.FeedEntry{PaneID: id, Turn: i, Outcome: "passed", Detail: strings.Repeat("d", 200)})
	}
	return p
}

func jsonSize(t *testing.T, s Snapshot) int {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

func TestTrimCapsCardsAndFeedPerPane(t *testing.T) {
	s := Snapshot{Panes: []Pane{bigPane("w1:p1", 150, 80, 10)}}
	got := Trim(s, MaxSnapshotBytes)
	p := got.Panes[0]
	if len(p.Cards) != MaxCardsPerPane || p.Cards[0].ID != "w1:p1-c000" || p.Cards[99].ID != "w1:p1-c099" {
		t.Fatalf("cards: %d, first %s", len(p.Cards), p.Cards[0].ID)
	}
	if len(p.Feed) != MaxFeedPerPane || p.Feed[0].Turn != 30 || p.Feed[49].Turn != 79 {
		t.Fatalf("feed keeps the newest 50: %d from turn %d", len(p.Feed), p.Feed[0].Turn)
	}
	if len(s.Panes[0].Cards) != 150 || len(s.Panes[0].Feed) != 80 {
		t.Fatal("Trim modified its input")
	}
}

func TestTrimStaysUnderTheByteBudgetWithoutCuttingACard(t *testing.T) {
	s := Snapshot{Panes: []Pane{bigPane("w1:p1", 100, 50, 6000), bigPane("w1:p2", 100, 50, 6000), bigPane("w1:p3", 3, 50, 10)}}
	got := Trim(s, MaxSnapshotBytes)
	if n := jsonSize(t, got); n > MaxSnapshotBytes {
		t.Fatalf("trimmed snapshot is %d bytes", n)
	}
	for _, p := range got.Panes {
		for _, c := range p.Cards {
			if len(c.Body) != len(s.Panes[0].Cards[0].Body) && p.PaneID != "w1:p3" {
				t.Fatalf("card %s was cut", c.ID)
			}
		}
		if len(p.Feed) > minFeedPerPane {
			t.Fatalf("feed of %s not shrunk first: %d", p.PaneID, len(p.Feed))
		}
	}
	if len(got.Panes[2].Cards) != 3 {
		t.Fatal("the small pane lost cards while the big ones had more")
	}
	if a, b := len(got.Panes[0].Cards), len(got.Panes[1].Cards); a < 1 || a-b > 1 || b-a > 1 {
		t.Fatalf("cards dropped unevenly: %d and %d", a, b)
	}
}

func TestTrimKeepsOneCardPerPaneEvenOverBudget(t *testing.T) {
	s := Snapshot{Panes: []Pane{bigPane("w1:p1", 5, 0, 300<<10)}}
	got := Trim(s, MaxSnapshotBytes)
	if len(got.Panes[0].Cards) != 1 || len(got.Panes[0].Cards[0].Body) != 300<<10 {
		t.Fatalf("floor: %d cards", len(got.Panes[0].Cards))
	}
}
```

Create `internal/engine/broadcast_test.go`:

```go
package engine

import (
	"testing"
	"time"
)

// A burst of 1,000 changes in one second reaches watchers as at most 11
// snapshots, and the last one shows the final state.
func TestBroadcastsAreCoalescedAndTheLastStateArrives(t *testing.T) {
	r := newRig(t)
	ch, stop := r.e.Watch()
	defer stop()
	for len(ch) > 0 {
		<-ch
	}
	got := 0
	done := make(chan struct{})
	var last bool
	go func() {
		defer close(done)
		deadline := time.After(1500 * time.Millisecond)
		for {
			select {
			case s := <-ch:
				got++
				last = s.PausedAll
			case <-deadline:
				return
			}
		}
	}()
	start := time.Now()
	for i := 0; i < 1000; i++ {
		r.e.TogglePauseAll() // each toggle broadcasts
		time.Sleep(time.Until(start.Add(time.Duration(i+1) * time.Millisecond)))
	}
	<-done
	if got > 11 {
		t.Fatalf("%d broadcasts for 1,000 changes in 1s; the cap is 11", got)
	}
	if last != r.e.Snapshot().PausedAll {
		t.Fatalf("watchers ended on PausedAll=%v, the engine is at %v", last, r.e.Snapshot().PausedAll)
	}
}

// A single change after a quiet period is not delayed.
func TestFirstBroadcastAfterAQuietGapIsImmediate(t *testing.T) {
	r := newRig(t)
	ch, stop := r.e.Watch()
	defer stop()
	time.Sleep(2 * minBroadcastGap)
	for len(ch) > 0 {
		<-ch
	}
	start := time.Now()
	r.e.TogglePauseAll()
	select {
	case s := <-ch:
		if !s.PausedAll {
			t.Fatal("snapshot without the change")
		}
		if d := time.Since(start); d > minBroadcastGap/2 {
			t.Fatalf("first broadcast took %v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/view/ ./internal/engine/ -run 'Trim|Broadcast' 2>&1 | tail -5`
Expected: FAIL to build: `undefined: Trim`, `undefined: MaxSnapshotBytes`, `undefined: minBroadcastGap`.

- [ ] **Step 3: `view.Trim`**

Create `internal/view/trim.go`:

```go
package view

import "encoding/json"

// Snapshot budgets (spec F9). The engine streams a snapshot to every UI on
// each change, so one must stay small however long a session runs.
const (
	MaxSnapshotBytes = 256 << 10
	MaxCardsPerPane  = 100
	MaxFeedPerPane   = 50
	minFeedPerPane   = 5
)

// Trim bounds s for broadcasting. Each pane keeps its first MaxCardsPerPane
// cards in queue order (most severe, then oldest) and its newest
// MaxFeedPerPane feed entries. While the JSON is still over maxBytes, feeds
// shrink to 5 entries, then the pane with the most cards drops its last card,
// one at a time, down to one card per pane. A card is shown whole or not at
// all: the UIs act on cards they show, so no card text is ever cut. Trim
// does not modify s.
func Trim(s Snapshot, maxBytes int) Snapshot {
	out := s
	out.Panes = make([]Pane, len(s.Panes))
	for i, p := range s.Panes {
		if len(p.Cards) > MaxCardsPerPane {
			p.Cards = p.Cards[:MaxCardsPerPane]
		}
		if len(p.Feed) > MaxFeedPerPane {
			p.Feed = p.Feed[len(p.Feed)-MaxFeedPerPane:]
		}
		out.Panes[i] = p
	}
	if size(out) <= maxBytes {
		return out
	}
	for i := range out.Panes {
		if f := out.Panes[i].Feed; len(f) > minFeedPerPane {
			out.Panes[i].Feed = f[len(f)-minFeedPerPane:]
		}
	}
	// Measure once, then subtract each dropped card (and its comma).
	for total := size(out); total > maxBytes; {
		most := -1
		for i, p := range out.Panes {
			if len(p.Cards) > 1 && (most < 0 || len(p.Cards) > len(out.Panes[most].Cards)) {
				most = i
			}
		}
		if most < 0 {
			break // one card per pane is the floor
		}
		c := out.Panes[most].Cards
		total -= size(c[len(c)-1]) + 1
		out.Panes[most].Cards = c[:len(c)-1]
	}
	return out
}

func size(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}
```

- [ ] **Step 4: Coalesced broadcasts**

Create `internal/engine/broadcast.go`:

```go
package engine

import (
	"sync"
	"time"

	"herdr-dots/internal/view"
)

// minBroadcastGap spaces snapshot broadcasts: at most 10 a second (spec F9).
const minBroadcastGap = 100 * time.Millisecond

// throttle is the broadcast state. Its mutex is a leaf lock: never held while
// taking e.mu, a pane lock or doing any I/O.
type throttle struct {
	mu      sync.Mutex
	pending bool      // a trailing broadcast is scheduled
	last    time.Time // when the last broadcast was sent
}

// Snapshot is the engine state for the UIs, trimmed to the snapshot budget.
func (e *Engine) Snapshot() view.Snapshot {
	return view.Trim(e.buildSnapshot(), view.MaxSnapshotBytes)
}

// broadcast tells every watcher about a change. The first change after a
// quiet gap is sent at once; changes within minBroadcastGap of a broadcast
// are coalesced into one trailing broadcast that carries the final state.
func (e *Engine) broadcast() {
	e.bc.mu.Lock()
	if e.bc.pending {
		e.bc.mu.Unlock()
		return
	}
	now := time.Now()
	wait := e.bc.last.Add(minBroadcastGap).Sub(now)
	if wait <= 0 {
		e.bc.last = now
		e.bc.mu.Unlock()
		e.sendSnapshot()
		return
	}
	e.bc.pending = true
	e.bc.mu.Unlock()
	if !e.spawn(func() {
		time.Sleep(wait)
		e.bc.mu.Lock()
		e.bc.pending = false
		e.bc.last = time.Now()
		e.bc.mu.Unlock()
		e.sendSnapshot()
	}) {
		// The engine is stopping (Wait ran): no trailing broadcast.
		e.bc.mu.Lock()
		e.bc.pending = false
		e.bc.mu.Unlock()
	}
}

// sendSnapshot hands the current snapshot to every watcher, replacing one
// it hasn't read yet.
func (e *Engine) sendSnapshot() {
	s := e.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ch := range e.watchers {
		select {
		case <-ch: // drop the stale snapshot
		default:
		}
		select {
		case ch <- s:
		default:
		}
	}
}
```

Apply to `internal/engine/engine.go` (three hunks against the merged engine: the `bc` field after the Phase 8 commit-poll fields, `Snapshot` renamed to `buildSnapshot` with a comment, and the old `broadcast` deleted, since it now lives in `broadcast.go`):

```diff
--- a/internal/engine/engine.go
+++ b/internal/engine/engine.go
@@ -178,6 +178,8 @@ type Engine struct {
 	commitAgain bool             // a HEAD check skipped a dot whose review was queued: check again when it ends
 	commitErrs  map[string]bool  // repo dir + git error already logged this session (commits.go); created lazily
 
+	bc throttle // spaces snapshot broadcasts (broadcast.go)
+
 	// wmu/wg/closed track background goroutines so Wait can drain them.
 	wmu    sync.Mutex
 	wg     sync.WaitGroup
@@ -1134,7 +1136,8 @@ func (e *Engine) feed(paneID, slug string, turn int, outcome, detail string) {
 	}
 }
 
-func (e *Engine) Snapshot() view.Snapshot {
+// buildSnapshot is the full, untrimmed engine state (Snapshot trims it).
+func (e *Engine) buildSnapshot() view.Snapshot {
 	e.mu.Lock()
 	defer e.mu.Unlock()
 	s := view.Snapshot{InjectEnabled: e.config().InjectEnabled, PausedAll: e.pausedAll, WriteEnabled: e.config().WriteEnabled}
@@ -1230,22 +1233,6 @@ func (e *Engine) Watch() (<-chan view.Snapshot, func()) {
 	}
 }
 
-func (e *Engine) broadcast() {
-	s := e.Snapshot()
-	e.mu.Lock()
-	defer e.mu.Unlock()
-	for _, ch := range e.watchers {
-		select {
-		case <-ch: // drop the stale snapshot
-		default:
-		}
-		select {
-		case ch <- s:
-		default:
-		}
-	}
-}
-
 func tail(turns []transcript.Turn, n int) []transcript.Turn {
 	if len(turns) > n {
 		return turns[len(turns)-n:]
```

Check that nothing else in the engine calls a now-missing name and that no `broadcast` caller holds `e.mu` (it never did: `Snapshot` takes `e.mu`):

Run: `grep -n 'func (e \*Engine) broadcast\|func (e \*Engine) Snapshot\|func (e \*Engine) buildSnapshot' internal/engine/*.go`
Expected: exactly `broadcast.go: broadcast`, `broadcast.go: Snapshot`, `engine.go: buildSnapshot`.

- [ ] **Step 5: Run the tests**

Run: `GOTOOLCHAIN=local go test -race ./internal/view/ ./internal/engine/ ./internal/ipc/ ./internal/ui/... ./cmd/... 2>&1 | tail -12`
Expected: every package `ok`, including `TestTrimCapsCardsAndFeedPerPane`, `TestTrimStaysUnderTheByteBudgetWithoutCuttingACard`, `TestTrimKeepsOneCardPerPaneEvenOverBudget`, `TestBroadcastsAreCoalescedAndTheLastStateArrives`, `TestFirstBroadcastAfterAQuietGapIsImmediate`, and every existing engine test (they read `Snapshot()` directly, which is not throttled).

- [ ] **Step 6: The soak test**

Create `internal/engine/soak_test.go`:

```go
//go:build soak && (darwin || linux)

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// Soak budgets (spec F9).
const (
	soakMaxIdleCPU = 0.01     // idle engine CPU, averaged over the idle window
	soakMaxRSS     = 80 << 20 // peak resident memory
	soakDots       = 12
	soakPanes      = 4
)

func soakEnv(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func peakRSS() int64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return int64(ru.Maxrss) * rssUnit
}

// TestSoak runs 12 scripted dots on 4 panes: turns end every few seconds on
// a random pane, half the runs flag something, and a simulated developer
// sends or dismisses cards every minute. Then the panes go quiet and the
// idle engine's CPU is measured.
//
//	SOAK_DURATION=8h SOAK_IDLE=10m go test -tags soak -run TestSoak -timeout 9h -v ./internal/engine/
func TestSoak(t *testing.T) {
	busy := soakEnv("SOAK_DURATION", 15*time.Minute)
	idle := soakEnv("SOAK_IDLE", min(10*time.Minute, busy/3))
	var ds []dots.Dot
	var slugs []string
	for i := 0; i < soakDots; i++ {
		d := dot(fmt.Sprintf("Soak %02d", i), dots.ModeQueue)
		d.Budget.MaxCallsPerSession = 1 << 30
		ds = append(ds, d)
		slugs = append(slugs, dots.Slugify(d.Name))
	}
	r := newRigWith(t, func(d *Deps) {
		d.Config.SessionBudgetCalls = 1 << 30
		d.Config.MaxParallelRuns = 3
		d.PollInterval = time.Second
	}, ds...)
	r.be.set(critical, backend.Verdict{Relevant: false, Severity: "info"})
	panes := make([]string, soakPanes)
	for i := range panes {
		panes[i] = fmt.Sprintf("w1:p%d", i+1)
		sess := fmt.Sprintf("s%d", i+1)
		r.srv.SetPane(herdr.PaneInfo{PaneID: panes[i], Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
			AgentSession: &herdr.AgentSession{Kind: "id", Value: sess}})
		r.setReader(sess, &fakeReader{})
		if err := r.e.Attach(context.Background(), panes[i], slugs); err != nil {
			t.Fatal(err)
		}
	}
	ch, stop := r.e.Watch()
	defer stop()
	var sent, biggest atomic.Int64
	go func() {
		for s := range ch {
			sent.Add(1)
			if b, _ := json.Marshal(s); int64(len(b)) > biggest.Load() {
				biggest.Store(int64(len(b)))
			}
		}
	}()
	startGoroutines := runtime.NumGoroutine()

	t.Logf("busy phase %v, then idle %v", busy, idle)
	end := time.Now().Add(busy)
	nextTidy := time.Now().Add(time.Minute)
	for turn := 0; time.Now().Before(end); turn++ {
		p := panes[turn%soakPanes]
		r.mu.Lock()
		rd := r.readers[fmt.Sprintf("s%d", turn%soakPanes+1)]
		r.mu.Unlock()
		rd.add(transcript.Turn{User: fmt.Sprintf("change %d", turn), Assistant: "done"})
		r.srv.Push(p, herdr.StatusWorking)
		r.srv.Push(p, herdr.StatusIdle)
		if time.Now().After(nextTidy) { // the developer works through the queue
			for i, c := range r.q.All() {
				if c.Status == "pending" {
					if i%2 == 0 {
						_ = r.e.Dismiss(c.ID)
					} else {
						_ = r.e.SendCards([]string{c.ID})
					}
				}
			}
			nextTidy = time.Now().Add(time.Minute)
		}
		time.Sleep(3 * time.Second)
	}

	time.Sleep(30 * time.Second) // let the last runs finish
	cpu0, wall0 := cpuTime(), time.Now()
	time.Sleep(idle)
	idleCPU := float64(cpuTime()-cpu0) / float64(time.Since(wall0))
	rss := peakRSS()
	goroutines := runtime.NumGoroutine()
	snap, _ := json.Marshal(r.e.Snapshot())
	t.Logf("runs %d, broadcasts %d, biggest snapshot %d B, final snapshot %d B, peak RSS %.1f MB, idle CPU %.3f%%, goroutines %d -> %d",
		r.be.n(), sent.Load(), biggest.Load(), len(snap), float64(rss)/(1<<20), idleCPU*100, startGoroutines, goroutines)

	if idleCPU > soakMaxIdleCPU {
		t.Errorf("idle CPU %.3f%% is over the 1%% budget", idleCPU*100)
	}
	if rss > soakMaxRSS {
		t.Errorf("peak RSS %.1f MB is over the 80 MB budget", float64(rss)/(1<<20))
	}
	if b := biggest.Load(); b > view.MaxSnapshotBytes {
		t.Errorf("a snapshot of %d bytes was broadcast; the budget is %d", b, view.MaxSnapshotBytes)
	}
	if maxSent := int64((busy+idle+30*time.Second)/minBroadcastGap) + 1; sent.Load() > maxSent {
		t.Errorf("%d broadcasts in %v: more than 10 a second", sent.Load(), busy+idle)
	}
	if goroutines > startGoroutines+10 {
		t.Errorf("goroutines grew from %d to %d", startGoroutines, goroutines)
	}
}
```

Create `internal/engine/soak_darwin_test.go`:

```go
//go:build soak && darwin

package engine

// rssUnit converts getrusage's ru_maxrss to bytes: macOS reports bytes.
const rssUnit = 1
```

Create `internal/engine/soak_linux_test.go`:

```go
//go:build soak && linux

package engine

// rssUnit converts getrusage's ru_maxrss to bytes: Linux reports kilobytes.
const rssUnit = 1024
```

- [ ] **Step 7: Run a short soak**

Run: `GOTOOLCHAIN=local go vet -tags soak ./internal/engine/ && SOAK_DURATION=60s SOAK_IDLE=30s GOTOOLCHAIN=local go test -tags soak -run TestSoak -timeout 10m -v ./internal/engine/ 2>&1 | tail -6`
Expected: `--- PASS: TestSoak` with a log line like `runs …, broadcasts …, biggest snapshot … B, final snapshot … B, peak RSS … MB, idle CPU …%, goroutines … -> …` (the reconcile pass ran this exact command at `dffabbf` with the Task 4 code: `runs 240, broadcasts 57, biggest snapshot 23141 B, final snapshot 23133 B, peak RSS 21.8 MB, idle CPU 0.579%, goroutines 21 -> 21`, in 120 s). The long run is Task 13's.

- [ ] **Step 8: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed (the soak files build only with `-tags soak`, which the gate vets).

- [ ] **Step 9: Commit (never push)**

```bash
git add internal/view/trim.go internal/view/trim_test.go internal/engine/broadcast.go internal/engine/broadcast_test.go internal/engine/engine.go internal/engine/soak_test.go internal/engine/soak_darwin_test.go internal/engine/soak_linux_test.go
git commit -m "feat(engine): at most 10 snapshot broadcasts a second, snapshots under 256 KB, soak test (Phase 10)"
```

---

### Task 5: Version 1.0.0-rc.1, build info, GoReleaser, workflows, Makefile

**Depends on:** none (Wave 1).

**Files:**
- Create: `internal/version/VERSION`, `internal/version/version_test.go`, `.goreleaser.yaml`, `.github/workflows/release.yml`, `release/SHA256SUMS` (empty), `release/REPO` (empty), `scripts/release-local.sh`
- Modify: `internal/version/version.go` (replaced whole), `cmd/herdr-dots/main.go`, `herdr-plugin.toml`, `.github/workflows/ci.yml` (replaced whole), `Makefile`, `.gitignore`, every `*_test.go` that pins `0.9.0` (Step 5)

**Interfaces:**
- Consumes: nothing from other Phase 10 tasks (`ci.yml` runs Task 3's `scripts/fuzz.sh` and Task 4's soak test once both are merged; the file only names them).
- Produces: `internal/version/VERSION` (`1.0.0-rc.1\n`), the single source of the version for Go (`go:embed`) and the install scripts; package `version`: `var Version string`, `var Source = "source"`, `var Commit = ""` (both set with `-ldflags -X herdr-dots/internal/version.Source=...`), `func CommitID() string`, `func String() string`; `herdr-dots version` and `herdr-dots --version` print `version.String()` (`printVersion` in `main`); archive names `herdr-dots_<VERSION>_<os>_<arch>.tar.gz` (`.zip` on Windows) and the checksum file `SHA256SUMS` (Task 6 depends on these names); `release/REPO` and `release/SHA256SUMS` paths; `scripts/release-local.sh` (env `HERDR_DOTS_TARGETS` narrows the targets); Makefile targets `fuzz`, `soak`, `snapshot`, `vulncheck`.

- [ ] **Step 1: Write the failing test**

Create `internal/version/version_test.go`:

```go
package version

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestVersionIsTheReleaseCandidate(t *testing.T) {
	// Phase 10 ships a release candidate. 1.0.0 is tagged only by the
	// developer, after the three-OS acceptance run (a user gate).
	if Version != "1.0.0-rc.1" {
		t.Fatalf("Version = %q, want 1.0.0-rc.1", Version)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-rc\.\d+)?$`).MatchString(Version) {
		t.Fatalf("VERSION %q is not semver", Version)
	}
}

func TestManifestCarriesTheSameVersion(t *testing.T) {
	b, err := os.ReadFile("../../herdr-plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Version string `toml:"version"`
	}
	if _, err := toml.Decode(string(b), &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != Version {
		t.Fatalf("herdr-plugin.toml says %q, VERSION says %q", m.Version, Version)
	}
}

func TestStringNamesVersionSourceAndCommit(t *testing.T) {
	old := Commit
	t.Cleanup(func() { Commit = old })
	Commit = "1a2b3c4d5e6f"
	if got := String(); got != "herdr-dots "+Version+" (source, commit 1a2b3c4d5e6f)" {
		t.Fatalf("String() = %q", got)
	}
	Commit = ""
	if got := CommitID(); got == "" || strings.ContainsAny(got, " \n") {
		t.Fatalf("CommitID() = %q", got)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `GOTOOLCHAIN=local go test ./internal/version/ 2>&1 | tail -5`
Expected: FAIL to build: `undefined: String`, `undefined: Commit`, `undefined: CommitID`.

- [ ] **Step 3: The version source**

Create `internal/version/VERSION`:

```text
1.0.0-rc.1
```

Replace `internal/version/version.go` with:

```go
// Package version holds the herdr-dots version and where this binary came
// from. The version lives in the VERSION file next to this one, so the
// install scripts (scripts/build.sh, build.ps1) read the same value.
package version

import (
	_ "embed"
	"fmt"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var raw string

// Version is the plugin version, for example "1.0.0-rc.1".
var Version = strings.TrimSpace(raw)

// Source and Commit are set with -ldflags -X by the release build
// (Source "release") and by scripts/build.sh (Source "source").
var (
	Source = "source"
	Commit = ""
)

// CommitID is Commit, or the VCS revision Go recorded in the binary, or
// "unknown".
func CommitID() string {
	if Commit != "" {
		return Commit
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			if dirty {
				rev += "+dirty"
			}
			return rev
		}
	}
	return "unknown"
}

// String is the --version line: "herdr-dots 1.0.0-rc.1 (release, commit 1a2b3c4d5e6f)".
func String() string {
	return fmt.Sprintf("herdr-dots %s (%s, commit %s)", Version, Source, CommitID())
}
```

Apply to `herdr-plugin.toml` (the version, the first comment line and the end of `description`; nothing else in the manifest changes):

```diff
--- a/herdr-plugin.toml
+++ b/herdr-plugin.toml
@@ -1,9 +1,9 @@
-# Herdr Dots — always-on specialist agents for Herdr. Still a scaffold (0.x), not done.
+# Herdr Dots — always-on specialist agents for Herdr. Release candidate 1.0.0-rc.1: not released.
 id = "herdr-dots"
 name = "Herdr Dots"
-version = "0.9.0"
+version = "1.0.0-rc.1"
 min_herdr_version = "0.8.0"
-description = "Specialist agents (dots) that join your Claude Code / Codex sessions and review each turn. Scaffold release."
+description = "Specialist agents (dots) that join your Claude Code / Codex sessions and review each turn."
 platforms = ["macos", "linux", "windows"]
 
 # One build step per OS (build steps have no id, so item-level platforms may
```

- [ ] **Step 4: `version` and `--version`**

Apply to `cmd/herdr-dots/main.go` (only the `"version"` entry is replaced and `--version` added; the map keeps every Phase 2–9 command; the aligned spacing is gofmt's):

```diff
--- a/cmd/herdr-dots/main.go
+++ b/cmd/herdr-dots/main.go
@@ -15,10 +15,8 @@ import (
 type handler func(args []string) error
 
 var commands = map[string]handler{
-	"version": func([]string) error {
-		fmt.Println("herdr-dots", version.Version, "(scaffold)")
-		return nil
-	},
+	"version":    printVersion,
+	"--version":  printVersion,
 	"engine":     runEngine,
 	"pane":       runPane,
 	"manage":     runManage,
@@ -33,6 +31,12 @@ var commands = map[string]handler{
 	"mcp":        runMCP,
 }
 
+// printVersion prints "herdr-dots <version> (<release|source>, commit <id>)".
+func printVersion([]string) error {
+	fmt.Println(version.String())
+	return nil
+}
+
 func usage() error {
 	names := make([]string, 0, len(commands))
 	for n := range commands {
```

- [ ] **Step 5: Tests that pin the old version**

At `dffabbf` exactly one test pins the version: Phase 9's `TestVersionIsPhaseNine` in `cmd/herdr-dots/memwire_test.go`. (The `"0.9.0"` literals in `internal/dotpack` and `internal/ui/manage` tests are `herdr_dots_version` values of example packs, not the binary's version: leave them.) Phase 6's `TestManifestDeclaresEveryPlatformAndOneBuildEach` compares the manifest with `version.Version`, so it follows on its own.

Apply to `cmd/herdr-dots/memwire_test.go`:

```diff
--- a/cmd/herdr-dots/memwire_test.go
+++ b/cmd/herdr-dots/memwire_test.go
@@ -57,8 +57,8 @@ func TestBuildEngineWiresMemory(t *testing.T) {
 	}
 }
 
-func TestVersionIsPhaseNine(t *testing.T) {
-	if version.Version != "0.9.0" {
-		t.Fatalf("version %s, want 0.9.0", version.Version)
+func TestVersionIsTheReleaseCandidateInTheBinary(t *testing.T) {
+	if version.Version != "1.0.0-rc.1" {
+		t.Fatalf("version %s, want 1.0.0-rc.1", version.Version)
 	}
 }
```

Run: `grep -rn 'version.Version != "' --include='*_test.go' cmd internal; grep -rn '(scaffold)' --include='*.go' cmd internal`
Expected: only `cmd/herdr-dots/memwire_test.go` (`"1.0.0-rc.1"`) and `internal/version/version_test.go`; no `(scaffold)`. If Phase 9's final fix wave added another pin, change it the same way.

- [ ] **Step 6: Run the tests**

Run: `GOTOOLCHAIN=local go test -race ./internal/version/ ./cmd/herdr-dots/ -v 2>&1 | grep -E '^(--- FAIL|ok|FAIL)|Version|Dispatch'; GOTOOLCHAIN=local go build -o "${TMPDIR:-/tmp}/herdr-dots-v" ./cmd/herdr-dots && "${TMPDIR:-/tmp}/herdr-dots-v" --version && "${TMPDIR:-/tmp}/herdr-dots-v" version; rm -f "${TMPDIR:-/tmp}/herdr-dots-v"`
Expected: PASS for `TestVersionIsTheReleaseCandidate`, `TestManifestCarriesTheSameVersion`, `TestStringNamesVersionSourceAndCommit`, `TestDispatchVersion`, `TestVersionIsTheReleaseCandidateInTheBinary` and `TestManifestDeclaresEveryPlatformAndOneBuildEach`; both commands print `herdr-dots 1.0.0-rc.1 (source, commit <12 hex>)`, with `+dirty` when the tree has uncommitted changes. A binary without Go's VCS stamp (`go run`, or a tree Go can't read as a repository; the reconcile pass's scratch worktree under the temp dir was one) prints `commit unknown`, which is expected.

- [ ] **Step 7: GoReleaser config, release files, local release script**

Create `.goreleaser.yaml`:

```yaml
# GoReleaser config for Herdr Dots. The release workflow
# (.github/workflows/release.yml) runs it; Phase 10 never publishes.
# Archive names must match scripts/build.sh and build.ps1:
#   herdr-dots_<VERSION>_<os>_<arch>.tar.gz (.zip on Windows)
# The inputs come from release/ASSET_ENV, so a rebuild of the same source
# gives byte-identical archives (the publish job checks that).
version: 2
project_name: herdr-dots

builds:
  - id: herdr-dots
    main: ./cmd/herdr-dots
    binary: herdr-dots
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
      - -buildvcs=false
    ldflags:
      - -s -w -X herdr-dots/internal/version.Source=release -X herdr-dots/internal/version.Commit={{ .Env.ASSET_COMMIT }}
    mod_timestamp: "{{ .Env.ASSET_TIME }}"
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]

archives:
  - id: default
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: "{{ .ProjectName }}_{{ .Env.HERDR_DOTS_VERSION }}_{{ .Os }}_{{ .Arch }}"
    files:
      - none* # the binary only
    builds_info:
      mtime: "{{ .Env.ASSET_TIME }}"

checksum:
  name_template: SHA256SUMS
  algorithm: sha256

snapshot:
  version_template: "{{ .Env.HERDR_DOTS_VERSION }}"

changelog:
  disable: true

release:
  disable: true # the workflow drafts the release with gh after signing
```

Create the two empty release files (no release exists yet, so every install builds from source; the release workflow fills `SHA256SUMS`, the developer fills `REPO` if needed):

```bash
mkdir -p release && : > release/SHA256SUMS && : > release/REPO
```

Create `scripts/release-local.sh`:

```sh
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
```

Apply to `.gitignore`:

```diff
--- a/.gitignore
+++ b/.gitignore
@@ -1,4 +1,5 @@
 /bin/
+/dist/
 *.test
 .superpowers/
 .worktrees/
```

- [ ] **Step 8: Workflows**

Replace `.github/workflows/ci.yml` with (Phase 6's three-OS matrix is kept; the Windows build step now runs the new `build.ps1` and `--version`; jobs added: `vulncheck`, `fuzz`, `soak`):

```yaml
# Herdr Dots CI: the same checks on macOS, Linux and Windows, plus fuzzing,
# govulncheck and a short soak. Written in Phases 6 and 10; it runs once the
# developer pushes the repository (nothing is pushed automatically).
name: ci

on:
  push:
  pull_request:
  schedule:
    - cron: "17 3 * * *" # nightly: the long soak

permissions:
  contents: read

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [macos-latest, ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    env:
      GOTOOLCHAIN: local
      CGO_ENABLED: "1" # -race needs cgo; windows-latest ships gcc (MinGW)
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: gofmt
        shell: bash
        run: test -z "$(gofmt -l .)"
      - name: vet
        run: go vet ./...
      - name: test
        run: go test -race ./...
      - name: cross-compile
        if: runner.os == 'Linux'
        run: |
          for t in darwin/amd64 darwin/arm64 linux/arm64 windows/amd64 windows/arm64; do
            GOOS=${t%/*} GOARCH=${t#*/} go build ./...
          done
          GOOS=windows GOARCH=amd64 go vet ./...
      - name: build the plugin (Windows)
        if: runner.os == 'Windows'
        shell: pwsh
        run: |
          powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1
          if (-not (Test-Path bin/herdr-dots.exe)) { throw "bin/herdr-dots.exe missing" }
          ./bin/herdr-dots.exe --version
      - name: build the plugin (Unix)
        if: runner.os != 'Windows'
        run: |
          sh scripts/build.sh
          ./bin/herdr-dots --version

  vulncheck:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: govulncheck
        run: go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

  fuzz:
    runs-on: ubuntu-latest
    env:
      GOTOOLCHAIN: local
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: fuzz every target for 30s
        run: sh scripts/fuzz.sh 30s
      - name: keep crashers
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: fuzz-crashers
          path: "**/testdata/fuzz/**"

  soak:
    runs-on: ubuntu-latest
    timeout-minutes: 350 # GitHub-hosted jobs stop at 6h; the 8h soak runs locally
    env:
      GOTOOLCHAIN: local
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: soak (15m on pushes, 5h nightly)
        shell: bash
        run: |
          if [ "${{ github.event_name }}" = schedule ]; then
            export SOAK_DURATION=5h SOAK_IDLE=10m
          else
            export SOAK_DURATION=15m SOAK_IDLE=5m
          fi
          go test -tags soak -run TestSoak -timeout 340m -v ./internal/engine/
```

Create `.github/workflows/release.yml`:

```yaml
# Herdr Dots release workflow. Written in Phase 10 and never run by it: the
# developer starts it by pushing a tag (a user gate).
#
#   v1.2.3-rc.N  candidate: tests on three OSes, a 10-minute fuzz pass per
#                target, the six archives built with GoReleaser (nothing
#                published), and a pull request that commits
#                release/SHA256SUMS and release/ASSET_ENV for exactly those
#                archives. internal/version/VERSION must already say 1.2.3.
#   v1.2.3       publish, tagged on the merged checksum commit: rebuilds the
#                archives from the same source and settings, refuses to go on
#                unless they match the committed checksums byte for byte,
#                signs them with cosign (keyless, GitHub OIDC) and creates a
#                draft GitHub release for the developer to publish.
#
# Herdr installs the default branch (or --ref), and scripts/build.sh only
# trusts release/SHA256SUMS from that tree, so the checksums a user installs
# always come from the repository they chose.
name: release

on:
  push:
    tags: ["v*"]

permissions:
  contents: read

env:
  GOTOOLCHAIN: local

jobs:
  meta:
    runs-on: ubuntu-latest
    outputs:
      version: ${{ steps.v.outputs.version }}
      candidate: ${{ steps.v.outputs.candidate }}
    steps:
      - uses: actions/checkout@v4
      - id: v
        shell: bash
        run: |
          version=$(tr -d ' \r\n' < internal/version/VERSION)
          tag=${GITHUB_REF_NAME#v}
          case "$tag" in
            "$version"-rc.*) echo "candidate=true" >> "$GITHUB_OUTPUT" ;;
            "$version") echo "candidate=false" >> "$GITHUB_OUTPUT" ;;
            *) echo "tag v$tag does not match VERSION $version"; exit 1 ;;
          esac
          echo "version=$version" >> "$GITHUB_OUTPUT"

  test:
    needs: meta
    if: needs.meta.outputs.candidate == 'true'
    strategy:
      fail-fast: true
      matrix:
        os: [macos-latest, ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    env:
      CGO_ENABLED: "1"
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test -race ./...

  fuzz:
    needs: meta
    if: needs.meta.outputs.candidate == 'true'
    runs-on: ubuntu-latest
    timeout-minutes: 300
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: sh scripts/fuzz.sh 10m

  candidate:
    needs: [meta, test, fuzz]
    if: needs.meta.outputs.candidate == 'true'
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: pin the build inputs
        shell: bash
        run: |
          {
            echo "HERDR_DOTS_VERSION=${{ needs.meta.outputs.version }}"
            echo "ASSET_COMMIT=$(git rev-parse --short=12 HEAD)"
            echo "ASSET_TIME=$(git log -1 --format=%ct HEAD)"
          } > release/ASSET_ENV
          cat release/ASSET_ENV >> "$GITHUB_ENV"
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean --skip=publish,sign,validate
      - name: commit the checksums on a branch and open a pull request
        shell: bash
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          cp dist/SHA256SUMS release/SHA256SUMS
          branch="release/v${{ needs.meta.outputs.version }}-checksums"
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git switch -c "$branch"
          git add release/SHA256SUMS release/ASSET_ENV
          git commit -m "release: checksums for v${{ needs.meta.outputs.version }} (from $GITHUB_REF_NAME)"
          git push origin "$branch"
          gh pr create --title "Release checksums for v${{ needs.meta.outputs.version }}" \
            --body "Archives built by $GITHUB_REF_NAME. Merge, then tag v${{ needs.meta.outputs.version }} on the merge commit."
      - uses: actions/upload-artifact@v4
        with:
          name: candidate-dist
          path: dist/

  publish:
    needs: meta
    if: needs.meta.outputs.candidate == 'false'
    runs-on: ubuntu-latest
    permissions:
      contents: write
      id-token: write # cosign keyless signing
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: load the pinned build inputs
        shell: bash
        run: |
          test -s release/SHA256SUMS || { echo "release/SHA256SUMS is empty: merge the candidate's checksum PR first"; exit 1; }
          grep -q "^HERDR_DOTS_VERSION=${{ needs.meta.outputs.version }}$" release/ASSET_ENV
          cat release/ASSET_ENV >> "$GITHUB_ENV"
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean --skip=publish,sign,validate
      - name: the rebuilt archives must match the committed checksums
        run: diff -u release/SHA256SUMS dist/SHA256SUMS
      - uses: sigstore/cosign-installer@v3
      - name: sign every archive
        shell: bash
        run: |
          for f in dist/*.tar.gz dist/*.zip; do
            cosign sign-blob --yes --bundle "$f.sigstore.json" "$f"
          done
      - name: draft the GitHub release
        shell: bash
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          gh release create "$GITHUB_REF_NAME" --draft --verify-tag --title "$GITHUB_REF_NAME" \
            --notes "See CHANGELOG.md. Verify with: herdr-dots verify-release" \
            dist/*.tar.gz dist/*.zip dist/*.sigstore.json dist/SHA256SUMS
```

- [ ] **Step 9: Makefile targets**

Apply to `Makefile` (Phase 6's form; if a line moved, add the four targets and the `.PHONY` names by hand):

```diff
--- a/Makefile
+++ b/Makefile
@@ -1,4 +1,4 @@
-.PHONY: build test vet cross link
+.PHONY: build test vet cross link fuzz soak snapshot vulncheck
 build:
 	sh scripts/build.sh
 test:
@@ -15,3 +15,21 @@
 	GOOS=windows GOARCH=amd64 go test -c -o "$${TMPDIR:-/tmp}/herdr-dots-wintest/" ./...
 link: build
 	herdr --session dots-test plugin link .
+# fuzz runs every fuzz target for FUZZTIME each (default 30s).
+fuzz:
+	sh scripts/fuzz.sh $(or $(FUZZTIME),30s)
+# soak runs the soak test for SOAK_DURATION (default 15m); 8h for a release.
+soak:
+	SOAK_DURATION=$(or $(SOAK_DURATION),15m) go test -tags soak -run TestSoak -timeout 10h -v ./internal/engine/
+# snapshot builds the six release archives and dist/SHA256SUMS locally. It
+# never publishes: GoReleaser in snapshot mode when installed, else plain go build.
+snapshot:
+	@if command -v goreleaser >/dev/null 2>&1; then \
+		HERDR_DOTS_VERSION=$$(tr -d ' \r\n' < internal/version/VERSION) \
+		ASSET_COMMIT=$$(git rev-parse --short=12 HEAD) ASSET_TIME=$$(git log -1 --format=%ct HEAD) \
+		goreleaser release --snapshot --clean --skip=publish,sign; \
+	else \
+		sh scripts/release-local.sh; \
+	fi
+vulncheck:
+	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
```

- [ ] **Step 10: Lint the workflows, the YAML and the script**

Run: `actionlint .github/workflows/ci.yml .github/workflows/release.yml && echo actionlint-ok; ruby -ryaml -e 'YAML.load_file(".goreleaser.yaml"); puts "goreleaser yaml parses"'; shellcheck -s sh scripts/release-local.sh && echo shellcheck-ok; if command -v goreleaser >/dev/null 2>&1; then goreleaser check; else echo 'goreleaser not installed: config checked by Task 6 TestGoReleaserConfigMatchesTheInstallScripts'; fi`
Expected: `actionlint-ok`, `goreleaser yaml parses`, `shellcheck-ok`, and either `goreleaser check` success or the not-installed line (both workflows passed actionlint while planning).

- [ ] **Step 11: Build the archives locally (never published)**

Run: `GOTOOLCHAIN=local make snapshot && ls dist/ && cat dist/SHA256SUMS`
Expected: six archives (`herdr-dots_1.0.0-rc.1_{darwin,linux}_{amd64,arm64}.tar.gz`, `herdr-dots_1.0.0-rc.1_windows_{amd64,arm64}.zip`) and six checksum lines. If `zip` is missing on this machine, the Windows archives fail: install nothing, rerun with `HERDR_DOTS_TARGETS="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64" GOTOOLCHAIN=local sh scripts/release-local.sh` and note it in the commit message. `dist/` is ignored by git.

- [ ] **Step 12: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 13: Commit (never push, never tag)**

```bash
git add internal/version cmd/herdr-dots/main.go herdr-plugin.toml .goreleaser.yaml .github/workflows/ci.yml .github/workflows/release.yml Makefile .gitignore release/SHA256SUMS release/REPO scripts/release-local.sh
git add -u   # the version pins Step 5 changed
git commit -m "build: version 1.0.0-rc.1 with build source and commit, GoReleaser config, release and CI workflows (Phase 10)"
```

---

### Task 6: Install scripts (verify, fall back) and `verify-release`

**Depends on:** Task 5 (`internal/version/VERSION`, `version.Source` ldflags name, archive names, `release/REPO`, `release/SHA256SUMS`, `scripts/release-local.sh`, `.goreleaser.yaml`). Wave 2.

**Files:**
- Create: `scripts/build_test.go`, `scripts/build_ps1_test.go`, `scripts/release_test.go`, `cmd/herdr-dots/verifyrelease.go`, `cmd/herdr-dots/verifyrelease_test.go`
- Modify: `scripts/build.sh` (replaced whole), `scripts/build.ps1` (replaced whole), `cmd/herdr-dots/platform_manifest_test.go` (Phase 6's `TestWindowsBuildScriptBuildsTheExe`)

**Interfaces:**
- Consumes: Task 5's files and names; `commands` map in `cmd/herdr-dots/main.go` (registered from `init()`, so `main.go` is not edited); `version.String()`.
- Produces: `scripts/build.sh` / `build.ps1` behavior: env `HERDR_PLUGIN_BUILD_OFFLINE=1` skips the download; `HERDR_DOTS_PLATFORM=<os>_<arch>` and `HERDR_DOTS_RELEASE_BASE=<url>` override detection and the GitHub base URL (tests); on success they write `bin/herdr-dots` (`.exe`), `bin/INSTALL_SOURCE` (`source=release|source`, `version=`, and for releases `repo=`, `asset=`, `sha256=`), and keep the verified archive at `bin/release/<asset>` plus `<asset>.sigstore.json` when the release has one. Command `herdr-dots verify-release [--dir <plugin root>]` (`runVerifyRelease`, testable `verifyRelease(verifyIO) error`, `type verifyIO struct{ root string; out io.Writer; lookPath func(string) (string, error); run func(name string, args ...string) (string, error) }`).

- [ ] **Step 1: Write the failing script tests**

Create `scripts/build_test.go`:

```go
// Package scripts tests the install scripts Herdr runs as [[build]] steps.
// The tests copy a script into a temp plugin root, serve a fake GitHub
// release from httptest and run the script with a PATH that has exactly
// the tools the case needs.
package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testVersion = "1.0.0-rc.1"
	testRepo    = "acme/herdr-dots"
	testPlat    = "darwin_arm64"
	testAsset   = "herdr-dots_" + testVersion + "_" + testPlat + ".tar.gz"
	fakeBinary  = "#!/bin/sh\necho fake-release-binary\n"
)

// unixTools are what build.sh needs besides go.
var unixTools = []string{"sh", "uname", "tr", "awk", "sed", "grep", "cut", "mkdir", "mktemp", "mv", "rm", "chmod",
	"cat", "dirname", "tar", "gzip", "curl", "shasum", "sha256sum", "head"}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("build.sh is the macOS and Linux build step; build.ps1 is tested on Windows")
	}
}

// toolDir links the named tools (those that exist here) into a new dir, so
// PATH can include or leave out go.
func toolDir(t *testing.T, withGo bool) string {
	t.Helper()
	dir := t.TempDir()
	names := unixTools
	if withGo {
		names = append(append([]string{}, names...), "go")
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			if err := os.Symlink(p, filepath.Join(dir, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func tarGz(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// pluginRoot is a temp plugin checkout: the script, VERSION, release/ files
// and a tiny module whose binary prints its version.Source.
func pluginRoot(t *testing.T, sums string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"internal/version/VERSION":    testVersion + "\n",
		"release/REPO":                testRepo + "\n",
		"release/SHA256SUMS":          sums,
		"go.mod":                      "module herdr-dots\n\ngo 1.25\n",
		"internal/version/version.go": "package version\n\nvar Source = \"unset\"\n",
		"cmd/herdr-dots/main.go": "package main\n\nimport (\n\t\"fmt\"\n\n\t\"herdr-dots/internal/version\"\n)\n\n" +
			"func main() { fmt.Println(\"built-from-\" + version.Source) }\n",
	}
	script, err := os.ReadFile("build.sh")
	if err != nil {
		t.Fatal(err)
	}
	files["scripts/build.sh"] = string(script)
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// release serves one asset (and counts requests) the way GitHub does.
func release(t *testing.T, asset []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/"+testRepo+"/releases/download/v"+testVersion+"/"+testAsset {
			_, _ = w.Write(asset)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func goEnv(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// runBuild runs scripts/build.sh in root and returns its stderr.
func runBuild(t *testing.T, root, base string, withGo bool, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "scripts/build.sh")
	cmd.Dir = root
	cmd.Env = append([]string{
		"PATH=" + toolDir(t, withGo),
		"HOME=" + t.TempDir(),
		"HERDR_DOTS_PLATFORM=" + testPlat,
		"HERDR_DOTS_RELEASE_BASE=" + base,
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=mod",
		"GOCACHE=" + goEnv(t, "GOCACHE"),
		"GOPATH=" + goEnv(t, "GOPATH"),
	}, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func runInstalled(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command(filepath.Join(root, "bin", "herdr-dots")).Output()
	if err != nil {
		t.Fatalf("run bin/herdr-dots: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func readInstallSource(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "bin", "INSTALL_SOURCE"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// binLeftovers lists what is in bin/ besides the expected entries.
func binLeftovers(t *testing.T, root string, want ...string) []string {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Join(root, "bin"))
	var extra []string
	for _, e := range ents {
		ok := false
		for _, w := range want {
			ok = ok || e.Name() == w
		}
		if !ok {
			extra = append(extra, e.Name())
		}
	}
	return extra
}

func TestVerifiedReleaseIsInstalled(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, hits := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n%s  herdr-dots_%s_linux_amd64.tar.gz\n", sum(asset), testAsset, strings.Repeat("0", 64), testVersion))
	stderr, err := runBuild(t, root, srv.URL, false)
	if err != nil {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if got := runInstalled(t, root); got != "fake-release-binary" {
		t.Fatalf("installed binary says %q", got)
	}
	src := readInstallSource(t, root)
	for _, want := range []string{"source=release\n", "repo=" + testRepo + "\n", "asset=" + testAsset + "\n", "sha256=" + sum(asset) + "\n"} {
		if !strings.Contains(src, want) {
			t.Errorf("INSTALL_SOURCE lacks %q:\n%s", want, src)
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "bin", "release", testAsset)); err != nil || sum(b) != sum(asset) {
		t.Fatalf("the verified archive is kept for verify-release: %v", err)
	}
	if extra := binLeftovers(t, root, "herdr-dots", "INSTALL_SOURCE", "release"); len(extra) != 0 {
		t.Fatalf("leftovers in bin/: %v", extra)
	}
	if hits.Load() < 1 {
		t.Fatal("no download happened")
	}
}

func TestChecksumMismatchFallsBackToASourceBuild(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, _ := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", strings.Repeat("ab", 32), testAsset))
	stderr, err := runBuild(t, root, srv.URL, true)
	if err != nil {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "checksum mismatch") {
		t.Fatalf("stderr does not explain the fallback:\n%s", stderr)
	}
	if got := runInstalled(t, root); got != "built-from-source" {
		t.Fatalf("installed binary says %q, want the source build", got)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "release", testAsset)); !os.IsNotExist(err) {
		t.Fatal("the unverified archive must not be kept")
	}
	if !strings.HasPrefix(readInstallSource(t, root), "source=source\n") {
		t.Fatal("INSTALL_SOURCE must say source")
	}
}

func TestNoChecksumMeansNoDownload(t *testing.T) {
	skipOnWindows(t)
	srv, hits := release(t, tarGz(t, "herdr-dots", fakeBinary))
	root := pluginRoot(t, "# filled in by the release workflow\n")
	stderr, err := runBuild(t, root, srv.URL, true)
	if err != nil {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests without a checksum to check them against", hits.Load())
	}
	if got := runInstalled(t, root); got != "built-from-source" {
		t.Fatalf("installed binary says %q", got)
	}
}

func TestArchiveWithoutTheBinaryIsRejected(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "something-else", fakeBinary)
	srv, _ := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), testAsset))
	stderr, err := runBuild(t, root, srv.URL, true)
	if err != nil || !strings.Contains(stderr, "has no herdr-dots binary") {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if got := runInstalled(t, root); got != "built-from-source" {
		t.Fatalf("installed binary says %q", got)
	}
}

func TestOfflineWithoutGoFailsWithInstructionsAndAnEmptyBin(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, hits := release(t, asset)
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), testAsset))
	stderr, err := runBuild(t, root, srv.URL, false, "HERDR_PLUGIN_BUILD_OFFLINE=1")
	if err == nil {
		t.Fatal("an offline install without Go must fail")
	}
	for _, want := range []string{"HERDR_PLUGIN_BUILD_OFFLINE=1", "Install Go 1.25 or newer", "release/SHA256SUMS"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("offline mode downloaded")
	}
	if extra := binLeftovers(t, root); len(extra) != 0 {
		t.Fatalf("bin/ must stay empty: %v", extra)
	}
}

func TestUnreachableServerWithoutGoFailsCleanly(t *testing.T) {
	skipOnWindows(t)
	asset := tarGz(t, "herdr-dots", fakeBinary)
	srv, _ := release(t, asset)
	srv.Close() // nothing listens any more
	root := pluginRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), testAsset))
	stderr, err := runBuild(t, root, srv.URL, false)
	if err == nil || !strings.Contains(stderr, "download failed") || !strings.Contains(stderr, "Install Go 1.25 or newer") {
		t.Fatalf("build.sh: %v\n%s", err, stderr)
	}
	if extra := binLeftovers(t, root); len(extra) != 0 {
		t.Fatalf("bin/ must stay empty: %v", extra)
	}
}

func TestScriptsPassShellcheck(t *testing.T) {
	skipOnWindows(t)
	sc, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("shellcheck not installed")
	}
	scripts, _ := filepath.Glob("*.sh")
	out, err := exec.Command(sc, append([]string{"-s", "sh"}, scripts...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("shellcheck: %v\n%s", err, out)
	}
}
```

Create `scripts/build_ps1_test.go`:

```go
package scripts

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const winAsset = "herdr-dots_" + testVersion + "_windows_amd64.zip"

// powershell finds pwsh (macOS, Linux, Windows) or Windows PowerShell; the
// tests skip without one (CI's ubuntu and windows runners have pwsh).
func powershell(t *testing.T) string {
	t.Helper()
	for _, n := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	t.Skip("PowerShell is not installed")
	return ""
}

func zipBytes(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func psRoot(t *testing.T, sums string) string {
	t.Helper()
	root := pluginRoot(t, sums)
	script, err := os.ReadFile("build.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.ps1"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runPS(t *testing.T, ps, root, base string, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join("scripts", "build.ps1"))
	cmd.Dir = root
	cmd.Env = append([]string{"PATH=" + filepath.Dir(ps), "HOME=" + t.TempDir(), "USERPROFILE=" + t.TempDir(),
		"SystemRoot=" + os.Getenv("SystemRoot"), "HERDR_DOTS_PLATFORM=windows_amd64", "HERDR_DOTS_RELEASE_BASE=" + base}, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func TestPowerShellInstallsAVerifiedRelease(t *testing.T) {
	ps := powershell(t)
	asset := zipBytes(t, "herdr-dots.exe", "fake windows binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+testRepo+"/releases/download/v"+testVersion+"/"+winAsset {
			_, _ = w.Write(asset)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	root := psRoot(t, fmt.Sprintf("%s  %s\n", sum(asset), winAsset))
	stderr, err := runPS(t, ps, root, srv.URL)
	if err != nil {
		t.Fatalf("build.ps1: %v\n%s", err, stderr)
	}
	if b, err := os.ReadFile(filepath.Join(root, "bin", "herdr-dots.exe")); err != nil || string(b) != "fake windows binary" {
		t.Fatalf("installed binary: %q %v", b, err)
	}
	if src := readInstallSource(t, root); !strings.Contains(src, "source=release\n") || !strings.Contains(src, "asset="+winAsset+"\n") {
		t.Fatalf("INSTALL_SOURCE:\n%s", src)
	}
}

func TestPowerShellRefusesAMismatchAndFailsWithoutGo(t *testing.T) {
	ps := powershell(t)
	asset := zipBytes(t, "herdr-dots.exe", "fake windows binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(asset) }))
	t.Cleanup(srv.Close)
	root := psRoot(t, fmt.Sprintf("%s  %s\n", strings.Repeat("ab", 32), winAsset))
	stderr, err := runPS(t, ps, root, srv.URL)
	if err == nil {
		t.Fatal("a checksum mismatch without Go must fail")
	}
	for _, want := range []string{"checksum mismatch", "Install Go 1.25 or newer"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if extra := binLeftovers(t, root); len(extra) != 0 {
		t.Fatalf("bin/ must stay empty: %v", extra)
	}
}
```

Create `scripts/release_test.go`:

```go
package scripts

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLocalReleaseInstallsEndToEnd builds the real release archives with
// release-local.sh, serves dist/ like a GitHub release and installs the
// host's binary with build.sh. It takes about a minute, so it runs only with
// HERDR_DOTS_E2E_RELEASE=1 (Task 13 runs it; CI's release job runs the
// GoReleaser equivalent).
func TestLocalReleaseInstallsEndToEnd(t *testing.T) {
	skipOnWindows(t)
	if os.Getenv("HERDR_DOTS_E2E_RELEASE") != "1" {
		t.Skip("set HERDR_DOTS_E2E_RELEASE=1 to build and install the real release archives")
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("sh", "scripts/release-local.sh")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("release-local.sh: %v\n%s", err, out)
	}
	dist := filepath.Join(repoRoot, "dist")
	sums, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	want := 6
	if tg := os.Getenv("HERDR_DOTS_TARGETS"); tg != "" {
		want = len(strings.Fields(tg))
	}
	if n := strings.Count(string(sums), "\n"); n != want {
		t.Fatalf("SHA256SUMS lists %d archives, want %d:\n%s", n, want, sums)
	}
	version, err := os.ReadFile(filepath.Join(repoRoot, "internal", "version", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	ver := strings.TrimSpace(string(version))
	prefix := "/" + testRepo + "/releases/download/v" + ver + "/"
	srv := httptest.NewServer(http.StripPrefix(prefix, http.FileServer(http.Dir(dist))))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	for rel, body := range map[string]string{
		"internal/version/VERSION": ver + "\n",
		"release/REPO":             testRepo + "\n",
		"release/SHA256SUMS":       string(sums),
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script, _ := os.ReadFile("build.sh")
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.sh"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "scripts/build.sh")
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + toolDir(t, false), "HOME=" + t.TempDir(), "HERDR_DOTS_RELEASE_BASE=" + srv.URL,
		"HERDR_DOTS_PLATFORM=" + runtime.GOOS + "_" + runtime.GOARCH}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build.sh: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(root, "bin", "herdr-dots"), "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); !strings.HasPrefix(got, "herdr-dots "+ver+" (release, commit ") {
		t.Fatalf("--version = %q", got)
	}
}

// The archive names GoReleaser makes are the names the install scripts and
// release-local.sh look for.
func TestGoReleaserConfigMatchesTheInstallScripts(t *testing.T) {
	b, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	for _, want := range []string{
		"project_name: herdr-dots",
		`name_template: "{{ .ProjectName }}_{{ .Env.HERDR_DOTS_VERSION }}_{{ .Os }}_{{ .Arch }}"`,
		"goos: [darwin, linux, windows]",
		"goarch: [amd64, arm64]",
		"formats: [zip]",
		"name_template: SHA256SUMS",
		"-X herdr-dots/internal/version.Source=release",
		"-buildvcs=false",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf(".goreleaser.yaml lacks %q", want)
		}
	}
	for _, f := range []string{"build.sh", "release-local.sh"} {
		s, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(s), "${NAME}_${VERSION}_") {
			t.Errorf("%s does not name archives <name>_<version>_<os>_<arch>", f)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTOOLCHAIN=local go test ./scripts/ 2>&1 | tail -15`
Expected: FAIL: `TestVerifiedReleaseIsInstalled` (the old script only builds from source: `installed binary says "built-from-source"`), `TestOfflineWithoutGoFailsWithInstructionsAndAnEmptyBin` (stderr lacks `Install Go 1.25 or newer`), and others; `TestGoReleaserConfigMatchesTheInstallScripts` fails on `build.sh does not name archives`. The PowerShell tests skip here unless `pwsh` is installed.

- [ ] **Step 3: `scripts/build.sh`**

Replace `scripts/build.sh` with:

```sh
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
```

- [ ] **Step 4: `scripts/build.ps1`**

Replace `scripts/build.ps1` with:

```powershell
# Herdr runs this as the manifest's Windows [[build]] step, during
# `herdr plugin install`. It installs bin\herdr-dots.exe, like build.sh:
#   1. the prebuilt release binary for this CPU, downloaded from the GitHub
#      release and checked against release/SHA256SUMS from the cloned tree;
#   2. otherwise a source build with Go 1.25 or newer;
#   3. otherwise it fails with the reason, leaving nothing half-installed.
# HERDR_PLUGIN_BUILD_OFFLINE=1 skips the download.
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Join-Path $PSScriptRoot '..')
$Name = 'herdr-dots'
$Version = (Get-Content -Raw -LiteralPath 'internal/version/VERSION').Trim()

function Say([string]$Message) { [Console]::Error.WriteLine("${Name}: $Message") }

function Get-Platform {
    if ($env:HERDR_DOTS_PLATFORM) { return $env:HERDR_DOTS_PLATFORM }
    switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { return 'windows_amd64' }
        'ARM64' { return 'windows_arm64' }
    }
    return $null
}

function Get-Repo {
    $r = ''
    if (Test-Path -LiteralPath 'release/REPO') { $r = (Get-Content -Raw -LiteralPath 'release/REPO').Trim() }
    if (-not $r -and (Get-Command git -ErrorAction SilentlyContinue)) {
        $url = (& git config --get remote.origin.url 2>$null)
        if ($url -match '^(?:https://github\.com/|git@github\.com:)(.+?)(?:\.git)?$') { $r = $Matches[1] }
    }
    if ($r -match '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$') { return $r }
    return $null
}

function Get-Checksum([string]$Asset) {
    if (-not (Test-Path -LiteralPath 'release/SHA256SUMS')) { return $null }
    foreach ($line in Get-Content -LiteralPath 'release/SHA256SUMS') {
        $f = $line.Trim() -split '\s+', 2
        if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq $Asset) { return $f[0].ToLower() }
    }
    return $null
}

function Install-Release([string]$Tmp) {
    if ($env:HERDR_PLUGIN_BUILD_OFFLINE -eq '1') { Say 'HERDR_PLUGIN_BUILD_OFFLINE=1: skipping the download'; return $false }
    $plat = Get-Platform
    if (-not $plat) { Say "no prebuilt binary for $env:PROCESSOR_ARCHITECTURE"; return $false }
    $asset = "${Name}_${Version}_${plat}.zip"
    $want = Get-Checksum $asset
    if (-not $want) { Say "release/SHA256SUMS has no checksum for $asset"; return $false }
    $repo = Get-Repo
    if (-not $repo) { Say 'unknown GitHub repository: put owner/repo in release/REPO'; return $false }
    $base = 'https://github.com'
    if ($env:HERDR_DOTS_RELEASE_BASE) { $base = $env:HERDR_DOTS_RELEASE_BASE }
    $url = "$base/$repo/releases/download/v$Version/$asset"
    $zip = Join-Path $Tmp $asset
    Say "downloading $url"
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $zip -TimeoutSec 300
    } catch {
        Say "download failed: $($_.Exception.Message)"
        return $false
    }
    $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash.ToLower()
    if ($got -ne $want) { Say "checksum mismatch for $asset (got $got, want $want): not installing it"; return $false }
    $x = Join-Path $Tmp 'x'
    Expand-Archive -LiteralPath $zip -DestinationPath $x
    $exe = Join-Path $x "$Name.exe"
    if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) { Say "$asset has no $Name.exe"; return $false }
    New-Item -ItemType Directory -Force -Path 'bin/release' | Out-Null
    $bundle = "bin/release/$asset.sigstore.json"
    Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $bundle
    try { Invoke-WebRequest -UseBasicParsing -Uri "$url.sigstore.json" -OutFile (Join-Path $Tmp 'bundle') -TimeoutSec 60; Move-Item -Force -LiteralPath (Join-Path $Tmp 'bundle') -Destination $bundle } catch { }
    Move-Item -Force -LiteralPath $zip -Destination "bin/release/$asset"
    Move-Item -Force -LiteralPath $exe -Destination "bin/$Name.exe"
    Set-Content -NoNewline -LiteralPath 'bin/INSTALL_SOURCE' -Value "source=release`nversion=$Version`nrepo=$repo`nasset=$asset`nsha256=$got`n"
    return $true
}

function Build-Source([string]$Tmp) {
    Say "building $Version from source with $(& go version)"
    $out = Join-Path $Tmp "$Name.exe"
    & go build -trimpath -ldflags '-X herdr-dots/internal/version.Source=source' -o $out '.\cmd\herdr-dots'
    if ($LASTEXITCODE -ne 0) { return $false }
    Move-Item -Force -LiteralPath $out -Destination "bin/$Name.exe"
    Set-Content -NoNewline -LiteralPath 'bin/INSTALL_SOURCE' -Value "source=source`nversion=$Version`n"
    return $true
}

New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
$tmp = Join-Path 'bin' ('.install.' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$code = 1
try {
    if (Install-Release $tmp) {
        Say "installed the verified $Version release binary"
        $code = 0
    } elseif (Get-Command go -ErrorAction SilentlyContinue) {
        if (Build-Source $tmp) { Say "built $Version from source"; $code = 0 }
        else { Say 'the source build failed (see above); bin/ is unchanged' }
    } else {
        Say 'could not install: no verified prebuilt binary (see above) and no Go toolchain.'
        Say 'Install Go 1.25 or newer (https://go.dev/dl/) and reinstall the plugin, or download'
        Say "${Name}_${Version}_windows_<arch>.zip from the GitHub release, check it against"
        Say "release/SHA256SUMS and put $Name.exe in bin\."
    }
} finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -LiteralPath $tmp
}
exit $code
```

Phase 6's `TestWindowsBuildScriptBuildsTheExe` pins strings of the old script.

Apply to `cmd/herdr-dots/platform_manifest_test.go`:

```diff
--- a/cmd/herdr-dots/platform_manifest_test.go
+++ b/cmd/herdr-dots/platform_manifest_test.go
@@ -112,7 +112,13 @@ func TestWindowsBuildScriptBuildsTheExe(t *testing.T) {
 		t.Fatal(err)
 	}
 	s := string(b)
-	for _, want := range []string{`go build -o 'bin\herdr-dots.exe' '.\cmd\herdr-dots'`, "Go toolchain (>= 1.25) is required", "exit $LASTEXITCODE"} {
+	for _, want := range []string{
+		"Get-FileHash -Algorithm SHA256", // verifies the download against release/SHA256SUMS
+		"HERDR_PLUGIN_BUILD_OFFLINE",
+		`'.\cmd\herdr-dots'`, // the source-build fallback
+		"Install Go 1.25 or newer",
+		"exit $code",
+	} {
 		if !strings.Contains(s, want) {
 			t.Errorf("build.ps1 lacks %q", want)
 		}
```

- [ ] **Step 5: Run the script tests**

Run: `GOTOOLCHAIN=local go test -v ./scripts/ ./cmd/herdr-dots/ -run 'Release|Checksum|Offline|Unreachable|Archive|NoChecksum|Shellcheck|PowerShell|GoReleaser|WindowsBuildScript' 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS for `TestVerifiedReleaseIsInstalled`, `TestChecksumMismatchFallsBackToASourceBuild`, `TestNoChecksumMeansNoDownload`, `TestArchiveWithoutTheBinaryIsRejected`, `TestOfflineWithoutGoFailsWithInstructionsAndAnEmptyBin`, `TestUnreachableServerWithoutGoFailsCleanly`, `TestScriptsPassShellcheck`, `TestGoReleaserConfigMatchesTheInstallScripts`, `TestWindowsBuildScriptBuildsTheExe`; SKIP for the two PowerShell tests (no `pwsh` here) and for `TestLocalReleaseInstallsEndToEnd` (opt-in).

- [ ] **Step 6: The end-to-end install with real archives**

Run: `HERDR_DOTS_E2E_RELEASE=1 GOTOOLCHAIN=local go test -run TestLocalReleaseInstallsEndToEnd -v -timeout 10m ./scripts/ 2>&1 | tail -4`
Expected: PASS (it builds the six archives with `release-local.sh`, serves `dist/` like a GitHub release, installs the darwin binary with `build.sh` and checks `--version` says `(release, commit ...)`; verified while planning). If `zip` is missing, run it with `HERDR_DOTS_TARGETS="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64"` and note it in the commit message.

- [ ] **Step 7: Write the failing `verify-release` tests**

Create `cmd/herdr-dots/verifyrelease_test.go`:

```go
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vrAsset = "herdr-dots_1.0.0-rc.1_darwin_arm64.tar.gz"

func vrTarGz(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "herdr-dots", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// vrRoot is an installed release: archive kept in bin/release, binary in bin/.
func vrRoot(t *testing.T, binary string, bundle bool) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	archive := vrTarGz(t, "the release binary")
	s := sha256.Sum256(archive)
	files := map[string][]byte{
		"bin/release/" + vrAsset: archive,
		"bin/herdr-dots":         []byte(binary),
		"bin/INSTALL_SOURCE":     []byte("source=release\nversion=1.0.0-rc.1\nrepo=acme/herdr-dots\nasset=" + vrAsset + "\nsha256=" + hex.EncodeToString(s[:]) + "\n"),
		"release/SHA256SUMS":     []byte(hex.EncodeToString(s[:]) + "  " + vrAsset + "\n"),
	}
	if bundle {
		files["bin/release/"+vrAsset+".sigstore.json"] = []byte(`{}`)
	}
	for rel, b := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, archive
}

type vrRun struct {
	calls [][]string
	err   error
}

func (r *vrRun) io(root string, out *bytes.Buffer, cosign bool) verifyIO {
	return verifyIO{root: root, out: out,
		lookPath: func(n string) (string, error) {
			if cosign && n == "cosign" {
				return "/usr/local/bin/cosign", nil
			}
			return "", errors.New("not found")
		},
		run: func(name string, a ...string) (string, error) {
			r.calls = append(r.calls, append([]string{name}, a...))
			return "Verified OK", r.err
		}}
}

func TestVerifyReleaseChecksArchiveAndBinary(t *testing.T) {
	root, _ := vrRoot(t, "the release binary", false)
	var out bytes.Buffer
	if err := verifyRelease((&vrRun{}).io(root, &out, false)); err != nil {
		t.Fatalf("verify: %v\n%s", err, out.String())
	}
	for _, want := range []string{"checksum ok", "binary ok", "signature: not checked (install cosign"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestVerifyReleaseRefusesATamperedArchive(t *testing.T) {
	root, archive := vrRoot(t, "the release binary", false)
	archive[len(archive)-1] ^= 0xff
	if err := os.WriteFile(filepath.Join(root, "bin", "release", vrAsset), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := verifyRelease((&vrRun{}).io(root, &out, true))
	if err == nil || !strings.Contains(err.Error(), "does not match release/SHA256SUMS") {
		t.Fatalf("tampered archive: %v", err)
	}
}

func TestVerifyReleaseRefusesAReplacedBinary(t *testing.T) {
	root, _ := vrRoot(t, "something else", false)
	var out bytes.Buffer
	err := verifyRelease((&vrRun{}).io(root, &out, false))
	if err == nil || !strings.Contains(err.Error(), "replaced after the install") {
		t.Fatalf("replaced binary: %v", err)
	}
}

func TestVerifyReleaseRunsCosignWithTheWorkflowIdentity(t *testing.T) {
	root, _ := vrRoot(t, "the release binary", true)
	r := &vrRun{}
	var out bytes.Buffer
	if err := verifyRelease(r.io(root, &out, true)); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("cosign calls %v", r.calls)
	}
	got := strings.Join(r.calls[0], " ")
	for _, want := range []string{"/usr/local/bin/cosign verify-blob --bundle", vrAsset + ".sigstore.json",
		`--certificate-identity-regexp ^https://github\.com/acme/herdr-dots/\.github/workflows/release\.yml@refs/tags/v`,
		"--certificate-oidc-issuer https://token.actions.githubusercontent.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("cosign args lack %q:\n%s", want, got)
		}
	}
	r.err = fmt.Errorf("exit status 1")
	if err := verifyRelease(r.io(root, &out, true)); err == nil || !strings.Contains(err.Error(), "signature check failed") {
		t.Fatalf("a failed signature must fail: %v", err)
	}
}

func TestVerifyReleaseOnASourceBuild(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "INSTALL_SOURCE"), []byte("source=source\nversion=1.0.0-rc.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := verifyRelease((&vrRun{}).io(root, &out, true)); err != nil || !strings.Contains(out.String(), "Built from source") {
		t.Fatalf("source build: %v\n%s", err, out.String())
	}
	if err := verifyRelease((&vrRun{}).io(t.TempDir(), &out, true)); err == nil || !strings.Contains(err.Error(), "reinstall") {
		t.Fatalf("no record: %v", err)
	}
}
```

Run: `GOTOOLCHAIN=local go test ./cmd/herdr-dots/ -run VerifyRelease 2>&1 | tail -3`
Expected: FAIL to build: `undefined: verifyRelease`, `undefined: verifyIO`.

- [ ] **Step 8: `verify-release`**

Create `cmd/herdr-dots/verifyrelease.go`:

```go
package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"herdr-dots/internal/platform"
	"herdr-dots/internal/version"
)

func init() { commands["verify-release"] = runVerifyRelease }

// verifyIO is what verify-release touches, so tests can fake it.
type verifyIO struct {
	root     string // the plugin root (bin/, release/)
	out      io.Writer
	lookPath func(string) (string, error)
	run      func(name string, args ...string) (string, error) // combined output
}

var (
	vrAssetRe = regexp.MustCompile(`^herdr-dots_[0-9A-Za-z.+-]+_(darwin|linux|windows)_(amd64|arm64)\.(tar\.gz|zip)$`)
	vrRepoRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
)

// vrSigIssuer is the OIDC issuer of GitHub Actions' keyless signing certificates.
const vrSigIssuer = "https://token.actions.githubusercontent.com"

func runVerifyRelease(args []string) error {
	fs := flag.NewFlagSet("verify-release", flag.ContinueOnError)
	dir := fs.String("dir", "", "plugin root (default: the directory above this binary's bin/)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := *dir
	if root == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		root = filepath.Dir(filepath.Dir(exe))
	}
	return verifyRelease(verifyIO{root: root, out: os.Stdout, lookPath: exec.LookPath,
		run: func(name string, a ...string) (string, error) {
			// Through internal/platform, like every child process (Phase 6's
			// portability guard): Windows .cmd shims, no console window.
			cmd, err := platform.Command(name, a)
			if err != nil {
				return "", err
			}
			cmd.Env = platform.ChildEnv(os.Environ())
			platform.HideWindow(cmd)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}})
}

// verifyRelease re-checks an installed release: the kept archive against
// release/SHA256SUMS, the installed binary against the one in the archive,
// and, when cosign is installed and the release had a signature bundle, the
// archive's keyless signature from the repository's release workflow.
func verifyRelease(v verifyIO) error {
	info, err := vrReadRecord(filepath.Join(v.root, "bin", "INSTALL_SOURCE"))
	if err != nil {
		return fmt.Errorf("no install record (bin/INSTALL_SOURCE): %w; reinstall the plugin", err)
	}
	fmt.Fprintln(v.out, version.String())
	if info["source"] != "release" {
		fmt.Fprintf(v.out, "Built from source (version %s): there is no release archive to verify.\n", info["version"])
		return nil
	}
	asset, repo := info["asset"], info["repo"]
	if !vrAssetRe.MatchString(asset) || !vrRepoRe.MatchString(repo) {
		return fmt.Errorf("bin/INSTALL_SOURCE names an unexpected asset %q or repository %q", asset, repo)
	}
	want, err := vrChecksum(filepath.Join(v.root, "release", "SHA256SUMS"), asset)
	if err != nil {
		return err
	}
	archive := filepath.Join(v.root, "bin", "release", asset)
	got, err := vrFileSHA256(archive)
	if err != nil {
		return fmt.Errorf("read the kept release archive: %w", err)
	}
	if got != want {
		return fmt.Errorf("%s: checksum %s does not match release/SHA256SUMS (%s)", asset, got, want)
	}
	fmt.Fprintf(v.out, "checksum ok: %s matches release/SHA256SUMS\n", asset)

	exe := "herdr-dots"
	if strings.HasSuffix(asset, ".zip") {
		exe = "herdr-dots.exe"
	}
	inArchive, err := vrMemberSHA256(archive, exe)
	if err != nil {
		return err
	}
	installed, err := vrFileSHA256(filepath.Join(v.root, "bin", exe))
	if err != nil {
		return fmt.Errorf("read bin/%s: %w", exe, err)
	}
	if installed != inArchive {
		return fmt.Errorf("bin/%s is not the binary in %s: it was replaced after the install", exe, asset)
	}
	fmt.Fprintf(v.out, "binary ok: bin/%s is the one in %s\n", exe, asset)

	bundle := archive + ".sigstore.json"
	cosign, err := v.lookPath("cosign")
	switch {
	case err != nil:
		fmt.Fprintln(v.out, "signature: not checked (install cosign to check it)")
	case !vrExists(bundle):
		fmt.Fprintln(v.out, "signature: not checked (the release had no .sigstore.json bundle)")
	default:
		identity := "^https://github\\.com/" + regexp.QuoteMeta(repo) + "/\\.github/workflows/release\\.yml@refs/tags/v"
		out, err := v.run(cosign, "verify-blob", "--bundle", bundle,
			"--certificate-identity-regexp", identity, "--certificate-oidc-issuer", vrSigIssuer, archive)
		if err != nil {
			return fmt.Errorf("signature check failed: %v\n%s", err, strings.TrimSpace(out))
		}
		fmt.Fprintf(v.out, "signature ok: signed by the release workflow of github.com/%s\n", repo)
	}
	return nil
}

func vrExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// vrReadRecord reads key=value lines.
func vrReadRecord(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok {
			m[k] = val
		}
	}
	return m, sc.Err()
}

// vrChecksum finds asset's line in a SHA256SUMS file ("<hex>  <name>").
func vrChecksum(path, asset string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("release/SHA256SUMS has no checksum for %s", asset)
}

func vrFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// vrMemberSHA256 hashes the file called name at the top of a .tar.gz or .zip.
func vrMemberSHA256(archive, name string) (string, error) {
	h := sha256.New()
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					return "", err
				}
				defer rc.Close()
				if _, err := io.Copy(h, rc); err != nil {
					return "", err
				}
				return hex.EncodeToString(h.Sum(nil)), nil
			}
		}
		return "", fmt.Errorf("%s has no %s", filepath.Base(archive), name)
	}
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	for {
		hd, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s has no %s", filepath.Base(archive), name)
		}
		if err != nil {
			return "", err
		}
		if hd.Name == name && hd.Typeflag == tar.TypeReg {
			if _, err := io.Copy(h, tr); err != nil {
				return "", err
			}
			return hex.EncodeToString(h.Sum(nil)), nil
		}
	}
}
```

Run: `GOTOOLCHAIN=local go test -race ./cmd/herdr-dots/ -run 'VerifyRelease|Dispatch' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS for `TestVerifyReleaseChecksArchiveAndBinary`, `TestVerifyReleaseRefusesATamperedArchive`, `TestVerifyReleaseRefusesAReplacedBinary`, `TestVerifyReleaseRunsCosignWithTheWorkflowIdentity`, `TestVerifyReleaseOnASourceBuild` and the dispatch tests.

- [ ] **Step 9: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 10: Commit (never push)**

```bash
git add scripts/build.sh scripts/build.ps1 scripts/build_test.go scripts/build_ps1_test.go scripts/release_test.go cmd/herdr-dots/verifyrelease.go cmd/herdr-dots/verifyrelease_test.go cmd/herdr-dots/platform_manifest_test.go
git commit -m "feat(install): verified prebuilt binaries with a source-build fallback, and verify-release (Phase 10)"
```

---

### Task 7: Migrations at startup, read-only protection, `migrate` command

**Depends on:** Task 2 (`migrate.Default`, `Set.Run`, `Roots`, `RootsOf`, `Options`, `Report`; `fsutil.Protected`, `ResetProtected`). Wave 2.

**Files:**
- Create: `cmd/herdr-dots/migrate.go`, `cmd/herdr-dots/migrate_test.go`
- Modify: `cmd/herdr-dots/main.go` (`dispatch`), `cmd/herdr-dots/engine.go` (`serveEngine`)

**Interfaces:**
- Consumes: Task 2's API; `paths.FromEnv`, `paths.Discover`, `paths.New`, `paths.Paths`; `lockfile.Held`, `lockfile.TryLock`; `herdr.Client.Notify(ctx, title, body string) error` (`herdr.NewFromEnv()` in `serveEngine`); the `commands` map and `dispatch` in `main.go` (Task 5's version). At `dffabbf` the commands that write persisted files outside the engine are `manage` (dots, keys, trust, memory, exports), `keys` (credentials), `dots` (dots, trust, memory imports), `setup-mcp` (`mcp-install.json`), plus Task 8's `uninstall-extras`; `memory` and `mcp` write only memory files, which refuse newer versions themselves (`memory.ErrVersion`), so they are left out and the MCP server's start stays fast.
- Produces: `func startupMigrations(p paths.Paths, logf func(string, ...any)) migrate.Report`; `func readOnlyNotice(rep migrate.Report) string`; `var writingCommands map[string]bool` (`manage`, `keys`, `dots`, `setup-mcp`, `uninstall-extras`); `func protectNewerFiles(cmd string)` (called by `dispatch` before every command); `var sessionDirRe` (12 hex); command `herdr-dots migrate [--dry-run]` (`runMigrate`, testable `migrateCmd(args []string, p paths.Paths, out io.Writer) error`).

- [ ] **Step 1: Write the failing tests**

Create `cmd/herdr-dots/migrate_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/migrate"
	"herdr-dots/internal/paths"
)

func migratePaths(t *testing.T) paths.Paths {
	t.Helper()
	base := t.TempDir()
	p := paths.New(filepath.Join(base, "config"), filepath.Join(base, "state"), "/tmp/herdr-test.sock")
	for _, d := range []string{p.ConfigDir, p.SessionDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateCommandReportsAndSkipsRunningSessions(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.ConfigFile(), "default_backend = \"claude-cli\"\n")
	writeTestFile(t, p.QueueFile(), `{"version":2,"cards":[]}`) // from a newer herdr-dots
	other := filepath.Join(p.StateDir, "0123456789ab")
	writeTestFile(t, filepath.Join(other, "queue.json"), `{"next":0,"cards":[]}`)
	held, err := lockfile.TryLock(filepath.Join(other, "engine.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	var out bytes.Buffer
	if err := migrateCmd([]string{"--dry-run"}, p, &out); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "read-only 1") || !strings.Contains(got, "queue.json") {
		t.Fatalf("the newer queue must be reported:\n%s", got)
	}
	if !strings.Contains(got, "skipped 1 running session(s) (0123456789ab)") {
		t.Fatalf("a running session must be skipped:\n%s", got)
	}
}

func TestReadOnlyNoticeNamesTheFiles(t *testing.T) {
	if readOnlyNotice(migrate.Report{}) != "" {
		t.Fatal("no notice without read-only files")
	}
	n := readOnlyNotice(migrate.Report{ReadOnly: []string{"/s/queue.json", "/c/dots/security.toml"}})
	if !strings.Contains(n, "2 file(s)") || !strings.Contains(n, "queue.json, security.toml") {
		t.Fatalf("notice %q", n)
	}
}

func TestWritingCommandsProtectNewerFiles(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.QueueFile(), `{"version":3,"cards":[]}`)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", p.ConfigDir)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", p.StateDir)
	t.Setenv("HERDR_SOCKET_PATH", p.SocketPath)
	protectNewerFiles("pane")
	if fsutil.Protected(p.QueueFile()) {
		t.Fatal("the pane writes no state: nothing to protect")
	}
	protectNewerFiles("manage")
	if !fsutil.Protected(p.QueueFile()) {
		t.Fatal("manage must not overwrite a newer queue")
	}
}

func TestStartupMigrationsLogsTheSummary(t *testing.T) {
	t.Cleanup(fsutil.ResetProtected)
	p := migratePaths(t)
	writeTestFile(t, p.UsageFile(), `{"version":5}`)
	var logs []string
	rep := startupMigrations(p, func(f string, a ...any) { logs = append(logs, f) })
	if len(rep.ReadOnly) != 1 || len(logs) == 0 {
		t.Fatalf("report %+v logs %v", rep, logs)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTOOLCHAIN=local go test ./cmd/herdr-dots/ -run 'Migrat|ReadOnly|Protect|Startup' 2>&1 | tail -3`
Expected: FAIL to build: `undefined: migrateCmd`, `undefined: readOnlyNotice`, `undefined: protectNewerFiles`, `undefined: startupMigrations`.

- [ ] **Step 3: The command and the helpers**

Create `cmd/herdr-dots/migrate.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/migrate"
	"herdr-dots/internal/paths"
)

func init() { commands["migrate"] = runMigrate }

// startupMigrations runs at engine start, while the engine holds its session
// lock: it upgrades config, shared state (under each file's lock) and this
// session's files, and marks files from a newer version read-only.
func startupMigrations(p paths.Paths, logf func(string, ...any)) migrate.Report {
	rep, err := migrate.Default().Run(migrate.RootsOf(p), migrate.Options{Logf: logf})
	if err != nil {
		logf("migrate: %v", err)
	}
	if !rep.Empty() {
		logf("migrate: %s", rep.Summary())
	}
	return rep
}

// readOnlyNotice is the toast for files a newer herdr-dots wrote ("" for none).
func readOnlyNotice(rep migrate.Report) string {
	if len(rep.ReadOnly) == 0 {
		return ""
	}
	names := make([]string, 0, len(rep.ReadOnly))
	for _, f := range rep.ReadOnly {
		names = append(names, filepath.Base(f))
	}
	return fmt.Sprintf("%d file(s) were written by a newer herdr-dots and stay read-only: %s. Update the plugin, or see engine.log.",
		len(names), strings.Join(names, ", "))
}

// writingCommands are the commands that write persisted files outside the
// engine. Before they run, files a newer version wrote are marked read-only
// in this process too. (`memory` and `mcp` write only memory files, which
// refuse newer versions themselves.)
var writingCommands = map[string]bool{
	"manage": true, "keys": true, "dots": true, "setup-mcp": true, "uninstall-extras": true,
}

// protectNewerFiles marks newer files read-only for this process. It only
// reads versions; any error just means nothing is marked.
func protectNewerFiles(cmd string) {
	if !writingCommands[cmd] {
		return
	}
	p, err := paths.FromEnv()
	if err != nil {
		if p, err = paths.Discover(); err != nil {
			return
		}
	}
	_, _ = migrate.Default().Run(migrate.RootsOf(p), migrate.Options{DryRun: true})
}

var sessionDirRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

func runMigrate(args []string) error {
	p, err := paths.Discover()
	if err != nil {
		return err
	}
	return migrateCmd(args, p, os.Stdout)
}

// migrateCmd upgrades config and shared state, then every session directory
// whose engine is not running (a running engine migrates its own session at
// its next start). --dry-run only reports.
func migrateCmd(args []string, p paths.Paths, out io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "report what would change; write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o := migrate.Options{DryRun: *dry}
	set := migrate.Default()
	total := migrate.Report{}
	add := func(r migrate.Report) {
		total.Migrated = append(total.Migrated, r.Migrated...)
		total.Pending = append(total.Pending, r.Pending...)
		total.ReadOnly = append(total.ReadOnly, r.ReadOnly...)
		total.Failed = append(total.Failed, r.Failed...)
		total.Corrupt = append(total.Corrupt, r.Corrupt...)
		total.Backups = append(total.Backups, r.Backups...)
	}
	rep, err := set.Run(migrate.Roots{Config: p.ConfigDir, State: p.StateDir}, o)
	if err != nil {
		return err
	}
	add(rep)
	ents, _ := os.ReadDir(p.StateDir)
	var busy []string
	for _, e := range ents {
		if !e.IsDir() || !sessionDirRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(p.StateDir, e.Name())
		if lockfile.Held(filepath.Join(dir, "engine.lock")) {
			busy = append(busy, e.Name())
			continue
		}
		rep, err := set.Run(migrate.Roots{Session: dir}, o)
		if err != nil {
			return err
		}
		add(rep)
	}
	sort.Strings(busy)
	fmt.Fprintln(out, total.Summary())
	for _, b := range total.Backups {
		fmt.Fprintf(out, "backup: %s\n", b)
	}
	if len(busy) > 0 {
		fmt.Fprintf(out, "skipped %d running session(s) (%s): their engines migrate them at the next start\n", len(busy), strings.Join(busy, ", "))
	}
	if len(total.Failed) > 0 {
		return fmt.Errorf("%d file(s) could not be migrated", len(total.Failed))
	}
	return nil
}
```

- [ ] **Step 4: Wire them in**

Apply to `cmd/herdr-dots/main.go` (one line in `dispatch`, after the unknown-command check; against Task 5's `main.go`):

```diff
--- a/cmd/herdr-dots/main.go
+++ b/cmd/herdr-dots/main.go
@@ -54,6 +54,7 @@ func dispatch(args []string) error {
 	if !ok {
 		return fmt.Errorf("unknown command %q (%v)", args[0], usage())
 	}
+	protectNewerFiles(args[0])
 	return h(args[1:])
 }
 
```

Apply to `cmd/herdr-dots/engine.go` (in `serveEngine`: the migrations go right after the logger is created, which is after the engine lock is held and before `config.Load` and `buildEngine`/`engineDeps` open any store; the toast goes right after `herdr.NewFromEnv()` succeeds):

```diff
--- a/cmd/herdr-dots/engine.go
+++ b/cmd/herdr-dots/engine.go
@@ -246,6 +246,7 @@ func serveEngine(p paths.Paths) error {
 	}
 	defer lock.Release()
 	logger := log.New(os.Stderr, "herdr-dots ", log.LstdFlags)
+	migrated := startupMigrations(p, logger.Printf) // before any store opens a file
 
 	cfg, err := config.Load(p.ConfigFile())
 	if err != nil {
@@ -255,6 +256,11 @@ func serveEngine(p paths.Paths) error {
 	if err != nil {
 		return err
 	}
+	if msg := readOnlyNotice(migrated); msg != "" {
+		if err := hc.Notify(context.Background(), "Herdr Dots", msg); err != nil {
+			logger.Printf("notify: %v", err)
+		}
+	}
 	e, err := buildEngine(p, hc, cfg, logger.Printf)
 	if err != nil {
 		return err
```

- [ ] **Step 5: Run the tests**

Run: `GOTOOLCHAIN=local go test -race ./cmd/herdr-dots/ -v 2>&1 | grep -E '^(--- (PASS|FAIL)|ok|FAIL)' | grep -E 'Migrat|ReadOnly|Protect|Startup|^ok|FAIL'`
Expected: PASS for `TestMigrateCommandReportsAndSkipsRunningSessions`, `TestReadOnlyNoticeNamesTheFiles`, `TestWritingCommandsProtectNewerFiles`, `TestStartupMigrationsLogsTheSummary`, and the package `ok`.

- [ ] **Step 6: Try the command on a scratch tree**

Run: `D=$(mktemp -d) && mkdir -p "$D/c" "$D/s/0123456789ab" && printf '{"version":2,"cards":[]}' > "$D/s/0123456789ab/queue.json" && HERDR_PLUGIN_CONFIG_DIR="$D/c" HERDR_PLUGIN_STATE_DIR="$D/s" GOTOOLCHAIN=local go run ./cmd/herdr-dots migrate --dry-run; cat "$D/s/0123456789ab/queue.json"; echo; rm -rf "$D"`
Expected: `read-only 1 (<tmp>/s/0123456789ab/queue.json)` and then the file unchanged: `{"version":2,"cards":[]}` (run again by the reconcile pass at `dffabbf` plus Tasks 2 and 5; the command works outside Herdr with only the two env vars, through `paths.Discover`).

- [ ] **Step 7: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 8: Commit (never push)**

```bash
git add cmd/herdr-dots/migrate.go cmd/herdr-dots/migrate_test.go cmd/herdr-dots/main.go cmd/herdr-dots/engine.go
git commit -m "feat: run schema migrations at engine start, keep newer files read-only everywhere, herdr-dots migrate (Phase 10)"
```

---

### Task 8: `uninstall-extras` and `keys.RemoveBlock`

**Depends on:** Task 3 (it may have touched `internal/keys/`; this task adds new files only). Phase 5's `runSetupMCP(args []string) error` with `--remove` and `--yes`. Wave 2.

**Files:**
- Create: `internal/keys/remove.go`, `internal/keys/remove_test.go`, `cmd/herdr-dots/uninstall.go`, `cmd/herdr-dots/uninstall_test.go`

**Interfaces:**
- Consumes: `keys.Block`, `keys.Defaults` (four bindings since Phase 3: manage, toggle-pane, summon, council), `keys.Existing(data []byte) (map[string]string, error)` (key → command), `keys.ConfigPath(getenv func(string) string, home string) string` (as `cmd/herdr-dots/setupkeys.go` calls it), the test fixture `userConfig` (keys_test.go); `fsutil.WriteFileAtomic`; `lockfile.Held`, `lockfile.TryLock`; `paths.Discover`, `paths.New`, `paths.Paths`; Phase 5's `runSetupMCP(args []string) error` with `--remove` and `--yes`.
- Produces: `func keys.RemoveBlock(data string) (out string, removed, left int, err error)`; command `herdr-dots uninstall-extras [--purge] [--yes]` (`runUninstallExtras`, testable `uninstallExtras(args []string, u uninstallIO) error`, `type uninstallIO struct{ in io.Reader; out io.Writer; keysPath string; removeMCP func(yes bool) error; paths func() (paths.Paths, error); home string; now func() time.Time }`); `const purgeConfirm = "purge herdr-dots"`; `func safeToPurge(dir, home string) bool`; `func runningEngines(stateDir string) []string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/keys/remove_test.go`:

```go
package keys

import (
	"strings"
	"testing"
)

func TestRemoveBlockKeepsEverythingElseByteForByte(t *testing.T) {
	for _, c := range []struct{ name, user, want string }{
		{"ends in another table", "# my config\n" + userConfig + "\n# a comment the user wrote\n[ui]\ntheme = \"dark\" # inline\n", ""},
		{"ends in someone's binding", userConfig, ""},
		{"empty config", "", ""},
		// setup-keys's leading newline ended the user's last line; it stays.
		{"no newline at the very end", "[ui]\ntheme = \"dark\"", "[ui]\ntheme = \"dark\"\n"},
	} {
		want := c.want
		if want == "" {
			want = c.user
		}
		out, removed, left, err := RemoveBlock(c.user + Block(Defaults()))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if removed != len(Defaults()) || left != 0 { // manage, toggle-pane, summon, council
			t.Fatalf("%s: removed %d left %d", c.name, removed, left)
		}
		if out != want {
			t.Fatalf("%s: other bytes changed:\n--- want\n%q\n--- got\n%q", c.name, want, out)
		}
	}
}

func TestRemoveBlockFindsHandWrittenBindingsInTheMiddle(t *testing.T) {
	in := "[keys]\nprefix = \"ctrl+a\"\n\n[[keys.command]]\nkey = \"prefix+d\"\ncommand = 'herdr-dots.manage'\ntype = \"plugin_action\"\n\n[[keys.command]]\nkey = \"prefix+up\"\ntype = \"plugin_action\"\ncommand = \"cloudmanic.herdr-plus.projects\"\n"
	out, removed, left, err := RemoveBlock(in)
	if err != nil || removed != 1 || left != 0 {
		t.Fatalf("removed %d left %d err %v", removed, left, err)
	}
	if strings.Contains(out, "herdr-dots") || !strings.Contains(out, "cloudmanic.herdr-plus.projects") || !strings.Contains(out, "prefix = \"ctrl+a\"") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestRemoveBlockCountsBindingsItCannotRemove(t *testing.T) {
	in := "[keys]\ncommand = [{ key = \"prefix+d\", type = \"plugin_action\", command = \"herdr-dots.manage\" }]\n"
	out, removed, left, err := RemoveBlock(in)
	if err != nil || removed != 0 || left != 1 || out != in {
		t.Fatalf("removed %d left %d err %v out %q", removed, left, err, out)
	}
}

func TestRemoveBlockWithNothingToRemoveIsANoOp(t *testing.T) {
	out, removed, left, err := RemoveBlock(userConfig)
	if err != nil || removed != 0 || left != 0 || out != userConfig {
		t.Fatalf("removed %d left %d err %v", removed, left, err)
	}
}
```

Create `cmd/herdr-dots/uninstall_test.go`:

```go
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/keys"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

const herdrConfig = "# mine\n[keys]\nprefix = \"ctrl+a\"\n\n[[keys.command]]\nkey = \"prefix+up\"\ntype = \"plugin_action\"\ncommand = \"cloudmanic.herdr-plus.projects\"\n"

type uninstallRig struct {
	u       uninstallIO
	out     *bytes.Buffer
	p       paths.Paths
	mcpRuns []bool
}

func newUninstallRig(t *testing.T, input string) *uninstallRig {
	t.Helper()
	base := t.TempDir()
	r := &uninstallRig{out: &bytes.Buffer{}}
	r.p = paths.New(filepath.Join(base, "plugins", "config", "herdr-dots"), filepath.Join(base, "plugins", "state", "herdr-dots"), "/tmp/h.sock")
	for _, d := range []string{r.p.DotsDir(), filepath.Join(r.p.StateDir, "memory", "g-0123456789abcdef")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	kp := filepath.Join(base, "herdr", "config.toml")
	if err := os.MkdirAll(filepath.Dir(kp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kp, []byte(herdrConfig+keys.Block(keys.Defaults())), 0o644); err != nil {
		t.Fatal(err)
	}
	r.u = uninstallIO{in: strings.NewReader(input), out: r.out, keysPath: kp,
		removeMCP: func(yes bool) error { r.mcpRuns = append(r.mcpRuns, yes); return nil },
		paths:     func() (paths.Paths, error) { return r.p, nil }, home: filepath.Join(base, "home"),
		now: func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }}
	return r
}

func TestUninstallRemovesMCPAndKeysWithABackupAndKeepsData(t *testing.T) {
	r := newUninstallRig(t, "")
	if err := uninstallExtras([]string{"--yes"}, r.u); err != nil {
		t.Fatalf("%v\n%s", err, r.out.String())
	}
	if len(r.mcpRuns) != 1 || !r.mcpRuns[0] {
		t.Fatalf("setup-mcp --remove --yes runs once: %v", r.mcpRuns)
	}
	got, _ := os.ReadFile(r.u.keysPath)
	if string(got) != herdrConfig {
		t.Fatalf("config after uninstall:\n%s", got)
	}
	if st, err := os.Stat(r.u.keysPath); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o644) {
		t.Fatalf("the config keeps its mode: %v", err)
	}
	backup, err := os.ReadFile(r.u.keysPath + ".bak-herdr-dots-20261008-120000")
	if err != nil || !strings.Contains(string(backup), "herdr-dots.manage") {
		t.Fatalf("backup: %v", err)
	}
	if _, err := os.Stat(r.p.DotsDir()); err != nil {
		t.Fatal("without --purge the dots stay")
	}
	if !strings.Contains(r.out.String(), "--purge") {
		t.Fatalf("output should mention --purge:\n%s", r.out.String())
	}
}

func TestUninstallAsksBeforeEditingTheConfig(t *testing.T) {
	r := newUninstallRig(t, "n\n")
	if err := uninstallExtras(nil, r.u); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(r.u.keysPath); !strings.Contains(string(got), "herdr-dots.manage") {
		t.Fatal("answering n must keep the keybindings")
	}
	if len(r.mcpRuns) != 1 || r.mcpRuns[0] {
		t.Fatalf("setup-mcp asks for itself without --yes: %v", r.mcpRuns)
	}
}

func TestUninstallContinuesWhenMCPRemovalFails(t *testing.T) {
	r := newUninstallRig(t, "")
	r.u.removeMCP = func(bool) error { return errors.New("claude CLI not found") }
	if err := uninstallExtras([]string{"--yes"}, r.u); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), "claude CLI not found (continuing)") {
		t.Fatalf("output:\n%s", r.out.String())
	}
	if got, _ := os.ReadFile(r.u.keysPath); string(got) != herdrConfig {
		t.Fatal("keys must still be removed")
	}
}

func TestPurgeNeedsTheTypedPhraseEvenWithYes(t *testing.T) {
	r := newUninstallRig(t, "yes\n")
	if err := uninstallExtras([]string{"--yes", "--purge"}, r.u); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.p.StateDir); err != nil {
		t.Fatal("a wrong confirmation must delete nothing")
	}
	r2 := newUninstallRig(t, purgeConfirm+"\n")
	if err := uninstallExtras([]string{"--yes", "--purge"}, r2.u); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{r2.p.ConfigDir, r2.p.StateDir} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("%s survived the purge", d)
		}
	}
}

func TestPurgeRefusesWhileAnEngineRuns(t *testing.T) {
	r := newUninstallRig(t, purgeConfirm+"\n")
	if err := os.MkdirAll(r.p.SessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := lockfile.TryLock(r.p.EngineLock())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	err = uninstallExtras([]string{"--yes", "--purge"}, r.u)
	if err == nil || !strings.Contains(err.Error(), "quit Herdr first") {
		t.Fatalf("purge with a running engine: %v", err)
	}
	if _, err := os.Stat(r.p.StateDir); err != nil {
		t.Fatal("nothing may be deleted")
	}
}

func TestSafeToPurgeRefusesHomeRootAndRelativePaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home", "me") // absolute on every OS
	root := filepath.VolumeName(home) + string(filepath.Separator)
	for _, d := range []string{"", filepath.Join("relative", "dir"), root, filepath.Dir(home), home, filepath.Join(root, "a", "b")} {
		if safeToPurge(d, home) {
			t.Errorf("safeToPurge(%q) = true", d)
		}
	}
	if !safeToPurge(filepath.Join(home, ".config", "herdr", "plugins", "herdr-dots"), home) {
		t.Error("a plugin dir under home must be purgeable")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/keys/ ./cmd/herdr-dots/ 2>&1 | tail -4`
Expected: FAIL to build: `undefined: RemoveBlock`, `undefined: uninstallExtras`.

- [ ] **Step 3: `keys.RemoveBlock`**

Create `internal/keys/remove.go`:

```go
package keys

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// blockHeader is the comment setup-keys writes above its bindings.
const blockHeader = "# herdr-dots keybindings (added by `herdr-dots setup-keys`)"

var (
	tableHeaderRe  = regexp.MustCompile(`^\s*\[`)
	commandTableRe = regexp.MustCompile(`^\s*\[\[\s*keys\.command\s*\]\]\s*(#.*)?$`)
	ourCommandRe   = regexp.MustCompile(`^\s*command\s*=\s*["']herdr-dots\.`)
)

// RemoveBlock deletes every [[keys.command]] table whose command is one of
// this plugin's actions (herdr-dots.*), and setup-keys's header comment, and
// keeps every other byte of the file. removed counts deleted tables; left
// counts plugin bindings it could not remove (other syntax, such as an
// inline keys.command array), which the developer removes by hand. The
// result must still parse; otherwise the original comes back with an error.
func RemoveBlock(data string) (out string, removed, left int, err error) {
	// First drop setup-keys's header comment and the blank line it wrote
	// before it (the header can sit inside the span of the user's last table).
	var lines []string
	for _, l := range strings.SplitAfter(data, "\n") {
		if strings.TrimRight(l, "\r\n") == blockHeader {
			if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == "" {
				lines = lines[:n-1]
			}
			continue
		}
		lines = append(lines, l)
	}
	var keep []string
	for i := 0; i < len(lines); {
		l := lines[i]
		if !commandTableRe.MatchString(strings.TrimRight(l, "\r\n")) {
			keep = append(keep, l)
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && !tableHeaderRe.MatchString(lines[end]) {
			end++
		}
		ours := false
		for _, b := range lines[i+1 : end] {
			ours = ours || ourCommandRe.MatchString(b)
		}
		if !ours {
			keep = append(keep, lines[i:end]...)
			i = end
			continue
		}
		removed++
		// Drop one blank line before the table, as setup-keys wrote one.
		if n := len(keep); n > 0 && strings.TrimSpace(keep[n-1]) == "" {
			keep = keep[:n-1]
		}
		i = end
	}
	out = strings.Join(keep, "")
	var check map[string]any
	if _, perr := toml.Decode(out, &check); perr != nil {
		return data, 0, 0, fmt.Errorf("removing the keybindings would make the config invalid TOML (%v); nothing changed: remove them by hand", perr)
	}
	ex, perr := Existing([]byte(out))
	if perr != nil {
		return data, 0, 0, perr
	}
	for _, cmd := range ex {
		if strings.HasPrefix(cmd, "herdr-dots.") {
			left++
		}
	}
	return out, removed, left, nil
}
```

Run: `GOTOOLCHAIN=local go test -race ./internal/keys/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS for `TestRemoveBlockKeepsEverythingElseByteForByte`, `TestRemoveBlockFindsHandWrittenBindingsInTheMiddle`, `TestRemoveBlockCountsBindingsItCannotRemove`, `TestRemoveBlockWithNothingToRemoveIsANoOp` and the existing keys tests (including Phase 3's council key and Task 3's `FuzzKeysExisting` seeds).

- [ ] **Step 4: `uninstall-extras`**

Create `cmd/herdr-dots/uninstall.go`:

```go
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/keys"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

func init() { commands["uninstall-extras"] = runUninstallExtras }

// uninstallSessionRe matches a session directory name (paths.SessionKey).
var uninstallSessionRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

// purgeConfirm is what the developer types to delete every dot and all memory.
const purgeConfirm = "purge herdr-dots"

// uninstallIO is what uninstall-extras touches, so tests can fake it.
type uninstallIO struct {
	in        io.Reader
	out       io.Writer
	keysPath  string               // Herdr's config.toml
	removeMCP func(yes bool) error // setup-mcp --remove
	paths     func() (paths.Paths, error)
	home      string
	now       func() time.Time
}

func runUninstallExtras(args []string) error {
	home, _ := os.UserHomeDir()
	return uninstallExtras(args, uninstallIO{
		in: os.Stdin, out: os.Stdout,
		keysPath: keys.ConfigPath(os.Getenv, home),
		removeMCP: func(yes bool) error {
			a := []string{"--remove"}
			if yes {
				a = append(a, "--yes")
			}
			return runSetupMCP(a)
		},
		paths: paths.Discover, home: home, now: time.Now,
	})
}

// uninstallExtras removes what herdr-dots added outside its own directories:
// the MCP server entries in Claude Code and Codex, and the keybinding block
// in Herdr's config.toml, each after a preview and a backup. `herdr plugin
// uninstall` removes the plugin itself. With --purge it also deletes the
// plugin's config and state directories (every dot, setting, credential and
// all memory) after the developer types "purge herdr-dots"; --yes never
// skips that.
func uninstallExtras(args []string, u uninstallIO) error {
	fs := flag.NewFlagSet("uninstall-extras", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete every dot, setting, credential and all memory")
	yes := fs.Bool("yes", false, "apply without asking (except --purge, which always asks)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	in := bufio.NewReader(u.in)

	fmt.Fprintln(u.out, "== MCP server ==")
	if err := u.removeMCP(*yes); err != nil {
		fmt.Fprintf(u.out, "MCP: %v (continuing)\n", err)
	}

	fmt.Fprintln(u.out, "\n== Keybindings ==")
	if err := removeKeyBlock(u, in, *yes); err != nil {
		return err
	}

	p, perr := u.paths()
	if !*purge {
		if perr == nil {
			fmt.Fprintf(u.out, "\nKept your dots, settings, credentials and memory in %s and %s.\nRun `herdr-dots uninstall-extras --purge` to delete them too.\n", p.ConfigDir, p.StateDir)
		}
		return nil
	}
	if perr != nil {
		return fmt.Errorf("find the plugin directories to purge: %w", perr)
	}
	return purgeDirs(u, in, p)
}

func removeKeyBlock(u uninstallIO, in *bufio.Reader, yes bool) error {
	path := u.keysPath
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // edit through a dotfiles symlink
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(u.out, "No Herdr config at %s: nothing to remove.\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	out, removed, left, err := keys.RemoveBlock(string(data))
	if err != nil {
		return err
	}
	if left > 0 {
		fmt.Fprintf(u.out, "%d herdr-dots binding(s) in %s use a syntax this command can't edit: remove them by hand.\n", left, path)
	}
	if removed == 0 {
		fmt.Fprintln(u.out, "No herdr-dots keybindings to remove.")
		return nil
	}
	fmt.Fprintf(u.out, "Will remove %d herdr-dots keybinding(s) from %s.\n", removed, path)
	if !yes && !uninstallConfirm(u.out, in, "Remove them? [y/N] ") {
		fmt.Fprintln(u.out, "Kept the keybindings.")
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	backup := path + ".bak-herdr-dots-" + u.now().Format("20060102-150405")
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(path, []byte(out), st.Mode().Perm()); err != nil {
		return err
	}
	fmt.Fprintf(u.out, "Removed. Backup: %s\nRun `herdr server reload-config` (or restart Herdr) to drop them now.\n", backup)
	return nil
}

func purgeDirs(u uninstallIO, in *bufio.Reader, p paths.Paths) error {
	for _, d := range []string{p.ConfigDir, p.StateDir} {
		if !safeToPurge(d, u.home) {
			return fmt.Errorf("refusing to delete %q: it is not a plugin directory", d)
		}
	}
	if busy := runningEngines(p.StateDir); len(busy) > 0 {
		return fmt.Errorf("%d Dots engine(s) are running (%s): quit Herdr first, then purge", len(busy), strings.Join(busy, ", "))
	}
	fmt.Fprintf(u.out, "\nThis deletes, with no undo:\n  %s (dots, settings, credentials)\n  %s (memory of every project, queues, history)\n", p.ConfigDir, p.StateDir)
	fmt.Fprintf(u.out, "Type %q to confirm: ", purgeConfirm)
	line, _ := in.ReadString('\n')
	if strings.TrimSpace(line) != purgeConfirm {
		fmt.Fprintln(u.out, "Not confirmed. Nothing deleted.")
		return nil
	}
	for _, d := range []string{p.ConfigDir, p.StateDir} {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	fmt.Fprintln(u.out, "Deleted. Run `herdr plugin uninstall herdr-dots` to remove the plugin itself.")
	return nil
}

func uninstallConfirm(out io.Writer, in *bufio.Reader, prompt string) bool {
	fmt.Fprint(out, prompt)
	line, _ := in.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

// safeToPurge refuses relative paths, the filesystem root, the home
// directory and anything above it, and very short paths.
func safeToPurge(dir, home string) bool {
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	dir = filepath.Clean(dir)
	if home != "" {
		h := filepath.Clean(home)
		if dir == h || strings.HasPrefix(h, dir+string(filepath.Separator)) {
			return false
		}
	}
	return len(strings.Split(strings.Trim(filepath.ToSlash(dir), "/"), "/")) >= 3
}

// runningEngines lists session directories whose engine holds its lock.
func runningEngines(stateDir string) []string {
	ents, _ := os.ReadDir(stateDir)
	var busy []string
	for _, e := range ents {
		if e.IsDir() && uninstallSessionRe.MatchString(e.Name()) && lockfile.Held(filepath.Join(stateDir, e.Name(), "engine.lock")) {
			busy = append(busy, e.Name())
		}
	}
	return busy
}
```


- [ ] **Step 5: Run the tests**

Run: `GOTOOLCHAIN=local go test -race ./cmd/herdr-dots/ -run 'Uninstall|Purge|SafeToPurge' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS for `TestUninstallRemovesMCPAndKeysWithABackupAndKeepsData`, `TestUninstallAsksBeforeEditingTheConfig`, `TestUninstallContinuesWhenMCPRemovalFails`, `TestPurgeNeedsTheTypedPhraseEvenWithYes`, `TestPurgeRefusesWhileAnEngineRuns`, `TestSafeToPurgeRefusesHomeRootAndRelativePaths`.

- [ ] **Step 6: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 7: Commit (never push)**

```bash
git add internal/keys/remove.go internal/keys/remove_test.go cmd/herdr-dots/uninstall.go cmd/herdr-dots/uninstall_test.go
git commit -m "feat: uninstall-extras removes MCP entries and keybindings with backups; --purge after a typed confirmation (Phase 10)"
```

---

### Task 9: Hardening triage A: secrets, nested projects, git pins (and the triage notes)

**Depends on:** Task 3 (it changed `internal/memory/tomlio.go` and added `internal/memory/fuzz_test.go`; this task edits other memory files). Wave 2.

**Files:**
- Create: `internal/memory/hardening_test.go`, `internal/memory/gitcalls_test.go`, `docs/superpowers/notes/2026-10-08-phase-10-triage.md`
- Modify: `internal/secrets/secrets.go` (`Scan`), `internal/memory/store.go` (`FindProject`)

**Interfaces:**
- Consumes: `secrets.Scan` (Phase 9 moved the patterns out of memory: `memory.ScanSecrets`, `dots.Encode`, `dotpack` and `history` all call it); `memory.ScanSecrets`, `ExecGit`, `ResolveProject`, `realPath`, `Store.SeeProject`, `FindProject`, `Project{ID, Name, Root}`, the test helpers `testStore`, `isolateGit`, `newRepo`, `gitT` (`project_test.go`, `store_test.go`). Projects come back from `Store.projects()` sorted by name, then id.
- Produces: `secrets.Scan` also scans the text with format characters (`unicode.Cf`) and combining marks (`Mn`, `Me`) removed (unexported `stripInvisible`, `scan`); `FindProject(path)` returns the project with the deepest matching root; the pins `TestExecGitRunsNothingTheRepositoryConfigures` and `TestEveryGitCallDisablesRepositoryPrograms`; the triage notes (Task 12 copies its known issues into the README).

What the merged code already has (verified at `dffabbf`, so this task only pins it; Plan decision 29): every git child process (`memory.ExecGit`, `toolloop` repo listing, `worktree.Git`) passes `core.fsmonitor=false`, `core.hooksPath=<null device>` and `protocol.allow=never` with `GIT_ALLOW_PROTOCOL=none` and `GIT_NO_LAZY_FETCH=1`; `textutil.SanitizeModelText` drops the bidi controls (`TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners`); `DeleteDot` scrubs the dot's ledger lines, corrupt asides and orphaned proposals (Phase 7 Task 0: `TestDeleteDotScrubsItsLedgerAndOrphanedProposals`). The old plan's `ExecGit`, `gitEnv`, `sanitize.go`, `bidi_test.go` and `DeleteDot` edits are dropped.

- [ ] **Step 1: Write the tests**

Create `internal/memory/hardening_test.go`:

```go
package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// char is the one-character string for a code point, so invisible characters
// stay visible in this source file.
func char(cp rune) string { return string(cp) }

func TestInvisibleCharactersDoNotHideASecret(t *testing.T) {
	key := "sk-ant-api03-" + strings.Repeat("Ab3d", 6)
	for name, text := range map[string]string{
		"zero-width space":  "use " + key[:7] + char(0x200b) + key[7:],
		"zero-width joiner": "use " + key[:10] + char(0x200d) + key[10:],
		"word joiner":       "AKIA" + char(0x2060) + "ABCDEFGHIJKLMNOP",
		"soft hyphen":       "ghp_" + strings.Repeat("a1B2", 4) + char(0x00ad) + strings.Repeat("c3D4", 6),
		"byte order mark":   "token" + char(0xfeff) + " = s3cr3tV4lu3XyZw",
		"combining mark":    "sk-" + char(0x0336) + "ant-" + strings.Repeat("Ab3d", 6),
	} {
		if found, _ := ScanSecrets(text); !found {
			t.Errorf("%s: %q passed the scanner", name, text)
		}
	}
	prose := "Prefer the shared fetch wrapper." + char(0x200b) + " Emoji " + char(0x1f44d) + char(0x1f3fd) + " and caf" + char(0x00e9) + " are fine."
	if found, kind := ScanSecrets(prose); found {
		t.Fatalf("plain prose with invisible characters flagged as %s", kind)
	}
}

// A repository's own config can name an fsmonitor hook or a pager; ExecGit
// must run none of them.
func TestExecGitRunsNothingTheRepositoryConfigures(t *testing.T) {
	isolateGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a shell script")
	}
	repo := newRepo(t, 1)
	marker := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "config", "core.fsmonitor", hook)
	gitT(t, repo, "config", "core.pager", hook)
	// Plain git runs the hook, so the test can see the difference.
	_, _ = exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git does not run core.fsmonitor hooks; nothing to guard against here")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecGit(context.Background(), repo, "status", "--porcelain"); err != nil {
		t.Fatalf("ExecGit status: %v", err)
	}
	if _, err := ResolveProject(context.Background(), repo, ExecGit); err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("ExecGit ran a program from the repository's config")
	}
}

func TestFindProjectPrefersTheNestedRepository(t *testing.T) {
	s := testStore(t)
	outer := t.TempDir()
	inner := filepath.Join(outer, "vendor", "lib")
	if err := os.MkdirAll(filepath.Join(inner, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Projects are listed by name: the outer one sorts first, so a
	// first-match search finds it for every path under it.
	if err := s.SeeProject(Project{ID: "g-00000000000000aa", Name: "a-outer", Root: realPath(outer)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SeeProject(Project{ID: "g-00000000000000bb", Name: "z-lib", Root: realPath(inner)}); err != nil {
		t.Fatal(err)
	}
	for arg, want := range map[string]string{
		filepath.Join(inner, "src"):    "g-00000000000000bb",
		inner:                          "g-00000000000000bb",
		filepath.Join(outer, "vendor"): "g-00000000000000aa",
		outer:                          "g-00000000000000aa",
	} {
		p, ok, err := s.FindProject(arg)
		if err != nil || !ok || p.ID != want {
			t.Errorf("FindProject(%s) = %s %v %v, want %s", arg, p.ID, ok, err, want)
		}
	}
}
```

Create `internal/memory/gitcalls_test.go`:

```go
package memory

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// gitExecRe finds a git child process started from Go: exec.Command("git",
// exec.CommandContext(ctx, "git", platform.Command(..., "git", and so on.
var gitExecRe = regexp.MustCompile(`Command(Context)?\([^)\n]*"git"`)

// Every place that runs git in a developer's repository must override the
// repository settings that run programs (hooks, fsmonitor). A repository is
// untrusted input: cloning one must never let it run code through herdr-dots.
// Phase 7's internal/worktree runs git through a configurable binary, so its
// files are checked even without a "git" literal.
func TestEveryGitCallDisablesRepositoryPrograms(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs", "dist", "bin", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		rel, _ := filepath.Rel(root, p)
		inWorktree := strings.HasPrefix(filepath.ToSlash(rel), "internal/worktree/") && strings.Contains(src, "exec.")
		if !gitExecRe.MatchString(src) && !inWorktree {
			return nil
		}
		checked++
		for _, want := range []string{"core.fsmonitor=false", "core.hooksPath="} {
			if !strings.Contains(src, want) {
				t.Errorf("%s runs git without -c %s", rel, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("found no git calls at all: the pattern is broken")
	}
}
```

- [ ] **Step 2: Run them to see the two failures**

Run: `GOTOOLCHAIN=local go test -count=1 ./internal/memory/ -run 'Invisible|RunsNothing|Nested|EveryGitCall' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected (reconcile pass, `dffabbf`): `--- FAIL: TestInvisibleCharactersDoNotHideASecret` (every case `passed the scanner`), `--- FAIL: TestFindProjectPrefersTheNestedRepository` (the inner paths resolve to `g-00000000000000aa`), and `--- PASS` for `TestExecGitRunsNothingTheRepositoryConfigures` and `TestEveryGitCallDisablesRepositoryPrograms`, which pin what Phases 6–8 already did (the first skips where git doesn't run fsmonitor hooks; git 2.52 does).

- [ ] **Step 3: Scan without invisible characters**

Apply to `internal/secrets/secrets.go`:

```diff
--- a/internal/secrets/secrets.go
+++ b/internal/secrets/secrets.go
@@ -43,8 +43,31 @@ var entropyTokenRe = regexp.MustCompile(`[A-Za-z0-9+/=_\-]{32,}`)
 
 // Scan reports whether text looks like it contains a credential, and
 // which kind. Model-written memory that matches is refused; user-written
-// memory that matches needs a confirmation.
+// memory that matches needs a confirmation. Zero-width and other invisible
+// characters can split a key so no pattern matches ("sk-ant-\u200bapi03-…"),
+// so the text is scanned without them too.
 func Scan(text string) (bool, string) {
+	if found, kind := scan(text); found {
+		return true, kind
+	}
+	if visible := stripInvisible(text); visible != text {
+		return scan(visible)
+	}
+	return false, ""
+}
+
+// stripInvisible removes format characters (zero-width spaces and joiners,
+// word joiners, BOMs, soft hyphens, bidi controls) and combining marks.
+func stripInvisible(s string) string {
+	return strings.Map(func(r rune) rune {
+		if unicode.In(r, unicode.Cf, unicode.Mn, unicode.Me) {
+			return -1
+		}
+		return r
+	}, s)
+}
+
+func scan(text string) (bool, string) {
 	for _, p := range secretPatterns {
 		if p.re.MatchString(text) {
 			return true, p.kind
```

- [ ] **Step 4: The deepest project root wins**

Apply to `internal/memory/store.go`:

```diff
--- a/internal/memory/store.go
+++ b/internal/memory/store.go
@@ -1119,14 +1119,20 @@ func (s *Store) FindProject(arg string) (ProjectMeta, bool, error) {
 			}
 		}
 	} else {
+		// The deepest matching root wins: a repository nested inside another
+		// one (a submodule, a vendored checkout) is its own project.
 		real := realPath(arg)
-		for _, p := range ps {
+		best, bestLen := -1, -1
+		for i, p := range ps {
 			for _, r := range p.Roots {
-				if real == r || strings.HasPrefix(real, r+string(filepath.Separator)) {
-					return p, true, nil
+				if (real == r || strings.HasPrefix(real, r+string(filepath.Separator))) && len(r) > bestLen {
+					best, bestLen = i, len(r)
 				}
 			}
 		}
+		if best >= 0 {
+			return ps[best], true, nil
+		}
 	}
 	// Nothing matched: say why instead of claiming there is no memory.
 	var errs []error
```

- [ ] **Step 5: Run the tests (including the Phase 2 VITAL suite and every secrets caller)**

Run: `GOTOOLCHAIN=local go test -race -count=1 ./internal/secrets/ ./internal/memory/ ./internal/dots/ ./internal/dotpack/ ./internal/history/ ./internal/engine/ ./internal/ui/... ./cmd/herdr-dots/ 2>&1 | tail -14`
Expected: every package `ok` (reconcile pass: all `ok`), including the four tests above, `TestImportPackRefusesInvisibleCarriers`, Phase 2's `TestExecGitNeverRunsHooks`, `TestResolveProjectIgnoresInheritedGitEnv`, `TestScanSecretsFindsCredentials`, `TestScanSecretsIgnoresLookAlikes` and Phase 9's hidden-character tests in `dots` and `dotpack`.

- [ ] **Step 6: Re-read every ledger and write the triage notes**

The deferred findings live in the controller's ledgers, `.superpowers/sdd/*/progress.md` (one per phase, v1 to Phase 9; Phase 9's final fix wave may add lines). Read all of them, in the main checkout when this task runs in a worktree:

Run: `L="$(git rev-parse --git-common-dir)/../.superpowers/sdd"; grep -n -i -E 'minor \(deferred|deferred →|deferred minors|deferred \(|parked|→ phase 10|phase 10 triage|declined to judge|flak|FLAG FOR FINAL' "$L"/*/progress.md .superpowers/sdd/*/progress.md 2>/dev/null | sort -u | wc -l`
Expected: about 99 lines at `dffabbf` (v1 through Phase 9) plus the Phase 9 final-wave lines (`0205616`). Every one is either folded below, already fixed by a later phase (with the test that shows it), or a known issue with its reason. Rule for a line not already listed: fold it into this task (a small fix plus a test, in `internal/memory/*`, `internal/secrets/*` or the notes) only if it is a security or privacy issue, a panic or data loss, or a way to act on something the developer didn't see; everything else goes to "Known issues" with its reason. A fold that needs a file another Wave 2 task owns is listed as a known issue with "follow-up" instead.

Create `docs/superpowers/notes/2026-10-08-phase-10-triage.md`:

```markdown
# Phase 10 triage of deferred review findings

Source: every deferred line in `.superpowers/sdd/*/progress.md` (v1 and
Phases 2–9: "minor (deferred)", "deferred → Phase 10 triage", "Parked →
Phase 10", "FLAG FOR FINAL REVIEW", flaky-test notes), plus the Phase 2
completion notes (P2-R3, P2-R9) and ruling P8-R7. Rule: fold what matters
for a release (security, privacy, a panic, data loss, acting on something
unseen); list the rest as known issues with the reason.

## Folded into Phase 10

| Finding (ledger) | Fix | Test |
|---|---|---|
| Zero-width-character secret evasion (Phase 2): `sk-ant-` + U+200B + key passed the scanner | `secrets.Scan` (behind `memory.ScanSecrets`, dot packs, `dots.Encode`, history) also scans the text without format characters and combining marks | `TestInvisibleCharactersDoNotHideASecret` |
| Nested-repo `FindProject` prefix match (Phase 2): the first project (by name) whose root prefixes the path won, so a vendored or nested repository could resolve to its parent | the deepest matching root wins | `TestFindProjectPrefersTheNestedRepository` |
| memui approve screen clipped in tiny popups (P2-R9): the terminal cut the top lines (who proposed it, kind, paths) while `y` still approved | the approve screen drops the list header; when it still doesn't fit, it says so and `y` refuses until the popup is enlarged | `TestApproveShowsTheFullProposalAndNeedsY` (extended, Task 10) |
| `ExportTOML` wrote `ref = "d:"` for an entry without an id (found by fuzzing) | no `ref` for an unsaved entry | `FuzzImportTOML/8a748f204ecc8c6f` (Task 3) |
| Load/Delete don't sanitize slug (v1 Task 4): `dots.Load/Save/Delete` accepted `../x` | `ErrBadSlug` for a slug that is not a plain file name in the dots dir (hand-named files keep working) | `TestSlugsThatAreNotFileNamesAreRefused` (Task 10) |
| O_CREATE doesn't tighten existing file mode (v1 Task 11): an old feed file kept a loose mode | `OpenFeed` makes it 0600 | `TestOpenFeedMakesAnOldLooseFileOwnerOnly` (Task 10) |
| `Recent(n<0)` panics (v1 Task 11), `TruncateRunes(n<=0)` panics (v1 Task 13) | both return empty | `TestOpenFeedMakesAnOldLooseFileOwnerOnly`, `TestTruncateRunesNeverPanics` (Task 10) |
| Flaky `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies` (Phase 6 note, Phase 8 post-merge): the stub engine's listener was closed asynchronously after cancel, and a Unix listener unlinks its socket path on close, so a late close could delete the socket of the engine the test had just started again | the test's `startStub` stop waits for `ipc.Serve` to return (the old listener is closed and unlinked before the test goes on) | `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies` (`-race -count=50` green, Task 10) |
| Phase 7 draft tests time out under load: the draft rig waits with `eventually`, whose 3 s deadline a loaded machine's git (worktree add, snapshot, diff) can exceed | `eventually` waits up to 15 s (a passing test never waits for it) | the Phase 7 draft tests under `-race -count=6` with parallel load (Task 10) |
| `FuzzCodexBlock`'s first invariant was wrong (found by fuzzing): the developer's own comment may quote the marker text inside a longer line | the invariant compares whole marker lines; no product change | `FuzzCodexBlock/a763424096dd21b2` (Task 3) |

## Already fixed by a later phase (verified at `dffabbf`)

| Finding (ledger) | Fixed by | Test |
|---|---|---|
| Git config execution in hostile repos (Phase 2 final review): `.git/config` can name `core.fsmonitor`, hooks or transports | Phases 6–8: every git child passes `core.fsmonitor=false`, `core.hooksPath=<null device>`, `protocol.allow=never`, env `GIT_ALLOW_PROTOCOL=none`, `GIT_NO_LAZY_FETCH=1` (`memory.ExecGit`, `toolloop`, `worktree.Git`) | `TestExecGitNeverRunsHooks`, and Task 9's pins `TestExecGitRunsNothingTheRepositoryConfigures`, `TestEveryGitCallDisablesRepositoryPrograms` |
| Unicode bidi controls in model text (an approval-screen spoof) | `textutil.SanitizeModelText` drops U+202A–U+202E, U+2066–U+2069, U+200E/F, U+061C | `TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners` |
| `DeleteDot` leaves ledger/corrupt/proposals (Phase 2 Task 6; the completion note scheduled ledger excerpts for a later phase) | Phase 7 Task 0 | `TestDeleteDotScrubsItsLedgerAndOrphanedProposals`, `TestDeleteDotRemovesItsCorruptAsidesOnly` |
| Memory edit temp file in `os.TempDir` holds plaintext memory (Phase 2 Task 14, flagged) | a private 0700 directory removed with everything the editor leaves | `TestMemoryEditRemovesItsTempDirectory` |
| `CleanEnv` returns nil, so a child inherits the full environment (v1 Task 8, flagged) | `CleanEnv` always returns a non-nil slice | `TestCleanEnvNeverReturnsNil` |
| Index-based card selection (v1 Task 18a), malformed config keeps memory on (Phase 2 Task 5), user-saved findings never suppressing (Phase 2 Task 10) | selection by id; every kill switch fails closed; the Phase 2 final fix wave | `TestSelectionFollowsCardIDAcrossSnapshots`, `TestMalformedConfigKeepsLastGoodButDisablesInject`, Phase 2 final-fix tests |
| Flaky `TestFinishedPaidReviewIsNotDoubleCounted` (Phase 9) | the controller's fix (the frontend's catch-up review raced the ask) | 100/100 runs |

## Known issues (not fixed in 1.0.0-rc.1)

Each is minor for a release; the reason is given.

- **Dots and engine (v1, Phase 2):** a suppressed verdict still sets the cooldown; an edited-then-dismissed card records the edited text; stats can be stale for a moment after memory is switched off at runtime; `r` doesn't reset `MemoryPasses`; with `memory_enabled = false`, attaching still runs git and writes `meta.json` (P2-R3; git runs with repository programs disabled). Reason: cosmetic or self-correcting, no data loss.
- **Engine lifecycle (v1 Task 17):** `lockfile.Held` probes with the real lock (a spurious engine exit is possible if an engine starts during the probe); a crash-looping engine is respawned about every 6 s and the "not ready" error doesn't point at `engine.log`; `e.Wait` doesn't cover in-flight IPC handlers. Reason: rare, recoverable by restarting Herdr; `engine.log` is documented.
- **IPC and the Herdr client (v1 Tasks 5, 15):** no server write deadlines; `Call` and the Herdr client honor a deadline but not cancellation; `Subscribe` returns a wrapped closed-connection error on cancel. Reason: local socket or pipe, one user, every call has a deadline; malformed input is fuzzed (`FuzzIPCDecode`).
- **Transcripts (v1 Tasks 6, 7):** compact-summary and slash-command records may become turns; the Codex payload type mismatch drops the whole record; a rollout glob `*` can span dashes. Reason: fuzzed for crashes; Codex is not live-verified anyway.
- **Verdicts and backends (v1 Tasks 8, 9; ruling P8-R7):** `ParseVerdict`, `ParseReply` and `ParseMemoryOps` take the first `{` to the last `}`, so a reply with prose braces around the JSON is malformed (no card) instead of parsed; `Hint` matches a bare "401"/"login"; `codex turn.failed` after an earlier `agent_message` is ignored. Reason: it fails closed (two objects never parse as one, so no wrong verdict is ever accepted), and the fuzz targets check every accepted reply is sanitized and capped; a string-aware extractor is follow-up work.
- **Queue and state (v1 Tasks 10, 11):** in-memory state changes before the save, with no rollback when the write fails (with Phase 10's read-only gate this is the intended "read-only mode"); `Open` trusts the persisted `next`; the feed loader ignores a scanner error on a truncated last line. Reason: no data loss on disk; a corrupt file is reported.
- **Memory (Phase 2 Tasks 3, 6, 7, 8):** `validEntryID` accepts `m+3`/`m007`; `FirstSentence` cuts at abbreviations; `Similar` is unbounded on very long inputs (texts are capped at 500 runes); the secret scanner errs toward refusing (`Bearer` + prose, `tokenizer=`); no final tie-break in memory block order (prompt-cache misses only); prune can delete archived pinned entries with a zero `UpdatedAt`. Reason: false positives refuse rather than leak; archived entries are already out of the prompt.
- **Memory CLI and UI (Phase 2 Tasks 11–15):** long status lines overflow the viewport; `memory --help` needs the state dir; `M` on an empty library is silent; the chooser can send while disconnected (the call fails visibly); import skip messages print the entry text to the developer's own terminal. Reason: cosmetic.
- **Council (Phase 3):** `ParseDigest` output is not clipped (the card shows it whole; it is sanitized and fuzzed); a negative `max_members` clamps to 2 while 0 means 8; dot names and preflight reasons are rendered as the developer wrote them; `councilConventionText` strips any trailing `(…)`; the chair is charged if shutdown lands mid-chair; one council can overrun the pane budget between preflight and member runs. Reason: developer-controlled input, cosmetic, or bounded by one council.
- **API backends, keys and cost (Phase 4):** `credentials.toml` read-modify-write has no file lock (two `keys set` at the same instant can lose one; the developer reruns it); keys from environment variables are not shape-checked; `Last4` slices bytes (an env key with non-ASCII characters can show a split character); a prices override without `as_of` keeps the embedded date; bad globs and unknown providers in `config.toml` are dropped without a warning; NFC/NFD forms of non-ASCII globs differ; a pane in a repository subdirectory walks the tree without `.gitignore`; the manage popup reads prices once when opened; `CostServed` falls back to the requested model's price when another model served; zero-token failed runs record a known $0; `chairWarned` is set when merge isn't `chair`. Reason: display or accounting edge cases; dollar and call budgets still cap spend.
- **MCP server (Phase 5):** a rejected proposal can be proposed again (no rejection ledger); a project-level `.codex/config.toml` server is not detected and a relative `CODEX_HOME` is not resolved; the manage footer reports install records, not live client configs; a consulted turn doesn't decrement the cooldown; an ask can run while a council starts on the same pane; `Usage.Add` fsyncs under `e.mu`; `paneClosed` matches an error's text suffix; spend after the last dot is detached isn't shown; turn-review budget checks ignore in-flight asks (overshoot of at most one run); full-scope proposals are archived soon after. Reason: the agent can't approve anything itself, budgets cap spend, and the rest is cosmetic.
- **Windows (Phase 6):** a residual race between `Purge` and a concurrent writer; `EINVAL`/`ACCESS_DENIED` opens during `Purge` aren't retried; a reader can recreate a purged project as an empty directory; files created with `CreateFile` inherit the private directory's ACL; stale `herdr-dots-run-*` directories aren't swept at start; an aliased `os/exec` import would slip past the portability guard; CI actions are pinned by major tag and run twice on PR branches. Reason: Windows is beta and not run here (user gate); the private directory protects the files.
- **Write-capable dots (Phase 7):** two engines on one repository drop each other's snapshot refs (by design, X8); an orphaned card whose patch was applied can't be dismissed after a failed card write; `strayOps` can match "index" inside base85 binary data (fails closed, about 1 in 85⁴); the `old mode 120000 → 100644` branch is dead in practice; a stale comment at `patchstore.go:57`; `FlagAgentConfig` lives in a heavier package than it needs; the `ui/diff` sweep test takes about 30 s. Reason: everything fails closed or is cosmetic.
- **Proactive dots (Phase 8):** no cross-session daily reservation (overshoot of at most one run per session); the daily toast is per engine; `route()` has no re-check after `Add`; research runs `--restricted`, which ignores the developer's own `~/.claude` deny rules, and `tool_deny_globs` isn't in the research settings (research still denies every secret file in `toolloop.DefaultDeny`, has no shell and no WebFetch); `charge()` has no generation check; a huge finite budget is accepted; inbox claims compare-and-delete by token (a suspend longer than the TTL duplicates a card, never loses one); corrupt inbox asides aren't cleaned up. Reason: bounded spend, no unapproved action, and research can't send file contents anywhere but a search query.
- **Profiles and sharing (Phase 9):** local dot args can still use `claude` short forms (`-c`, `-r`), combined short flags and flags such as `--chrome`, `--remote-control`, `--teleport`, `--from-pr`, `--file`, `--debug-file`, `--bg`, `--permission-prompts`, and `codex` `--output-schema`/`-i` file reads (only the developer's own dot files can carry them: imported and project dots keep no args but `--model`/`--effort`); the pane and memory screen draw `─` and `▸` without the ASCII fallback; `SetASCII` sets an unlocked package variable; `dots untrust FILE` revokes every checkout (fails closed); a scheduled project dot runs the first checkout's copy only; pruning stops scheduled project dots in idle checkouts (fails safe); `scanSeq` is never cleaned; one full-suite flake was seen once and not identified. From Phase 9's final fix wave (after `dffabbf`): a hostile project file within the size limits still costs about 60 ms per 100 MB to pre-scan (no key or table count cap); the templates' delivery mode isn't pinned by a test; `TestAskCouncilCountsAsOneAsk` flaked once under load (the reconcile pass ran it 150 times under load on `0205616` with Phase 10 applied without a failure; the likely cause is the seat's turn being processed after the first council, which resets the per-turn ask count, the same shape as Phase 9's `TestFinishedPaidReviewIsNotDoubleCounted`: if it recurs, wait in `seat` until that turn is processed). Reason: developer-controlled input, fail-closed behavior, bounded cost or test-only.
- **setup-keys and summon (v1 Tasks 20a, 21):** backup names have 1 s resolution; `Plan` misses a plugin action already bound to another key; a double `enter` in the summon popup can summon twice (one extra budgeted run). Reason: the backup is written before the change and the developer sees the plan first; budgets cap the extra run.
- **Snapshots (Phase 10):** a pane with more than 100 visible cards shows the first 100 (most severe, oldest) and the rest wait unseen until earlier ones are resolved; the pane doesn't yet say how many are hidden.
- **Platforms:** `windows/arm64` is built and published but never tested; Linux and Windows have only CI and cross-compiled tests here (user gate); Codex paths are documented-format, not live-verified (user gate).
```

Then check each "Parked → Phase 10" or deferred line that Phase 9's final fix wave added after `dffabbf` by the same rule, and add it to the right section.

- [ ] **Step 7: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 8: Commit (never push)**

```bash
git add internal/secrets/secrets.go internal/memory/store.go internal/memory/hardening_test.go internal/memory/gitcalls_test.go docs/superpowers/notes/2026-10-08-phase-10-triage.md
git commit -m "fix: invisible-character secret evasion, nested projects; git and bidi pins; triage of every deferred finding (Phase 10)"
```

---

### Task 10: Hardening triage B: memui approve fit, slugs, feed, panics, flaky tests

**Depends on:** Task 3 (it added `internal/dots/fuzz_test.go`, which must still pass with slug validation). Wave 2.

**Files:**
- Create: `internal/delivery/truncate_test.go`, `internal/dots/slug_test.go`, `internal/state/feedmode_test.go`
- Modify: `internal/ui/memui/model.go`, `internal/ui/memui/model_test.go`, `internal/delivery/delivery.go`, `internal/dots/store.go`, `internal/state/feed.go`, `internal/mcpserver/engine_test.go` (`startStub`), `internal/engine/engine_test.go` (`eventually`)

**Interfaces:**
- Consumes: memui's `Model{width, height, approve, mode}`, `approveScreen`, `wrap`, `clean`, `style.Title`, `style.Dim`; `delivery.TruncateRunes`; `dots.Slugify`, `Load`, `Save`, `Delete`, `New` (project dots never reach these: the manage popup and the CLI handle `OriginProject` dots before calling them, and `cmd`'s `loadDot` already checks `memory.ValidSlug`); `state.OpenFeed`, `Feed.Recent`; `ipc.Listen`, `ipc.Serve` (returns after its listener is closed); the engine test rig's `eventually`, which the Phase 7 draft rig (`patchCard`, `draftsDone`, `noDraftsInFlight`, `reviewsDone`) waits with.
- Produces: memui `const approveTail = 3`, `(Model) screenWidth() int`, `(Model) approveFits() bool` (the approve screen plus its 3 tail lines fit `height`; unknown height fits); in confirm mode the list header is dropped, and a popup too small shows "Too small to show the whole proposal (needs N lines, has H). Enlarge the popup to approve." with "any key cancels", and `y` sets status "not approved: the popup is too small to show the whole proposal"; `delivery.TruncateRunes(s, n<=0)` returns `""` when `s` is longer; `var dots.ErrBadSlug`, unexported `validSlug(slug) bool` (not empty, no `/`, `\`, `:` or NUL, and `filepath.IsLocal(slug+".toml")`, which also refuses Windows device names), `Load`/`Delete`/`Save` return `ErrBadSlug` for any other slug (a hand-named `My_Dot.toml` keeps working, so the check is file-name safety, not `Slugify`'s canonical form: Plan decision 31); `OpenFeed` chmods an existing feed with group/other bits to 0600; `Feed.Recent(pane, n<=0)` returns nil.

- [ ] **Step 1: Write the failing tests**

Create `internal/delivery/truncate_test.go`:

```go
package delivery

import "testing"

func TestTruncateRunesNeverPanics(t *testing.T) {
	for _, c := range []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 0, ""}, {"hello", -3, ""}, {"", 0, ""}, {"hello", 1, "…"}, {"héllo", 3, "hé…"}, {"hi", 5, "hi"},
	} {
		if got := TruncateRunes(c.s, c.n); got != c.want {
			t.Errorf("TruncateRunes(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}
```

Create `internal/dots/slug_test.go`:

```go
package dots

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A slug reaches Load, Save and Delete from the manage popup and the CLI; it
// must never name a file outside the dots directory.
func TestSlugsThatAreNotFileNamesAreRefused(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "dots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim.toml")
	if err := os.WriteFile(victim, []byte("name = \"x\"\npurpose = \"p\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"../victim", "", "a/b", `..\victim`, "c:victim"} {
		if _, err := Load(dir, slug); !errors.Is(err, ErrBadSlug) {
			t.Errorf("Load(%q): %v", slug, err)
		}
		if err := Delete(dir, slug); !errors.Is(err, ErrBadSlug) {
			t.Errorf("Delete(%q): %v", slug, err)
		}
		if slug == "" {
			continue // Save makes a slug from the name
		}
		d := New("Fine")
		d.Purpose = "p"
		d.Slug = slug
		if err := Save(dir, d, false); !errors.Is(err, ErrBadSlug) {
			t.Errorf("Save with slug %q: %v", slug, err)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("a file outside the dots directory was touched")
	}
	d := New("Fine Dot")
	d.Purpose = "p"
	if err := Save(dir, d, true); err != nil {
		t.Fatalf("a normal dot must still save: %v", err)
	}
	if _, err := Load(dir, "fine-dot"); err != nil {
		t.Fatalf("and load: %v", err)
	}
	// A file the developer named by hand keeps loading by its own name.
	if err := os.WriteFile(filepath.Join(dir, "My_Dot.toml"), []byte("name = \"Mine\"\npurpose = \"p\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(dir, "My_Dot"); err != nil || got.Name != "Mine" {
		t.Fatalf("a hand-named dot file: %+v %v", got, err)
	}
}
```

Create `internal/state/feedmode_test.go`:

```go
package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenFeedMakesAnOldLooseFileOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits; Windows privacy is the session dir's DACL")
	}
	p := filepath.Join(t.TempDir(), "feed.jsonl")
	if err := os.WriteFile(p, []byte(`{"pane_id":"w1:p1","dot":"security","outcome":"passed"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil { // umask may have narrowed it
		t.Fatal(err)
	}
	f, err := OpenFeed(p)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("feed mode %v, want 0600", st.Mode().Perm())
	}
	if got := f.Recent("w1:p1", 0); got != nil {
		t.Fatalf("Recent(0) = %v", got)
	}
	if got := f.Recent("w1:p1", -1); got != nil {
		t.Fatalf("Recent(-1) = %v", got)
	}
	if got := f.Recent("w1:p1", 5); len(got) != 1 {
		t.Fatalf("Recent(5) = %v", got)
	}
}
```

Apply to `internal/ui/memui/model_test.go` (the end of `TestApproveShowsTheFullProposalAndNeedsY`: a too-small popup now refuses instead of overflowing):

```diff
--- a/internal/ui/memui/model_test.go
+++ b/internal/ui/memui/model_test.go
@@ -450,10 +450,21 @@ func TestApproveShowsTheFullProposalAndNeedsY(t *testing.T) {
 	if strings.Contains(m.View(), "y approve") {
 		t.Fatalf("esc closes the confirm screen:\n%s", m.View())
 	}
-	// A short popup never cuts the proposal's text.
+	// A popup too short for the whole proposal refuses to approve (P2-R9):
+	// the terminal would cut the top lines (who proposed it, kind, paths).
 	short, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 8})
-	if got := strings.Join(strings.Fields(press(short, "a").View()), " "); !strings.Contains(got, long) {
-		t.Fatalf("a short screen cut the proposal:\n%s", press(short, "a").View())
+	sv := press(short, "a").View()
+	if !strings.Contains(sv, "Too small to show the whole proposal") || strings.Contains(sv, "y approve") || strings.Count(sv, "\n") >= 8 {
+		t.Fatalf("a too-small popup must say so and fit:\n%s", sv)
+	}
+	press(short, "a", "y")
+	if e, _ := st.Get(pid, ref); e.Status != memory.StatusProposed || n != 0 {
+		t.Fatal("y in a popup too small for the proposal must not approve")
+	}
+	// Enlarging the popup while the confirm screen is open shows it whole.
+	grown, _ := press(short, "a").Update(tea.WindowSizeMsg{Width: 60, Height: 40})
+	if got := strings.Join(strings.Fields(grown.View()), " "); !strings.Contains(got, long) || !strings.Contains(grown.View(), "y approve") {
+		t.Fatalf("after growing, the whole proposal shows:\n%s", grown.View())
 	}
 	press(m, "a", "y")
 	if e, _ := st.Get(pid, ref); e.Status != memory.StatusActive || n != 1 {
```

- [ ] **Step 2: Run them to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/delivery/ ./internal/dots/ ./internal/state/ ./internal/ui/memui/ 2>&1 | grep -E '^(--- FAIL|ok|FAIL|panic)'`
Expected: `panic: runtime error: slice bounds out of range` in `TestTruncateRunesNeverPanics` (delivery FAIL), `undefined: ErrBadSlug` (dots FAIL to build), `feed mode -rw-r--r--, want 0600` or a panic for `Recent(-1)` (state FAIL), and `a too-small popup must say so and fit` (memui FAIL).

- [ ] **Step 3: The approve screen fits or refuses**

Apply to `internal/ui/memui/model.go`:

```diff
--- a/internal/ui/memui/model.go
+++ b/internal/ui/memui/model.go
@@ -376,6 +376,10 @@ func (m Model) keysConfirmApprove(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 		m.status = "not approved"
 		return m, nil
 	}
+	if !m.approveFits() {
+		m.status = "not approved: the popup is too small to show the whole proposal"
+		return m, nil
+	}
 	e := m.approve
 	ref, pid, seen := e.Ref(), m.pidOf(e), e.Text
 	return m, m.mutate("approved "+ref.String(), func(st *memory.Store) error {
@@ -418,6 +422,26 @@ func wrap(s string, w int) []string {
 	return lines
 }
 
+// approveTail is the lines View adds under the approve screen: a blank line,
+// the y prompt and the footer.
+const approveTail = 3
+
+// screenWidth is the width View lays out for.
+func (m Model) screenWidth() int {
+	if m.width <= 0 {
+		return 80
+	}
+	return m.width
+}
+
+// approveFits reports whether the whole approve screen fits the popup. A
+// taller view would have its top lines (who proposed it, the kind, the
+// paths) cut by the terminal, so approving is refused until it fits. An
+// unknown height (no WindowSizeMsg yet) fits.
+func (m Model) approveFits() bool {
+	return m.height <= 0 || len(m.approveScreen(m.screenWidth()))+approveTail <= m.height
+}
+
 // approveScreen is the full proposal: who proposed it, its kind, its paths
 // and every word of its text.
 func (m Model) approveScreen(w int) []string {
@@ -687,10 +711,7 @@ func (m Model) list(w int) (lines []string, cur int) {
 }
 
 func (m Model) View() string {
-	w := m.width
-	if w <= 0 {
-		w = 80
-	}
+	w := m.screenWidth()
 	var head strings.Builder
 	who := clean(m.d.DotName)
 	if who == "" {
@@ -721,7 +742,11 @@ func (m Model) View() string {
 		sc, _, _, want := m.forgetAllTarget()
 		tail.WriteString(fmt.Sprintf("\n%s Type %q and press enter:\n%s\n", style.Title.Render("Forget ALL "+string(sc)+" memory?"), want, m.input.View()))
 	case modeConfirmApprove:
-		tail.WriteString("\ny approve · any other key cancels\n")
+		if m.approveFits() {
+			tail.WriteString("\ny approve · any other key cancels\n")
+		} else {
+			tail.WriteString("\nany key cancels\n")
+		}
 	case modeFilter:
 		tail.WriteString("\n/" + m.input.View() + "\n")
 	case modeEdit:
@@ -747,7 +772,15 @@ func (m Model) View() string {
 
 	lines, cur := m.list(w)
 	if m.mode == modeConfirmApprove {
+		// The approve screen replaces the header: it has its own title, and
+		// every line of the popup is needed for the proposal.
+		head.Reset()
 		lines, cur = m.approveScreen(w), 0
+		if !m.approveFits() {
+			need := len(lines) + approveTail
+			lines = append([]string{style.Title.Render("APPROVE " + clean(m.approve.Ref().String()) + "?")},
+				wrap(fmt.Sprintf("Too small to show the whole proposal (needs %d lines, has %d). Enlarge the popup to approve.", need, m.height), w)...)
+		}
 	} else if len(m.rows) == 0 && m.loadErr == "" {
 		lines = []string{style.Dim.Render("No memory here yet. Press n to add an entry.")}
 	}
```

- [ ] **Step 4: Slugs, the feed and the two panics**

Apply to `internal/delivery/delivery.go`:

```diff
--- a/internal/delivery/delivery.go
+++ b/internal/delivery/delivery.go
@@ -79,6 +79,9 @@ func TruncateRunes(s string, n int) string {
 	if len(r) <= n {
 		return s
 	}
+	if n <= 0 {
+		return ""
+	}
 	return string(r[:n-1]) + "…"
 }
 
```

Apply to `internal/dots/store.go` (`Load`, `Save` and `Delete`; Phase 9's project dots load through `DecodeProject` and never reach them):

```diff
--- a/internal/dots/store.go
+++ b/internal/dots/store.go
@@ -21,6 +21,19 @@ import (
 
 var ErrExists = errors.New("a dot with this name already exists")
 
+// ErrBadSlug refuses a slug that is not a plain file name in the dots
+// directory ("../x", "a/b", ""). Slugs reach Load, Save and Delete from the
+// manage popup and the CLI.
+var ErrBadSlug = errors.New("invalid dot name")
+
+// validSlug reports a slug that names a file directly inside the dots
+// directory: no separator, no NUL, no drive or stream colon, and a local
+// file name (filepath.IsLocal also refuses Windows device names). Files the
+// developer named by hand (My_Dot.toml) keep working.
+func validSlug(slug string) bool {
+	return slug != "" && !strings.ContainsAny(slug, "/\\:\x00") && filepath.IsLocal(slug+".toml")
+}
+
 type LoadError struct {
 	File string // base name, e.g. "security.toml"
 	Line int    // 0 when not a parse error
@@ -85,6 +98,9 @@ func LoadAll(dir string) ([]Dot, []LoadError) {
 }
 
 func Load(dir, slug string) (Dot, error) {
+	if !validSlug(slug) {
+		return Dot{}, fmt.Errorf("%w: %q", ErrBadSlug, slug)
+	}
 	d, err := decode(path(dir, slug))
 	if err != nil {
 		return Dot{}, err
@@ -101,6 +117,9 @@ func Save(dir string, d Dot, create bool) error {
 	if create || d.Slug == "" {
 		d.Slug = Slugify(d.Name)
 	}
+	if !validSlug(d.Slug) {
+		return fmt.Errorf("%w: %q", ErrBadSlug, d.Slug)
+	}
 	p := path(dir, d.Slug)
 	if create {
 		if _, err := os.Stat(p); err == nil {
@@ -265,4 +284,9 @@ func insertPurpose(tomlStr, purpose string) string {
 	return strings.Join(append(lines, PurposeBlock(purpose)), "\n")
 }
 
-func Delete(dir, slug string) error { return os.Remove(path(dir, slug)) }
+func Delete(dir, slug string) error {
+	if !validSlug(slug) {
+		return fmt.Errorf("%w: %q", ErrBadSlug, slug)
+	}
+	return os.Remove(path(dir, slug))
+}
```

Apply to `internal/state/feed.go` (the two hunks are in `OpenFeed` and `Recent`):

```diff
--- a/internal/state/feed.go
+++ b/internal/state/feed.go
@@ -69,6 +69,11 @@ func OpenFeed(path string) (*Feed, error) {
 	if st, err := os.Stat(path); err == nil && st.Size() > feedRotateBytes {
 		_ = os.Rename(path, path+".1")
 	}
+	// The feed quotes model output: an older file made with a looser mode
+	// becomes owner-only (O_CREATE's 0600 only applies to new files).
+	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
+		_ = os.Chmod(path, 0o600)
+	}
 	file, err := os.Open(path)
 	if err == nil {
 		sc := bufio.NewScanner(file)
@@ -116,6 +121,9 @@ func (f *Feed) Append(e FeedEntry) error {
 
 // Recent returns up to n entries for a pane, oldest first.
 func (f *Feed) Recent(paneID string, n int) []FeedEntry {
+	if n <= 0 {
+		return nil
+	}
 	f.mu.Lock()
 	defer f.mu.Unlock()
 	list := f.recent[paneID]
```

- [ ] **Step 5: Run the tests**

Run: `GOTOOLCHAIN=local go test -race -count=1 ./internal/delivery/ ./internal/dots/ ./internal/state/ ./internal/ui/... ./internal/engine/ ./cmd/herdr-dots/ 2>&1 | tail -12`
Expected: every package `ok`, including `TestTruncateRunesNeverPanics`, `TestSlugsThatAreNotFileNamesAreRefused`, `TestOpenFeedMakesAnOldLooseFileOwnerOnly`, the extended `TestApproveShowsTheFullProposalAndNeedsY`, `TestSelectedRowStaysOnScreenInAShortPopup`, and Task 3's `FuzzDotFile`/`FuzzDecodeStrict` seeds.

- [ ] **Step 6: Stabilize the two known flaky tests**

The ledgers name two (Plan decision 32). `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies` (Phase 6, again after Phase 8's merge): `startStub`'s stop only cancelled `ipc.Serve`, which closes its listener on its own goroutine, and a Unix listener unlinks its socket path when it closes; a late close could unlink the socket of the stub the test had just started again, so the next dial failed. The Phase 7 draft tests under load: the draft rig waits with `eventually`, whose 3 s deadline a loaded machine's git can exceed. Neither change touches product code.

Apply to `internal/mcpserver/engine_test.go`:

```diff
--- a/internal/mcpserver/engine_test.go
+++ b/internal/mcpserver/engine_test.go
@@ -37,6 +37,11 @@ func shortSock(t *testing.T) string {
 	return platformtest.Endpoint(t)
 }
 
+// startStub serves a stub engine at path. Its stop function returns only
+// once Serve has returned, so the old listener is closed (and, on Unix, its
+// socket file unlinked) before the test goes on: a listener closed late
+// would unlink the socket of the engine started after it (the flake seen in
+// Phases 6 and 8).
 func startStub(t *testing.T, path string) context.CancelFunc {
 	t.Helper()
 	ln, err := ipc.Listen(path)
@@ -44,8 +49,9 @@ func startStub(t *testing.T, path string) context.CancelFunc {
 		t.Fatal(err)
 	}
 	ctx, cancel := context.WithCancel(context.Background())
-	go ipc.Serve(ctx, ln, stubEngine{pane: "w1:p1"})
-	return cancel
+	done := make(chan struct{})
+	go func() { _ = ipc.Serve(ctx, ln, stubEngine{pane: "w1:p1"}); close(done) }()
+	return func() { cancel(); <-done }
 }
 
 // Review Focus 3: the engine isn't running when the agent first calls a tool.
```

Apply to `internal/engine/engine_test.go`:

```diff
--- a/internal/engine/engine_test.go
+++ b/internal/engine/engine_test.go
@@ -190,9 +190,13 @@ func newRigFull(t *testing.T, opt func(*Deps), eng func(*Engine), ds ...dots.Dot
 	return r
 }
 
+// eventually waits for cond. The deadline is generous because drafts and
+// proactive runs shell out to git, which is slow on a loaded machine (the
+// Phase 7 draft tests timed out under load at 3 s); a passing test never
+// waits for it.
 func eventually(t *testing.T, what string, cond func() bool) {
 	t.Helper()
-	deadline := time.Now().Add(3 * time.Second)
+	deadline := time.Now().Add(15 * time.Second)
 	for time.Now().Before(deadline) {
 		if cond() {
 			return
```

Run: `GOTOOLCHAIN=local go test -race -count=50 -run TestIPCEngine ./internal/mcpserver/ && (GOTOOLCHAIN=local go test -race -count=3 ./internal/worktree/ ./internal/ui/diff/ >/dev/null & GOTOOLCHAIN=local go test -race -count=6 -run 'Draft|draft' ./internal/engine/; wait)`
Expected: `ok` for both (the second runs the draft tests six times while other packages load the machine; the reconcile pass ran both green).

- [ ] **Step 7: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 8: Commit (never push)**

```bash
git add internal/ui/memui/model.go internal/ui/memui/model_test.go internal/delivery/delivery.go internal/delivery/truncate_test.go internal/dots/store.go internal/dots/slug_test.go internal/state/feed.go internal/state/feedmode_test.go internal/mcpserver/engine_test.go internal/engine/engine_test.go
git commit -m "fix: memui approves only a proposal it shows whole; dot slugs stay file names; owner-only feed; two panics; two flaky tests (Phase 10)"
```

---

### Task 11: Threat model, security review, `SECURITY.md`

**Depends on:** Tasks 6–10 (the code the review reads is final except for its own fixes). Wave 3.

**Files:**
- Create: `docs/security/threat-model.md`, `docs/security/2026-10-08-security-review.md`, `SECURITY.md`
- Modify: product files that a High or Critical finding points at (never `README.md`, `docs/guide/*`, `CHANGELOG.md` or `CONTRIBUTING.md`, which Task 12 owns)

**Interfaces:**
- Consumes: the whole tree; test names from Phases 2–10 (the table cites them; Step 2 checks each exists).
- Produces: `docs/security/threat-model.md` (every threat → mitigation → the test that pins it, or a named gap); `docs/security/2026-10-08-security-review.md` (findings, severity, disposition); `SECURITY.md` (how to report, supported versions, the safety model in brief). Task 12 links all three from the README; Task 13's acceptance notes cite them.

- [ ] **Step 1: Write the threat model**

Create `docs/security/threat-model.md`:

```markdown
# Herdr Dots threat model (1.0.0-rc.1)

Scope: the plugin binary (`herdr-dots`), its engine, panes and popups, the
MCP server, the install scripts and the release pipeline. Out of scope: Herdr
itself, Claude Code, Codex, the model providers, and the developer's own
machine account (anything running as the developer can already read every
file the plugin can).

## Assets

- The developer's source code and the coding agent's conversation (transcripts).
- API keys (`credentials.toml`, environment variables).
- Dot memory: project conventions, findings, dismissed findings, preferences.
- The developer's working tree, index, branches and git history.
- Herdr, Claude Code and Codex configuration files (keybindings, MCP entries).
- Money: provider spend under budgets.

## Trust boundaries

1. **Model output → plugin.** Every model reply is untrusted text: verdicts,
   memory ops, chair digests, council stances, patches, research digests.
2. **Repository → plugin.** A cloned repository is untrusted: its files, its
   `.git/config`, its project dots (`.herdr-dots/dots/`), its transcripts.
3. **Shared files → plugin.** Imported dot packs and project dots come from
   other people.
4. **Local processes → engine.** The engine socket or pipe, the state dir.
5. **Network → install.** The release download and the GitHub release.

## Threats, mitigations and tests

| # | Threat | Mitigation | Pinned by |
|---|---|---|---|
| T1 | Prompt injection in a transcript or a repository file makes a dot run tools that change things | Dots are read-only: claude `--tools Read,Grep,Glob` with hooks disabled, codex `--sandbox read-only`, API dots get a jailed read-only tool loop; denied backend args are dropped; write-capable dots (Phase 7) need two explicit approvals and never get a shell | `TestClaudeBuildCmdIsReadOnlyAndHookless`, `TestClaudeBuildCmdNeverEmitsDeniedArgs`, `TestCodexBuildCmdNeverEmitsDeniedArgs`, `TestValidateRejectsUnsafeBackendArgs`, `TestSafeArgsDropsDeniedFlagsAndTheirValues`, `TestResolveInRootRefusesEscapes`, `TestNoExistenceOracleOutsideRoot`, `TestDefaultDenyCoversCommonCredentialFiles`, `TestClaudeDraftGetsFileToolsAndNoShell`, `TestWriteToolsNeedBothWriteAndPassWrite`, `TestApplyPatchRefusesAPatchThatIsNotTheOneReviewed` |
| T2 | Model output with terminal escapes or bidi controls rewrites the screen, the clipboard, or makes approved text read differently | `SanitizeModelText` drops C0/C1 controls and bidi controls before display, storage and delivery | `TestSanitizeModelTextDropsControlsKeepsNewlineTab`, `TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners`, `FuzzDiffViewParse`, `FuzzParseCLIOutput`, `TestFeedSanitizesDetail`, `TestNamesAreSanitizedOnScreen`, `FuzzParseVerdict`, `FuzzParseDigest` |
| T3 | Injected text becomes shared project memory ("always disable CSRF") | Model-written project memory waits as a proposal until the developer approves it seeing the full text; models can't write dismissed, preference or global entries, nor edit user or other dots' entries; memory goes only in the user prompt, framed as reference data | `TestApplyAddsDotActiveAndProjectProposed`, `TestApplyRefusesWhatModelsMayNotWrite`, `TestModelNeverRewritesDeveloperWording`, `TestUserPromptPutsMemoryBeforeConversation`, `TestApproveShowsTheFullProposalAndNeedsY` |
| T4 | A model suppresses future findings by writing its own "dismissed" entries | Only outcome and user findings suppress repeats | `TestOutcomeMergePromotesModelFinding` and the Phase 2 final-fix suppression tests |
| T5 | Approving something the developer didn't see (stale selection, clipped popup) | Selection by id; approve/pin refuse text that changed on disk; the approve screen refuses when the popup can't show it whole | `TestSelectionFollowsCardIDAcrossSnapshots`, `TestApproveAndPinRefuseTextThatChangedOnDisk`, `TestApproveShowsTheFullProposalAndNeedsY` |
| T6 | Secrets end up in memory, the ledger, history, exports or packs | The secret scanner (`internal/secrets`) refuses model writes and asks before user writes; ledger lines and history excerpts that look like secrets are redacted; invisible characters can't split a key; packs never include credentials and are scanned (Phase 9) | `TestScanSecretsFindsCredentials`, `TestRecordOutcomeNeverStoresSecrets`, `TestInvisibleCharactersDoNotHideASecret`, `TestSecretNeedsASecondSave`, `FuzzScanSecrets`, `TestExportRefusesSecretsUnlessForced`, `TestExportRefusesDotSecretsEvenWhenForced` |
| T7 | API keys leak to logs, the feed, snapshots, IPC, argv or errors | `credentials.toml` 0600 (DACL on Windows) or env indirection; `credentials.Redact` on every error path; keys never on argv | `TestPermissiveFileIsRefusedWithoutReadingIt`, `TestMalformedFileErrorNeverQuotesTheKey`, `TestEntryNeverFormatsItsKey`, `TestRedactMasksWholeAuthorizationValues`, `TestBrokenCredentialsAreReportedWithoutTheirText`, `FuzzCredentialsFile` |
| T8 | A hostile repository runs code through git (`core.fsmonitor`, hooks, transports, lazy fetches) | Every git call passes `core.hooksPath` to the null device, `core.fsmonitor=false` and `protocol.allow=never`, with `GIT_ALLOW_PROTOCOL=none` and `GIT_NO_LAZY_FETCH=1`, and filters `GIT_*` overrides; drafts also turn off filters and submodule recursion | `TestExecGitRunsNothingTheRepositoryConfigures`, `TestEveryGitCallDisablesRepositoryPrograms`, `TestExecGitNeverRunsHooks`, `TestResolveProjectIgnoresInheritedGitEnv` |
| T9 | Repository project dots or imported packs smuggle risky capabilities | Project dots need a content-hashed trust; a changed file revokes trust; risky capabilities are clamped on import | `TestAnyEditRevokesTrustAndChangesTheStamp`, `TestUntrustedFileIsClampedAndListsWhatItAsks`, `TestCorruptTrustFileFailsClosedAndIsNeverOverwritten`, `TestClampDotTurnsEveryRiskyCapabilityOff`, `FuzzDotpackParse`, `FuzzDecodeStrict` |
| T10 | A dot name or slug from the popup, the CLI, IPC or MCP names a file outside the dots dir | `dots.Load/Save/Delete` refuse slugs `Slugify` couldn't make; memory validates slugs and project ids before any path | `TestSlugsThatAreNotFileNamesAreRefused`, `TestUnsafeIDsAndSlugsAreRefused` |
| T11 | Another local user talks to the engine or reads state | Socket in a 0700 directory with a 0600 socket (Unix); a per-user DACL'd named pipe with an owner check (Windows); state files 0600, dirs 0700; the feed is tightened to 0600 | `TestEnsureCreatesPrivateDirs`, `TestFilesArePrivate`, `TestOpenFeedMakesAnOldLooseFileOwnerOnly`, `TestPipeOwnerError`, `TestCheckPrivateEntries` |
| T12 | Malformed IPC input crashes or hangs the engine | Each connection decodes JSON into typed structs; unknown commands fail | `FuzzIPCDecode` |
| T13 | A dot's delivered message re-triggers the same dot (loops, runaway spend) | Loop guard on the delivery header; inject never follows a dot-started turn; councils and consulted dots skip the next turn | `TestLoopGuardSkipsTurnStartedByThisDot`, `TestInjectForcedToQueueForDotStartedTurns`, `TestConsultedDotSkipsTheNextTurnOnly`, `FuzzFromDots` |
| T14 | Cost abuse: a chatty model, a recursion through MCP, background research | Call and dollar budgets per dot and per session, ask limits per turn, a global daily budget for background work, MCP tools refuse inside dot runs | `TestBudgetStopsDotAndWarnsOnce`, `TestPlanRefusesWhenThePaneBudgetIsShort`, `TestDollarBudgetStopsTheDot`, `TestAskLimiterPerMinuteWindow`, `TestAskChargesDollarsAndRespectsTheDollarBudget`, `TestDailyBudgetStopsBackgroundRunsUntilMidnight`, `TestDecideBackgroundNeverUnlimited`, `TestEveryToolRefusesOutsideHerdr` |
| T15 | Inject types into the agent while the developer is typing | Inject only when the agent is idle and the pane isn't focused; a runtime kill switch that fails closed | `TestInjectWaitsWhileFocusedThenFlushes`, `TestInjectKillSwitchAppliesAtRuntime`, `TestMalformedConfigKeepsLastGoodButDisablesInject`, `FuzzConfigLoad` |
| T16 | A write-capable dot's patch escapes the worktree or touches `.git/` | Patch validation (no `.git/`, no escapes, size caps), apply only when the agent is idle, never stage or commit | `TestEscapesRefusesNTFSAliasesOfDotGit`, `TestValidateRefusesARenameOrCopyThatHidesASourcePath`, `TestACommittedSymlinkCannotBeRetargetedByAForgedDiff`, `TestAnApplyWithoutTheReviewedHashIsRefused`, `FuzzDiffHeaders` |
| T17 | A tampered or substituted release binary | `build.sh`/`build.ps1` verify the archive against `release/SHA256SUMS` from the cloned tree and never run an unverified binary; the publish job refuses archives that differ from the committed checksums; cosign keyless signatures, checked by `verify-release` | `TestChecksumMismatchFallsBackToASourceBuild`, `TestArchiveWithoutTheBinaryIsRejected`, `TestVerifyReleaseRefusesATamperedArchive`, `TestVerifyReleaseRefusesAReplacedBinary`, `TestVerifyReleaseRunsCosignWithTheWorkflowIdentity` |
| T18 | Vulnerable dependencies | Few, pinned modules; `govulncheck` in CI and before a release | the `vulncheck` CI job; Task 11 Step 3 result |
| T19 | Files from a newer version are overwritten by an older binary (data loss after a downgrade) | `migrate.Run` marks them read-only; `WriteFileAtomic` refuses them; migrations back up first and run under the file's lock | `TestNewerFileIsReadOnlyAndNeverRewritten`, `TestFutureQueueIsReadButNeverRewritten`, `TestWritingCommandsProtectNewerFiles`, `TestLockedFileWaitsThenFailsWithoutChange` |
| T20 | Uninstall deletes the developer's data or config by mistake | Backups before every config edit; only herdr-dots bindings removed; `--purge` needs a typed phrase, refuses while an engine runs, and refuses home, root and short paths | `TestRemoveBlockKeepsEverythingElseByteForByte`, `TestPurgeNeedsTheTypedPhraseEvenWithYes`, `TestPurgeRefusesWhileAnEngineRuns`, `TestSafeToPurgeRefusesHomeRootAndRelativePaths` |
| T21 | Telemetry or unexpected network traffic | None: the plugin talks only to the backends the developer configures, Herdr's local socket, and GitHub during install | code review (Task 11 Step 3) |

## Known gaps

- Homoglyph secrets (Cyrillic look-alikes) and secrets split by spaces pass the scanner.
- The memory block and dot purposes are prompts: a determined injection can still make a dot *say* something wrong. Dots can't act on it (read-only), and memory written from it waits for approval.
- `lockfile.Held` probes with the real lock (see the triage notes).
- Research runs don't apply the developer's `tool_deny_globs` or their own `~/.claude` deny rules (`--restricted`); the plugin's secret-file deny list still applies, and research has no shell and no WebFetch.
- A dot file the developer writes can still pass some `claude`/`codex` flags the denylist doesn't name (short forms, combined flags; see the triage notes). Imported and project dots keep no args but `--model`/`--effort`.
- Linux and Windows privacy (modes, DACLs, pipes) is tested in CI only; not run on those systems here.
```

- [ ] **Step 2: Check every test the table cites exists**

Run: `for t in $(grep -o 'Test[A-Za-z0-9]*\|Fuzz[A-Za-z0-9]*' docs/security/threat-model.md | sort -u); do grep -rq "func $t(" --include='*_test.go' . || echo "missing: $t"; done`
Expected: no output (the reconcile pass checked every name in the table against `dffabbf` plus Tasks 2–10). For a `missing:` line (a name Phase 9's final fix wave renamed), find the real name (`grep -rn '<keyword>' --include='*_test.go' .`) and fix the table. If no test pins a mitigation, add the row to "Known gaps" instead of citing nothing.

- [ ] **Step 3: Run the security review and govulncheck**

Run: `GOTOOLCHAIN=local go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./... 2>&1 | tail -20`
Expected: `No vulnerabilities found.` If a vulnerability is reported in a dependency, bump that module (`go get <module>@<fixed version> && go mod tidy`) and rerun; if the tool can't be downloaded (offline) or doesn't build with Go 1.25, try `@latest`, and if that fails too, record "govulncheck: not run (reason)" in the review notes and in the ready-to-publish checklist as a user gate.

Then run the Claude Code `security-review` skill over the whole repository (not only the branch diff), and read these files yourself: `internal/backend/claude.go`, `codex.go`, `exec.go`, `args` handling in `internal/dots/args.go`, `internal/backend/toolloop/*`, `internal/credentials/*`, `internal/secrets/secrets.go`, `internal/memory/apply.go`, `importpack.go`, `project.go`, `internal/engine/deliver.go`, `draft.go`, `projectdots.go`, `internal/ipc/ipc.go`, `internal/mcpserver/*`, `internal/worktree/*`, `internal/projectdots/*`, `internal/dotpack/*`, `internal/history/history.go`, `internal/migrate/*`, `cmd/herdr-dots/uninstall.go`, `cmd/herdr-dots/verifyrelease.go`, `scripts/build.sh`, `scripts/build.ps1`, `.github/workflows/release.yml`.

Create `docs/security/2026-10-08-security-review.md` with this structure and one row per finding (the skill's findings plus yours):

```markdown
# Security review, 2026-10-08 (1.0.0-rc.1)

Method: the Claude Code `security-review` skill over the whole tree, a human
read of the files in Task 11 Step 3, and govulncheck.

govulncheck: (result line, or "not run: <reason>")

| # | Finding | Where | Severity | Disposition |
|---|---|---|---|---|
| R1 | (one line) | path:line | Critical / High / Medium / Low | fixed in <commit subject> / known issue (reason) |

High and Critical findings are fixed in this task with a test each; the
others are listed in the triage notes' known issues.
```

Fix every High and Critical finding now, each with a test in its package, and rerun the verification gate after each fix.

- [ ] **Step 4: `SECURITY.md`**

Create `SECURITY.md`:

```markdown
# Security policy

## Reporting a vulnerability

Please report security problems privately, not in public issues: use GitHub's
"Report a vulnerability" (Security tab) on this repository. Include the
version (`herdr-dots --version`), your OS, and steps to reproduce. You can
expect an acknowledgement within a week.

## Supported versions

Only the newest release gets fixes. `1.0.0-rc.1` is a release candidate and
has not been released.

## How Herdr Dots keeps you safe

- **Dots are read-only.** Claude Code dots run with read-only tools and hooks
  disabled, Codex dots in its read-only sandbox, API dots with a jailed
  read-only tool loop. Write-capable dots (off by default) work in a separate
  worktree and need two approvals; they never get a shell.
- **Nothing reaches your agent without you,** unless you choose inject mode,
  which only types when the agent is idle and you're not in the pane.
- **Model output is untrusted:** control and bidi characters are stripped
  before anything is shown, stored or sent; model-written project memory waits
  for your approval with the full text on screen.
- **Secrets:** keys live in `credentials.toml` (owner-only) or environment
  variables and never appear in logs, the feed, snapshots or errors; memory
  refuses text that looks like a secret.
- **Repositories are untrusted:** git runs with hooks, fsmonitor and lazy
  fetches disabled; project dots need your trust, and a changed file loses it.
- **Installs are verified:** prebuilt binaries are checked against the
  checksums committed in the repository; otherwise the plugin builds from
  source. `herdr-dots verify-release` re-checks an install (and its cosign
  signature when cosign is installed).
- **No telemetry.**

The full threat model is in [docs/security/threat-model.md](docs/security/threat-model.md).
```

- [ ] **Step 5: Verification gate**

Run the commands of "The verification gate".
Expected: `gofmt` prints nothing; the others succeed.

- [ ] **Step 6: Commit (never push)**

```bash
git add docs/security SECURITY.md
git add -u   # only product files a High/Critical fix touched, with their tests
git commit -m "docs(security): threat model, security review and SECURITY.md (Phase 10)"
```

---

### Task 12: User docs: README, guide, CHANGELOG, CONTRIBUTING

**Depends on:** Tasks 6–10 (commands and behavior are final; the triage notes exist). Wave 3.

**Files:**
- Create: `docs/guide/README.md`, `docs/guide/dots.md`, `docs/guide/delivery.md`, `docs/guide/memory.md`, `docs/guide/council.md`, `docs/guide/backends.md`, `docs/guide/mcp.md`, `docs/guide/windows.md`, `docs/guide/write-capable.md`, `docs/guide/proactive.md`, `docs/guide/profiles-and-sharing.md`, `docs/guide/privacy-and-safety.md`, `docs/guide/install-and-upgrade.md`, `CHANGELOG.md`, `CONTRIBUTING.md`
- Modify: `README.md` (restructured)

**Interfaces:**
- Consumes: the README sections Phases 1–9 wrote (moved, not rewritten), Task 9's triage notes (known issues), Task 11's `SECURITY.md` and threat model (linked; Task 11 runs in the same wave, so link the paths whether or not they're merged yet).
- Produces: README: the title, a status line, an intro paragraph, then `## Status`, `## Requirements`, `## Install`, `## Quick start`, `## Guide`, `## Privacy and safety`, `## Uninstall`, `## Known issues`, `## Roadmap history`, `## License`; the guide pages; `CHANGELOG.md` with `1.0.0-rc.1` and `0.1.0`–`0.9.0`; `CONTRIBUTING.md` with the schema rules. Task 13 rewrites only the `## Status` block.

- [ ] **Step 1: Move the feature sections into the guide**

For each README section Phases 1–9 wrote, move its text (verbatim, then fix headings to start at `##` and fix relative links) into the guide page below. Keep anything that doesn't fit a row in the README.

| README section at `dffabbf` (heading as Phases 1–9 wrote it) | Guide page |
|---|---|
| `## Using it` (its intro) and `### Delivery modes` | `docs/guide/delivery.md` (the dot editor parts into `dots.md`) |
| `## Keybindings`, `## Files` | `docs/guide/dots.md` |
| `### Dot memory` | `docs/guide/memory.md` |
| `### Council mode` | `docs/guide/council.md` |
| `### Backends and keys` | `docs/guide/backends.md` |
| `### Dots MCP server: let the main agent ask the dots` | `docs/guide/mcp.md` |
| `## Windows (beta)` | `docs/guide/windows.md` |
| `### Write-capable dots (drafts)` | `docs/guide/write-capable.md` |
| `### Proactive dots (0.8.0)` | `docs/guide/proactive.md` (drop the version from the heading) |
| `### Profiles, history and sharing (0.9.0)` | `docs/guide/profiles-and-sharing.md` (drop the version from the heading) |
| `## Privacy and safety` (every bullet, verbatim) | `docs/guide/privacy-and-safety.md`; the README keeps the short list below and links it |
| `## Known gaps in this scaffold` | merged into the README's `## Known issues` (the live-verification gaps stay there, worded without "scaffold") |
| `## Roadmap` | the README's `## Roadmap history` table |

Start each guide page with a one-line title and one sentence saying what the feature is, then the moved text. Create `docs/guide/install-and-upgrade.md` with:

```markdown
# Install and upgrade

## From the Herdr marketplace

    herdr plugin install <owner>/herdr-dots

Herdr clones the repository and runs `scripts/build.sh` (`build.ps1` on
Windows). The script downloads the prebuilt binary for your OS and CPU from
the GitHub release, checks it against `release/SHA256SUMS` in the clone, and
installs it in `bin/`. With no release for this version, no network, or a
checksum that doesn't match, it builds from source with Go 1.25 or newer;
without Go it stops and says what to do. Nothing half-installed is left
behind. Set `HERDR_PLUGIN_BUILD_OFFLINE=1` to skip the download.

Check an install: `./bin/herdr-dots --version` says `release` or `source`
and the commit; `./bin/herdr-dots verify-release` re-checks the archive, the
binary and (with cosign installed) the release signature.

Targets: macOS and Linux on amd64 and arm64, Windows on amd64; Windows arm64
binaries are built but not tested.

## From source (development)

    git clone <repository> && cd herdr-dots
    make build
    herdr --session <a test session> plugin link .

`plugin link` runs no build step, so run `make build` after every change.

## Upgrading

Herdr has no `plugin update`: reinstall the plugin. Your dots, settings,
credentials, memory and history live in the plugin's config and state
directories and are kept. At start the engine checks every file's schema
version; older files are upgraded (the original is kept as
`<file>.bak-v<old>`), and a file written by a newer herdr-dots is left
read-only with a notice. `herdr-dots migrate --dry-run` shows what it would
do.
```

Create `docs/guide/README.md`:

```markdown
# Herdr Dots guide

- [Install and upgrade](install-and-upgrade.md)
- [Dots: create, edit, attach](dots.md)
- [Delivery modes](delivery.md)
- [Dot memory](memory.md) (the heart of the product)
- [Council mode](council.md)
- [Backends, keys and cost](backends.md)
- [MCP server](mcp.md)
- [Windows](windows.md)
- [Write-capable dots](write-capable.md)
- [Proactive dots](proactive.md)
- [Profiles, history and sharing](profiles-and-sharing.md)
- [Privacy and safety](privacy-and-safety.md) (every safety rule in detail)
```

- [ ] **Step 2: Rewrite the README around the guide**

Replace the top of `README.md` up to the first moved section with the text below, keep any remaining Phase 1–9 text that didn't move, and end with the sections from `## Privacy and safety` on. Fill `## Known issues` from the triage notes' "Known issues" list (one bullet per group, each linking to `docs/superpowers/notes/2026-10-08-phase-10-triage.md` for detail), followed by the live-verification gaps from the old `## Known gaps in this scaffold` section (Codex not live-verified, API backends other than Ollama not live-verified, Windows not run, drafts and proactive dots not run end to end in Herdr, and so on), each kept as written there except the word "scaffold". Keep the Phase 2 row marked **VITAL** in the roadmap history.

```markdown
# Herdr Dots

> **Status: release candidate 1.0.0-rc.1, not released.** Every roadmap phase
> is built. It has not been published, it has no license yet, and it has been
> run on macOS only; see [Status](#status).

Herdr Dots is a [Herdr](https://herdr.dev) plugin that adds **dots**:
specialist agents (security, frontend, backend, copywriter, ...) that join a
Claude Code or Codex session running in a Herdr pane. After every turn of the
main agent, each attached dot checks the turn against its purpose and speaks
up only when it has something worth saying. Dots remember your project, can
convene as a council, run on Claude Code, Codex or API models, answer your
agent over MCP, draft fixes in a sandbox when you allow it, research while
you're away, and can be shared with your team.

## Status

1.0.0-rc.1 is a release candidate. What is still open before a 1.0.0
release: a license, publishing, the three-OS acceptance run on real Linux and
Windows machines, and the live provider checks (Claude, the API backends and
Codex). See
[docs/release/ready-to-publish.md](docs/release/ready-to-publish.md).

## Requirements

- Herdr 0.8.0 or newer on macOS or Linux; 0.9.1 or newer on Windows (beta)
- `claude` (Claude Code) logged in for `claude-cli` dots, `codex` for `codex-cli` dots, or an API key for API dots
- Go 1.25 or newer only when no prebuilt binary fits (the install falls back to a source build)

## Install

    herdr plugin install <owner>/herdr-dots

Details, checks and upgrades: [docs/guide/install-and-upgrade.md](docs/guide/install-and-upgrade.md).

## Quick start

1. `./bin/herdr-dots setup-keys` (from a shell inside Herdr) adds the keybindings, after showing them and backing up your config.
2. Press `prefix+d`, create a dot from a template (Security is a good first one).
3. Focus your Claude Code or Codex pane and attach the dot; press `prefix+a` to open the Dots pane.
4. Work as usual. Cards wait in the queue: `enter` sends one, `x` dismisses it, and dismissing teaches the dot not to raise it again.

## Guide

[docs/guide/](docs/guide/README.md): dots, delivery modes, memory, council,
backends and cost, MCP, Windows, write-capable dots, proactive dots,
profiles and sharing, install and upgrade.

## Privacy and safety

- **No telemetry.** The plugin talks only to the backends you configure, Herdr's local socket, and GitHub when it installs.
- Dots are read-only unless you opt a dot into write mode (then two approvals, a separate worktree, no shell).
- Memory and history stay on your machine, in the plugin's state directory; every entry can be listed, edited, forgotten or purged.
- Keys stay in `credentials.toml` (owner-only) or environment variables.
- `NO_COLOR=1` turns colors off (bold, dim and reverse stay).
- Every rule in detail: [docs/guide/privacy-and-safety.md](docs/guide/privacy-and-safety.md). Security policy: [SECURITY.md](SECURITY.md); threat model: [docs/security/threat-model.md](docs/security/threat-model.md).

## Uninstall

    ./bin/herdr-dots uninstall-extras            # MCP entries and keybindings, with backups
    ./bin/herdr-dots uninstall-extras --purge    # also every dot, setting, credential and all memory
    herdr plugin uninstall herdr-dots

## Known issues

(bullets from the triage notes)

## Roadmap history

| Phase | Version | Increment |
|---|---|---|
| 1 | 0.1.0 | Core loop |
| **2** | 0.2.0 | **Dot memory (VITAL)** |
| 3 | 0.3.0 | Council mode |
| 4 | 0.4.0 | API and Ollama backends, keys, cost |
| 5 | 0.5.0 | MCP server |
| 6 | 0.6.0 | Windows support |
| 7 | 0.7.0 | Write-capable dots in sandboxed worktrees |
| 8 | 0.8.0 | Proactive dots |
| 9 | 0.9.0 | Profiles, history and sharing |
| 10 | 1.0.0-rc.1 | Release hardening: migrations, fuzzing, soak, verified installs, threat model |

## License

Not chosen yet. Until the owner picks one, no license is granted.
```

- [ ] **Step 3: `CHANGELOG.md`**

Create `CHANGELOG.md` (one line per phase from the roadmap; under 1.0.0-rc.1, the Phase 10 changes):

```markdown
# Changelog

All notable changes. Versions before 1.0.0-rc.1 were development
milestones, never released.

## 1.0.0-rc.1 (release candidate, not released)

- Schema versions for every persisted file and a migration framework: older files are upgraded with a backup, files from a newer version stay read-only (`herdr-dots migrate`).
- Prebuilt, checksum-verified binaries for macOS, Linux and Windows (amd64, arm64) with a source-build fallback; `herdr-dots verify-release`; `--version` shows the build source and commit.
- `herdr-dots uninstall-extras [--purge]`.
- Snapshot broadcasts capped at 10 a second and 256 KB; an 8-hour soak test.
- Fuzz tests for every parser; `govulncheck` in CI.
- Hardening: secrets split by invisible characters are caught; the memory approve screen refuses when the popup can't show the whole proposal; nested repositories are their own projects; dot names can't escape the dots directory; an old feed file becomes owner-only; two panics are gone; a memory export of an unsaved entry imports again; every git call's repository-program guard is pinned by a test.
- Threat model, security review, user guide, `SECURITY.md`, `CONTRIBUTING.md`.

## 0.9.0

Dot profiles with 7- and 30-day stats, run history, dot packs (export and import with clamps and secret scanning), project dots with content-hashed trust, more templates.

## 0.8.0

Proactive dots: idle-time research with a "while you were away" digest, scheduled dots with catch-up, commit-triggered reviews, a project inbox, a daily dollar budget.

## 0.7.0

Write-capable dots: drafts in a sandboxed worktree from a side-effect-free snapshot, two approval gates, validated patches, a diff viewer.

## 0.6.0

Windows support (Herdr Windows beta): named pipes, `LockFileEx`, Job Objects, DACL privacy, `.cmd` shim handling; system prompts moved off argv; a three-OS CI matrix.

## 0.5.0

The Dots MCP server: the main agent can list and ask dots, convene a council, read the queue and memory, and propose memory.

## 0.4.0

Anthropic, OpenAI, OpenRouter and Ollama backends with a jailed read-only tool loop, `credentials.toml`, costs in dollars and dollar budgets.

## 0.3.0

Council mode: every attached dot answers at once, one merged digest card, an optional dot-to-dot second round.

## 0.2.0

Dot memory (VITAL): project, dot and global memory, learned from outcomes and memory passes, dismissed findings never raised again, proposals that wait for approval, full TUI and CLI control.

## 0.1.0

The core loop: dots attached to Claude Code and Codex panes review each turn and deliver through observe, suggest, queue or inject.
```

- [ ] **Step 4: `CONTRIBUTING.md`**

Create `CONTRIBUTING.md`:

```markdown
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

## Commits

Small commits with a type prefix (`feat:`, `fix:`, `test:`, `docs:`,
`build:`). Never commit secrets, `bin/` or `dist/`.
```

- [ ] **Step 5: Check the links**

Run: `for f in README.md CONTRIBUTING.md SECURITY.md docs/guide/*.md; do grep -o '](\([^)#]*\))' "$f" 2>/dev/null | sed 's/](\(.*\))/\1/' | grep -v '^http' | while read -r l; do [ -e "$(dirname "$f")/$l" ] || echo "$f: broken link $l"; done; done`
Expected: no output, except links to `SECURITY.md` and `docs/security/threat-model.md` if Task 11 isn't merged yet in this worktree (they exist after the Wave 3 merge; rerun after merging).

- [ ] **Step 6: Check the wording rules**

Run: `grep -n -i -E '\b(released|ready|done|finished|feature complete)\b' README.md CHANGELOG.md docs/guide/*.md | grep -v -i -E 'not released|never released|release candidate|ready-to-publish|Patch ready|finished run|reports .done.'`
Expected: no output (the README says "not released"; "feature complete" appears only after Task 13). The three exclusions after `ready-to-publish` are technical text moved verbatim from Phases 7–8 (`*Patch ready: …*`, the message a draft sends; "a finished run"; Herdr reporting the `done` agent status).

- [ ] **Step 7: Commit (never push)**

```bash
git add README.md docs/guide CHANGELOG.md CONTRIBUTING.md
git commit -m "docs: README around a user guide, CHANGELOG, CONTRIBUTING with schema rules (Phase 10)"
```

---

### Task 13: Acceptance checklist, soak run, ready-to-publish checklist

**Depends on:** all (Wave 4).

**Files:**
- Create: `docs/superpowers/notes/1.0-acceptance.md`, `docs/release/ready-to-publish.md`
- Modify: `README.md` (the `## Status` block and the line under the title only), `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md` (Phase 10 row status), `docs/superpowers/notes/2026-10-08-phase-10-verification.md` (`## Phase 10 wrap-up`)

**Interfaces:**
- Consumes: everything; the roadmap's feature-complete checklist; Task 1's tool list.
- Produces: the macOS acceptance evidence per checklist item, with Linux and Windows marked as user gates; the soak result; the ready-to-publish checklist; the final README status.

- [ ] **Step 1: Start the 8-hour soak in the background**

Run (in the background, with output to a file outside the repo): `SOAK_DURATION=8h SOAK_IDLE=10m GOTOOLCHAIN=local go test -tags soak -run TestSoak -timeout 9h -v ./internal/engine/ > "${TMPDIR:-/tmp}/herdr-dots-soak-8h.log" 2>&1`
Expected: it runs for about 8 h 11 min. Continue with Steps 2–5 meanwhile. If this session can't wait for it, record the longest completed soak (Task 4's, or a 1-hour run: `SOAK_DURATION=1h SOAK_IDLE=10m`) and list "8-hour soak" in the ready-to-publish checklist as a user gate.

- [ ] **Step 2: Full automated run on macOS**

Run, one after the other:

```bash
gofmt -l internal cmd scripts
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race -count=1 ./... 2>&1 | tail -50
GOTOOLCHAIN=local make cross
GOTOOLCHAIN=local sh scripts/fuzz.sh 60s 2>&1 | tail -3
HERDR_DOTS_E2E_RELEASE=1 GOTOOLCHAIN=local go test -run TestLocalReleaseInstallsEndToEnd -v -timeout 10m ./scripts/ 2>&1 | tail -3
GOTOOLCHAIN=local go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./... 2>&1 | tail -3
actionlint .github/workflows/*.yml && shellcheck -s sh scripts/*.sh && echo lint-ok
```

Expected: `gofmt` prints nothing; vet and every package pass; `make cross` succeeds; `fuzzed 24 targets for 60s each`; the release e2e passes; `No vulnerabilities found.` (or the recorded reason); `lint-ok`. The live provider tests (`go test -tags live ./internal/backend/ ./internal/council/`, which spend API money) are not run here: live provider checks are a user gate (Plan decision 34), listed in the ready-to-publish checklist.

- [ ] **Step 3: Live smoke on macOS (no plugin link)**

Using only `herdr --session dots-test` and the manual-env method Phase 2 Task 16 used (run `./bin/herdr-dots` subcommands with `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR` and `HERDR_SOCKET_PATH` pointing at temp dirs and the test session's socket; never `plugin link`, never slash commands into Claude panes, never touch `~/.config/herdr/config.toml`):
1. `./bin/herdr-dots --version` → `herdr-dots 1.0.0-rc.1 (source, commit ...)`.
2. Start the engine for the test session; check `engine.log` has no `migrate:` warnings on a fresh state dir.
3. Write `{"version":2,"cards":[]}` into the session's `queue.json` before starting the engine a second time: the engine starts, the Herdr test session shows the toast naming `queue.json`, and the file is unchanged afterwards.
4. Make one proposal without any provider: `./bin/herdr-dots memory import` (with the same env) a file holding one project entry with `status = "proposed"` (write it with `memory export`, then edit the status), open the memory screen in a popup shrunk to under 10 lines with that proposal selected, press `a`, and check it says "Too small to show the whole proposal" and `y` doesn't approve.

The provider part of the old smoke (attach a Security dot to a Claude Code pane, get a card, dismiss it, see `suppressed` on a repeat) needs a live model: it is a user gate (Plan decision 34), listed in the ready-to-publish checklist with the live tests.

Record each step's result (command output or a short description; screenshots are optional) for Step 4.

- [ ] **Step 4: The acceptance notes**

Create `docs/superpowers/notes/1.0-acceptance.md`. Copy every item of the roadmap's "Feature-complete checklist" (`docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, all sections from "Core loop" to "Release") into this table, one row per checkbox, in the same order:

```markdown
# 1.0 feature-complete acceptance run

Run on 2026-10-08 on macOS (darwin/arm64, Herdr 0.8.2, Go 1.25.x) for
1.0.0-rc.1. Linux and Windows were not available: their columns are user
gates (CI is written but has not run, because nothing was pushed). Codex is
not installed: Codex items are documented-format, not live-verified.

| # | Checklist item (roadmap) | macOS evidence | Linux | Windows |
|---|---|---|---|---|
| 1 | Create, edit, delete dots from scratch and from templates in the manage popup; invalid files are reported with their line number. | (test names from `go test` output, e.g. `TestLoadAllReportsLineNumbers`, plus Step 3 item 4) | user gate | user gate |
```

Rules for the evidence column: cite the tests that cover the item (find them with `grep -rn 'func Test' internal/<package>/`), add the Step 3 live result where one applies, and write "user gate: <what is missing>" when macOS can't show it (for example Codex panes, the Windows item, "listed on herdr.dev/plugins", "installs without Go on all six targets", "LICENSE exists"). A Release-section item that depends on publishing, the license or other OSes is "not met: user gate" here. Do not mark any row as passing without evidence.

End the file with:

```markdown
## Soak

(the TestSoak log line from the 8-hour run, or the longest completed run
and "8-hour run: user gate")

## Result

On macOS, every item that can be shown without publishing, a license, a live
model provider or other operating systems has evidence above.
The product is feature complete on paper.
Not met yet, and required before 1.0.0: the Linux and
Windows columns, the live provider checks (Claude, API backends, Codex), the
license, publishing and the marketplace listing (see
docs/release/ready-to-publish.md).
```

If any macOS-testable item has no evidence or fails, stop: fix it (a new task in this wave, with a test) or record it in the Result paragraph as a failure, and do not write "feature complete on paper" in Steps 4–6.

- [ ] **Step 5: The ready-to-publish checklist**

Create `docs/release/ready-to-publish.md`:

```markdown
# Ready to publish: the developer's checklist

Phase 10 built Herdr Dots up to a release and stopped here. Everything below
needs the owner. Nothing has been pushed, tagged, listed or published.

## Blockers

- [ ] **License.** Choose one (the spec suggested Apache-2.0 for its patent grant, or MIT) and add `LICENSE`. No license means no one may use the code.
- [ ] **Name.** Keep "Herdr Dots" or rename. OpenAI announced "Dots" (DevDay 2026); a rename later is supported by `internal/migrate` but touches the plugin id, binary and docs. Check the name on GitHub and herdr.dev/plugins.
- [ ] **Acceptance on Linux and Windows.** Run `docs/superpowers/notes/1.0-acceptance.md` on a Linux machine and on Windows with the Herdr Windows beta, and on macOS with a real `herdr plugin install`; fill the Linux and Windows columns.
- [ ] **Live provider checks.** With Claude Code logged in, run `go test -tags live ./internal/backend/ ./internal/council/` (a small API spend; snapshot `~/.claude/settings.json` before and diff it after: it must not change) and the live smoke in a `herdr --session dots-test` session: attach a Security dot to a Claude Code pane, get a card for an obvious secret, dismiss it, and see `suppressed` in the feed when the same request repeats. Run the API backends' live tests with your keys (`docs/superpowers/notes/2026-10-08-phase-4-verification.md`).
- [ ] **Codex.** Install Codex, log in, run `go test -tags live ./internal/backend/ ./internal/council/` and the Codex rows of the acceptance run.
- [ ] **Soak.** (only if Task 13 recorded it as a user gate) `make soak SOAK_DURATION=8h`.
- [ ] **govulncheck.** (only if it couldn't run) `make vulncheck`.

## Publishing steps (in order)

1. Create the GitHub repository and push `main`. Check that CI is green on macOS, Linux and Windows, including the PowerShell install tests and the fuzz, vulncheck and soak jobs.
2. If Herdr's clone won't have a GitHub `origin`, put `owner/herdr-dots` in `release/REPO`.
3. When the acceptance run passes everywhere, set `internal/version/VERSION` and `herdr-plugin.toml` to `1.0.0`, update `TestVersionIsTheReleaseCandidate` and the CHANGELOG heading, commit, push.
4. Tag `v1.0.0-rc.1` (or the next rc) on that commit and push the tag. The release workflow tests, fuzzes, builds the six archives and opens a pull request with `release/SHA256SUMS` and `release/ASSET_ENV`.
5. Merge that pull request. Tag `v1.0.0` on the merge commit and push the tag. The workflow rebuilds, refuses if the archives differ from the committed checksums, signs them with cosign and creates a draft release.
6. Check the draft (six archives, six `.sigstore.json` bundles, `SHA256SUMS`), then publish it.
7. Install on a machine without Go: `herdr plugin install <owner>/herdr-dots`; `./bin/herdr-dots --version` must say `release`; `./bin/herdr-dots verify-release` must pass (with cosign installed, the signature too). Repeat on each of the six targets you can reach.
8. Add the `herdr-plugin` topic to the repository and check the listing on herdr.dev/plugins (it refreshes every 30 minutes).
9. Only now may the README and CHANGELOG say "released"; "feature complete" without "on paper" needs the acceptance run to pass on all three OSes.

## What Phase 10 prepared

- `.goreleaser.yaml`, `.github/workflows/release.yml` and `ci.yml` (lint-clean with actionlint; never run).
- `scripts/build.sh`, `build.ps1` (verify, fall back, fail cleanly), `scripts/release-local.sh`, `make snapshot` (archives built and installed end to end locally).
- `herdr-dots verify-release`, `migrate`, `uninstall-extras`.
- `docs/security/threat-model.md`, the security review, `SECURITY.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `docs/guide/`.
- `docs/superpowers/notes/1.0-acceptance.md` (macOS evidence).
```

Fill the two conditional lines according to Steps 1 and 2 (delete a line that doesn't apply).

- [ ] **Step 6: README status, roadmap, wrap-up**

In `README.md`, replace the status line under the title and the `## Status` section body with (only if Step 4's Result says "feature complete on paper"; otherwise keep Task 12's text and add the failures):

```markdown
> **Status: release candidate 1.0.0-rc.1, not released.**
> Feature complete on paper: every roadmap phase is built and the macOS
> acceptance run has evidence for every item it can show. Not yet verified on
> Linux or Windows hardware, live providers and Codex not verified in this run,
> no license, not published.
```

```markdown
## Status

Every roadmap phase (1–10) is built, and on macOS the acceptance checklist
has evidence for every item that doesn't need publishing, a license, a live
model provider or another OS ([notes](docs/superpowers/notes/1.0-acceptance.md)).
Open before 1.0.0: the acceptance run on Linux and Windows hardware, the live
provider checks (Claude, API backends, Codex), a license, and publishing
([checklist](docs/release/ready-to-publish.md)).
```

In `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, in the Phases table, replace the Phase 10 row's version and increment cells, `| 10 | 1.0.0 | Rename, hardening, marketplace release; **feature complete only when the checklist passes** |`, with `| 10 | 1.0.0-rc.1 (1.0.0 is a user gate) | Release hardening (1.0.0-rc.1 built; 1.0.0 waits on the user gates in docs/release/ready-to-publish.md); **feature complete only when the checklist passes** |`, keeping the row's last three cells. This is the only roadmap edit (Phases 2–9 left the roadmap alone; Phase 10's row is its own). Leave every checklist checkbox unchecked (the roadmap's checklist is for the three-OS run).

Fill `## Phase 10 wrap-up` in the Task 1 notes with: the soak result, the fuzz result (targets × time, crashers fixed), govulncheck, the security review summary (counts by severity, fixed vs known), and the user gates that remain (copy the Blockers list).

- [ ] **Step 7: Wording check**

Run: `grep -rn -i -E '\b(released|ready|done|finished)\b' README.md CHANGELOG.md docs/guide docs/release docs/superpowers/notes/1.0-acceptance.md | grep -v -i -E 'not released|never released|ready to publish|ready-to-publish|release candidate|may .* say .released|Patch ready|finished run|reports .done.'`
Expected: no output. `feature complete` appears only together with `on paper`: `grep -rn -i 'feature complete' README.md docs/superpowers/notes/1.0-acceptance.md | grep -v -i 'on paper'` prints nothing.

- [ ] **Step 8: Wait for the soak, then commit (never push, never tag)**

When the background soak ends: `tail -5 "${TMPDIR:-/tmp}/herdr-dots-soak-8h.log"` must show `--- PASS: TestSoak` and the log line; put it into the acceptance notes' Soak section and the wrap-up. If it failed, fix the cause (with a test) before committing, or record the failure and move "8-hour soak" to the Blockers.

```bash
git add docs/superpowers/notes/1.0-acceptance.md docs/release/ready-to-publish.md README.md docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md docs/superpowers/notes/2026-10-08-phase-10-verification.md
git commit -m "docs: 1.0 acceptance evidence (macOS), soak result and the ready-to-publish checklist (Phase 10)"
```

---

## Self-review

Checked against the spec (with the decisions doc's amendments) after writing.

**Spec coverage:**
- Success criterion 1 (name and migration of installs): replaced by ruling 1; the data-migration framework is Task 2, wired by Task 7; rename-only pieces dropped (Plan decision 1).
- Criterion 2 (install without Go, verified binaries, `go build` fallback): Tasks 5 (GoReleaser, archives, workflow) and 6 (scripts, verify-release); the six-target run on real machines is a user gate (Task 13 checklist).
- Criterion 3 (listing, README, demo, license, security policy, changelog, guide): Tasks 11 and 12; listing, topic and license are user gates; the demo (a recording) is left to publishing, listed in Task 12's README only by the Quick start, a deliberate omission noted here.
- Criterion 4 (schema versions, fuzzing, reviewed threat model, soak): Tasks 2, 3, 11, 4 + 13.
- Criterion 5 (acceptance run on three OSes before 1.0.0): Task 13 for macOS; Linux/Windows and the `1.0.0` tag are user gates; the version is `1.0.0-rc.1`.
- F5 (committed checksums anchoring trust): build scripts read only `release/SHA256SUMS` from the clone; the release flow commits it through a PR (Plan decision 14). F6 (cosign optional verification): release workflow and `verify-release`. F7: Plan decision 2. F8: Task 3 (spec list plus extras, decision 7). F9: Task 4. F10: README and threat model T21. F11: Task 8. F12: Tasks 11–12 (no LICENSE, ruling 16).
- §6.3 startup migrations and read-only mode with a toast: Tasks 2 and 7. §7 UI: migration notice dropped with the rename; `--version` shows source and commit (Task 5); manage footer dropped (decision 22). §8.1 errors: checksum mismatch → fallback (Task 6), legacy dirs n/a, partial copy n/a, failed migration → untouched, read-only, reported (Task 2). §8.2 safety: Task 11. §8.3 testing: migration fixtures (Task 2), build script with httptest (Task 6), fuzz (Task 3), soak (Task 4/13), acceptance (Task 13).
- Deferred-minor triage (dispatch requirement): Tasks 9 and 10 read every ledger (v1 to Phase 9, "Parked → Phase 10" lines included) and fold, mark as already fixed, or list each line; the two known flaky tests are stabilized in Task 10.

**Placeholder scan:** every code step has complete code (generated from the compiled scratch copy). The steps that ask the executor to fill text from real output (Task 1 RESULT lines, Task 9 later ledger lines, Task 11 review findings and test-name check, Task 12 moved README sections, Task 13 evidence) are evidence-capture steps with an exact format and a rule, because the content only exists at execution time.

**Type consistency:** `migrate.Report` fields (`Migrated`, `Pending`, `ReadOnly`, `Failed`, `Corrupt`, `Backups`) are used the same way in Tasks 2 and 7; `fsutil.ProtectNewer`/`Protected`/`ResetProtected`/`ErrNewerVersion` in Tasks 2 and 7; `version.String()`/`Source`/`Commit` in Tasks 5 and 6; archive names `herdr-dots_<VERSION>_<os>_<arch>` in Tasks 5 and 6 (and pinned by `TestGoReleaserConfigMatchesTheInstallScripts`); `keys.RemoveBlock` returns `(out, removed, left, err)` in Task 8 only.

**Review Focus:** each of the five lines has its tests in the owning task (listed in the section).

**Reconcile pass (2026-10-09, against `dffabbf`, Phases 2–9 merged):** every task was replayed in wave order from this text in a scratch worktree, then replayed a second time from the final text onto a fresh `dffabbf`; the two trees matched. Plan decisions 24–41 record what changed and why; the final text was also replayed on `0205616`, where Phase 9's final fix wave landed during the pass. Code blocks were rewritten where the merged code differed (writers and kinds in Task 2, five new fuzz targets and the dotpack, credentials and Codex invariants in Task 3, the engine hunks in Task 4, the version pin and manifest in Task 5, `verify-release`'s child process in Task 6, the writing commands in Task 7, the binding count and Windows-portable tests in Task 8, the whole of Task 9, the slug rule and the flaky tests in Task 10); the spec's design, the existing tests and the decisions are kept. The docs tasks (11–13) were checked against the merged tree: every test the threat model cites exists, the README table names the real headings, and the live provider checks moved to the user gates.

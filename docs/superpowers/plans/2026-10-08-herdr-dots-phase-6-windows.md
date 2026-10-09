# Herdr Dots Phase 6: Windows Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run every Herdr Dots feature from Phases 1–5 natively on the Herdr Windows beta (named pipes, `LockFileEx`, Job Objects, DACL privacy, `.cmd`-shim-safe command lines) without changing Unix behavior, and ship it as `0.6.0`.

**Architecture:** Every OS difference moves into a new `internal/platform` package (portable rules plus `//go:build unix` / `windows` files) and a few build-tagged files in the packages that own a transport (`herdr`, `herdrtest`, `ipc`, `paths`, `lockfile`, `fsutil`). Business logic calls platform helpers and never branches on `runtime.GOOS`; a portability test enforces that for every later phase. Long system prompts leave argv on every OS, and on Windows every `.cmd`/`.bat` program runs through an explicitly quoted `cmd.exe /d /s /c` line.

**Tech Stack:** Go 1.25 (`go 1.25.0` directive since Phase 4). New dependency, pinned: `github.com/Microsoft/go-winio v0.6.2` (named pipes). `golang.org/x/sys` (already in `go.mod` at Phase 4's `v0.47.0`) becomes a direct dependency for `x/sys/windows`. GitHub Actions for the three-OS CI file. PowerShell for the Windows build script.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-6-windows-design.md` (approved with the amendments in `docs/superpowers/decisions/2026-10-08-phase-decisions.md`: ruling 4 puts Windows here, before Phases 7–9; the release-gate amendment says this macOS machine can only cross-compile and unit-test Windows, so a real Windows run is a user gate). This plan replaces the outline `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-6-windows-outline.md`.

**Preconditions (check before Task 1; stop and report if any fails):**
- Phases 2, 3, 4 and 5 are merged into the base branch: `internal/memory`, `internal/council`, `internal/credentials`, `internal/backend/toolloop`, `internal/backend/ollama`, `internal/mcpinstall`, `internal/mcpserver` and `cmd/herdr-dots/setupmcp.go` exist, and `internal/version/version.go` says `0.5.0`.
- `go.mod` says `go 1.25.0` and lists `golang.org/x/sys` (Phase 4 put it at `v0.47.0`, still `// indirect`).
- `GOTOOLCHAIN=local go test -race ./... && GOTOOLCHAIN=local go vet ./...` passes on the base (darwin).

**Reconciled against the merged code (2026-10-09).** This plan was first written against v1 + Phase 2 and the *plans* of Phases 3–5. It has since been replayed task by task, in wave order, on `feat/dots-v1` at `5863171` (Phases 2–5 merged, 0.5.0) and then again at `a47427d` (the same plus the Phase 5 final fix wave): every code block below was applied as written, darwin `go test -race ./...` and `go vet ./...` passed, `gofmt -l internal cmd` printed nothing, `GOOS=linux go build ./... && go vet ./...` and `GOOS=windows go build ./... && go vet ./...` were clean, `GOOS=windows go test -c ./...` compiled every package's Windows test binary, and `make cross` passed. Every diff is a real `git diff` against `a47427d` and applies there with plain `git apply`. Decisions added by the reconcile are marked "(reconciled)" (Plan decisions 24–34). The base for Wave 1 is therefore `a47427d` or later.

## Global Constraints

Phase 6 constraints:
- **The product is NOT done after Phase 6.** The version becomes `0.6.0` (`internal/version/version.go` and `herdr-plugin.toml`). Never call the project done, finished, complete or ready in code, docs, commits or reports. Feature complete is defined only by Phase 10. The README roadmap keeps Phase 2 marked **VITAL**.
- Platforms: macOS, Linux, Windows 10 1809+ and Windows 11 under the Herdr Windows beta. `platforms = ["macos", "linux", "windows"]`. Windows ARM64 and WSL modes are out of scope.
- No `runtime.GOOS` branches in product code. OS differences live in `internal/platform` or in files constrained with `//go:build unix` / `//go:build windows` (a `_unix.go` file name alone is **not** a build constraint in Go; always write the `//go:build` line).
- Unix behavior stays byte-identical: same socket paths, same lock files, same file modes, same argv, except W7 (a system prompt over 4,000 characters moves to a private file on every OS).
- Windows privacy: an owner-only DACL (current user, SYSTEM, Administrators; inheritance off) for credentials, memory, config and session state; `CheckPrivate` refuses any DACL entry for anyone else. Never request elevation.
- Every dot child process on Windows runs in a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, started suspended so no grandchild escapes.
- The engine IPC endpoint on Windows is the named pipe `\\.\pipe\herdr-dots-<SessionKey>` with security descriptor `O:<user SID>D:P(A;;GA;;;<user SID>)`; clients refuse a pipe owned by anyone else.
- Every task's verification runs, from the repo root: `gofmt -l internal cmd` (prints nothing), `GOTOOLCHAIN=local go vet ./...`, `GOTOOLCHAIN=local go test -race ./...` (darwin), `GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...`, `GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./...`, `GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./...`, and `GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c` for the packages it touches. Windows-only behavior is covered by `//go:build windows` tests that compile here and run in CI or on a Windows machine (user gate).
- The CI file (`.github/workflows/ci.yml`) is written and committed, never pushed. No `git push`, no tags (release-gate amendment).
- Tests never touch the user's real Herdr session, `STATE_DIR` or `CONFIG_DIR`; use `t.TempDir()`. Run every `go` command with `GOTOOLCHAIN=local`.

All earlier Global Constraints still apply (v1 via Phases 2–4): plugin id `herdr-dots`; module `herdr-dots`; dots are read-only (`Read,Grep,Glob` or no tools for claude, `--sandbox read-only` for codex, read-only jailed tools for API dots); every child environment is stripped of `HERDR_*`, `CLAUDECODE`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_CHILD_SESSION` (`backend.CleanEnv`); the claude backend always passes `--settings` with `{"disableAllHooks":true}` and `--no-session-persistence`, never `--bare`; state files `0600`, directories `0700`, atomic writes (`fsutil.WriteFileAtomic`); default delivery mode `queue`; keys never reach logs, the feed, snapshots, IPC, memory, argv or error strings; memory is rendered in the user prompt, never the system prompt.

## Plan decisions

No human was available; every ambiguity below was resolved here. Each can be reversed.

1. **Herdr's socket on Windows (V1) is settled from Herdr's own source**, so Task 1 records evidence instead of running a spike. Herdr writes a marker file at `HERDR_SOCKET_PATH` (a normal path such as `%APPDATA%\herdr\herdr.sock`) and serves the named pipe `\\.\pipe\<that path>` (`src/platform/windows.rs` `probe_local_server`: `format!(r"\\.\pipe\{}", path.to_string_lossy())`). `platform.PipeName(path)` reproduces that; a value that already starts with `\\.\pipe\` passes through unchanged. Herdr Dots keeps a raw pipe client (go-winio) instead of shelling out to `HERDR_BIN_PATH`, because it needs long-lived event subscriptions.
2. **Manifest (V2).** Item-level `platforms` exist for every manifest item, but Herdr rejects duplicate action and pane ids even when their platforms differ (`manifest.rs` `reject_duplicate_action_ids` / `reject_duplicate_pane_ids`). So only `[[build]]` is split per OS (`sh scripts/build.sh` for macOS/Linux; `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1` for Windows). Startup, actions and panes keep `./bin/herdr-dots`: Herdr joins it to the plugin root, Rust's `std::process::Command` appends `.exe` to an extension-less path (actions, startup), and the pane launcher (portable-pty `search_path`) tries `PATHEXT` extensions. If a real Windows run shows otherwise, the fix is a Windows-only `[[build]]` that also copies `bin\herdr-dots.exe` to `bin\herdr-dots` (recorded in the notes as the fallback).
3. **`--append-system-prompt-file` is confirmed live (V4)** on Claude Code 2.1.295 here, and `--settings` takes a file path. The spec's stdin-preamble alternative is not built. If the temp files can't be written, BuildCmd falls back to the v1 argv.
4. **W7 trigger.** The system prompt moves to a file when it exceeds `platform.MaxInlinePrompt` (4,000 characters, counted as runes) on every OS, **or when claude resolves to a `.cmd`/`.bat` shim**. The spec said "always on Windows"; the shim rule has the same effect where it matters (cmd.exe can't carry quotes, `%` or newlines), needs no OS branch, and keeps a native `claude.exe` command line identical to Unix. With a shim, the settings JSON goes to a file too (it contains quotes).
5. **Shim execution.** `platform.Command` resolves the program; for a `.cmd`/`.bat` it runs `%ComSpec% /d /s /c ""<script>" "<arg>" ...""` through `SysProcAttr.CmdLine`, every argument quoted, trailing backslashes doubled, and refuses `"`, `%`, `!`, CR, LF and NUL (`platform.ErrUnsafeBatchArg`) and lines over 8,191 UTF-16 units (`platform.ErrCmdTooLong`, message names the shim and suggests the native `.exe`). Inside double quotes cmd.exe treats `& | < > ^ ( )` literally, so paths like `C:\R&D\my app` work. Prompts never reach a shim's argv, so purpose text can't inject commands.
6. **Binary resolution (W8).** `platform.LookPath` prefers `name.exe` in any PATH directory over a `.cmd` found earlier, skips relative PATH entries, and otherwise defers to `exec.LookPath` (PATHEXT).
7. **Process trees live in `platform`, not `backend`.** The spec placed `exec_unix.go`/`exec_windows.go` in `internal/backend`; `platform.StartTree` / `Tree.Kill` / `Tree.Close` keep the backend free of build tags. Windows starts the child `CREATE_SUSPENDED`, assigns it to a `KILL_ON_JOB_CLOSE` job, then resumes its primary thread (found with a Toolhelp snapshot), which is exactly how Herdr itself guards its status commands. `Tree.Close` after a normal exit also kills leftover grandchildren (stricter than Unix, which leaves them).
8. **Detached engine (W5).** `platform.StartDetached(cmd) (*os.Process, error)` replaces the spec's `Detach(cmd)`. Windows flags are `CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW`, first with `CREATE_BREAKAWAY_FROM_JOB` and, if a job forbids breakaway (access denied), again without it. `DETACHED_PROCESS` is dropped: Windows ignores `CREATE_NO_WINDOW` alongside it, and a console-less engine would make every console child (claude, git) open a visible window. A hidden console is inherited by every child. `platform.HideWindow` is still applied to dot runs and git calls.
9. **Engine pipe security.** The spec's DACL gains an explicit owner (`O:<SID>`), and `ipc.Dial` calls `platform.CheckPipeOwner`, refusing a pipe another user created first under the engine's name (Unix gets this from the 0700 directory). An Administrators owner is accepted when this process's token belongs to Administrators. go-winio creates the first instance with `FILE_FLAG_FIRST_PIPE_INSTANCE`, so a second engine (or a squatter) can't share the name.
10. **DACL privacy rules (W10).** `SecureDir`/`SecureFile` write `D:P(A;OICI;FA;;;<SID>)(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)` (no `OICI` on files) and skip paths that are already private, so `paths.Ensure` (run by every command) doesn't re-propagate ACLs. `CheckPrivate` allows the current user, the owner, SYSTEM, Administrators and OWNER RIGHTS; ignores deny and inherit-only entries; treats a missing DACL and object/callback entries as not private. `fsutil.WriteFileAtomic` applies `SecureFile` to every write with a private perm (no group/other bits) on Windows, and a failure fails the write; that is the spec's "ACL setup fails → refuse to write credentials", applied to all state.
11. **Credentials (Phase 4).** `platform.CheckPrivateFile` (on the opened file; see decision 25) replaces the mode-bit and uid checks. `ErrInsecurePerms` keeps its identity (`errors.Is` unchanged) but its text becomes "credentials.toml is readable by others" plus the OS fix from `platform.FixPrivateHint` (Unix: `run: chmod 600 "<path>"`; Windows: an `icacls` command or "delete it and set it again").
12. **File replacement on Windows.** Go opens files without `FILE_SHARE_DELETE`, so renaming over a file another process is reading fails briefly. `fsutil` retries the rename up to 40 × 25 ms on access-denied, sharing and lock violations. Unix calls `os.Rename` once, as before.
13. **Locks (W4).** `LockFileEx` exclusive + fail-immediately on one byte at offset 2^62 (so the lock never blocks reading or rewriting the pid), on a handle opened with `FILE_SHARE_DELETE`. Memory `Purge` now removes everything except `memory.lock` while holding the lock and removes the lock file and directory after release, because Windows can't delete a file another handle has open.
14. **Environment names.** `platform.EnvKey` upper-cases names on Windows and leaves them alone on Unix; `backend.CleanEnv` and Phase 2's `memory.gitEnv` compare in that form.
15. **Paths (W12, V3, V5, V6).** Herdr's config path follows Herdr's source (`src/config/io.rs`): `HERDR_CONFIG_PATH`, then `XDG_CONFIG_HOME\herdr`, then `%APPDATA%\herdr`, then `%USERPROFILE%\AppData\Roaming\herdr`, then `~/.config/herdr`. Claude and Codex keep their folders under the user's home on every OS. The spec's `ClaudeProjectKey` and `platform.Home` are dropped (YAGNI): transcripts are found by globbing every project folder, which doesn't depend on how Claude Code encodes `C:\Users\x\repo`, and `os.UserHomeDir` already returns `%USERPROFILE%`. `CODEX_HOME` stays unsupported on every OS (Unix byte-identical). (Superseded by decision 29: Phase 5 honors `CODEX_HOME`.)
16. **UI (V7).** Herdr 0.9.1's changelog says Windows input "preserves Alt combinations, Ctrl+S". So no `ctrl+w` save fallback (it would also break the textarea's delete-word). ASCII glyphs only with `HERDR_DOTS_ASCII=1` (ruling O4 b).
17. **`min_herdr_version` stays `0.8.0`.** The manifest has one global minimum; raising it would block Unix users on 0.8.x. The README says Windows needs Herdr ≥ 0.9.1 (Windows plugin panes resolve relative commands from the plugin root since 0.9.0; Ctrl+S since 0.9.1).
18. **Tool jail hardening (Phase 4).** `toolloop.Denied` matches case-insensitively on every OS (".ENV" opens ".env" on Windows and on default macOS volumes; Phase 4 already ships this with full Unicode folding and an exact on-disk-name check, see decision 27), and `platform.ReservedPathName` denies Windows alternate data streams (`.env::$DATA`), device names (`NUL`, `COM1.txt`) and names ending in a dot or space. `toolloop` opens files with `platform.OpenNoFollow` (Windows: `Lstat` refuses symlinks and reparse points, then `os.Open`; the jail already resolved symlinks, and Phase 4's `openVerified` re-checks the opened file after the open).
19. **Portability guard.** Task 10 adds `TestProductCodeHasNoUnixOnlyCalls`, which fails on Unix-only calls, OS branches, hard-coded client paths, `sh` execs, the literal `"HOME"` and raw `claude`/`codex` execs in untagged product code. It keeps Phases 7–9 portable without a Windows machine.
20. **Dependencies.** go-winio `v0.6.2` (`v0.6.3` needs Go 1.26). It adds two `go.sum` lines and no other module. Only Task 3 edits `go.mod`/`go.sum`; tasks that import `golang.org/x/sys/windows` earlier in the same wave build fine because the module is already listed.
21. **Windows build status during the port.** Wave 1 and Wave 2 tasks branch from bases where some packages still fail to compile for Windows (they belong to sibling tasks). Each task's Windows `go build ./...` / `go vet ./...` must print only packages from that wave's "may still fail" list (table below) and never a package the task touches; its own packages must pass `GOOS=windows go vet` (except where a package imports a sibling's failing package, as the task's gate says). From Wave 3 on, the Windows build and vet of `./...` print nothing. (Reconciled: at `a47427d` only `internal/lockfile`, `internal/backend` and `internal/backend/toolloop` fail to compile for Windows; `internal/credentials` compiles through Phase 4's `open_other.go` but refuses every file there, and every Phase 5 package compiles.)
22. **Line endings.** `.gitattributes` forces LF in every checkout, so `gofmt -l .` and byte-comparing tests behave the same on Windows CI.
23. **What "tested" means here.** On this macOS machine: darwin `-race` tests, Linux and Windows cross-builds, Windows `go vet`, and compiled (not run) Windows test binaries. Everything every `//go:build windows` test asserts runs only in CI or on a Windows machine; the README and notes say so, and the real run is a user gate.
24. **(reconciled) The real Phase 5 files, not a guard-driven search.** Task 10 no longer "lets a test find the lines": it carries concrete diffs for every Phase 5 file that needs a port (`mcpinstall` config paths, `setup-mcp`'s child commands, the codex dot run's `-c mcp_servers…` config lookup, the MCP and engine tests' sockets), plus the Phase 2 memory editor the guard's new `exec.Command("sh"` pattern found. The guard still runs and passes on the result. Task 6 already switches Phase 5's `mcp.go` (and its test) to `EngineEndpoint`, because `EngineSock` no longer exists on Windows.
25. **(reconciled) Credentials keep Phase 4's open-then-check order.** Phase 4 opens `credentials.toml` first (`O_NOFOLLOW|O_NONBLOCK`) and checks the descriptor (`fstat`), so the file checked is the file read. Task 3 adds `platform.CheckPrivateFile(f *os.File) error` (Unix: `fstat` mode bits and uid; Windows: owner and DACL read through the handle with `GetSecurityInfo`) beside `CheckPrivate(path)`, and Task 8 uses it. `open_unix.go` changes its constraint from `darwin || linux` to `unix` and loses `ownedByMe`; Phase 4's `open_other.go` becomes `open_windows.go`: `Lstat` refuses a symlink or reparse point, then the file is opened and the handle's own FileInfo returned. The `Lstat`→open gap on Windows is the documented fallback (no `O_NOFOLLOW` there): exploiting it needs write access to CONFIG_DIR, which is owner-only, and the privacy check runs on the handle that is read.
26. **(reconciled) Connection refused keeps its identity through httpx on Windows.** Phase 4 checks `syscall.ECONNREFUSED` in `backend.Unreachable`, the OpenAI and Anthropic adapters, Ollama, and keeps it as a sentinel when `httpx` scrubs an error (the providers screen's save check, ruling P4-R8, treats it as "unreachable"). Windows reports `WSAECONNREFUSED`, which that sentinel never matches. Task 2 adds `platform.ErrConnRefused` (`syscall.ECONNREFUSED` on Unix, `windows.WSAECONNREFUSED` on Windows); Task 8 makes every check `platform.ConnRefused(err)` and lets httpx keep `platform.ErrConnRefused` when `platform.ConnRefused` matches. Unix keeps exactly the v1 error identity.
27. **(reconciled) Phase 4's jail already folds case.** `toolloop.Denied` uses full Unicode case folding and `ResolveInRoot` requires exact on-disk names (Phase 4 fix waves, tests `TestDeniedIsCaseInsensitive` and `TestDeniedUsesFullUnicodeCaseFolding`), so Task 8 drops its `strings.ToLower` hunk and its `TestDeniedIgnoresCase` test. Task 8 still adds `platform.ReservedPathName` (streams, devices, trailing dots/spaces) before the deny check, switches `openVerified` to `platform.OpenNoFollow` (which gains `O_NONBLOCK` on Unix so Phase 4's FIFO guard stays), makes `Unavailable` refuse any file-system root (`filepath.Dir(rr) == rr`: `/`, `C:\`, a `\\server\share\` root; ruling P4-R5) instead of only `/`, and folds env names in the jail's `gitEnv` with `platform.EnvKey`. A `\\?\` root that `EvalSymlinks` can't resolve already fails closed (every tool reports "project root" errors).
28. **(reconciled) Tests that set `HOME` set `USERPROFILE` too.** `os.UserHomeDir` reads `USERPROFILE` on Windows, so the jail's home tests use a `setHome` helper (Task 8), and the codex MCP tests build the variable from `platform.HomeEnv` (Task 10).
29. **(reconciled) `platform.HomeEnv` and the codex config lookup.** Phase 5's `codexConfigPath` reads `HOME` from a dot run's environment to find `~/.codex/config.toml`. Task 2 adds `platform.HomeEnv` (`HOME`; `USERPROFILE` on Windows) and Task 10 compares names in `platform.EnvKey` form and builds the path with `platform.CodexConfig`. Decision 15's "CODEX_HOME stays unsupported" is superseded: Phase 5 honors `CODEX_HOME` in `mcpinstall` and in this lookup, and Phase 6 keeps that on every OS.
30. **(reconciled) The memory editor runs without `sh` on Windows.** Phase 2's `herdr-dots memory edit` runs `sh -c '<editor> "$1"'`. Task 10 adds `platform.EditorCommand(editor, file string) *exec.Cmd` (Unix: exactly that; Windows: `%ComSpec% /d /s /c "<editor> "<file>""`, the file being herdr-dots' own private temp path) and gives the temp directory `platform.SecureDir` (an owner-only DACL on Windows; `chmod 0700` on Unix, which `MkdirTemp` already did). The three `sh`-script editor tests move to `memory_unix_test.go`; the Windows behavior is tested in `internal/platform`.
31. **(reconciled) Test endpoints.** Task 3 adds `platformtest.Endpoint(t testing.TB) string` (a short unix socket path, or a unique named pipe), used by Phase 5's engine and MCP tests (Task 10). `internal/ipc` keeps its own `testEndpoint` (Task 6), which also checks unix-socket details.
32. **(reconciled) Phase 5's `engineEnv` folds name case.** The detached engine's environment replaces `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR` and `HERDR_SOCKET_PATH`; on Windows a variable spelled in another case would survive next to the new one. Task 6 compares names with `platform.EnvKey`.
33. **(reconciled) Version test.** Phase 5 pins the version in `cmd/herdr-dots/memwire_test.go` (`TestVersionIsPhaseFive`). Task 12 renames it `TestVersionIsPhaseSix`, expecting `0.6.0`, and changes it before the version (test first).
34. **(reconciled) Phase 5 final fix wave.** Merged at `a47427d` while this plan was reconciled; the plan was replayed again on it. Its `engineEnv` also drops `HERDR_PANE_ID`, `CLAUDECODE` and session `CLAUDE_CODE_*` markers: Task 6 folds the name once with `platform.EnvKey`, so `claudeProviderSetting` sees the upper-case name on Windows. Its new `TestUnreadableClaudeConfigIsRefused` makes a file unreadable with `chmod 000`, which Windows ignores (it only sets the read-only attribute), so Task 10 moves it to `internal/mcpinstall/final_unix_test.go`; the refusal code is the same on every OS, and a real ACL-denied file on Windows is a user gate. Its other new tests (`final_*_test.go` in `cmd/herdr-dots`, `internal/engine`, `internal/mcpserver`, `internal/memory/proposal_cap_test.go`) need no port.

## Confirmed while planning (2026-10-08)

| # | Question | Finding | Evidence | Assumption holds |
|---|---|---|---|---|
| V1 | Herdr socket on Windows | Named pipe `\\.\pipe\<HERDR_SOCKET_PATH>`; the env var is a file path with a marker file | herdr.dev/docs/socket-api ("On Windows, it is a named pipe"); herdr.dev/docs/plugins ("Windows clients connect to a named pipe"); Herdr source `src/ipc.rs`, `src/platform/windows.rs` (v0.9.3, commit 2563803) | yes (refined: path, not pipe name) |
| V2 | Per-platform manifest syntax; `.exe` resolution | Item-level `platforms` on every item; duplicate action/pane ids rejected across platforms; `./bin/herdr-dots` resolves to `.exe` | herdr.dev/docs/plugins; Herdr `src/app/api/plugins/manifest.rs`, `src/plugin_command.rs`, `vendor/portable-pty/src/cmdbuilder.rs`; CHANGELOG 0.9.0 | yes (only `[[build]]` split) |
| V3 | Claude transcripts on Windows | `%USERPROFILE%\.claude\projects\<encoded cwd>\<id>.jsonl`; resolved by glob, encoding irrelevant | Claude Code uses the OS home; Phase 6 globs every project dir | not live-verified, design unaffected |
| V4 | `--append-system-prompt-file` | Works in print mode; `--settings` takes a file | Live run, Claude Code 2.1.295 (a file prompt made the model answer `PINEAPPLE42`; a missing file fails before any API call) | yes |
| V5 | Herdr `config.toml` on Windows | `HERDR_CONFIG_PATH` > `XDG_CONFIG_HOME\herdr` > `%APPDATA%\herdr` > `%USERPROFILE%\AppData\Roaming\herdr` > `~/.config/herdr` | Herdr `src/config/io.rs` | yes |
| V6 | Claude/Codex config and binaries | `~\.claude.json`, `~\.codex\config.toml`; `claude.exe` (native) or `claude.cmd` (npm); `codex.cmd` (npm) | Claude Code and Codex docs; npm shim behavior | not live-verified, handled by `platform.Command` |
| V7 | Glyphs and `ctrl+s` in ConPTY | Ctrl+S preserved since Herdr 0.9.1; glyph fonts unverified | Herdr CHANGELOG 0.9.1 | ctrl+s yes; glyphs: user gate |
| V8 | `HERDR_PLUGIN_*` on Windows | Same names, no OS gates | Herdr `src/app/api/plugins/env.rs`, `runtime.rs` | yes |

## Review Focus

The five inputs the spec implies but its test list doesn't exercise, most likely to bite first. Each has a test in its owning task.

1. **A project path with spaces and non-ASCII characters** (`C:\Users\José Núñez\src\my app`). Expect transcripts found, Herdr's config found, shim command lines quoted, the engine pipe name free of path characters. Tests: `TestWindowsTranscriptUnderANonASCIIHome` (Task 7), `TestBatchCmdLineQuotesEveryArgument`, `TestHerdrConfigPathOverridesThenDefault` (Task 2), `TestEngineEndpointIsAPerSessionPipe` (Task 6).
2. **`claude.cmd` instead of `claude.exe`.** Expect purpose text full of `" & | % ^ < > !` never to reach cmd.exe, unsafe arguments refused before anything starts, and an over-long line refused with a hint naming `claude.exe`. Tests: `TestCommandPassesShimArgumentsLiterally`, `TestCommandRefusesUnsafeShimArguments` (Task 2), `TestShimGetsPromptAndSettingsAsFiles`, `TestClaudeThroughACmdShimKeepsThePromptOffTheCommandLine`, `TestRunReportsShimPreflightErrorsVerbatim` (Task 9).
3. **A timeout with grandchildren** (node under a shim, `cmd /c ping`). Expect the whole tree dead within 2 s, and leftovers killed when the run ends. Tests: `TestTreeKillEndsGrandchildren`, `TestTreeCloseKillsWhatIsLeft`, `TestExecRunnerKillsTheJobOnTimeout` (Task 2).
4. **Two Herdr sessions of one user, or a pipe name taken first.** Expect two engines on two pipes with no cross-talk, a second listener refused, and a client that refuses a pipe it doesn't own. Tests: `TestTwoEndpointsNeverCrossTalk`, `TestEngineEndpointIsPerSession`, `TestListenRefusesANameThatIsAlreadyServed` (Task 6), `TestPipeRoundTripOwnerCheckAndNoSecondListener` (Task 3).
5. **A `credentials.toml` copied from another machine with permissive inherited ACLs.** Expect reads and writes refused with `ErrInsecurePerms` and a fix-it hint, the key never quoted. Tests: `TestPermissiveFileIsRefusedWithoutReadingIt`, `TestInheritedUsersAccessIsRefusedAndSetFixesNothingSilently` (Task 8), `TestCheckPrivateRefusesReadAccessForOthers`, `TestCheckPrivateFileReadsTheHandlesDACL` (Task 3).

---

## Parallel execution: dependencies and waves

Each task runs in its own git worktree branched from the wave's base (the base branch with Phases 2–5 merged, plus every earlier wave). Merge every task of a wave, in task order, before starting the next wave. Tasks in the same wave never touch the same file.

| Task | Title | Depends on | Wave |
|---|---|---|---|
| 1 | Verification notes (V1–V8), Windows baseline, user gates | — | 1 |
| 2 | `internal/platform` core and process trees (ExecRunner, CleanEnv, `Cmd.Cleanup`) | — | 1 |
| 3 | `internal/platform` privacy, pipes and `platformtest` | — | 1 |
| 4 | `LockFileEx` locks and memory `Purge` | — | 1 |
| 5 | ASCII glyph fallback | — | 1 |
| 6 | Transports: Herdr pipe dial, engine endpoint, detached engine | Task 2, Task 3 | 2 |
| 7 | Files, client paths and git: `fsutil`, transcripts, setup-keys, memory git | Task 2, Task 3 | 2 |
| 8 | Phase 4 ports: credentials, tool jail, connection-refused checks | Task 2, Task 3 | 2 |
| 9 | CLI backends: system prompt off argv (W7), shims (W8) | Task 2, Task 3 | 2 |
| 10 | Phase 5 ports, the memory editor and the portability guard | Tasks 2–9 | 3 |
| 11 | Test-suite portability | Tasks 2–9 | 3 |
| 12 | Manifest, `build.ps1`, CI matrix, `.gitattributes`, version 0.6.0 | Tasks 2–9 | 3 |
| 13 | README, roadmap and notes (still not done) | all | 4 |

**Wave schedule:** W1 = {1, 2, 3, 4, 5} → W2 = {6, 7, 8, 9} → W3 = {10, 11, 12} → W4 = {13}.

**Files per task** (disjoint within each wave):
- Task 1: `docs/superpowers/notes/2026-10-08-phase-6-verification.md`.
- Task 2: `internal/platform/platform.go`, `cmdline.go`, `platform_unix.go`, `platform_windows.go`, `platform_test.go`, `platform_unix_test.go`, `platform_windows_test.go`; `internal/backend/exec.go`, `exec_test.go`, `exec_unix_test.go`, `exec_windows_test.go`.
- Task 3: `internal/platform/security.go`, `security_unix.go`, `security_windows.go`, `names.go`, `pipe.go`, `pipe_windows.go`, `security_test.go`, `security_unix_test.go`, `security_windows_test.go`; `internal/platform/platformtest/platformtest.go`, `platformtest_unix.go`, `platformtest_windows.go`; `go.mod`, `go.sum`.
- Task 4: `internal/lockfile/lockfile.go`, `lockfile_unix.go`, `lockfile_windows.go`, `xproc_test.go`; `internal/memory/store.go`, `purge_test.go`.
- Task 5: `internal/ui/style/style.go`, `style_test.go`; `internal/ui/pane/model.go`.
- Task 6: `internal/herdr/client.go`, `client_test.go`, `dial_unix.go`, `dial_windows.go`; `internal/herdr/herdrtest/fake.go`, `listen_unix.go`, `listen_windows.go`; `internal/ipc/ipc.go`, `ipc_test.go`, `query_test.go`, `endpoint_unix.go`, `endpoint_windows.go`, `endpoint_unix_test.go`, `endpoint_windows_test.go`; `internal/paths/paths.go`, `paths_test.go`, `endpoint_unix.go`, `endpoint_windows.go`, `paths_unix_test.go`, `paths_windows_test.go`; `cmd/herdr-dots/engine.go`, `engine_test.go`, `ui.go`, `action.go`, `mcp.go`, `mcp_test.go`.
- Task 7: `internal/fsutil/atomic.go`, `atomic_test.go`, `atomic_unix_test.go`, `atomic_windows_test.go`, `private_unix.go`, `private_windows.go`; `internal/transcript/claude.go`, `codex.go`, `claude_test.go`; `internal/keys/keys.go`, `keys_test.go`; `internal/memory/project.go`, `project_test.go`.
- Task 8: `internal/credentials/credentials.go`, `credentials_test.go`, `hardening_test.go`, `open_unix.go`, `open_other.go` (renamed to `open_windows.go`), `perms_unix_test.go`, `perms_windows_test.go`; `internal/backend/toolloop/jail.go`, `tools.go`, `tools_test.go`, `fix1_test.go`, `fix2_test.go`, `final_test.go`, `jail_windows_test.go`; `internal/backend/ollama/ollama.go`, `connrefused_test.go`; `internal/backend/api.go`; `internal/backend/openaiapi/openai.go`; `internal/backend/anthropicapi/anthropic.go`; `internal/backend/httpx/httpx.go`, `final_test.go`.
- Task 9: `internal/backend/claude.go`, `codex.go`, `runfiles.go`, `runfiles_test.go`, `runfiles_windows_test.go`.
- Task 10: `internal/platform/portability_test.go`, `editor_unix.go`, `editor_windows.go`, `editor_unix_test.go`, `editor_windows_test.go`; `internal/mcpinstall/install.go`, `install_test.go`, `final_test.go`, `final_unix_test.go`; `internal/backend/codex_mcp.go`, `codex_mcp_test.go`; `internal/mcpserver/engine_test.go`; `internal/engine/ask_test.go`; `cmd/herdr-dots/setupmcp.go`, `setupmcp_test.go`, `memory.go`, `memory_test.go`, `memory_unix_test.go`; the Task 1 notes file (Phase 5 section).
- Task 11: `internal/memory/store_test.go`, `internal/paths/memory_test.go`, `internal/paths/providers_test.go`, `internal/state/state_test.go`, `internal/queue/queue_test.go`, `internal/dots/store_test.go`, `cmd/herdr-dots/keys_test.go`.
- Task 12: `herdr-plugin.toml`, `internal/version/version.go`, `scripts/build.ps1`, `cmd/herdr-dots/platform_manifest_test.go`, `cmd/herdr-dots/memwire_test.go`, `.github/workflows/ci.yml`, `.gitattributes`, `Makefile`.
- Task 13: `README.md`, `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, the Task 1 notes file (wrap-up section).

**Windows build status (Plan decision 21).** A Wave 1 or Wave 2 task's Windows `go build ./...` and `go vet ./...` may print only these packages (minus the ones the task itself ports). The baseline at `a47427d` (and `5863171`) is `# herdr-dots/internal/backend`, `# herdr-dots/internal/backend/toolloop`, `# herdr-dots/internal/lockfile` (vet also prints `# herdr-dots/internal/backend [herdr-dots/internal/credentials.test]`); no Phase 5 package fails.

| Wave | May still fail for Windows | Ported by |
|---|---|---|
| 1 | `herdr-dots/internal/lockfile`, `herdr-dots/internal/backend`, `herdr-dots/internal/backend/toolloop`, and, once its imports compile, `herdr-dots/cmd/herdr-dots` (`unknown field Setsid` in `engine.go`) | Tasks 4, 2, 8, 6 |
| 2 | `herdr-dots/internal/backend/toolloop`, `herdr-dots/cmd/herdr-dots` (each until its task merges; a package that imports a failing one shows up after it) | Tasks 8, 6 |
| 3 | none | — |
| 4 | none | — |

(`go build` doesn't compile packages that import a failing package, so a dependent may appear only once its dependency is fixed.)

**Gate after Wave 2:** after merging Tasks 6–9, `GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./...` must print nothing. If anything fails, fix it before Wave 3. (The replay reached this state as soon as Task 8 merged.)

**Gate after Wave 3:** the Windows build and vet of `./...` succeed, `make cross` succeeds, and `go test -race ./...` passes on darwin.

**Test-name hygiene:** helpers added here: Task 2 `envMap`, `echoShim`, `pingTree`; Task 3 `setSDDL`, `daclString`, `mySID`; Task 4 none; Task 5 none; Task 3 also `endpoint`, `endpointSeq` (package `platformtest`); Task 6 `testEndpoint`, `pipeSeq`; Task 7 none; Task 8 `readableByOthers`, `symlinkOrSkip` (package `credentials`), `symlinkOrSkip`, `setHome` (package `toolloop`), `setDACL`, `me`; Task 9 `withBin`, `longReq`, `fileRunner`, `runDirs`, `errRunner`; Task 10 `moduleRoot`, `osSpecific`, `itoa`, and `symlinkOrSkip` plus the `tRoot` variable (package `mcpinstall`); Task 12 `platformManifest`, `readPlatformManifest`. Phase 3's `manifest` type in `cmd/herdr-dots` is untouched. Before adding a helper, run `grep -rn "func <name>\b" <package dir>`; if Phases 2–5 already define it, add the suffix `P6`.

### Shared files with Phases 2–5

Phases 2–5 are merged, so these files already carry their changes. Every diff below is a `git diff` against `a47427d` (Phases 2–5 and the Phase 5 final fix wave merged; reconciled 2026-10-09). Where a hunk doesn't apply because a later commit moved a line, make the same edit by hand: every hunk shows the exact lines to remove and add.

| File | Phase 2 | Phase 3 | Phase 4 | Phase 5 | Phase 6 task |
|---|---|---|---|---|---|
| `cmd/herdr-dots/engine.go` | memory deps | council | backend set | `ensureEngine` reuse, `engineEnv` | 6 |
| `cmd/herdr-dots/ui.go`, `action.go` | memory | council popup | manage deps | — | 6 |
| `internal/ipc/ipc.go`, `ipc_test.go`, `query_test.go` | `CmdRemember` | council cmds | — | `Query`, `Data` (`query_test.go` created) | 6 |
| `internal/backend/exec.go` | — | — | — | — | 2 |
| `internal/backend/claude.go` | `parseClaude(out, pass)` | — | cache tokens | — | 9 |
| `internal/backend/codex.go` | `parseCodex(out, pass)` | — | — | `-c mcp_servers.herdr-dots.enabled=false` | 9 |
| `internal/memory/store.go` | (created) | — | — | `Propose` | 4 |
| `internal/memory/project.go` | (created) | — | — | — | 7 |
| `internal/keys/keys.go`, `keys_test.go` | — | council key | — | — | 7 |
| `internal/ui/pane/model.go` | learning status | council card | cost display | — | 5 |
| `internal/credentials/*`, `internal/backend/toolloop/*`, `internal/backend/ollama/ollama.go`, `internal/backend/api.go`, `openaiapi/openai.go`, `anthropicapi/anthropic.go`, `httpx/httpx.go` | — | — | (created) | — | 8 |
| `cmd/herdr-dots/mcp.go`, `mcp_test.go` | — | — | — | (created) | 6 (EngineSock) |
| `internal/mcpinstall/*`, `internal/mcpserver/engine_test.go`, `internal/engine/ask_test.go`, `internal/backend/codex_mcp.go`, `cmd/herdr-dots/setupmcp.go` | — | — | — | (created) | 10 |
| `cmd/herdr-dots/memory.go`, `memory_test.go` | (created) | — | — | — | 10 |
| `cmd/herdr-dots/keys_test.go`, `internal/paths/providers_test.go` | — | — | (created) | — | 11 |
| `herdr-plugin.toml`, `internal/version/version.go`, `cmd/herdr-dots/memwire_test.go` | 0.2.0 | 0.3.0, council | 0.4.0 | 0.5.0 | 12 |
| `README.md`, roadmap | memory | council | API backends | MCP | 13 |

## File Structure

```
internal/platform/platform.go            (new)  package doc, ErrCmdTooLong, ErrUnsafeBatchArg, MaxInlinePrompt, MaxBatchCmdLine, HerdrConfigPath, client paths
internal/platform/cmdline.go             (new)  IsBatchFile, BatchCmdLine (cmd.exe quoting), findExecutable
internal/platform/platform_unix.go       (new)  DevNull, HomeEnv, EnvKey, LookPath, Command(Context), HideWindow, StartDetached, Tree (process group), ErrConnRefused, ConnRefused
internal/platform/platform_windows.go    (new)  the same for Windows: NUL, upper-case env names, .exe-first LookPath, cmd.exe shims, Job Objects, breakaway detach
internal/platform/security.go            (new)  ErrNotPrivate, ErrNotOwner
internal/platform/security_unix.go       (new)  SecureDir/SecureFile (chmod), CheckPrivate/CheckPrivateFile (mode bits, uid), FixPrivateHint, OpenNoFollow, ReservedPathName
internal/platform/security_windows.go    (new)  owner-only DACLs, DACL check by path or handle, icacls hint, reparse-safe open, Windows reserved names
internal/platform/names.go               (new)  windowsReservedName (OS-neutral rule, testable everywhere)
internal/platform/pipe.go                (new)  PipePrefix, PipeName
internal/platform/pipe_windows.go        (new)  PipeSDDL, ListenPipe, DialPipe, CheckPipeOwner (go-winio)
internal/platform/platformtest/          (new)  Abs, Endpoint, AssertPrivateFile, AssertPrivateDir
internal/platform/editor_*.go            (new)  EditorCommand: sh -c on Unix, cmd.exe on Windows (Task 10)
internal/platform/portability_test.go    (new)  the portability guard (Task 10)
internal/backend/exec.go                 (mod)  ExecRunner via platform.Command/StartTree; CleanEnv via EnvKey; Cmd.Cleanup/Close; Preflight
internal/backend/runfiles.go             (new)  resolveBin, writeRunFiles (private temp files for W7)
internal/backend/claude.go, codex.go     (mod)  system prompt/settings files, Preflight errors, platform.LookPath
internal/lockfile/lockfile*.go           (mod/new) shared TryLock/Held + flock (Unix) + LockFileEx (Windows)
internal/memory/store.go                 (mod)  Purge removes the lock file after release
internal/memory/project.go               (mod)  ExecGit: platform.DevNull, HideWindow; gitEnv via EnvKey
internal/herdr/client.go, dial_*.go      (mod/new) dialSocket: unix socket or \\.\pipe\<path>
internal/herdr/herdrtest/listen_*.go     (new)  Listen: the fake Herdr serves a pipe on Windows
internal/ipc/ipc.go, endpoint_*.go       (mod/new) Listen, Dial (owner-checked pipe on Windows)
internal/paths/paths.go, endpoint_*.go   (mod/new) EngineEndpoint; Ensure via SecureDir; EngineSock moves to the Unix file
internal/fsutil/atomic.go, private_*.go  (mod/new) Windows DACL for private writes, rename retry
internal/transcript/claude.go, codex.go  (mod)  platform client dirs
internal/keys/keys.go                    (mod)  ConfigPath = platform.HerdrConfigPath
internal/credentials/credentials.go, open_*.go (mod/renamed) CheckPrivateFile on the opened file; open_windows.go (Phase 4 files)
internal/backend/toolloop/jail.go, tools.go (mod) reserved names, OpenNoFollow, any file-system root refused, DevNull, EnvKey (Phase 4 files)
internal/backend/{api.go,ollama,openaiapi,anthropicapi,httpx} (mod) platform.ConnRefused / ErrConnRefused (Phase 4 files)
internal/backend/codex_mcp.go            (mod)  HomeEnv + CodexConfig for the codex run's config lookup (Phase 5 file)
internal/mcpinstall/install.go           (mod)  platform.ClaudeUserConfig / CodexConfig (Phase 5 file)
internal/ui/style/style.go               (mod)  ASCII(), Sym()
internal/ui/pane/model.go                (mod)  learning status through style.Sym
cmd/herdr-dots/engine.go, ui.go, action.go, mcp.go (mod) StartDetached, EngineEndpoint, ipc.Listen/Dial, engineEnv via EnvKey
cmd/herdr-dots/setupmcp.go, memory.go    (mod)  platform.Command/LookPath for claude/codex; EditorCommand + SecureDir for memory edit
cmd/herdr-dots/platform_manifest_test.go (new)  manifest per-platform build and shared runtime commands
herdr-plugin.toml                        (mod)  windows platform, Windows [[build]], 0.6.0
scripts/build.ps1                        (new)  Windows build
.github/workflows/ci.yml                 (new)  macOS/Ubuntu/Windows matrix
.gitattributes                           (new)  LF everywhere
Makefile                                 (mod)  cross target
internal/version/version.go              (mod)  0.6.0
README.md, roadmap, notes                (mod/new) Windows docs, still not done, not run-verified here
```

**Conventions for code steps.** New files are given in full. Changes to existing files are unified diffs: save the block to a file (for example `/tmp/p6-task.patch`) and run `git apply /tmp/p6-task.patch` from the repo root (the replay used `git apply --recount`, which tolerates shifted line numbers). If a hunk doesn't apply because a neighboring line changed after `a47427d`, make the same edit by hand (every hunk shows the exact lines), then run `gofmt -w` on the touched files. A file moved with `git mv` is named in its step. Every code block below was replayed against `a47427d` (Phases 2–5 and the Phase 5 final fix wave merged) in wave order, for darwin, linux/amd64 and windows/amd64, and its darwin tests ran green with `go test -race` (Go 1.25.7). The Windows-only tests compiled (`GOOS=windows go test -c`) but could not run here.

**The cross-OS gate** (each task's verification step spells it out with that task's packages):

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet <task packages>
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" <task packages>
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

---

### Task 1: Verification notes (V1–V8), Windows baseline, user gates

This task produces **findings, not product code**. V1–V8 were answered while planning (table above) from Herdr's documentation, Herdr's source and a live Claude Code run; this task writes them down with their evidence, records which packages fail to compile for Windows at the start of the phase, and lists what only a real Windows machine can confirm.

**Depends on:** none (Wave 1).

**Files:**
- Create: `docs/superpowers/notes/2026-10-08-phase-6-verification.md`

**Interfaces:**
- Consumes: nothing.
- Produces: the "Windows baseline" lists that Plan decision 21 and the Wave 2 gate refer to (any Phase 5 package that fails `GOOS=windows go build`), and the user-gate checklist Task 13 links from the README.

- [ ] **Step 1: Check the preconditions**

Run:

```bash
ls internal/memory internal/council internal/credentials internal/backend/toolloop internal/backend/ollama internal/mcpinstall internal/mcpserver cmd/herdr-dots/setupmcp.go >/dev/null && echo phases-present
grep -n 'const Version' internal/version/version.go
grep -n '^go \|golang.org/x/sys' go.mod
GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./... >/dev/null && echo base-green
```

Expected: `phases-present`, `const Version = "0.5.0"`, `go 1.25.0`, a `golang.org/x/sys` line, `base-green`. If any line is missing, stop and report to the coordinator: Phase 6 ports Phases 1–5 and needs all of them merged.

- [ ] **Step 2: Record the Windows baseline**

Run:

```bash
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep -v '^#' | head -40
```

Expected (replayed at `5863171` and `a47427d`): the first command prints `# herdr-dots/internal/backend`, `# herdr-dots/internal/backend/toolloop`, `# herdr-dots/internal/lockfile`; the second prints the same three plus `# herdr-dots/internal/backend [herdr-dots/internal/credentials.test]`. The third shows the errors: `undefined: syscall.Flock` / `LOCK_EX` / `LOCK_NB` / `LOCK_UN` (lockfile.go), `undefined: syscall.O_NOFOLLOW` (toolloop/jail.go), `unknown field Setpgid` and `undefined: syscall.Kill` (backend/exec.go). `internal/credentials` compiles (Phase 4's `open_other.go`) but its mode-bit check refuses every file on Windows, where Go reports `0666`; Task 8 ports it. No Phase 5 package fails to compile; Task 10 ports their behavior. If anything else shows up (a commit after `a47427d`), list it in the notes (Step 3) and in the wave table's "may still fail" column before Wave 2.

- [ ] **Step 3: Write the notes**

Create `docs/superpowers/notes/2026-10-08-phase-6-verification.md` with this content, pasting the Step 2 output where it says so:

````markdown
# Phase 6 (Windows) verification notes

- **Date:** 2026-10-08
- **Machine:** macOS (darwin/arm64). Windows is cross-compiled and unit-tested here; nothing in this phase has run on Windows on this machine. A real Windows run is a **user gate** (release-gate amendment in `docs/superpowers/decisions/2026-10-08-phase-decisions.md`).
- **Herdr source consulted:** github.com/ogulcancelik/herdr at commit 2563803 (v0.9.3, 2026-10-08), read only.

## V1: Herdr's socket API on Windows

- **Finding:** a named pipe. `HERDR_SOCKET_PATH` is an ordinary file path (default `%APPDATA%\herdr\herdr.sock`; named sessions `...\sessions\<name>\herdr.sock`) where Herdr writes a marker file; the pipe is `\\.\pipe\` followed by that path.
- **Evidence:** herdr.dev/docs/socket-api ("On Unix, that socket is a Unix domain socket. On Windows, it is a named pipe."); herdr.dev/docs/plugins ("Windows clients connect to a named pipe"); `src/ipc.rs` (`connect_local_stream` uses the path as a namespaced pipe name; `bind_local_listener` writes the marker file); `src/platform/windows.rs` (`probe_local_server`: `format!(r"\\.\pipe\{}", path.to_string_lossy())`).
- **Assumption holds (W2):** yes, refined: the env var is a path, so `platform.PipeName` prefixes `\\.\pipe\`.

## V2: manifest syntax for Windows, `.exe` resolution

- **Finding:** every manifest item (`[[build]]`, `[[startup]]`, `[[actions]]`, `[[events]]`, `[[panes]]`, `[[link_handlers]]`) may declare `platforms`, which overrides the top-level list. Duplicate action and pane ids are rejected even when their platforms differ, so only `[[build]]` (no id) is split. `./bin/herdr-dots` is joined to the plugin root; actions and startup run through Rust's `std::process::Command`, which appends `.exe` to an extension-less path that exists with it; panes run through portable-pty's `search_path`, which tries `PATHEXT` extensions. Bare build commands resolve `PATHEXT` shims (`powershell` -> `powershell.exe`).
- **Evidence:** herdr.dev/docs/plugins ("item-level platforms override the top-level list"; "build commands, action commands, and event commands resolve common PATHEXT shims"); `src/app/api/plugins/manifest.rs` (`effective_platforms`, `reject_duplicate_action_ids`, `reject_duplicate_pane_ids`); `src/plugin_command.rs` (`program_for_cwd`, `resolve_windows_program`); `vendor/portable-pty/src/cmdbuilder.rs` (`search_path`); CHANGELOG 0.9.0 ("Windows plugin panes now resolve relative commands from the plugin root").
- **Assumption holds (W11):** yes. Fallback if a real run shows `./bin/herdr-dots` not resolving: make the Windows `[[build]]` also copy `bin\herdr-dots.exe` to `bin\herdr-dots`.

## V3: Claude Code transcripts on Windows

- **Finding:** `%USERPROFILE%\.claude\projects\<encoded cwd>\<session id>.jsonl` (Claude Code uses the OS home; the cwd is encoded with non-alphanumerics replaced by `-`, e.g. `C--Users-x-repo`). Herdr Dots resolves a session id by globbing every project folder, so the encoding is never computed.
- **Not verified here:** whether `pane.get` reports `agent_session` for Claude Code on Windows. Without it, the scrollback reader is used (v1 fallback).
- **Assumption holds:** not live-verified, design unaffected.

## V4: system prompt from a file

- **Finding:** `claude -p --append-system-prompt-file <file>` works in print mode, and `--settings <file>` accepts a JSON file.
- **Evidence:** live run on this machine, Claude Code 2.1.295: with a file saying "reply with exactly the word PINEAPPLE42", `echo hello | claude -p --output-format json --no-session-persistence --strict-mcp-config --settings ./settings.json --append-system-prompt-file ./sp.txt --model haiku --tools ""` returned `PINEAPPLE42`; a missing file fails before any API call with `Error: Append system prompt file not found: ...`.
- **Assumption holds (W7):** yes.

## V5: Herdr config.toml on Windows

- **Finding:** `HERDR_CONFIG_PATH`, else `%XDG_CONFIG_HOME%\herdr\config.toml`, else `%APPDATA%\herdr\config.toml`, else `%USERPROFILE%\AppData\Roaming\herdr\config.toml`, else `~/.config/herdr/config.toml`.
- **Evidence:** `src/config/io.rs` (`config_dir`, `platform_config_dir` for Windows).
- **Not verified here:** `herdr --default-config` on Windows (Herdr Dots doesn't call it; `keys.BuiltinPrefixKeys` is a static list).
- **Assumption holds:** yes.

## V6: Claude Code and Codex on Windows

- **Finding:** Claude Code's user config is `~\.claude.json`; Codex's is `~\.codex\config.toml` (`CODEX_HOME` overrides it; Phase 5's installer and the codex dot run honor `CODEX_HOME` on every OS). `claude` is `claude.exe` (native installer, `%USERPROFILE%\.local\bin`) or `claude.cmd` (npm); `codex` from npm is `codex.cmd`. `platform.LookPath` prefers an `.exe`; `platform.Command` runs a `.cmd` safely.
- **Assumption holds:** not live-verified, handled by design.

## V7: glyphs and ctrl+s in Herdr's ConPTY panes

- **Finding:** Herdr 0.9.1 "Windows local and SSH input preserves Alt combinations, Ctrl+S" (CHANGELOG). Glyph rendering depends on the terminal font and is a user gate; `HERDR_DOTS_ASCII=1` switches to ASCII.
- **Assumption holds:** ctrl+s yes; glyphs not verified.

## V8: plugin environment on Windows

- **Finding:** `HERDR_SOCKET_PATH`, `HERDR_BIN_PATH`, `HERDR_PLUGIN_ID`, `HERDR_PLUGIN_ROOT`, `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR` and `HERDR_PLUGIN_CONTEXT_JSON` are set the same way on every OS.
- **Evidence:** `src/app/api/plugins/env.rs`, `src/app/api/plugins/runtime.rs` (no `cfg` gates).
- **Assumption holds:** yes.

## Windows baseline at the start of Phase 6

Packages failing `GOOS=windows GOARCH=amd64 go build ./...`:

```
(paste Step 2's first command output here)
```

Packages failing `GOOS=windows GOARCH=amd64 go vet ./...`:

```
(paste Step 2's second command output here)
```

Phase 5 packages among them (Plan decision 21): none at `a47427d`; list any that a later commit adds.

Compiles but behaves wrongly on Windows (ported in this phase): `internal/credentials` (mode bits refuse every file; Task 8), Phase 5's `mcpinstall` config paths, `setup-mcp` child commands, the codex run's `HOME` lookup (Task 10), Phase 2's `memory edit` (`sh -c`; Task 10), and every `syscall.ECONNREFUSED` check (Task 8).

## User gates: what only a real Windows machine can confirm

Run on Windows 10 1809+ or Windows 11 with the Herdr Windows beta (preview channel, Herdr >= 0.9.1), Go >= 1.25 and Git for Windows:

- [ ] CI: push the branch to a private remote and see `.github/workflows/ci.yml` green on `windows-latest` (and macOS, Ubuntu).
- [ ] `herdr plugin link .` runs `scripts/build.ps1` and produces `bin\herdr-dots.exe`.
- [ ] Herdr starts the engine at startup; `engine.log` in the session state dir says `endpoint \\.\pipe\herdr-dots-<key>`.
- [ ] `prefix+d`, `prefix+a`, `prefix+shift+s` and the council popup open (`./bin/herdr-dots` resolves to the `.exe`; else apply the V2 fallback).
- [ ] With `claude.exe` and again with npm's `claude.cmd`: attach a dot to a Claude Code pane in `C:\Users\<name with a space>\src\my app`, get a card, send it.
- [ ] The pane's transcript source is Claude's file (not scrollback) when Herdr reports the session.
- [ ] A dot with `run_timeout_seconds = 5` and a slow prompt times out and leaves no `node.exe`/`claude.exe` behind (Task Manager).
- [ ] No console window flashes during dot runs or memory git calls.
- [ ] Council, MCP ask (`herdr-dots setup-mcp --client both`, with npm's `claude.cmd` and `codex.cmd`; the MCP client starts `bin\herdr-dots.exe mcp` and the engine starts detached), the memory screen, and `herdr-dots keys set anthropic` (no echo while typing).
- [ ] `herdr-dots memory edit` opens `%EDITOR%` (for example `notepad`, or `code --wait`) through cmd.exe and applies the edit; the temp folder is gone afterwards.
- [ ] `icacls` on `credentials.toml`, the memory dir and the session dir shows only the user, SYSTEM and Administrators.
- [ ] A `credentials.toml` given `icacls credentials.toml /grant Users:R` is refused with the `icacls` fix, and `herdr-dots keys list` never prints a key.
- [ ] An API dot in a pane at `C:\`, at `%USERPROFILE%` or at a drive root such as `D:\` gets no repo tools ("REPO TOOLS: unavailable"); in a project below the home it reads files but not `.env`, `.ENV`, `.env::$DATA` or `NUL`.
- [ ] A codex dot run with the Dots MCP server installed for Codex (`%USERPROFILE%\.codex\config.toml`) gets `-c mcp_servers.herdr-dots.enabled=false`.
- [ ] A stopped Ollama shows "Ollama isn't running" (Windows reports `WSAECONNREFUSED`), and the providers screen still saves with a network warning.
- [ ] A second Windows user (or a pipe pre-created under `\\.\pipe\herdr-dots-<key>` by another account) can't reach the engine: the UI refuses the pipe instead of talking to it.
- [ ] Glyphs render in Windows Terminal; otherwise `HERDR_DOTS_ASCII=1` gives readable ASCII.
````

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/notes/2026-10-08-phase-6-verification.md
git commit -m "docs: Phase 6 verification notes, Windows baseline and user gates"
```

---

### Task 2: `internal/platform` core and process trees

**Depends on:** none (Wave 1).

**Files:**
- Create: `internal/platform/platform.go`, `internal/platform/cmdline.go`, `internal/platform/platform_unix.go`, `internal/platform/platform_windows.go`
- Test: `internal/platform/platform_test.go`, `internal/platform/platform_unix_test.go`, `internal/platform/platform_windows_test.go`
- Modify: `internal/backend/exec.go`, `internal/backend/exec_test.go`
- Test: `internal/backend/exec_unix_test.go`, `internal/backend/exec_windows_test.go` (the two `sh`-based v1 tests move into the Unix file)

**Interfaces:**
- Consumes: `golang.org/x/sys/windows` (already in `go.mod`; do not edit `go.mod` in this task, Task 3 runs `go mod tidy`).
- Produces (package `platform`, every OS):
  - `var ErrCmdTooLong, ErrUnsafeBatchArg error`; `const MaxInlinePrompt = 4000`; `const MaxBatchCmdLine = 8191`
  - `func HerdrConfigPath(getenv func(string) string, home string) string`
  - `func ClaudeProjectsDir(home string) string`, `func CodexSessionsDir(home string) string`, `func ClaudeUserConfig(home string) string`, `func CodexConfig(home string) string`
  - `func IsBatchFile(path string) bool`; `func BatchCmdLine(comspec, script string, args []string) (string, error)`
  - `const DevNull` (`/dev/null` or `NUL`); `const HomeEnv` (`HOME` or `USERPROFILE`; Plan decision 29); `func EnvKey(name string) string`; `func LookPath(name string) (string, error)`
  - `func Command(name string, args []string) (*exec.Cmd, error)`; `func CommandContext(ctx context.Context, name string, args []string) (*exec.Cmd, error)`
  - `func HideWindow(cmd *exec.Cmd)`; `func StartDetached(cmd *exec.Cmd) (*os.Process, error)`
  - `type Tree`; `func StartTree(cmd *exec.Cmd) (*Tree, error)`; `func (*Tree) Kill() error`; `func (*Tree) Close() error`
  - `var ErrConnRefused error` (`syscall.ECONNREFUSED` or `windows.WSAECONNREFUSED`; Plan decision 26); `func ConnRefused(err error) bool`
- Produces (package `backend`): `Cmd.Cleanup func()`; `func (c Cmd) Close()`; `func Preflight(err error) bool`; `ExecRunner.Run` keeps its signature and now kills the whole tree on both OSes; `CleanEnv` compares names in `platform.EnvKey` form.

- [ ] **Step 1: Write the failing tests**

`internal/platform/platform_test.go`:

```go
package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestHerdrConfigPathOverridesThenDefault(t *testing.T) {
	home := filepath.Join(t.TempDir(), "José Núñez")
	if got := HerdrConfigPath(envMap(map[string]string{"HERDR_CONFIG_PATH": "/custom.toml", "XDG_CONFIG_HOME": "/x"}), home); got != "/custom.toml" {
		t.Fatalf("HERDR_CONFIG_PATH must win: %s", got)
	}
	if got := HerdrConfigPath(envMap(map[string]string{"XDG_CONFIG_HOME": "/x"}), home); got != filepath.Join("/x", "herdr", "config.toml") {
		t.Fatalf("XDG_CONFIG_HOME: %s", got)
	}
	got := HerdrConfigPath(envMap(nil), home)
	if !strings.HasSuffix(got, filepath.Join("herdr", "config.toml")) || !strings.HasPrefix(got, home) {
		t.Fatalf("default %s not under home %s", got, home)
	}
}

func TestWindowsHerdrConfigDirRules(t *testing.T) {
	home := filepath.Join("Users", "José Núñez")
	appdata := filepath.Join("Users", "José Núñez", "AppData", "Roaming")
	if got := windowsHerdrConfigDir(envMap(map[string]string{"APPDATA": appdata, "USERPROFILE": "/ignored"}), home); got != filepath.Join(appdata, "herdr") {
		t.Fatalf("APPDATA rule: %s", got)
	}
	if got := windowsHerdrConfigDir(envMap(map[string]string{"USERPROFILE": home}), "/other"); got != filepath.Join(home, "AppData", "Roaming", "herdr") {
		t.Fatalf("USERPROFILE rule: %s", got)
	}
	if got := windowsHerdrConfigDir(envMap(nil), home); got != filepath.Join(home, ".config", "herdr") {
		t.Fatalf("home fallback: %s", got)
	}
}

func TestClientPaths(t *testing.T) {
	home := filepath.Join(t.TempDir(), "my home")
	for got, want := range map[string]string{
		ClaudeProjectsDir(home): filepath.Join(home, ".claude", "projects"),
		CodexSessionsDir(home):  filepath.Join(home, ".codex", "sessions"),
		ClaudeUserConfig(home):  filepath.Join(home, ".claude.json"),
		CodexConfig(home):       filepath.Join(home, ".codex", "config.toml"),
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestIsBatchFile(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\Users\x\AppData\Roaming\npm\claude.cmd`: true,
		`C:\tools\CODEX.CMD`:                        true,
		`C:\tools\run.bat`:                          true,
		`C:\Users\x\.local\bin\claude.exe`:          false,
		"/usr/local/bin/claude":                     false,
		`C:\dir.cmd\claude.exe`:                     false,
	} {
		if IsBatchFile(p) != want {
			t.Errorf("IsBatchFile(%q) = %v", p, !want)
		}
	}
}

func TestFindExecutablePrefersFirstAbsoluteDirWithTheFile(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(b, "claude.exe"), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	isFile := func(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }
	if got := findExecutable("claude", []string{"", "relative", a, b}, ".exe", isFile); got != filepath.Join(b, "claude.exe") {
		t.Fatalf("got %q", got)
	}
	if got := findExecutable("codex", []string{a, b}, ".exe", isFile); got != "" {
		t.Fatalf("missing program found: %q", got)
	}
	wd, _ := os.Getwd()
	if rel, err := filepath.Rel(wd, b); err == nil {
		if got := findExecutable("claude", []string{rel, a}, ".exe", isFile); got != "" {
			t.Fatalf("relative PATH entry %q must be skipped, got %q", rel, got)
		}
	}
}

func TestBatchCmdLineQuotesEveryArgument(t *testing.T) {
	line, err := BatchCmdLine(`C:\Windows\system32\cmd.exe`, `C:\Users\José Núñez\AppData\Roaming\npm\claude.cmd`,
		[]string{"-p", "--tools", "", "--settings", `C:\Users\José Núñez\AppData\Local\Temp\herdr-dots-run-1\settings.json`, "a&b|c<d>e^f(g)", `C:\repo\`})
	if err != nil {
		t.Fatal(err)
	}
	want := `C:\Windows\system32\cmd.exe /d /s /c ""C:\Users\José Núñez\AppData\Roaming\npm\claude.cmd" "-p" "--tools" "" "--settings" "C:\Users\José Núñez\AppData\Local\Temp\herdr-dots-run-1\settings.json" "a&b|c<d>e^f(g)" "C:\repo\\""`
	if line != want {
		t.Fatalf("line\n got %s\nwant %s", line, want)
	}
}

func TestBatchCmdLineRefusesCharactersCmdCantCarry(t *testing.T) {
	for _, bad := range []string{`say "hi"`, "100%", "%PATH%", "wow!", "two\nlines", "cr\r"} {
		if _, err := BatchCmdLine("cmd.exe", `C:\npm\claude.cmd`, []string{"-p", bad}); !errors.Is(err, ErrUnsafeBatchArg) {
			t.Errorf("%q: err = %v, want ErrUnsafeBatchArg", bad, err)
		}
	}
}

func TestBatchCmdLineRefusesOverLongLinesWithAHint(t *testing.T) {
	_, err := BatchCmdLine("cmd.exe", `C:\npm\claude.cmd`, []string{strings.Repeat("x", 8200)})
	if !errors.Is(err, ErrCmdTooLong) || !strings.Contains(err.Error(), "claude.cmd") || !strings.Contains(err.Error(), "claude.exe") || !strings.Contains(err.Error(), "> 8191") {
		t.Fatalf("err = %v", err)
	}
	if _, err := BatchCmdLine("cmd.exe", `C:\npm\claude.cmd`, []string{strings.Repeat("x", 8000)}); err != nil {
		t.Fatalf("8000 characters fit: %v", err)
	}
}

func TestConnRefusedRecognisesAClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, err = net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil || !ConnRefused(err) {
		t.Fatalf("dial a closed port: %v (ConnRefused=%v)", err, ConnRefused(err))
	}
	if ConnRefused(errors.New("timeout")) {
		t.Fatal("other errors are not refusals")
	}
	if !ConnRefused(fmt.Errorf("kept: %w", ErrConnRefused)) {
		t.Fatal("ErrConnRefused must count as a refusal")
	}
}
```

`internal/platform/platform_unix_test.go`:

```go
//go:build unix

package platform

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestUnixConstants(t *testing.T) {
	if DevNull != "/dev/null" || EnvKey("Herdr_Pane_Id") != "Herdr_Pane_Id" || HomeEnv != "HOME" {
		t.Fatalf("DevNull %q, EnvKey must keep case on Unix", DevNull)
	}
}

func TestTreeKillEndsTheWholeProcessGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & sleep 30")
	var out bytes.Buffer
	cmd.Stdout = &out // the background sleep holds the pipe: Wait returns only when it dies too
	tree, err := StartTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if err := tree.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("tree took %v to die", d)
	}
}

func TestStartDetachedStartsTheProcess(t *testing.T) {
	p, err := StartDetached(exec.Command("sh", "-c", "exit 0"))
	if err != nil {
		t.Fatal(err)
	}
	if st, err := p.Wait(); err != nil || !st.Success() {
		t.Fatalf("detached child: %v %v", st, err)
	}
}

func TestCommandResolvesAtStartLikeV1(t *testing.T) {
	cmd, err := Command("definitely-not-a-program-xyz", []string{"a"})
	if err != nil || cmd.Start() == nil {
		t.Fatalf("Command must defer the not-found error to Start: %v", err)
	}
}

func TestCommandContextIsBoundToTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd, err := CommandContext(ctx, "sleep", []string{"5"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if cmd.Run() == nil || time.Since(start) > 2*time.Second {
		t.Fatal("a cancelled context must stop the command")
	}
}
```

`internal/platform/platform_windows_test.go` (runs only on Windows; Review Focus 2 and 3):

```go
//go:build windows

package platform

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsConstants(t *testing.T) {
	if DevNull != "NUL" || EnvKey("Herdr_Pane_Id") != "HERDR_PANE_ID" || HomeEnv != "USERPROFILE" {
		t.Fatalf("DevNull %q; EnvKey must fold case on Windows", DevNull)
	}
}

func TestLookPathPrefersExeOverAnEarlierCmdShim(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(a, "claude.cmd"), []byte("@echo off\r\n"), 0o700)
	os.WriteFile(filepath.Join(b, "claude.exe"), []byte("MZ"), 0o700)
	t.Setenv("PATH", a+string(os.PathListSeparator)+b)
	got, err := LookPath("claude")
	if err != nil || !strings.EqualFold(got, filepath.Join(b, "claude.exe")) {
		t.Fatalf("LookPath = %q, %v", got, err)
	}
	os.Remove(filepath.Join(b, "claude.exe"))
	got, err = LookPath("claude")
	if err != nil || !strings.EqualFold(got, filepath.Join(a, "claude.cmd")) {
		t.Fatalf("without an .exe LookPath = %q, %v", got, err)
	}
}

// echoShim puts echo-args.cmd on PATH; it prints its arguments in brackets.
func echoShim(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "echo-args.cmd"), []byte("@echo off\r\necho [%*]\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCommandPassesShimArgumentsLiterally(t *testing.T) {
	echoShim(t)
	cmd, err := Command("echo-args", []string{"a&b", "c d", "x & echo INJECTED", "p|q", ""})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != `["a&b" "c d" "x & echo INJECTED" "p|q" ""]` {
		t.Fatalf("shim saw %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == "INJECTED" {
			t.Fatal("cmd.exe ran an injected command")
		}
	}
}

func TestCommandRefusesUnsafeShimArguments(t *testing.T) {
	echoShim(t)
	if _, err := Command("echo-args", []string{"100%"}); !errors.Is(err, ErrUnsafeBatchArg) {
		t.Fatalf("err = %v, want ErrUnsafeBatchArg", err)
	}
	if _, err := Command("echo-args", []string{strings.Repeat("y", 9000)}); !errors.Is(err, ErrCmdTooLong) {
		t.Fatalf("err = %v, want ErrCmdTooLong", err)
	}
}

// pingTree starts cmd.exe running ping as a grandchild that shares stdout, so
// Wait only returns once the grandchild is gone too.
func pingTree(t *testing.T) (*exec.Cmd, *Tree) {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1")
	var out bytes.Buffer
	cmd.Stdout = &out
	tree, err := StartTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // let cmd.exe start ping
	return cmd, tree
}

func TestTreeKillEndsGrandchildren(t *testing.T) {
	cmd, tree := pingTree(t)
	defer tree.Close()
	start := time.Now()
	if err := tree.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("process tree took %v to die", d)
	}
}

func TestTreeCloseKillsWhatIsLeft(t *testing.T) {
	cmd, tree := pingTree(t)
	start := time.Now()
	if err := tree.Close(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("KILL_ON_JOB_CLOSE took %v", d)
	}
}

// TestDetachHelper is the parent process for the test below: it starts a
// detached ping, prints its pid and exits at once.
func TestDetachHelper(t *testing.T) {
	if os.Getenv("PLATFORM_DETACH_HELPER") != "1" {
		t.Skip("helper process for TestStartDetachedSurvivesItsParent")
	}
	p, err := StartDetached(exec.Command("ping", "-n", "30", "127.0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("PID " + strconv.Itoa(p.Pid) + "\n")
	_ = p.Release()
}

func TestStartDetachedSurvivesItsParent(t *testing.T) {
	helper := exec.Command(os.Args[0], "-test.run=^TestDetachHelper$", "-test.count=1", "-test.v")
	helper.Env = append(os.Environ(), "PLATFORM_DETACH_HELPER=1")
	out, err := helper.Output()
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	pid := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PID "); ok {
			pid, _ = strconv.Atoi(v)
		}
	}
	if pid == 0 {
		t.Fatalf("no pid in helper output:\n%s", out)
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatalf("detached process %d is gone: %v", pid, err)
	}
	defer windows.CloseHandle(h)
	defer windows.TerminateProcess(h, 1)
	if ev, _ := windows.WaitForSingleObject(h, 0); ev != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("detached process %d exited with its parent", pid)
	}
}

func TestCommandContextRunsAShimUnderTheContext(t *testing.T) {
	echoShim(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, err := CommandContext(ctx, "echo-args", []string{"x y"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != `["x y"]` {
		t.Fatalf("%q %v", out, err)
	}
}
```

`internal/backend/exec_test.go` (the `sh`-based tests leave for the Unix file; new tests for the case rule, `Cmd.Close` and `Preflight`):

```diff
--- a/internal/backend/exec_test.go
+++ b/internal/backend/exec_test.go
@@ -1,11 +1,13 @@
 package backend
 
 import (
-	"context"
+	"errors"
+	"fmt"
 	"slices"
 	"strings"
 	"testing"
-	"time"
+
+	"herdr-dots/internal/platform"
 )
 
 func TestCleanEnvStripsHerdrAndClaudeVars(t *testing.T) {
@@ -25,30 +27,32 @@ func TestCleanEnvNeverReturnsNil(t *testing.T) {
 	}
 }
 
-func TestExecRunnerPassesStdinDirEnv(t *testing.T) {
-	dir := t.TempDir()
-	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{
-		Name: "sh", Args: []string{"-c", `printf '%s|%s|' "$(pwd)" "$DOTS_X"; cat`},
-		Dir: dir, Env: []string{"DOTS_X=yes", "PATH=/usr/bin:/bin"}, Stdin: "from-stdin",
-	})
-	if err != nil {
-		t.Fatal(err)
+func TestCleanEnvFoldsCaseWhereTheOSDoes(t *testing.T) {
+	in := []string{"Herdr_Pane_Id=w1:p1", "claudecode=1", "Claude_Code_Entrypoint=cli", "Path=/bin"}
+	got := CleanEnv(in)
+	if platform.EnvKey("a") == "A" { // Windows: one variable whatever its case
+		if !slices.Equal(got, []string{"Path=/bin"}) {
+			t.Fatalf("Windows CleanEnv = %v", got)
+		}
+		return
 	}
-	if !strings.HasSuffix(string(out), "|yes|from-stdin") || !strings.Contains(string(out), dir[len(dir)-8:]) {
-		t.Fatalf("out = %q", out)
+	if !slices.Equal(got, in) { // Unix: different names, kept as in v1
+		t.Fatalf("Unix CleanEnv = %v", got)
 	}
 }
 
-func TestExecRunnerKillsProcessGroupOnTimeout(t *testing.T) {
-	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
-	defer cancel()
-	start := time.Now()
-	_, _, err := ExecRunner{}.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", "sleep 30 & sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}})
-	if err == nil {
-		t.Fatal("want timeout error")
+func TestCmdCloseRunsCleanupOnce(t *testing.T) {
+	Cmd{}.Close() // nil Cleanup is fine
+	n := 0
+	Cmd{Cleanup: func() { n++ }}.Close()
+	if n != 1 {
+		t.Fatalf("cleanup ran %d times", n)
 	}
-	if time.Since(start) > 3*time.Second {
-		t.Fatalf("took %v; process group not killed", time.Since(start))
+}
+
+func TestPreflightRecognisesShimErrors(t *testing.T) {
+	if !Preflight(fmt.Errorf("x: %w", platform.ErrCmdTooLong)) || !Preflight(platform.ErrUnsafeBatchArg) || Preflight(errors.New("exit 1")) {
+		t.Fatal("Preflight")
 	}
 }
 
```

`internal/backend/exec_unix_test.go` (the two v1 tests, unchanged):

```go
//go:build unix

package backend

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerPassesStdinDirEnv(t *testing.T) {
	dir := t.TempDir()
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", `printf '%s|%s|' "$(pwd)" "$DOTS_X"; cat`},
		Dir: dir, Env: []string{"DOTS_X=yes", "PATH=/usr/bin:/bin"}, Stdin: "from-stdin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "|yes|from-stdin") || !strings.Contains(string(out), dir[len(dir)-8:]) {
		t.Fatalf("out = %q", out)
	}
}

func TestExecRunnerKillsProcessGroupOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := ExecRunner{}.Run(ctx, Cmd{Name: "sh", Args: []string{"-c", "sleep 30 & sleep 30"}, Env: []string{"PATH=/usr/bin:/bin"}})
	if err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v; process group not killed", time.Since(start))
	}
}
```

`internal/backend/exec_windows_test.go`:

```go
//go:build windows

package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform"
)

func TestExecRunnerPassesStdinDirEnvOnWindows(t *testing.T) {
	dir := t.TempDir()
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{
		Name: "cmd.exe", Args: []string{"/d", "/c", "cd & echo %DOTS_X% & sort"},
		Dir: dir, Env: []string{"DOTS_X=yes", "PATH=" + os.Getenv("PATH")}, Stdin: "from-stdin\r\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "yes") || !strings.Contains(s, "from-stdin") || !strings.Contains(strings.ToLower(s), strings.ToLower(filepath.Base(dir))) {
		t.Fatalf("out = %q", s)
	}
}

func TestExecRunnerKillsTheJobOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := ExecRunner{}.Run(ctx, Cmd{Name: "cmd.exe", Args: []string{"/d", "/c", "ping -n 30 127.0.0.1"}, Env: []string{"PATH=" + os.Getenv("PATH")}})
	if err == nil {
		t.Fatal("want timeout error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v; the ping grandchild survived the timeout", d)
	}
}

func TestExecRunnerRefusesUnsafeShimArgsBeforeStarting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fakecli.cmd"), []byte("@echo off\r\necho ran\r\n"), 0o700)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, _, err := ExecRunner{}.Run(context.Background(), Cmd{Name: "fakecli", Args: []string{"--model", "50%"}, Env: os.Environ()})
	if !errors.Is(err, platform.ErrUnsafeBatchArg) || len(out) != 0 {
		t.Fatalf("err = %v out = %q", err, out)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/platform/ ./internal/backend/`
Expected: FAIL to compile (`undefined: HerdrConfigPath`, `undefined: findExecutable`, `undefined: platform.EnvKey`, `c.Close undefined`, `undefined: Preflight`).

- [ ] **Step 3: Implement**

`internal/platform/platform.go`:

```go
// Package platform holds herdr-dots' operating-system differences: where
// other programs keep their files, how child processes start and die, how
// command lines reach cmd.exe shims, file privacy and named pipes. Business
// logic calls these helpers instead of branching on runtime.GOOS; each helper
// has a Unix file and a Windows file behind build tags.
package platform

import (
	"errors"
	"path/filepath"
)

var (
	// ErrCmdTooLong means a command line exceeds cmd.exe's limit.
	ErrCmdTooLong = errors.New("command line too long")
	// ErrUnsafeBatchArg means an argument holds a character cmd.exe would
	// interpret (" % ! or a line break) and can't be passed to a .cmd shim.
	ErrUnsafeBatchArg = errors.New("argument can't be passed safely to a .cmd/.bat program")
)

// MaxInlinePrompt is the longest system prompt passed as a command-line
// argument; longer prompts go through a private temp file on every OS.
const MaxInlinePrompt = 4000

// MaxBatchCmdLine is cmd.exe's command-line limit, in UTF-16 code units.
const MaxBatchCmdLine = 8191

// HerdrConfigPath is Herdr's config.toml: HERDR_CONFIG_PATH, then
// $XDG_CONFIG_HOME/herdr/config.toml, then the OS default (Unix:
// ~/.config/herdr; Windows: %APPDATA%\herdr). These are Herdr's own rules.
func HerdrConfigPath(getenv func(string) string, home string) string {
	if p := getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "herdr", "config.toml")
	}
	return filepath.Join(herdrConfigDir(getenv, home), "config.toml")
}

// unixHerdrConfigDir is Herdr's default config dir on macOS and Linux.
func unixHerdrConfigDir(home string) string {
	return filepath.Join(home, ".config", "herdr")
}

// windowsHerdrConfigDir is Herdr's default config dir on Windows:
// %APPDATA%\herdr, then %USERPROFILE%\AppData\Roaming\herdr, then
// ~/.config/herdr.
func windowsHerdrConfigDir(getenv func(string) string, home string) string {
	if a := getenv("APPDATA"); a != "" {
		return filepath.Join(a, "herdr")
	}
	if u := getenv("USERPROFILE"); u != "" {
		return filepath.Join(u, "AppData", "Roaming", "herdr")
	}
	return unixHerdrConfigDir(home)
}

// ClaudeProjectsDir holds Claude Code's transcripts, one folder per project,
// on every OS (Claude Code uses the user's home, %USERPROFILE% on Windows).
func ClaudeProjectsDir(home string) string { return filepath.Join(home, ".claude", "projects") }

// CodexSessionsDir holds Codex rollout files (YYYY/MM/DD/rollout-*.jsonl).
func CodexSessionsDir(home string) string { return filepath.Join(home, ".codex", "sessions") }

// ClaudeUserConfig is Claude Code's user config (MCP servers at user scope).
func ClaudeUserConfig(home string) string { return filepath.Join(home, ".claude.json") }

// CodexConfig is Codex's config.toml.
func CodexConfig(home string) string { return filepath.Join(home, ".codex", "config.toml") }
```

`internal/platform/cmdline.go`:

```go
package platform

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// IsBatchFile reports whether path names a .cmd or .bat script, which
// Windows runs through cmd.exe (npm installs claude and codex this way).
func IsBatchFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(baseName(path)))
	return ext == ".cmd" || ext == ".bat"
}

// baseName is the last element of a path written with / or \ separators,
// whatever OS runs the code.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// findExecutable returns the first dirs[i]/name+ext that isFile accepts.
// Relative PATH entries are skipped, so a program in the current directory
// can never shadow the real one.
func findExecutable(name string, dirs []string, ext string, isFile func(string) bool) string {
	for _, d := range dirs {
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		if c := filepath.Join(d, name+ext); isFile(c) {
			return c
		}
	}
	return ""
}

// quoteBatchArg quotes one argument for a command line that cmd.exe parses
// and then hands to a program using the usual Windows argv rules. Inside
// double quotes cmd.exe treats & | < > ^ ( ) literally; " % ! and line
// breaks can't be made safe, so they are refused. Backslashes before the
// closing quote are doubled so they stay literal.
func quoteBatchArg(a string) (string, error) {
	if strings.ContainsAny(a, "\"%!\r\n\x00") {
		shown := a
		if r := []rune(shown); len(r) > 60 {
			shown = string(r[:60]) + "…"
		}
		return "", fmt.Errorf("%w: %q holds \", %%, ! or a line break", ErrUnsafeBatchArg, shown)
	}
	trail := len(a) - len(strings.TrimRight(a, `\`))
	return `"` + a + strings.Repeat(`\`, trail) + `"`, nil
}

// BatchCmdLine builds the command line that runs a .cmd/.bat script through
// cmd.exe with every argument quoted:
//
//	<comspec> /d /s /c ""<script>" "<arg1>" "<arg2>""
//
// /d skips AutoRun commands; /s makes cmd.exe strip only the outer quotes.
// It fails with ErrUnsafeBatchArg or, above MaxBatchCmdLine UTF-16 code units,
// ErrCmdTooLong with a hint naming the script.
func BatchCmdLine(comspec, script string, args []string) (string, error) {
	var b strings.Builder
	if strings.ContainsAny(comspec, " \t") {
		b.WriteString(`"` + comspec + `"`)
	} else {
		b.WriteString(comspec)
	}
	b.WriteString(` /d /s /c "`)
	q, err := quoteBatchArg(script)
	if err != nil {
		return "", err
	}
	b.WriteString(q)
	for _, a := range args {
		q, err := quoteBatchArg(a)
		if err != nil {
			return "", err
		}
		b.WriteString(" " + q)
	}
	b.WriteString(`"`)
	line := b.String()
	if n := len(utf16.Encode([]rune(line))); n > MaxBatchCmdLine {
		name := baseName(script)
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		return "", fmt.Errorf("%w for %s (%d > %d): install the native %s.exe or shorten the dot's backend args",
			ErrCmdTooLong, name, n, MaxBatchCmdLine, stem)
	}
	return line, nil
}
```

`internal/platform/platform_unix.go`:

```go
//go:build unix

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// DevNull is the null device, for git's -c core.hooksPath.
const DevNull = "/dev/null"

// HomeEnv is the variable that names the user's home directory, the one
// os.UserHomeDir and child CLIs (codex) read.
const HomeEnv = "HOME"

func herdrConfigDir(_ func(string) string, home string) string { return unixHerdrConfigDir(home) }

// EnvKey is the form environment variable names are compared in: as-is on
// Unix, where names are case-sensitive.
func EnvKey(name string) string { return name }

// LookPath finds a program on PATH (exec.LookPath).
func LookPath(name string) (string, error) { return exec.LookPath(name) }

// Command is exec.Command; the program is resolved at Start, as in v1.
func Command(name string, args []string) (*exec.Cmd, error) { return exec.Command(name, args...), nil }

// CommandContext is Command bound to ctx (exec.CommandContext).
func CommandContext(ctx context.Context, name string, args []string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, name, args...), nil
}

// HideWindow keeps a child from opening a console window. Unix: nothing to do.
func HideWindow(*exec.Cmd) {}

// StartDetached starts cmd in a new session so it outlives the hook or pane
// that launched it, and returns the started process.
func StartDetached(cmd *exec.Cmd) (*os.Process, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// Tree is a started child process together with everything it spawns.
type Tree struct{ pid int }

// StartTree starts cmd in its own process group, so Kill ends the CLI and
// every process it started.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Tree{pid: cmd.Process.Pid}, nil
}

// Kill sends SIGKILL to the whole process group.
func (t *Tree) Kill() error { return syscall.Kill(-t.pid, syscall.SIGKILL) }

// Close releases the tree. Unix: nothing to release.
func (t *Tree) Close() error { return nil }

// ErrConnRefused is the error a refused dial carries on this OS, for code
// that keeps an error's identity while dropping its text (httpx scrubbing).
var ErrConnRefused error = syscall.ECONNREFUSED

// ConnRefused reports whether a dial failed because nothing listens there.
func ConnRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
```

`internal/platform/platform_windows.go`:

```go
//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DevNull is the null device, for git's -c core.hooksPath.
const DevNull = "NUL"

// HomeEnv is the variable that names the user's home directory, the one
// os.UserHomeDir and child CLIs (codex) read.
const HomeEnv = "USERPROFILE"

func herdrConfigDir(getenv func(string) string, home string) string {
	return windowsHerdrConfigDir(getenv, home)
}

// EnvKey is the form environment variable names are compared in: upper case
// on Windows, where Herdr_Pane_Id and HERDR_PANE_ID are the same variable.
func EnvKey(name string) string { return strings.ToUpper(name) }

// LookPath finds a program on PATH, preferring name.exe in any PATH directory
// over a .cmd/.bat shim found earlier (exec.LookPath picks by directory
// first). Names with a directory or an extension go straight to exec.LookPath.
func LookPath(name string) (string, error) {
	if strings.ContainsAny(name, `/\:`) || filepath.Ext(name) != "" {
		return exec.LookPath(name)
	}
	if p := findExecutable(name, filepath.SplitList(os.Getenv("PATH")), ".exe", isRegularFile); p != "" {
		return p, nil
	}
	return exec.LookPath(name)
}

func isRegularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func comspec() string {
	if c := os.Getenv("ComSpec"); c != "" {
		return c
	}
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
}

// Command builds the exec.Cmd for name. A .cmd/.bat shim runs through
// cmd.exe with a command line built by BatchCmdLine, which refuses unsafe
// arguments and over-long lines before anything starts.
func Command(name string, args []string) (*exec.Cmd, error) {
	return CommandContext(context.Background(), name, args)
}

// CommandContext is Command bound to ctx (exec.CommandContext).
func CommandContext(ctx context.Context, name string, args []string) (*exec.Cmd, error) {
	path, err := LookPath(name)
	if err != nil {
		return exec.CommandContext(ctx, name, args...), nil // Start reports "not found", as on Unix
	}
	if !IsBatchFile(path) {
		return exec.CommandContext(ctx, path, args...), nil
	}
	shell := comspec()
	line, err := BatchCmdLine(shell, path, args)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	return cmd, nil
}

// HideWindow keeps a console child of the (windowless) engine from opening a
// console window.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	cmd.SysProcAttr.HideWindow = true
}

const detachFlags = windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW

// StartDetached starts cmd with its own hidden console and process group, so
// it survives the hook or pane that launched it and its own console children
// never open windows. It first asks to break away from any job the caller is
// in; a job that forbids breakaway makes CreateProcess fail with access
// denied, and the start is retried inside the job.
func StartDetached(cmd *exec.Cmd) (*os.Process, error) {
	again := exec.Command(cmd.Path, cmd.Args[1:]...)
	again.Dir, again.Env = cmd.Dir, cmd.Env
	again.Stdin, again.Stdout, again.Stderr = cmd.Stdin, cmd.Stdout, cmd.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachFlags | windows.CREATE_BREAKAWAY_FROM_JOB, HideWindow: true}
	err := cmd.Start()
	if err == nil {
		return cmd.Process, nil
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return nil, err
	}
	again.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachFlags, HideWindow: true}
	if err := again.Start(); err != nil {
		return nil, err
	}
	return again.Process, nil
}

// Tree is a started child process together with everything it spawns: they
// all live in one Job Object.
type Tree struct{ job windows.Handle }

// StartTree starts cmd suspended, puts it in a new Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, then resumes it, so no grandchild can
// escape the job. Kill terminates the job; Close (and an engine crash, which
// closes the handle) kills whatever is still running in it.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	HideWindow(cmd)
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	fail := func(err error) (*Tree, error) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		windows.CloseHandle(job)
		return nil, err
	}
	pid := uint32(cmd.Process.Pid)
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return fail(fmt.Errorf("open child process: %w", err))
	}
	err = windows.AssignProcessToJobObject(job, h)
	windows.CloseHandle(h)
	if err != nil {
		return fail(fmt.Errorf("assign child to job: %w", err))
	}
	if err := resumeProcess(pid); err != nil {
		return fail(fmt.Errorf("resume child: %w", err))
	}
	return &Tree{job: job}, nil
}

// resumeProcess resumes the suspended primary thread of pid.
func resumeProcess(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	for err = windows.Thread32First(snap, &te); err == nil; err = windows.Thread32Next(snap, &te) {
		if te.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(th)
		windows.CloseHandle(th)
		return err
	}
	return errors.New("primary thread not found")
}

// Kill terminates every process in the job.
func (t *Tree) Kill() error { return windows.TerminateJobObject(t.job, 1) }

// Close closes the job handle; KILL_ON_JOB_CLOSE ends anything still running.
func (t *Tree) Close() error { return windows.CloseHandle(t.job) }

// ErrConnRefused is the error a refused dial carries on this OS, for code
// that keeps an error's identity while dropping its text (httpx scrubbing).
var ErrConnRefused error = windows.WSAECONNREFUSED

// ConnRefused reports whether a dial failed because nothing listens there
// (Winsock's WSAECONNREFUSED, which syscall.ECONNREFUSED doesn't match).
func ConnRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}
```

`internal/backend/exec.go`:

```diff
--- a/internal/backend/exec.go
+++ b/internal/backend/exec.go
@@ -3,12 +3,12 @@ package backend
 import (
 	"bytes"
 	"context"
+	"errors"
 	"fmt"
-	"os/exec"
 	"strings"
-	"syscall"
 
 	"herdr-dots/internal/dots"
+	"herdr-dots/internal/platform"
 )
 
 type Cmd struct {
@@ -17,49 +17,74 @@ type Cmd struct {
 	Dir   string
 	Env   []string
 	Stdin string
+	// Cleanup removes files BuildCmd wrote for this command (a long system
+	// prompt); nil when there are none. Callers run it through Close.
+	Cleanup func()
+}
+
+// Close runs Cleanup when it is set; safe on any Cmd.
+func (c Cmd) Close() {
+	if c.Cleanup != nil {
+		c.Cleanup()
+	}
 }
 
 type Runner interface {
 	Run(ctx context.Context, c Cmd) (stdout, stderr []byte, err error)
 }
 
-// ExecRunner runs commands in their own process group so a timeout kills the
-// CLI and everything it spawned.
+// ExecRunner runs commands as a process tree (a process group on Unix, a Job
+// Object on Windows) so a timeout kills the CLI and everything it spawned.
+// On Windows a .cmd/.bat shim runs through cmd.exe with every argument
+// quoted; an argument cmd.exe can't carry, or a line over its limit, fails
+// before anything starts (platform.ErrUnsafeBatchArg, platform.ErrCmdTooLong).
 type ExecRunner struct{}
 
 func (ExecRunner) Run(ctx context.Context, c Cmd) ([]byte, []byte, error) {
-	cmd := exec.Command(c.Name, c.Args...)
+	cmd, err := platform.Command(c.Name, c.Args)
+	if err != nil {
+		return nil, nil, err
+	}
 	cmd.Dir = c.Dir
 	cmd.Env = c.Env
 	cmd.Stdin = strings.NewReader(c.Stdin)
 	var out, errb bytes.Buffer
 	cmd.Stdout = &out
 	cmd.Stderr = &errb
-	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
-	if err := cmd.Start(); err != nil {
+	tree, err := platform.StartTree(cmd)
+	if err != nil {
 		return nil, nil, err
 	}
+	defer tree.Close()
 	done := make(chan error, 1)
 	go func() { done <- cmd.Wait() }()
 	select {
 	case err := <-done:
 		return out.Bytes(), errb.Bytes(), err
 	case <-ctx.Done():
-		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
+		_ = tree.Kill()
 		<-done
 		return out.Bytes(), errb.Bytes(), ctx.Err()
 	}
 }
 
+// Preflight reports whether err stopped a command before it started (a
+// Windows .cmd shim argument or length problem); its message is the fix.
+func Preflight(err error) bool {
+	return errors.Is(err, platform.ErrCmdTooLong) || errors.Is(err, platform.ErrUnsafeBatchArg)
+}
+
 // CleanEnv drops variables that would make a dot's CLI talk to Herdr or think
 // it runs nested inside another Claude Code session. Drops HERDR_*, CLAUDECODE,
 // CLAUDE_CODE_ENTRYPOINT, and CLAUDE_CODE_CHILD_SESSION, but preserves other
 // CLAUDE_* variables that carry auth and config (CLAUDE_CONFIG_DIR, etc.).
+// Names compare in platform.EnvKey form, so case-insensitively on Windows.
 func CleanEnv(env []string) []string {
 	// Non-nil on purpose: a nil exec.Cmd.Env inherits the full parent environment.
 	out := make([]string, 0, len(env))
 	for _, kv := range env {
-		k, _, _ := strings.Cut(kv, "=")
+		name, _, _ := strings.Cut(kv, "=")
+		k := platform.EnvKey(name)
 		if strings.HasPrefix(k, "HERDR_") || k == "CLAUDECODE" || k == "CLAUDE_CODE_ENTRYPOINT" || k == "CLAUDE_CODE_CHILD_SESSION" {
 			continue
 		}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -w internal/platform internal/backend && GOTOOLCHAIN=local go test -race ./internal/platform/ ./internal/backend/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS, including `TestHerdrConfigPathOverridesThenDefault`, `TestWindowsHerdrConfigDirRules`, `TestBatchCmdLineQuotesEveryArgument`, `TestBatchCmdLineRefusesCharactersCmdCantCarry`, `TestBatchCmdLineRefusesOverLongLinesWithAHint`, `TestConnRefusedRecognisesAClosedPort` (it also checks `ErrConnRefused`), `TestUnixConstants` (with `HomeEnv`), `TestTreeKillEndsTheWholeProcessGroup`, `TestCommandContextIsBoundToTheContext`, `TestCleanEnvFoldsCaseWhereTheOSDoes`, `TestExecRunnerKillsProcessGroupOnTimeout` and every existing backend test.

- [ ] **Step 5: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/platform/... ./internal/backend/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/platform/ ./internal/backend/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five commands succeed (`platform.test.exe` and `backend.test.exe` appear in `$TMPDIR/p6-win`); the last two print only lines from Wave 1's list without `internal/backend` (replayed: `# herdr-dots/internal/backend/toolloop`, `# herdr-dots/internal/lockfile`).

- [ ] **Step 6: Commit**

```bash
git add internal/platform internal/backend/exec.go internal/backend/exec_test.go internal/backend/exec_unix_test.go internal/backend/exec_windows_test.go
git commit -m "feat(platform): OS helpers, process trees and cmd.exe-safe command lines (Phase 6)"
```

---

### Task 3: `internal/platform` privacy, pipes and `platformtest`

**Depends on:** none (Wave 1). Task 2 writes other files in the same package; this task's files use none of Task 2's symbols.

**Files:**
- Create: `internal/platform/security.go`, `internal/platform/security_unix.go`, `internal/platform/security_windows.go`, `internal/platform/names.go`, `internal/platform/pipe.go`, `internal/platform/pipe_windows.go`
- Create: `internal/platform/platformtest/platformtest.go`, `internal/platform/platformtest/platformtest_unix.go`, `internal/platform/platformtest/platformtest_windows.go`
- Test: `internal/platform/security_test.go`, `internal/platform/security_unix_test.go`, `internal/platform/security_windows_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `github.com/Microsoft/go-winio v0.6.2` (added here), `golang.org/x/sys/windows`.
- Produces (package `platform`, every OS): `var ErrNotPrivate, ErrNotOwner error`; `func SecureDir(path string) error`; `func SecureFile(path string) error`; `func CheckPrivate(path string) error`; `func CheckPrivateFile(f *os.File) error` (the same rules for an open file: `fstat` on Unix, the handle's owner and DACL on Windows; Plan decision 25); `func FixPrivateHint(path string) string`; `func OpenNoFollow(path string) (*os.File, error)` (Unix: `O_NOFOLLOW|O_NONBLOCK`, so a FIFO never blocks the open); `func ReservedPathName(rel string) bool`; `const PipePrefix` (the string `\\.\pipe\`); `func PipeName(socketPath string) string`.
- Produces (package `platform`, Windows only): `func PipeSDDL() (string, error)`; `func ListenPipe(name string) (net.Listener, error)`; `func DialPipe(ctx context.Context, name string) (net.Conn, error)`; `func CheckPipeOwner(conn net.Conn) error`.
- Produces (package `platformtest`): `func Abs(parts ...string) string`; `func Endpoint(t testing.TB) string` (a fresh engine endpoint: a short unix socket path whose directory the test removes, or a unique `\\.\pipe\herdr-dots-test-<pid>-<n>`; Plan decision 31); `func AssertPrivateFile(t testing.TB, path string)`; `func AssertPrivateDir(t testing.TB, path string)`.

- [ ] **Step 1: Write the failing tests**

`internal/platform/security_test.go` (OS-neutral rules, run everywhere):

```go
package platform

import "testing"

func TestPipeName(t *testing.T) {
	sock := `C:\Users\José Núñez\AppData\Roaming\herdr\herdr.sock`
	if got := PipeName(sock); got != `\\.\pipe\`+sock {
		t.Fatalf("PipeName = %s", got)
	}
	for _, p := range []string{`\\.\pipe\herdr`, `\\.\PIPE\x`, `//./pipe/y`} {
		if PipeName(p) != p {
			t.Errorf("pipe path %s changed to %s", p, PipeName(p))
		}
	}
}

func TestWindowsReservedName(t *testing.T) {
	for rel, want := range map[string]bool{
		".env::$DATA":           true,
		"notes.txt:secret":      true,
		"C:/Windows/win.ini":    true,
		"src/NUL":               true,
		"src/nul.txt":           true,
		`logs\com1.log`:         true,
		"CONOUT$":               true,
		"env.":                  true,
		"secrets /prod.json":    true,
		"src/main.go":           false,
		"console/log.go":        false,
		"docs/null.md":          false,
		"./a/../b.txt":          false,
		"my folder/José's.txt":  false,
		".github/workflows/x.y": false,
	} {
		if windowsReservedName(rel) != want {
			t.Errorf("windowsReservedName(%q) = %v", rel, !want)
		}
	}
}
```

`internal/platform/security_unix_test.go`:

```go
//go:build unix

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPrivateReadsModeBits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(p); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("0644: %v, want ErrNotPrivate", err)
	}
	if err := SecureFile(p); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(p); err != nil {
		t.Fatalf("after SecureFile: %v", err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("SecureFile mode %v", st.Mode().Perm())
	}
}

func TestCheckPrivateFileChecksTheOpenFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := CheckPrivateFile(f); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("0644: %v, want ErrNotPrivate", err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateFile(f); err != nil {
		t.Fatalf("0600: %v", err)
	}
}

func TestSecureDirIs0700(t *testing.T) {
	d := filepath.Join(t.TempDir(), "memory")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SecureDir(d); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(d); st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestOpenNoFollowRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	os.WriteFile(real, []byte("ok"), 0o600)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if f, err := OpenNoFollow(real); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}
	if f, err := OpenNoFollow(link); err == nil {
		f.Close()
		t.Fatal("symlink opened")
	}
	if ReservedPathName(".env::$DATA") || ReservedPathName("NUL") {
		t.Fatal("Unix has no reserved names")
	}
}
```

`internal/platform/security_windows_test.go` (Review Focus 4 and 5; runs only on Windows):

```go
//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// setSDDL replaces path's DACL with the one in sddl (a file copied from
// another machine, or left world-readable).
func setSDDL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func daclString(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func mySID(t *testing.T) string {
	t.Helper()
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	return sid.String()
}

func TestSecureFileReplacesAPermissiveDACL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	os.WriteFile(p, []byte("x"), 0o600)
	setSDDL(t, p, "D:(A;;FA;;;WD)") // Everyone: full control
	if err := CheckPrivate(p); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Everyone ACE: %v, want ErrNotPrivate", err)
	}
	if err := SecureFile(p); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(p); err != nil {
		t.Fatalf("after SecureFile: %v", err)
	}
	d := daclString(t, p)
	if !strings.Contains(d, "D:P") || !strings.Contains(d, mySID(t)) || strings.Contains(d, ";WD)") {
		t.Fatalf("DACL %s: want protected, owner-only", d)
	}
}

func TestCheckPrivateRefusesReadAccessForOthers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	os.WriteFile(p, []byte("x"), 0o600)
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;AU)", mySID(t))) // Authenticated Users may read
	if err := CheckPrivate(p); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Authenticated Users read: %v, want ErrNotPrivate", err)
	}
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)(A;;FA;;;BA)", mySID(t)))
	if err := CheckPrivate(p); err != nil {
		t.Fatalf("owner, SYSTEM, Administrators: %v", err)
	}
	if h := FixPrivateHint(p); !strings.Contains(h, "icacls") || !strings.Contains(h, p) {
		t.Fatalf("hint %q", h)
	}
}

func TestCheckPrivateFileReadsTheHandlesDACL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	os.WriteFile(p, []byte("x"), 0o600)
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;WD)", mySID(t))) // Everyone may read
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := CheckPrivateFile(f); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Everyone read: %v, want ErrNotPrivate", err)
	}
	setSDDL(t, p, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)", mySID(t)))
	if err := CheckPrivateFile(f); err != nil {
		t.Fatalf("owner and SYSTEM: %v", err)
	}
}

func TestSecureDirIsInheritedByNewFiles(t *testing.T) {
	d := filepath.Join(t.TempDir(), "memory")
	os.Mkdir(d, 0o700)
	setSDDL(t, d, "D:(A;OICI;FA;;;WD)")
	if err := SecureDir(d); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(d, "entry.json")
	os.WriteFile(f, []byte("{}"), 0o600)
	if err := CheckPrivate(f); err != nil {
		t.Fatalf("new file in a secured dir: %v", err)
	}
}

func TestOpenNoFollowRefusesSymlinksAndReservedNames(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	os.WriteFile(real, []byte("ok"), 0o600)
	f, err := OpenNoFollow(real)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks need Developer Mode or admin here: %v", err)
	}
	if f, err := OpenNoFollow(link); err == nil {
		f.Close()
		t.Fatal("symlink opened")
	}
	if !ReservedPathName(".env::$DATA") || !ReservedPathName("src/NUL") || ReservedPathName("src/main.go") {
		t.Fatal("ReservedPathName rules")
	}
}

func TestPipeSDDLGrantsOnlyTheUser(t *testing.T) {
	s, err := PipeSDDL()
	if err != nil {
		t.Fatal(err)
	}
	if s != fmt.Sprintf("O:%[1]sD:P(A;;GA;;;%[1]s)", mySID(t)) {
		t.Fatalf("SDDL %s", s)
	}
}

func TestPipeRoundTripOwnerCheckAndNoSecondListener(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\herdr-dots-test-%d`, time.Now().UnixNano())
	ln, err := ListenPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	if _, err := ListenPipe(name); err == nil {
		t.Fatal("a second listener on the same name must fail")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := DialPipe(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := CheckPipeOwner(conn); err != nil {
		t.Fatalf("own pipe refused: %v", err)
	}
	conn.Write([]byte("ping\n"))
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping\n" {
		t.Fatalf("echo %q %v", buf, err)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/platform/ -run 'PipeName|ReservedName|CheckPrivate|SecureDir|OpenNoFollow'`
Expected: FAIL to compile (`undefined: PipeName`, `undefined: windowsReservedName`, `undefined: CheckPrivate`, `undefined: CheckPrivateFile`, ...).

- [ ] **Step 3: Add go-winio**

Run:

```bash
GOTOOLCHAIN=local go get github.com/Microsoft/go-winio@v0.6.2
```

Expected: `go: added github.com/Microsoft/go-winio v0.6.2`. (`v0.6.3` needs Go 1.26; don't upgrade.)

- [ ] **Step 4: Implement**

`internal/platform/security.go`:

```go
package platform

import "errors"

var (
	// ErrNotPrivate means a file or directory is usable by other users.
	ErrNotPrivate = errors.New("other users can read it")
	// ErrNotOwner means a file, directory or pipe belongs to another user.
	ErrNotOwner = errors.New("it is owned by another user")
)
```

`internal/platform/security_unix.go`:

```go
//go:build unix

package platform

import (
	"fmt"
	"os"
	"syscall"
)

// SecureDir makes a directory private to the current user (0700).
func SecureDir(path string) error { return os.Chmod(path, 0o700) }

// SecureFile makes a file private to the current user (0600).
func SecureFile(path string) error { return os.Chmod(path, 0o600) }

// CheckPrivate fails with ErrNotPrivate when group or others have any
// permission bit on path, and with ErrNotOwner when another user owns it.
// Symlinks are not followed.
func CheckPrivate(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return checkPrivateInfo(st)
}

// CheckPrivateFile is CheckPrivate for a file already open: it checks the
// file the descriptor refers to (fstat), so a path swapped after the open
// can't pass the check.
func CheckPrivateFile(f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return checkPrivateInfo(st)
}

func checkPrivateInfo(st os.FileInfo) error {
	if st.Mode().Perm()&0o077 != 0 {
		return ErrNotPrivate
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Getuid() {
		return ErrNotOwner
	}
	return nil
}

// FixPrivateHint tells the user how to make path private again.
func FixPrivateHint(path string) string { return fmt.Sprintf("run: chmod 600 %q", path) }

// OpenNoFollow opens path read-only and fails if its last element is a
// symlink. O_NONBLOCK keeps a FIFO from hanging the open; callers still
// require a regular file or a directory.
func OpenNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

// ReservedPathName reports whether a project-relative path names something
// other than a plain file. Unix has no such names.
func ReservedPathName(string) bool { return false }
```

`internal/platform/security_windows.go`:

```go
//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Well-known SIDs that may hold access to a private file besides its owner.
const (
	sidSystem         = "S-1-5-18"
	sidAdministrators = "S-1-5-32-544"
	sidOwnerRights    = "S-1-3-4"
)

// currentUserSID is the SID of the user running this process.
func currentUserSID() (*windows.SID, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read the current user's SID: %w", err)
	}
	return tu.User.Sid, nil
}

// privateDACL builds "owner-only" access for path: full control for the
// current user, SYSTEM and Administrators, inheritance from the parent off.
// Directories pass their entries on to new children (OICI).
func privateDACL(dir bool) (*windows.ACL, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	inherit := ""
	if dir {
		inherit = "OICI"
	}
	sddl := fmt.Sprintf("D:P(A;%[1]s;FA;;;%[2]s)(A;%[1]s;FA;;;SY)(A;%[1]s;FA;;;BA)", inherit, sid.String())
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	return dacl, err
}

func applyPrivate(path string, dir bool) error {
	if CheckPrivate(path) == nil {
		return nil // already private: don't rewrite (and re-propagate) the ACL
	}
	dacl, err := privateDACL(dir)
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("make %s private: %w", path, err)
	}
	return nil
}

// SecureDir gives a directory an explicit owner-only DACL (current user,
// SYSTEM, Administrators; inheritance off) unless it is already private.
// New files created inside inherit it.
func SecureDir(path string) error { return applyPrivate(path, true) }

// SecureFile gives a file an explicit owner-only DACL unless it is already
// private.
func SecureFile(path string) error { return applyPrivate(path, false) }

// CheckPrivate fails with ErrNotOwner when path's owner is neither the
// current user nor (for an elevated user) Administrators, and with
// ErrNotPrivate when its DACL is missing or allows anyone but the owner, the
// current user, SYSTEM and Administrators. Deny entries and inherit-only
// entries are ignored.
func CheckPrivate(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return checkPrivateSD(sd)
}

// CheckPrivateFile is CheckPrivate for a file already open: it reads the
// owner and DACL through the handle, so the file checked is the file read.
func CheckPrivateFile(f *os.File) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return checkPrivateSD(sd)
}

// checkPrivateSD applies CheckPrivate's rules to a security descriptor.
func checkPrivateSD(sd *windows.SECURITY_DESCRIPTOR) error {
	me, err := currentUserSID()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !ownerOK(owner, me) {
		return ErrNotOwner
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return ErrNotPrivate // no DACL at all grants everyone everything
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if sid.Equals(me) || sid.Equals(owner) {
				continue
			}
			switch sid.String() {
			case sidSystem, sidAdministrators, sidOwnerRights:
				continue
			}
			return ErrNotPrivate
		default:
			return ErrNotPrivate // object/callback entries: be conservative
		}
	}
	return nil
}

// FixPrivateHint tells the user how to make path private again.
func FixPrivateHint(path string) string {
	return fmt.Sprintf(`run: icacls "%s" /inheritance:r /grant:r "%%USERNAME%%:F" (or delete it and set it again)`, path)
}

// OpenNoFollow opens path read-only and fails if it is a symlink or another
// reparse point (a junction or mount point).
func OpenNoFollow(path string) (*os.File, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, &os.PathError{Op: "open", Path: path, Err: errors.New("is a symlink or reparse point")}
	}
	return os.Open(path)
}

// ReservedPathName reports whether a project-relative path (slash or
// backslash separated) names an NTFS alternate data stream ("a.txt:secret",
// ".env::$DATA"), a device (CON, NUL, COM1, ...), or a name Windows silently
// rewrites (a trailing dot or space).
func ReservedPathName(rel string) bool { return windowsReservedName(rel) }
```

`internal/platform/names.go`:

```go
package platform

import "strings"

// windowsDevices are names Windows maps to devices in every directory, with
// or without an extension ("nul.txt" is NUL too).
var windowsDevices = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// windowsReservedName is ReservedPathName's Windows rule, kept OS-neutral so
// every OS can test it: any ':' (an alternate data stream or a drive), a
// device name as a path element, or an element ending in a dot or a space
// (Windows strips those, so "env." opens "env"). "." and ".." are left to
// the caller's root check.
func windowsReservedName(rel string) bool {
	if strings.Contains(rel, ":") {
		return true
	}
	for _, el := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if el == "." || el == ".." {
			continue
		}
		if strings.HasSuffix(el, ".") || strings.HasSuffix(el, " ") {
			return true
		}
		stem, _, _ := strings.Cut(el, ".")
		if windowsDevices[strings.ToUpper(strings.TrimRight(stem, " "))] {
			return true
		}
	}
	return false
}
```

`internal/platform/pipe.go`:

```go
package platform

import "strings"

// PipePrefix starts every local named pipe path on Windows.
const PipePrefix = `\\.\pipe\`

// PipeName maps a Herdr socket path to the named pipe Herdr serves on
// Windows. Herdr writes a marker file at HERDR_SOCKET_PATH and listens on
// \\.\pipe\<that path> (its interprocess crate's namespaced name). A value
// that already is a pipe path is returned unchanged.
func PipeName(socketPath string) string {
	low := strings.ToLower(socketPath)
	if strings.HasPrefix(low, `\\.\pipe\`) || strings.HasPrefix(low, `//./pipe/`) {
		return socketPath
	}
	return PipePrefix + socketPath
}
```

`internal/platform/pipe_windows.go`:

```go
//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// PipeSDDL is the security descriptor for herdr-dots' pipes: owned by the
// current user, who alone gets access.
func PipeSDDL() (string, error) {
	sid, err := currentUserSID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("O:%[1]sD:P(A;;GA;;;%[1]s)", sid.String()), nil
}

// ListenPipe serves the named pipe name, reachable only by the current user.
// It fails if the name already exists (go-winio creates the first instance
// with FILE_FLAG_FIRST_PIPE_INSTANCE), so a pipe another user created first
// is never shared.
func ListenPipe(name string) (net.Listener, error) {
	sddl, err := PipeSDDL()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: sddl})
}

// DialPipe connects to the named pipe name.
func DialPipe(ctx context.Context, name string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, name)
}

// ownerOK accepts the current user, or Administrators when this process's
// token belongs to that group (an elevated process's objects are owned by
// Administrators unless it says otherwise).
func ownerOK(owner, me *windows.SID) bool {
	if owner.Equals(me) {
		return true
	}
	admins, err := windows.StringToSid(sidAdministrators)
	if err != nil || !owner.Equals(admins) {
		return false
	}
	member, err := windows.Token(0).IsMember(admins)
	return err == nil && member
}

// CheckPipeOwner fails with ErrNotOwner unless the pipe behind conn belongs
// to the current user, so a pipe another user squatted under the engine's
// name is refused instead of trusted.
func CheckPipeOwner(conn net.Conn) error {
	f, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("pipe connection exposes no handle")
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read pipe owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read pipe owner: %w", err)
	}
	me, err := currentUserSID()
	if err != nil {
		return err
	}
	if !ownerOK(owner, me) {
		return fmt.Errorf("%w: the pipe is owned by %s", ErrNotOwner, owner.String())
	}
	return nil
}
```

`internal/platform/platformtest/platformtest.go`:

```go
// Package platformtest holds test assertions whose meaning differs by OS, so
// tests in other packages state intent ("this file is private") instead of
// Unix mode bits.
package platformtest

import (
	"path/filepath"
	"testing"
)

// Abs joins parts under an absolute root: /a/b on Unix, C:\a\b on Windows.
// Use it where a test needs an absolute path that never touches the disk.
func Abs(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }

// Endpoint is a fresh engine IPC endpoint for a test, never served before:
// a short unix socket path (macOS caps them at 104 bytes) whose directory is
// removed after the test, or a unique named pipe on Windows. Serve it with
// ipc.Listen and dial it with ipc.Dial or an ipc.Client.
func Endpoint(t testing.TB) string {
	t.Helper()
	return endpoint(t)
}

// AssertPrivateFile fails t unless path is a file only its owner can use:
// mode 0600 on Unix, an owner-only DACL on Windows.
func AssertPrivateFile(t testing.TB, path string) {
	t.Helper()
	assertPrivate(t, path, false)
}

// AssertPrivateDir fails t unless path is a directory only its owner can
// use: mode 0700 on Unix, an owner-only DACL on Windows.
func AssertPrivateDir(t testing.TB, path string) {
	t.Helper()
	assertPrivate(t, path, true)
}
```

`internal/platform/platformtest/platformtest_unix.go`:

```go
//go:build unix

package platformtest

import (
	"os"
	"path/filepath"
	"testing"
)

const root = "/"

func endpoint(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hdep")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "e.sock")
}

func assertPrivate(t testing.TB, path string, dir bool) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	want := os.FileMode(0o600)
	if dir {
		want = 0o700
	}
	if st.IsDir() != dir || st.Mode().Perm() != want {
		t.Fatalf("%s: mode %v (dir=%v), want %v", path, st.Mode(), st.IsDir(), want)
	}
}
```

`internal/platform/platformtest/platformtest_windows.go`:

```go
//go:build windows

package platformtest

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"herdr-dots/internal/platform"
)

const root = `C:\`

var endpointSeq atomic.Int64

func endpoint(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("%sherdr-dots-test-%d-%d", platform.PipePrefix, os.Getpid(), endpointSeq.Add(1))
}

func assertPrivate(t testing.TB, path string, dir bool) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if st.IsDir() != dir {
		t.Fatalf("%s: dir=%v, want %v", path, st.IsDir(), dir)
	}
	if err := platform.CheckPrivate(path); err != nil {
		t.Fatalf("%s is not private: %v", path, err)
	}
}
```

Then tidy the module:

```bash
GOTOOLCHAIN=local go mod tidy
git diff go.mod
```

Expected `go.mod` diff: `github.com/Microsoft/go-winio v0.6.2` and `golang.org/x/sys v0.47.0` move from the indirect block into the direct `require` block (no other line changes); `go.sum` gains the two `go-winio v0.6.2` lines.

- [ ] **Step 5: Run the tests to see them pass**

Run: `gofmt -w internal/platform && GOTOOLCHAIN=local go test -race ./internal/platform/... -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS (`TestPipeName`, `TestWindowsReservedName`, `TestCheckPrivateReadsModeBits`, `TestCheckPrivateFileChecksTheOpenFile`, `TestSecureDirIs0700`, `TestOpenNoFollowRefusesSymlinks`).

- [ ] **Step 6: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/platform/...
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/platform/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed (`go test -c` also reports `herdr-dots/internal/platform/platformtest [no test files]`); the last two print only Wave 1's list (`# herdr-dots/internal/backend`, `# herdr-dots/internal/backend/toolloop`, `# herdr-dots/internal/lockfile`).

- [ ] **Step 7: Commit**

```bash
git add internal/platform go.mod go.sum
git commit -m "feat(platform): owner-only DACLs, named pipes and private-file test helpers (Phase 6)"
```

---

### Task 4: `LockFileEx` locks and memory `Purge`

**Depends on:** none (Wave 1).

**Files:**
- Modify: `internal/lockfile/lockfile.go`
- Create: `internal/lockfile/lockfile_unix.go`, `internal/lockfile/lockfile_windows.go`
- Test: `internal/lockfile/xproc_test.go`
- Modify: `internal/memory/store.go` (`Purge`; Phase 2 file, Phase 5 changed it elsewhere and its final fix wave may move `Purge`: the hunk's `-`/`+` lines are what matters)
- Test: `internal/memory/purge_test.go`

**Interfaces:**
- Consumes: `golang.org/x/sys/windows` (`CreateFile`, `LockFileEx`, `UnlockFileEx`).
- Produces: `lockfile.TryLock(path string) (*Lock, error)`, `(*Lock) Release() error`, `Held(path string) bool`, `Acquire(path string, timeout time.Duration) (*Lock, error)` with unchanged signatures and semantics on both OSes (exclusive across handles, in the same process too); `memory.(*Store).Purge` removes the whole project directory on Windows as well; unexported `removeAllBut(dir, keep string) error`.

- [ ] **Step 1: Write the failing tests**

`internal/lockfile/xproc_test.go` (two real processes; the test binary re-runs itself as the holder):

```go
package lockfile

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLockHolderChild takes the lock, says so, and holds it until stdin
// closes. It is the other process for the test below.
func TestLockHolderChild(t *testing.T) {
	p := os.Getenv("LOCKFILE_CHILD_PATH")
	if p == "" {
		t.Skip("helper process for TestLockIsExclusiveAcrossProcesses")
	}
	l, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString("LOCKED\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	l.Release()
}

func TestLockIsExclusiveAcrossProcesses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "engine.lock")
	child := exec.Command(os.Args[0], "-test.run=^TestLockHolderChild$", "-test.count=1", "-test.v")
	child.Env = append(os.Environ(), "LOCKFILE_CHILD_PATH="+p)
	stdin, _ := child.StdinPipe()
	stdout, _ := child.StdoutPipe()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	locked := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "LOCKED") {
				locked <- true
			}
		}
		close(locked)
	}()
	select {
	case ok := <-locked:
		if !ok {
			t.Fatal("child never took the lock")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child never took the lock")
	}
	if !Held(p) {
		t.Fatal("Held must see the other process's lock")
	}
	if _, err := TryLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("TryLock while the child holds it: %v", err)
	}
	stdin.Close()
	if err := child.Wait(); err != nil {
		t.Fatalf("child: %v", err)
	}
	l, err := Acquire(p, 2*time.Second)
	if err != nil {
		t.Fatalf("lock not free after the child exited: %v", err)
	}
	l.Release()
}
```

`internal/memory/purge_test.go`:

```go
package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveAllButKeepsOnlyTheNamedEntry(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"memory.lock", "meta.json", "dots/security.json"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeAllBut(dir, "memory.lock"); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "memory.lock" {
		t.Fatalf("left %v", ents)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/memory/ -run TestRemoveAllBut; GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/lockfile/`
Expected: the first FAILs to compile (`undefined: removeAllBut`); the second fails with `undefined: syscall.Flock`. (`TestLockIsExclusiveAcrossProcesses` already passes on Unix with v1's flock; on Windows it is the regression test for `LockFileEx`.)

- [ ] **Step 3: Implement**

`internal/lockfile/lockfile.go`:

```diff
--- a/internal/lockfile/lockfile.go
+++ b/internal/lockfile/lockfile.go
@@ -1,28 +1,28 @@
-// Package lockfile guarantees one engine per Herdr session with flock(2). The
-// kernel releases the lock if the engine dies, so stale locks can't happen.
+// Package lockfile guarantees one engine per Herdr session with an OS file
+// lock: flock(2) on Unix, LockFileEx on Windows. The OS releases the lock if
+// the holder dies, so stale locks can't happen.
 package lockfile
 
 import (
 	"errors"
 	"fmt"
 	"os"
-	"syscall"
 )
 
 var ErrLocked = errors.New("lock held by another process")
 
 type Lock struct{ f *os.File }
 
+// TryLock takes the exclusive lock on path without waiting, creating the file
+// if needed, and records this process's pid in it. A lock held through any
+// other open of the file, in this process or another, is ErrLocked.
 func TryLock(path string) (*Lock, error) {
-	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
+	f, err := openLockFile(path)
 	if err != nil {
 		return nil, err
 	}
-	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
+	if err := lockFile(f); err != nil {
 		f.Close()
-		if errors.Is(err, syscall.EWOULDBLOCK) {
-			return nil, ErrLocked
-		}
 		return nil, err
 	}
 	_ = f.Truncate(0)
@@ -31,7 +31,7 @@ func TryLock(path string) (*Lock, error) {
 }
 
 func (l *Lock) Release() error {
-	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
+	_ = unlockFile(l.f)
 	return l.f.Close()
 }
 
```

`internal/lockfile/lockfile_unix.go`:

```go
//go:build unix

package lockfile

import (
	"errors"
	"os"
	"syscall"
)

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
```

`internal/lockfile/lockfile_windows.go`:

```go
//go:build windows

package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffsetHigh places the one locked byte at 1<<62, far past the pid the
// file holds, so the lock never blocks reading or rewriting the content.
const lockOffsetHigh = 0x40000000

// openLockFile opens path with FILE_SHARE_DELETE, so a directory holding a
// lock file can still be removed (memory Purge) while the lock is held.
func openLockFile(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func lockFile(f *os.File) error {
	ol := &windows.Overlapped{OffsetHigh: lockOffsetHigh}
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	ol := &windows.Overlapped{OffsetHigh: lockOffsetHigh}
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}
```

`internal/memory/store.go`:

```diff
--- a/internal/memory/store.go
+++ b/internal/memory/store.go
@@ -988,7 +988,10 @@ func (s *Store) FindProject(arg string) (ProjectMeta, bool, error) {
 	return ProjectMeta{}, false, errors.Join(errs...)
 }
 
-// Purge deletes a project's whole memory directory.
+// Purge deletes a project's whole memory directory. Everything but the lock
+// file goes while the lock is held; the lock file and the directory go after
+// it is released, because Windows can't delete a file another handle (the
+// lock) has open.
 func (s *Store) Purge(p string) error {
 	if !ValidProjectID(p) {
 		return fmt.Errorf("%w: project id %q", ErrInvalid, p)
@@ -997,7 +1000,28 @@ func (s *Store) Purge(p string) error {
 	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
 		return fmt.Errorf("%w: project %s has no memory", ErrNotFound, p)
 	}
-	return s.withLock(dir, func() error { return os.RemoveAll(dir) })
+	if err := s.withLock(dir, func() error { return removeAllBut(dir, "memory.lock") }); err != nil {
+		return err
+	}
+	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
+		return err
+	}
+	return nil
+}
+
+// removeAllBut removes every entry of dir except keep.
+func removeAllBut(dir, keep string) error {
+	ents, err := os.ReadDir(dir)
+	if err != nil {
+		return err
+	}
+	var errs []error
+	for _, e := range ents {
+		if e.Name() != keep {
+			errs = append(errs, os.RemoveAll(filepath.Join(dir, e.Name())))
+		}
+	}
+	return errors.Join(errs...)
 }
 
 // DeleteDot removes a dot's memory in every project and its global memory,
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -w internal/lockfile internal/memory && GOTOOLCHAIN=local go test -race ./internal/lockfile/ ./internal/memory/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS, including `TestLockIsExclusiveAcrossProcesses`, `TestRemoveAllButKeepsOnlyTheNamedEntry`, Phase 2's `TestForgetAllDeleteDotAndPurge` and `TestCrossProcessWritersLoseNoUpdates`.

- [ ] **Step 5: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/lockfile/ ./internal/memory/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/lockfile/ ./internal/memory/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed; the last two print only Wave 1's list without `internal/lockfile` (`# herdr-dots/internal/backend`, `# herdr-dots/internal/backend/toolloop`).

- [ ] **Step 6: Commit**

```bash
git add internal/lockfile internal/memory/store.go internal/memory/purge_test.go
git commit -m "feat(lockfile): LockFileEx on Windows; memory purge drops the lock file after release (Phase 6)"
```

---

### Task 5: ASCII glyph fallback

**Depends on:** none (Wave 1).

**Files:**
- Modify: `internal/ui/style/style.go`, `internal/ui/pane/model.go` (one line; Phases 2–4 changed this file elsewhere)
- Test: `internal/ui/style/style_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `func style.ASCII() bool` (true when `HERDR_DOTS_ASCII=1`); `func style.Sym(s string) string` (identity unless `ASCII()`; then `◆→*`, `▲→^`, `✎→~`, `⊕→+`, `◌→o`, `★→*`, `▼→v`, `✓→+`, `…→...`, other non-ASCII → `*`). `style.Glyph` renders through `Sym`.

- [ ] **Step 1: Write the failing test**

`internal/ui/style/style_test.go`:

```go
package style

import (
	"strings"
	"testing"
)

func TestSymIsIdentityByDefault(t *testing.T) {
	if ascii {
		t.Skip("HERDR_DOTS_ASCII is set in this environment")
	}
	for _, s := range []string{"◆", "▲", "✎", "⊕", "◌", "?", "★"} {
		if Sym(s) != s {
			t.Errorf("Sym(%q) = %q", s, Sym(s))
		}
	}
}

func TestASCIIModeMapsEveryGlyph(t *testing.T) {
	old := ascii
	ascii = true
	defer func() { ascii = old }()
	if !ASCII() {
		t.Fatal("ASCII() must report the switch")
	}
	for in, want := range map[string]string{"◆": "*", "▲": "^", "✎": "~", "⊕": "+", "◌": "o", "★": "*", "▼": "v", "✓": "+", "…": "...", "?": "?", "S": "S", "☂": "*"} {
		if got := Sym(in); got != want {
			t.Errorf("Sym(%q) = %q, want %q", in, got, want)
		}
	}
	if g := Glyph("◆", "red"); !strings.Contains(g, "*") || strings.Contains(g, "◆") {
		t.Fatalf("Glyph = %q", g)
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `GOTOOLCHAIN=local go test ./internal/ui/style/`
Expected: FAIL to compile (`undefined: Sym`, `undefined: ascii`, `undefined: ASCII`).

- [ ] **Step 3: Implement**

`internal/ui/style/style.go`:

```diff
--- a/internal/ui/style/style.go
+++ b/internal/ui/style/style.go
@@ -1,7 +1,13 @@
 // Package style holds the colors and text styles shared by the Dots TUIs.
 package style
 
-import "github.com/charmbracelet/lipgloss"
+import (
+	"os"
+	"strings"
+	"unicode/utf8"
+
+	"github.com/charmbracelet/lipgloss"
+)
 
 var palette = map[string]lipgloss.Color{
 	"red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5", "cyan": "6", "white": "7",
@@ -22,7 +28,35 @@ var (
 )
 
 func Glyph(glyph, color string) string {
-	return lipgloss.NewStyle().Foreground(Color(color)).Render(glyph)
+	return lipgloss.NewStyle().Foreground(Color(color)).Render(Sym(glyph))
+}
+
+// ascii is on when HERDR_DOTS_ASCII=1: for terminals or fonts that show the
+// dot glyphs as empty boxes (some Windows console fonts).
+var ascii = os.Getenv("HERDR_DOTS_ASCII") == "1"
+
+// ASCII reports whether symbols are drawn as ASCII stand-ins.
+func ASCII() bool { return ascii }
+
+// asciiSyms are the stand-ins for the symbols the Dots UIs and the dot
+// templates use.
+var asciiSyms = map[string]string{
+	"◆": "*", "▲": "^", "✎": "~", "⊕": "+", "◌": "o", "★": "*", "▼": "v", "✓": "+", "…": "...",
+}
+
+// Sym returns s unchanged, or with ASCII() on its ASCII stand-in: a known
+// symbol's mapping, s itself when it is already ASCII, else "*".
+func Sym(s string) string {
+	if !ascii {
+		return s
+	}
+	if a, ok := asciiSyms[s]; ok {
+		return a
+	}
+	if strings.IndexFunc(s, func(r rune) bool { return r >= utf8.RuneSelf }) < 0 {
+		return s
+	}
+	return "*"
 }
 
 func Severity(sev string) lipgloss.Style {
```

`internal/ui/pane/model.go` (post-Phase-4 form: the line sits in the roster loop of `View`):

```diff
--- a/internal/ui/pane/model.go
+++ b/internal/ui/pane/model.go
@@ -484,7 +484,7 @@ func (m Model) View() string {
 	for i, d := range p.Dots {
 		status := d.Status
 		if d.Learning {
-			status = "◌ learning…"
+			status = style.Sym("◌") + " learning" + style.Sym("…")
 		}
 		rest := fmt.Sprintf("%-12s %-8s %s", trunc(d.Name, 12), d.Mode, status) + dotCost(d)
 		line := style.Glyph(d.Glyph, d.Color) + " " + trunc(rest, max(10, w-2))
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -w internal/ui && GOTOOLCHAIN=local go test -race ./internal/ui/...`
Expected: PASS (`TestSymIsIdentityByDefault`, `TestASCIIModeMapsEveryGlyph`, every pane test).

- [ ] **Step 5: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/ui/style/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/ui/style/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed; the last two print only Wave 1's list. (`internal/ui/pane` imports `internal/memory`, which imports `internal/lockfile`, so its Windows vet waits for Task 4's merge; it is checked at the Wave 2 gate.)

- [ ] **Step 6: Commit**

```bash
git add internal/ui/style internal/ui/pane/model.go
git commit -m "feat(ui): HERDR_DOTS_ASCII=1 draws ASCII stand-ins for dot glyphs (Phase 6)"
```

---

### Task 6: Transports: Herdr pipe dial, engine endpoint, detached engine

**Depends on:** Task 2 (`platform.StartDetached`), Task 3 (`platform.PipeName`, `PipePrefix`, `ListenPipe`, `DialPipe`, `CheckPipeOwner`, `SecureDir`, `platformtest.AssertPrivateDir`). Wave 2.

**Files:**
- Modify: `internal/herdr/client.go`, `internal/herdr/client_test.go`, `internal/herdr/herdrtest/fake.go`
- Create: `internal/herdr/dial_unix.go`, `internal/herdr/dial_windows.go`, `internal/herdr/herdrtest/listen_unix.go`, `internal/herdr/herdrtest/listen_windows.go`
- Modify: `internal/ipc/ipc.go`, `internal/ipc/ipc_test.go` (Phases 3 and 5 extended both), `internal/ipc/query_test.go` (Phase 5)
- Create: `internal/ipc/endpoint_unix.go`, `internal/ipc/endpoint_windows.go`
- Test: `internal/ipc/endpoint_unix_test.go`, `internal/ipc/endpoint_windows_test.go`
- Modify: `internal/paths/paths.go`, `internal/paths/paths_test.go`
- Create: `internal/paths/endpoint_unix.go`, `internal/paths/endpoint_windows.go`
- Test: `internal/paths/paths_unix_test.go`, `internal/paths/paths_windows_test.go`
- Modify: `cmd/herdr-dots/engine.go`, `cmd/herdr-dots/engine_test.go`, `cmd/herdr-dots/ui.go`, `cmd/herdr-dots/action.go`, `cmd/herdr-dots/mcp.go`, `cmd/herdr-dots/mcp_test.go` (Phase 5: the only other `EngineSock()` callers)

**Interfaces:**
- Consumes: `platform.PipeName(socketPath string) string`, `platform.PipePrefix`, `platform.ListenPipe(name string) (net.Listener, error)`, `platform.DialPipe(ctx context.Context, name string) (net.Conn, error)`, `platform.CheckPipeOwner(conn net.Conn) error`, `platform.SecureDir(path string) error`, `platform.StartDetached(cmd *exec.Cmd) (*os.Process, error)`; `platformtest.AssertPrivateDir`.
- Produces:
  - `func ipc.Listen(endpoint string) (net.Listener, error)` (Unix: removes a stale socket, listens, chmod 0600, closing removes the file; Windows: owner-only pipe, fails if the name exists)
  - `func ipc.Dial(ctx context.Context, endpoint string) (net.Conn, error)` (Windows: refuses a pipe owned by another user); `ipc.Client` (and Phase 5's `Client.Query`, which goes through `Client.dial`) uses it
  - `func (paths.Paths) EngineEndpoint() string` (Unix: `EngineSock()`; Windows: `\\.\pipe\herdr-dots-<SessionKey>`). `EngineSock`, `sockFallbackDir`, `maxSockPath` and the fallback-dir creation move to `internal/paths/endpoint_unix.go` and exist only on Unix.
  - `func herdrtest.Listen(path string) (net.Listener, error)` (Windows: marker file plus `\\.\pipe\<path>`, like Herdr); `herdrtest.New` uses it, so every engine test runs against a pipe on Windows
  - `cmd/herdr-dots`: `waitForEndpoint(endpoint string, d time.Duration) error` replaces `waitForSocket`; Phase 5's `engineEnv` compares names in `platform.EnvKey` form (Plan decision 32)

- [ ] **Step 1: Write the failing tests**

`internal/ipc/ipc_test.go` (`serve` listens through `Listen` on a per-OS test endpoint; Review Focus 4):

```diff
--- a/internal/ipc/ipc_test.go
+++ b/internal/ipc/ipc_test.go
@@ -4,8 +4,6 @@ import (
 	"context"
 	"encoding/json"
 	"errors"
-	"net"
-	"os"
 	"path/filepath"
 	"sync"
 	"testing"
@@ -39,10 +37,8 @@ func (f *fakeHandler) Watch() (<-chan view.Snapshot, func()) {
 
 func serve(t *testing.T) (*fakeHandler, Client) {
 	t.Helper()
-	dir, _ := os.MkdirTemp("", "ipc")
-	t.Cleanup(func() { os.RemoveAll(dir) })
-	path := filepath.Join(dir, "d.sock")
-	ln, err := net.Listen("unix", path)
+	path := testEndpoint(t)
+	ln, err := Listen(path)
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -113,6 +109,30 @@ func TestCallFailsFastWithoutServer(t *testing.T) {
 	}
 }
 
+// Review Focus: two Herdr sessions of one user run two engines on two
+// endpoints; a client of one never reaches the other.
+func TestTwoEndpointsNeverCrossTalk(t *testing.T) {
+	ha, ca := serve(t)
+	hb, cb := serve(t)
+	if ca.Path == cb.Path {
+		t.Fatal("test endpoints must differ")
+	}
+	ctx := context.Background()
+	if err := ca.Call(ctx, CmdPauseAll, nil); err != nil {
+		t.Fatal(err)
+	}
+	if err := cb.Call(ctx, CmdReloadDots, nil); err != nil {
+		t.Fatal(err)
+	}
+	ha.mu.Lock()
+	hb.mu.Lock()
+	defer ha.mu.Unlock()
+	defer hb.mu.Unlock()
+	if len(ha.got) != 1 || ha.got[0] != CmdPauseAll || len(hb.got) != 1 || hb.got[0] != CmdReloadDots {
+		t.Fatalf("engine A got %v, engine B got %v", ha.got, hb.got)
+	}
+}
+
 func TestCouncilArgsRoundTrip(t *testing.T) {
 	h, c := serve(t)
 	want := CouncilArgs{PaneID: "w1:p1", Question: "Ready to merge?", Slugs: []string{"security", "copy"}}
```

`internal/ipc/query_test.go` (Phase 5's query tests served a raw unix socket and dialed it with `net.Dial`; they now use the same `testEndpoint`, `Listen` and `Dial`, so they run over a pipe on Windows):

```diff
--- a/internal/ipc/query_test.go
+++ b/internal/ipc/query_test.go
@@ -6,8 +6,6 @@ import (
 	"encoding/json"
 	"errors"
 	"net"
-	"os"
-	"path/filepath"
 	"strings"
 	"sync/atomic"
 	"testing"
@@ -39,17 +37,12 @@ func (q *queryHandler) Query(ctx context.Context, cmd string, args json.RawMessa
 	return q.query(ctx, cmd, args)
 }
 
-// serveHandler serves h on a short socket path (macOS caps unix socket paths
-// at 104 bytes, and t.TempDir() is long there).
+// serveHandler serves h on a fresh test endpoint (testEndpoint: a short unix
+// socket path, or a named pipe on Windows).
 func serveHandler(t *testing.T, h Handler) Client {
 	t.Helper()
-	dir, err := os.MkdirTemp("", "ipcq")
-	if err != nil {
-		t.Fatal(err)
-	}
-	t.Cleanup(func() { os.RemoveAll(dir) })
-	path := filepath.Join(dir, "q.sock")
-	ln, err := net.Listen("unix", path)
+	path := testEndpoint(t)
+	ln, err := Listen(path)
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -80,13 +73,8 @@ func TestServeWaitsForRunningQueriesOnShutdown(t *testing.T) {
 		finished.Store(true)
 		return nil, ctx.Err()
 	})
-	dir, err := os.MkdirTemp("", "ipcq")
-	if err != nil {
-		t.Fatal(err)
-	}
-	defer os.RemoveAll(dir)
-	path := filepath.Join(dir, "q.sock")
-	ln, err := net.Listen("unix", path)
+	path := testEndpoint(t)
+	ln, err := Listen(path)
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -96,7 +84,7 @@ func TestServeWaitsForRunningQueriesOnShutdown(t *testing.T) {
 	go func() { Serve(ctx, ln, h); close(served) }()
 	go Client{Path: path}.Query(context.Background(), CmdAsk, AskArgs{PaneID: "w1:p1"}, nil)
 	// An idle connection must not keep Serve from returning either.
-	idle, err := net.Dial("unix", path)
+	idle, err := Dial(context.Background(), path)
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -115,7 +103,7 @@ func TestServeWaitsForRunningQueriesOnShutdown(t *testing.T) {
 
 func TestOversizedRequestIsRefused(t *testing.T) {
 	_, c := serve(t)
-	conn, err := net.Dial("unix", c.Path)
+	conn, err := Dial(context.Background(), c.Path)
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -129,13 +117,8 @@ func TestOversizedRequestIsRefused(t *testing.T) {
 }
 
 func TestFailureWithoutTextStillReportsAnError(t *testing.T) {
-	dir, err := os.MkdirTemp("", "ipcq")
-	if err != nil {
-		t.Fatal(err)
-	}
-	defer os.RemoveAll(dir)
-	path := filepath.Join(dir, "q.sock")
-	ln, err := net.Listen("unix", path)
+	path := testEndpoint(t)
+	ln, err := Listen(path)
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -206,7 +189,7 @@ func TestQueryWithoutQuerierIsRefused(t *testing.T) {
 
 func TestV1ResponsesCarryNoData(t *testing.T) {
 	_, c := serve(t)
-	conn, err := net.Dial("unix", c.Path)
+	conn, err := Dial(context.Background(), c.Path)
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -340,7 +323,7 @@ func TestQueryToleratesUnknownFields(t *testing.T) {
 		}
 		return map[string]any{"project_id": a.PaneID, "dots": []any{}, "future_field": 1}, nil
 	}))
-	conn, err := net.Dial("unix", c.Path)
+	conn, err := Dial(context.Background(), c.Path)
 	if err != nil {
 		t.Fatal(err)
 	}
```

`internal/ipc/endpoint_unix_test.go`:

```go
//go:build unix

package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

// testEndpoint is a fresh socket path short enough for sun_path.
func testEndpoint(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

func TestListenReplacesAStaleSocketAndIsPrivate(t *testing.T) {
	path := testEndpoint(t)
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 || st.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket %v %v", st, err)
	}
	ln.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closing the listener must remove the socket: %v", err)
	}
}
```

`internal/ipc/endpoint_windows_test.go`:

```go
//go:build windows

package ipc

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"herdr-dots/internal/platform"
)

var pipeSeq atomic.Int64

// testEndpoint is a fresh named pipe path, unique within this test run.
func testEndpoint(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%sherdr-dots-test-%d-%d", platform.PipePrefix, os.Getpid(), pipeSeq.Add(1))
}

func TestListenRefusesANameThatIsAlreadyServed(t *testing.T) {
	path := testEndpoint(t)
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if second, err := Listen(path); err == nil {
		second.Close()
		t.Fatal("a second engine must not share the pipe")
	}
}
```

`internal/paths/paths_test.go` (the two unix-socket tests move to the Unix file; `Ensure` privacy through `platformtest`; Review Focus 4):

```diff
--- a/internal/paths/paths_test.go
+++ b/internal/paths/paths_test.go
@@ -1,10 +1,11 @@
 package paths
 
 import (
-	"os"
 	"path/filepath"
 	"strings"
 	"testing"
+
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func TestSessionKeyStableAndShort(t *testing.T) {
@@ -27,33 +28,11 @@ func TestFilesLiveUnderSessionDir(t *testing.T) {
 			t.Fatalf("%s not under session dir", f)
 		}
 	}
-	if p.DotsDir() != "/c/dots" || p.ConfigFile() != "/c/config.toml" {
+	if p.DotsDir() != filepath.Join("/c", "dots") || p.ConfigFile() != filepath.Join("/c", "config.toml") {
 		t.Fatal("config paths wrong")
 	}
 }
 
-// Review Focus 1: macOS caps unix socket paths at 104 bytes.
-func TestEngineSockFallsBackWhenPathTooLong(t *testing.T) {
-	long := "/" + strings.Repeat("d", 120)
-	p := New("/c", long, "/sock")
-	s := p.EngineSock()
-	if len(s) > 100 {
-		t.Fatalf("socket path too long (%d): %s", len(s), s)
-	}
-	if !strings.HasPrefix(s, os.TempDir()) {
-		t.Fatalf("expected temp-dir fallback, got %s", s)
-	}
-	// Verify fallback uses per-uid directory
-	expectedFallbackDir := p.sockFallbackDir()
-	if !strings.HasPrefix(s, expectedFallbackDir) {
-		t.Fatalf("expected fallback in per-uid dir %s, got %s", expectedFallbackDir, s)
-	}
-	short := New("/c", "/s", "/sock").EngineSock()
-	if short != filepath.Join(New("/c", "/s", "/sock").SessionDir, "dots.sock") {
-		t.Fatalf("short path should stay in session dir: %s", short)
-	}
-}
-
 func TestFromEnvRequiresHerdr(t *testing.T) {
 	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
 	if _, err := FromEnv(); err == nil {
@@ -75,42 +54,16 @@ func TestEnsureCreatesPrivateDirs(t *testing.T) {
 		t.Fatal(err)
 	}
 	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir} {
-		st, err := os.Stat(d)
-		if err != nil || st.Mode().Perm() != 0o700 {
-			t.Fatalf("%s: %v %v", d, err, st)
-		}
+		platformtest.AssertPrivateDir(t, d)
 	}
 }
 
-func TestEnsureCreatesFallbackDirWhenNeeded(t *testing.T) {
-	// Use a long state directory path that triggers the fallback
+// Review Focus: two Herdr sessions of one user get two engine endpoints.
+func TestEngineEndpointIsPerSession(t *testing.T) {
 	root := t.TempDir()
-	long := filepath.Join(root, strings.Repeat("d", 100))
-	p := New(filepath.Join(root, "c"), long, "/sock")
-
-	// Ensure the fallback directory doesn't exist yet
-	fallbackDir := p.sockFallbackDir()
-	t.Cleanup(func() {
-		// Clean up the fallback directory after the test
-		os.RemoveAll(fallbackDir)
-	})
-
-	if err := p.Ensure(); err != nil {
-		t.Fatal(err)
-	}
-
-	// Verify fallback directory was created with 0700 permissions
-	st, err := os.Stat(fallbackDir)
-	if err != nil {
-		t.Fatalf("fallback dir not created: %v", err)
-	}
-	if st.Mode().Perm() != 0o700 {
-		t.Fatalf("fallback dir perm = %v, want 0700", st.Mode().Perm())
-	}
-
-	// Verify the engine socket path is still within the limit
-	sock := p.EngineSock()
-	if len(sock) > 100 {
-		t.Fatalf("fallback socket path too long (%d): %s", len(sock), sock)
+	a := New(filepath.Join(root, "c"), filepath.Join(root, "s"), filepath.Join(root, "a", "herdr.sock"))
+	b := New(filepath.Join(root, "c"), filepath.Join(root, "s"), filepath.Join(root, "b", "herdr.sock"))
+	if a.EngineEndpoint() == b.EngineEndpoint() || a.EngineEndpoint() == "" {
+		t.Fatalf("endpoints %q and %q", a.EngineEndpoint(), b.EngineEndpoint())
 	}
 }
```

`internal/paths/paths_unix_test.go` (v1's socket tests, unchanged, plus the endpoint identity):

```go
//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineEndpointIsTheUnixSocket(t *testing.T) {
	p := New("/c", "/s", "/sock")
	if p.EngineEndpoint() != p.EngineSock() || p.EngineEndpoint() != filepath.Join(p.SessionDir, "dots.sock") {
		t.Fatalf("endpoint %q", p.EngineEndpoint())
	}
}

// Review Focus 1: macOS caps unix socket paths at 104 bytes.
func TestEngineSockFallsBackWhenPathTooLong(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	p := New("/c", long, "/sock")
	s := p.EngineSock()
	if len(s) > 100 {
		t.Fatalf("socket path too long (%d): %s", len(s), s)
	}
	if !strings.HasPrefix(s, os.TempDir()) {
		t.Fatalf("expected temp-dir fallback, got %s", s)
	}
	// Verify fallback uses per-uid directory
	expectedFallbackDir := p.sockFallbackDir()
	if !strings.HasPrefix(s, expectedFallbackDir) {
		t.Fatalf("expected fallback in per-uid dir %s, got %s", expectedFallbackDir, s)
	}
	short := New("/c", "/s", "/sock").EngineSock()
	if short != filepath.Join(New("/c", "/s", "/sock").SessionDir, "dots.sock") {
		t.Fatalf("short path should stay in session dir: %s", short)
	}
}

func TestEnsureCreatesFallbackDirWhenNeeded(t *testing.T) {
	// Use a long state directory path that triggers the fallback
	root := t.TempDir()
	long := filepath.Join(root, strings.Repeat("d", 100))
	p := New(filepath.Join(root, "c"), long, "/sock")

	// Ensure the fallback directory doesn't exist yet
	fallbackDir := p.sockFallbackDir()
	t.Cleanup(func() {
		// Clean up the fallback directory after the test
		os.RemoveAll(fallbackDir)
	})

	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}

	// Verify fallback directory was created with 0700 permissions
	st, err := os.Stat(fallbackDir)
	if err != nil {
		t.Fatalf("fallback dir not created: %v", err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("fallback dir perm = %v, want 0700", st.Mode().Perm())
	}

	// Verify the engine socket path is still within the limit
	sock := p.EngineSock()
	if len(sock) > 100 {
		t.Fatalf("fallback socket path too long (%d): %s", len(sock), sock)
	}
}
```

`internal/paths/paths_windows_test.go` (Review Focus 1):

```go
//go:build windows

package paths

import (
	"strings"
	"testing"
)

func TestEngineEndpointIsAPerSessionPipe(t *testing.T) {
	sock := `C:\Users\José Núñez\AppData\Roaming\herdr\herdr.sock`
	p := New(`C:\c`, `C:\s`, sock)
	if got := p.EngineEndpoint(); got != `\\.\pipe\herdr-dots-`+SessionKey(sock) || strings.ContainsAny(got[len(`\\.\pipe\`):], `\/`) {
		t.Fatalf("endpoint %q", got)
	}
}
```

`internal/herdr/client_test.go`:

```diff
--- a/internal/herdr/client_test.go
+++ b/internal/herdr/client_test.go
@@ -159,7 +159,7 @@ func TestNewFromEnv(t *testing.T) {
 	}
 }
 
-// rawListener returns a unix socket path whose connections are handed to h.
+// rawListener returns a Herdr socket path whose connections are handed to h.
 func rawListener(t *testing.T, h func(net.Conn)) string {
 	t.Helper()
 	dir, err := os.MkdirTemp("", "hdt")
@@ -167,7 +167,7 @@ func rawListener(t *testing.T, h func(net.Conn)) string {
 		t.Fatal(err)
 	}
 	path := filepath.Join(dir, "h.sock")
-	ln, err := net.Listen("unix", path)
+	ln, err := herdrtest.Listen(path)
 	if err != nil {
 		t.Fatal(err)
 	}
```

`cmd/herdr-dots/engine_test.go`:

```diff
--- a/cmd/herdr-dots/engine_test.go
+++ b/cmd/herdr-dots/engine_test.go
@@ -43,9 +43,10 @@ func TestBuildEngineLoadsDotsFromConfigDir(t *testing.T) {
 	}
 }
 
-func TestWaitForSocketTimesOut(t *testing.T) {
+func TestWaitForEndpointTimesOut(t *testing.T) {
 	start := time.Now()
-	err := waitForSocket(filepath.Join(t.TempDir(), "none.sock"), 200*time.Millisecond)
+	p := paths.New(filepath.Join(t.TempDir(), "c"), filepath.Join(t.TempDir(), "s"), filepath.Join(t.TempDir(), "none.sock"))
+	err := waitForEndpoint(p.EngineEndpoint(), 200*time.Millisecond)
 	if err == nil || time.Since(start) > 2*time.Second {
 		t.Fatalf("err=%v after %v", err, time.Since(start))
 	}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/ipc/ ./internal/paths/ ./internal/herdr/... ./cmd/herdr-dots/`
Expected: FAIL to compile (`undefined: testEndpoint` / `undefined: Listen` / `undefined: Dial` in `ipc`, `p.EngineEndpoint undefined`, `undefined: herdrtest.Listen`, `undefined: waitForEndpoint`).

- [ ] **Step 3: Implement the Herdr dial and the fake server**

`internal/herdr/client.go`:

```diff
--- a/internal/herdr/client.go
+++ b/internal/herdr/client.go
@@ -59,8 +59,7 @@ type response struct {
 }
 
 func (c *SocketClient) dial(ctx context.Context) (net.Conn, error) {
-	var d net.Dialer
-	conn, err := d.DialContext(ctx, "unix", c.Path)
+	conn, err := dialSocket(ctx, c.Path)
 	if err != nil {
 		return nil, fmt.Errorf("connect herdr socket: %w", err)
 	}
```

`internal/herdr/dial_unix.go`:

```go
//go:build unix

package herdr

import (
	"context"
	"net"
)

// dialSocket connects to Herdr's unix socket.
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
```

`internal/herdr/dial_windows.go`:

```go
//go:build windows

package herdr

import (
	"context"
	"net"

	"herdr-dots/internal/platform"
)

// dialSocket connects to the named pipe Herdr serves for HERDR_SOCKET_PATH
// (\\.\pipe\<path>).
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	return platform.DialPipe(ctx, platform.PipeName(path))
}
```

`internal/herdr/herdrtest/fake.go`:

```diff
--- a/internal/herdr/herdrtest/fake.go
+++ b/internal/herdr/herdrtest/fake.go
@@ -44,7 +44,7 @@ func New(t testing.TB) *Server {
 		reads: map[string]string{},
 		subs:  map[string][]*json.Encoder{},
 	}
-	s.ln, err = net.Listen("unix", s.Path)
+	s.ln, err = Listen(s.Path)
 	if err != nil {
 		t.Fatal(err)
 	}
```

`internal/herdr/herdrtest/listen_unix.go`:

```go
//go:build unix

package herdrtest

import "net"

// Listen serves a Herdr-style socket at path the way Herdr does on this OS:
// a unix socket.
func Listen(path string) (net.Listener, error) { return net.Listen("unix", path) }
```

`internal/herdr/herdrtest/listen_windows.go`:

```go
//go:build windows

package herdrtest

import (
	"net"
	"os"

	"herdr-dots/internal/platform"
)

// Listen serves a Herdr-style socket at path the way Herdr does on Windows:
// a marker file at path and a named pipe at \\.\pipe\<path>.
func Listen(path string) (net.Listener, error) {
	if err := os.WriteFile(path, []byte("herdrtest\n"), 0o600); err != nil {
		return nil, err
	}
	return platform.ListenPipe(platform.PipeName(path))
}
```

- [ ] **Step 4: Implement the engine endpoint**

`internal/ipc/ipc.go` (post-Phase-5 form: `Client.Query` calls `c.dial(ctx)` too, so it needs no change):

```diff
--- a/internal/ipc/ipc.go
+++ b/internal/ipc/ipc.go
@@ -1,5 +1,6 @@
 // Package ipc is the newline-delimited JSON protocol between the engine and
-// the Bubble Tea UIs over the engine's local unix socket.
+// the Bubble Tea UIs over the engine's local endpoint: a unix socket, or on
+// Windows a named pipe only the current user can open.
 package ipc
 
 import (
@@ -235,12 +236,11 @@ var callTimeout = 30 * time.Second
 // maxRequestBytes bounds one request line read by the server.
 const maxRequestBytes = 1 << 20
 
+// Client talks to the engine. Path is the engine endpoint
+// (paths.Paths.EngineEndpoint).
 type Client struct{ Path string }
 
-func (c Client) dial(ctx context.Context) (net.Conn, error) {
-	var d net.Dialer
-	return d.DialContext(ctx, "unix", c.Path)
-}
+func (c Client) dial(ctx context.Context) (net.Conn, error) { return Dial(ctx, c.Path) }
 
 func (c Client) Call(ctx context.Context, cmd string, args any) error {
 	conn, err := c.dial(ctx)
```

`internal/ipc/endpoint_unix.go`:

```go
//go:build unix

package ipc

import (
	"context"
	"net"
	"os"
)

// Listen serves the engine endpoint, a unix socket path. A stale socket from
// a dead engine is removed first (the caller holds the engine lock), and the
// socket is made 0600. Closing the listener removes the socket file.
func Listen(endpoint string) (net.Listener, error) {
	_ = os.Remove(endpoint)
	ln, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(endpoint, 0o600)
	return ln, nil
}

// Dial connects to the engine endpoint.
func Dial(ctx context.Context, endpoint string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", endpoint)
}
```

`internal/ipc/endpoint_windows.go`:

```go
//go:build windows

package ipc

import (
	"context"
	"net"

	"herdr-dots/internal/platform"
)

// Listen serves the engine endpoint, a named pipe whose security descriptor
// lets only the current user connect. It fails if the name already exists.
func Listen(endpoint string) (net.Listener, error) { return platform.ListenPipe(endpoint) }

// Dial connects to the engine endpoint and refuses a pipe that another user
// owns (one created under the engine's name before the engine started).
func Dial(ctx context.Context, endpoint string) (net.Conn, error) {
	conn, err := platform.DialPipe(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if err := platform.CheckPipeOwner(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
```

`internal/paths/paths.go` (the socket helpers move out; `Ensure` secures directories through `platform.SecureDir`, which is `chmod 0700` on Unix):

```diff
--- a/internal/paths/paths.go
+++ b/internal/paths/paths.go
@@ -8,13 +8,11 @@ import (
 	"crypto/sha256"
 	"encoding/hex"
 	"errors"
-	"fmt"
 	"os"
 	"path/filepath"
-)
 
-// maxSockPath stays under the 104-byte sun_path limit on macOS.
-const maxSockPath = 100
+	"herdr-dots/internal/platform"
+)
 
 type Paths struct {
 	ConfigDir  string
@@ -58,49 +56,17 @@ func (p Paths) FeedFile() string        { return filepath.Join(p.SessionDir, "fe
 func (p Paths) UsageFile() string       { return filepath.Join(p.SessionDir, "usage.json") }
 func (p Paths) DotsPanesFile() string   { return filepath.Join(p.SessionDir, "dotspanes.json") }
 
-// sockFallbackDir returns a per-uid directory in the temp dir for socket fallback.
-// This ensures the directory is private to the user and protects against squatting.
-func (p Paths) sockFallbackDir() string {
-	return filepath.Join(os.TempDir(), fmt.Sprintf("herdr-dots-%d", os.Getuid()))
-}
-
-// EngineSock is the engine's IPC socket. Deep state dirs would exceed the unix
-// socket path limit, so those fall back to a short path in the temp dir.
-func (p Paths) EngineSock() string {
-	s := filepath.Join(p.SessionDir, "dots.sock")
-	if len(s) <= maxSockPath {
-		return s
-	}
-	return filepath.Join(p.sockFallbackDir(), SessionKey(p.SocketPath)+".sock")
-}
-
 func (p Paths) Ensure() error {
 	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir, p.MemoryDir()} {
 		if err := os.MkdirAll(d, 0o700); err != nil {
 			return err
 		}
-		if err := os.Chmod(d, 0o700); err != nil {
+		if err := platform.SecureDir(d); err != nil {
 			return err
 		}
 	}
 	if err := p.writeStatePointer(); err != nil {
 		return err
 	}
-	// If the primary socket path is too long, create the fallback directory
-	primarySock := filepath.Join(p.SessionDir, "dots.sock")
-	if len(primarySock) > maxSockPath {
-		fallbackDir := p.sockFallbackDir()
-		if err := os.MkdirAll(fallbackDir, 0o700); err != nil {
-			return err
-		}
-		if err := os.Chmod(fallbackDir, 0o700); err != nil {
-			return err
-		}
-		// Verify that even the fallback path stays within the limit
-		fallbackSock := filepath.Join(fallbackDir, SessionKey(p.SocketPath)+".sock")
-		if len(fallbackSock) > maxSockPath {
-			return fmt.Errorf("engine socket path too long: %s", fallbackSock)
-		}
-	}
-	return nil
+	return p.ensureEndpoint()
 }
```

`internal/paths/endpoint_unix.go` (the moved v1 code, unchanged, plus `EngineEndpoint`):

```go
//go:build unix

package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// EngineEndpoint is where the engine serves IPC: the unix socket EngineSock.
func (p Paths) EngineEndpoint() string { return p.EngineSock() }

func (p Paths) ensureEndpoint() error { return p.ensureSockFallback() }

// maxSockPath stays under the 104-byte sun_path limit on macOS.
const maxSockPath = 100

// sockFallbackDir returns a per-uid directory in the temp dir for socket fallback.
// This ensures the directory is private to the user and protects against squatting.
func (p Paths) sockFallbackDir() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("herdr-dots-%d", os.Getuid()))
}

// EngineSock is the engine's unix socket. Deep state dirs would exceed the
// unix socket path limit, so those fall back to a short path in the temp dir.
// Use EngineEndpoint, which is this on Unix and a named pipe on Windows.
func (p Paths) EngineSock() string {
	s := filepath.Join(p.SessionDir, "dots.sock")
	if len(s) <= maxSockPath {
		return s
	}
	return filepath.Join(p.sockFallbackDir(), SessionKey(p.SocketPath)+".sock")
}

// ensureSockFallback creates the per-user temp directory when the primary
// socket path is too long (Unix only; see EngineSock).
func (p Paths) ensureSockFallback() error {
	primarySock := filepath.Join(p.SessionDir, "dots.sock")
	if len(primarySock) <= maxSockPath {
		return nil
	}
	fallbackDir := p.sockFallbackDir()
	if err := os.MkdirAll(fallbackDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(fallbackDir, 0o700); err != nil {
		return err
	}
	// Verify that even the fallback path stays within the limit
	fallbackSock := filepath.Join(fallbackDir, SessionKey(p.SocketPath)+".sock")
	if len(fallbackSock) > maxSockPath {
		return fmt.Errorf("engine socket path too long: %s", fallbackSock)
	}
	return nil
}
```

`internal/paths/endpoint_windows.go`:

```go
//go:build windows

package paths

import "herdr-dots/internal/platform"

// EngineEndpoint is where the engine serves IPC on Windows: the named pipe
// \\.\pipe\herdr-dots-<SessionKey>, one per Herdr session.
func (p Paths) EngineEndpoint() string {
	return platform.PipePrefix + "herdr-dots-" + SessionKey(p.SocketPath)
}

// ensureEndpoint has nothing to create: a pipe needs no directory.
func (p Paths) ensureEndpoint() error { return nil }
```

- [ ] **Step 5: Wire the commands**

`cmd/herdr-dots/engine.go` (post-Phase-5 form: Phase 5's `mcp.go` calls `ensureEngine`, and Phase 5 and its fix wave added `engineEnv` with `claudeProviderSetting`; these hunks touch only the imports, `ensureEngine`, `engineEnv`'s name folding, `waitForSocket` and the listener in `serveEngine`):

```diff
--- a/cmd/herdr-dots/engine.go
+++ b/cmd/herdr-dots/engine.go
@@ -6,7 +6,6 @@ import (
 	"flag"
 	"fmt"
 	"log"
-	"net"
 	"os"
 	"os/exec"
 	"os/signal"
@@ -21,6 +20,7 @@ import (
 	"herdr-dots/internal/ipc"
 	"herdr-dots/internal/lockfile"
 	"herdr-dots/internal/paths"
+	"herdr-dots/internal/platform"
 	"herdr-dots/internal/queue"
 	"herdr-dots/internal/state"
 	"herdr-dots/internal/transcript"
@@ -48,10 +48,10 @@ func runEngine(args []string) error {
 }
 
 // ensureEngine starts a detached engine for this Herdr session unless one
-// already holds the lock, then waits until its socket accepts connections.
+// already holds the lock, then waits until its endpoint accepts connections.
 func ensureEngine(p paths.Paths) error {
 	if lockfile.Held(p.EngineLock()) {
-		return waitForSocket(p.EngineSock(), 3*time.Second)
+		return waitForEndpoint(p.EngineEndpoint(), 3*time.Second)
 	}
 	exe, err := os.Executable()
 	if err != nil {
@@ -71,12 +71,14 @@ func ensureEngine(p paths.Paths) error {
 		cmd.Dir = "/"
 	}
 	cmd.Stdout, cmd.Stderr = logf, logf
-	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive the hook/pane that started us
-	if err := cmd.Start(); err != nil {
+	// Survive the hook/pane that started us: a new session on Unix; a hidden
+	// console and process group (out of any job that allows it) on Windows.
+	proc, err := platform.StartDetached(cmd)
+	if err != nil {
 		return fmt.Errorf("start engine: %w", err)
 	}
-	_ = cmd.Process.Release()
-	return waitForSocket(p.EngineSock(), 5*time.Second)
+	_ = proc.Release()
+	return waitForEndpoint(p.EngineEndpoint(), 5*time.Second)
 }
 
 // engineEnv is a detached engine's environment: the caller's, with the
@@ -94,10 +96,14 @@ func ensureEngine(p paths.Paths) error {
 // environment (only the variables Codex passes to MCP servers), so API keys
 // set only in the shell environment may be missing for that engine's whole
 // life; keys in credentials.toml are unaffected.
+//
+// Names compare in platform.EnvKey form, so a Windows variable spelled in
+// another case is dropped or replaced too.
 func engineEnv(env []string, p paths.Paths) []string {
 	out := make([]string, 0, len(env)+3)
 	for _, kv := range env {
-		k, _, _ := strings.Cut(kv, "=")
+		name, _, _ := strings.Cut(kv, "=")
+		k := platform.EnvKey(name)
 		switch {
 		case k == "HERDR_PLUGIN_CONFIG_DIR", k == "HERDR_PLUGIN_STATE_DIR", k == "HERDR_SOCKET_PATH",
 			k == "HERDR_PANE_ID", k == "CLAUDECODE":
@@ -118,16 +124,19 @@ func claudeProviderSetting(k string) bool {
 		strings.HasPrefix(k, "CLAUDE_CODE_CLIENT_") || k == "CLAUDE_CODE_OAUTH_TOKEN"
 }
 
-func waitForSocket(path string, d time.Duration) error {
+// waitForEndpoint polls until the engine accepts connections at endpoint.
+func waitForEndpoint(endpoint string, d time.Duration) error {
 	deadline := time.Now().Add(d)
 	for {
-		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
+		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
+		conn, err := ipc.Dial(ctx, endpoint)
+		cancel()
 		if err == nil {
 			conn.Close()
 			return nil
 		}
 		if time.Now().After(deadline) {
-			return fmt.Errorf("engine socket %s not ready: %w", path, err)
+			return fmt.Errorf("engine endpoint %s not ready: %w", endpoint, err)
 		}
 		time.Sleep(50 * time.Millisecond)
 	}
@@ -216,18 +225,16 @@ func serveEngine(p paths.Paths) error {
 	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
 	defer cancel()
 
-	sock := p.EngineSock()
-	_ = os.Remove(sock) // stale socket from a dead engine; we hold the lock
-	ln, err := net.Listen("unix", sock)
+	endpoint := p.EngineEndpoint()
+	ln, err := ipc.Listen(endpoint) // replaces a stale socket: we hold the lock
 	if err != nil {
 		return err
 	}
-	_ = os.Chmod(sock, 0o600)
-	defer os.Remove(sock)
+	defer ln.Close() // also removes the unix socket file
 
 	e.Start(ctx)
 	go watchHerdr(ctx, cancel, hc, logger)
-	logger.Printf("engine started: session dir %s, socket %s", p.SessionDir, sock)
+	logger.Printf("engine started: session dir %s, endpoint %s", p.SessionDir, endpoint)
 	err = ipc.Serve(ctx, ln, e)
 	cancel()
 	e.Wait() // no goroutine may write state files after the lock is released
```

`cmd/herdr-dots/ui.go`:

```diff
--- a/cmd/herdr-dots/ui.go
+++ b/cmd/herdr-dots/ui.go
@@ -72,7 +72,7 @@ func uiSetup() (paths.Paths, ipc.Client, error) {
 	if err := ensureEngine(p); err != nil {
 		return p, ipc.Client{}, err
 	}
-	return p, ipc.Client{Path: p.EngineSock()}, nil
+	return p, ipc.Client{Path: p.EngineEndpoint()}, nil
 }
 
 // pumpSnapshots forwards engine snapshots to a program, restarting the engine
```

`cmd/herdr-dots/action.go`:

```diff
--- a/cmd/herdr-dots/action.go
+++ b/cmd/herdr-dots/action.go
@@ -47,7 +47,7 @@ func runAction(args []string) error {
 	case "toggle-pane":
 		err = togglePane(ctx, p, hc, target)
 	case "pause-all":
-		err = ipc.Client{Path: p.EngineSock()}.Call(ctx, ipc.CmdPauseAll, nil)
+		err = ipc.Client{Path: p.EngineEndpoint()}.Call(ctx, ipc.CmdPauseAll, nil)
 	default:
 		err = fmt.Errorf("unknown action %q", args[0])
 	}
```

Phase 5's MCP entry point is the only other `EngineSock()` caller.

`cmd/herdr-dots/mcp.go`:

```diff
--- a/cmd/herdr-dots/mcp.go
+++ b/cmd/herdr-dots/mcp.go
@@ -74,14 +74,14 @@ func serveMCP(ctx context.Context, args []string, getenv func(string) string, t
 		logger.Printf("started outside a Herdr pane (HERDR_PANE_ID %q, HERDR_SOCKET_PATH set: %v): every tool refuses", pane, sock != "")
 	} else {
 		deps.PaneID = pane
-		deps.Engine = mcpserver.NewIPCEngine(ipc.Client{Path: p.EngineSock()}, pane, func() error {
+		deps.Engine = mcpserver.NewIPCEngine(ipc.Client{Path: p.EngineEndpoint()}, pane, func() error {
 			if err := p.Ensure(); err != nil {
 				return err
 			}
 			return ensure(p)
 		})
 		deps.Memory = memoryStoreFor(p, cfg, logger.Printf)
-		logger.Printf("serving pane %s through %s", pane, p.EngineSock())
+		logger.Printf("serving pane %s through %s", pane, p.EngineEndpoint())
 	}
 	err = mcpserver.New(deps).Run(ctx, t)
 	logger.Printf("stopped: %v", err)
```

`cmd/herdr-dots/mcp_test.go` (its fake engine listens through `ipc.Listen` on the endpoint the server will dial; closing the listener removes the unix socket, which replaces the old `os.Remove(p.EngineSock())`):

```diff
--- a/cmd/herdr-dots/mcp_test.go
+++ b/cmd/herdr-dots/mcp_test.go
@@ -6,7 +6,6 @@ import (
 	"encoding/json"
 	"errors"
 	"io"
-	"net"
 	"os"
 	"path/filepath"
 	"slices"
@@ -139,7 +138,7 @@ func TestServeMCPNeedsAbsoluteDirs(t *testing.T) {
 	}
 }
 
-// mcpFakeEngine answers queries for one pane on the engine socket.
+// mcpFakeEngine answers queries for one pane on the engine endpoint.
 type mcpFakeEngine struct{ asked chan ipc.AskArgs }
 
 func (f mcpFakeEngine) Handle(context.Context, string, json.RawMessage) error { return nil }
@@ -165,13 +164,14 @@ func TestServeMCPAsksThePanesEngine(t *testing.T) {
 	ensured := 0
 	ensure := func(got paths.Paths) error {
 		ensured++
-		if got.EngineSock() != p.EngineSock() {
-			t.Errorf("engine socket %s, want %s", got.EngineSock(), p.EngineSock())
+		if got.EngineEndpoint() != p.EngineEndpoint() {
+			t.Errorf("engine endpoint %s, want %s", got.EngineEndpoint(), p.EngineEndpoint())
 		}
-		ln, err := net.Listen("unix", got.EngineSock())
+		ln, err := ipc.Listen(got.EngineEndpoint())
 		if err != nil {
 			return err
 		}
+		t.Cleanup(func() { ln.Close() }) // also removes the unix socket file
 		ctx, cancel := context.WithCancel(context.Background())
 		t.Cleanup(cancel)
 		go ipc.Serve(ctx, ln, fe)
@@ -188,7 +188,6 @@ func TestServeMCPAsksThePanesEngine(t *testing.T) {
 	if ensured != 1 {
 		t.Fatalf("ensure ran %d times", ensured)
 	}
-	t.Cleanup(func() { os.Remove(p.EngineSock()) })
 }
 
 func TestEngineEnvSetsThePluginDirs(t *testing.T) {
```

Check that nothing else calls the Unix-only names:

```bash
grep -rn 'EngineSock()\|waitForSocket(' --include='*.go' internal cmd | grep -v '^internal/paths/\|^internal/platform/'
```

Expected: no output. (If a commit after `a47427d` added a caller, replace `.EngineSock()` with `.EngineEndpoint()` there too; a test that checks unix-socket behavior moves into a file with `//go:build unix` at the top.)

- [ ] **Step 6: Run the tests to see them pass**

Run: `gofmt -w internal cmd && GOTOOLCHAIN=local go test -race ./internal/ipc/ ./internal/paths/ ./internal/herdr/... ./internal/engine/ ./cmd/herdr-dots/ -v 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'`
Expected: every package `ok`, including `TestTwoEndpointsNeverCrossTalk`, `TestListenReplacesAStaleSocketAndIsPrivate`, `TestEngineEndpointIsPerSession`, `TestEngineEndpointIsTheUnixSocket`, `TestEngineSockFallsBackWhenPathTooLong`, `TestWaitForEndpointTimesOut`, Phase 5's `TestServeWaitsForRunningQueriesOnShutdown`, `TestOversizedRequestIsRefused`, `TestServeMCPAsksThePanesEngine`, `TestEngineEnvSetsThePluginDirs`, and the whole engine suite over the fake Herdr.

- [ ] **Step 7: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/herdr/... ./internal/ipc/ ./internal/paths/ ./internal/engine/ ./cmd/herdr-dots/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/herdr/ ./internal/ipc/ ./internal/paths/ ./internal/engine/ ./cmd/herdr-dots/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed. The Windows `go vet` and `go test -c` of `./cmd/herdr-dots/` need the Phase 4 packages it imports; if they fail only inside `internal/backend/toolloop` (`undefined: syscall.O_NOFOLLOW`; Task 8, same wave), that is expected. The last two print only Wave 2's list without `cmd/herdr-dots`: `# herdr-dots/internal/backend/toolloop` until Task 8 is merged.

- [ ] **Step 8: Commit**

```bash
git add internal/herdr internal/ipc internal/paths cmd/herdr-dots
git commit -m "feat(ipc): named-pipe engine endpoint and Herdr pipe dial on Windows (Phase 6)"
```

(`cmd/herdr-dots` covers Phase 5's `mcp.go` and `mcp_test.go`.)

---

### Task 7: Files, client paths and git

**Depends on:** Task 2 (`platform.HerdrConfigPath`, `ClaudeProjectsDir`, `CodexSessionsDir`, `DevNull`, `HideWindow`, `EnvKey`), Task 3 (`platform.SecureFile`, `CheckPrivate`, `platformtest`). Wave 2.

**Files:**
- Modify: `internal/fsutil/atomic.go`, `internal/fsutil/atomic_test.go`
- Create: `internal/fsutil/private_unix.go`, `internal/fsutil/private_windows.go`
- Test: `internal/fsutil/atomic_unix_test.go`, `internal/fsutil/atomic_windows_test.go`
- Modify: `internal/transcript/claude.go`, `internal/transcript/codex.go`, `internal/transcript/claude_test.go`
- Modify: `internal/keys/keys.go`, `internal/keys/keys_test.go` (Phase 3 added the council key to both)
- Modify: `internal/memory/project.go`, `internal/memory/project_test.go`

**Interfaces:**
- Consumes: `platform.SecureFile(path string) error`, `platform.CheckPrivate(path string) error`, `platform.HerdrConfigPath(getenv func(string) string, home string) string`, `platform.ClaudeProjectsDir(home string) string`, `platform.CodexSessionsDir(home string) string`, `platform.DevNull`, `platform.HideWindow(cmd *exec.Cmd)`, `platform.EnvKey(name string) string`; `platformtest.AssertPrivateFile`.
- Produces: no new exported symbols. `fsutil.WriteFileAtomic` gives private writes an owner-only DACL on Windows and retries the rename while a reader holds the old file; `transcript.ResolveClaudePath`, `ResolveCodexPath` and `keys.ConfigPath` resolve per OS; `memory.ExecGit` uses the null device of the OS and opens no console window; `memory.gitEnv` folds name case on Windows.

- [ ] **Step 1: Write the tests**

`internal/fsutil/atomic_test.go`:

```diff
--- a/internal/fsutil/atomic_test.go
+++ b/internal/fsutil/atomic_test.go
@@ -4,6 +4,8 @@ import (
 	"os"
 	"path/filepath"
 	"testing"
+
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func TestWriteFileAtomicCreatesWithPerm(t *testing.T) {
@@ -16,13 +18,9 @@ func TestWriteFileAtomicCreatesWithPerm(t *testing.T) {
 	if string(got) != "hello" {
 		t.Fatalf("content = %q", got)
 	}
-	st, _ := os.Stat(p)
-	if st.Mode().Perm() != 0o600 {
-		t.Fatalf("perm = %v", st.Mode().Perm())
-	}
-	dst, _ := os.Stat(filepath.Dir(p))
-	if dst.Mode().Perm() != 0o700 {
-		t.Fatalf("dir perm = %v", dst.Mode().Perm())
+	platformtest.AssertPrivateFile(t, p)
+	if st, _ := os.Stat(filepath.Dir(p)); !st.IsDir() {
+		t.Fatal("parent dir not created")
 	}
 }
 
```

`internal/fsutil/atomic_unix_test.go`:

```go
//go:build unix

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicCreatesParentWith0700(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "f.json")
	if err := WriteFileAtomic(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Dir(p)); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %v", st.Mode().Perm())
	}
}
```

`internal/fsutil/atomic_windows_test.go`:

```go
//go:build windows

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"herdr-dots/internal/platform"
)

// A reader holding the old file open (Go opens without FILE_SHARE_DELETE)
// makes the replacing rename fail for a moment; the write must wait it out.
func TestWriteFileAtomicWaitsForAReader(t *testing.T) {
	p := filepath.Join(t.TempDir(), "queue.json")
	if err := WriteFileAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(150 * time.Millisecond); f.Close() }()
	if err := WriteFileAtomic(p, []byte("two"), 0o600); err != nil {
		t.Fatalf("write while a reader had the file open: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "two" {
		t.Fatalf("content %q", got)
	}
}

// A private write into a directory that lets Everyone in still ends up
// owner-only: the DACL is set on the file itself, not left to inheritance.
func TestPrivateWritesGetAnOwnerOnlyDACLEvenInAnOpenDir(t *testing.T) {
	dir := t.TempDir()
	sd, err := windows.SecurityDescriptorFromString("D:(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(dir, "attachments.json")
	if err := WriteFileAtomic(priv, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := platform.CheckPrivate(priv); err != nil {
		t.Fatalf("0600 write in an open dir: %v", err)
	}
}
```

`internal/transcript/claude_test.go` (Review Focus 1):

```diff
--- a/internal/transcript/claude_test.go
+++ b/internal/transcript/claude_test.go
@@ -105,3 +105,34 @@ func TestParseClaudeRealFixture(t *testing.T) {
 		}
 	}
 }
+
+// claudeWindowsSample is a Claude Code transcript as written on Windows:
+// backslash paths with spaces and non-ASCII characters in tool inputs.
+const claudeWindowsSample = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"Add login"},"sessionId":"s1","cwd":"C:\\Users\\José Núñez\\src\\my app"}
+{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"C:\\Users\\José Núñez\\src\\my app\\login.tsx","old_string":"a","new_string":"b"}}]}}
+{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"Added login form."}]}}
+`
+
+// Review Focus: a project path with spaces and non-ASCII characters.
+func TestWindowsTranscriptUnderANonASCIIHome(t *testing.T) {
+	home := filepath.Join(t.TempDir(), "José Núñez")
+	dir := filepath.Join(home, ".claude", "projects", "C--Users-Jos--N--ez-src-my-app")
+	if err := os.MkdirAll(dir, 0o700); err != nil {
+		t.Fatal(err)
+	}
+	f := filepath.Join(dir, "0f9e-77.jsonl")
+	if err := os.WriteFile(f, []byte(claudeWindowsSample), 0o600); err != nil {
+		t.Fatal(err)
+	}
+	p, err := ResolveClaudePath(home, "id", "0f9e-77")
+	if err != nil || p != f {
+		t.Fatalf("ResolveClaudePath = %q %v", p, err)
+	}
+	turns, err := ClaudeReader{Path: p}.Turns(context.Background())
+	if err != nil || len(turns) != 1 || turns[0].Assistant != "Added login form." {
+		t.Fatalf("turns %+v %v", turns, err)
+	}
+	if len(turns[0].Tools) != 1 || turns[0].Tools[0].Paths[0] != `C:\Users\José Núñez\src\my app\login.tsx` {
+		t.Fatalf("tool paths %+v", turns[0].Tools)
+	}
+}
```

`internal/keys/keys_test.go` (expected paths built with `filepath.Join`; the mode checks compare with the mode before the write, because Windows reports `0666`; a symlink the OS refuses skips the test):

```diff
--- a/internal/keys/keys_test.go
+++ b/internal/keys/keys_test.go
@@ -3,11 +3,14 @@ package keys
 import (
 	"os"
 	"path/filepath"
+	"runtime"
 	"strings"
 	"testing"
 	"time"
 
 	"github.com/BurntSushi/toml"
+
+	"herdr-dots/internal/platform"
 )
 
 const userConfig = `onboarding = false
@@ -29,11 +32,11 @@ command = "someone.else.thing"
 func TestConfigPath(t *testing.T) {
 	env := map[string]string{}
 	get := func(k string) string { return env[k] }
-	if p := ConfigPath(get, "/h"); p != "/h/.config/herdr/config.toml" {
+	if p := ConfigPath(get, "/h"); p != filepath.Join("/h", ".config", "herdr", "config.toml") {
 		t.Fatal(p)
 	}
 	env["XDG_CONFIG_HOME"] = "/x"
-	if p := ConfigPath(get, "/h"); p != "/x/herdr/config.toml" {
+	if p := ConfigPath(get, "/h"); p != filepath.Join("/x", "herdr", "config.toml") {
 		t.Fatal(p)
 	}
 	env["HERDR_CONFIG_PATH"] = "/custom.toml"
@@ -83,6 +86,7 @@ func TestAppendRefusesWhenResultWouldNotParse(t *testing.T) {
 	dir := t.TempDir()
 	p := filepath.Join(dir, "config.toml")
 	os.WriteFile(p, []byte(inline), 0o640)
+	before, _ := os.Stat(p)
 	if _, err := Existing([]byte(inline)); err != nil {
 		t.Fatalf("the inline form is valid TOML on its own: %v", err)
 	}
@@ -93,8 +97,8 @@ func TestAppendRefusesWhenResultWouldNotParse(t *testing.T) {
 	if string(now) != inline {
 		t.Fatalf("file must be untouched, got:\n%s", now)
 	}
-	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
-		t.Fatalf("mode changed: %v", st.Mode().Perm())
+	if st, _ := os.Stat(p); st.Mode().Perm() != before.Mode().Perm() {
+		t.Fatalf("mode changed: %v -> %v", before.Mode().Perm(), st.Mode().Perm())
 	}
 }
 
@@ -102,8 +106,12 @@ func TestAppendPreservesFileModeAndSymlink(t *testing.T) {
 	dir := t.TempDir()
 	real := filepath.Join(dir, "dotfiles-config.toml")
 	os.WriteFile(real, []byte(userConfig), 0o640)
+	before, _ := os.Stat(real)
 	link := filepath.Join(dir, "config.toml")
 	if err := os.Symlink(real, link); err != nil {
+		if runtime.GOOS == "windows" {
+			t.Skipf("symlinks unavailable: %v", err)
+		}
 		t.Fatal(err)
 	}
 	if _, err := Append(link, Defaults()[2:], time.Now()); err != nil {
@@ -113,8 +121,8 @@ func TestAppendPreservesFileModeAndSymlink(t *testing.T) {
 		t.Fatal("a symlinked config must stay a symlink")
 	}
 	st, _ := os.Stat(real)
-	if st.Mode().Perm() != 0o640 {
-		t.Fatalf("mode %v, want 0640", st.Mode().Perm())
+	if st.Mode().Perm() != before.Mode().Perm() {
+		t.Fatalf("mode %v, want %v", st.Mode().Perm(), before.Mode().Perm())
 	}
 	if data, _ := os.ReadFile(real); !strings.Contains(string(data), "prefix+shift+s") {
 		t.Fatalf("not appended:\n%s", data)
@@ -164,3 +172,12 @@ func TestCouncilKeyIsFreeUnlessTheUserBoundIt(t *testing.T) {
 		t.Fatalf("a user binding wins: %v", conflicts)
 	}
 }
+
+func TestConfigPathFollowsHerdrOnThisOS(t *testing.T) {
+	env := map[string]string{"APPDATA": filepath.Join("C:", "Users", "José Núñez", "AppData", "Roaming")}
+	get := func(k string) string { return env[k] }
+	home := filepath.Join("Users", "José Núñez")
+	if got, want := ConfigPath(get, home), platform.HerdrConfigPath(get, home); got != want {
+		t.Fatalf("ConfigPath %q, platform says %q", got, want)
+	}
+}
```

`internal/memory/project_test.go`:

```diff
--- a/internal/memory/project_test.go
+++ b/internal/memory/project_test.go
@@ -8,6 +8,8 @@ import (
 	"strings"
 	"testing"
 	"time"
+
+	"herdr-dots/internal/platform"
 )
 
 // isolateGit keeps the developer's git config (signing, hooks) out of tests.
@@ -208,3 +210,18 @@ func TestGitEnvStripsHerdrAndGitOverrides(t *testing.T) {
 		t.Error("must return a non-nil slice")
 	}
 }
+
+func TestGitEnvFoldsCaseWhereTheOSDoes(t *testing.T) {
+	in := []string{"Git_Dir=/x", "herdr_pane_id=w1:p1", "Path=/bin"}
+	out := gitEnv(in)
+	joined := strings.Join(out, "\n")
+	if platform.EnvKey("a") == "A" { // Windows: Git_Dir is GIT_DIR
+		if strings.Contains(joined, "Git_Dir") || strings.Contains(joined, "herdr_pane_id") || !strings.Contains(joined, "Path=/bin") {
+			t.Fatalf("Windows gitEnv = %v", out)
+		}
+		return
+	}
+	if !strings.Contains(joined, "Git_Dir=/x") || !strings.Contains(joined, "herdr_pane_id") {
+		t.Fatalf("Unix gitEnv = %v", out)
+	}
+}
```

- [ ] **Step 2: Run the tests**

Run: `GOTOOLCHAIN=local go test ./internal/fsutil/ ./internal/transcript/ ./internal/keys/ ./internal/memory/ -run 'WriteFileAtomic|NonASCIIHome|ConfigPath|GitEnv|ExecGit' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS on darwin. Unix behavior doesn't change in this task, so these tests are the **Windows** regression tests: on Windows, `TestWriteFileAtomicWaitsForAReader`, `TestPrivateWritesGetAnOwnerOnlyDACLEvenInAnOpenDir`, `TestGitEnvFoldsCaseWhereTheOSDoes` and Phase 2's `TestExecGitNeverRunsHooks` (which expects `os.DevNull`, that is `NUL`) fail until Step 3. Here, confirm what Step 3 replaces: `grep -n 'hooksPath=/dev/null\|os.Rename(tmp, path)\|".claude", "projects"\|".codex", "sessions"' internal/memory/project.go internal/fsutil/atomic.go internal/transcript/claude.go internal/transcript/codex.go` prints four lines.

- [ ] **Step 3: Implement**

`internal/fsutil/atomic.go`:

```diff
--- a/internal/fsutil/atomic.go
+++ b/internal/fsutil/atomic.go
@@ -8,7 +8,10 @@ import (
 
 // WriteFileAtomic writes data to a temp file in the same directory and renames
 // it over path, so readers never observe a half-written file. The parent
-// directory is created with 0700.
+// directory is created with 0700. A private perm (no group or other bits)
+// also gets an owner-only DACL on Windows, and a write whose privacy can't be
+// set fails. On Windows the rename is retried briefly while another process
+// has the old file open.
 func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
 	dir := filepath.Dir(path)
 	if err := os.MkdirAll(dir, 0o700); err != nil {
@@ -28,6 +31,10 @@ func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
 		f.Close()
 		return err
 	}
+	if err := restrict(tmp, perm); err != nil {
+		f.Close()
+		return err
+	}
 	if err := f.Sync(); err != nil {
 		f.Close()
 		return err
@@ -35,5 +42,5 @@ func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
 	if err := f.Close(); err != nil {
 		return err
 	}
-	return os.Rename(tmp, path)
+	return rename(tmp, path)
 }
```

`internal/fsutil/private_unix.go`:

```go
//go:build unix

package fsutil

import "os"

// restrict: Chmod already applied the Unix permission bits.
func restrict(string, os.FileMode) error { return nil }

func rename(from, to string) error { return os.Rename(from, to) }
```

`internal/fsutil/private_windows.go`:

```go
//go:build windows

package fsutil

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"

	"herdr-dots/internal/platform"
)

// restrict gives a file written with a private perm an owner-only DACL;
// Windows ignores Unix permission bits.
func restrict(path string, perm os.FileMode) error {
	if perm&0o077 != 0 {
		return nil
	}
	return platform.SecureFile(path)
}

// renameTries x renameWait bounds how long rename waits for a reader (Go
// opens files without FILE_SHARE_DELETE, which blocks replacing them).
const (
	renameTries = 40
	renameWait  = 25 * time.Millisecond
)

func rename(from, to string) error {
	var err error
	for i := 0; i < renameTries; i++ {
		if err = os.Rename(from, to); err == nil || !busy(err) {
			return err
		}
		time.Sleep(renameWait)
	}
	return err
}

func busy(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
```

`internal/transcript/claude.go`:

```diff
--- a/internal/transcript/claude.go
+++ b/internal/transcript/claude.go
@@ -11,6 +11,8 @@ import (
 	"os"
 	"path/filepath"
 	"strings"
+
+	"herdr-dots/internal/platform"
 )
 
 type claudeRecord struct {
@@ -151,7 +153,9 @@ func (r ClaudeReader) Turns(context.Context) ([]Turn, error) {
 }
 
 // ResolveClaudePath finds the transcript for a Herdr-reported session: a
-// direct path, or <home>/.claude/projects/*/<id>.jsonl.
+// direct path, or <home>/.claude/projects/*/<id>.jsonl. The glob covers every
+// project folder, so it doesn't depend on how Claude Code encodes a cwd
+// (C:\Users\x\repo becomes C--Users-x-repo on Windows).
 func ResolveClaudePath(home, kind, value string) (string, error) {
 	switch kind {
 	case "path":
@@ -163,7 +167,7 @@ func ResolveClaudePath(home, kind, value string) (string, error) {
 		if value == "" || strings.ContainsAny(value, `/\*?[]`) || strings.Contains(value, "..") {
 			return "", fmt.Errorf("invalid claude session id %q", value)
 		}
-		matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", value+".jsonl"))
+		matches, _ := filepath.Glob(filepath.Join(platform.ClaudeProjectsDir(home), "*", value+".jsonl"))
 		if len(matches) == 0 {
 			return "", fmt.Errorf("no transcript for claude session %s", value)
 		}
```

`internal/transcript/codex.go`:

```diff
--- a/internal/transcript/codex.go
+++ b/internal/transcript/codex.go
@@ -12,6 +12,8 @@ import (
 	"path/filepath"
 	"regexp"
 	"strings"
+
+	"herdr-dots/internal/platform"
 )
 
 // Codex rollout files (~/.codex/sessions/YYYY/MM/DD/rollout-*-<id>.jsonl).
@@ -103,7 +105,7 @@ func ResolveCodexPath(home, kind, value string) (string, error) {
 		if value == "" || strings.ContainsAny(value, `/\*?[]`) || strings.Contains(value, "..") {
 			return "", fmt.Errorf("invalid codex session id %q", value)
 		}
-		matches, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*-"+value+".jsonl"))
+		matches, _ := filepath.Glob(filepath.Join(platform.CodexSessionsDir(home), "*", "*", "*", "rollout-*-"+value+".jsonl"))
 		if len(matches) == 0 {
 			return "", fmt.Errorf("no transcript for codex session %s", value)
 		}
```

`internal/keys/keys.go` (post-Phase-3 form: the council binding in `Defaults` is untouched):

```diff
--- a/internal/keys/keys.go
+++ b/internal/keys/keys.go
@@ -13,6 +13,7 @@ import (
 	"github.com/BurntSushi/toml"
 
 	"herdr-dots/internal/fsutil"
+	"herdr-dots/internal/platform"
 )
 
 type Binding struct {
@@ -46,14 +47,11 @@ var BuiltinPrefixKeys = func() []string {
 	return ks
 }()
 
+// ConfigPath is Herdr's config.toml (platform.HerdrConfigPath: on Windows
+// %APPDATA%\herdr\config.toml unless HERDR_CONFIG_PATH or XDG_CONFIG_HOME
+// says otherwise).
 func ConfigPath(getenv func(string) string, home string) string {
-	if p := getenv("HERDR_CONFIG_PATH"); p != "" {
-		return p
-	}
-	if x := getenv("XDG_CONFIG_HOME"); x != "" {
-		return filepath.Join(x, "herdr", "config.toml")
-	}
-	return filepath.Join(home, ".config", "herdr", "config.toml")
+	return platform.HerdrConfigPath(getenv, home)
 }
 
 // Existing maps every bound key in the user's config to what it triggers.
```

`internal/memory/project.go`:

```diff
--- a/internal/memory/project.go
+++ b/internal/memory/project.go
@@ -12,6 +12,8 @@ import (
 	"sort"
 	"strings"
 	"time"
+
+	"herdr-dots/internal/platform"
 )
 
 // Project identifies the repository a pane works in.
@@ -27,23 +29,28 @@ type GitRunner func(ctx context.Context, dir string, args ...string) (string, er
 // gitTimeout bounds one ResolveProject call; tests shorten it.
 var gitTimeout = 5 * time.Second
 
-// ExecGit runs git with hooks disabled, no terminal prompts and no optional locks.
+// ExecGit runs git with hooks disabled (core.hooksPath is the null device:
+// /dev/null, or NUL on Windows), no terminal prompts, no optional locks and,
+// on Windows, no console window.
 func ExecGit(ctx context.Context, dir string, args ...string) (string, error) {
-	full := append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null"}, args...)
+	full := append([]string{"-C", dir, "-c", "core.hooksPath=" + platform.DevNull}, args...)
 	cmd := exec.CommandContext(ctx, "git", full...)
 	cmd.Env = gitEnv(os.Environ())
 	cmd.WaitDelay = 500 * time.Millisecond
+	platform.HideWindow(cmd)
 	out, err := cmd.Output()
 	return strings.TrimSpace(string(out)), err
 }
 
 // gitEnv filters env for a git child: no HERDR_*/Claude session variables, and
 // none of the GIT_* variables that would override -C or the config. (A local
-// filter: importing internal/backend here would be an import cycle.)
+// filter: importing internal/backend here would be an import cycle.) Names
+// compare in platform.EnvKey form, so case-insensitively on Windows.
 func gitEnv(env []string) []string {
 	out := make([]string, 0, len(env)+3)
 	for _, kv := range env {
-		name, _, _ := strings.Cut(kv, "=")
+		raw, _, _ := strings.Cut(kv, "=")
+		name := platform.EnvKey(raw)
 		if strings.HasPrefix(name, "HERDR_") || strings.HasPrefix(name, "GIT_CONFIG") {
 			continue
 		}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -w internal && GOTOOLCHAIN=local go test -race ./internal/fsutil/ ./internal/transcript/ ./internal/keys/ ./internal/memory/`
Expected: PASS.

- [ ] **Step 5: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/fsutil/ ./internal/transcript/ ./internal/keys/ ./internal/memory/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/fsutil/ ./internal/transcript/ ./internal/keys/ ./internal/memory/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed; the last two print only Wave 2's list (`# herdr-dots/internal/backend/toolloop`, `# herdr-dots/cmd/herdr-dots`, each until its task merges).

- [ ] **Step 6: Commit**

```bash
git add internal/fsutil internal/transcript internal/keys internal/memory/project.go internal/memory/project_test.go
git commit -m "feat(platform): Windows file privacy and replace, client paths, git null device (Phase 6)"
```

---

### Task 8: Phase 4 ports: credentials, tool jail, connection-refused checks

**Depends on:** Task 2 (`platform.DevNull`, `HideWindow`, `EnvKey`, `ConnRefused`, `ErrConnRefused`), Task 3 (`platform.CheckPrivateFile`, `FixPrivateHint`, `ErrNotPrivate`, `ErrNotOwner`, `OpenNoFollow`, `ReservedPathName`, `platformtest`). Wave 2.

The diffs are against the merged Phase 4 code at `a47427d` (reconciled: Plan decisions 25–28). Phase 4 already opens `credentials.toml` with `O_NOFOLLOW` and checks the descriptor, already folds case in the jail's deny-list and already refuses `/` and the home directory; this task keeps all of that and makes it hold on Windows.

**Files:**
- Modify: `internal/credentials/credentials.go`, `internal/credentials/open_unix.go`, `internal/credentials/credentials_test.go`, `internal/credentials/hardening_test.go`
- Rename: `internal/credentials/open_other.go` → `internal/credentials/open_windows.go` (new content)
- Test: `internal/credentials/perms_unix_test.go`, `internal/credentials/perms_windows_test.go`
- Modify: `internal/backend/toolloop/jail.go`, `internal/backend/toolloop/tools.go`, `internal/backend/toolloop/tools_test.go`, `fix1_test.go`, `fix2_test.go`, `final_test.go`
- Test: `internal/backend/toolloop/jail_windows_test.go`
- Modify: `internal/backend/ollama/ollama.go`, `internal/backend/api.go`, `internal/backend/openaiapi/openai.go`, `internal/backend/anthropicapi/anthropic.go`, `internal/backend/httpx/httpx.go`, `internal/backend/httpx/final_test.go`
- Test: `internal/backend/ollama/connrefused_test.go`

**Interfaces:**
- Consumes: `platform.CheckPrivateFile(f *os.File) error`, `platform.ErrNotPrivate`, `platform.ErrNotOwner`, `platform.FixPrivateHint(path string) string`, `platform.OpenNoFollow(path string) (*os.File, error)`, `platform.ReservedPathName(rel string) bool`, `platform.DevNull`, `platform.HideWindow(cmd *exec.Cmd)`, `platform.EnvKey(name string) string`, `platform.ConnRefused(err error) bool`, `platform.ErrConnRefused`; `platformtest.AssertPrivateFile`.
- Produces: no new exported symbols. `credentials.ErrInsecurePerms` keeps its identity; its message is now "credentials.toml is readable by others" and the returned error carries the OS's fix. `credentials` checks privacy on the opened file on every OS (`open_unix.go` for every Unix, `open_windows.go` on Windows). `toolloop.ResolveInRoot` denies Windows streams, devices and trailing dots/spaces before touching the disk; `toolloop.Unavailable` refuses any file-system root (`/`, `C:\`, a share root); the jail opens files through `platform.OpenNoFollow` and runs git with the OS null device and no console window. `backend.Unreachable`, the OpenAI, Anthropic and Ollama adapters and `httpx`'s scrubbed errors recognise a refused connection on Windows (`WSAECONNREFUSED`).

- [ ] **Step 1: Write the failing tests**

`internal/credentials/credentials_test.go` (privacy asserted per OS; the permissive file is made readable by others the OS's way; the error must carry the fix and never the key):

```diff
--- a/internal/credentials/credentials_test.go
+++ b/internal/credentials/credentials_test.go
@@ -6,6 +6,9 @@ import (
 	"path/filepath"
 	"strings"
 	"testing"
+
+	"herdr-dots/internal/platform"
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func envOf(m map[string]string) func(string) string {
@@ -22,10 +25,7 @@ func TestSetWritesOwnerOnlyAndLookupReadsIt(t *testing.T) {
 	if err := f.Set("anthropic", "  sk-ant-api03-abcdefghijklmnop  "); err != nil {
 		t.Fatal(err)
 	}
-	st, err := os.Stat(f.Path)
-	if err != nil || st.Mode().Perm() != 0o600 {
-		t.Fatalf("mode %v err %v, want 0600", st.Mode().Perm(), err)
-	}
+	platformtest.AssertPrivateFile(t, f.Path)
 	key, from, err := f.Lookup("anthropic")
 	if err != nil || key != "sk-ant-api03-abcdefghijklmnop" || from != "file" {
 		t.Fatalf("Lookup = %q %q %v", key, from, err)
@@ -64,11 +64,16 @@ func TestMissingKeyHasAHint(t *testing.T) {
 
 func TestPermissiveFileIsRefusedWithoutReadingIt(t *testing.T) {
 	f := credFile(t)
-	if err := os.WriteFile(f.Path, []byte("[anthropic]\napi_key = \"sk-ant-api03-abcdefghijklmnop\"\n"), 0o644); err != nil {
+	if err := os.WriteFile(f.Path, []byte("[anthropic]\napi_key = \"sk-ant-api03-abcdefghijklmnop\"\n"), 0o600); err != nil {
 		t.Fatal(err)
 	}
-	if _, _, err := f.Lookup("anthropic"); !errors.Is(err, ErrInsecurePerms) {
-		t.Fatalf("Lookup err = %v, want ErrInsecurePerms", err)
+	readableByOthers(t, f.Path) // 0644 on Unix; an Everyone ACE on Windows
+	_, _, err := f.Lookup("anthropic")
+	if !errors.Is(err, ErrInsecurePerms) || !strings.Contains(err.Error(), platform.FixPrivateHint(f.Path)) {
+		t.Fatalf("Lookup err = %v, want ErrInsecurePerms with the fix", err)
+	}
+	if strings.Contains(err.Error(), "sk-ant") {
+		t.Fatal("the error quoted the key")
 	}
 	if err := f.Set("openai", "sk-xxxxxxxxxxxxxxxxxxxx"); !errors.Is(err, ErrInsecurePerms) {
 		t.Fatalf("Set err = %v, want ErrInsecurePerms", err)
@@ -84,9 +89,7 @@ func TestSymlinkedCredentialsFileIsRefused(t *testing.T) {
 	if err := os.WriteFile(real, []byte(""), 0o600); err != nil {
 		t.Fatal(err)
 	}
-	if err := os.Symlink(real, f.Path); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, real, f.Path)
 	if _, _, err := f.Lookup("anthropic"); err == nil || !strings.Contains(err.Error(), "regular file") {
 		t.Fatalf("err = %v", err)
 	}
```

`internal/credentials/hardening_test.go` (Phase 4's symlink test skips where the OS refuses symlinks):

```diff
--- a/internal/credentials/hardening_test.go
+++ b/internal/credentials/hardening_test.go
@@ -39,9 +39,7 @@ func TestSymlinkRefusedEvenWhenTargetIsOwnerOnly(t *testing.T) {
 	if err := os.WriteFile(real, []byte("[anthropic]\napi_key = \"sk-ant-api03-abcdefghijklmnop\"\n"), 0o600); err != nil {
 		t.Fatal(err)
 	}
-	if err := os.Symlink(real, f.Path); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, real, f.Path)
 	k, _, err := f.Lookup("anthropic")
 	if err == nil || k != "" || !strings.Contains(err.Error(), "regular file") {
 		t.Fatalf("key %q err %v", k, err)
```

`internal/credentials/perms_unix_test.go`:

```go
//go:build unix

package credentials

import (
	"os"
	"testing"
)

func readableByOthers(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
```

`internal/credentials/perms_windows_test.go` (Review Focus 5):

```go
//go:build windows

package credentials

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// setDACL replaces path's DACL with sddl.
func setDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func me(t *testing.T) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return tu.User.Sid.String()
}

// readableByOthers gives Everyone read access, as a file copied from a
// shared folder or another machine can carry.
func readableByOthers(t *testing.T, path string) {
	t.Helper()
	setDACL(t, path, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;WD)", me(t)))
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// Review Focus: a credentials file copied with inherited Users access.
func TestInheritedUsersAccessIsRefusedAndSetFixesNothingSilently(t *testing.T) {
	f := credFile(t)
	if err := os.WriteFile(f.Path, []byte("[openai]\napi_key = \"sk-xxxxxxxxxxxxxxxxxxxx\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setDACL(t, f.Path, fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FR;;;BU)", me(t))) // BUILTIN\Users may read
	if _, _, err := f.Lookup("openai"); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("err = %v, want ErrInsecurePerms", err)
	}
	if err := f.Set("openai", "sk-yyyyyyyyyyyyyyyyyyyy"); !errors.Is(err, ErrInsecurePerms) {
		t.Fatalf("Set must refuse too: %v", err)
	}
}
```

`internal/backend/toolloop/tools_test.go` (symlinks skip where the OS refuses them; `setHome` sets `USERPROFILE` too, Plan decision 28; streams and devices never resolve):

```diff
--- a/internal/backend/toolloop/tools_test.go
+++ b/internal/backend/toolloop/tools_test.go
@@ -7,6 +7,7 @@ import (
 	"os"
 	"os/exec"
 	"path/filepath"
+	"runtime"
 	"strings"
 	"testing"
 )
@@ -31,15 +32,9 @@ func repoDir(t *testing.T) (root, outside string) {
 	write(root, ".env", "API_KEY=supersecret\n")
 	write(root, "config/secrets/prod.json", `{"token":"x"}`)
 	write(root, "bin/blob.dat", "ab\x00cd")
-	if err := os.Symlink(filepath.Join(outside, "notes.txt"), filepath.Join(root, "escape.txt")); err != nil {
-		t.Fatal(err)
-	}
-	if err := os.Symlink(outside, filepath.Join(root, "outdir")); err != nil {
-		t.Fatal(err)
-	}
-	if err := os.Symlink(".env", filepath.Join(root, "innocent.txt")); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, filepath.Join(outside, "notes.txt"), filepath.Join(root, "escape.txt"))
+	symlinkOrSkip(t, outside, filepath.Join(root, "outdir"))
+	symlinkOrSkip(t, ".env", filepath.Join(root, "innocent.txt"))
 	return root, outside
 }
 
@@ -167,9 +162,7 @@ func TestOpenRejectsParentSwappedForSymlink(t *testing.T) {
 	if err := os.Rename(filepath.Join(root, "sub"), filepath.Join(root, "sub.old")); err != nil {
 		t.Fatal(err)
 	}
-	if err := os.Symlink(outside, filepath.Join(root, "sub")); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, outside, filepath.Join(root, "sub"))
 	f, err := openVerified(abs)
 	if err == nil {
 		f.Close()
@@ -240,3 +233,32 @@ func TestListWithoutGitWalksAndCaps(t *testing.T) {
 		t.Fatalf("list:\n%s\n%v", out, err)
 	}
 }
+
+// setHome points the user's home directory at dir on every OS:
+// os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows.
+func setHome(t *testing.T, dir string) {
+	t.Helper()
+	t.Setenv("HOME", dir)
+	t.Setenv("USERPROFILE", dir)
+}
+
+// symlinkOrSkip creates a symlink, skipping the test where the OS refuses
+// (Windows without Developer Mode or admin rights).
+func symlinkOrSkip(t *testing.T, target, link string) {
+	t.Helper()
+	if err := os.Symlink(target, link); err != nil {
+		if runtime.GOOS == "windows" {
+			t.Skipf("symlinks unavailable: %v", err)
+		}
+		t.Fatal(err)
+	}
+}
+
+func TestResolveInRootNeverReachesAStreamOrDevice(t *testing.T) {
+	root, _ := repoDir(t)
+	for _, p := range []string{".env::$DATA", "main.go:hidden", "NUL", "web/CON.ts", ".env."} {
+		if abs, _, err := ResolveInRoot(root, p, DefaultDeny); err == nil {
+			t.Errorf("%s resolved to %s", p, abs)
+		}
+	}
+}
```

`internal/backend/toolloop/fix1_test.go` (Phase 4's fix-wave tests use the same two helpers, as do `fix2_test.go` and `final_test.go` below):

```diff
--- a/internal/backend/toolloop/fix1_test.go
+++ b/internal/backend/toolloop/fix1_test.go
@@ -131,7 +131,7 @@ func TestNoToolsForHomeOrAboveIt(t *testing.T) {
 	if err := os.MkdirAll(proj, 0o700); err != nil {
 		t.Fatal(err)
 	}
-	t.Setenv("HOME", home)
+	setHome(t, home)
 	for _, r := range []string{home, parent, "/"} {
 		if got := RepoTools(r, DefaultLimits()); len(got) != 0 {
 			t.Errorf("RepoTools(%q) gave %d tools", r, len(got))
@@ -151,9 +151,7 @@ func TestNoToolsForHomeOrAboveIt(t *testing.T) {
 // Minor 4: no existence oracle outside the root.
 func TestNoExistenceOracleOutsideRoot(t *testing.T) {
 	root, outside := repoDir(t)
-	if err := os.Symlink(filepath.Join(outside, "missing.txt"), filepath.Join(root, "dangling.txt")); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, filepath.Join(outside, "missing.txt"), filepath.Join(root, "dangling.txt"))
 	for _, p := range []string{"dangling.txt", "outdir/missing.txt", "outdir/notes.txt"} {
 		_, _, err := ResolveInRoot(root, p, DefaultDeny)
 		if !errors.Is(err, ErrOutsideRoot) {
```

`internal/backend/toolloop/fix2_test.go`:

```diff
--- a/internal/backend/toolloop/fix2_test.go
+++ b/internal/backend/toolloop/fix2_test.go
@@ -69,9 +69,7 @@ func TestOrdinaryCaseVariantIsNoSuchFile(t *testing.T) {
 func TestSymlinkTargetSpellingIsExactToo(t *testing.T) {
 	root := t.TempDir()
 	mustWrite(t, filepath.Join(root, "id_rsa"), "PRIVATE KEY")
-	if err := os.Symlink("id_rſa", filepath.Join(root, "link.txt")); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, "id_rſa", filepath.Join(root, "link.txt"))
 	if _, _, err := ResolveInRoot(root, "link.txt", DefaultDeny); err == nil {
 		t.Fatal("a link spelled with a folded name must not resolve")
 	}
@@ -93,9 +91,7 @@ func TestListFiltersStaleIndexEntriesUnderSymlinkedParents(t *testing.T) {
 		t.Fatal(err)
 	}
 	mustWrite(t, filepath.Join(outside, "a.go"), "x")
-	if err := os.Symlink(outside, filepath.Join(root, "real")); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, outside, filepath.Join(root, "real"))
 	out, err := run(t, RepoTools(root, DefaultLimits()), "list_files", map[string]any{})
 	if err != nil || strings.Contains(out, "real/a.go") {
 		t.Fatalf("list leaked via a stale entry:\n%s\n%v", out, err)
@@ -108,7 +104,7 @@ func TestUnavailableFallsBackWhenHomeIsUnsetAndMatchesCaseVariants(t *testing.T)
 	if err := os.MkdirAll(home, 0o700); err != nil {
 		t.Fatal(err)
 	}
-	t.Setenv("HOME", home)
+	setHome(t, home)
 	variant := filepath.Join(parent, "hOME")
 	if _, err := os.Stat(variant); err == nil {
 		if !Unavailable(variant) {
@@ -117,7 +113,7 @@ func TestUnavailableFallsBackWhenHomeIsUnsetAndMatchesCaseVariants(t *testing.T)
 	} else {
 		t.Log("case-sensitive filesystem: variant check skipped")
 	}
-	t.Setenv("HOME", "")
+	setHome(t, "")
 	if h := homeDir(); h == "" {
 		t.Error("homeDir must fall back to the user database when HOME is unset")
 	}
@@ -125,7 +121,7 @@ func TestUnavailableFallsBackWhenHomeIsUnsetAndMatchesCaseVariants(t *testing.T)
 
 func TestHomeAnchoredGlobsApplyWhenCwdIsBelowHome(t *testing.T) {
 	home := t.TempDir()
-	t.Setenv("HOME", home)
+	setHome(t, home)
 	mustWrite(t, filepath.Join(home, ".config", "gh", "hosts.yml"), "oauth_token: SECRET")
 	mustWrite(t, filepath.Join(home, ".config", "ok.txt"), "fine")
 	mustWrite(t, filepath.Join(home, ".docker", "config.json"), "SECRET")
```

`internal/backend/toolloop/final_test.go`:

```diff
--- a/internal/backend/toolloop/final_test.go
+++ b/internal/backend/toolloop/final_test.go
@@ -19,10 +19,8 @@ func TestRootReachingHomeThroughASuffixIsUnavailable(t *testing.T) {
 		t.Fatal(err)
 	}
 	// data/Users is the same directory as tmp/Users (a firmlink stand-in).
-	if err := os.Symlink(filepath.Join(tmp, "Users"), filepath.Join(data, "Users")); err != nil {
-		t.Fatal(err)
-	}
-	t.Setenv("HOME", home)
+	symlinkOrSkip(t, filepath.Join(tmp, "Users"), filepath.Join(data, "Users"))
+	setHome(t, home)
 	if !Unavailable(data) {
 		t.Fatal("a root that reaches home via data/Users/u must be unavailable")
 	}
```

`internal/backend/toolloop/jail_windows_test.go`:

```go
//go:build windows

package toolloop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamsDevicesAndTrailingDotsAreDenied(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("API_KEY=x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".env::$DATA", ".env:secret", "NUL", "src/com1.txt", ".env.", ".env "} {
		if _, _, err := ResolveInRoot(root, p, DefaultDeny); !errors.Is(err, ErrDenied) {
			t.Errorf("%q: err = %v, want ErrDenied", p, err)
		}
	}
}
```

`internal/backend/ollama/connrefused_test.go`:

```go
package ollama

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// A stopped Ollama must get the "isn't running" hint on every OS; Windows
// reports WSAECONNREFUSED, which syscall.ECONNREFUSED doesn't match.
func TestMapErrNamesAStoppedServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	_, derr := net.DialTimeout("tcp", addr, 2*time.Second)
	if derr == nil {
		t.Skip("something is listening on the closed port")
	}
	got := mapErr(fmt.Errorf("post /api/chat: %w", derr), "http://"+addr, "llama3.2")
	if got == nil || !strings.Contains(got.Error(), "isn't running") {
		t.Fatalf("mapErr = %v", got)
	}
}
```

`internal/backend/httpx/final_test.go` (a refused dial must stay recognisable after scrubbing on every OS, so the test asks `platform.ConnRefused` instead of matching `syscall.ECONNREFUSED`, which Windows never reports):

```diff
--- a/internal/backend/httpx/final_test.go
+++ b/internal/backend/httpx/final_test.go
@@ -6,9 +6,10 @@ import (
 	"net/http"
 	"net/http/httptest"
 	"strings"
-	"syscall"
 	"testing"
 	"time"
+
+	"herdr-dots/internal/platform"
 )
 
 // Final review item 15: a deadline that hits while the body is read stays
@@ -47,7 +48,7 @@ func TestScrubKeepsConnectionRefused(t *testing.T) {
 	url := srv.URL
 	srv.Close()
 	_, err := (Client{Base: time.Millisecond}).Do(context.Background(), "GET", url, nil, nil)
-	if !errors.Is(err, syscall.ECONNREFUSED) {
-		t.Fatalf("err %v: want errors.Is ECONNREFUSED", err)
+	if !platform.ConnRefused(err) {
+		t.Fatalf("err %v: want a connection refusal (platform.ConnRefused)", err)
 	}
 }
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/credentials/ ./internal/backend/... 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'; GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/credentials/ ./internal/backend/toolloop/`
Expected (replayed): `--- FAIL: TestPermissiveFileIsRefusedWithoutReadingIt` (`Lookup err = credentials.toml is readable by others: run chmod 600, want ErrInsecurePerms with the fix`); every other package `ok` (`TestMapErrNamesAStoppedServer`, `TestResolveInRootNeverReachesAStreamOrDevice` and the httpx test already pass on Unix; they guard Windows). The Windows vet fails with `internal/backend/toolloop/jail.go:286:49: undefined: syscall.O_NOFOLLOW`.

- [ ] **Step 3: Implement the credentials check**

`internal/credentials/credentials.go`:

```diff
--- a/internal/credentials/credentials.go
+++ b/internal/credentials/credentials.go
@@ -17,13 +17,16 @@ import (
 	"github.com/BurntSushi/toml"
 
 	"herdr-dots/internal/fsutil"
+	"herdr-dots/internal/platform"
 )
 
 var (
 	// ErrNoCredentials means no key was found for a provider.
 	ErrNoCredentials = errors.New("no credentials")
-	// ErrInsecurePerms means credentials.toml is readable by group or others.
-	ErrInsecurePerms = errors.New("credentials.toml is readable by others: run chmod 600")
+	// ErrInsecurePerms means credentials.toml is readable by other users
+	// (Unix group/other bits; on Windows, a DACL entry for anyone but the
+	// owner, SYSTEM and Administrators). The error carries the OS's fix.
+	ErrInsecurePerms = errors.New("credentials.toml is readable by others")
 	// ErrNotOwner means credentials.toml belongs to another user.
 	ErrNotOwner = errors.New("credentials.toml is owned by another user")
 	// ErrBadKey means a key failed the shape check in Set.
@@ -111,7 +114,9 @@ func (f File) Lookup(provider string) (string, string, error) {
 }
 
 // read loads credentials.toml. A missing file is empty. The file must be a
-// regular file owned by this user with no group or other permission bits.
+// regular file owned by this user that no other user can read
+// (platform.CheckPrivateFile on the opened file: mode bits on Unix, the DACL
+// on Windows).
 func (f File) read() (map[string]entry, error) {
 	fd, st, err := openChecked(f.Path)
 	if errors.Is(err, fs.ErrNotExist) {
@@ -126,11 +131,14 @@ func (f File) read() (map[string]entry, error) {
 	if fd == nil || !st.Mode().IsRegular() {
 		return nil, fmt.Errorf("credentials.toml must be a regular file (%s)", f.Path)
 	}
-	if st.Mode().Perm()&0o077 != 0 {
-		return nil, ErrInsecurePerms
-	}
-	if !ownedByMe(st) {
-		return nil, ErrNotOwner
+	if err := platform.CheckPrivateFile(fd); err != nil {
+		switch {
+		case errors.Is(err, platform.ErrNotOwner):
+			return nil, ErrNotOwner
+		case errors.Is(err, platform.ErrNotPrivate):
+			return nil, fmt.Errorf("%w: %s", ErrInsecurePerms, platform.FixPrivateHint(f.Path))
+		}
+		return nil, err
 	}
 	data, err := io.ReadAll(io.LimitReader(fd, 1<<20))
 	if err != nil {
```

`internal/credentials/open_unix.go` (every Unix, not only darwin and linux; the uid check moves into `platform.CheckPrivateFile`):

```diff
--- a/internal/credentials/open_unix.go
+++ b/internal/credentials/open_unix.go
@@ -1,4 +1,4 @@
-//go:build darwin || linux
+//go:build unix
 
 package credentials
 
@@ -28,9 +28,3 @@ func openChecked(path string) (*os.File, fs.FileInfo, error) {
 	}
 	return fd, st, nil
 }
-
-// ownedByMe reports whether st belongs to the current user.
-func ownedByMe(st fs.FileInfo) bool {
-	sys, ok := st.Sys().(*syscall.Stat_t)
-	return !ok || int(sys.Uid) == os.Getuid()
-}
```

Rename Phase 4's fallback to the Windows file and replace its content:

```bash
git mv internal/credentials/open_other.go internal/credentials/open_windows.go
```

`internal/credentials/open_windows.go` (Plan decision 25: the documented `Lstat`→open fallback, privacy checked on the handle):

```go
//go:build windows

package credentials

import (
	"io/fs"
	"os"
)

// openChecked refuses a symlink or other reparse point at path (Lstat), then
// opens it and returns the handle's own FileInfo. Windows has no O_NOFOLLOW;
// the gap between the Lstat and the open needs write access to CONFIG_DIR,
// which is private to the user (owner-only DACL), and read checks the
// privacy of the handle it reads (platform.CheckPrivateFile).
func openChecked(path string) (*os.File, fs.FileInfo, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, st, nil
	}
	fd, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fst, err := fd.Stat()
	if err != nil {
		fd.Close()
		return nil, nil, err
	}
	return fd, fst, nil
}
```

- [ ] **Step 4: Implement the jail changes**

`internal/backend/toolloop/jail.go`:

```diff
--- a/internal/backend/toolloop/jail.go
+++ b/internal/backend/toolloop/jail.go
@@ -10,10 +10,11 @@ import (
 	"os"
 	"path/filepath"
 	"strings"
-	"syscall"
 
 	"github.com/bmatcuk/doublestar/v4"
 	"golang.org/x/text/cases"
+
+	"herdr-dots/internal/platform"
 )
 
 var (
@@ -191,7 +192,9 @@ func (r *resolver) resolve(root, p string, deny []string) (abs, rel string, err
 	if !ok {
 		return "", "", ErrOutsideRoot
 	}
-	if Denied(lexRel, deny) {
+	// Windows: an alternate data stream (".env::$DATA"), a device name (NUL)
+	// or a trailing dot would reach a file the deny-list can't see.
+	if platform.ReservedPathName(lexRel) || Denied(lexRel, deny) {
 		return "", "", ErrDenied
 	}
 	real, err := filepath.EvalSymlinks(joined)
@@ -277,13 +280,14 @@ func walkInside(rr, rel string) error {
 }
 
 // openVerified opens abs (a path ResolveInRoot returned) for reading and closes
-// the check-to-open race: O_NOFOLLOW stops a symlink swapped in for the last
-// component, and after the open the path must still be its own fully resolved
-// form (so no parent was swapped for a symlink) and name the very file opened.
-// O_NONBLOCK keeps a FIFO swapped in from hanging the run; callers must still
-// require a regular file or directory.
+// the check-to-open race: platform.OpenNoFollow stops a symlink swapped in for
+// the last component (O_NOFOLLOW on Unix; on Windows an Lstat that refuses
+// symlinks and reparse points), and after the open the path must still be its
+// own fully resolved form (so no parent was swapped for a symlink) and name
+// the very file opened. On Unix O_NONBLOCK keeps a FIFO swapped in from
+// hanging the run; callers must still require a regular file or directory.
 func openVerified(abs string) (*os.File, error) {
-	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
+	f, err := platform.OpenNoFollow(abs)
 	if err != nil {
 		return nil, err
 	}
```

`internal/backend/toolloop/tools.go`:

```diff
--- a/internal/backend/toolloop/tools.go
+++ b/internal/backend/toolloop/tools.go
@@ -20,6 +20,8 @@ import (
 	"unicode/utf8"
 
 	"github.com/bmatcuk/doublestar/v4"
+
+	"herdr-dots/internal/platform"
 )
 
 // Tool is one read-only tool offered to a model. Schema is a JSON schema
@@ -83,15 +85,17 @@ func Note(want bool, cwd string, tools bool) string {
 	return ""
 }
 
-// Unavailable reports whether root is too broad for repo tools: "/", the
-// user's home directory, or any ancestor of it. A pane started in $HOME would
-// otherwise expose dotfiles and credentials the deny-list can't enumerate.
+// Unavailable reports whether root is too broad for repo tools: a file
+// system root ("/", or on Windows a drive root such as C:\ or a share root),
+// the user's home directory, or any ancestor of it. A pane started in $HOME
+// would otherwise expose dotfiles and credentials the deny-list can't
+// enumerate.
 func Unavailable(root string) bool {
 	rr, err := filepath.EvalSymlinks(root)
 	if err != nil {
 		return false // a missing root yields tools that fail, never a panic
 	}
-	if rr == string(filepath.Separator) {
+	if filepath.Dir(rr) == rr { // a file system root on any OS
 		return true
 	}
 	rootInfo, err := os.Stat(rr)
@@ -498,11 +502,12 @@ func gitFiles(ctx context.Context, root string) ([]string, error) {
 	if fi, err := os.Lstat(gitDir); err != nil || !fi.IsDir() {
 		return nil, errors.New("not a git work tree root")
 	}
-	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=", "-c", "core.hooksPath=/dev/null",
+	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=", "-c", "core.hooksPath="+platform.DevNull,
 		"--git-dir="+gitDir, "--work-tree="+root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
 	cmd.Dir = root
 	cmd.Env = gitEnv(os.Environ())
 	cmd.WaitDelay = 500 * time.Millisecond
+	platform.HideWindow(cmd)
 	var out bytes.Buffer
 	cmd.Stdout, cmd.Stderr = &out, io.Discard
 	if err := cmd.Run(); err != nil {
@@ -520,11 +525,13 @@ func gitFiles(ctx context.Context, root string) ([]string, error) {
 // gitEnv filters env for a git child: no HERDR_*/Claude session variables
 // (the CleanEnv set) and none of the GIT_* variables that would override the
 // pinned git dir or the config. (A local copy: internal/backend imports this
-// package, and memory's filter is unexported.)
+// package, and memory's filter is unexported.) Names compare in
+// platform.EnvKey form, so case-insensitively on Windows.
 func gitEnv(env []string) []string {
 	out := make([]string, 0, len(env)+3)
 	for _, kv := range env {
-		name, _, _ := strings.Cut(kv, "=")
+		raw, _, _ := strings.Cut(kv, "=")
+		name := platform.EnvKey(raw)
 		if strings.HasPrefix(name, "HERDR_") || strings.HasPrefix(name, "GIT_CONFIG") || strings.HasPrefix(name, "CLAUDE_CODE_") {
 			continue
 		}
```

- [ ] **Step 5: Implement the connection-refused checks (Plan decision 26)**

`internal/backend/api.go`:

```diff
--- a/internal/backend/api.go
+++ b/internal/backend/api.go
@@ -5,9 +5,9 @@ import (
 	"errors"
 	"fmt"
 	"strings"
-	"syscall"
 	"time"
 
+	"herdr-dots/internal/platform"
 	"herdr-dots/internal/pricing"
 	"herdr-dots/internal/textutil"
 )
@@ -81,7 +81,7 @@ func (u unreachable) Unwrap() error        { return u.error }
 // connection refused or a timeout. Validation errors (bad key, unknown
 // model) are not.
 func Unreachable(err error) bool {
-	return errors.Is(err, ErrUnreachable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ECONNREFUSED)
+	return errors.Is(err, ErrUnreachable) || errors.Is(err, context.DeadlineExceeded) || platform.ConnRefused(err)
 }
 
 // ErrRefused marks a provider safety refusal. It is a run error: it reaches
```

`internal/backend/openaiapi/openai.go`:

```diff
--- a/internal/backend/openaiapi/openai.go
+++ b/internal/backend/openaiapi/openai.go
@@ -11,7 +11,6 @@ import (
 	"fmt"
 	"sort"
 	"strings"
-	"syscall"
 	"time"
 
 	"github.com/openai/openai-go/v3"
@@ -23,6 +22,7 @@ import (
 	"herdr-dots/internal/backend/httpx"
 	"herdr-dots/internal/backend/toolloop"
 	"herdr-dots/internal/credentials"
+	"herdr-dots/internal/platform"
 	"herdr-dots/internal/pricing"
 	"herdr-dots/internal/textutil"
 )
@@ -278,7 +278,7 @@ func mapErr(err error, key, model string) error {
 		}
 	}
 	msg := errors.New(clip(credentials.Redact(err.Error(), key), 300))
-	if errors.Is(err, syscall.ECONNREFUSED) {
+	if platform.ConnRefused(err) {
 		return backend.MarkUnreachable(msg)
 	}
 	return msg
```

`internal/backend/anthropicapi/anthropic.go`:

```diff
--- a/internal/backend/anthropicapi/anthropic.go
+++ b/internal/backend/anthropicapi/anthropic.go
@@ -8,7 +8,6 @@ import (
 	"encoding/json"
 	"errors"
 	"fmt"
-	"syscall"
 	"time"
 
 	"github.com/anthropics/anthropic-sdk-go"
@@ -19,6 +18,7 @@ import (
 	"herdr-dots/internal/backend/httpx"
 	"herdr-dots/internal/backend/toolloop"
 	"herdr-dots/internal/credentials"
+	"herdr-dots/internal/platform"
 	"herdr-dots/internal/pricing"
 	"herdr-dots/internal/textutil"
 )
@@ -267,7 +267,7 @@ func mapErr(err error, key, model string) error {
 		}
 	}
 	msg := errors.New(clip(credentials.Redact(err.Error(), key), 300))
-	if errors.Is(err, syscall.ECONNREFUSED) {
+	if platform.ConnRefused(err) {
 		return backend.MarkUnreachable(msg)
 	}
 	return msg
```

`internal/backend/ollama/ollama.go`:

```diff
--- a/internal/backend/ollama/ollama.go
+++ b/internal/backend/ollama/ollama.go
@@ -13,12 +13,12 @@ import (
 	"net/url"
 	"os"
 	"strings"
-	"syscall"
 	"time"
 
 	"herdr-dots/internal/backend"
 	"herdr-dots/internal/backend/httpx"
 	"herdr-dots/internal/backend/toolloop"
+	"herdr-dots/internal/platform"
 	"herdr-dots/internal/pricing"
 	"herdr-dots/internal/textutil"
 	"herdr-dots/internal/transcript"
@@ -419,7 +419,7 @@ func mapErr(err error, base, model string) error {
 		}
 		return fmt.Errorf("ollama error: %s", textutil.SanitizeModelText(se.Error()))
 	}
-	if errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "connection refused") {
+	if platform.ConnRefused(err) || strings.Contains(err.Error(), "connection refused") {
 		return backend.MarkUnreachable(fmt.Errorf("Ollama isn't running at %s: start it with `ollama serve`", base))
 	}
 	return fmt.Errorf("ollama at %s: %s", base, textutil.SanitizeModelText(err.Error()))
```

`internal/backend/httpx/httpx.go`:

```diff
--- a/internal/backend/httpx/httpx.go
+++ b/internal/backend/httpx/httpx.go
@@ -16,10 +16,10 @@ import (
 	"os"
 	"strconv"
 	"strings"
-	"syscall"
 	"time"
 
 	"herdr-dots/internal/credentials"
+	"herdr-dots/internal/platform"
 )
 
 // maxBody caps how much of a response is read.
@@ -243,13 +243,17 @@ func (c Client) scrub(err error) error {
 			s.causes = append(s.causes, sentinel)
 		}
 	}
+	if platform.ConnRefused(err) {
+		s.causes = append(s.causes, platform.ErrConnRefused)
+	}
 	return s
 }
 
 // keptSentinels are what a scrubbed error still matches with errors.Is: the
-// deadline and cancellation (callers report timeouts), and connection
-// refused (the editor's save check treats it as "unreachable").
-var keptSentinels = []error{context.DeadlineExceeded, context.Canceled, os.ErrDeadlineExceeded, syscall.ECONNREFUSED}
+// deadline and cancellation (callers report timeouts). Connection refused
+// is kept too, as platform.ErrConnRefused (platform.ConnRefused matches it on
+// every OS; the editor's save check treats it as "unreachable").
+var keptSentinels = []error{context.DeadlineExceeded, context.Canceled, os.ErrDeadlineExceeded}
 
 // scrubbed is a redacted error. It unwraps only to sentinels, never to the
 // original error, whose text may carry the secret.
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `gofmt -w internal && GOTOOLCHAIN=local go test -race ./internal/credentials/ ./internal/backend/...`
Expected: PASS, including Phase 4's `TestResolveInRootRefusesEscapes`, `TestDeniedIsCaseInsensitive`, `TestDeniedUsesFullUnicodeCaseFolding`, `TestResolveInRootRefusesCaseVariantsOnDisk`, `TestNoToolsForHomeOrAboveIt`, `TestRootReachingHomeThroughASuffixIsUnavailable`, `TestSymlinkRefusedEvenWhenTargetIsOwnerOnly`, the redaction tests, `backend.TestUnreachable`, and the new `TestResolveInRootNeverReachesAStreamOrDevice`, `TestPermissiveFileIsRefusedWithoutReadingIt`, `TestMapErrNamesAStoppedServer`.

- [ ] **Step 7: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/credentials/ ./internal/backend/...
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/credentials/ ./internal/backend/...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed; the last two print only `# herdr-dots/cmd/herdr-dots` until Task 6 is merged (Wave 2's list), and nothing once it is.

- [ ] **Step 8: Commit**

```bash
git add internal/credentials internal/backend/toolloop internal/backend/ollama internal/backend/api.go internal/backend/openaiapi/openai.go internal/backend/anthropicapi/anthropic.go internal/backend/httpx
git commit -m "feat(credentials): privacy checked on the opened file per OS; tool jail denies NTFS tricks and drive roots; refused connections recognised on Windows (Phase 6)"
```

---

### Task 9: CLI backends: system prompt off argv (W7), shims (W8)

**Depends on:** Task 2 (`Cmd.Cleanup`, `Cmd.Close`, `Preflight`, `platform.LookPath`, `IsBatchFile`, `MaxInlinePrompt`, `ErrCmdTooLong`, `ErrUnsafeBatchArg`), Task 3 (`platform.SecureDir`, `SecureFile`, `platformtest.AssertPrivateFile`). Wave 2.

**Files:**
- Modify: `internal/backend/claude.go` (Phase 4 changed `parseClaude` only), `internal/backend/codex.go` (Phase 5 appends `codexMCPOff(env)` in `BuildCmd`; Task 10 ports `codex_mcp.go`, not this file)
- Create: `internal/backend/runfiles.go`
- Test: `internal/backend/runfiles_test.go`, `internal/backend/runfiles_windows_test.go`

**Interfaces:**
- Consumes: `platform.LookPath(name string) (string, error)`, `platform.IsBatchFile(path string) bool`, `platform.MaxInlinePrompt`, `platform.SecureDir`, `platform.SecureFile`, `platform.ErrCmdTooLong`, `platform.ErrUnsafeBatchArg`; `backend.Cmd.Cleanup`, `Cmd.Close`, `Preflight`; v1's `SystemPrompt`, `UserPrompt`, `fakeRunner`, `req`, `argAfter` (test helpers in `claude_test.go`).
- Produces:
  - `var resolveBin = platform.LookPath` (unexported; tests replace it; a test-file `init` makes it report "not installed" for the whole package, so a developer's own `claude.cmd` never changes argv expectations)
  - `func writeRunFiles(files map[string]string) (map[string]string, func(), error)` (private temp dir `herdr-dots-run-*`, files `0600`/owner-only)
  - `ClaudeCLI.BuildCmd(r)`: short prompt and native binary → v1 argv exactly; prompt over 4,000 characters → `--append-system-prompt-file <file>`; `.cmd`/`.bat` claude → prompt **and** settings in files; `Cmd.Cleanup` removes them
  - `ClaudeCLI.Run` and `CodexCLI.Run` close the command after the run and return `platform.ErrCmdTooLong` / `ErrUnsafeBatchArg` verbatim (`claude: ...`, `codex: ...`); `Check` uses `platform.LookPath`

- [ ] **Step 1: Write the failing tests**

`internal/backend/runfiles_test.go` (Review Focus 2):

```go
package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform"
	"herdr-dots/internal/platform/platformtest"
)

// Every test in this package sees "claude not installed" unless it says
// otherwise, so a developer's own claude.cmd never changes argv expectations.
func init() { resolveBin = func(string) (string, error) { return "", exec.ErrNotFound } }

// withBin makes resolveBin report path for the duration of the test.
func withBin(t *testing.T, path string) {
	t.Helper()
	old := resolveBin
	resolveBin = func(string) (string, error) { return path, nil }
	t.Cleanup(func() { resolveBin = old })
}

func longReq() Request {
	r := req()
	r.Purpose = strings.Repeat("Check every auth path for token leaks. ", 130) // > 4,000 characters
	return r
}

func TestShortPromptStaysInArgvExactlyAsV1(t *testing.T) {
	r := req()
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	defer cmd.Close()
	want := []string{"-p", "--output-format", "json", "--no-session-persistence", "--strict-mcp-config",
		"--settings", `{"disableAllHooks":true}`, "--append-system-prompt", SystemPrompt(r),
		"--model", "sonnet", "--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob"}
	if !slices.Equal(cmd.Args, want) || cmd.Cleanup != nil {
		t.Fatalf("args %q cleanup %v", cmd.Args, cmd.Cleanup != nil)
	}
}

func TestLongPromptMovesToAPrivateFile(t *testing.T) {
	r := longReq()
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
	path, ok := argAfter(cmd.Args, "--append-system-prompt-file")
	if !ok || slices.Contains(cmd.Args, "--append-system-prompt") || slices.Contains(cmd.Args, SystemPrompt(r)) {
		t.Fatalf("args %q", cmd.Args)
	}
	if v, _ := argAfter(cmd.Args, "--settings"); v != `{"disableAllHooks":true}` {
		t.Fatalf("settings %q: no shim, so inline", v)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != SystemPrompt(r) {
		t.Fatalf("prompt file: %v (%d bytes)", err, len(got))
	}
	platformtest.AssertPrivateFile(t, path)
	cmd.Close()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Close left %s: %v", path, err)
	}
}

func TestShimGetsPromptAndSettingsAsFiles(t *testing.T) {
	withBin(t, `C:\Users\x\AppData\Roaming\npm\claude.cmd`)
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(req())
	defer cmd.Close()
	settings, _ := argAfter(cmd.Args, "--settings")
	prompt, ok := argAfter(cmd.Args, "--append-system-prompt-file")
	if !ok || settings == `{"disableAllHooks":true}` {
		t.Fatalf("args %q", cmd.Args)
	}
	if b, err := os.ReadFile(settings); err != nil || string(b) != `{"disableAllHooks":true}` {
		t.Fatalf("settings file %q %v", b, err)
	}
	for _, a := range cmd.Args {
		if strings.ContainsAny(a, "\"%!\n") {
			t.Fatalf("argument %q can't pass through cmd.exe", a)
		}
	}
	if _, err := os.Stat(prompt); err != nil {
		t.Fatal(err)
	}
}

// fileRunner checks the prompt file exists while the command "runs".
type fileRunner struct {
	sawFile bool
	stdout  string
	err     error
	wait    bool
}

func (f *fileRunner) Run(ctx context.Context, c Cmd) ([]byte, []byte, error) {
	if p, ok := argAfter(c.Args, "--append-system-prompt-file"); ok {
		_, err := os.Stat(p)
		f.sawFile = err == nil
	}
	if f.wait {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	return []byte(f.stdout), nil, f.err
}

func TestRunDeletesThePromptFileOnSuccessErrorAndTimeout(t *testing.T) {
	ok := `{"is_error":false,"result":"{\"relevant\":false}","total_cost_usd":0,"usage":{"input_tokens":1,"output_tokens":1}}`
	for name, fr := range map[string]*fileRunner{
		"success": {stdout: ok},
		"error":   {err: errors.New("exit status 1")},
		"timeout": {wait: true},
	} {
		before, _ := os.ReadDir(os.TempDir())
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, _ = ClaudeCLI{Runner: fr, Environ: func() []string { return nil }}.Run(ctx, longReq())
		cancel()
		after, _ := os.ReadDir(os.TempDir())
		if !fr.sawFile {
			t.Errorf("%s: the prompt file must exist during the run", name)
		}
		if leftover := runDirs(after) - runDirs(before); leftover > 0 {
			t.Errorf("%s: %d herdr-dots-run-* dirs left behind", name, leftover)
		}
	}
}

func runDirs(es []os.DirEntry) int {
	n := 0
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "herdr-dots-run-") {
			n++
		}
	}
	return n
}

type errRunner struct{ err error }

func (e errRunner) Run(context.Context, Cmd) ([]byte, []byte, error) { return nil, nil, e.err }

func TestRunReportsShimPreflightErrorsVerbatim(t *testing.T) {
	tooLong := fmt.Errorf("%w for claude.cmd (9214 > 8191): install the native claude.exe or shorten the dot's backend args", platform.ErrCmdTooLong)
	_, err := ClaudeCLI{Runner: errRunner{tooLong}, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if !errors.Is(err, platform.ErrCmdTooLong) || !strings.Contains(err.Error(), "claude.exe") {
		t.Fatalf("claude err = %v", err)
	}
	_, err = CodexCLI{Runner: errRunner{platform.ErrUnsafeBatchArg}, Environ: func() []string { return nil }}.Run(context.Background(), req())
	if !errors.Is(err, platform.ErrUnsafeBatchArg) {
		t.Fatalf("codex err = %v", err)
	}
}
```

`internal/backend/runfiles_windows_test.go` (a fake `claude.cmd` records its command line; Review Focus 2):

```go
//go:build windows

package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/platform"
)

// A fake claude.cmd records the command line it received. Purpose text full
// of cmd.exe metacharacters must reach the prompt file, never argv.
func TestClaudeThroughACmdShimKeepsThePromptOffTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	shim := "@echo off\r\necho %* > \"%~dp0args.txt\"\r\necho {\"is_error\":false,\"result\":\"\"}\r\n"
	if err := os.WriteFile(filepath.Join(dir, "claude.cmd"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := resolveBin
	resolveBin = platform.LookPath
	defer func() { resolveBin = old }()
	r := req()
	r.Purpose = `Flag "quotes" & pipes | 100% %PATH% carets ^ and <angle> brackets! & echo INJECTED`
	cmd := ClaudeCLI{Environ: os.Environ}.BuildCmd(r)
	defer cmd.Close()
	if _, _, err := (ExecRunner{}).Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	if !strings.Contains(got, "--append-system-prompt-file") || strings.Contains(got, "INJECTED") || strings.Contains(got, "disableAllHooks") {
		t.Fatalf("shim command line: %s", got)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/backend/`
Expected: FAIL to compile (`undefined: resolveBin`).

- [ ] **Step 3: Implement**

`internal/backend/runfiles.go`:

```go
package backend

import (
	"os"
	"path/filepath"

	"herdr-dots/internal/platform"
)

// resolveBin finds the program a backend will run; tests replace it.
var resolveBin = platform.LookPath

// writeRunFiles writes files (name -> content) into a new private temp
// directory (0700 / owner-only DACL, files 0600) and returns their paths and
// a cleanup that removes the directory.
func writeRunFiles(files map[string]string) (map[string]string, func(), error) {
	dir, err := os.MkdirTemp("", "herdr-dots-run-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := platform.SecureDir(dir); err != nil {
		cleanup()
		return nil, nil, err
	}
	paths := make(map[string]string, len(files))
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			cleanup()
			return nil, nil, err
		}
		if err := platform.SecureFile(p); err != nil {
			cleanup()
			return nil, nil, err
		}
		paths[name] = p
	}
	return paths, cleanup, nil
}
```

`internal/backend/claude.go` (post-Phase-4 form: `parseClaude` below `Run` carries the cache-token changes; leave it alone):

```diff
--- a/internal/backend/claude.go
+++ b/internal/backend/claude.go
@@ -6,9 +6,10 @@ import (
 	"encoding/json"
 	"fmt"
 	"os"
-	"os/exec"
+	"unicode/utf8"
 
 	"herdr-dots/internal/dots"
+	"herdr-dots/internal/platform"
 )
 
 type ClaudeCLI struct {
@@ -34,21 +35,47 @@ func (c ClaudeCLI) environ() []string {
 }
 
 func (c ClaudeCLI) Check(context.Context) error {
-	if _, err := exec.LookPath(c.bin()); err != nil {
+	if _, err := platform.LookPath(c.bin()); err != nil {
 		return fmt.Errorf("claude CLI not found on PATH: install Claude Code (https://claude.com/claude-code)")
 	}
 	return nil
 }
 
+// hooklessSettings disables every hook for a dot run.
+const hooklessSettings = `{"disableAllHooks":true}`
+
 // BuildCmd: print mode, JSON output, no saved session, no MCP servers, hooks
 // disabled (so Herdr's integration hooks never fire for a dot), read-only tools.
+//
+// The system prompt rides in argv up to platform.MaxInlinePrompt characters.
+// A longer one, or any prompt when claude resolves to a .cmd/.bat shim
+// (cmd.exe can't carry its quotes, % signs and newlines), goes in a private
+// temp file passed with --append-system-prompt-file; for a shim the settings
+// JSON goes in a file too. Cmd.Cleanup deletes the files: call Cmd.Close
+// after the run. If the files can't be written, the v1 argv is used.
 func (c ClaudeCLI) BuildCmd(r Request) Cmd {
+	sys := SystemPrompt(r)
+	settingsArg, promptFlag, promptArg := hooklessSettings, "--append-system-prompt", sys
+	shim := false
+	if p, err := resolveBin(c.bin()); err == nil {
+		shim = platform.IsBatchFile(p)
+	}
+	var cleanup func()
+	if shim || utf8.RuneCountInString(sys) > platform.MaxInlinePrompt {
+		if files, done, err := writeRunFiles(map[string]string{"system.txt": sys, "settings.json": hooklessSettings}); err == nil {
+			cleanup = done
+			promptFlag, promptArg = "--append-system-prompt-file", files["system.txt"]
+			if shim {
+				settingsArg = files["settings.json"]
+			}
+		}
+	}
 	args := []string{
 		"-p", "--output-format", "json",
 		"--no-session-persistence",
 		"--strict-mcp-config",
-		"--settings", `{"disableAllHooks":true}`,
-		"--append-system-prompt", SystemPrompt(r),
+		"--settings", settingsArg,
+		promptFlag, promptArg,
 	}
 	if r.Model != "" {
 		args = append(args, "--model", r.Model)
@@ -61,14 +88,19 @@ func (c ClaudeCLI) BuildCmd(r Request) Cmd {
 	// Denied args (dots.Validate rejects them too) are dropped so they can never
 	// override the flags above.
 	args = append(args, dots.SafeArgs(c.Kind(), r.Args)...)
-	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: UserPrompt(r)}
+	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: UserPrompt(r), Cleanup: cleanup}
 }
 
 func (c ClaudeCLI) Run(ctx context.Context, r Request) (Result, error) {
 	if err := checkArgs(c.Kind(), r.Args); err != nil {
 		return Result{}, err
 	}
-	out, stderr, err := c.Runner.Run(ctx, c.BuildCmd(r))
+	cmd := c.BuildCmd(r)
+	defer cmd.Close()
+	out, stderr, err := c.Runner.Run(ctx, cmd)
+	if Preflight(err) {
+		return Result{}, fmt.Errorf("claude: %w", err)
+	}
 	if ctx.Err() != nil {
 		return Result{}, fmt.Errorf("claude timed out: %w", ctx.Err())
 	}
```

`internal/backend/codex.go` (post-Phase-5 form: keep the `-c mcp_servers.herdr-dots.enabled=false` line Phase 5 added to `BuildCmd`):

```diff
--- a/internal/backend/codex.go
+++ b/internal/backend/codex.go
@@ -8,9 +8,9 @@ import (
 	"errors"
 	"fmt"
 	"os"
-	"os/exec"
 
 	"herdr-dots/internal/dots"
+	"herdr-dots/internal/platform"
 )
 
 // CodexCLI runs `codex exec --json` in a read-only sandbox. Codex has no
@@ -39,7 +39,7 @@ func (c CodexCLI) environ() []string {
 }
 
 func (c CodexCLI) Check(context.Context) error {
-	if _, err := exec.LookPath(c.bin()); err != nil {
+	if _, err := platform.LookPath(c.bin()); err != nil {
 		return fmt.Errorf("codex CLI not found on PATH: install it with `npm i -g @openai/codex`")
 	}
 	return nil
@@ -65,6 +65,9 @@ func (c CodexCLI) Run(ctx context.Context, r Request) (Result, error) {
 		return Result{}, err
 	}
 	out, stderr, err := c.Runner.Run(ctx, c.BuildCmd(r))
+	if Preflight(err) {
+		return Result{}, fmt.Errorf("codex: %w", err)
+	}
 	if ctx.Err() != nil {
 		return Result{}, fmt.Errorf("codex timed out: %w", ctx.Err())
 	}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `gofmt -w internal/backend && GOTOOLCHAIN=local go test -race ./internal/backend/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS, including `TestShortPromptStaysInArgvExactlyAsV1`, `TestLongPromptMovesToAPrivateFile`, `TestShimGetsPromptAndSettingsAsFiles`, `TestRunDeletesThePromptFileOnSuccessErrorAndTimeout`, `TestRunReportsShimPreflightErrorsVerbatim`, and v1's `TestClaudeBuildCmdIsReadOnlyAndHookless`, `TestClaudeBuildCmdNeverEmitsDeniedArgs`, `TestCodexBuildCmd`.

- [ ] **Step 5: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/backend/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./internal/backend/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed; the last two print only Wave 2's list (`# herdr-dots/internal/backend/toolloop`, `# herdr-dots/cmd/herdr-dots`, each until its task merges).

- [ ] **Step 6: Commit**

```bash
git add internal/backend/claude.go internal/backend/codex.go internal/backend/runfiles.go internal/backend/runfiles_test.go internal/backend/runfiles_windows_test.go
git commit -m "feat(backend): long system prompts via a private file; cmd.exe-safe claude/codex shims (Phase 6)"
```

---

### Task 10: Phase 5 ports, the memory editor and the portability guard

**Depends on:** Tasks 2–9 (Wave 3): `platform.HomeEnv`, `EnvKey`, `Command`, `LookPath`, `CodexConfig`, `ClaudeUserConfig` (Task 2), `platform.SecureDir`, `platformtest.Endpoint`, `platformtest.AssertPrivateFile` (Task 3), `ipc.Listen` and `paths.Paths.EngineEndpoint` (Task 6). Reconciled (Plan decision 24): every Phase 5 port below is a concrete diff against the merged Phase 5 code; the guard confirms there is nothing left.

**Files:**
- Test: `internal/platform/portability_test.go`, `internal/platform/editor_unix_test.go`, `internal/platform/editor_windows_test.go`
- Create: `internal/platform/editor_unix.go`, `internal/platform/editor_windows.go`
- Modify: `internal/mcpinstall/install.go`, `internal/mcpinstall/install_test.go`, `internal/mcpinstall/final_test.go`
- Create: `internal/mcpinstall/final_unix_test.go`
- Modify: `internal/backend/codex_mcp.go`, `internal/backend/codex_mcp_test.go`
- Modify: `internal/mcpserver/engine_test.go`, `internal/engine/ask_test.go`
- Modify: `cmd/herdr-dots/setupmcp.go`, `cmd/herdr-dots/setupmcp_test.go`, `cmd/herdr-dots/memory.go`, `cmd/herdr-dots/memory_test.go`
- Create: `cmd/herdr-dots/memory_unix_test.go`
- Modify: `docs/superpowers/notes/2026-10-08-phase-6-verification.md` (Phase 5 section)

**Interfaces:**
- Consumes: `platform.HomeEnv`, `platform.EnvKey(name string) string`, `platform.Command(name string, args []string) (*exec.Cmd, error)`, `platform.LookPath(name string) (string, error)`, `platform.CodexConfig(home string) string`, `platform.ClaudeUserConfig(home string) string`, `platform.SecureDir(path string) error`, `platformtest.Endpoint(t testing.TB) string`, `platformtest.AssertPrivateFile`, `ipc.Listen(endpoint string) (net.Listener, error)`.
- Produces: `func platform.EditorCommand(editor, file string) *exec.Cmd` (Unix: `sh -c '<editor> "$1"' sh <file>`, byte-identical to Phase 2; Windows: `%ComSpec% /d /s /c "<editor> "<file>""`; Plan decision 30). `TestProductCodeHasNoUnixOnlyCalls` (package `platform`), which fails on any of these in untagged, non-test product code outside `internal/platform`: `"/dev/null"`, `syscall.Kill(`, `Setpgid`, `Setsid`, `syscall.Flock`, `syscall.O_NOFOLLOW`, `syscall.Stat_t`, `syscall.ECONNREFUSED`, `"unix"`, `os.Getuid()`, `exec.LookPath(`, `runtime.GOOS`, `.EngineSock()`, `"/tmp`, `".claude"`, `".claude.json"`, `".codex"`, `Command("claude"`, `Command("codex"`, `(ctx, "claude"`, `(ctx, "codex"`, `exec.Command("sh"`, `"HOME"`. Phases 7–9 inherit it. Behavior: `setup-mcp` runs `claude mcp …` / `codex mcp …` through `platform.Command` (a `claude.cmd` shim is quoted for cmd.exe; a `%`, `"` or `!` in a path is refused with `platform.ErrUnsafeBatchArg` before anything runs) and finds them with `platform.LookPath`; `mcpinstall.Env.ClaudeConfigPath`/`CodexConfigPath` use the platform helpers; a codex dot run finds `%USERPROFILE%\.codex\config.toml` on Windows; `memory edit` works without `sh`.

- [ ] **Step 1: Write the guard**

`internal/platform/portability_test.go`:

```go
package platform

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unixOnly maps a source pattern that only works on Unix (or hides an OS
// branch) to what portable code uses instead.
var unixOnly = map[string]string{
	`"/dev/null"`:          "platform.DevNull",
	"syscall.Kill(":        "platform.StartTree / Tree.Kill",
	"Setpgid":              "platform.StartTree",
	"Setsid":               "platform.StartDetached",
	"syscall.Flock":        "internal/lockfile",
	"syscall.O_NOFOLLOW":   "platform.OpenNoFollow",
	"syscall.Stat_t":       "platform.CheckPrivate",
	"syscall.ECONNREFUSED": "platform.ConnRefused",
	`"unix"`:               "ipc.Listen / ipc.Dial (build-tagged)",
	"os.Getuid()":          "platform.CheckPrivate",
	"exec.LookPath(":       "platform.LookPath (prefers .exe over .cmd shims)",
	"runtime.GOOS":         "a _unix.go / _windows.go file or internal/platform",
	".EngineSock()":        "Paths.EngineEndpoint",
	`"/tmp`:                "os.TempDir()",
	`".claude"`:            "platform.ClaudeProjectsDir / ClaudeUserConfig",
	`".claude.json"`:       "platform.ClaudeUserConfig",
	`".codex"`:             "platform.CodexSessionsDir / CodexConfig",
	`Command("claude"`:     "platform.Command (safe for a claude.cmd shim)",
	`Command("codex"`:      "platform.Command (safe for a codex.cmd shim)",
	`(ctx, "claude"`:       "platform.Command (safe for a claude.cmd shim)",
	`(ctx, "codex"`:        "platform.Command (safe for a codex.cmd shim)",
	`exec.Command("sh"`:    "platform.EditorCommand (no sh on Windows)",
	`"HOME"`:               "platform.HomeEnv or os.UserHomeDir (Windows reads USERPROFILE)",
}

// moduleRoot walks up from the test's directory to go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// osSpecific reports whether a file only builds on some OSes: a _unix.go or
// _windows.go name, or a //go:build line.
func osSpecific(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_unix.go") || strings.HasSuffix(base, "_windows.go") {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "//go:build") {
			return true
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
	}
	return false
}

// TestProductCodeHasNoUnixOnlyCalls keeps every later phase portable: shared
// (untagged) product code may not use Unix-only calls or branch on the OS.
// internal/platform, OS-specific files and tests are exempt.
func TestProductCodeHasNoUnixOnlyCalls(t *testing.T) {
	root := moduleRoot(t)
	var bad []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && rel != "." || rel == "internal/platform" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || osSpecific(p) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			code, _, _ := strings.Cut(line, "//")
			for pat, use := range unixOnly {
				if strings.Contains(code, pat) {
					bad = append(bad, rel+":"+itoa(i+1)+": "+pat+" (use "+use+")")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Error(b)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
```

- [ ] **Step 2: Run it to see what Phases 2 and 5 need**

Run: `GOTOOLCHAIN=local go test ./internal/platform/ -run TestProductCodeHasNoUnixOnlyCalls -v 2>&1 | grep -E 'portability_test|^(--- |ok|FAIL)'`
Expected (replayed; the order of findings within a line may vary): FAIL with exactly these six findings, all fixed in Step 4:

```
cmd/herdr-dots/memory.go:649: exec.Command("sh" (use platform.EditorCommand (no sh on Windows))
internal/backend/codex_mcp.go:51: "HOME" (use platform.HomeEnv or os.UserHomeDir (Windows reads USERPROFILE))
internal/backend/codex_mcp.go:59: ".codex" (use platform.CodexSessionsDir / CodexConfig)
internal/mcpinstall/install.go:67: ".claude.json" (use platform.ClaudeUserConfig)
internal/mcpinstall/install.go:69: ".claude.json" (use platform.ClaudeUserConfig)
internal/mcpinstall/install.go:77: ".codex" (use platform.CodexSessionsDir / CodexConfig)
```

Line numbers are those at `a47427d`. Any other finding comes from a commit after `a47427d`: fix it with the replacement the guard names and list it in the notes (Step 8).

- [ ] **Step 3: Write the editor tests**

`internal/platform/editor_unix_test.go`:

```go
//go:build unix

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditorCommandPassesTheFileAsAnArgument(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "my memory; echo INJECTED.toml")
	out := filepath.Join(dir, "out.txt")
	cmd := EditorCommand("printf '%s' >"+out, file)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != file {
		t.Fatalf("editor saw %q, want %q", got, file)
	}
}
```

`internal/platform/editor_windows_test.go` (runs only on Windows; no non-ASCII in the path, because cmd.exe's `echo` writes the OEM code page):

```go
//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An editor given as a command line with a flag gets the file as one quoted
// argument, even with spaces in both paths.
func TestEditorCommandRunsTheEditorLineOnTheFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my dir")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	editor := filepath.Join(dir, "my editor.cmd")
	if err := os.WriteFile(editor, []byte("@echo off\r\necho [%1] [%2] > \"%~dp0seen.txt\"\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "memory file.toml")
	if err := EditorCommand(`"`+editor+`" --wait`, file).Run(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "seen.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.TrimSpace(string(got)); s != `[--wait] ["`+file+`"]` {
		t.Fatalf("editor saw %s", s)
	}
}
```

Run: `GOTOOLCHAIN=local go test ./internal/platform/`
Expected: FAIL to compile (`undefined: EditorCommand`).

- [ ] **Step 4: Implement**

`internal/platform/editor_unix.go`:

```go
//go:build unix

package platform

import "os/exec"

// EditorCommand runs the user's editor command line (from $EDITOR, which may
// carry flags such as "code --wait") on file through the shell: sh -c
// '<editor> "$1"' sh <file>. file is passed as an argument, never parsed.
func EditorCommand(editor, file string) *exec.Cmd {
	return exec.Command("sh", "-c", editor+` "$1"`, "sh", file)
}
```

`internal/platform/editor_windows.go`:

```go
//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// EditorCommand runs the user's editor command line (from %EDITOR%, which
// may carry flags such as "code --wait") on file through cmd.exe:
// <comspec> /d /s /c "<editor> "<file>"". file is a path herdr-dots made
// (a private temp file), so it holds no quote; the editor line is the user's
// own setting and runs as they wrote it.
func EditorCommand(editor, file string) *exec.Cmd {
	shell := comspec()
	cmd := exec.Command(shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: shell + ` /d /s /c "` + editor + ` "` + file + `""`}
	return cmd
}
```

`cmd/herdr-dots/memory.go` (Phase 2's `memory edit`: the editor through `platform.EditorCommand`, the temp directory made private on every OS):

```diff
--- a/cmd/herdr-dots/memory.go
+++ b/cmd/herdr-dots/memory.go
@@ -9,7 +9,6 @@ import (
 	"fmt"
 	"io"
 	"os"
-	"os/exec"
 	"path/filepath"
 	"strings"
 	"text/tabwriter"
@@ -18,6 +17,7 @@ import (
 	"herdr-dots/internal/config"
 	"herdr-dots/internal/memory"
 	"herdr-dots/internal/paths"
+	"herdr-dots/internal/platform"
 	"herdr-dots/internal/textutil"
 )
 
@@ -635,18 +635,22 @@ func (c *memCLI) edit(args []string) error {
 	if err != nil {
 		return err
 	}
-	// A private 0700 directory, removed with everything the editor leaves
-	// in it (swap and backup files hold the same memory text).
+	// A private directory (0700; an owner-only DACL on Windows), removed
+	// with everything the editor leaves in it (swap and backup files hold the
+	// same memory text).
 	dir, err := os.MkdirTemp("", "herdr-dots-memory-*") // 0700
 	if err != nil {
 		return err
 	}
 	defer os.RemoveAll(dir)
+	if err := platform.SecureDir(dir); err != nil {
+		return err
+	}
 	file := filepath.Join(dir, "memory.toml")
 	if err := os.WriteFile(file, data, 0o600); err != nil {
 		return err
 	}
-	cmd := exec.Command("sh", "-c", c.editor+` "$1"`, "sh", file)
+	cmd := platform.EditorCommand(c.editor, file) // sh -c on Unix, cmd.exe on Windows
 	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.in, c.out, c.errOut
 	if err := cmd.Run(); err != nil {
 		return fmt.Errorf("edit: the editor failed: %w (nothing changed)", err)
```

`internal/mcpinstall/install.go` (Phase 5; `CLAUDE_CONFIG_DIR` and `CODEX_HOME` stay honored, Plan decision 29):

```diff
--- a/internal/mcpinstall/install.go
+++ b/internal/mcpinstall/install.go
@@ -18,6 +18,7 @@ import (
 	"unicode"
 
 	"herdr-dots/internal/fsutil"
+	"herdr-dots/internal/platform"
 )
 
 type Client string
@@ -61,20 +62,22 @@ func (e Env) get(k string) string {
 	return e.Getenv(k)
 }
 
-// ClaudeConfigPath is the file `claude mcp add --scope user` edits.
+// ClaudeConfigPath is the file `claude mcp add --scope user` edits
+// (platform.ClaudeUserConfig under CLAUDE_CONFIG_DIR or the home directory).
 func (e Env) ClaudeConfigPath() string {
 	if d := e.get("CLAUDE_CONFIG_DIR"); d != "" {
-		return filepath.Join(d, ".claude.json")
+		return platform.ClaudeUserConfig(d)
 	}
-	return filepath.Join(e.Home, ".claude.json")
+	return platform.ClaudeUserConfig(e.Home)
 }
 
-// CodexConfigPath is Codex's user config.
+// CodexConfigPath is Codex's user config: CODEX_HOME/config.toml, else
+// platform.CodexConfig (~/.codex/config.toml, %USERPROFILE%\.codex on Windows).
 func (e Env) CodexConfigPath() string {
 	if d := e.get("CODEX_HOME"); d != "" {
 		return filepath.Join(d, "config.toml")
 	}
-	return filepath.Join(e.Home, ".codex", "config.toml")
+	return platform.CodexConfig(e.Home)
 }
 
 // Command is one CLI call; a MayFail command's failure is ignored (and its
```

`internal/backend/codex_mcp.go` (Phase 5's config lookup for a codex dot run):

```diff
--- a/internal/backend/codex_mcp.go
+++ b/internal/backend/codex_mcp.go
@@ -6,6 +6,8 @@ import (
 	"strings"
 
 	"github.com/BurntSushi/toml"
+
+	"herdr-dots/internal/platform"
 )
 
 // CodexMCPServer is the Dots MCP server's name in Codex's config.toml
@@ -40,15 +42,17 @@ func codexMCPOff(env []string) []string {
 }
 
 // codexConfigPath is the config.toml a codex child with env reads:
-// $CODEX_HOME/config.toml, else $HOME/.codex/config.toml ("" when neither is set).
+// $CODEX_HOME/config.toml, else platform.CodexConfig of the home variable
+// (HOME; USERPROFILE on Windows), "" when neither is set. Names compare in
+// platform.EnvKey form, so case-insensitively on Windows.
 func codexConfigPath(env []string) string {
 	var home, codexHome string
 	for _, kv := range env {
 		k, v, _ := strings.Cut(kv, "=")
-		switch k {
-		case "CODEX_HOME":
+		switch platform.EnvKey(k) {
+		case platform.EnvKey("CODEX_HOME"):
 			codexHome = v
-		case "HOME":
+		case platform.EnvKey(platform.HomeEnv):
 			home = v
 		}
 	}
@@ -56,7 +60,7 @@ func codexConfigPath(env []string) string {
 	case codexHome != "":
 		return filepath.Join(codexHome, "config.toml")
 	case home != "":
-		return filepath.Join(home, ".codex", "config.toml")
+		return platform.CodexConfig(home)
 	}
 	return ""
 }
```

`cmd/herdr-dots/setupmcp.go` (the installed command is already `os.Executable()` resolved through `EvalSymlinks`, so it ends in `.exe` on Windows; the children go through `platform.Command`):

```diff
--- a/cmd/herdr-dots/setupmcp.go
+++ b/cmd/herdr-dots/setupmcp.go
@@ -7,13 +7,13 @@ import (
 	"fmt"
 	"io"
 	"os"
-	"os/exec"
 	"path/filepath"
 	"strings"
 	"time"
 
 	"herdr-dots/internal/mcpinstall"
 	"herdr-dots/internal/paths"
+	"herdr-dots/internal/platform"
 )
 
 // setupIO is everything setup-mcp touches, so tests can fake it.
@@ -39,11 +39,18 @@ func runSetupMCP(args []string) error {
 	home, _ := os.UserHomeDir()
 	return setupMCP(args, setupIO{
 		in: os.Stdin, out: os.Stdout,
+		// platform.Command runs an npm claude.cmd/codex.cmd shim through
+		// cmd.exe with every argument quoted, and refuses a path cmd.exe
+		// can't carry (a %, " or !) before anything runs.
 		run: func(a []string) (string, error) {
-			out, err := exec.Command(a[0], a[1:]...).CombinedOutput()
+			cmd, err := platform.Command(a[0], a[1:])
+			if err != nil {
+				return "", err
+			}
+			out, err := cmd.CombinedOutput()
 			return string(out), err
 		},
-		lookPath: exec.LookPath,
+		lookPath: platform.LookPath,
 		env:      mcpinstall.Env{Getenv: os.Getenv, Home: home},
 		now:      time.Now,
 		exe:      exe,
```

- [ ] **Step 5: Port the Phase 2 and Phase 5 tests**

`cmd/herdr-dots/memory_test.go` and the new `cmd/herdr-dots/memory_unix_test.go`: the three editor tests drive a `sh` script (`sed`, `stat`) as `$EDITOR`, so they move, unchanged, into a Unix-only file (Plan decision 30; the Windows editor is tested in `internal/platform`):

```diff
--- a/cmd/herdr-dots/memory_test.go
+++ b/cmd/herdr-dots/memory_test.go
@@ -141,30 +141,6 @@ func TestMemoryPinApproveAndProjects(t *testing.T) {
 	}
 }
 
-func TestMemoryEditAppliesExactlyOneUpdate(t *testing.T) {
-	c, out, _ := newMemCLI(t)
-	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
-	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Sessions expire after one hour")
-	script := filepath.Join(t.TempDir(), "editor.sh")
-	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/Use zod for validation/Use valibot for validation/' \"$1\"\n"), 0o700); err != nil {
-		t.Fatal(err)
-	}
-	c.editor = script
-	out.Reset()
-	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
-	if !strings.Contains(out.String(), "applied: 0 added, 1 updated, 0 forgotten") {
-		t.Fatalf("%s", out)
-	}
-	e, _ := c.store.Get(testPID, memory.Ref{Scope: memory.ScopeDot, Dot: "security", ID: "m1"})
-	if e.Text != "Use valibot for validation" {
-		t.Fatalf("%+v", e)
-	}
-	c.editor = ""
-	if err := c.run([]string{"edit"}); err == nil || !strings.Contains(err.Error(), "EDITOR") {
-		t.Fatalf("want an $EDITOR hint, got %v", err)
-	}
-}
-
 func TestMemoryExportImportIsIdempotent(t *testing.T) {
 	c, out, _ := newMemCLI(t)
 	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
@@ -265,23 +241,6 @@ func TestMemoryImportSecretInteractiveConfirm(t *testing.T) {
 	}
 }
 
-func TestMemoryEditMarksUserSource(t *testing.T) {
-	c, _, _ := newMemCLI(t)
-	if _, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "Use zod for validation", Source: memory.SourceModel, Status: memory.StatusActive}); err != nil {
-		t.Fatal(err)
-	}
-	script := filepath.Join(t.TempDir(), "editor.sh")
-	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/zod/valibot/' \"$1\"\n"), 0o700); err != nil {
-		t.Fatal(err)
-	}
-	c.editor = script
-	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
-	e, _ := c.store.Get(testPID, memory.Ref{Scope: memory.ScopeDot, Dot: "security", ID: "m1"})
-	if e.Source != memory.SourceUser {
-		t.Fatalf("source %q", e.Source)
-	}
-}
-
 // Final review minor 4: a secret in paths is gated like a secret in text.
 func TestMemorySecretInPathsIsGated(t *testing.T) {
 	c, _, _ := newMemCLI(t)
@@ -297,42 +256,6 @@ func TestMemorySecretInPathsIsGated(t *testing.T) {
 	}
 }
 
-// Final review minor 8: edit works in a private 0700 directory that is
-// removed afterwards with whatever the editor left in it (swap, backup).
-func TestMemoryEditRemovesItsTempDirectory(t *testing.T) {
-	c, _, _ := newMemCLI(t)
-	t.Setenv("TMPDIR", t.TempDir())
-	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
-	record := filepath.Join(t.TempDir(), "record")
-	script := filepath.Join(t.TempDir(), "editor.sh")
-	body := "#!/bin/sh\nd=$(dirname \"$1\")\necho \"$d\" > " + record + "\n" +
-		"(stat -f %Lp \"$d\" 2>/dev/null || stat -c %a \"$d\") >> " + record + "\n" +
-		"touch \"$d/.swp-leftover\"\nsed -i.bak 's/zod/valibot/' \"$1\"\n"
-	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
-		t.Fatal(err)
-	}
-	c.editor = script
-	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
-	data, err := os.ReadFile(record)
-	if err != nil {
-		t.Fatal(err)
-	}
-	lines := strings.Fields(string(data))
-	if len(lines) != 2 {
-		t.Fatalf("record %q", data)
-	}
-	dir, mode := lines[0], lines[1]
-	if filepath.Clean(dir) == filepath.Clean(os.Getenv("TMPDIR")) {
-		t.Fatalf("edit must use its own directory, got the shared temp dir %s", dir)
-	}
-	if mode != "700" {
-		t.Fatalf("temp directory mode %s, want 700", mode)
-	}
-	if _, err := os.Stat(dir); !os.IsNotExist(err) {
-		t.Fatalf("temp directory %s survived the edit (err %v)", dir, err)
-	}
-}
-
 // Final review minor 9: a skipped import entry is named by its number, never
 // its text (it may be a secret).
 func TestMemoryImportSkipNamesTheEntryNotItsText(t *testing.T) {
```

`cmd/herdr-dots/memory_unix_test.go`:

```go
//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/memory"
)

// The memory editor tests drive a sh script as $EDITOR (sed, stat), so they
// run on Unix; on Windows the editor goes through cmd.exe
// (platform.EditorCommand, tested in internal/platform).

func TestMemoryEditAppliesExactlyOneUpdate(t *testing.T) {
	c, out, _ := newMemCLI(t)
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Sessions expire after one hour")
	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/Use zod for validation/Use valibot for validation/' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	out.Reset()
	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
	if !strings.Contains(out.String(), "applied: 0 added, 1 updated, 0 forgotten") {
		t.Fatalf("%s", out)
	}
	e, _ := c.store.Get(testPID, memory.Ref{Scope: memory.ScopeDot, Dot: "security", ID: "m1"})
	if e.Text != "Use valibot for validation" {
		t.Fatalf("%+v", e)
	}
	c.editor = ""
	if err := c.run([]string{"edit"}); err == nil || !strings.Contains(err.Error(), "EDITOR") {
		t.Fatalf("want an $EDITOR hint, got %v", err)
	}
}

func TestMemoryEditMarksUserSource(t *testing.T) {
	c, _, _ := newMemCLI(t)
	if _, err := c.store.Add(testPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "Use zod for validation", Source: memory.SourceModel, Status: memory.StatusActive}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/zod/valibot/' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
	e, _ := c.store.Get(testPID, memory.Ref{Scope: memory.ScopeDot, Dot: "security", ID: "m1"})
	if e.Source != memory.SourceUser {
		t.Fatalf("source %q", e.Source)
	}
}

// Final review minor 8: edit works in a private 0700 directory that is
// removed afterwards with whatever the editor left in it (swap, backup).
func TestMemoryEditRemovesItsTempDirectory(t *testing.T) {
	c, _, _ := newMemCLI(t)
	t.Setenv("TMPDIR", t.TempDir())
	mustRun(t, c, "add", "--scope", "dot", "--dot", "security", "Use zod for validation")
	record := filepath.Join(t.TempDir(), "record")
	script := filepath.Join(t.TempDir(), "editor.sh")
	body := "#!/bin/sh\nd=$(dirname \"$1\")\necho \"$d\" > " + record + "\n" +
		"(stat -f %Lp \"$d\" 2>/dev/null || stat -c %a \"$d\") >> " + record + "\n" +
		"touch \"$d/.swp-leftover\"\nsed -i.bak 's/zod/valibot/' \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	c.editor = script
	mustRun(t, c, "edit", "--scope", "dot", "--dot", "security")
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(data))
	if len(lines) != 2 {
		t.Fatalf("record %q", data)
	}
	dir, mode := lines[0], lines[1]
	if filepath.Clean(dir) == filepath.Clean(os.Getenv("TMPDIR")) {
		t.Fatalf("edit must use its own directory, got the shared temp dir %s", dir)
	}
	if mode != "700" {
		t.Fatalf("temp directory mode %s, want 700", mode)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("temp directory %s survived the edit (err %v)", dir, err)
	}
}
```

`internal/mcpinstall/install_test.go` (absolute test paths on every OS without backslashes, so the expected TOML and JSON stay byte-exact; privacy through `platformtest`; the user's own file mode compared with its mode before the write; symlinks skip where refused):

```diff
--- a/internal/mcpinstall/install_test.go
+++ b/internal/mcpinstall/install_test.go
@@ -4,16 +4,24 @@ import (
 	"errors"
 	"os"
 	"path/filepath"
+	"runtime"
 	"slices"
 	"strings"
 	"testing"
 	"time"
+
+	"herdr-dots/internal/platform/platformtest"
 )
 
-const (
-	tBin   = "/opt/herdr dots/bin/herdr-dots"
-	tCfg   = "/home/u/.config/herdr/plugins/herdr-dots"
-	tState = "/home/u/.local/state/herdr/plugins/herdr-dots"
+// tRoot is "" on Unix and the temp dir's drive ("C:") on Windows, so the
+// paths below are absolute everywhere (checkInputs) and hold no backslash,
+// which keeps the expected TOML and JSON byte-exact on every OS.
+var tRoot = filepath.VolumeName(os.TempDir())
+
+var (
+	tBin   = tRoot + "/opt/herdr dots/bin/herdr-dots"
+	tCfg   = tRoot + "/home/u/.config/herdr/plugins/herdr-dots"
+	tState = tRoot + "/home/u/.local/state/herdr/plugins/herdr-dots"
 )
 
 var tNow = time.Date(2026, 10, 8, 14, 30, 0, 0, time.UTC)
@@ -43,6 +51,18 @@ func readFile(t *testing.T, path string) string {
 	return string(b)
 }
 
+// symlinkOrSkip creates a symlink, skipping the test where the OS refuses
+// (Windows without Developer Mode or admin rights).
+func symlinkOrSkip(t *testing.T, target, link string) {
+	t.Helper()
+	if err := os.Symlink(target, link); err != nil {
+		if runtime.GOOS == "windows" {
+			t.Skipf("symlinks unavailable: %v", err)
+		}
+		t.Fatal(err)
+	}
+}
+
 func noRun(t *testing.T) func(Command) error {
 	return func(c Command) error { t.Fatalf("unexpected command %v", c.Args); return nil }
 }
@@ -58,10 +78,10 @@ args = ["--port", "0"]
 model = "gpt-6-luna"
 `
 
-const wantBlock = "# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)\n" +
+var wantBlock = "# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)\n" +
 	"[mcp_servers.herdr-dots]\n" +
-	"command = \"/opt/herdr dots/bin/herdr-dots\"\n" +
-	"args = [\"mcp\", \"--config-dir\", \"/home/u/.config/herdr/plugins/herdr-dots\", \"--state-dir\", \"/home/u/.local/state/herdr/plugins/herdr-dots\"]\n" +
+	"command = \"" + tBin + "\"\n" +
+	"args = [\"mcp\", \"--config-dir\", \"" + tCfg + "\", \"--state-dir\", \"" + tState + "\"]\n" +
 	"env_vars = [\"HERDR_PANE_ID\", \"HERDR_SOCKET_PATH\"]\n" +
 	"startup_timeout_sec = 20\n" +
 	"tool_timeout_sec = 180\n" +
@@ -108,6 +128,7 @@ func TestCodexInstallPreservesOtherTablesAndRemoveRestores(t *testing.T) {
 	env := tempEnv(t)
 	path := env.CodexConfigPath()
 	writeFile(t, path, userCodex)
+	before, _ := os.Stat(path) // 0640 on Unix; Windows reports 0666
 	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
 	if err != nil {
 		t.Fatal(err)
@@ -125,12 +146,10 @@ func TestCodexInstallPreservesOtherTablesAndRemoveRestores(t *testing.T) {
 	if len(backups) != 1 || backups[0] != path+".bak-herdr-dots-20261008-143000" || readFile(t, backups[0]) != userCodex {
 		t.Fatalf("backups %v", backups)
 	}
-	if st, _ := os.Stat(path); st.Mode().Perm() != 0o640 {
-		t.Fatalf("mode %v: the file keeps its mode", st.Mode().Perm())
-	}
-	if st, _ := os.Stat(backups[0]); st.Mode().Perm() != 0o600 {
-		t.Fatalf("backup mode %v", st.Mode().Perm())
+	if st, _ := os.Stat(path); st.Mode().Perm() != before.Mode().Perm() {
+		t.Fatalf("mode %v -> %v: the file keeps its mode", before.Mode().Perm(), st.Mode().Perm())
 	}
+	platformtest.AssertPrivateFile(t, backups[0])
 
 	again, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
 	if err != nil || !again.Nothing {
@@ -167,9 +186,7 @@ func TestCodexInstallIntoAMissingFile(t *testing.T) {
 	if got := readFile(t, env.CodexConfigPath()); got != wantBlock {
 		t.Fatalf("got:\n%s", got)
 	}
-	if st, _ := os.Stat(env.CodexConfigPath()); st.Mode().Perm() != 0o600 {
-		t.Fatalf("a new config is private: %v", st.Mode().Perm())
-	}
+	platformtest.AssertPrivateFile(t, env.CodexConfigPath()) // a new config is private
 }
 
 func TestCodexUnmarkedTableIsReplacedInPlace(t *testing.T) {
@@ -246,7 +263,7 @@ func TestApplyRunsCommandsAndIgnoresOnlyMayFail(t *testing.T) {
 
 func TestPlanRefusesRelativeOrControlPaths(t *testing.T) {
 	env := tempEnv(t)
-	for _, bin := range []string{"bin/herdr-dots", "/bin/herdr\n-dots"} {
+	for _, bin := range []string{"bin/herdr-dots", tRoot + "/bin/herdr\n-dots"} {
 		if _, err := PlanInstall(ClientCodex, env, bin, tCfg, tState); err == nil {
 			t.Errorf("bin %q accepted", bin)
 		}
@@ -271,9 +288,7 @@ func TestRecordsAndStatusLine(t *testing.T) {
 	if err != nil || len(loaded) != 2 {
 		t.Fatalf("loaded %+v %v", loaded, err)
 	}
-	if st, _ := os.Stat(RecordPath(dir)); st.Mode().Perm() != 0o600 {
-		t.Fatalf("mode %v", st.Mode().Perm())
-	}
+	platformtest.AssertPrivateFile(t, RecordPath(dir))
 	if err := SaveRecords(RecordPath(dir), WithoutClient(loaded, ClientClaude)); err != nil {
 		t.Fatal(err)
 	}
@@ -400,9 +415,7 @@ func TestDanglingSymlinkConfigIsRefused(t *testing.T) {
 	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
 		t.Fatal(err)
 	}
-	if err := os.Symlink(filepath.Join(t.TempDir(), "nope.toml"), path); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, filepath.Join(t.TempDir(), "nope.toml"), path)
 	for _, plan := range []func() (Plan, error){
 		func() (Plan, error) { return PlanInstall(ClientCodex, env, tBin, tCfg, tState) },
 		func() (Plan, error) { return PlanRemove(ClientCodex, env) },
@@ -424,9 +437,7 @@ func TestSymlinkedConfigIsEditedThroughTheLink(t *testing.T) {
 	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
 		t.Fatal(err)
 	}
-	if err := os.Symlink(target, path); err != nil {
-		t.Fatal(err)
-	}
+	symlinkOrSkip(t, target, path)
 	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
 	if err != nil {
 		t.Fatal(err)
```

`internal/mcpinstall/final_test.go` and the new `internal/mcpinstall/final_unix_test.go` (the fix wave's `chmod 000` test moves, unchanged, to a Unix-only file; Plan decision 34):

```diff
--- a/internal/mcpinstall/final_test.go
+++ b/internal/mcpinstall/final_test.go
@@ -1,7 +1,6 @@
 package mcpinstall
 
 import (
-	"os"
 	"strings"
 	"testing"
 )
@@ -26,27 +25,3 @@ func TestCodexReinstallNotesTheUsersEditsInsideTheBlock(t *testing.T) {
 		t.Fatalf("plain reinstall: %+v %v", p, err)
 	}
 }
-
-// Phase 5 final review M7: a Claude config that exists but can't be read is
-// never "not installed"; both remove and install refuse and say how to fix it.
-func TestUnreadableClaudeConfigIsRefused(t *testing.T) {
-	if os.Geteuid() == 0 {
-		t.Skip("root reads a mode-000 file")
-	}
-	env := tempEnv(t)
-	claudeEntryJSON(t, env, `{"type": "stdio"}`)
-	path := env.ClaudeConfigPath()
-	if err := os.Chmod(path, 0); err != nil {
-		t.Fatal(err)
-	}
-	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
-	want := "can't read " + path + ": permission denied; fix it or remove manually with `claude mcp remove --scope user herdr-dots`"
-	p, err := PlanRemove(ClientClaude, env)
-	if err == nil || err.Error() != want {
-		t.Fatalf("remove: %+v %v", p, err)
-	}
-	p, err = PlanInstall(ClientClaude, env, tBin, tCfg, tState)
-	if err == nil || !strings.HasPrefix(err.Error(), "can't read "+path+": permission denied") {
-		t.Fatalf("install: %+v %v", p, err)
-	}
-}
```

`internal/mcpinstall/final_unix_test.go`:

```go
//go:build unix

package mcpinstall

import (
	"os"
	"strings"
	"testing"
)

// Mode 000 makes a file unreadable only on Unix (on Windows it only sets the
// read-only attribute), so this Phase 5 test runs there; the refusal itself
// is the same code on every OS.

// Phase 5 final review M7: a Claude config that exists but can't be read is
// never "not installed"; both remove and install refuse and say how to fix it.
func TestUnreadableClaudeConfigIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 file")
	}
	env := tempEnv(t)
	claudeEntryJSON(t, env, `{"type": "stdio"}`)
	path := env.ClaudeConfigPath()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	want := "can't read " + path + ": permission denied; fix it or remove manually with `claude mcp remove --scope user herdr-dots`"
	p, err := PlanRemove(ClientClaude, env)
	if err == nil || err.Error() != want {
		t.Fatalf("remove: %+v %v", p, err)
	}
	p, err = PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	if err == nil || !strings.HasPrefix(err.Error(), "can't read "+path+": permission denied") {
		t.Fatalf("install: %+v %v", p, err)
	}
}
```

`cmd/herdr-dots/setupmcp_test.go` (absolute paths built for the OS; the `go run` check matches `<sep>go-build`):

```diff
--- a/cmd/herdr-dots/setupmcp_test.go
+++ b/cmd/herdr-dots/setupmcp_test.go
@@ -47,7 +47,7 @@ func newMCPSetupRig(t *testing.T, onPath ...string) *mcpSetupRig {
 		},
 		env:   mcpinstall.Env{Getenv: func(string) string { return "" }, Home: r.home},
 		now:   func() time.Time { return time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC) },
-		exe:   "/opt/herdr-dots/bin/herdr-dots",
+		exe:   filepath.Join(r.home, "opt", "herdr-dots", "bin", "herdr-dots"), // absolute on every OS
 		paths: func() (paths.Paths, error) { return r.p, nil },
 	}
 	return r
@@ -129,7 +129,7 @@ func TestSetupMCPBothSkipsAMissingClientAndInstallsCodex(t *testing.T) {
 
 func TestSetupMCPRefusesGoRunBinaries(t *testing.T) {
 	r := newMCPSetupRig(t, "claude")
-	r.io.exe = "/var/folders/x/T/go-build123/b001/exe/herdr-dots"
+	r.io.exe = filepath.Join(os.TempDir(), "go-build123", "b001", "exe", "herdr-dots")
 	if err := setupMCP([]string{"--yes"}, r.io); err == nil || !strings.Contains(err.Error(), "go run") {
 		t.Fatalf("got %v", err)
 	}
```

`internal/backend/codex_mcp_test.go` (the home variable a codex child reads on this OS):

```diff
--- a/internal/backend/codex_mcp_test.go
+++ b/internal/backend/codex_mcp_test.go
@@ -6,6 +6,8 @@ import (
 	"slices"
 	"strings"
 	"testing"
+
+	"herdr-dots/internal/platform"
 )
 
 func codexHomeWith(t *testing.T, config string) string {
@@ -31,12 +33,12 @@ const withServer = "model = \"x\"\n\n[mcp_servers.docs]\ncommand = \"docs\"\n\n[
 // Review Focus 1: a codex dot run must not start the Dots MCP server.
 func TestCodexDisablesTheDotsMCPServerWhenConfigured(t *testing.T) {
 	home := codexHomeWith(t, withServer)
-	args := codexArgs("HOME="+home, "PATH=/bin", "HERDR_PANE_ID=w1:p1")
+	args := codexArgs(platform.HomeEnv+"="+home, "PATH=/bin", "HERDR_PANE_ID=w1:p1")
 	n := len(args)
 	if args[n-1] != "-" || args[n-3] != "-c" || args[n-2] != "mcp_servers.herdr-dots.enabled=false" {
 		t.Fatalf("args %v: the override sits right before the stdin marker", args)
 	}
-	cmd := CodexCLI{Environ: func() []string { return []string{"HOME=" + home, "HERDR_PANE_ID=w1:p1"} }}.BuildCmd(req())
+	cmd := CodexCLI{Environ: func() []string { return []string{platform.HomeEnv + "=" + home, "HERDR_PANE_ID=w1:p1"} }}.BuildCmd(req())
 	if slices.Contains(cmd.Env, "HERDR_PANE_ID=w1:p1") {
 		t.Fatal("HERDR_* must still be stripped")
 	}
@@ -48,7 +50,7 @@ func TestCodexHomeOverridesHome(t *testing.T) {
 		t.Fatal(err)
 	}
 	empty := codexHomeWith(t, "")
-	if args := codexArgs("HOME="+empty, "CODEX_HOME="+codexHome); !slices.Contains(args, "mcp_servers.herdr-dots.enabled=false") {
+	if args := codexArgs(platform.HomeEnv+"="+empty, "CODEX_HOME="+codexHome); !slices.Contains(args, "mcp_servers.herdr-dots.enabled=false") {
 		t.Fatalf("CODEX_HOME config not read: %v", args)
 	}
 }
@@ -61,7 +63,7 @@ func TestCodexLeavesTheOverrideOutWithoutTheServer(t *testing.T) {
 		"commented table": "# [mcp_servers.herdr-dots]\n",
 	} {
 		home := codexHomeWith(t, cfg)
-		if args := codexArgs("HOME="+home, "PATH=/bin"); strings.Contains(strings.Join(args, " "), "mcp_servers.") {
+		if args := codexArgs(platform.HomeEnv+"="+home, "PATH=/bin"); strings.Contains(strings.Join(args, " "), "mcp_servers.") {
 			t.Errorf("%s: args %v", name, args)
 		}
 	}
@@ -76,7 +78,7 @@ func TestCodexUserMCPArgsCannotReplaceTheOverride(t *testing.T) {
 	home := codexHomeWith(t, withServer)
 	r := req()
 	r.Args = []string{"-c", "mcp_servers.herdr-dots.enabled=true", "--config=mcp_servers.x.command=y"}
-	args := CodexCLI{Environ: func() []string { return []string{"HOME=" + home} }}.BuildCmd(r).Args
+	args := CodexCLI{Environ: func() []string { return []string{platform.HomeEnv + "=" + home} }}.BuildCmd(r).Args
 	if strings.Contains(strings.Join(args, " "), "enabled=true") || strings.Contains(strings.Join(args, " "), "mcp_servers.x") {
 		t.Fatalf("denied user args leaked: %v", args)
 	}
```

`internal/mcpserver/engine_test.go` (the stub engine, like the real one in `internal/engine/ask_test.go` below, serves `platformtest.Endpoint` through `ipc.Listen`, so both run over a pipe on Windows; `ipc.Client` dials it with `ipc.Dial`):

```diff
--- a/internal/mcpserver/engine_test.go
+++ b/internal/mcpserver/engine_test.go
@@ -4,13 +4,12 @@ import (
 	"context"
 	"encoding/json"
 	"errors"
-	"net"
 	"os"
-	"path/filepath"
 	"testing"
 	"time"
 
 	"herdr-dots/internal/ipc"
+	"herdr-dots/internal/platform/platformtest"
 	"herdr-dots/internal/view"
 )
 
@@ -31,19 +30,16 @@ func (s stubEngine) Query(_ context.Context, cmd string, raw json.RawMessage) (a
 	return ipc.DotsResult{ProjectID: testPID, Dots: []ipc.DotInfo{{Slug: "security"}}}, nil
 }
 
+// shortSock is a fresh engine endpoint: a short unix socket path, or a named
+// pipe on Windows.
 func shortSock(t *testing.T) string {
 	t.Helper()
-	dir, err := os.MkdirTemp("", "mcpe")
-	if err != nil {
-		t.Fatal(err)
-	}
-	t.Cleanup(func() { os.RemoveAll(dir) })
-	return filepath.Join(dir, "e.sock")
+	return platformtest.Endpoint(t)
 }
 
 func startStub(t *testing.T, path string) context.CancelFunc {
 	t.Helper()
-	ln, err := net.Listen("unix", path)
+	ln, err := ipc.Listen(path)
 	if err != nil {
 		t.Fatal(err)
 	}
```

`internal/engine/ask_test.go`:

```diff
--- a/internal/engine/ask_test.go
+++ b/internal/engine/ask_test.go
@@ -4,9 +4,6 @@ import (
 	"context"
 	"encoding/json"
 	"errors"
-	"net"
-	"os"
-	"path/filepath"
 	"strings"
 	"sync"
 	"sync/atomic"
@@ -18,6 +15,7 @@ import (
 	"herdr-dots/internal/dots"
 	"herdr-dots/internal/ipc"
 	"herdr-dots/internal/memory"
+	"herdr-dots/internal/platform/platformtest"
 	"herdr-dots/internal/queue"
 	"herdr-dots/internal/state"
 	"herdr-dots/internal/transcript"
@@ -338,24 +336,20 @@ func TestListDotsAndListQueue(t *testing.T) {
 	}
 }
 
-// The engine answers queries over its real socket (ipc.Querier).
+// The engine answers queries over its real endpoint (ipc.Querier).
 func TestQueriesOverTheEngineSocket(t *testing.T) {
 	r := newRig(t, dot("Security", dots.ModeQueue))
 	r.attach(t, "security")
 	r.be.set(critical)
-	dir, err := os.MkdirTemp("", "engq")
-	if err != nil {
-		t.Fatal(err)
-	}
-	t.Cleanup(func() { os.RemoveAll(dir) })
-	ln, err := net.Listen("unix", filepath.Join(dir, "d.sock"))
+	endpoint := platformtest.Endpoint(t)
+	ln, err := ipc.Listen(endpoint)
 	if err != nil {
 		t.Fatal(err)
 	}
 	ctx, cancel := context.WithCancel(context.Background())
 	t.Cleanup(cancel)
 	go ipc.Serve(ctx, ln, r.e)
-	c := ipc.Client{Path: filepath.Join(dir, "d.sock")}
+	c := ipc.Client{Path: endpoint}
 	qctx, qcancel := context.WithTimeout(ctx, 5*time.Second)
 	defer qcancel()
 	var res ipc.AskResult
```

Then check that no Phase 5 test still assumes Unix:

```bash
grep -rnE 'Mode\(\)\.Perm\(\)|net\.Listen\("unix"|"sh", "-c"|exec\.Command\("sh"|/bin/sh|os\.Symlink\(|os\.Getuid|0o644\)|"/dev/null"|filepath\.Join\("/|"HOME="' --include='*_test.go' internal/mcpinstall internal/mcpserver internal/engine internal/ipc cmd/herdr-dots/mcp_test.go cmd/herdr-dots/setupmcp_test.go cmd/herdr-dots/memory_test.go internal/backend/codex_mcp_test.go | grep -v '_unix_test.go'
```

Expected: exactly three lines, all in `internal/mcpinstall/install_test.go`: the `os.Symlink` inside `symlinkOrSkip`, and the two lines comparing the user's file mode with `before.Mode().Perm()`. Also check `grep -rn 'os.Chmod(' --include='*_test.go' internal/mcpinstall | grep -v _unix_test.go` prints nothing (mode 000 tests are Unix-only).

- [ ] **Step 6: Run the guard and the tests**

Run: `gofmt -w internal cmd && GOTOOLCHAIN=local go test -race ./...`
Expected: PASS, including `TestProductCodeHasNoUnixOnlyCalls`, `TestEditorCommandPassesTheFileAsAnArgument`, Phase 5's `TestCodexInstallPreservesOtherTablesAndRemoveRestores`, `TestClaudePlanIsTheExactCLICommand`, `TestCodexDisablesTheDotsMCPServerWhenConfigured`, `TestCodexHomeOverridesHome`, `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies`, `TestQueriesOverTheEngineSocket`, `TestSetupMCPRefusesGoRunBinaries`, and Phase 2's moved `TestMemoryEditAppliesExactlyOneUpdate`, `TestMemoryEditMarksUserSource`, `TestMemoryEditRemovesItsTempDirectory`.

- [ ] **Step 7: Cross-OS gate (everything green from here on)**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./...
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./...
```

Expected: `gofmt` prints nothing and every other command succeeds (the Windows build and vet of `./...` are clean; one `*.test.exe` per package with tests in `$TMPDIR/p6-win`, plus `[no test files]` lines for `herdrtest`, `platformtest` and `version`).

- [ ] **Step 8: Notes and commit**

Append to `docs/superpowers/notes/2026-10-08-phase-6-verification.md`:

```markdown
## Phase 5 port (Task 10)

- Guard findings before the port: `cmd/herdr-dots/memory.go` (`sh -c` editor, Phase 2), `internal/backend/codex_mcp.go` (`HOME`, `.codex`), `internal/mcpinstall/install.go` (`.claude.json`, `.codex`). All fixed; the guard passes.
- Ported without a guard finding: `setup-mcp` children through `platform.Command` / `platform.LookPath`; `mcp.go` and its test on `EngineEndpoint` (Task 6); Phase 5's IPC, MCP and engine tests on `ipc.Listen` endpoints.
- Not run on Windows: `claude mcp add` through npm's `claude.cmd`, `codex mcp list` through `codex.cmd`, the detached engine started by the MCP server, and `memory edit` through cmd.exe are user gates.
```

```bash
git add internal/platform docs/superpowers/notes/2026-10-08-phase-6-verification.md internal/mcpinstall internal/mcpserver internal/engine/ask_test.go internal/backend/codex_mcp.go internal/backend/codex_mcp_test.go cmd/herdr-dots/setupmcp.go cmd/herdr-dots/setupmcp_test.go cmd/herdr-dots/memory.go cmd/herdr-dots/memory_test.go cmd/herdr-dots/memory_unix_test.go
git status --short   # nothing else is modified
git commit -m "feat(platform): portability guard; MCP installer, codex config lookup and memory editor run on Windows (Phase 6)"
```

---

### Task 11: Test-suite portability

**Depends on:** Tasks 2–9 (Wave 3). The CI matrix runs `go test -race ./...` on Windows, so tests that assume Unix (mode bits, `sh`, absolute paths like `/c`) must state their intent portably. Product code doesn't change in this task.

**Files:**
- Modify: `internal/memory/store_test.go`, `internal/paths/memory_test.go`, `internal/state/state_test.go`, `internal/queue/queue_test.go`, `internal/dots/store_test.go`
- Modify (Phase 4 tests the reconcile audit found): `internal/paths/providers_test.go`, `cmd/herdr-dots/keys_test.go`. (Phase 4's toolloop and credentials tests are ported in Task 8; Phase 2 and Phase 5 tests that Task 10 owns are not touched here, nor `cmd/herdr-dots/platform_manifest_test.go` or `memwire_test.go`, which Task 12 owns.)

**Interfaces:**
- Consumes: `platformtest.Abs(parts ...string) string`, `platformtest.AssertPrivateFile(t testing.TB, path string)`, `platformtest.AssertPrivateDir(t testing.TB, path string)`.
- Produces: no product symbols; the darwin suite stays green and the Windows test binaries compile.

- [ ] **Step 1: Port the v1 and Phase 2 tests**

`internal/memory/store_test.go`:

```diff
--- a/internal/memory/store_test.go
+++ b/internal/memory/store_test.go
@@ -12,6 +12,7 @@ import (
 	"time"
 
 	"herdr-dots/internal/lockfile"
+	"herdr-dots/internal/platform/platformtest"
 )
 
 const pid = "g-0123456789abcdef"
@@ -283,14 +284,10 @@ func TestFilesArePrivate(t *testing.T) {
 	mustAdd(t, s, pid, dotNote("security", "a fact"))
 	mustAdd(t, s, "", Entry{Scope: ScopeGlobal, Dot: "security", Kind: KindPreference, Text: "British spelling"})
 	for _, d := range []string{s.Dir, filepath.Join(s.Dir, pid), filepath.Join(s.Dir, pid, "dots"), filepath.Join(s.Dir, GlobalID, "dots")} {
-		if st, err := os.Stat(d); err != nil || st.Mode().Perm() != 0o700 {
-			t.Errorf("%s: %v %v", d, err, st)
-		}
+		platformtest.AssertPrivateDir(t, d)
 	}
 	for _, f := range []string{filepath.Join(s.Dir, pid, "dots", "security.json"), filepath.Join(s.Dir, pid, "memory.lock"), filepath.Join(s.Dir, GlobalID, "dots", "security.json")} {
-		if st, err := os.Stat(f); err != nil || st.Mode().Perm() != 0o600 {
-			t.Errorf("%s: %v %v", f, err, st)
-		}
+		platformtest.AssertPrivateFile(t, f)
 	}
 }
 
```

`internal/paths/memory_test.go`:

```diff
--- a/internal/paths/memory_test.go
+++ b/internal/paths/memory_test.go
@@ -7,17 +7,20 @@ import (
 	"path/filepath"
 	"strings"
 	"testing"
+
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func TestMemoryDirIsSharedAcrossSessions(t *testing.T) {
-	p := New("/c", "/s", "/sock")
-	if p.MemoryDir() != "/s/memory" {
+	c, s := platformtest.Abs("c"), platformtest.Abs("s")
+	p := New(c, s, "/sock")
+	if p.MemoryDir() != filepath.Join(s, "memory") {
 		t.Fatalf("memory dir %q", p.MemoryDir())
 	}
 	if strings.HasPrefix(p.MemoryDir(), p.SessionDir) {
 		t.Fatal("memory must not live under the per-session dir")
 	}
-	if New("/c", "/s", "/other.sock").MemoryDir() != p.MemoryDir() {
+	if New(c, s, "/other.sock").MemoryDir() != p.MemoryDir() {
 		t.Fatal("two Herdr sessions must share one memory dir")
 	}
 }
@@ -30,17 +33,12 @@ func TestEnsureCreatesMemoryDirAndStatePointer(t *testing.T) {
 			t.Fatal(err)
 		}
 	}
-	st, err := os.Stat(p.MemoryDir())
-	if err != nil || st.Mode().Perm() != 0o700 {
-		t.Fatalf("memory dir: %v %v", err, st)
-	}
+	platformtest.AssertPrivateDir(t, p.MemoryDir())
 	b, err := os.ReadFile(p.StatePointerFile())
 	if err != nil || strings.TrimSpace(string(b)) != p.StateDir {
 		t.Fatalf("pointer = %q, %v", b, err)
 	}
-	if st, _ := os.Stat(p.StatePointerFile()); st.Mode().Perm() != 0o600 {
-		t.Fatalf("pointer perm %v", st.Mode().Perm())
-	}
+	platformtest.AssertPrivateFile(t, p.StatePointerFile())
 }
 
 func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
@@ -50,8 +48,9 @@ func TestDiscoverPrefersHerdrEnv(t *testing.T) {
 		t.Fatal("must not shell out when the env is set")
 		return "", nil
 	}
-	p, err := discover(env(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": "/c", "HERDR_PLUGIN_STATE_DIR": "/s"}), run)
-	if err != nil || p.ConfigDir != "/c" || p.StateDir != "/s" || p.MemoryDir() != "/s/memory" {
+	c, s := platformtest.Abs("c"), platformtest.Abs("s")
+	p, err := discover(env(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": c, "HERDR_PLUGIN_STATE_DIR": s}), run)
+	if err != nil || p.ConfigDir != c || p.StateDir != s || p.MemoryDir() != filepath.Join(s, "memory") {
 		t.Fatalf("%+v %v", p, err)
 	}
 }
@@ -128,7 +127,7 @@ func TestDiscoverRejectsRelativeStateDir(t *testing.T) {
 	run := func(context.Context, string, ...string) (string, error) {
 		return "", nil
 	}
-	_, err := discover(env(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": "/c", "HERDR_PLUGIN_STATE_DIR": "relative/state"}), run)
+	_, err := discover(env(map[string]string{"HERDR_PLUGIN_CONFIG_DIR": platformtest.Abs("c"), "HERDR_PLUGIN_STATE_DIR": "relative/state"}), run)
 	if err == nil || !strings.Contains(err.Error(), "not absolute") || !strings.Contains(err.Error(), "HERDR_PLUGIN_STATE_DIR") {
 		t.Fatalf("want error about HERDR_PLUGIN_STATE_DIR not absolute, got %v", err)
 	}
```

`internal/state/state_test.go`:

```diff
--- a/internal/state/state_test.go
+++ b/internal/state/state_test.go
@@ -9,6 +9,8 @@ import (
 	"strings"
 	"testing"
 	"time"
+
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func TestAttachmentsCRUDAndPersistence(t *testing.T) {
@@ -136,10 +138,7 @@ func TestFeedAppendRecentAndReload(t *testing.T) {
 	if len(r) != 5 || r[4].Turn != 60 || r[0].Turn != 56 {
 		t.Fatalf("recent %+v", r)
 	}
-	st, _ := os.Stat(p)
-	if st.Mode().Perm() != 0o600 {
-		t.Fatalf("perm %v", st.Mode().Perm())
-	}
+	platformtest.AssertPrivateFile(t, p)
 	f2, _ := OpenFeed(p)
 	if r := f2.Recent("p1", 100); len(r) != 50 || r[49].Turn != 60 {
 		t.Fatalf("reload keeps last 50 per pane, got %d", len(r))
```

`internal/queue/queue_test.go`:

```diff
--- a/internal/queue/queue_test.go
+++ b/internal/queue/queue_test.go
@@ -5,6 +5,8 @@ import (
 	"os"
 	"path/filepath"
 	"testing"
+
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func open(t *testing.T) (*Store, string) {
@@ -22,10 +24,7 @@ func TestAddAssignsIDAndPersists(t *testing.T) {
 	if err != nil || c.ID == "" || c.Status != StatusPending || c.CreatedAt.IsZero() {
 		t.Fatalf("%+v %v", c, err)
 	}
-	st, _ := os.Stat(p)
-	if st.Mode().Perm() != 0o600 {
-		t.Fatalf("perm %v", st.Mode().Perm())
-	}
+	platformtest.AssertPrivateFile(t, p)
 	s2, _ := Open(p)
 	if got, ok := s2.Get(c.ID); !ok || got.Body != "x" {
 		t.Fatal("not persisted")
```

`internal/dots/store_test.go`:

```diff
--- a/internal/dots/store_test.go
+++ b/internal/dots/store_test.go
@@ -6,6 +6,8 @@ import (
 	"path/filepath"
 	"strings"
 	"testing"
+
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func valid(name string) Dot {
@@ -28,10 +30,7 @@ func TestSaveLoadRoundTrip(t *testing.T) {
 	if got.Name != "Security" || got.Purpose != d.Purpose || got.Trigger.FileGlobs[0] != "**/*.ts" || got.Slug != "security" {
 		t.Fatalf("round trip: %+v", got)
 	}
-	st, _ := os.Stat(filepath.Join(dir, "security.toml"))
-	if st.Mode().Perm() != 0o600 {
-		t.Fatalf("perm %v", st.Mode().Perm())
-	}
+	platformtest.AssertPrivateFile(t, filepath.Join(dir, "security.toml"))
 	raw, _ := os.ReadFile(filepath.Join(dir, "security.toml"))
 	if !strings.Contains(string(raw), `"""`) {
 		t.Fatalf("purpose should be written as a multi-line string:\n%s", raw)
```

- [ ] **Step 2: Port the Phase 4 tests**

`internal/paths/providers_test.go` (expected paths built with `filepath.Join`, which uses `\` on Windows):

```diff
--- a/internal/paths/providers_test.go
+++ b/internal/paths/providers_test.go
@@ -1,10 +1,13 @@
 package paths
 
-import "testing"
+import (
+	"path/filepath"
+	"testing"
+)
 
 func TestProviderFilesLiveInTheConfigDir(t *testing.T) {
 	p := New("/cfg", "/state", "/tmp/herdr.sock")
-	if p.CredentialsFile() != "/cfg/credentials.toml" || p.PricesFile() != "/cfg/prices.toml" {
+	if p.CredentialsFile() != filepath.Join("/cfg", "credentials.toml") || p.PricesFile() != filepath.Join("/cfg", "prices.toml") {
 		t.Fatalf("%s %s", p.CredentialsFile(), p.PricesFile())
 	}
 }
```

`cmd/herdr-dots/keys_test.go` (`keys set` writes `credentials.toml` owner-only on every OS):

```diff
--- a/cmd/herdr-dots/keys_test.go
+++ b/cmd/herdr-dots/keys_test.go
@@ -4,7 +4,6 @@ import (
 	"bytes"
 	"context"
 	"errors"
-	"os"
 	"path/filepath"
 	"strings"
 	"testing"
@@ -12,6 +11,7 @@ import (
 	"herdr-dots/internal/config"
 	"herdr-dots/internal/credentials"
 	"herdr-dots/internal/paths"
+	"herdr-dots/internal/platform/platformtest"
 )
 
 func TestKeysNeverEchoAnUnrecognisedProviderArgument(t *testing.T) {
@@ -73,9 +73,9 @@ func TestKeysSetReadsStdinWritesOwnerOnlyAndVerifies(t *testing.T) {
 	if err := keysCmd([]string{"set", "anthropic"}, k); err != nil {
 		t.Fatal(err)
 	}
-	st, _ := os.Stat(k.store.Path)
-	if st.Mode().Perm() != 0o600 || out.String() != "stored · verified\n" || len(*verified) != 1 {
-		t.Fatalf("mode %v out %q verified %v", st.Mode().Perm(), out, *verified)
+	platformtest.AssertPrivateFile(t, k.store.Path)
+	if out.String() != "stored · verified\n" || len(*verified) != 1 {
+		t.Fatalf("out %q verified %v", out, *verified)
 	}
 	if key, _, _ := k.store.Lookup("anthropic"); key != "sk-ant-api03-PIPEDKEY00001234" {
 		t.Fatalf("stored %q", key)
```

(`internal/queue/queue_test.go` keeps its `os` import: Phase 3 tests in that file use it.)

- [ ] **Step 3: Audit what is left**

Run:

```bash
grep -rnE 'Mode\(\)\.Perm\(\)|net\.Listen\("unix"|"sh", "-c"|exec\.Command\("sh"|/bin/sh|os\.Symlink\(|os\.Getuid|0o644\)|"/dev/null"|filepath\.Join\("/|Setenv\("HOME"' --include='*_test.go' internal cmd \
  | grep -v '_unix_test.go' | grep -v '^internal/platform/' | grep -v 'symlinkOrSkip\|before.Mode()'
```

Expected (replayed with Tasks 8 and 10 merged; Task 10's files are in the same wave, so without them `internal/mcpinstall`, `internal/mcpserver`, `internal/engine/ask_test.go` and `cmd/herdr-dots/memory_test.go` lines also appear, and are Task 10's to fix): only data-only or intentional lines remain:

- `internal/memory/project_test.go`: the `#!/bin/sh` hook in `TestExecGitNeverRunsHooks` (the test asserts the hook never runs; Git for Windows has its own `sh`, so it holds there too).
- `internal/council/live_test.go`: the `0o644` of a recorded transcript (`RECORD=1` live test, never in CI).
- `internal/paths/paths_test.go`, `internal/paths/providers_test.go` and `internal/keys/keys_test.go`: `filepath.Join("/...` expected values (the code under test joins the same way).
- `internal/keys/keys_test.go`: the `os.Symlink` that skips on Windows (Task 7).
- `internal/backend/toolloop/tools_test.go`: `setHome`'s own `t.Setenv("HOME", dir)` (it sets `USERPROFILE` too).
- `internal/credentials/perms_windows_test.go`, `internal/backend/toolloop/tools_test.go`, `internal/mcpinstall/install_test.go`: the `os.Symlink` inside each `symlinkOrSkip` helper.

Paths that are only data (transcript tool inputs such as `"/r/auth.ts"`, memory entry `Paths`, `"/repo"` working directories, socket paths that are hashed but never opened) stay as they are. If a commit after `a47427d` adds another hit, fix it with these rules:

| Pattern in a test | Fix |
|---|---|
| `st.Mode().Perm() != 0o600` or `!= 0o700`, asserting privacy | `platformtest.AssertPrivateFile(t, path)` / `platformtest.AssertPrivateDir(t, path)` (drop the now-unused `os.Stat`) |
| `Mode().Perm()` compared with a mode the test itself wrote (`0o640`) | take `before, _ := os.Stat(path)` before the action and compare with `before.Mode().Perm()` (Windows reports `0666`) |
| `os.WriteFile(..., 0o644)` or `os.Chmod(..., 0o644)` to make a file readable by others | a helper `readableByOthers(t, path)` in `<pkg>_unix_test.go` (`os.Chmod(path, 0o644)`) and `<pkg>_windows_test.go` (copy `setDACL`, `me` and `readableByOthers` from `internal/credentials/perms_windows_test.go`) |
| `net.Listen("unix", p)` | `herdrtest.Listen(p)` for a fake Herdr; `ipc.Listen(platformtest.Endpoint(t))` for an engine |
| a test that runs `sh`, `/bin/sh`, `sleep` or `cat` | move it into `<pkg>_unix_test.go` with `//go:build unix` as the first line; add a `cmd.exe` version in `<pkg>_windows_test.go` only when the code under test behaves differently on Windows |
| `os.Symlink` that fails | `if runtime.GOOS == "windows" { t.Skipf("symlinks unavailable: %v", err) }` before `t.Fatal(err)` (symlinks need Developer Mode or admin) |
| `t.Setenv("HOME", dir)` for code that calls `os.UserHomeDir` | also `t.Setenv("USERPROFILE", dir)` (Plan decision 28) |
| an absolute Unix path (`"/c"`, `"/repo"`) given to code that calls `filepath.IsAbs`, or compared with a `filepath.Join` result | `platformtest.Abs("c")` for inputs, `filepath.Join(...)` for expected values |
| `"/dev/null"` | `os.DevNull` |

- [ ] **Step 4: Run the tests**

Run: `gofmt -w internal cmd && GOTOOLCHAIN=local go test -race ./...`
Expected: PASS.

- [ ] **Step 5: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./internal/memory/ ./internal/paths/ ./internal/state/ ./internal/queue/ ./internal/dots/ ./cmd/herdr-dots/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed; the last two print nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/memory/store_test.go internal/paths/memory_test.go internal/paths/providers_test.go internal/state/state_test.go internal/queue/queue_test.go internal/dots/store_test.go cmd/herdr-dots/keys_test.go
git status --short   # nothing else is modified
git commit -m "test: state privacy and paths portably so the suite runs on Windows CI (Phase 6)" -m "data-only paths left as is: internal/memory/project_test.go (hook script), internal/council/live_test.go (recorded transcript)"
```

---

### Task 12: Manifest, `build.ps1`, CI matrix, `.gitattributes`, version 0.6.0

**Depends on:** Tasks 2–9 (Wave 3).

**Files:**
- Modify: `herdr-plugin.toml`, `internal/version/version.go`, `Makefile`
- Create: `scripts/build.ps1`, `.github/workflows/ci.yml`, `.gitattributes`
- Test: `cmd/herdr-dots/platform_manifest_test.go`; modify `cmd/herdr-dots/memwire_test.go` (Phase 5's version pin, Plan decision 33)

**Interfaces:**
- Consumes: `version.Version`; Phase 3's manifest test (`TestManifestDeclaresTheCouncilActionAndPopup`, `cmd/herdr-dots/council_test.go`) keeps passing because actions and panes are unchanged.
- Produces: `platforms = ["macos", "linux", "windows"]`; one `[[build]]` per OS; `version = "0.6.0"` in both places; `make cross`; the CI workflow (written, committed, not pushed).

- [ ] **Step 1: Write the failing test**

`cmd/herdr-dots/platform_manifest_test.go`:

```go
package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/version"
)

// platformManifest is the part of herdr-plugin.toml that decides where and
// how the plugin builds and runs.
type platformManifest struct {
	Version   string   `toml:"version"`
	Platforms []string `toml:"platforms"`
	Build     []struct {
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"build"`
	Startup []struct {
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"startup"`
	Actions []struct {
		ID        string   `toml:"id"`
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"actions"`
	Panes []struct {
		ID        string   `toml:"id"`
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"panes"`
}

func readPlatformManifest(t *testing.T) platformManifest {
	t.Helper()
	var m platformManifest
	if _, err := toml.DecodeFile("../../herdr-plugin.toml", &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestManifestDeclaresEveryPlatformAndOneBuildEach(t *testing.T) {
	m := readPlatformManifest(t)
	if !slices.Equal(m.Platforms, []string{"macos", "linux", "windows"}) {
		t.Fatalf("platforms %v", m.Platforms)
	}
	if m.Version != version.Version {
		t.Fatalf("manifest version %s, binary %s", m.Version, version.Version)
	}
	for _, goos := range m.Platforms {
		var builds [][]string
		for _, b := range m.Build {
			if len(b.Platforms) == 0 || slices.Contains(b.Platforms, goos) {
				builds = append(builds, b.Command)
			}
		}
		if len(builds) != 1 {
			t.Fatalf("%s gets %d build steps: %v", goos, len(builds), builds)
		}
		want := []string{"sh", "scripts/build.sh"}
		if goos == "windows" {
			want = []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "scripts/build.ps1"}
		}
		if !slices.Equal(builds[0], want) {
			t.Errorf("%s build %v, want %v", goos, builds[0], want)
		}
	}
}

// Herdr rejects duplicate action and pane ids even when their platforms
// differ, so every runtime command must work on every OS: ./bin/herdr-dots
// resolves to bin\herdr-dots.exe on Windows.
func TestRuntimeCommandsAreSharedByEveryPlatform(t *testing.T) {
	m := readPlatformManifest(t)
	check := func(kind, id string, cmd, platforms []string) {
		if len(platforms) != 0 {
			t.Errorf("%s %q must not narrow platforms (%v)", kind, id, platforms)
		}
		if len(cmd) == 0 || cmd[0] != "./bin/herdr-dots" {
			t.Errorf("%s %q runs %v, want ./bin/herdr-dots ...", kind, id, cmd)
		}
	}
	for _, s := range m.Startup {
		check("startup", "", s.Command, s.Platforms)
	}
	ids := map[string]bool{}
	for _, a := range m.Actions {
		check("action", a.ID, a.Command, a.Platforms)
		if ids["a:"+a.ID] {
			t.Errorf("duplicate action %s", a.ID)
		}
		ids["a:"+a.ID] = true
	}
	for _, p := range m.Panes {
		check("pane", p.ID, p.Command, p.Platforms)
		if ids["p:"+p.ID] {
			t.Errorf("duplicate pane %s", p.ID)
		}
		ids["p:"+p.ID] = true
	}
}

func TestWindowsBuildScriptBuildsTheExe(t *testing.T) {
	b, err := os.ReadFile("../../scripts/build.ps1")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`go build -o 'bin\herdr-dots.exe' '.\cmd\herdr-dots'`, "Go toolchain (>= 1.25) is required", "exit $LASTEXITCODE"} {
		if !strings.Contains(s, want) {
			t.Errorf("build.ps1 lacks %q", want)
		}
	}
}
```

`cmd/herdr-dots/memwire_test.go` (Phase 5 pinned the version here; rename the test and expect the new version before changing it):

```diff
--- a/cmd/herdr-dots/memwire_test.go
+++ b/cmd/herdr-dots/memwire_test.go
@@ -57,8 +57,8 @@ func TestBuildEngineWiresMemory(t *testing.T) {
 	}
 }
 
-func TestVersionIsPhaseFive(t *testing.T) {
-	if version.Version != "0.5.0" {
-		t.Fatalf("version %s, want 0.5.0", version.Version)
+func TestVersionIsPhaseSix(t *testing.T) {
+	if version.Version != "0.6.0" {
+		t.Fatalf("version %s, want 0.6.0", version.Version)
 	}
 }
```

- [ ] **Step 2: Run it to see it fail**

Run: `GOTOOLCHAIN=local go test ./cmd/herdr-dots/ -run 'TestManifestDeclaresEveryPlatform|TestRuntimeCommandsAreShared|TestWindowsBuildScript|TestVersionIsPhaseSix' -v`
Expected: FAIL: `platforms [macos linux]`, `open ../../scripts/build.ps1: no such file or directory` and `version 0.5.0, want 0.6.0`.

- [ ] **Step 3: The manifest and the version**

`herdr-plugin.toml` (post-Phase-5 form). Find `version = "0.5.0"` and replace it with `version = "0.6.0"`. Find `platforms = ["macos", "linux"]` and replace it with `platforms = ["macos", "linux", "windows"]`. Find:

```toml
[[build]]
command = ["sh", "scripts/build.sh"]
```

Replace with:

```toml
# One build step per OS (build steps have no id, so item-level platforms may
# split them). Actions and panes can't be split: Herdr rejects duplicate ids
# even across platforms, so they all run ./bin/herdr-dots, which Herdr
# resolves to bin\herdr-dots.exe on Windows.
[[build]]
platforms = ["macos", "linux"]
command = ["sh", "scripts/build.sh"]

[[build]]
platforms = ["windows"]
command = ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "scripts/build.ps1"]
```

Leave `min_herdr_version`, `[[startup]]`, every `[[actions]]` and every `[[panes]]` as they are (Plan decisions 2 and 17).

`internal/version/version.go`: replace `const Version = "0.5.0"` with `const Version = "0.6.0"`.

- [ ] **Step 4: The Windows build script**

`scripts/build.ps1`:

```powershell
# Herdr runs this as the manifest's Windows [[build]] step. It builds
# bin\herdr-dots.exe from the plugin root (this script's parent directory).
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Join-Path $PSScriptRoot '..')
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    [Console]::Error.WriteLine('herdr-dots: Go toolchain (>= 1.25) is required to build')
    exit 1
}
New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
& go build -o 'bin\herdr-dots.exe' '.\cmd\herdr-dots'
exit $LASTEXITCODE
```

- [ ] **Step 5: CI, line endings and `make cross`**

`.github/workflows/ci.yml`:

```yaml
# Herdr Dots CI: the same checks on macOS, Linux and Windows. Written in
# Phase 6; it runs once the user pushes the repository (nothing is pushed
# automatically).
name: ci

on:
  push:
  pull_request:

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
          GOOS=windows GOARCH=amd64 go build ./...
          GOOS=windows GOARCH=amd64 go vet ./...
          GOOS=darwin GOARCH=arm64 go build ./...
      - name: build the plugin (Windows)
        if: runner.os == 'Windows'
        shell: pwsh
        run: |
          powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1
          if (-not (Test-Path bin/herdr-dots.exe)) { throw "bin/herdr-dots.exe missing" }
          ./bin/herdr-dots.exe version
      - name: build the plugin (Unix)
        if: runner.os != 'Windows'
        run: |
          sh scripts/build.sh
          ./bin/herdr-dots version
```

`.gitattributes`:

```text
# Keep LF line endings in every checkout, Windows included: gofmt and the
# test data compare bytes.
* text=auto eol=lf
```

`Makefile`:

```diff
--- a/Makefile
+++ b/Makefile
@@ -1,9 +1,17 @@
-.PHONY: build test vet link
+.PHONY: build test vet cross link
 build:
 	sh scripts/build.sh
 test:
 	go test -race ./...
 vet:
 	go vet ./...
+# cross checks Linux and Windows from macOS: builds, vet and compiled (not run)
+# Windows test binaries. A real Windows run needs CI or a Windows machine.
+cross:
+	GOOS=linux GOARCH=amd64 go build ./...
+	GOOS=windows GOARCH=amd64 go build ./...
+	GOOS=windows GOARCH=amd64 go vet ./...
+	mkdir -p "$${TMPDIR:-/tmp}/herdr-dots-wintest"
+	GOOS=windows GOARCH=amd64 go test -c -o "$${TMPDIR:-/tmp}/herdr-dots-wintest/" ./...
 link: build
 	herdr --session dots-test plugin link .
```

Check that `.gitattributes` changes no tracked file (everything is LF already):

```bash
git add .gitattributes && git add --renormalize . && git status --short
```

Expected: only the files of this task are staged or modified.

- [ ] **Step 6: Run the tests and the cross build**

Run: `GOTOOLCHAIN=local go test -race ./cmd/herdr-dots/ && GOTOOLCHAIN=local make cross && ruby -e 'require "yaml"; YAML.load_file(".github/workflows/ci.yml"); puts "ci.yml parses"'`
Expected: PASS (`TestManifestDeclaresEveryPlatformAndOneBuildEach`, `TestRuntimeCommandsAreSharedByEveryPlatform`, `TestWindowsBuildScriptBuildsTheExe`, `TestVersionIsPhaseSix`, Phase 3's manifest test); `make cross` ends without error; `ci.yml parses`. (Ruby ships with macOS; if it is missing, `python3 -c 'import sys,yaml; yaml.safe_load(open(".github/workflows/ci.yml"))'` with PyYAML does the same.)

- [ ] **Step 7: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./cmd/herdr-dots/
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./cmd/herdr-dots/
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... 2>&1 | grep '^# herdr-dots' | sort -u
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./... 2>&1 | grep '^# herdr-dots' | sort -u
```

Expected: `gofmt` prints nothing; the next five succeed; the last two print nothing.

- [ ] **Step 8: Commit (never push)**

```bash
git add herdr-plugin.toml internal/version/version.go Makefile scripts/build.ps1 .github/workflows/ci.yml .gitattributes cmd/herdr-dots/platform_manifest_test.go cmd/herdr-dots/memwire_test.go
git commit -m "build: Windows platform, build.ps1, three-OS CI matrix, version 0.6.0 (Phase 6)"
```

---

### Task 13: README, roadmap and notes (still not done)

**Depends on:** all (Wave 4).

**Files:**
- Modify: `README.md`, `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, `docs/superpowers/notes/2026-10-08-phase-6-verification.md`

**Interfaces:**
- Consumes: the facts above (Plan decisions 2, 16, 17, 23; the user gates in the notes).
- Produces: user-facing docs that say Windows is supported but **not run-verified here**, the product is **not done**, and Phase 2 stays **VITAL**.

- [ ] **Step 1: README status, requirements and install**

In `README.md` (post-Phase-5 form; Phases 2–5 rewrote parts of it, so anchor on the headings):

1. Replace the status blockquote at the top (the three lines starting `> **Status: scaffold (v0.5.0).**`) with:

```markdown
> **Status: scaffold (v0.6.0).** Phases 1 (core loop), 2 (dot memory, VITAL), 3 (council), 4 (API and
> Ollama backends, cost), 5 (Dots MCP server) and 6 (Windows support) are in. Windows support is
> cross-compiled and unit-tested, but it has **not been run on Windows** by the developer yet (see
> [Windows (beta)](#windows-beta)). The project is not done and not ready for general use; see the
> [Roadmap](#roadmap) for what's missing.
```

2. Under `## Requirements`, replace the Herdr bullet (`- Herdr ≥ 0.8.0 on macOS or Linux`) with:

```markdown
- Herdr ≥ 0.8.0 on macOS or Linux, or Herdr ≥ 0.9.1 (the Windows beta, preview channel) on
  Windows 10 1809+ or Windows 11
```

3. Under `## Install`, after the existing shell block (the one ending `herdr plugin link .        # development install`), add:

````markdown
On Windows (PowerShell, from a clone of this repo):

```powershell
herdr plugin link .        # runs scripts\build.ps1, which builds bin\herdr-dots.exe
```

If your machine's policy blocks PowerShell scripts, build by hand with
`go build -o bin\herdr-dots.exe .\cmd\herdr-dots` and link again.
````

- [ ] **Step 2: README Windows section**

Add this section directly before `## Privacy and safety`:

```markdown
## Windows (beta)

Herdr Dots runs on the Herdr Windows beta with the same features as on macOS and Linux.

- **Not run-verified yet.** Every Windows code path is compiled and unit-tested on macOS, and
  `.github/workflows/ci.yml` runs the full test suite on `windows-latest`, but the developer has
  not run Herdr Dots on a Windows machine. The checklist in
  `docs/superpowers/notes/2026-10-08-phase-6-verification.md` ("User gates") lists what to try first.
- **Engine IPC** is a named pipe, `\\.\pipe\herdr-dots-<session key>`, that only your Windows user
  can open. The engine checks that the pipe really belongs to you before trusting it.
- **Privacy:** `credentials.toml`, memory, settings and session state get an owner-only ACL (you,
  SYSTEM and Administrators). A credentials file that other users can read is refused with an
  `icacls` command that fixes it.
- **Dot processes** run in a Job Object, so a timeout or an engine crash ends the CLI and
  everything it started (node, git).
- **`claude.cmd` / `codex.cmd`** (npm installs) work: their command lines are quoted for cmd.exe
  and the system prompt goes through a private temp file. A native `claude.exe` is preferred when
  both are on `PATH`. A path containing `%`, `"` or `!` can't be passed to a `.cmd` program; such a
  run fails with a message saying so.
- **Keybindings:** `setup-keys` edits `%APPDATA%\herdr\config.toml` (or `HERDR_CONFIG_PATH`).
- **Glyphs:** if the dot symbols show as boxes, set `HERDR_DOTS_ASCII=1` for ASCII stand-ins.
- Herdr's own Windows limits apply (no direct terminal attach, no live server handoff).
```

- [ ] **Step 3: README known gaps and roadmap**

Under `## Known gaps in this scaffold`, delete the bullet `- No Windows support.`, shorten the bullet that ends `no access for agents outside Herdr; MCP on Windows` / `comes with Phase 6.` so it ends `no access for agents outside Herdr.`, and add:

```markdown
- Windows support (0.6.0) has not been run on Windows by the developer; it is cross-compiled and
  unit-tested only. The three-OS CI workflow is written but not yet pushed anywhere.
```

In the `## Roadmap` table, replace the row `| 6–10 | Windows, write-capable dots, proactive dots, profiles and sharing, then hardening and release. The project is feature complete only after Phase 10. |` with these two rows:

```markdown
| 6 | Windows support: named pipes, Job Objects, ACL privacy, `.cmd` shims (in this scaffold since 0.6.0; built and unit-tested, **not run on Windows yet**) |
| 7–10 | Write-capable dots, proactive dots, profiles and sharing, then hardening and release. The project is feature complete only after Phase 10. |
```

Keep the Phase 2 row exactly as it is (**VITAL**), and keep every later phase listed as not built.

- [ ] **Step 4: Roadmap spec and notes**

In `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, directly under the heading `### Phase 6: Windows support`, add:

```markdown
**Status (0.6.0):** implemented. Cross-compiled for Windows and Linux and unit-tested on macOS; Windows-only tests compile but have not run. A real Windows run (CI on `windows-latest` and the Herdr Windows beta) is a user gate. The product is not done.
```

Append to `docs/superpowers/notes/2026-10-08-phase-6-verification.md`:

```markdown
## Phase 6 result

- Version 0.6.0. `make cross` and `go test -race ./...` (darwin) pass; `GOOS=windows go vet ./...` and `go build ./...` are clean.
- Not run on Windows. Open user gates: every box in "User gates" above.
- The portability guard (`internal/platform/portability_test.go`) keeps Phases 7–9 from adding Unix-only code.
```

- [ ] **Step 5: Check the wording**

Run:

```bash
grep -n 'VITAL' README.md
grep -n 'not been run on' README.md
grep -nE 'is (done|finished|complete|ready)|feature complete|production[- ]ready' README.md docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md | grep -v 'only when\|only after\|until\|not \|→ feature complete\|in "feature complete"'
sh scripts/build.sh && ./bin/herdr-dots version
```

Expected (replayed): two `VITAL` lines (the status blockquote and the Phase 2 roadmap row); exactly two `not been run on` lines (the status blockquote's `> cross-compiled and unit-tested, but it has **not been run on Windows** by the developer yet (see` and the known-gaps bullet `- Windows support (0.6.0) has not been run on Windows by the developer; it is cross-compiled and`); the third command prints nothing (the existing "feature complete only after Phase 10" lines and the roadmap's Phase 10 lines are excluded on purpose); the last prints `herdr-dots 0.6.0 (scaffold)`.

- [ ] **Step 6: Cross-OS gate**

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./...
mkdir -p "${TMPDIR:-/tmp}/p6-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p6-win/" ./...
```

Expected: `gofmt` prints nothing and every command succeeds.

- [ ] **Step 7: Commit**

```bash
git add README.md docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md docs/superpowers/notes/2026-10-08-phase-6-verification.md
git commit -m "docs: Windows beta support in README and roadmap; not run on Windows yet (Phase 6)"
```

---

## Self-review

**1. Spec coverage.**
- §1 success criteria 1 (install/link on Windows, `platforms` with `"windows"`): Task 12 (manifest, `build.ps1`), user gate in Task 1. 2 (every feature on Windows): engine lock and detached start (Tasks 4, 6), IPC (6), panes and popups (6, 12), transcripts (7), CLI backends (2, 9), API backends and credentials (8), memory (4, 7), council (no OS-specific code; covered by the guard and Task 11), MCP server and installers (6, 10). 3 (no Unix regression, same suite on three OSes): byte-identical Unix code paths in every task, Task 11, CI in Task 12. 4 (private to the user): Tasks 3, 6, 7, 8.
- §3 decisions: W1 Tasks 2–3 and the guard (10); W2 Task 6 (`dialSocket`, refined by V1); W3 Task 6 (+ owner check); W4 Task 4; W5 Task 2 / Task 6 (Plan decision 8); W6 Task 2; W7 Task 9 (Plan decision 4); W8 Tasks 2 and 9; W9 Task 2; W10 Tasks 3, 6, 7, 8; W11 Task 12; W12 Tasks 2 and 7 (`ClaudeProjectKey` dropped, Plan decision 15); W13 Tasks 7 and 8; W14 Task 12.
- §4.2 interface changes: `herdr.SocketClient` dial (6), `ipc.Listen`/`Dial` (6), `EngineEndpoint` (6), `lockfile` (4), `ExecRunner` (2), `CleanEnv` (2), `ClaudeCLI.BuildCmd` + `Cmd.Cleanup` (2, 9), `ensureEngine` (6), transcripts and `keys.ConfigPath` (7), credentials (8; `CheckPrivateFile` on the opened file), mcpinstall (10), memory/toolloop `DevNull` (7, 8), manifest and `build.ps1` (12). Found in the merged code by the reconcile and covered: Phase 4's `ECONNREFUSED` checks and httpx sentinel (8), the jail's drive-root refusal and `gitEnv` (8), Phase 5's `engineEnv` (6), codex config lookup, `setup-mcp` children and test sockets (10), Phase 2's `sh -c` memory editor (10), Phase 4–5 tests that assumed Unix (8, 10, 11), and Phase 5's version pin (12).
- §7 UI: Task 5 (ASCII switch); `ctrl+w` dropped with evidence (Plan decision 16).
- §8.1 errors: unreachable socket (v1 30 s exit, unchanged), pipe collision (Tasks 3, 6), over-long `.cmd` line (Tasks 2, 9), ACL failure (Plan decision 10, Task 7). §8.3 tests: per-OS path tables (2), `CleanEnv` (2), prompt file (9), pipe and SDDL (3, 6), `LockFileEx` across processes (4), Job Object kills `cmd /c ping` (2), detach survives its parent (2), pipe-mode fake Herdr (6), CI (12), manual run (Task 1 user gates).
- §9 V1–V8: Task 1 and "Confirmed while planning". §10 out of scope respected.

**2. Placeholder scan.** No "TBD", "TODO" or "similar to Task N". Task 1's notes contain two "(paste ... here)" slots for command output the executor records; they are recording steps, not missing design (the expected output is given in Task 1 Step 2). Task 10 carries concrete diffs for every Phase 2/5 port (reconciled); the guard confirms nothing is left, and its table of replacements stays only for code added after `a47427d`.

**3. Type consistency.** `platform.StartDetached` returns `(*os.Process, error)` everywhere (Tasks 2, 6). `platform.Command`/`CommandContext` take `args []string` (Tasks 2, 10). `Cmd.Close()` and `Preflight(err)` (Task 2) are what Task 9 calls. `EngineEndpoint()` (Task 6) is what Task 10's table and the guard name. `platformtest.Abs`/`AssertPrivateFile`/`AssertPrivateDir` (Task 3) match their uses in Tasks 6, 7, 8, 9, 11. `ErrInsecurePerms` keeps its name; `platform.FixPrivateHint` is used in Task 8 and its tests.

**4. Review Focus.** Each of the five items names its tests and owning tasks (2, 3, 6, 7, 8, 9). Further inputs found while planning got tests too: `.ENV` and NTFS stream tricks against the tool jail (Task 8), a reader holding a file during an atomic replace (Task 7), two engines on one machine (Task 6), and a cancelled context for shim commands (Task 2). Added by the reconcile: a credentials file whose DACL is checked through the open handle (`TestCheckPrivateFileReadsTheHandlesDACL`, Task 3), a refused dial keeping its identity after httpx scrubbing (`platform.ConnRefused` in httpx's test, Task 8), an editor command line with flags and spaces (`TestEditorCommandRunsTheEditorLineOnTheFile`, Task 10).

**5. Reconcile replay (2026-10-09).** All 13 tasks were replayed in wave order on scratch worktrees of `5863171` and `a47427d`; every code block in this plan is the replayed content. Final state: darwin `go test -race ./...`, `go vet ./...` and `gofmt -l internal cmd` clean; `GOOS=linux go build ./...`, `GOOS=windows go build ./...` and `GOOS=windows go vet ./...` clean; `GOOS=windows go test -c ./...` compiles every test binary; `make cross` passes. Nothing ran on Windows: every Windows-only behavior is a user gate in Task 1's notes.

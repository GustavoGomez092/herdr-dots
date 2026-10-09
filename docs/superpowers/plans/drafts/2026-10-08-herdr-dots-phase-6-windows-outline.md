# Herdr Dots Phase 6: Windows Support, Implementation Plan Outline

**Status: DRAFT, pending user review**

> **This is an outline, not an executable plan.** Earlier phases' code doesn't exist yet. When Phase 6 starts, write the full step-by-step plan with superpowers:writing-plans against the real code and the Herdr Windows beta, then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Run every existing Herdr Dots feature natively on the Herdr Windows beta without regressing macOS or Linux.

**Architecture:** OS differences move behind build-tagged files and a new `internal/platform` package: named pipes for Herdr and engine IPC, `LockFileEx` for locks, Job Objects for process trees, DACLs for privacy, and Windows path rules for transcripts and client configs. Long system prompts leave argv on every OS.

**Tech Stack:** Go 1.25; new dependencies `github.com/Microsoft/go-winio`, `golang.org/x/sys/windows`; GitHub Actions matrix (macOS, Ubuntu, Windows).

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-6-windows-design.md`

## Global Constraints

- The product is **not** done after this phase. Version `0.6.0`. README roadmap keeps Phase 2 **VITAL**.
- Platforms: macOS, Linux, Windows 10 1809+ / 11. `platforms = ["macos", "linux", "windows"]`.
- No `runtime.GOOS` branches in business logic. Use `_unix.go`/`_windows.go` files or `internal/platform`.
- Unix behavior must stay byte-identical (same socket paths, same lock files, same argv except W7).
- Windows privacy: DACL owner-only (plus SYSTEM and Administrators) for credentials, memory and session state; never request elevation.
- Every child process on Windows runs in a Job Object with `KILL_ON_JOB_CLOSE`.
- The CI matrix runs `go test -race ./...` and `go vet ./...` on all three OSes before merge.

## Review Focus

1. **A path with spaces and non-ASCII characters** (`C:\Users\José Núñez\src\my app`). Expect transcripts found, git commands working, memory project resolved, MCP install args quoted correctly. (Tasks 2, 6, 7)
2. **`claude.cmd` instead of `claude.exe`.** Expect the argument quoting safe (no injection through purpose text) and an over-long command line refused pre-flight with a hint. (Task 5)
3. **A timeout with grandchildren** (node under a shim). Expect the whole tree dead within 2 s. (Task 4)
4. **Two Herdr sessions of the same user.** Expect two engines with distinct pipes, and no cross-talk. (Task 3)
5. **A credentials file copied from another machine with inherited permissive ACLs.** Expect `CheckPrivate` to refuse it with a fix-it hint. (Task 2)

---

## File Structure

```
internal/platform/platform.go            shared API
internal/platform/platform_unix.go       Unix implementations
internal/platform/platform_windows.go    Windows implementations (paths, Detach, ACL, DevNull)
internal/platform/jobs_windows.go        Job Object helpers
internal/herdr/dial_unix.go, dial_windows.go
internal/ipc/endpoint_unix.go, endpoint_windows.go   Listen, Dial
internal/lockfile/lockfile_unix.go, lockfile_windows.go
internal/backend/exec_unix.go, exec_windows.go       process group vs job kill
internal/backend/exec.go                 CleanEnv case rule, Cmd.Cleanup
internal/backend/claude.go               system prompt file (W7)
internal/paths/paths.go                  EngineEndpoint
internal/transcript/claude.go, codex.go  platform path helpers
internal/keys/keys.go                    platform.HerdrConfigPath
internal/credentials/perms_unix.go, perms_windows.go
internal/mcpinstall/install.go           .exe, config locations
internal/memory/project.go, internal/backend/toolloop/tools.go  DevNull
internal/ui/style/style.go               ASCII fallback set
internal/herdr/herdrtest/fake.go         pipe mode on Windows
cmd/herdr-dots/engine.go                 platform.Detach
scripts/build.ps1
herdr-plugin.toml
.github/workflows/ci.yml
```

---

## Tasks

### Task 1: Verification spike on the Herdr Windows beta (V1–V8)

**Files:** Create `docs/superpowers/notes/phase-6-verification.md`; capture a redacted Windows Claude transcript fixture `internal/transcript/testdata/claude_windows.jsonl`.
**Interfaces:** Findings only.
**Tests:** None. Each V-question answered with evidence and "assumption holds: yes/no". STOP on any "no" that changes a W-decision.

### Task 2: `internal/platform`

**Files:** Create the `internal/platform/*` files. Modify `internal/credentials` (perms split), `internal/memory/project.go`, `internal/backend/toolloop/tools.go`. Test: `internal/platform/platform_test.go`, `platform_windows_test.go`.
**Interfaces:**
- Produces: `Home() string`; `const DevNull string` (per OS); `HerdrConfigPath(getenv func(string) string, home string) string`; `ClaudeProjectsDir(home string) string`; `ClaudeProjectKey(cwd string) string`; `CodexSessionsDir(home string) string`; `ClaudeUserConfig(home string) string`; `CodexConfig(home string) string`; `Detach(c *exec.Cmd)`; `SecureDir(path string) error`; `SecureFile(path string) error`; `CheckPrivate(path string) error`; `var ErrNotPrivate`.
**Tests:** per-OS path tables (including spaces and non-ASCII); `ClaudeProjectKey` matches the V3 fixture; Windows: `SecureFile` produces an owner-only DACL (read back with `GetNamedSecurityInfo`); `CheckPrivate` refuses a file with an Everyone ACE.

### Task 3: Transports: Herdr dial, engine endpoint, locks

**Files:** Create `internal/herdr/dial_*.go`, `internal/ipc/endpoint_*.go`, `internal/lockfile/lockfile_*.go`. Modify `internal/paths/paths.go`, `internal/ipc/ipc.go`, `internal/herdr/herdrtest/fake.go`. Test: `internal/ipc/endpoint_test.go`, `internal/lockfile/lockfile_test.go` (both OS), `internal/herdr/client_test.go` (pipe mode on Windows).
**Interfaces:**
- Produces: `ipc.Listen(endpoint string) (net.Listener, error)`; `ipc.Dial(ctx context.Context, endpoint string) (net.Conn, error)`; `(paths.Paths) EngineEndpoint() string`; Windows `lockfile` implementations with unchanged signatures; `herdrtest.New` serves a named pipe on Windows.
**Tests:** listen/dial round trip on each OS; two endpoints for two session keys; the lock is exclusive across two processes (re-exec test) on each OS; the full v1 engine integration suite passes on Windows via the pipe-mode fake.

### Task 4: Process management

**Files:** Create `internal/backend/exec_unix.go`, `exec_windows.go`, `internal/platform/jobs_windows.go`. Modify `internal/backend/exec.go`, `cmd/herdr-dots/engine.go`. Test: `internal/backend/exec_test.go` (extend), `exec_windows_test.go`.
**Interfaces:**
- Consumes: `platform.Detach`.
- Produces: `ExecRunner.Run` kills the process tree on both OSes; `CleanEnv` case-insensitive on Windows; `Cmd.Cleanup func()` always called after the run.
**Tests:** Windows: timeout kills `cmd /c "ping -t 127.0.0.1"` and its child within 2 s; Unix test unchanged; `CleanEnv` drops `Herdr_Pane_Id` on Windows and keeps it on Unix; `Cleanup` called on success, error and timeout; the detached engine keeps running after the parent exits (Windows test).

### Task 5: CLI backends on Windows (W7, W8)

**Files:** Modify `internal/backend/claude.go`, `internal/backend/codex.go`, `internal/backend/exec.go`. Test: extend `claude_test.go`, `codex_test.go`.
**Interfaces:**
- Produces: `ClaudeCLI.BuildCmd` writes the system prompt to a private temp file above 4,000 characters (or always on Windows) and passes `--append-system-prompt-file` (or a stdin preamble per V4), setting `Cmd.Cleanup`; `backend.CmdLineLength(c Cmd) int`; `var ErrCmdTooLong`.
**Tests:** a short prompt on Unix keeps v1 argv exactly (golden); a long prompt switches to a file and the file is `0600`/owner-ACL and removed after the run; a `.cmd` resolution over 8,191 characters → `ErrCmdTooLong` with a hint; purpose text containing `"&|<>^%` doesn't break quoting (a Windows test echoes argv back through a fake `.cmd`).

### Task 6: Paths for transcripts, keys and installers

**Files:** Modify `internal/transcript/claude.go`, `codex.go`, `internal/keys/keys.go`, `internal/mcpinstall/install.go`. Test: extend the respective tests with Windows fixtures.
**Interfaces:**
- Consumes: `platform` helpers.
- Produces: no new exported symbols; the existing `ResolveClaudePath`, `ResolveCodexPath`, `ConfigPath`, `PlanInstall` behave per OS.
**Tests:** a Windows transcript fixture parses with the same turns as the Unix fixture; `ResolveClaudePath` on Windows paths; `keys.ConfigPath` per V5; the MCP install plan uses `herdr-dots.exe` and quoted Windows paths.

### Task 7: Manifest, build script, UI fallback, CI

**Files:** Modify `herdr-plugin.toml`, `internal/ui/style/style.go`, `README.md`, `internal/version/version.go` (`0.6.0`). Create `scripts/build.ps1`, `.github/workflows/ci.yml`. Test: manifest parse test; `style` ASCII fallback test.
**Interfaces:**
- Produces: manifest with `windows` and per-platform commands (V2 syntax); `style.ASCII() bool`; CI workflow with a three-OS matrix.
**Tests:** CI green on all three OSes; a manual smoke test on the Herdr Windows beta following the v1 E2E checklist plus council, MCP ask and the memory screen, recorded in the notes; README Windows section (beta caveats); Phase 2 still **VITAL**; no "done/ready".

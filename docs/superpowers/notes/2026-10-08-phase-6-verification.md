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
# herdr-dots/internal/backend
# herdr-dots/internal/backend/toolloop
# herdr-dots/internal/lockfile
```

Packages failing `GOOS=windows GOARCH=amd64 go vet ./...`:

```
# herdr-dots/internal/backend
# herdr-dots/internal/backend [herdr-dots/internal/credentials.test]
# herdr-dots/internal/backend/toolloop
# herdr-dots/internal/lockfile
```

Compile errors behind them:

```
internal/lockfile/lockfile.go:21:20: undefined: syscall.Flock
internal/lockfile/lockfile.go:21:47: undefined: syscall.LOCK_EX
internal/lockfile/lockfile.go:21:63: undefined: syscall.LOCK_NB
internal/lockfile/lockfile.go:34:14: undefined: syscall.Flock
internal/lockfile/lockfile.go:34:43: undefined: syscall.LOCK_UN
internal/backend/toolloop/jail.go:286:49: undefined: syscall.O_NOFOLLOW
internal/backend/exec.go:38:41: unknown field Setpgid in struct literal of type syscall.SysProcAttr
internal/backend/exec.go:48:15: undefined: syscall.Kill
```

Phase 5 packages among them (Plan decision 21): none at `c657a18` (replayed; same result as `a47427d`); list any that a later commit adds.

Compiles but behaves wrongly on Windows (ported in this phase): `internal/credentials` (mode bits refuse every file; Task 8), Phase 5's `mcpinstall` config paths, `setup-mcp` child commands, the codex run's `HOME` lookup (Task 10), Phase 2's `memory edit` (`sh -c`; Task 10), and every `syscall.ECONNREFUSED` check (Task 8).

## User gates: what only a real Windows machine can confirm

Run on Windows 10 1809+ or Windows 11 with the Herdr Windows beta (preview channel, Herdr >= 0.9.1), Go >= 1.25 and Git for Windows. Each item is **not live-verified (no Windows machine) — user gate**.

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

## Phase 5 port (Task 10)

- Guard findings before the port: `cmd/herdr-dots/memory.go` (`sh -c` editor, Phase 2), `internal/backend/codex_mcp.go` (`HOME`, `.codex`), `internal/mcpinstall/install.go` (`.claude.json`, `.codex`). All fixed; the guard passes.
- Ported without a guard finding: `setup-mcp` children through `platform.Command` / `platform.LookPath`; `mcp.go` and its test on `EngineEndpoint` (Task 6); Phase 5's IPC, MCP and engine tests on `ipc.Listen` endpoints.
- Carry-in: `internal/backend/toolloop/tools.go` `gitFiles` runs through `platform.CommandContext` (a dedicated guard test, `TestToolloopListingChildGoesThroughPlatform`, forbids `exec.Command*` there). `internal/memory/project.go` still calls `exec.CommandContext(ctx, "git", ...)` directly (not a guard finding; not in this task).
- Not run on Windows: `claude mcp add` through npm's `claude.cmd`, `codex mcp list` through `codex.cmd`, the detached engine started by the MCP server, and `memory edit` through cmd.exe are user gates.

## Phase 6 result

- Version 0.6.0. `make cross` and `go test -race ./...` (darwin) pass; `GOOS=windows go vet ./...` and `go build ./...` are clean.
- Not run on Windows. Open user gates: every box in "User gates" above.
- The portability guard (`internal/platform/portability_test.go`) keeps Phases 7-9 from adding Unix-only code.

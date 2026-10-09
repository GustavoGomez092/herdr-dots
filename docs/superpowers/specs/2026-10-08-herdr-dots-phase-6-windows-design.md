# Herdr Dots Phase 6: Windows Support, Design Spec

**Status: DRAFT, pending user review**

- **Date:** 2026-10-08
- **Phase:** 6 of 10 (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first.
- **Builds on:** every earlier phase (it ports what exists): v1 `herdr` client, `ipc`, `lockfile`, `backend` exec runner, `paths`, `transcript` readers, `keys`, the manifest; Phase 2 memory locks; Phase 4 credential permissions; Phase 5 MCP install paths
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-6-windows-outline.md`
- **The product is not done after this phase.** Ships as `0.6.0`.

---

## 1. Intent

### What the user wants

Herdr has a Windows beta. Herdr Dots should run there with the same features as on macOS and Linux, not as a degraded port. Doing this before the write-capable, proactive and sharing phases means those phases are built cross-platform from the start instead of being ported later.

### Success criteria

1. `herdr plugin install` / `link` builds and runs Herdr Dots on Windows 10 (1809+) and Windows 11 under the Herdr Windows beta, with `platforms` including `"windows"`.
2. Every v1–Phase 5 feature works on Windows: the engine single-instance lock and detached start, IPC, the Dots pane and popups, transcripts for Claude Code and Codex, the CLI backends, API backends, memory, council, the MCP server and both installers.
3. No Unix behavior regresses. The same test suite passes on macOS, Linux and Windows CI.
4. Secrets and state files are private to the current Windows user (ACLs instead of `0600`).

---

## 2. Research summary (looked up 2026-10-08)

- **Herdr Windows beta** (herdr.dev Windows-beta docs): native Windows build using **ConPTY**, installed with PowerShell (`irm https://herdr.dev/install.ps1 | iex`). Stable and preview channels. Shells: PowerShell and cmd.exe. Agent integrations on Windows include Claude Code and Codex. Unsupported on Windows: direct terminal attach, live server handoff, Unix file-descriptor handoff. **Plugins can declare `windows` as a manifest platform (preview), and Windows-specific command alternatives are required.** The docs I could reach don't say how the socket API is exposed on Windows (named pipe or AF_UNIX) or the exact manifest syntax for per-platform commands. These are verification questions V1 and V2.
- **Go on Windows:** `net` supports AF_UNIX on Windows 10 1803+; named pipes need `github.com/Microsoft/go-winio`; `flock` maps to `LockFileEx` (`golang.org/x/sys/windows`); process-tree kill needs **Job Objects**; detached processes need `CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS | CREATE_NO_WINDOW`. `exec.LookPath` honors **PATHEXT**. Running `.cmd`/`.bat` shims goes through `cmd.exe` with stricter argument quoting and an **8,191-character command-line limit**.
- **v1 hazards found in the plan:** `ClaudeCLI.BuildCmd` passes the whole system prompt as one `--append-system-prompt` argument (it grows with purpose text; on Windows through a `.cmd` shim it can exceed 8,191 characters); `CleanEnv` matches `HERDR_*` case-sensitively (Windows variable names are case-insensitive); `ExecRunner` kills a Unix process group; `lockfile` uses `syscall.Flock`; `paths.EngineSock` assumes Unix socket paths; `Ensure` relies on `0700`.

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| W1 | Platform code layout | Build-tagged files (`_unix.go` / `_windows.go`) behind small internal interfaces in the existing packages. No `runtime.GOOS` branches in business logic | One `if windows` sprinkled everywhere |
| W2 | Herdr socket dial | `herdr.dial(path)`: on Windows, paths starting with `\\.\pipe\` use `winio.DialPipeContext`; anything else uses AF_UNIX. The real form is settled by V1 | Assuming AF_UNIX |
| W3 | Engine IPC endpoint | Windows: named pipe `\\.\pipe\herdr-dots-<sessionkey>` with a security descriptor granting only the current user SID (`D:P(A;;GA;;;<SID>)`). Unix unchanged | AF_UNIX on Windows (path quirks, no per-user ACL by default) |
| W4 | Single-instance lock | `LockFileEx` exclusive, fail-immediately, on `engine.lock` (same file name, same semantics as `TryLock`, `Acquire`, `Held`) | Named mutex (different lifetime semantics) |
| W5 | Detached engine start | `SysProcAttr{CreationFlags: CREATE_NEW_PROCESS_GROUP \| DETACHED_PROCESS \| CREATE_NO_WINDOW, HideWindow: true}`; stdio to `engine.log` | A Windows service (needs admin; violates "no daemon outside Herdr") |
| W6 | Process-tree kill on timeout | Each child is assigned to a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`; on timeout the job is terminated | `Process.Kill` only (orphans grandchildren such as node under a `.cmd` shim) |
| W7 | Long prompts as arguments | **All platforms:** the system prompt moves off argv when it exceeds 4,000 characters, or always on Windows: `--append-system-prompt-file <tmpfile>` if V4 confirms the flag; otherwise the system prompt is prepended to stdin under a `SYSTEM INSTRUCTIONS` header. Temp files are `0600`/user-ACL and deleted after the run | Hoping prompts stay short |
| W8 | CLI binary resolution | `exec.LookPath` (PATHEXT). Prefer `.exe` over `.cmd` when both exist. With a `.cmd` shim, enforce the 8,191 limit pre-flight and fail with a clear hint if exceeded | Hard-coding `claude.exe` |
| W9 | Environment cleaning | `CleanEnv` compares names case-insensitively on Windows (`strings.EqualFold` prefix) | Case-sensitive match |
| W10 | File privacy | Windows: state and config dirs under the user profile; on creation, set an explicit DACL (owner full control, inheritance off) for `credentials.toml`, `memory/` and the session dir. Phase 4's "refuse if group/world-readable" becomes "refuse if the DACL grants anyone but the owner, SYSTEM and Administrators" | Ignoring ACLs (credentials exposed on shared machines) |
| W11 | Manifest | `platforms = ["macos", "linux", "windows"]`; per-platform commands using whatever syntax V2 confirms; build via `scripts/build.ps1` on Windows | Requiring WSL |
| W12 | Paths | Transcript locations, the Herdr `config.toml` path (setup-keys), Claude Code / Codex MCP config locations: all resolved through one `internal/platform` package with Windows rules confirmed by V3, V5 and V6 | Scattered `os.Getenv("HOME")` |
| W13 | Git | Git commands (memory project identity, tool loop file listing) use `-c core.hooksPath=NUL` on Windows, `/dev/null` elsewhere, via `platform.DevNull` | Literal `/dev/null` |
| W14 | CI | GitHub Actions matrix: `macos-latest`, `ubuntu-latest`, `windows-latest`; `go test -race ./...` on all three | Manual Windows testing only |

---

## 4. Architecture

### 4.1 New package

`internal/platform` (new) centralizes OS differences:

- `platform.Home() string`, `platform.DevNull` (`/dev/null` or `NUL`)
- `platform.HerdrConfigPath(getenv) string` (setup-keys)
- `platform.ClaudeProjectsDir(home) string`, `platform.ClaudeProjectKey(cwd string) string` (how Claude encodes a cwd into a directory name on each OS, per V3), `platform.CodexSessionsDir(home) string`
- `platform.ClaudeUserConfig(home) string`, `platform.CodexConfig(home) string` (Phase 5 installer)
- `platform.Detach(cmd *exec.Cmd)`, `platform.KillTree` helpers (Job Object or process group)
- `platform.SecureDir(path) error`, `platform.SecureFile(path) error`, `platform.CheckPrivate(path) error`

### 4.2 Changes to earlier interfaces (exact)

| Symbol | Change |
|---|---|
| `herdr.SocketClient` | Internal dialing goes through `dial(ctx, path) (net.Conn, error)` (build-tagged). No exported change |
| `ipc.Serve` | Unchanged signature (takes a `net.Listener`). New `ipc.Listen(endpoint string) (net.Listener, error)` and `ipc.Dial(ctx, endpoint string) (net.Conn, error)` (build-tagged); `ipc.Client` uses `Dial` internally |
| `paths.Paths.EngineSock()` | Renamed concept **endpoint**: new `EngineEndpoint() string` returns the socket path on Unix and `\\.\pipe\herdr-dots-<key>` on Windows. `EngineSock()` stays as a deprecated alias on Unix |
| `lockfile` | `TryLock`, `Acquire`, `Held`, `Release` get Windows implementations (`LockFileEx`). Signatures unchanged |
| `backend.ExecRunner` | Unix: process group (v1). Windows: Job Object kill. Signature unchanged |
| `backend.CleanEnv` | Case-insensitive on Windows |
| `backend.ClaudeCLI.BuildCmd` | W7: when `len(SystemPrompt(r)) > 4000 \|\| runtime.GOOS == "windows"`, write the prompt to a temp file and pass `--append-system-prompt-file` (V4); `Cmd` gains `Cleanup func()` so the runner deletes the file after the run |
| `backend.Cmd` | Add `Cleanup func()` (nil-safe) |
| `cmd/herdr-dots` `ensureEngine` | Uses `platform.Detach` |
| `transcript.ResolveClaudePath`, `ResolveCodexPath` | Use `platform` helpers for directory roots and path encoding |
| `keys.ConfigPath` | Uses `platform.HerdrConfigPath` |
| `credentials` (Phase 4) | `CheckPrivate` replaces the mode-bits check |
| `mcpinstall` (Phase 5) | Binary path with `.exe`; config paths from `platform` |
| `memory` (Phase 2), `toolloop` (Phase 4) | `platform.DevNull` for `core.hooksPath` |
| Manifest | `platforms` adds `"windows"`; Windows commands per V2 |
| `scripts/build.ps1` (new) | `go build -o bin\herdr-dots.exe .\cmd\herdr-dots` |

---

## 5. Data model

No data format changes. Paths inside state files (`attachments.json` `cwd`, memory `meta.json` roots) are stored as the OS gives them. Memory project ids for git repos are unaffected (root commit hash). Path-identity projects differ by OS, which is expected.

---

## 6. Data flow

Unchanged from earlier phases. Only the transport (pipe vs socket), process management (job vs group) and file security (DACL vs mode bits) differ, behind the interfaces in §4.

---

## 7. UI

- Bubble Tea runs under Herdr's ConPTY panes. Glyphs (`◆ ▲ ✎ ⊕ ◌`) need a font with those code points. If V7 shows tofu in Windows Terminal's default font, the `style` package gains an ASCII fallback set (`*`, `^`, `~`, `+`, `o`), selected by `HERDR_DOTS_ASCII=1` or automatically when `TERM_PROGRAM` is unknown on Windows.
- Key handling: `ctrl+s` may be intercepted by some Windows terminals (XOFF is not an issue on ConPTY, but check V7). The editor also accepts `ctrl+w` for save as a fallback.

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Behavior |
|---|---|
| Herdr socket form unknown or unreachable | Clear error naming the endpoint tried; exits after 30 s like on Unix |
| Pipe name collision (another user) | Impossible by ACL; another session of the same user → the lock prevents a second engine |
| `.cmd` shim with an over-long command line | Pre-flight error: "command line too long for claude.cmd (9,214 > 8,191). Install the native claude.exe or shorten the purpose" |
| ACL setup fails | Refuse to write credentials; log a warning for other state |

### 8.2 Safety

- Named pipe and DACLs restrict IPC and files to the current user.
- Job Objects guarantee dot processes die on timeout or engine exit (`KILL_ON_JOB_CLOSE` also covers engine crashes).
- No elevated privileges are ever requested.

### 8.3 Testing

| Layer | Tests |
|---|---|
| Unit (all OSes) | `platform` path helpers with table tests per OS (`GOOS`-specific files); `CleanEnv` case-insensitivity; `BuildCmd` uses a prompt file above the threshold |
| Windows-only (`//go:build windows`) | Pipe listen/dial and ACL (a second user can't be simulated in CI, so assert the security descriptor string); `LockFileEx` exclusive semantics with two processes; the Job Object kills a `cmd /c ping -t` grandchild on timeout; `Detach` survives parent exit |
| Fake Herdr server | `herdrtest` gains a named-pipe mode on Windows so the full engine integration suite runs there |
| CI | Matrix on all three OSes, `-race` |
| Manual | Herdr Windows beta (preview channel): install the plugin, attach a Claude Code dot, card → send, council, MCP ask, memory screen |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | How is Herdr's socket API exposed on Windows (named pipe path? AF_UNIX path?), and what's in `HERDR_SOCKET_PATH`? | W2 |
| V2 | Manifest syntax for Windows-specific commands (`[[build]]`, `[[startup]]`, `[[actions]]`, `[[panes]]`), and whether `./bin/herdr-dots` resolves `.exe` | W11 |
| V3 | Where Claude Code stores transcripts on Windows and how it encodes `C:\Users\…\repo` into the project directory name; whether `pane.get` reports `agent_session` on Windows | W12, transcript |
| V4 | Does `claude -p` support `--append-system-prompt-file` (or `--system-prompt-file` in print mode)? If not, does a stdin preamble work as well? | W7 |
| V5 | Herdr `config.toml` location on Windows (`%APPDATA%\herdr\config.toml`?) and whether `herdr --default-config` works there | setup-keys |
| V6 | Claude Code (`~/.claude.json` equivalent) and Codex (`%USERPROFILE%\.codex\config.toml`?) config locations on Windows; are `claude`/`codex` `.exe` or `.cmd`? | Phase 5 installer, W8 |
| V7 | Rendering of the glyphs and `ctrl+s` behavior inside Herdr's ConPTY panes in Windows Terminal | UI |
| V8 | Do `HERDR_PLUGIN_*` variables and `HERDR_PLUGIN_CONTEXT_JSON` exist with the same names on Windows? | paths, actions |

---

## 10. Out of scope

WSL-specific modes (Herdr running in WSL is just Linux); Windows ARM64 binaries until Phase 10 release engineering; a Windows installer separate from `herdr plugin install`.

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Phase position | (a) Phase 6, before write-capable, proactive and sharing; (b) last, before release | **(a)** | Later phases get built cross-platform once instead of ported |
| O2 | Engine IPC on Windows | (a) named pipe with a user ACL; (b) AF_UNIX | **(a)** | Native, per-user security, no path-length quirks |
| O3 | Long system prompts | (a) move off argv above 4,000 characters on all OSes; (b) Windows only | **(a)** | Linux also caps a single argument at 128 KB; Phase 2 memory and long purposes grow prompts |
| O4 | ASCII glyph fallback | (a) auto on Windows when the font lacks glyphs, plus an env switch; (b) env switch only | **(b)** until V7 shows a problem | Avoid guessing; Windows Terminal's default fonts usually have these glyphs |

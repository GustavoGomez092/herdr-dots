# Herdr Dots Phase 5: Dots MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the main agent (Claude Code or Codex, running in a Herdr pane) consult the pane's dots itself through a stdio MCP server, `herdr-dots mcp`, with the tools `list_dots`, `ask_dot`, `ask_council`, `list_queue`, `read_memory` and `propose_memory`, installed into Claude Code and Codex by `herdr-dots setup-mcp` (preview, confirmation, backup, `--remove`).

**Architecture:** The agent CLI starts `herdr-dots mcp --config-dir C --state-dir S` as a child process inside the pane, so it inherits `HERDR_PANE_ID` and `HERDR_SOCKET_PATH`. The new `internal/mcpserver` package (official Go SDK `github.com/modelcontextprotocol/go-sdk` v1.8.0) builds the tools; dot questions travel over v1's engine socket through a new data-returning IPC call (`ipc.Querier`, `Client.Query`, `Response.Data`), and the engine answers them with a synchronous `Engine.Ask` (a forced run through `runDot` with a result sink: no card), `Engine.AskCouncil` (Phase 3's orchestration with a result callback instead of a card), `ListDots` and `ListQueue`, guarded by per-pane ask limits and a "consulted" flag that makes an asked dot skip the turn it was asked in. Memory tools go straight to `memory.Store` (`List`, new `Propose`). The new `internal/mcpinstall` package plans and applies the installs: Claude Code through `claude mcp add --scope user`, Codex through a line-preserving managed block in `config.toml`.

**Tech Stack:** Go 1.25 (`go 1.25.0` directive since Phase 4), new dependency `github.com/modelcontextprotocol/go-sdk v1.8.0` (MCP 2026-07-28, negotiates down to 2025-11-25 and earlier; pulls `github.com/google/jsonschema-go v0.4.3`, `github.com/segmentio/encoding v0.5.4`, `github.com/yosida95/uritemplate/v3 v3.0.2`, `golang.org/x/oauth2 v0.35.0`, `golang.org/x/time v0.15.0`, `golang.org/x/sync v0.20.0`), existing `github.com/BurntSushi/toml`, Bubble Tea v1, `lipgloss`. `claude` CLI (2.1.295 on the planning machine) for the live checks; Codex is not installed here.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-5-mcp-server-design.md` (approved with the amendments in `docs/superpowers/decisions/2026-10-08-phase-decisions.md`, row 9). This plan replaces the outline `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-5-mcp-server-outline.md`.

**Binding rulings used here (row 9):** `ask_dot` reaches **attached dots only**. `list_queue` is **on** by default, and the README warns that **MCP answers bypass the approval queue**. Spec open decisions O3–O5 take the spec's recommendations: user-scope install, ask limits 3 per turn and 6 per minute, and a dot's `remember` ops apply on asks (Phase 2 rules). The product keeps the name **Herdr Dots** (row 1).

**Builds on:** v1 as merged (real code at HEAD) **plus Phases 2, 3 and 4**, which execute and merge **before** this plan starts. For v1 symbols this plan uses the real code; for later symbols it uses the Interfaces blocks of `docs/superpowers/plans/2026-10-08-herdr-dots-phase-2-dot-memory.md`, `...-phase-3-council-mode.md` and `...-phase-4-api-backends.md`.

**Preconditions (check before Wave 1; stop and report if any fails):**
- Phase 2 is merged: `ls internal/engine/memory.go cmd/herdr-dots/memwire.go internal/memory/store.go` lists all three; `grep -n 'func memoryStoreFor(p paths.Paths, cfg config.Config, logf func(string, ...any)) \*memory.Store' cmd/herdr-dots/memwire.go`; `grep -n 'func (e \*Engine) projectOf(att \*state.Attachment) string' internal/engine/memory.go`; `grep -n 'CmdMemoryChanged' internal/ipc/ipc.go`.
- Phase 3 is merged: `grep -n 'func (e \*Engine) Council(ctx context.Context, paneID, question string, slugs \[\]string) (string, error)' internal/engine/council.go`; `grep -n 'sink    func(backend.Result, error)' internal/engine/engine.go`; `grep -n 'func errText(err error) string\|const councilSlug\|func (e \*Engine) runCouncil(run \*councilRun)' internal/engine/council.go`; `grep -n 'func councilRig\|func member(' internal/engine/council_test.go`.
- Phase 4 is merged: `grep -n 'func OverUSD\|func costDetail\|func withCost' internal/engine/cost.go`; `grep -n 'func (u \*Usage) Cost\|func (u \*Usage) PaneCost' internal/state/usage.go`; `grep -n 'func Redact' internal/credentials/redact.go`; `grep -nE 'CostKnown +bool|Model +string' internal/backend/backend.go`; `grep -n 'MaxUSDPerSession' internal/dots/dot.go`; `grep -n 'SessionBudgetUSD' internal/config/config.go`; `head -3 go.mod` shows `go 1.25.0`.
- `internal/version/version.go` says `0.4.0`.
- `GOTOOLCHAIN=local go test -race ./... && GOTOOLCHAIN=local go vet ./...` passes on the base and `gofmt -l .` prints nothing.
- If a reconciled earlier phase renamed one of the symbols above, use the real name everywhere this plan uses the old one and say so in that task's commit message.

## Global Constraints

Phase 5 constraints:
- **The product is NOT done or ready after Phase 5.** The version becomes `0.5.0` (`internal/version` and `herdr-plugin.toml`). Never call the project done, finished, complete or ready in code, docs, commits or reports. Feature complete is defined only by Phase 10. The README roadmap keeps Phase 2 (dot memory) marked **VITAL**.
- The product name stays **Herdr Dots** (plugin id `herdr-dots`). The MCP server's name is `herdr-dots`, so tools appear as `mcp__herdr-dots__<tool>`.
- Transport is **stdio** only: `herdr-dots mcp --config-dir <abs> --state-dir <abs>`. The server never writes anything to stdout except protocol messages; logs go to `SessionDir/mcp.log` (`StateDir/mcp.log` outside Herdr).
- Every tool is scoped to the server's own `HERDR_PANE_ID`. Without `HERDR_PANE_ID` or `HERDR_SOCKET_PATH` (outside Herdr, or inside a dot's own CLI run, whose environment `backend.CleanEnv` strips), every tool is still listed and returns the error `herdr-dots: not running inside a Herdr pane; dots are unavailable here.` No recursion, no cross-pane access.
- `ask_dot` reaches **attached dots only** (ruling 9). It never creates a queue card, writes a feed line with outcome `asked`, and marks the dot consulted so it skips the turn it was asked in, once.
- Ask limits per pane: `[mcp] max_asks_per_turn = 3` (reset when a turn ends) and `max_asks_per_minute = 6` (sliding window); call and dollar budgets apply too. A refused ask runs nothing and spends nothing.
- `list_queue` is on by default; the README says plainly that MCP answers (`ask_dot`, `ask_council`, `list_queue`) reach the main agent **without the developer's approval**, and that `[mcp] tools` removes any tool.
- Agent memory writes are always **project-scope proposals** (`memory.Store.Propose`, source `agent`), pass the secret scanner, and are at most 500 characters. `read_memory` returns active entries only.
- Every tool result (text plus structured content) stays within 24,000 characters (`mcpserver.DefaultMaxResultChars`). Lists drop items and say so; long answers are clipped with `…`.
- The installer never edits a client config silently: it prints the exact commands and the diff (or the expected entry), asks `Apply these changes? [y/N]` (unless `--yes`), backs up every file it touches (`<file>.bak-herdr-dots-<time>`, `0600`), and refuses a file that changed after the preview. Claude Code's config is changed **only** through the `claude mcp` CLI; Codex's `config.toml` only inside the managed `# >>> herdr-dots MCP server` block (or an existing `[mcp_servers.herdr-dots]` table). Tests point the installer at temp dirs and **never** read or write the real `~/.claude.json`, `~/.claude/` or `~/.codex/`.
- v1 `ipc.Client.Call` keeps its behavior (30-second default deadline); `ipc.Client.Query` follows its context and has no default.
- `github.com/modelcontextprotocol/go-sdk` is pinned at **v1.8.0** (≥ v1.7.0, the first release with MCP 2026-07-28). Run every `go` command with `GOTOOLCHAIN=local`; `go mod tidy` must not raise the `go` directive past `1.25.0`.
- Run `gofmt -l .` (must print nothing), `GOTOOLCHAIN=local go vet ./...` and `GOTOOLCHAIN=local go test -race ./...` before every commit. Tests are deterministic and race-safe: fakes, in-memory MCP transports, `t.TempDir()` (short `os.MkdirTemp("", …)` dirs for unix sockets), `eventually` polling instead of fixed sleeps for anything that must happen.
- **Phase 5 starts only after Phases 2, 3 and 4 merge.** Files shared with them are listed under "Shared files" below.

Phase 4 constraints that still bind code touched here (copied from `docs/superpowers/plans/2026-10-08-herdr-dots-phase-4-api-backends.md`):
- Keys never reach logs, the feed, snapshots, IPC, memory, argv or error strings: an ask's error text passes through `credentials.Redact` before it goes back over IPC.
- Dollar budgets sit next to call budgets (`[budget] max_usd_per_session`, `session_budget_usd`; 0 = off); runs with an unknown price never count toward a dollar budget.

Phase 3 constraints that still apply (copied from `docs/superpowers/plans/2026-10-08-herdr-dots-phase-3-council-mode.md`):
- Council cards are always queue mode; `ask_council` makes **no** card at all (its digest goes back to the agent).
- One council per pane at a time (`ErrCouncilRunning`); member runs go through `sem`; council code takes only `e.mu` briefly and never a pane lock.
- All model text is sanitized with `textutil.SanitizeModelText` before it is stored, rendered or returned.

Phase 2 constraints that still apply (copied from `docs/superpowers/plans/2026-10-08-herdr-dots-phase-2-dot-memory.md`):
- **Phase 2 is VITAL**; the README roadmap says so.
- Memory lives under `STATE_DIR/memory` (`paths.Paths.MemoryDir()`), never under `SessionDir`. Memory files `0600`, directories `0700`, written atomically under the directory's `memory.lock` (2 s timeout).
- Model- and agent-written project memory is `proposed` until the developer approves it (ruling 2). Secret-looking text never becomes a memory entry.
- Memory I/O never happens under `e.mu`, a pane lock or `e.dmu`.
- Project ids (`^[gp]-[0-9a-f]{16}$`) and dot slugs (`^[a-z0-9][a-z0-9-]{0,127}$`) are the only values that become paths.

v1 constraints that still apply (copied from `docs/superpowers/plans/2026-10-08-herdr-dots-v1.md`):
- Go module path `herdr-dots`. Platforms: macOS and Linux only (`platforms = ["macos", "linux"]`). No Windows code paths (Phase 6 ports the MCP server).
- Dots are read-only: the claude backend uses only `Read,Grep,Glob` (or no tools) and always passes `--strict-mcp-config`, `--settings '{"disableAllHooks":true}'` and `--no-session-persistence`; codex uses `--sandbox read-only`. Never pass `--dangerously-skip-permissions` or `bypassPermissions`. Never `--bare`.
- Every child process environment is stripped of `HERDR_*`, `CLAUDECODE`, `CLAUDE_CODE_ENTRYPOINT` and `CLAUDE_CODE_CHILD_SESSION` (`backend.CleanEnv`).
- All state files `0600`, directories `0700`, writes atomic (`fsutil.WriteFileAtomic`).
- Lock order: `paneLock` → `e.mu` → store mutexes; `e.dmu` is separate. Never hold a pane lock or `e.mu` while calling a backend.
- Tests never touch the user's real Herdr session (`~/.config/herdr/herdr.sock`), real `STATE_DIR`/`CONFIG_DIR`, or real agent configs. Live experiments use `herdr --session dots-test`, started with every `CLAUDE*` variable unset, and never `herdr plugin link` (plugins are global to the user).

## Plan decisions

No human was available; every ambiguity below was resolved here. Each can be reversed.

1. **go-sdk v1.8.0, not v1.7.0.** v1.8.0 (the latest) supports MCP 2026-07-28 and 2025-11-25 like v1.7.0 and adds transport hardening (capped stdio buffers, session-leak fixes). Its `go.mod` says `go 1.25.0`, which the module already uses since Phase 4. The API this plan uses was compiled and run against v1.8.0 while planning (see "Confirmed while planning").
2. **Claude Code's install forwards the pane variables explicitly.** `claude mcp add --scope user --env HERDR_PANE_ID=${HERDR_PANE_ID:-} --env HERDR_SOCKET_PATH=${HERDR_SOCKET_PATH:-} --transport stdio herdr-dots -- <bin> mcp …`. Claude Code already passes its whole environment to stdio servers (V1, confirmed live), but the `${VAR:-}` expansion (confirmed live) keeps the server working if that ever changes, and expands to empty outside Herdr. `--transport stdio` sits between the last `--env` and the name because the CLI otherwise reads the name as another `KEY=value` pair (Claude Code docs). The server treats an unexpanded `${…}` value as unset.
3. **Codex is installed by our own managed block, not `codex mcp add`.** `codex mcp add` can't write `env_vars` or the timeouts, so the spec's flow edits `config.toml` anyway; one line-preserving edit is testable byte for byte and needs no Codex binary (Codex isn't installed on the planning machine; ruling "Codex paths stay documented-format, not live-verified"). The block is `[mcp_servers.herdr-dots]` with `command`, `args`, `env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]`, `startup_timeout_sec = 20`, `tool_timeout_sec = 180`, between `# >>> herdr-dots MCP server …` and `# <<< herdr-dots MCP server`. An existing unmarked `[mcp_servers.herdr-dots]` table (and its sub-tables) is replaced in place. An inline-table or dotted-key definition, an invalid file, or a result that doesn't read back as expected is refused with nothing changed. `codex mcp list` verifies when `codex` is on PATH.
4. **Installer flags.** `--client claude|codex|both` (default `both`: every client found, the others skipped with a note); `claude` needs the `claude` CLI; `codex` needs the `codex` CLI or an existing Codex config. A missing required CLI is an error before anything changes. `--remove` removes; `--yes` skips the question. Running a `go run` binary is refused (its path is temporary).
5. **The Claude "diff" is the expected entry.** Claude Code owns `~/.claude.json` (`$CLAUDE_CONFIG_DIR/.claude.json` when set), so setup-mcp shows the commands it will run plus the `mcpServers` entry Claude Code will store, backs the file up, and lets the CLI write it. Re-install runs `claude mcp remove --scope user herdr-dots` first (allowed to fail, output hidden).
6. **Install records** live in `CONFIG_DIR/mcp-install.json` (`{"version":1,"installs":[{client, scope, command, config_path, backups, at}]}`, `0600`). The manage popup's library footer shows `MCP: installed for Claude Code, Codex` or `MCP: not installed (run herdr-dots setup-mcp)`, read once when the popup opens (`manage.Deps.MCPStatus`).
7. **One query per connection.** `serveConn` hands query commands to `serveQuery`, which answers once and returns (the connection closes), and watches the connection: when the client hangs up, the query's context is cancelled, so an agent that gives up stops its dot run. A handler without `Query` answers `query not supported`; query commands never reach `Handle`. `Call`'s 30-second default moves into the package variable `callTimeout` (same value) so a test can shorten it.
8. **`list_dots` returns `ipc.DotsResult{ProjectID, ProjectName, Dots}`**, not a bare list with a project id on every dot (the outline's `DotInfo.ProjectID`): the memory tools need the pane's project even when the list is empty. A pane with no attachment returns an empty list, not an error.
9. **The engine owns the ask limits** (`askLimiter`, per pane, its own leaf lock), so restarting the MCP server can't reset them. The per-turn count resets in `processPaneLocked` whenever a pane has new turns (a turn ended); `dropPane` forgets the pane. A refused ask (paused, unattached, over budget) does not use up a slot; a failed run does. `ask_council` counts as one ask, taken before its preflight.
10. **Ask = `runDot` with a sink.** `Engine.Ask` reuses Phase 3's `runOpts.sink`/`ctx` plus a new `runOpts.ask`: `summon: true` (so `Force`, no suppression, no `finishRun`), the memory block and `remember` ops (O5), usage, the failure streak. With `ask` set, `runDot` writes the feed line `asked` (detail `<severity or pass> · <cost detail>`), sets the roster status `consulted`, never routes a card, and sets **no cooldown** (the consulted flag already covers double advice; a cooldown would silence the dot on later turns too). The question reaches the dot prefixed with `(The main coding agent is asking you directly, on the developer's behalf, through the Dots MCP server.)`. The ask runs in a tracked goroutine (`e.spawn`) so `Engine.Wait` drains it, and ends with the IPC request or the engine.
11. **Consulted guard.** `Engine.consulted[key]` is set after a successful ask (and for every council member that answered an `ask_council`). In `processPaneLocked`, a dot with new turns reads and clears its flag, and `Decide` returns `ReasonConsulted` (after the paused/observe/on-turn checks), which sets the roster status `consulted`. A dot that was running when its turn ended keeps the flag for its rerun. A failed ask sets nothing.
12. **Ask checks** are `Decide`'s without the turn-based ones: attached (else `no attached dot 'x'; attached: a, b`), dot file known, not paused or paused-all (`ErrPaused`), dot call and dollar budget left, pane call and dollar budget left (`ErrBudget`). Questions are sanitized and capped at 4,000 runes; empty questions are refused.
13. **`ask_council` = Phase 3's council with a result callback.** `Council` becomes a thin wrapper over a new `startCouncil(ctx, paneID, question, slugs, result)`; `councilRun` gains `result` and `done`. With `result` set, `runCouncil` hands the parts, digest and note to it instead of `addCouncilCard` (no card, no `council` feed line); `AskCouncil` waits for `done`, writes an `asked` feed line (Dot `council`, `"<severity> · N of M members answered"`), marks answering members consulted, and reports the pane's spend during the council as `cost_usd` (approximate: it includes any concurrent run on the pane). If the agent gives up, the council is abandoned (runs cancelled, no result). Phase 3 caps the council question at 2,000 runes.
14. **Timeouts.** `ask_dot` gets `run_timeout_seconds + 10 s` (130 s by default); `ask_council` gets `3 × run_timeout + 10 s` (two member rounds plus the chair, at worst). Codex's `tool_timeout_sec = 180` (spec P13) covers `ask_dot`; a council with the default 120 s run timeout can outlast it, which the README says (raise `tool_timeout_sec` or lower `run_timeout_seconds`). Claude Code's per-tool timeout (`MCP_TOOL_TIMEOUT`) defaults to about 28 hours (docs), so nothing is needed there.
15. **Structured results are primary.** Every tool returns `structuredContent` (typed `Out`) plus a short text rendering. A live check showed Claude Code passes the **structured** result to the model, not the text (V6), so every structured result carries a `note` with the advisory and queue-bypass reminder, and the size budget counts both. Lists are wrapped in objects (`{"dots": [...]}`) so older clients that require an object get one.
16. **Memory tools.** `read_memory` takes `query`, `scope` (`project`, `dot` or `all`, default `all`, which also includes the dots' global entries), `dot`, `limit` (default 20, at most 50); active entries only; partial read errors become a `note`. `propose_memory` takes `kind` (`convention`, `decision` or `note`), `text` (≤ 500), `paths`; it stores through `memory.Store.Propose` (project scope, `proposed`, source `agent`, no dot, never pinned, ignores `memory_auto_approve_project`), then sends `CmdMemoryChanged` so the Dots pane's proposal badge updates. Text matching an approved entry returns that entry (`status: active`) with a note. Both need memory on and a known project (from `list_dots`); otherwise they refuse with a clear message.
17. **Codex dot runs get `-c mcp_servers.herdr-dots.enabled=false` only when the `config.toml` they will read declares the server** (`$CODEX_HOME/config.toml`, else `$HOME/.codex/config.toml`, taken from the child's environment). Without a declared server, the override would give Codex a server entry with no command, which could break every codex dot for users who never installed the MCP server. The server refuses anyway without `HERDR_PANE_ID` (CleanEnv), so the override is the second layer (P11).
18. **The engine started by the MCP server** gets `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR` and `HERDR_SOCKET_PATH` from the server's paths (`engineEnv`), because the agent's environment has no `HERDR_PLUGIN_*`. The server starts the engine lazily on the first tool call (never at server start, which must stay under the client's startup timeout) and once more when a call can't connect.
19. **`[mcp]` config.** `tools` (default every tool; unknown names dropped; an explicit empty list means none), `max_asks_per_turn` (default 3, clamped 1–50), `max_asks_per_minute` (default 6, clamped 1–120); 0 or a negative value means the default (remove `ask_dot` from `tools` to turn asking off). Read by the server at start (tools, timeouts) and by the engine per ask (limits; `config.toml` reloads at runtime).
20. **Version bump to 0.5.0 happens in the last task**, with the README and roadmap.

## Confirmed while planning (2026-10-08)

Probes ran in a scratch module outside the repo; nothing touched the user's real Claude Code or Codex config.

- **go-sdk v1.8.0 API** (compiled and run with Go 1.25.7, `GOTOOLCHAIN=local`): `mcp.NewServer(&mcp.Implementation{Name, Version}, nil)`; `mcp.AddTool(s, &mcp.Tool{Name, Description}, func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error))`; input schemas inferred from `json` tags with `jsonschema:"<description>"` (an `omitempty` field is optional; `*bool` becomes `["null","boolean"]`); a handler error becomes `IsError: true` with the error text as content; a schema violation (missing required field) also comes back as `IsError: true` ("validating \"arguments\": …"); an unknown tool is a protocol error from `CallTool`; when the handler returns a result with `Content` set, `StructuredContent` is still filled from `Out` (the client sees it as `map[string]any`); with `Content` unset, the SDK fills it with the JSON of `Out`; a nil slice in `Out` passes output validation. `mcp.NewInMemoryTransports()`, `server.Connect(ctx, t, nil)`, `mcp.NewClient(...).Connect(ctx, t, nil)`, `cs.ListTools(ctx, nil)`, `cs.CallTool(ctx, &mcp.CallToolParams{Name, Arguments})`, `mcp.IOTransport{Reader, Writer}`, `mcp.StdioTransport{}` all work; `server.Run` returns `nil` when stdin reaches EOF.
- **V1 (Claude Code environment):** a probe stdio server started by `claude -p --mcp-config <temp file> --strict-mcp-config` (Claude Code 2.1.295) received Claude Code's whole environment (61 variables, including `HERDR_PANE_ID` and `HERDR_SOCKET_PATH`), and `"env": {"PROBE_PANE": "${HERDR_PANE_ID:-}", "PROBE_MISSING": "${PROBE_NOT_SET_ANYWHERE:-}"}` expanded to `w9:p9` and `""`. Assumption holds: yes.
- **V5 (protocol):** Claude Code 2.1.295 sent `server/discover` and `subscriptions/listen` with `_meta["io.modelcontextprotocol/protocolVersion"] = "2026-07-28"` and called the tool successfully against go-sdk v1.8.0. Assumption holds: yes.
- **V6 (what the model sees):** the probe tool returned text content `TEXT-WORD: walrus` and structured content `{"secret": "STRUCTURED-WORD: pelican"}`; asked to repeat every code word it could see, Claude answered only `pelican`. Claude Code gives the model the structured result when there is one. Design consequence: Plan decision 15.
- **V4 (Claude Code tool timeout):** the Claude Code MCP docs say `MCP_TOOL_TIMEOUT` (per-server `timeout` in `.mcp.json`) defaults to about 28 hours; `MCP_TIMEOUT` is the startup timeout. A 130 s ask fits. Assumption holds: yes (docs).
- **V2/V3 (Codex):** Codex docs (learn.chatgpt.com/docs/extend/mcp): servers get only `env` plus the variables named in `env_vars` (plus Codex's small default set); keys `command`, `args`, `env`, `env_vars`, `cwd`, `enabled` (default true), `required`, `startup_timeout_sec` (default 10), `tool_timeout_sec` (default 60), `enabled_tools`, `disabled_tools`; config in `~/.codex/config.toml` (`$CODEX_HOME`). Codex isn't installed here: not live-verified (Task 1 records it; the design doesn't depend on V3 because of Plan decision 17).
- **BurntSushi/toml v1.6.0:** decoding `[mcp] tools = []` into a struct whose slice already holds defaults gives an empty non-nil slice; a missing key keeps the defaults; `[mcp] max_asks_per_turn = 2` alone keeps the default tools.
- Every code block in Tasks 2–8 and 10 was compiled and its tests run green (`go test -race`, `go vet`, `gofmt -l`) in a scratch copy of v1 + merged Phase 2 tasks, with stand-ins for the Phase 2–4 names those tasks use (`memoryStoreFor`, `OverUSD`, `costDetail`, `withCost`, `errText`, `projectOf`, `Usage.Cost/PaneCost`, `credentials.Redact`, `Result.CostKnown/Model`, `MaxUSDPerSession`, `SessionBudgetUSD`) and Phase 3's `runDot` sink applied. Task 9 needs Phase 3's council code and could not be compiled while planning; its code follows Phase 3's plan text exactly.

## Review Focus

The five inputs the spec implies but its test list doesn't exercise, most likely to bite a person using this first. Each has a test in its owning task.

1. **A codex dot run spawns the MCP server** (codex loads the user's MCP servers). Expect the run to carry `-c mcp_servers.herdr-dots.enabled=false` when the server is configured, and the server, if started anyway, to refuse every tool and log the refusal. Tests: `TestCodexDisablesTheDotsMCPServerWhenConfigured` (Task 5), `TestEveryToolRefusesOutsideHerdr` (Task 8), `TestServeMCPOutsideHerdrRefusesEveryTool` (Task 10).
2. **The agent calls `ask_dot` in a loop.** Expect the 4th ask in one turn to come back `ask limit reached; continue without the dot (3 per turn)` with no backend run and no usage, and the next turn to start over. Tests: `TestFourthAskInATurnIsRefusedWithoutARun`, `TestAskLimiterPerMinuteWindow` (Task 7).
3. **The engine isn't running when the agent first calls a tool.** Expect server start to stay instant (nothing started), the first call to start the engine and succeed, and an engine that died later to be started again once. Test: `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies` (Task 8).
4. **A dot run longer than 30 seconds.** Expect `Client.Query` to follow its context (no 30-second cut-off, while `Call` keeps its default) and `ask_dot` to carry a deadline of the run timeout plus 10 s. Tests: `TestQueryFollowsTheContextNotCallsDefault` (Task 2), `TestAskDotPassesADeadline` (Task 8).
5. **The user's `~/.codex/config.toml` has other servers, profiles and comments.** Expect setup-mcp to keep every other line byte for byte, back the file up, be a no-op on re-run, and make `--remove` restore the original exactly. Test: `TestCodexInstallPreservesOtherTablesAndRemoveRestores` (Task 6).

---

## Parallel execution: dependencies and waves

Each task runs in its own git worktree branched from the wave's base (the base branch with Phases 2–4 merged, plus every earlier wave). Merge every task of a wave, in task order, before starting the next wave. Tasks in the same wave never touch the same file.

| Task | Title | Depends on | Wave |
|---|---|---|---|
| 1 | Verification spike (V1–V6) | — | 1 |
| 2 | Data-returning IPC (`Querier`, `Client.Query`) | — | 1 |
| 3 | `[mcp]` config, `asked` feed outcome, `consulted` status | — | 1 |
| 4 | Agent memory proposals (`Store.Propose`, `SourceAgent`) | — | 1 |
| 5 | Codex dot runs disable the Dots MCP server | — | 1 |
| 6 | Installer (`internal/mcpinstall`, `setup-mcp`, manage footer) | — | 1 |
| 7 | Engine: `Ask`, `ListDots`, `ListQueue`, `Query`, consulted guard, ask limits | Task 2, Task 3 | 2 |
| 8 | MCP server package (`internal/mcpserver`) | Task 2, Task 3, Task 4 | 2 |
| 9 | Engine: `AskCouncil` | Task 7 | 3 |
| 10 | `herdr-dots mcp` command | Task 2, Task 3, Task 8 | 3 |
| 11 | README, roadmap, version 0.5.0 and end-to-end check | all | 4 |

**Wave schedule:** W1 = {1, 2, 3, 4, 5, 6} → W2 = {7, 8} → W3 = {9, 10} → W4 = {11}.

**Files per task** (disjoint within each wave):
- Task 1: `docs/superpowers/notes/2026-10-08-phase-5-verification.md`.
- Task 2: `internal/ipc/ipc.go`, `internal/ipc/query.go`, `internal/ipc/query_test.go`.
- Task 3: `internal/config/config.go`, `internal/config/mcp.go`, `internal/config/mcp_test.go`, `internal/state/feed.go`, `internal/view/view.go`, `internal/ui/pane/model.go`, `internal/ui/pane/asked_test.go`.
- Task 4: `internal/memory/types.go`, `internal/memory/propose.go`, `internal/memory/propose_test.go`.
- Task 5: `internal/backend/codex.go`, `internal/backend/codex_mcp.go`, `internal/backend/codex_mcp_test.go`.
- Task 6: `internal/mcpinstall/install.go`, `codex.go`, `diff.go`, `records.go`, `install_test.go`; `cmd/herdr-dots/setupmcp.go`, `setupmcp_test.go`, `main.go`, `ui.go`; `internal/ui/manage/model.go`, `internal/ui/manage/mcp_test.go`.
- Task 7: `internal/engine/ask.go`, `ask_test.go`, `filter.go`, `engine.go`, `deliver.go`.
- Task 8: `internal/mcpserver/server.go`, `engine.go`, `server_test.go`, `engine_test.go`; `go.mod`, `go.sum`.
- Task 9: `internal/engine/council.go`, `internal/engine/ask_council.go`, `internal/engine/ask_council_test.go`, `internal/engine/ask.go`.
- Task 10: `cmd/herdr-dots/mcp.go`, `cmd/herdr-dots/mcp_test.go`, `cmd/herdr-dots/main.go`, `cmd/herdr-dots/engine.go`.
- Task 11: `README.md`, `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, `internal/version/version.go`, `herdr-plugin.toml`, the Task 1 notes file (E2E section).

**Gate after Wave 1:** start Wave 2 only after Task 1's notes say "Assumption holds: yes" for V1, V4 and V5 on the execution machine. If one says "no", stop and report to the coordinator (spec §9). V2 and V3 may say "not live-verified" (Codex not installed); that does not stop the plan. V6 is informational (Plan decision 15 already assumes the structured result is what the model sees).

**Test-name hygiene:** helpers added to shared packages use names no earlier phase uses. Task 2 (`ipc`): `queryHandler`, `newQueryHandler`, `serveHandler`. Task 3 (`config`): `writeMCPConfig`; (`pane`): `askedSnap`. Task 4 (`memory`): none new (reuses Phase 2's `testStore`, `mustAdd`, `list`, `pid`). Task 5 (`backend`): `codexHomeWith`, `codexArgs`, `withServer`. Task 6 (`main`): `mcpSetupRig`, `newMCPSetupRig`. Task 7 (`engine`): `mcpAsk`, `mcpAskedFeed`, `mcpDotStatus`. Task 9 (`engine`): `askCouncilNow`. Task 10 (`main`): `mcpDirs`, `mcpEnvOf`, `mcpNoEnsure`, `mcpSession`, `mcpToolText`, `mcpNopCloser`, `mcpFakeEngine`. Production helpers in `package main` are prefixed too (`mcpShellJoin`, `mcpIndent`, `mcpFileExists`, `mcpHasRecord`, `mcpLogger`, `herdrEnv`, `engineEnv`). None collides with v1, Phase 2, 3 or 4 names (`fakeBackend`, `rig`, `newRig`, `dot`, `pane`, `critical`, `memRig`, `councilRig`, `member`, `seat`, `says`, `quiet`, `costRig`, `keysRig`, `fakeCaller`, `key`, `press`, `snap`, `lib`, `setup`, …).

### Shared files with Phases 2, 3 and 4

These files already carry the earlier phases' changes when Phase 5 starts. Edits below are **Find / Replace** pairs whose find text was chosen from lines the earlier phases leave alone; where an earlier phase rewrote the spot, the task names the post-Phase-2/3/4 form. If a find text doesn't match exactly (a neighboring line moved during a reconciliation), make the same edit by hand.

| File | Phase 2 | Phase 3 | Phase 4 | Phase 5 task |
|---|---|---|---|---|
| `internal/ipc/ipc.go` | `CmdRemember`, `CmdMemoryChanged` | `CmdCouncil`, `CouncilArgs` | — | 2 |
| `internal/config/config.go` | `Memory*` settings | `Council` | `SessionBudgetUSD`, `Providers`, `ToolDenyGlobs`, `PricesFile` | 3 |
| `internal/config/config_test.go` | memory tests | council tests | `reflect.DeepEqual` comparisons | 3 (only if a `!=` comparison of `Config` is left) |
| `internal/state/feed.go` | `OutcomeLearned`, `OutcomeSuppressed` | `OutcomeCouncil` | — | 3 |
| `internal/view/view.go` | memory counts | `Council`, `CouncilPreflight` | cost fields | 3 |
| `internal/ui/pane/model.go` | header, `feedOutcome`, `M` chooser | council cards | cost display | 3 |
| `internal/memory/types.go` | (created) | — | — | 4 |
| `internal/backend/codex.go` | parse by `Pass` | — | — | 5 |
| `internal/ui/manage/model.go` | `M`/`P` memory screens | — | `K` providers, `CheckDot` | 6 |
| `cmd/herdr-dots/main.go` | `"memory"` | — | `"keys"` | 6, 10 |
| `cmd/herdr-dots/ui.go` | manage `Memory`/`Project` deps | council popup | manage backend deps | 6 |
| `cmd/herdr-dots/engine.go` | `buildEngine` memory deps | — | backend set | 10 |
| `internal/engine/engine.go` | memory hooks | `runOpts` sink, council fields | `requestFor`, `Usage.Add`, cost feed | 7 |
| `internal/engine/filter.go` | — | — | dollar budgets | 7 |
| `internal/engine/deliver.go` | outcomes | `dropPane` abandons councils | — | 7 |
| `internal/engine/council.go` | — | (created) | dollar preflight (Step 7) | 9 |
| `go.mod`, `go.sum` | — | — | SDKs, `go 1.25.0` | 8 |
| `internal/version/version.go`, `herdr-plugin.toml` | 0.2.0 | 0.3.0 | 0.4.0 | 11 |
| `README.md`, roadmap | memory docs | council docs | backends docs | 11 |

## File Structure

```
internal/ipc/query.go                (new)  query consts, QueryCmds, Querier, arg/result types, serveQuery, Client.Query
internal/ipc/ipc.go                  (mod)  Response.Data; serveConn routes queries; callTimeout
internal/config/mcp.go               (new)  MCPTools, MCPConfig, DefaultMCP, Normalized
internal/config/config.go            (mod)  Config.MCP; Default; Load normalizes
internal/state/feed.go               (mod)  OutcomeAsked
internal/view/view.go                (mod)  StatusConsulted
internal/ui/pane/model.go            (mod)  feed line "◆ asked by agent · <severity>"
internal/memory/propose.go           (new)  SourceAgent, AgentKinds, Store.Propose
internal/memory/types.go             (mod)  Source.Valid accepts agent
internal/backend/codex_mcp.go        (new)  CodexMCPServer, codexMCPOff, codexConfigPath
internal/backend/codex.go            (mod)  BuildCmd appends the override
internal/mcpinstall/install.go       (new)  Client, Env, Command, FileEdit, Plan, PlanInstall, PlanRemove, Apply, backups
internal/mcpinstall/codex.go         (new)  managed block, UpsertCodexBlock, RemoveCodexBlock
internal/mcpinstall/diff.go          (new)  Diff
internal/mcpinstall/records.go       (new)  Record, LoadRecords, SaveRecords, WithRecord, WithoutClient, StatusLine
cmd/herdr-dots/setupmcp.go           (new)  runSetupMCP, setupMCP, setupIO
internal/ui/manage/model.go          (mod)  Deps.MCPStatus, footer line
cmd/herdr-dots/ui.go                 (mod)  runManage passes MCPStatus
internal/engine/ask.go               (new)  errors, Query, askLimiter, Ask, ListDots, ListQueue, consulted helpers
internal/engine/filter.go            (mod)  FilterInput.Consulted, ReasonConsulted
internal/engine/engine.go            (mod)  runOpts.ask; Engine.consulted/asks; processPaneLocked; runDot ask branch
internal/engine/deliver.go           (mod)  dropPane forgets the pane's asks
internal/mcpserver/server.go         (new)  Deps, EngineAPI, New, tool handlers, rendering, size caps
internal/mcpserver/engine.go         (new)  NewIPCEngine (lazy ensure, retry once), ErrEngineDown
internal/engine/ask_council.go       (new)  AskCouncil, stopCouncil
internal/engine/council.go           (mod)  councilRun.result/done; startCouncil; runCouncil result branch
cmd/herdr-dots/mcp.go                (new)  runMCP, serveMCP, herdrEnv, mcpLogger
cmd/herdr-dots/engine.go             (mod)  engineEnv for the detached engine
cmd/herdr-dots/main.go               (mod)  "setup-mcp" (Task 6), "mcp" (Task 10)
go.mod, go.sum                       (mod)  github.com/modelcontextprotocol/go-sdk v1.8.0
internal/version/version.go          (mod)  0.5.0
herdr-plugin.toml                    (mod)  version 0.5.0
README.md                            (mod)  MCP section with the queue-bypass warning; roadmap; status 0.5.0 (not done)
docs/superpowers/notes/2026-10-08-phase-5-verification.md   (new)  V1–V6 and E2E findings
docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md     (mod)  Phase 5 plan link
```

**Conventions for code steps.** New files are given in full. Changes to existing files are **Find / Replace** pairs: find the exact text (unique in the file) and replace it. After editing, run `gofmt -w` on the touched files.

---

### Task 1: Verification spike (V1–V6)

This task produces **findings, not product code**. It answers the spec's §9 questions on the execution machine. Most were confirmed while planning (above); the spike repeats the live checks so the record comes from the machine that ships.

**Depends on:** none. Wave 1.

**Files:**
- Create: `docs/superpowers/notes/2026-10-08-phase-5-verification.md`

**Interfaces:**
- Consumes: CLI tools only (`go`, `claude`, `codex` if installed). Every probe lives in a fresh `mktemp -d` directory outside the repo and uses `--mcp-config <temp file> --strict-mcp-config` or a temporary `CODEX_HOME`, so the user's real `~/.claude.json`, `~/.claude/` and `~/.codex/config.toml` are never modified.
- Produces: "Assumption holds: yes/no/not live-verified" lines for V1–V6 that the Wave 1 gate reads; Task 11 appends the end-to-end results to the same file.

- [ ] **Step 1: Start the notes file with what was confirmed while planning**

Create `docs/superpowers/notes/2026-10-08-phase-5-verification.md`:

````markdown
# Phase 5 verification findings (V1–V6), 2026-10-08

Environment: record `go version`, `claude --version`, `codex --version` (or "not installed"), `sw_vers -productVersion` (or `uname -a`).

Every section records the command, the evidence and "Assumption holds: yes/no/not live-verified".
Probes run in `mktemp -d` directories with `--mcp-config <temp> --strict-mcp-config` or a temporary
`CODEX_HOME`; the real `~/.claude.json`, `~/.claude/` and `~/.codex/config.toml` are never modified.

## Confirmed while planning (2026-10-08, Claude Code 2.1.295, go-sdk v1.8.0, Go 1.25.7)

- V1: a probe stdio server started by `claude -p --mcp-config <temp> --strict-mcp-config` received Claude
  Code's whole environment (61 variables including HERDR_PANE_ID and HERDR_SOCKET_PATH); `${HERDR_PANE_ID:-}`
  in the server's `env` expanded to the value, `${UNSET:-}` to "". Assumption holds: yes.
- V4: Claude Code docs: MCP_TOOL_TIMEOUT defaults to about 28 hours; MCP_TIMEOUT is the startup timeout.
  Assumption holds: yes (docs).
- V5: Claude Code sent `server/discover` with protocolVersion "2026-07-28" and called the tool. Assumption holds: yes.
- V6: with text "TEXT-WORD: walrus" and structured content "STRUCTURED-WORD: pelican", Claude repeated only
  "pelican": the model sees the structured result. (Informational; Plan decision 15.)
- V2/V3: Codex docs only (Codex not installed): servers get `env` plus `env_vars`; `-c key=value` overrides
  config keys. Not live-verified.
````

- [ ] **Step 2: Re-run the V1/V5/V6 probe on this machine**

Create the probe in a temp dir (never inside the repo) and build it:

```bash
P=$(mktemp -d) && echo "$P"
mkdir -p "$P/cmd/probe"
printf 'module sdkprobe\n\ngo 1.25\n' > "$P/go.mod"
```

Write `"$P/cmd/probe/main.go"`:

```go
// Command probe is a throwaway stdio MCP server: it records the environment it
// was started with and every raw line it receives, to answer Phase 5 V1/V5/V6.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type rc struct {
	io.Reader
	io.Closer
}

type EchoOut struct {
	Secret string `json:"secret"`
}

func main() {
	logPath := os.Args[1]
	f, err := os.Create(logPath)
	if err != nil {
		os.Exit(1)
	}
	defer f.Close()
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HERDR_") || strings.HasPrefix(kv, "PROBE_") || strings.HasPrefix(kv, "PATH=") || strings.HasPrefix(kv, "HOME=") {
			fmt.Fprintln(f, "ENV", kv)
		}
	}
	fmt.Fprintln(f, "ENVCOUNT", len(os.Environ()))
	s := mcp.NewServer(&mcp.Implementation{Name: "probe", Version: "0.0.1"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "echo", Description: "Returns a fixed code word. Call it when asked."}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, EchoOut, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "TEXT-WORD: walrus"}}}, EchoOut{Secret: "STRUCTURED-WORD: pelican"}, nil
	})
	in := rc{Reader: io.TeeReader(os.Stdin, f), Closer: os.Stdin}
	err = s.Run(context.Background(), &mcp.IOTransport{Reader: in, Writer: os.Stdout})
	fmt.Fprintln(f, "\nRUN END", err)
}
```

```bash
GOTOOLCHAIN=local go -C "$P" get github.com/modelcontextprotocol/go-sdk@v1.8.0
GOTOOLCHAIN=local go -C "$P" build -o "$P/probe" ./cmd/probe
cat > "$P/mcp.json" <<EOF
{"mcpServers": {
  "probe":  {"type": "stdio", "command": "$P/probe", "args": ["$P/plain.log"]},
  "probex": {"type": "stdio", "command": "$P/probe", "args": ["$P/expand.log"],
             "env": {"PROBE_PANE": "\${HERDR_PANE_ID:-}", "PROBE_MISSING": "\${PROBE_NOT_SET_ANYWHERE:-}"}}
}}
EOF
cat > "$P/run.sh" <<EOF
#!/bin/sh
unset CLAUDECODE CLAUDE_CODE_ENTRYPOINT CLAUDE_CODE_CHILD_SESSION
export HERDR_PANE_ID=w9:p9 HERDR_SOCKET_PATH=/tmp/fake.sock
claude -p "Call the tool mcp__probe__echo exactly once, then reply with every code word you can see in its result, verbatim, and nothing else." \
  --mcp-config "$P/mcp.json" --strict-mcp-config --allowedTools mcp__probe__echo \
  --model haiku --no-session-persistence --settings '{"disableAllHooks":true}'
EOF
sh "$P/run.sh"; echo "---- plain"; head -6 "$P/plain.log"; echo "---- expand"; head -8 "$P/expand.log"
```

Expected: the reply is `pelican` (V6: structured content reaches the model; `walrus` too means both do, which changes nothing); `plain.log` lists `ENV HERDR_PANE_ID=w9:p9` and `ENV HERDR_SOCKET_PATH=/tmp/fake.sock` (V1) and a `server/discover` line whose `_meta` has `"io.modelcontextprotocol/protocolVersion":"2026-07-28"` or an `initialize` line with a `protocolVersion` (V5: record which); `expand.log` lists `ENV PROBE_PANE=w9:p9` and `ENV PROBE_MISSING=` (V1, expansion). Record the evidence under "## Execution-machine results" in the notes. V1 or V5 "no" → stop (the Wave 1 gate).

- [ ] **Step 3: V2/V3 with Codex, only if `codex` is installed**

If `command -v codex` prints nothing, record "V2, V3: not live-verified (codex not installed)" and skip to Step 4. Otherwise, with a temporary `CODEX_HOME` that borrows only the login file (read, never written):

```bash
C=$(mktemp -d)
ln -s "$HOME/.codex/auth.json" "$C/auth.json"
cat > "$C/config.toml" <<EOF
[mcp_servers.probe]
command = "$P/probe"
args = ["$P/codex.log"]
env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]
EOF
env -u CLAUDECODE CODEX_HOME="$C" HERDR_PANE_ID=w9:p9 HERDR_SOCKET_PATH=/tmp/fake.sock \
  codex exec --skip-git-repo-check --sandbox read-only "Call the echo tool of the probe server once and print the code word." | tail -5
head -6 "$P/codex.log"
rm -f "$P/codex.log"
env -u CLAUDECODE CODEX_HOME="$C" HERDR_PANE_ID=w9:p9 \
  codex exec --skip-git-repo-check --sandbox read-only -c mcp_servers.probe.enabled=false "Say hi." | tail -2
ls "$P/codex.log" 2>/dev/null || echo "V3: the server did not start"
```

Expected: `codex.log` lists `ENV HERDR_PANE_ID=w9:p9` (V2: `env_vars` forwards it); after the `-c …enabled=false` run there is no `codex.log` (V3). Record both. A "no" for V3 does not stop the plan (Plan decision 17: the server refuses anyway); a "no" for V2 means the README must tell Codex users to add the variables by hand, so stop and report.

- [ ] **Step 4: Commit the notes**

```bash
git add docs/superpowers/notes/2026-10-08-phase-5-verification.md
git commit -m "docs: Phase 5 verification findings (V1-V6)"
```

---

### Task 2: Data-returning IPC (`Querier`, `Client.Query`)

**Depends on:** none. Wave 1.

**Files:**
- Create: `internal/ipc/query.go`, `internal/ipc/query_test.go`
- Modify: `internal/ipc/ipc.go`
- Shared with Phases 2 and 3 (already merged; edit on top of them): `internal/ipc/ipc.go`

**Interfaces:**
- Consumes: v1 `Request`, `Response`, `Handler`, `Serve`, `serveConn`, `Client`, `Client.dial`, `Client.Call`; test helpers `fakeHandler` (fields `mu`, `got`, `updates`) and `serve(t) (*fakeHandler, Client)` from `ipc_test.go`.
- Produces:
  - `ipc.Response.Data json.RawMessage` (JSON `data,omitempty`).
  - consts `ipc.CmdListDots = "list_dots"`, `ipc.CmdAsk = "ask"`, `ipc.CmdAskCouncil = "ask_council"`, `ipc.CmdListQueue = "list_queue"`; `var ipc.QueryCmds = []string{CmdListDots, CmdAsk, CmdAskCouncil, CmdListQueue}`.
  - `type ipc.Querier interface{ Query(ctx context.Context, cmd string, args json.RawMessage) (any, error) }`; `var ipc.ErrQueryUnsupported` ("query not supported").
  - `ipc.QueuePending = "pending"`, `ipc.QueueAll = "all"`.
  - Arg types: `PaneArgs{PaneID}`, `AskArgs{PaneID, Slug, Question string; IncludeTurns bool}`, `AskCouncilArgs{PaneID, Question}`, `ListQueueArgs{PaneID, Status}` (JSON `pane_id`, `slug`, `question`, `include_turns`, `status,omitempty`).
  - Result types: `DotInfo{Slug, Name, PurposeSummary, Mode, Status string; Paused bool; Calls, Budget int; CostUSD float64}`; `DotsResult{ProjectID, ProjectName string; Dots []DotInfo}`; `AskResult{Slug, Name string; Relevant bool; Severity, Message string; CostUSD float64; CostKnown bool; Model string}`; `CouncilMember{Slug, Severity, Stance string; Failed bool}`; `CouncilResult{Digest string; Members []CouncilMember; CostUSD float64; Note string}`; `CardInfo{ID, Dot, Severity string; Turn int; Status, Body string}`; `QueueResult{Cards []CardInfo; Total int}` (snake_case JSON tags as in the code).
  - `(ipc.Client) Query(ctx context.Context, cmd string, args, out any) error`: no default deadline; the context's deadline bounds it and cancelling ctx aborts it; `out` is decoded from `Data`.
  - Server side: a query command runs on its own connection and its context ends when the client hangs up; a handler without `Query` answers `query not supported`; queries never reach `Handle`.
  - Unexported `callTimeout` (30 s, `Call`'s default), `isQuery`, `serveQuery`, `ctxErr`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ipc/query_test.go`:

```go
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/view"
)

// queryHandler answers queries with a scripted function. v1 commands go to
// the embedded fakeHandler; the command "slow" sleeps for slow first.
type queryHandler struct {
	*fakeHandler
	query func(ctx context.Context, cmd string, args json.RawMessage) (any, error)
	slow  time.Duration
}

func newQueryHandler(f func(ctx context.Context, cmd string, args json.RawMessage) (any, error)) *queryHandler {
	return &queryHandler{fakeHandler: &fakeHandler{updates: make(chan view.Snapshot, 1)}, query: f}
}

func (q *queryHandler) Handle(ctx context.Context, cmd string, args json.RawMessage) error {
	if cmd == "slow" {
		time.Sleep(q.slow)
	}
	return q.fakeHandler.Handle(ctx, cmd, args)
}

func (q *queryHandler) Query(ctx context.Context, cmd string, args json.RawMessage) (any, error) {
	return q.query(ctx, cmd, args)
}

// serveHandler serves h on a short socket path (macOS caps unix socket paths
// at 104 bytes, and t.TempDir() is long there).
func serveHandler(t *testing.T, h Handler) Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "ipcq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "q.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go Serve(ctx, ln, h)
	return Client{Path: path}
}

func TestQueryRoundTripCarriesData(t *testing.T) {
	var got AskArgs
	c := serveHandler(t, newQueryHandler(func(_ context.Context, cmd string, args json.RawMessage) (any, error) {
		if cmd != CmdAsk {
			return nil, errors.New("wrong command " + cmd)
		}
		if err := json.Unmarshal(args, &got); err != nil {
			return nil, err
		}
		return AskResult{Slug: got.Slug, Name: "Security", Relevant: true, Severity: "warn",
			Message: "use httpOnly cookies", CostUSD: 0.01, CostKnown: true, Model: "sonnet"}, nil
	}))
	var res AskResult
	err := c.Query(context.Background(), CmdAsk, AskArgs{PaneID: "w1:p1", Slug: "security", Question: "JWT storage?", IncludeTurns: true}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if got != (AskArgs{PaneID: "w1:p1", Slug: "security", Question: "JWT storage?", IncludeTurns: true}) {
		t.Fatalf("server got %+v", got)
	}
	if res.Name != "Security" || res.Severity != "warn" || res.Message != "use httpOnly cookies" || !res.CostKnown || res.Model != "sonnet" {
		t.Fatalf("client got %+v", res)
	}
}

func TestQueryErrorsComeBackAsText(t *testing.T) {
	c := serveHandler(t, newQueryHandler(func(context.Context, string, json.RawMessage) (any, error) {
		return nil, errors.New("ask limit reached; continue without the dot")
	}))
	err := c.Query(context.Background(), CmdListDots, PaneArgs{PaneID: "w1:p1"}, &DotsResult{})
	if err == nil || err.Error() != "ask limit reached; continue without the dot" {
		t.Fatalf("got %v", err)
	}
}

func TestQueryWithoutQuerierIsRefused(t *testing.T) {
	h, c := serve(t) // v1's fakeHandler has no Query method
	err := c.Query(context.Background(), CmdListQueue, ListQueueArgs{PaneID: "w1:p1"}, &QueueResult{})
	if err == nil || err.Error() != ErrQueryUnsupported.Error() {
		t.Fatalf("got %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.got) != 0 {
		t.Fatalf("a query must never reach Handle: %v", h.got)
	}
}

func TestV1ResponsesCarryNoData(t *testing.T) {
	_, c := serve(t)
	conn, err := net.Dial("unix", c.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(Request{ID: 7, Cmd: CmdPing}); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, `"data"`) || !strings.Contains(line, `"ok":true`) {
		t.Fatalf("v1 response %s", line)
	}
	// A v1 client's Response type (no Data field) still decodes a query reply.
	var v1 struct {
		ID       int            `json:"id"`
		OK       bool           `json:"ok"`
		Error    string         `json:"error"`
		Snapshot *view.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal([]byte(`{"id":1,"ok":true,"data":{"dots":[]}}`), &v1); err != nil || !v1.OK {
		t.Fatalf("v1 decode: %v %+v", err, v1)
	}
}

// Review Focus 4: a dot run longer than Call's 30-second default must not be
// cut off; Query follows its context instead (the default is shortened here).
func TestQueryFollowsTheContextNotCallsDefault(t *testing.T) {
	old := callTimeout
	callTimeout = 50 * time.Millisecond
	t.Cleanup(func() { callTimeout = old })
	h := newQueryHandler(func(context.Context, string, json.RawMessage) (any, error) {
		time.Sleep(200 * time.Millisecond) // a "run" four times longer than the default
		return DotsResult{ProjectID: "g-0123456789abcdef", Dots: []DotInfo{{Slug: "security"}}}, nil
	})
	h.slow = 200 * time.Millisecond
	c := serveHandler(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var res DotsResult
	if err := c.Query(ctx, CmdListDots, PaneArgs{PaneID: "w1:p1"}, &res); err != nil {
		t.Fatalf("query cut short: %v", err)
	}
	if res.ProjectID != "g-0123456789abcdef" || len(res.Dots) != 1 {
		t.Fatalf("res %+v", res)
	}
	if err := c.Call(context.Background(), "slow", nil); err == nil {
		t.Fatal("Call must keep its default deadline")
	}
}

func TestQueryHonorsItsOwnDeadline(t *testing.T) {
	c := serveHandler(t, newQueryHandler(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Query(ctx, CmdAsk, AskArgs{PaneID: "w1:p1", Slug: "security", Question: "q"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("deadline ignored")
	}
}

func TestQueryContextEndsWhenTheClientHangsUp(t *testing.T) {
	ended := make(chan struct{})
	c := serveHandler(t, newQueryHandler(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		close(ended)
		return nil, ctx.Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if err := c.Query(ctx, CmdAsk, AskArgs{PaneID: "w1:p1"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the engine kept working for a client that left")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/ipc/`
Expected: compile errors (`undefined: AskArgs`, `undefined: callTimeout`, `c.Query undefined`, …).

- [ ] **Step 3: Add the query protocol**

Create `internal/ipc/query.go`:

```go
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"time"
)

// Query commands (Phase 5) return data in Response.Data. Each one runs on its
// own connection: the server answers once and closes it, and a client that
// hangs up cancels the query's context.
const (
	CmdListDots   = "list_dots"   // PaneArgs → DotsResult
	CmdAsk        = "ask"         // AskArgs → AskResult
	CmdAskCouncil = "ask_council" // AskCouncilArgs → CouncilResult
	CmdListQueue  = "list_queue"  // ListQueueArgs → QueueResult
)

// QueryCmds are the commands serveConn hands to a Querier instead of Handle.
var QueryCmds = []string{CmdListDots, CmdAsk, CmdAskCouncil, CmdListQueue}

// Querier is implemented by handlers that answer query commands with data.
type Querier interface {
	Query(ctx context.Context, cmd string, args json.RawMessage) (any, error)
}

// ErrQueryUnsupported is the error text a handler without Query returns.
var ErrQueryUnsupported = errors.New("query not supported")

// ListQueueArgs.Status values.
const (
	QueuePending = "pending" // pending and auto cards (the default)
	QueueAll     = "all"     // every card of the pane, newest first
)

type PaneArgs struct {
	PaneID string `json:"pane_id"`
}

type AskArgs struct {
	PaneID       string `json:"pane_id"`
	Slug         string `json:"slug"`
	Question     string `json:"question"`
	IncludeTurns bool   `json:"include_turns"`
}

type AskCouncilArgs struct {
	PaneID   string `json:"pane_id"`
	Question string `json:"question"`
}

type ListQueueArgs struct {
	PaneID string `json:"pane_id"`
	Status string `json:"status,omitempty"`
}

// DotInfo is one attached dot as the MCP server's list_dots shows it.
type DotInfo struct {
	Slug           string  `json:"slug"`
	Name           string  `json:"name"`
	PurposeSummary string  `json:"purpose_summary"`
	Mode           string  `json:"mode"`
	Status         string  `json:"status"`
	Paused         bool    `json:"paused"`
	Calls          int     `json:"calls"`
	Budget         int     `json:"budget"`
	CostUSD        float64 `json:"cost_usd"`
}

// DotsResult is the pane's project and its attached dots.
type DotsResult struct {
	ProjectID   string    `json:"project_id,omitempty"`
	ProjectName string    `json:"project_name,omitempty"`
	Dots        []DotInfo `json:"dots"`
}

// AskResult is one dot's answer to ask_dot.
type AskResult struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Relevant  bool    `json:"relevant"`
	Severity  string  `json:"severity"`
	Message   string  `json:"message"`
	CostUSD   float64 `json:"cost_usd"`
	CostKnown bool    `json:"cost_known"`
	Model     string  `json:"model,omitempty"`
}

type CouncilMember struct {
	Slug     string `json:"slug"`
	Severity string `json:"severity,omitempty"`
	Stance   string `json:"stance,omitempty"`
	Failed   bool   `json:"failed,omitempty"`
}

// CouncilResult is ask_council's merged digest. CostUSD is approximate: the
// pane's spend while the council ran.
type CouncilResult struct {
	Digest  string          `json:"digest"`
	Members []CouncilMember `json:"members"`
	CostUSD float64         `json:"cost_usd"`
	Note    string          `json:"note,omitempty"`
}

type CardInfo struct {
	ID       string `json:"id"`
	Dot      string `json:"dot"`
	Severity string `json:"severity"`
	Turn     int    `json:"turn"`
	Status   string `json:"status"`
	Body     string `json:"body"`
}

// QueueResult holds at most 20 cards; Total counts every matching card.
type QueueResult struct {
	Cards []CardInfo `json:"cards"`
	Total int        `json:"total"`
}

func isQuery(cmd string) bool { return slices.Contains(QueryCmds, cmd) }

// serveQuery answers one query; serveConn closes the connection afterwards.
// The query's context ends when the client hangs up, so the engine stops
// working for an agent that gave up.
func serveQuery(ctx context.Context, conn net.Conn, dec *json.Decoder, enc *json.Encoder, req Request, h Handler) {
	resp := Response{ID: req.ID, OK: true}
	q, ok := h.(Querier)
	if !ok {
		resp.OK, resp.Error = false, ErrQueryUnsupported.Error()
		_ = enc.Encode(resp)
		return
	}
	qctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_, _ = io.Copy(io.Discard, io.MultiReader(dec.Buffered(), conn))
		cancel() // EOF or a closed connection: nobody waits for the answer
	}()
	v, err := q.Query(qctx, req.Cmd, req.Args)
	if err == nil {
		resp.Data, err = json.Marshal(v)
	}
	if err != nil {
		resp.OK, resp.Error, resp.Data = false, err.Error(), nil
	}
	_ = enc.Encode(resp)
}

// Query sends one query command and decodes Response.Data into out (when out
// is non-nil). Unlike Call it has no 30-second default: the context's
// deadline, if any, bounds the exchange, and cancelling ctx aborts it.
func (c Client) Query(ctx context.Context, cmd string, args, out any) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if d, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(d)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	var raw json.RawMessage
	if args != nil {
		if raw, err = json.Marshal(args); err != nil {
			return err
		}
	}
	if err := json.NewEncoder(conn).Encode(Request{ID: 1, Cmd: cmd, Args: raw}); err != nil {
		return ctxErr(ctx, err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return ctxErr(ctx, err)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Data) > 0 {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

// ctxErr prefers the context's error over the i/o error it caused.
func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
			return context.DeadlineExceeded
		}
	}
	return err
}
```

In `internal/ipc/ipc.go`, find:

```go
	Snapshot *view.Snapshot `json:"snapshot,omitempty"`
}
```

Replace with:

```go
	Snapshot *view.Snapshot `json:"snapshot,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"` // query commands only (query.go)
}
```

Find:

```go
		if req.Cmd == CmdWatch {
			stream(ctx, conn, enc, req.ID, h)
			return
		}
```

Replace with:

```go
		if req.Cmd == CmdWatch {
			stream(ctx, conn, enc, req.ID, h)
			return
		}
		if isQuery(req.Cmd) {
			serveQuery(ctx, conn, dec, enc, req, h) // one query per connection
			return
		}
```

Find:

```go
type Client struct{ Path string }
```

Replace with:

```go
// callTimeout is Call's deadline when its context has none. Query has no
// default: a dot run can take longer (Phase 5).
var callTimeout = 30 * time.Second

type Client struct{ Path string }
```

Find:

```go
	deadline := time.Now().Add(30 * time.Second)
```

Replace with:

```go
	deadline := time.Now().Add(callTimeout)
```

Run `gofmt -w internal/ipc`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race -count=3 ./internal/ipc/`
Expected: PASS three times, including v1's `TestCallRoundTripAndErrors`, `TestWatchStreamsInitialAndUpdates` and `TestCallFailsFastWithoutServer` unchanged.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/ipc
git commit -m "feat(ipc): data-returning queries on their own connection; Call keeps its 30s default"
```

---

### Task 3: `[mcp]` config, `asked` feed outcome, `consulted` status

**Depends on:** none. Wave 1.

**Files:**
- Create: `internal/config/mcp.go`, `internal/config/mcp_test.go`, `internal/ui/pane/asked_test.go`
- Modify: `internal/config/config.go`, `internal/state/feed.go`, `internal/view/view.go`, `internal/ui/pane/model.go`; `internal/config/config_test.go` only if it still compares `Config` values with `!=`
- Shared with Phases 2, 3 and 4 (already merged): all the modified files

**Interfaces:**
- Consumes: `config.Config`, `config.Default`, `config.Load`; `state.FeedEntry`; `view.Snapshot`, `view.DotState`; the pane's `feedOutcome(e state.FeedEntry) string` (Phase 2); pane test helpers `snap()`, `press`, `fakeCaller`.
- Produces:
  - `var config.MCPTools = []string{"list_dots", "ask_dot", "ask_council", "list_queue", "read_memory", "propose_memory"}`.
  - `type config.MCPConfig struct{ Tools []string; MaxAsksPerTurn, MaxAsksPerMinute int }` (TOML `tools`, `max_asks_per_turn`, `max_asks_per_minute`); `config.DefaultMCP() MCPConfig` = all tools, 3, 6; `(MCPConfig) Normalized() MCPConfig` (0 or less → default; clamp 1–50 and 1–120; known tools only, once, in `MCPTools` order; `nil` tools = all; `[]` = none).
  - `config.Config.MCP MCPConfig` (TOML `[mcp]`); `Default()` sets `DefaultMCP()`; `Load` normalizes.
  - `const state.OutcomeAsked = "asked"`; detail is `"<severity or pass>"` optionally followed by `" · "` and the cost detail.
  - `const view.StatusConsulted = "consulted"`.
  - The Dots pane feed renders an `asked` entry as `◆ asked by agent · <severity>`; the roster shows `consulted` as the status text (statuses render verbatim).

- [ ] **Step 1: Write the failing tests**

Create `internal/config/mcp_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeMCPConfig(t *testing.T, body string) Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMCPDefaults(t *testing.T) {
	c := Default()
	if !slices.Equal(c.MCP.Tools, MCPTools) || c.MCP.MaxAsksPerTurn != 3 || c.MCP.MaxAsksPerMinute != 6 {
		t.Fatalf("defaults %+v", c.MCP)
	}
	c.MCP.Tools[0] = "changed"
	if MCPTools[0] != "list_dots" {
		t.Fatal("Default must copy MCPTools, not share it")
	}
}

func TestMCPLoadKeepsKnownToolsAndClamps(t *testing.T) {
	c := writeMCPConfig(t, "[mcp]\ntools = [\"read_memory\", \"ask_dot\", \"bogus\", \"ask_dot\"]\nmax_asks_per_turn = 0\nmax_asks_per_minute = 500\n")
	if !slices.Equal(c.MCP.Tools, []string{"ask_dot", "read_memory"}) {
		t.Fatalf("tools %v", c.MCP.Tools)
	}
	if c.MCP.MaxAsksPerTurn != 3 || c.MCP.MaxAsksPerMinute != 120 {
		t.Fatalf("limits %+v", c.MCP)
	}
}

func TestMCPEmptyToolListMeansNoTools(t *testing.T) {
	c := writeMCPConfig(t, "[mcp]\ntools = []\n")
	if c.MCP.Tools == nil || len(c.MCP.Tools) != 0 {
		t.Fatalf("tools %#v: an explicit empty list offers no tools", c.MCP.Tools)
	}
}

func TestMCPMissingTableKeepsDefaults(t *testing.T) {
	c := writeMCPConfig(t, "inject_enabled = false\n")
	if !slices.Equal(c.MCP.Tools, MCPTools) || c.MCP.MaxAsksPerTurn != 3 || c.MCP.MaxAsksPerMinute != 6 {
		t.Fatalf("mcp %+v", c.MCP)
	}
}

func TestMCPNormalizedNilToolsMeansAll(t *testing.T) {
	m := MCPConfig{MaxAsksPerTurn: 2}.Normalized()
	if !slices.Equal(m.Tools, MCPTools) || m.MaxAsksPerTurn != 2 || m.MaxAsksPerMinute != 6 {
		t.Fatalf("normalized %+v", m)
	}
}
```

Create `internal/ui/pane/asked_test.go`:

```go
package pane

import (
	"strings"
	"testing"

	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

func askedSnap() view.Snapshot {
	s := snap()
	p := &s.Panes[0]
	p.Dots[0].Status = view.StatusConsulted
	p.Feed = []state.FeedEntry{
		{Turn: 9, Dot: "security", Outcome: state.OutcomeAsked, Detail: "warn · cost=$0.0100 tokens=900/80"},
		{Turn: 9, Dot: "frontend", Outcome: state.OutcomeAsked, Detail: "pass"},
	}
	return s
}

func TestFeedShowsAgentAsks(t *testing.T) {
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(askedSnap()))
	v := press(m, "f").View()
	if !strings.Contains(v, "◆ asked by agent · warn") || !strings.Contains(v, "◆ asked by agent · pass") {
		t.Fatalf("feed:\n%s", v)
	}
	if strings.Contains(v, "cost=$0.0100 tokens") {
		t.Fatalf("the feed line shows the severity, not the whole detail:\n%s", v)
	}
}

func TestRosterShowsConsulted(t *testing.T) {
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(askedSnap()))
	if v := m.View(); !strings.Contains(v, "consulted") {
		t.Fatalf("roster:\n%s", v)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/config/ ./internal/ui/pane/`
Expected: compile errors (`undefined: MCPTools`, `c.MCP undefined`, `undefined: view.StatusConsulted`, `undefined: state.OutcomeAsked`).

- [ ] **Step 3: Add the settings, the outcome, the status and the feed line**

Create `internal/config/mcp.go`:

```go
package config

import "slices"

// MCPTools are the Dots MCP server's tools (Phase 5), in display order.
var MCPTools = []string{"list_dots", "ask_dot", "ask_council", "list_queue", "read_memory", "propose_memory"}

// MCPConfig is [mcp] in config.toml.
type MCPConfig struct {
	Tools            []string `toml:"tools"`               // tools the server offers; [] = none
	MaxAsksPerTurn   int      `toml:"max_asks_per_turn"`   // per pane, reset when a turn ends
	MaxAsksPerMinute int      `toml:"max_asks_per_minute"` // per pane, sliding window
}

func DefaultMCP() MCPConfig {
	return MCPConfig{Tools: slices.Clone(MCPTools), MaxAsksPerTurn: 3, MaxAsksPerMinute: 6}
}

// Normalized fills unset limits with the defaults and clamps them (1–50 per
// turn, 1–120 per minute), and keeps only known tool names, once each, in
// MCPTools order. Tools == nil means every tool; an empty list means none.
func (m MCPConfig) Normalized() MCPConfig {
	d := DefaultMCP()
	if m.MaxAsksPerTurn <= 0 {
		m.MaxAsksPerTurn = d.MaxAsksPerTurn
	}
	if m.MaxAsksPerMinute <= 0 {
		m.MaxAsksPerMinute = d.MaxAsksPerMinute
	}
	m.MaxAsksPerTurn = min(m.MaxAsksPerTurn, 50)
	m.MaxAsksPerMinute = min(m.MaxAsksPerMinute, 120)
	if m.Tools == nil {
		m.Tools = d.Tools
		return m
	}
	out := []string{}
	for _, t := range MCPTools {
		if slices.Contains(m.Tools, t) {
			out = append(out, t)
		}
	}
	m.Tools = out
	return m
}
```

In `internal/config/config.go`, find:

```go
type Config struct {
```

Replace with:

```go
type Config struct {
	MCP MCPConfig `toml:"mcp"` // [mcp]: the Dots MCP server (Phase 5, mcp.go)

```

Find:

```go
func Default() Config {
	return Config{
```

Replace with:

```go
func Default() Config {
	return Config{
		MCP: DefaultMCP(),

```

Find:

```go
	c.MemoryMaxEntries = min(max(c.MemoryMaxEntries, 10), 10000)
```

Replace with:

```go
	c.MemoryMaxEntries = min(max(c.MemoryMaxEntries, 10), 10000)
	c.MCP = c.MCP.Normalized()
```

`Config` holds slices since Phase 4 and is compared with `reflect.DeepEqual` in `config_test.go` (Phase 4 Plan decision 4). Check: `grep -n 'c != \|!= want\|== Default()' internal/config/config_test.go`. If a `!=`/`==` comparison of whole `Config` values is still there, switch it to `!reflect.DeepEqual(…)` / `reflect.DeepEqual(…)` and import `reflect`.

Append to the end of `internal/state/feed.go`:

```go
// OutcomeAsked: the main agent asked this dot (or a council, Dot "council")
// through the Dots MCP server (Phase 5). Detail starts with the severity, or
// "pass", followed by " · " and the run's cost detail when there is one.
const OutcomeAsked = "asked"
```

Append to the end of `internal/view/view.go`:

```go
// StatusConsulted: the main agent asked this dot through the MCP server; it
// skips the turn it was asked in (Phase 5).
const StatusConsulted = "consulted"
```

In `internal/ui/pane/model.go`, find (Phase 2's `feedOutcome`):

```go
	case state.OutcomeSuppressed:
		return "suppressed (" + strings.TrimPrefix(e.Detail, "similar to ") + ")"
```

Replace with:

```go
	case state.OutcomeSuppressed:
		return "suppressed (" + strings.TrimPrefix(e.Detail, "similar to ") + ")"
	case state.OutcomeAsked:
		sev, _, _ := strings.Cut(e.Detail, " · ")
		return "◆ asked by agent · " + sev
```

If Phase 3 or 4 reshaped `feedOutcome` so the find text differs, add the same `case state.OutcomeAsked:` branch to whatever switch maps a feed entry's outcome to its label, and make sure Phase 4's cost tail (`cost=…`) is not printed a second time for `asked` lines (the test checks it). Run `gofmt -w internal/config internal/state internal/view internal/ui/pane`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/config/ ./internal/ui/pane/ ./internal/state/ ./internal/view/`
Expected: PASS, including every earlier config, pane, state and view test.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/config internal/state internal/view internal/ui/pane
git commit -m "feat: [mcp] settings, asked feed outcome and consulted status for the Dots MCP server"
```

---

### Task 4: Agent memory proposals (`Store.Propose`, `SourceAgent`)

**Depends on:** none. Wave 1.

**Files:**
- Create: `internal/memory/propose.go`, `internal/memory/propose_test.go`
- Modify: `internal/memory/types.go`
- Shared with Phase 2 (created there): `internal/memory/types.go`

**Interfaces:**
- Consumes: Phase 2's `memory.Store.Add(p string, e Entry) (Entry, error)` (dedupe, caps, `prepare`: sanitizing, 500-rune limit → `ErrTooLong`), `ValidProjectID`, `ScanSecrets(text string) (bool, string)`, `Entry`, `Evidence`, `Kind*`, `Status*`, `Scope*`, errors `ErrInvalid`, `ErrSecret`, `ErrTooLong`; test helpers `testStore`, `mustAdd`, `list`, `pid` from `store_test.go`.
- Produces:
  - `const memory.SourceAgent Source = "agent"`; `(Source) Valid()` accepts it (so TOML import and hand edits keep agent entries).
  - `var memory.AgentKinds = []Kind{KindConvention, KindDecision, KindNote}`.
  - `(*memory.Store) Propose(p string, e Entry) (Entry, error)`: stores `{Scope: project, Kind, Text, Paths, Status: proposed, Source: agent, Evidence}` whatever else the caller set (no dot, not pinned); `ErrInvalid` for a bad project id or another kind; `ErrSecret` (wrapped with the kind) for secret-looking text, storing nothing; text matching an existing entry returns that entry with its own status.

- [ ] **Step 1: Write the failing tests**

Create `internal/memory/propose_test.go`:

```go
package memory

import (
	"errors"
	"strings"
	"testing"
)

func TestProposeForcesAProjectProposalFromTheAgent(t *testing.T) {
	s := testStore(t)
	got, err := s.Propose(pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindConvention, Text: "Use pnpm, never npm.",
		Paths: []string{"package.json"}, Status: StatusActive, Source: SourceUser, Pinned: true, Evidence: Evidence{Pane: "w1:p1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeProject || got.Status != StatusProposed || got.Source != SourceAgent || got.Dot != "" || got.Pinned ||
		got.Kind != KindConvention || got.Evidence.Pane != "w1:p1" || got.Ref().String() != "p:"+got.ID {
		t.Fatalf("stored %+v", got)
	}
	es := list(t, s, pid, Filter{Scope: ScopeProject, Statuses: []Status{StatusProposed}})
	if len(es) != 1 || es[0].Text != "Use pnpm, never npm." || es[0].Paths[0] != "package.json" {
		t.Fatalf("listed %+v", es)
	}
}

func TestProposeRefusesSecretsAndStoresNothing(t *testing.T) {
	s := testStore(t)
	_, err := s.Propose(pid, Entry{Kind: KindNote, Text: "the deploy key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"})
	if !errors.Is(err, ErrSecret) || !strings.Contains(err.Error(), "anthropic key") {
		t.Fatalf("got %v", err)
	}
	if es := list(t, s, pid, Filter{}); len(es) != 0 {
		t.Fatalf("stored %+v", es)
	}
}

func TestProposeRefusesOtherKindsAndBadProjects(t *testing.T) {
	s := testStore(t)
	for _, k := range []Kind{KindFinding, KindDismissed, KindPreference, ""} {
		if _, err := s.Propose(pid, Entry{Kind: k, Text: "x"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("kind %q: got %v", k, err)
		}
	}
	if _, err := s.Propose("../..", Entry{Kind: KindNote, Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad project: %v", err)
	}
	if _, err := s.Propose(pid, Entry{Kind: KindNote, Text: strings.Repeat("a", 501)}); !errors.Is(err, ErrTooLong) {
		t.Fatalf("too long: %v", err)
	}
}

func TestProposeMatchingAnApprovedEntryKeepsItApproved(t *testing.T) {
	s := testStore(t)
	approved := mustAdd(t, s, pid, Entry{Scope: ScopeProject, Kind: KindDecision, Text: "State lives in zustand stores."})
	got, err := s.Propose(pid, Entry{Kind: KindDecision, Text: "State lives in zustand stores."})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != approved.ID || got.Status != StatusActive || got.Source != SourceUser || got.Uses != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestAgentIsAValidSource(t *testing.T) {
	if !SourceAgent.Valid() {
		t.Fatal("imports and hand edits must accept source \"agent\"")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/memory/ -run 'Propose|AgentIs'`
Expected: compile errors (`s.Propose undefined`, `undefined: SourceAgent`).

- [ ] **Step 3: Add proposals**

Create `internal/memory/propose.go`:

```go
package memory

import (
	"fmt"
	"slices"
)

// SourceAgent marks project memory the main coding agent proposed through the
// Dots MCP server (Phase 5).
const SourceAgent Source = "agent"

// AgentKinds are the kinds the main agent may propose.
var AgentKinds = []Kind{KindConvention, KindDecision, KindNote}

// Propose stores a project-scope proposal from the main agent. Whatever the
// caller set, the entry is project scope, status proposed (the developer
// approves it in the memory screen), source agent, no dot, not pinned.
// Secret-looking text is refused with ErrSecret. Text matching an existing
// entry returns that entry (its uses go up) with its own status.
func (s *Store) Propose(p string, e Entry) (Entry, error) {
	if !ValidProjectID(p) {
		return Entry{}, fmt.Errorf("%w: project id %q", ErrInvalid, p)
	}
	if !slices.Contains(AgentKinds, e.Kind) {
		return Entry{}, fmt.Errorf("%w: kind %q (want convention, decision or note)", ErrInvalid, e.Kind)
	}
	if found, kind := ScanSecrets(e.Text); found {
		return Entry{}, fmt.Errorf("%w (%s)", ErrSecret, kind)
	}
	return s.Add(p, Entry{Scope: ScopeProject, Kind: e.Kind, Text: e.Text, Paths: e.Paths,
		Status: StatusProposed, Source: SourceAgent, Evidence: e.Evidence})
}
```

In `internal/memory/types.go`, find:

```go
	return s == SourceUser || s == SourceModel || s == SourceOutcome || s == SourceImport
}
```

Replace with:

```go
	return s == SourceUser || s == SourceModel || s == SourceOutcome || s == SourceImport || s == SourceAgent
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/memory/`
Expected: PASS (the whole memory suite, Phase 2's tests included).

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/memory
git commit -m "feat(memory): agent proposals (project scope, proposed, secret-scanned)"
```

---

### Task 5: Codex dot runs disable the Dots MCP server

**Depends on:** none. Wave 1.

**Files:**
- Create: `internal/backend/codex_mcp.go`, `internal/backend/codex_mcp_test.go`
- Modify: `internal/backend/codex.go`
- Shared with Phase 2 (already merged): `internal/backend/codex.go`

**Interfaces:**
- Consumes: v1 `CodexCLI{Runner, Bin, Environ}`, `CodexCLI.BuildCmd`, `CleanEnv`, `dots.SafeArgs`; test helpers `req()` and `argAfter` from `claude_test.go`; `github.com/BurntSushi/toml`.
- Produces: `const backend.CodexMCPServer = "herdr-dots"`; `CodexCLI.BuildCmd` appends `-c mcp_servers.herdr-dots.enabled=false` right before the stdin marker `-` when the `config.toml` the child will read (`$CODEX_HOME/config.toml`, else `$HOME/.codex/config.toml`, from the child's cleaned environment) declares `[mcp_servers.herdr-dots]`; otherwise nothing changes. Unexported `codexMCPOff(env []string) []string`, `codexConfigPath(env []string) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/backend/codex_mcp_test.go`:

```go
package backend

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func codexHomeWith(t *testing.T, config string) string {
	t.Helper()
	home := t.TempDir()
	if config != "" {
		if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func codexArgs(env ...string) []string {
	return CodexCLI{Environ: func() []string { return env }}.BuildCmd(req()).Args
}

const withServer = "model = \"x\"\n\n[mcp_servers.docs]\ncommand = \"docs\"\n\n[mcp_servers.herdr-dots]\ncommand = \"/bin/herdr-dots\"\nargs = [\"mcp\"]\n"

// Review Focus 1: a codex dot run must not start the Dots MCP server.
func TestCodexDisablesTheDotsMCPServerWhenConfigured(t *testing.T) {
	home := codexHomeWith(t, withServer)
	args := codexArgs("HOME="+home, "PATH=/bin", "HERDR_PANE_ID=w1:p1")
	n := len(args)
	if args[n-1] != "-" || args[n-3] != "-c" || args[n-2] != "mcp_servers.herdr-dots.enabled=false" {
		t.Fatalf("args %v: the override sits right before the stdin marker", args)
	}
	cmd := CodexCLI{Environ: func() []string { return []string{"HOME=" + home, "HERDR_PANE_ID=w1:p1"} }}.BuildCmd(req())
	if slices.Contains(cmd.Env, "HERDR_PANE_ID=w1:p1") {
		t.Fatal("HERDR_* must still be stripped")
	}
}

func TestCodexHomeOverridesHome(t *testing.T) {
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(withServer), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := codexHomeWith(t, "")
	if args := codexArgs("HOME="+empty, "CODEX_HOME="+codexHome); !slices.Contains(args, "mcp_servers.herdr-dots.enabled=false") {
		t.Fatalf("CODEX_HOME config not read: %v", args)
	}
}

func TestCodexLeavesTheOverrideOutWithoutTheServer(t *testing.T) {
	for name, cfg := range map[string]string{
		"no config":       "",
		"other servers":   "[mcp_servers.docs]\ncommand = \"docs\"\n",
		"malformed":       "[mcp_servers.herdr-dots\ncommand = ",
		"commented table": "# [mcp_servers.herdr-dots]\n",
	} {
		home := codexHomeWith(t, cfg)
		if args := codexArgs("HOME="+home, "PATH=/bin"); strings.Contains(strings.Join(args, " "), "mcp_servers.") {
			t.Errorf("%s: args %v", name, args)
		}
	}
	if args := codexArgs("PATH=/bin"); strings.Contains(strings.Join(args, " "), "mcp_servers.") {
		t.Errorf("no HOME: args %v", args)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/backend/ -run Codex`
Expected: `TestCodexDisablesTheDotsMCPServerWhenConfigured` and `TestCodexHomeOverridesHome` FAIL (no override in the args); `TestCodexLeavesTheOverrideOutWithoutTheServer` passes already.

- [ ] **Step 3: Add the override**

Create `internal/backend/codex_mcp.go`:

```go
package backend

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// CodexMCPServer is the Dots MCP server's name in Codex's config.toml
// (herdr-dots setup-mcp writes [mcp_servers.herdr-dots]).
const CodexMCPServer = "herdr-dots"

// codexMCPOff returns the -c override that disables the Dots MCP server for
// one codex dot run, when the config.toml that run reads declares it. `codex
// exec` loads the user's MCP servers, so a dot would otherwise start ours
// (which refuses every tool anyway: CleanEnv strips HERDR_PANE_ID). Without
// a declared server the override is left out, so Codex never sees a server
// entry that has no command.
func codexMCPOff(env []string) []string {
	path := codexConfigPath(env)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		MCPServers map[string]toml.Primitive `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil
	}
	if _, ok := cfg.MCPServers[CodexMCPServer]; !ok {
		return nil
	}
	return []string{"-c", "mcp_servers." + CodexMCPServer + ".enabled=false"}
}

// codexConfigPath is the config.toml a codex child with env reads:
// $CODEX_HOME/config.toml, else $HOME/.codex/config.toml ("" when neither is set).
func codexConfigPath(env []string) string {
	var home, codexHome string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "CODEX_HOME":
			codexHome = v
		case "HOME":
			home = v
		}
	}
	switch {
	case codexHome != "":
		return filepath.Join(codexHome, "config.toml")
	case home != "":
		return filepath.Join(home, ".codex", "config.toml")
	}
	return ""
}
```

In `internal/backend/codex.go` (`BuildCmd`), find:

```go
	args = append(args, dots.SafeArgs(c.Kind(), r.Args)...) // never override --sandbox
	args = append(args, "-")
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: SystemPrompt(r) + "\n\n" + UserPrompt(r)}
```

Replace with:

```go
	env := CleanEnv(c.environ())
	args = append(args, dots.SafeArgs(c.Kind(), r.Args)...) // never override --sandbox
	args = append(args, codexMCPOff(env)...)                // a dot never starts the Dots MCP server (Phase 5)
	args = append(args, "-")
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: env, Stdin: SystemPrompt(r) + "\n\n" + UserPrompt(r)}
```

If a later phase changed the `return Cmd{…}` line, keep its other fields; the change is: compute `env` once, pass it as `Env`, and append `codexMCPOff(env)...` after the `SafeArgs` line and before `"-"`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/backend/`
Expected: PASS, including v1's `TestCodexBuildCmd` and `TestCodexBuildCmdNeverEmitsDeniedArgs` (their environments have no `HOME`, so no override is added and their first `-c` is still the dot's own).

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/backend
git commit -m "feat(backend): codex dot runs disable the herdr-dots MCP server when it is configured"
```

---

### Task 6: Installer (`internal/mcpinstall`, `setup-mcp`, manage footer)

**Depends on:** none. Wave 1.

**Files:**
- Create: `internal/mcpinstall/install.go`, `internal/mcpinstall/codex.go`, `internal/mcpinstall/diff.go`, `internal/mcpinstall/records.go`, `internal/mcpinstall/install_test.go`, `cmd/herdr-dots/setupmcp.go`, `cmd/herdr-dots/setupmcp_test.go`, `internal/ui/manage/mcp_test.go`
- Modify: `cmd/herdr-dots/main.go`, `cmd/herdr-dots/ui.go`, `internal/ui/manage/model.go`
- Shared with Phases 2, 3 and 4 (already merged): `cmd/herdr-dots/main.go`, `cmd/herdr-dots/ui.go`, `internal/ui/manage/model.go`

**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`; `github.com/BurntSushi/toml`; Phase 2's `paths.Discover() (paths.Paths, error)` (works inside and outside Herdr); `paths.New`; `manage.Deps`, `manage.New`, `style.Dim`; manage test helpers `fakeCaller`, `press`, `lib()`, `setup(t, target)`; `os/exec`.
- Produces:
  - Package `mcpinstall`: `type Client string` (`ClientClaude = "claude"`, `ClientCodex = "codex"`; `(Client) Title()` = `Claude Code` / `Codex`); `const ServerName = "herdr-dots"`; `var ForwardEnv = []string{"HERDR_PANE_ID", "HERDR_SOCKET_PATH"}`; `CodexStartupTimeoutSec = 20`, `CodexToolTimeoutSec = 180`; `type Env struct{ Getenv func(string) string; Home string }` with `ClaudeConfigPath()` (`$CLAUDE_CONFIG_DIR/.claude.json` or `~/.claude.json`) and `CodexConfigPath()` (`$CODEX_HOME/config.toml` or `~/.codex/config.toml`); `type Command struct{ Args []string; MayFail bool }`; `type FileEdit struct{ Path, Before, After string }`; `type Plan struct{ Client Client; Remove bool; Commands []Command; Edits []FileEdit; Backups []string; Verify []string; Preview string; Nothing bool; Config string }`; `var ErrUnsafeEdit`; `ServerArgs(configDir, stateDir string) []string` = `mcp --config-dir C --state-dir S`; `PlanInstall(c Client, env Env, bin, configDir, stateDir string) (Plan, error)`; `PlanRemove(c Client, env Env) (Plan, error)`; `Apply(p Plan, run func(Command) error, now time.Time) (backups []string, err error)`; `const BlockBegin, BlockEnd`; `CodexBlock(bin, configDir, stateDir string) string`; `UpsertCodexBlock(before, bin, configDir, stateDir string) (string, error)`; `RemoveCodexBlock(before string) (after string, found bool, err error)`; `Diff(before, after string) string`; `type Record struct{ Client Client; Scope string; Command []string; ConfigPath string; Backups []string; At time.Time }`; `RecordPath(configDir string) string` (= `CONFIG_DIR/mcp-install.json`); `LoadRecords(path string) ([]Record, error)`; `SaveRecords(path string, rs []Record) error`; `WithRecord(rs []Record, r Record) []Record`; `WithoutClient(rs []Record, c Client) []Record`; `StatusLine(configDir string) string`.
  - `package main`: `runSetupMCP(args []string) error` registered as `"setup-mcp"`; testable core `setupMCP(args []string, s setupIO) error` with `type setupIO struct{ in io.Reader; out io.Writer; run func([]string) (string, error); lookPath func(string) (string, error); env mcpinstall.Env; now func() time.Time; exe string; paths func() (paths.Paths, error) }`. Flags `--client claude|codex|both` (default `both`), `--remove`, `--yes`.
  - `manage.Deps.MCPStatus func() string` (nil = no line): read once in `manage.New`; the library view prints it dim under the status line. `runManage` passes `mcpinstall.StatusLine(p.ConfigDir)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/mcpinstall/install_test.go`:

```go
package mcpinstall

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	tBin   = "/opt/herdr dots/bin/herdr-dots"
	tCfg   = "/home/u/.config/herdr/plugins/herdr-dots"
	tState = "/home/u/.local/state/herdr/plugins/herdr-dots"
)

var tNow = time.Date(2026, 10, 8, 14, 30, 0, 0, time.UTC)

// tempEnv points both clients at a temp home; never the real one.
func tempEnv(t *testing.T) Env {
	t.Helper()
	return Env{Getenv: func(string) string { return "" }, Home: t.TempDir()}
}

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o640); err != nil {
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

func noRun(t *testing.T) func(Command) error {
	return func(c Command) error { t.Fatalf("unexpected command %v", c.Args); return nil }
}

const userCodex = `# Codex config, hand edited
model = "gpt-6.1-sol"

[mcp_servers.docs]
command = "docs-mcp"   # keep me
args = ["--port", "0"]

[profiles.fast]
model = "gpt-6-luna"
`

const wantBlock = "# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)\n" +
	"[mcp_servers.herdr-dots]\n" +
	"command = \"/opt/herdr dots/bin/herdr-dots\"\n" +
	"args = [\"mcp\", \"--config-dir\", \"/home/u/.config/herdr/plugins/herdr-dots\", \"--state-dir\", \"/home/u/.local/state/herdr/plugins/herdr-dots\"]\n" +
	"env_vars = [\"HERDR_PANE_ID\", \"HERDR_SOCKET_PATH\"]\n" +
	"startup_timeout_sec = 20\n" +
	"tool_timeout_sec = 180\n" +
	"# <<< herdr-dots MCP server\n"

func TestClaudePlanIsTheExactCLICommand(t *testing.T) {
	env := tempEnv(t)
	writeFile(t, env.ClaudeConfigPath(), `{"numStartups": 3}`)
	p, err := PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "mcp", "add", "--scope", "user",
		"--env", "HERDR_PANE_ID=${HERDR_PANE_ID:-}", "--env", "HERDR_SOCKET_PATH=${HERDR_SOCKET_PATH:-}",
		"--transport", "stdio", "herdr-dots", "--", tBin, "mcp", "--config-dir", tCfg, "--state-dir", tState}
	if len(p.Commands) != 2 || !p.Commands[0].MayFail || !slices.Equal(p.Commands[0].Args, []string{"claude", "mcp", "remove", "--scope", "user", "herdr-dots"}) ||
		p.Commands[1].MayFail || !slices.Equal(p.Commands[1].Args, want) {
		t.Fatalf("commands %+v", p.Commands)
	}
	if !slices.Equal(p.Backups, []string{env.ClaudeConfigPath()}) || len(p.Edits) != 0 {
		t.Fatalf("plan %+v: Claude's file is backed up and edited only by the claude CLI", p)
	}
	if !strings.Contains(p.Preview, `+   "herdr-dots": {`) || !strings.Contains(p.Preview, `"HERDR_PANE_ID": "${HERDR_PANE_ID:-}"`) {
		t.Fatalf("preview:\n%s", p.Preview)
	}
}

func TestClaudeConfigDirIsHonored(t *testing.T) {
	dir := t.TempDir()
	env := Env{Getenv: func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return dir
		}
		return ""
	}, Home: t.TempDir()}
	if got := env.ClaudeConfigPath(); got != filepath.Join(dir, ".claude.json") {
		t.Fatalf("path %s", got)
	}
}

// Review Focus 5: the user's Codex config keeps its other tables and
// comments, is backed up, and --remove restores it byte for byte.
func TestCodexInstallPreservesOtherTablesAndRemoveRestores(t *testing.T) {
	env := tempEnv(t)
	path := env.CodexConfigPath()
	writeFile(t, path, userCodex)
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Preview, "+ [mcp_servers.herdr-dots]") || strings.Contains(p.Preview, "- ") {
		t.Fatalf("preview:\n%s", p.Preview)
	}
	backups, err := Apply(p, noRun(t), tNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != userCodex+"\n"+wantBlock {
		t.Fatalf("after install:\n%s", got)
	}
	if len(backups) != 1 || backups[0] != path+".bak-herdr-dots-20261008-143000" || readFile(t, backups[0]) != userCodex {
		t.Fatalf("backups %v", backups)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v: the file keeps its mode", st.Mode().Perm())
	}
	if st, _ := os.Stat(backups[0]); st.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v", st.Mode().Perm())
	}

	again, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil || !again.Nothing {
		t.Fatalf("re-install must be a no-op: %+v %v", again, err)
	}

	rm, err := PlanRemove(ClientCodex, env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(rm, noRun(t), tNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != userCodex {
		t.Fatalf("remove must restore the original exactly:\n%q", got)
	}
	if rm2, _ := PlanRemove(ClientCodex, env); !rm2.Nothing {
		t.Fatalf("second remove: %+v", rm2)
	}
}

func TestCodexInstallIntoAMissingFile(t *testing.T) {
	env := tempEnv(t)
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Backups) != 0 {
		t.Fatalf("nothing to back up: %v", p.Backups)
	}
	if _, err := Apply(p, noRun(t), tNow); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, env.CodexConfigPath()); got != wantBlock {
		t.Fatalf("got:\n%s", got)
	}
	if st, _ := os.Stat(env.CodexConfigPath()); st.Mode().Perm() != 0o600 {
		t.Fatalf("a new config is private: %v", st.Mode().Perm())
	}
}

func TestCodexUnmarkedTableIsReplacedInPlace(t *testing.T) {
	before := "[mcp_servers.herdr-dots]\ncommand = \"/old/herdr-dots\"\nargs = [\"mcp\"]\n\n[mcp_servers.herdr-dots.env]\nX = \"1\"\n\n# profiles\n[profiles.fast]\nmodel = \"m\"\n"
	after, err := UpsertCodexBlock(before, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	if want := wantBlock + "\n# profiles\n[profiles.fast]\nmodel = \"m\"\n"; after != want {
		t.Fatalf("got:\n%s\nwant:\n%s", after, want)
	}
}

func TestCodexRefusesWhatItCannotEditSafely(t *testing.T) {
	for name, before := range map[string]string{
		"inline table":  "[mcp_servers]\nherdr-dots = { command = \"x\" }\n",
		"dotted keys":   "mcp_servers.herdr-dots.command = \"x\"\n",
		"invalid TOML":  "[mcp_servers.docs\ncommand = ",
		"no end marker": BlockBegin + "\n[mcp_servers.herdr-dots]\ncommand = \"x\"\n",
	} {
		if _, err := UpsertCodexBlock(before, tBin, tCfg, tState); !errors.Is(err, ErrUnsafeEdit) {
			t.Errorf("%s: install got %v", name, err)
		}
		if _, _, err := RemoveCodexBlock(before); !errors.Is(err, ErrUnsafeEdit) {
			t.Errorf("%s: remove got %v", name, err)
		}
	}
}

func TestApplyRefusesAFileChangedAfterThePreview(t *testing.T) {
	env := tempEnv(t)
	path := env.CodexConfigPath()
	writeFile(t, path, userCodex)
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, userCodex+"# edited meanwhile\n")
	if _, err := Apply(p, noRun(t), tNow); err == nil || !strings.Contains(err.Error(), "changed since the preview") {
		t.Fatalf("got %v", err)
	}
	if got := readFile(t, path); got != userCodex+"# edited meanwhile\n" {
		t.Fatal("the file must stay untouched")
	}
	if m, _ := filepath.Glob(path + ".bak-*"); len(m) != 0 {
		t.Fatalf("no backup without a change: %v", m)
	}
}

func TestApplyRunsCommandsAndIgnoresOnlyMayFail(t *testing.T) {
	env := tempEnv(t)
	p, _ := PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	var ran [][]string
	_, err := Apply(p, func(c Command) error {
		ran = append(ran, c.Args)
		if c.MayFail {
			return errors.New("No MCP server found")
		}
		return nil
	}, tNow)
	if err != nil || len(ran) != 2 {
		t.Fatalf("ran %v err %v", ran, err)
	}
	_, err = Apply(p, func(c Command) error {
		if !c.MayFail {
			return errors.New("exit status 1")
		}
		return nil
	}, tNow)
	if err == nil || !strings.Contains(err.Error(), "claude mcp add") {
		t.Fatalf("a failing add must fail the apply: %v", err)
	}
}

func TestPlanRefusesRelativeOrControlPaths(t *testing.T) {
	env := tempEnv(t)
	for _, bin := range []string{"bin/herdr-dots", "/bin/herdr\n-dots"} {
		if _, err := PlanInstall(ClientCodex, env, bin, tCfg, tState); err == nil {
			t.Errorf("bin %q accepted", bin)
		}
	}
}

func TestRecordsAndStatusLine(t *testing.T) {
	dir := t.TempDir()
	if got := StatusLine(dir); got != "MCP: not installed (run herdr-dots setup-mcp)" {
		t.Fatalf("status %q", got)
	}
	rs := WithRecord(nil, Record{Client: ClientCodex, Scope: "user", At: tNow})
	rs = WithRecord(rs, Record{Client: ClientClaude, Scope: "user", At: tNow})
	rs = WithRecord(rs, Record{Client: ClientCodex, Scope: "user", At: tNow.Add(time.Hour)})
	if err := SaveRecords(RecordPath(dir), rs); err != nil {
		t.Fatal(err)
	}
	if got := StatusLine(dir); got != "MCP: installed for Claude Code, Codex" {
		t.Fatalf("status %q", got)
	}
	loaded, err := LoadRecords(RecordPath(dir))
	if err != nil || len(loaded) != 2 {
		t.Fatalf("loaded %+v %v", loaded, err)
	}
	if st, _ := os.Stat(RecordPath(dir)); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	if err := SaveRecords(RecordPath(dir), WithoutClient(loaded, ClientClaude)); err != nil {
		t.Fatal(err)
	}
	if got := StatusLine(dir); got != "MCP: installed for Codex" {
		t.Fatalf("status %q", got)
	}
}

func TestDiffShowsChangesWithContext(t *testing.T) {
	d := Diff("a\nb\nc\nd\ne\nf\n", "a\nb\nc\nX\ne\nf\n")
	if d != "  b\n  c\n- d\n+ X\n  e\n  f\n" {
		t.Fatalf("diff:\n%s", d)
	}
}
```

Create `cmd/herdr-dots/setupmcp_test.go` (fake CLIs and a temp home: nothing reads or writes the real `~/.claude.json` or `~/.codex`):

```go
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/paths"
)

// mcpSetupRig fakes every outside effect of setup-mcp. Nothing here reads or
// writes the real ~/.claude.json or ~/.codex: the home is a temp dir.
type mcpSetupRig struct {
	io   setupIO
	out  *bytes.Buffer
	ran  [][]string
	home string
	p    paths.Paths
}

func newMCPSetupRig(t *testing.T, onPath ...string) *mcpSetupRig {
	t.Helper()
	r := &mcpSetupRig{out: &bytes.Buffer{}, home: t.TempDir()}
	r.p = paths.New(filepath.Join(r.home, "cfg"), filepath.Join(r.home, "state"), "")
	if err := os.MkdirAll(r.p.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	r.io = setupIO{
		in:  strings.NewReader("y\n"),
		out: r.out,
		run: func(a []string) (string, error) {
			r.ran = append(r.ran, a)
			return "ok: " + strings.Join(a[:3], " ") + "\n", nil
		},
		lookPath: func(bin string) (string, error) {
			for _, b := range onPath {
				if b == bin {
					return "/usr/bin/" + bin, nil
				}
			}
			return "", errors.New("not found")
		},
		env:   mcpinstall.Env{Getenv: func(string) string { return "" }, Home: r.home},
		now:   func() time.Time { return time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC) },
		exe:   "/opt/herdr-dots/bin/herdr-dots",
		paths: func() (paths.Paths, error) { return r.p, nil },
	}
	return r
}

func (r *mcpSetupRig) codexConfig() string { return r.io.env.CodexConfigPath() }

func TestSetupMCPInstallsForClaudeAfterConfirmation(t *testing.T) {
	r := newMCPSetupRig(t, "claude")
	if err := setupMCP([]string{"--client", "claude"}, r.io); err != nil {
		t.Fatal(err)
	}
	out := r.out.String()
	if !strings.Contains(out, "Will run: claude mcp add --scope user --env 'HERDR_PANE_ID=${HERDR_PANE_ID:-}'") ||
		!strings.Contains(out, "Apply these changes? [y/N]") || !strings.Contains(out, "bypass the Dots approval queue") {
		t.Fatalf("output:\n%s", out)
	}
	if len(r.ran) != 3 || r.ran[1][2] != "add" || strings.Join(r.ran[2], " ") != "claude mcp get herdr-dots" {
		t.Fatalf("ran %v", r.ran)
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: installed for Claude Code" {
		t.Fatalf("status %q", got)
	}
}

func TestSetupMCPAnythingButYesCancels(t *testing.T) {
	r := newMCPSetupRig(t, "claude", "codex")
	r.io.in = strings.NewReader("\n")
	if err := setupMCP(nil, r.io); err != nil {
		t.Fatal(err)
	}
	if len(r.ran) != 0 || mcpFileExists(r.codexConfig()) || mcpFileExists(mcpinstall.RecordPath(r.p.ConfigDir)) {
		t.Fatalf("cancel must change nothing: ran %v", r.ran)
	}
	if !strings.Contains(r.out.String(), "Cancelled. Nothing changed.") {
		t.Fatalf("output:\n%s", r.out.String())
	}
}

func TestSetupMCPMissingClaudeIsAClearErrorWithNoEdits(t *testing.T) {
	r := newMCPSetupRig(t, "codex")
	err := setupMCP([]string{"--client", "claude", "--yes"}, r.io)
	if err == nil || !strings.Contains(err.Error(), "claude CLI not found on PATH") {
		t.Fatalf("got %v", err)
	}
	if len(r.ran) != 0 || mcpFileExists(r.codexConfig()) {
		t.Fatal("no partial edits")
	}
}

func TestSetupMCPBothSkipsAMissingClientAndInstallsCodex(t *testing.T) {
	r := newMCPSetupRig(t, "codex")
	if err := setupMCP([]string{"--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(r.codexConfig())
	if err != nil || !strings.Contains(string(b), "tool_timeout_sec = 180") || !strings.Contains(string(b), `env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]`) {
		t.Fatalf("codex config %q %v", b, err)
	}
	if !strings.Contains(r.out.String(), "Skipping Claude Code") || !strings.Contains(r.out.String(), "$ codex mcp list") {
		t.Fatalf("output:\n%s", r.out.String())
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: installed for Codex" {
		t.Fatalf("status %q", got)
	}

	// Remove restores the (empty) config and drops the record.
	r.out.Reset()
	if err := setupMCP([]string{"--remove", "--client", "codex", "--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(r.codexConfig()); len(b) != 0 {
		t.Fatalf("after remove: %q", b)
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: not installed (run herdr-dots setup-mcp)" {
		t.Fatalf("status %q", got)
	}
}

func TestSetupMCPRefusesGoRunBinaries(t *testing.T) {
	r := newMCPSetupRig(t, "claude")
	r.io.exe = "/var/folders/x/T/go-build123/b001/exe/herdr-dots"
	if err := setupMCP([]string{"--yes"}, r.io); err == nil || !strings.Contains(err.Error(), "go run") {
		t.Fatalf("got %v", err)
	}
}

func TestSetupMCPRejectsAnUnknownClient(t *testing.T) {
	r := newMCPSetupRig(t, "claude")
	if err := setupMCP([]string{"--client", "cursor"}, r.io); err == nil {
		t.Fatal("unknown client accepted")
	}
}
```

Create `internal/ui/manage/mcp_test.go`:

```go
package manage

import (
	"context"
	"strings"
	"testing"
)

func TestLibraryFooterShowsMCPInstallStatus(t *testing.T) {
	calls := 0
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil },
		MCPStatus: func() string { calls++; return "MCP: installed for Claude Code, Codex" }}
	m, _ := New("w1:p1", d).Update(SnapshotMsg(lib()))
	m = press(m, "j", "k")
	if v := m.View(); !strings.Contains(v, "MCP: installed for Claude Code, Codex") {
		t.Fatalf("view:\n%s", v)
	}
	if calls != 1 {
		t.Fatalf("MCPStatus read %d times; once when the popup opens", calls)
	}
}

func TestLibraryFooterWithoutMCPStatus(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	if strings.Contains(m.View(), "MCP:") {
		t.Fatal("no MCP line without Deps.MCPStatus")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/mcpinstall/ ./cmd/herdr-dots/ ./internal/ui/manage/`
Expected: compile errors (`no Go files in internal/mcpinstall` / `undefined: setupMCP`, `unknown field MCPStatus`).

- [ ] **Step 3: Write the installer package**

Create `internal/mcpinstall/install.go`:

```go
// Package mcpinstall installs and removes the Dots MCP server in Claude Code
// and Codex (`herdr-dots setup-mcp`). It computes a Plan that the command
// previews first; Apply backs up every file it touches before changing it,
// and refuses when a file changed after the preview.
package mcpinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"herdr-dots/internal/fsutil"
)

type Client string

const (
	ClientClaude Client = "claude"
	ClientCodex  Client = "codex"
)

// Title is the client's display name.
func (c Client) Title() string {
	if c == ClientCodex {
		return "Codex"
	}
	return "Claude Code"
}

// ServerName is the MCP server's name in both clients; tools show up as
// mcp__herdr-dots__<tool>.
const ServerName = "herdr-dots"

// ForwardEnv are the variables the server needs from the agent's pane.
var ForwardEnv = []string{"HERDR_PANE_ID", "HERDR_SOCKET_PATH"}

// Codex timeouts written into [mcp_servers.herdr-dots].
const (
	CodexStartupTimeoutSec = 20
	CodexToolTimeoutSec    = 180
)

// Env locates the clients' config files (tests point it at temp dirs).
type Env struct {
	Getenv func(string) string
	Home   string
}

func (e Env) get(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// ClaudeConfigPath is the file `claude mcp add --scope user` edits.
func (e Env) ClaudeConfigPath() string {
	if d := e.get("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	return filepath.Join(e.Home, ".claude.json")
}

// CodexConfigPath is Codex's user config.
func (e Env) CodexConfigPath() string {
	if d := e.get("CODEX_HOME"); d != "" {
		return filepath.Join(d, "config.toml")
	}
	return filepath.Join(e.Home, ".codex", "config.toml")
}

// Command is one CLI call; a MayFail command's failure is ignored (and its
// output hidden).
type Command struct {
	Args    []string
	MayFail bool
}

// FileEdit replaces a whole file: Before is what the preview showed.
type FileEdit struct {
	Path, Before, After string
}

// Plan is what setup-mcp shows and then applies for one client.
type Plan struct {
	Client   Client
	Remove   bool
	Commands []Command
	Edits    []FileEdit
	Backups  []string // copied aside before anything changes
	Verify   []string // shows the result afterwards
	Preview  string   // the change, as a diff or the expected entry
	Nothing  bool     // nothing to do
	Config   string   // the client's config file
}

// ErrUnsafeEdit: the config can't be edited without risking the user's file.
var ErrUnsafeEdit = errors.New("refusing to edit")

// ServerArgs are the server's arguments after the binary.
func ServerArgs(configDir, stateDir string) []string {
	return []string{"mcp", "--config-dir", configDir, "--state-dir", stateDir}
}

func checkInputs(bin, configDir, stateDir string) error {
	for _, p := range []string{bin, configDir, stateDir} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%q is not an absolute path", p)
		}
		if strings.IndexFunc(p, unicode.IsControl) >= 0 {
			return fmt.Errorf("%q contains control characters", p)
		}
	}
	return nil
}

// PlanInstall plans installing the server for c (user scope).
func PlanInstall(c Client, env Env, bin, configDir, stateDir string) (Plan, error) {
	if err := checkInputs(bin, configDir, stateDir); err != nil {
		return Plan{}, err
	}
	switch c {
	case ClientClaude:
		return planClaudeInstall(env, bin, configDir, stateDir), nil
	case ClientCodex:
		return planCodexInstall(env, bin, configDir, stateDir)
	}
	return Plan{}, fmt.Errorf("unknown client %q (want claude or codex)", c)
}

// PlanRemove plans removing the server from c.
func PlanRemove(c Client, env Env) (Plan, error) {
	switch c {
	case ClientClaude:
		p := Plan{Client: c, Remove: true, Config: env.ClaudeConfigPath(),
			Commands: []Command{{Args: claudeRemoveArgs()}},
			Verify:   []string{"claude", "mcp", "list"},
			Preview:  "Claude Code removes the \"" + ServerName + "\" entry from " + env.ClaudeConfigPath() + ".\n"}
		if exists(p.Config) {
			p.Backups = []string{p.Config}
		}
		return p, nil
	case ClientCodex:
		path := env.CodexConfigPath()
		before, err := readOptional(path)
		if err != nil {
			return Plan{}, err
		}
		after, found, err := RemoveCodexBlock(before)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", path, err)
		}
		p := Plan{Client: c, Remove: true, Config: path, Verify: []string{"codex", "mcp", "list"}}
		if !found {
			p.Nothing, p.Preview = true, "not installed in "+path+"\n"
			return p, nil
		}
		p.Edits = []FileEdit{{Path: path, Before: before, After: after}}
		p.Backups = []string{path}
		p.Preview = Diff(before, after)
		return p, nil
	}
	return Plan{}, fmt.Errorf("unknown client %q (want claude or codex)", c)
}

func claudeRemoveArgs() []string {
	return []string{"claude", "mcp", "remove", "--scope", "user", ServerName}
}

// planClaudeInstall removes any older entry (allowed to fail) and adds the
// server with the pane variables forwarded through ${VAR:-} expansion, so a
// Claude Code started outside Herdr hands the server empty values.
func planClaudeInstall(env Env, bin, configDir, stateDir string) Plan {
	add := []string{"claude", "mcp", "add", "--scope", "user"}
	for _, k := range ForwardEnv {
		add = append(add, "--env", k+"=${"+k+":-}")
	}
	add = append(add, "--transport", "stdio", ServerName, "--", bin)
	add = append(add, ServerArgs(configDir, stateDir)...)
	p := Plan{Client: ClientClaude, Config: env.ClaudeConfigPath(),
		Commands: []Command{{Args: claudeRemoveArgs(), MayFail: true}, {Args: add}},
		Verify:   []string{"claude", "mcp", "get", ServerName}}
	if exists(p.Config) {
		p.Backups = []string{p.Config}
	}
	entry := map[string]any{"type": "stdio", "command": bin, "args": ServerArgs(configDir, stateDir), "env": claudeEnv()}
	b, _ := json.MarshalIndent(map[string]any{ServerName: entry}, "", "  ")
	var pv strings.Builder
	fmt.Fprintf(&pv, "Claude Code stores this under \"mcpServers\" in %s (user scope):\n", p.Config)
	for _, l := range strings.Split(string(b), "\n") {
		pv.WriteString("+ " + l + "\n")
	}
	p.Preview = pv.String()
	return p
}

func claudeEnv() map[string]string {
	m := map[string]string{}
	for _, k := range ForwardEnv {
		m[k] = "${" + k + ":-}"
	}
	return m
}

func planCodexInstall(env Env, bin, configDir, stateDir string) (Plan, error) {
	path := env.CodexConfigPath()
	before, err := readOptional(path)
	if err != nil {
		return Plan{}, err
	}
	after, err := UpsertCodexBlock(before, bin, configDir, stateDir)
	if err != nil {
		return Plan{}, fmt.Errorf("%s: %w", path, err)
	}
	p := Plan{Client: ClientCodex, Config: path, Verify: []string{"codex", "mcp", "list"}}
	if before == after {
		p.Nothing, p.Preview = true, "already installed in "+path+"\n"
		return p, nil
	}
	p.Edits = []FileEdit{{Path: path, Before: before, After: after}}
	if exists(path) {
		p.Backups = []string{path}
	}
	p.Preview = Diff(before, after)
	return p, nil
}

// Apply carries out a plan: it checks that no file changed since the preview,
// backs up, writes the edits, then runs the commands. run gets each command;
// it returns the command's error.
func Apply(p Plan, run func(Command) error, now time.Time) ([]string, error) {
	if p.Nothing {
		return nil, nil
	}
	for _, e := range p.Edits {
		cur, err := readOptional(e.Path)
		if err != nil {
			return nil, err
		}
		if cur != e.Before {
			return nil, fmt.Errorf("%s changed since the preview; nothing was changed: run setup-mcp again", e.Path)
		}
	}
	var backups []string
	for _, f := range p.Backups {
		b, err := backup(f, now)
		if err != nil {
			return backups, fmt.Errorf("back up %s: %w", f, err)
		}
		backups = append(backups, b)
	}
	for _, e := range p.Edits {
		if err := writeKeepingMode(e.Path, e.After); err != nil {
			return backups, err
		}
	}
	for _, c := range p.Commands {
		if err := run(c); err != nil && !c.MayFail {
			return backups, fmt.Errorf("`%s` failed: %w", strings.Join(c.Args, " "), err)
		}
	}
	return backups, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readOptional reads a file; a missing file reads as "".
func readOptional(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// backup copies path to path.bak-herdr-dots-<time> (0600), never overwriting
// an older backup.
func backup(path string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	base := path + ".bak-herdr-dots-" + now.Format("20060102-150405")
	for i := 0; i < 100; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", err
		}
		return name, f.Close()
	}
	return "", fmt.Errorf("too many backups of %s", path)
}

// writeKeepingMode writes atomically through a dotfiles symlink, keeping the
// file's mode (0600 for a new file).
func writeKeepingMode(path, content string) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return fsutil.WriteFileAtomic(path, []byte(content), mode)
}
```

Create `internal/mcpinstall/codex.go`:

```go
package mcpinstall

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// The block setup-mcp writes into Codex's config.toml. Only these lines
// change; the rest of the file is never re-serialized.
const (
	BlockBegin = "# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)"
	BlockEnd   = "# <<< herdr-dots MCP server"
)

var (
	tableHeaderRe = regexp.MustCompile(`^\s*\[`)
	// ourHeaderRe matches [mcp_servers.herdr-dots] and its sub-tables.
	ourHeaderRe = regexp.MustCompile(`^\s*\[\s*mcp_servers\s*\.\s*(?:herdr-dots|"herdr-dots"|'herdr-dots')\s*(?:\.[^\]]*)?\]\s*(?:#.*)?$`)
)

type codexServer struct {
	Command        string   `toml:"command"`
	Args           []string `toml:"args"`
	EnvVars        []string `toml:"env_vars"`
	ToolTimeoutSec int      `toml:"tool_timeout_sec"`
}

type codexConfig struct {
	MCPServers map[string]codexServer `toml:"mcp_servers"`
}

func parseCodex(s string) (codexConfig, error) {
	var c codexConfig
	_, err := toml.Decode(s, &c)
	return c, err
}

// CodexBlock is the managed [mcp_servers.herdr-dots] table.
func CodexBlock(bin, configDir, stateDir string) string {
	var b strings.Builder
	b.WriteString(BlockBegin + "\n")
	b.WriteString("[mcp_servers." + ServerName + "]\n")
	fmt.Fprintf(&b, "command = %s\n", tomlString(bin))
	b.WriteString("args = " + tomlArray(ServerArgs(configDir, stateDir)) + "\n")
	b.WriteString("env_vars = " + tomlArray(ForwardEnv) + "\n")
	fmt.Fprintf(&b, "startup_timeout_sec = %d\n", CodexStartupTimeoutSec)
	fmt.Fprintf(&b, "tool_timeout_sec = %d\n", CodexToolTimeoutSec)
	b.WriteString(BlockEnd + "\n")
	return b.String()
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = tomlString(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// findRegion finds the server's lines: the managed block, else an unmarked
// [mcp_servers.herdr-dots] table with its sub-tables (without the blank and
// comment lines before the next table). end is exclusive.
func findRegion(lines []string) (start, end int, ok bool, err error) {
	for i, l := range lines {
		if strings.TrimSpace(l) != BlockBegin {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == BlockEnd {
				return i, j + 1, true, nil
			}
		}
		return 0, 0, false, fmt.Errorf("%w: the herdr-dots block has no end marker; fix it by hand", ErrUnsafeEdit)
	}
	start = -1
	for i, l := range lines {
		if start < 0 {
			if ourHeaderRe.MatchString(l) {
				start = i
			}
			continue
		}
		if tableHeaderRe.MatchString(l) && !ourHeaderRe.MatchString(l) {
			return start, trimTail(lines, start, i), true, nil
		}
	}
	if start >= 0 {
		return start, trimTail(lines, start, len(lines)), true, nil
	}
	return 0, 0, false, nil
}

// trimTail moves end back over blank and comment lines (they introduce the
// next table, not ours).
func trimTail(lines []string, start, end int) int {
	for end > start+1 {
		t := strings.TrimSpace(lines[end-1])
		if t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		end--
	}
	return end
}

// UpsertCodexBlock returns config.toml with the managed block in place: it
// replaces the managed block or an unmarked herdr-dots table, else appends
// the block. Every other line stays byte for byte. It refuses a file that
// isn't valid TOML or defines the server in a form it can't edit safely.
func UpsertCodexBlock(before, bin, configDir, stateDir string) (string, error) {
	old, err := parseCodex(before)
	if err != nil {
		return "", fmt.Errorf("%w: it is not valid TOML (%v); fix it first", ErrUnsafeEdit, err)
	}
	block := CodexBlock(bin, configDir, stateDir)
	lines := strings.SplitAfter(before, "\n")
	start, end, ok, err := findRegion(lines)
	if err != nil {
		return "", err
	}
	var after string
	switch {
	case ok:
		after = strings.Join(lines[:start], "") + block + strings.Join(lines[end:], "")
	case hasServer(old):
		return "", fmt.Errorf("%w: [mcp_servers.%s] is defined as an inline table or dotted keys; remove it by hand and run setup-mcp again", ErrUnsafeEdit, ServerName)
	default:
		after = before
		if after != "" && !strings.HasSuffix(after, "\n") {
			after += "\n"
		}
		if after != "" {
			after += "\n"
		}
		after += block
	}
	got, err := parseCodex(after)
	if err != nil {
		return "", fmt.Errorf("%w: the edit would make the file invalid TOML (%v); nothing changed", ErrUnsafeEdit, err)
	}
	s := got.MCPServers[ServerName]
	if s.Command != bin || !slices.Equal(s.Args, ServerArgs(configDir, stateDir)) || !slices.Equal(s.EnvVars, ForwardEnv) || s.ToolTimeoutSec != CodexToolTimeoutSec {
		return "", fmt.Errorf("%w: the edited file doesn't read back as expected; nothing changed", ErrUnsafeEdit)
	}
	if err := keptOthers(old, got); err != nil {
		return "", err
	}
	return after, nil
}

// RemoveCodexBlock deletes the server's lines. After an install that
// appended the block, the result equals the original file.
func RemoveCodexBlock(before string) (string, bool, error) {
	old, err := parseCodex(before)
	if err != nil {
		return "", false, fmt.Errorf("%w: it is not valid TOML (%v); fix it first", ErrUnsafeEdit, err)
	}
	lines := strings.SplitAfter(before, "\n")
	start, end, ok, err := findRegion(lines)
	if err != nil {
		return "", false, err
	}
	if !ok {
		if hasServer(old) {
			return "", false, fmt.Errorf("%w: [mcp_servers.%s] is defined as an inline table or dotted keys; remove it by hand", ErrUnsafeEdit, ServerName)
		}
		return before, false, nil
	}
	head, tail := strings.Join(lines[:start], ""), strings.Join(lines[end:], "")
	if tail == "" && strings.HasSuffix(head, "\n\n") {
		head = strings.TrimSuffix(head, "\n") // the blank line the install added
	}
	after := head + tail
	got, err := parseCodex(after)
	if err != nil {
		return "", false, fmt.Errorf("%w: the edit would make the file invalid TOML (%v); nothing changed", ErrUnsafeEdit, err)
	}
	if hasServer(got) {
		return "", false, fmt.Errorf("%w: [mcp_servers.%s] is still defined after removing the block; remove it by hand", ErrUnsafeEdit, ServerName)
	}
	if err := keptOthers(old, got); err != nil {
		return "", false, err
	}
	return after, true, nil
}

func hasServer(c codexConfig) bool {
	_, ok := c.MCPServers[ServerName]
	return ok
}

// keptOthers checks that every other MCP server survived the edit unchanged.
func keptOthers(before, after codexConfig) error {
	for name, s := range before.MCPServers {
		if name == ServerName {
			continue
		}
		if a, ok := after.MCPServers[name]; !ok || a.Command != s.Command || !slices.Equal(a.Args, s.Args) {
			return fmt.Errorf("%w: the edit would change the %q server; nothing changed", ErrUnsafeEdit, name)
		}
	}
	return nil
}
```

Create `internal/mcpinstall/diff.go`:

```go
package mcpinstall

import "strings"

// diffContext is how many unchanged lines show around each change.
const diffContext = 2

// Diff renders a line diff of before → after: "- " removed, "+ " added and
// "  " unchanged lines near a change; "  …" marks skipped lines.
func Diff(before, after string) string {
	a, b := splitLines(before), splitLines(after)
	if len(a)*len(b) > 4_000_000 { // far bigger than any config file
		var out strings.Builder
		for _, l := range a {
			out.WriteString("- " + l + "\n")
		}
		for _, l := range b {
			out.WriteString("+ " + l + "\n")
		}
		return out.String()
	}
	// lcs[i][j] = length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, op{'-', a[i]}) // removals print before additions
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	near := make([]bool, len(ops))
	for k, o := range ops {
		if o.kind == ' ' {
			continue
		}
		for d := max(0, k-diffContext); d <= min(len(ops)-1, k+diffContext); d++ {
			near[d] = true
		}
	}
	var out strings.Builder
	skipped := false
	for k, o := range ops {
		if !near[k] {
			skipped = true
			continue
		}
		if skipped && out.Len() > 0 {
			out.WriteString("  …\n")
		}
		skipped = false
		out.WriteString(string(o.kind) + " " + o.text + "\n")
	}
	return out.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
```

Create `internal/mcpinstall/records.go`:

```go
package mcpinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdr-dots/internal/fsutil"
)

// Record is one install that setup-mcp made, kept in CONFIG_DIR/mcp-install.json
// so --remove, upgrades and the manage popup know what is installed.
type Record struct {
	Client     Client    `json:"client"`
	Scope      string    `json:"scope"`   // "user"
	Command    []string  `json:"command"` // the server's command line
	ConfigPath string    `json:"config_path"`
	Backups    []string  `json:"backups,omitempty"`
	At         time.Time `json:"at"`
}

type recordsFile struct {
	Version  int      `json:"version"`
	Installs []Record `json:"installs"`
}

// RecordPath is CONFIG_DIR/mcp-install.json.
func RecordPath(configDir string) string { return filepath.Join(configDir, "mcp-install.json") }

// LoadRecords reads the install records; a missing file means none.
func LoadRecords(path string) ([]Record, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f recordsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.Installs, nil
}

// SaveRecords writes the records (0600, atomic).
func SaveRecords(path string, rs []Record) error {
	if rs == nil {
		rs = []Record{}
	}
	b, err := json.MarshalIndent(recordsFile{Version: 1, Installs: rs}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, append(b, '\n'), 0o600)
}

// WithRecord replaces the record for r.Client (or adds it).
func WithRecord(rs []Record, r Record) []Record {
	return append(WithoutClient(rs, r.Client), r)
}

// WithoutClient drops the record for c.
func WithoutClient(rs []Record, c Client) []Record {
	out := []Record{}
	for _, x := range rs {
		if x.Client != c {
			out = append(out, x)
		}
	}
	return out
}

// StatusLine is the manage popup's footer line.
func StatusLine(configDir string) string {
	rs, err := LoadRecords(RecordPath(configDir))
	if err != nil {
		return "MCP: install record unreadable (" + RecordPath(configDir) + ")"
	}
	if len(rs) == 0 {
		return "MCP: not installed (run herdr-dots setup-mcp)"
	}
	var names []string
	for _, c := range []Client{ClientClaude, ClientCodex} {
		for _, r := range rs {
			if r.Client == c {
				names = append(names, c.Title())
				break
			}
		}
	}
	return "MCP: installed for " + strings.Join(names, ", ")
}
```

- [ ] **Step 4: Write the command**

Create `cmd/herdr-dots/setupmcp.go`:

```go
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/paths"
)

// setupIO is everything setup-mcp touches, so tests can fake it.
type setupIO struct {
	in       io.Reader
	out      io.Writer
	run      func(args []string) (string, error) // combined output
	lookPath func(string) (string, error)
	env      mcpinstall.Env
	now      func() time.Time
	exe      string // absolute path of the herdr-dots binary to install
	paths    func() (paths.Paths, error)
}

func runSetupMCP(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	home, _ := os.UserHomeDir()
	return setupMCP(args, setupIO{
		in: os.Stdin, out: os.Stdout,
		run: func(a []string) (string, error) {
			out, err := exec.Command(a[0], a[1:]...).CombinedOutput()
			return string(out), err
		},
		lookPath: exec.LookPath,
		env:      mcpinstall.Env{Getenv: os.Getenv, Home: home},
		now:      time.Now,
		exe:      exe,
		paths:    paths.Discover,
	})
}

func setupMCP(args []string, s setupIO) error {
	fs := flag.NewFlagSet("setup-mcp", flag.ContinueOnError)
	client := fs.String("client", "both", "claude, codex or both")
	remove := fs.Bool("remove", false, "remove the server instead of installing it")
	yes := fs.Bool("yes", false, "apply without asking")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.Contains(s.exe, string(filepath.Separator)+"go-build") {
		return errors.New("run the built plugin binary (./bin/herdr-dots setup-mcp), not `go run`: the clients would point at a temporary file")
	}
	clients, err := s.pickClients(*client, *remove)
	if err != nil {
		return err
	}
	p, err := s.paths()
	if err != nil {
		return err
	}
	var plans []mcpinstall.Plan
	for _, c := range clients {
		var pl mcpinstall.Plan
		if *remove {
			pl, err = mcpinstall.PlanRemove(c, s.env)
		} else {
			pl, err = mcpinstall.PlanInstall(c, s.env, s.exe, p.ConfigDir, p.StateDir)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", c.Title(), err)
		}
		plans = append(plans, pl)
	}

	todo := 0
	for _, pl := range plans {
		fmt.Fprintf(s.out, "\n== %s ==\n", pl.Client.Title())
		if pl.Nothing {
			fmt.Fprintf(s.out, "Nothing to do: %s", pl.Preview)
			continue
		}
		todo++
		for _, c := range pl.Commands {
			fmt.Fprintf(s.out, "Will run: %s\n", mcpShellJoin(c.Args))
		}
		for _, e := range pl.Edits {
			fmt.Fprintf(s.out, "Will edit %s:\n", e.Path)
		}
		fmt.Fprint(s.out, pl.Preview)
		for _, b := range pl.Backups {
			fmt.Fprintf(s.out, "Backs up %s first.\n", b)
		}
	}
	if todo == 0 {
		fmt.Fprintln(s.out, "\nNothing changed.")
		return s.saveRecords(p, plans, nil, *remove)
	}
	if !*yes {
		fmt.Fprint(s.out, "\nApply these changes? [y/N] ")
		line, _ := bufio.NewReader(s.in).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Fprintln(s.out, "Cancelled. Nothing changed.")
			return nil
		}
	}
	backups := map[mcpinstall.Client][]string{}
	for _, pl := range plans {
		if pl.Nothing {
			continue
		}
		bs, err := mcpinstall.Apply(pl, func(c mcpinstall.Command) error {
			out, err := s.run(c.Args)
			if err == nil || !c.MayFail {
				fmt.Fprint(s.out, mcpIndent(out))
			}
			return err
		}, s.now())
		for _, b := range bs {
			fmt.Fprintf(s.out, "Backup: %s\n", b)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", pl.Client.Title(), err)
		}
		backups[pl.Client] = bs
		s.verify(pl)
	}
	if err := s.saveRecords(p, plans, backups, *remove); err != nil {
		return err
	}
	if *remove {
		fmt.Fprintln(s.out, "\nRemoved. Restart the agent sessions in your Herdr panes to drop the tools.")
		return nil
	}
	fmt.Fprintln(s.out, "\nInstalled. Restart Claude Code or Codex in your Herdr panes to load the tools (mcp__herdr-dots__*).")
	fmt.Fprintln(s.out, "Note: answers from these tools reach the main agent directly; they bypass the Dots approval queue.")
	return nil
}

// pickClients resolves --client. Claude needs the claude CLI (it edits its
// own config); Codex needs the codex CLI or an existing config file.
func (s setupIO) pickClients(flagVal string, remove bool) ([]mcpinstall.Client, error) {
	claudeOK := s.has("claude")
	codexOK := s.has("codex") || mcpFileExists(s.env.CodexConfigPath())
	switch flagVal {
	case "claude":
		if !claudeOK {
			return nil, errors.New("claude CLI not found on PATH: install Claude Code first; nothing changed")
		}
		return []mcpinstall.Client{mcpinstall.ClientClaude}, nil
	case "codex":
		if !codexOK && remove {
			return nil, fmt.Errorf("no Codex config at %s; nothing to remove", s.env.CodexConfigPath())
		}
		if !codexOK {
			return nil, fmt.Errorf("codex CLI not found on PATH and no %s: install Codex first; nothing changed", s.env.CodexConfigPath())
		}
		return []mcpinstall.Client{mcpinstall.ClientCodex}, nil
	case "both":
		var out []mcpinstall.Client
		if claudeOK {
			out = append(out, mcpinstall.ClientClaude)
		} else {
			fmt.Fprintln(s.out, "Skipping Claude Code: claude CLI not found on PATH.")
		}
		if codexOK {
			out = append(out, mcpinstall.ClientCodex)
		} else {
			fmt.Fprintln(s.out, "Skipping Codex: codex CLI not found on PATH and no "+s.env.CodexConfigPath()+".")
		}
		if len(out) == 0 {
			return nil, errors.New("neither Claude Code nor Codex found; nothing changed")
		}
		return out, nil
	}
	return nil, fmt.Errorf("--client %q: want claude, codex or both", flagVal)
}

func (s setupIO) has(bin string) bool {
	_, err := s.lookPath(bin)
	return err == nil
}

func (s setupIO) verify(pl mcpinstall.Plan) {
	if len(pl.Verify) == 0 {
		return
	}
	if !s.has(pl.Verify[0]) {
		fmt.Fprintf(s.out, "(%s is not on PATH: not verified)\n", pl.Verify[0])
		return
	}
	out, err := s.run(pl.Verify)
	fmt.Fprintf(s.out, "$ %s\n%s", mcpShellJoin(pl.Verify), mcpIndent(out))
	if err != nil {
		fmt.Fprintf(s.out, "(verification failed: %v)\n", err)
	}
}

// saveRecords updates mcp-install.json for the clients just handled.
func (s setupIO) saveRecords(p paths.Paths, plans []mcpinstall.Plan, backups map[mcpinstall.Client][]string, remove bool) error {
	path := mcpinstall.RecordPath(p.ConfigDir)
	rs, err := mcpinstall.LoadRecords(path)
	if err != nil {
		return err
	}
	for _, pl := range plans {
		switch {
		case remove:
			rs = mcpinstall.WithoutClient(rs, pl.Client)
		case pl.Nothing:
			if !mcpHasRecord(rs, pl.Client) {
				rs = mcpinstall.WithRecord(rs, s.record(p, pl, nil))
			}
		default:
			rs = mcpinstall.WithRecord(rs, s.record(p, pl, backups[pl.Client]))
		}
	}
	return mcpinstall.SaveRecords(path, rs)
}

func (s setupIO) record(p paths.Paths, pl mcpinstall.Plan, backups []string) mcpinstall.Record {
	return mcpinstall.Record{Client: pl.Client, Scope: "user", ConfigPath: pl.Config, Backups: backups, At: s.now(),
		Command: append([]string{s.exe}, mcpinstall.ServerArgs(p.ConfigDir, p.StateDir)...)}
}

func mcpHasRecord(rs []mcpinstall.Record, c mcpinstall.Client) bool {
	for _, r := range rs {
		if r.Client == c {
			return true
		}
	}
	return false
}

func mcpFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// mcpShellJoin quotes arguments for display.
func mcpShellJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r == '/' || r == '-' || r == '_' || r == '.' || r == ':' || r == '=' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
		}) < 0 {
			q[i] = a
			continue
		}
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

func mcpIndent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return "  " + strings.ReplaceAll(s, "\n", "\n  ") + "\n"
}
```

In `cmd/herdr-dots/main.go`, find:

```go
	"setup-keys": runSetupKeys,
```

Replace with:

```go
	"setup-keys": runSetupKeys,
	"setup-mcp":  runSetupMCP,
```

- [ ] **Step 5: Show the install status in the manage popup**

In `internal/ui/manage/model.go`, find:

```go
type Deps struct {
```

Replace with:

```go
type Deps struct {
	MCPStatus func() string // footer line about the MCP server install (Phase 5); nil = none

```

Find:

```go
type Model struct {
```

Replace with:

```go
type Model struct {
	mcpStatus string // Deps.MCPStatus, read once when the popup opens

```

Find the body of `New` (v1 form; earlier phases may have added fields to the literal, keep them):

```go
	return Model{d: d, target: target, selected: map[string]bool{}, attached: map[string]bool{}, templates: dots.Templates()}
}
```

Replace with:

```go
	m := Model{d: d, target: target, selected: map[string]bool{}, attached: map[string]bool{}, templates: dots.Templates()}
	if d.MCPStatus != nil {
		m.mcpStatus = d.MCPStatus()
	}
	return m
}
```

(If `New` no longer ends in a single `return Model{…}`, assign `m.mcpStatus` from `d.MCPStatus()` right before it returns the model.)

In `View`, library branch, find:

```go
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
```

Replace with:

```go
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
	if m.mcpStatus != "" {
		b.WriteString("\n" + style.Dim.Render(m.mcpStatus) + "\n")
	}
```

In `cmd/herdr-dots/ui.go` (`runManage`), add the field to the `manage.Deps{…}` literal (Phases 2 and 4 added fields to it; keep them):

```go
		MCPStatus: func() string { return mcpinstall.StatusLine(p.ConfigDir) },
```

and add `"herdr-dots/internal/mcpinstall"` to the imports. Run `gofmt -w internal/mcpinstall cmd/herdr-dots internal/ui/manage`.

- [ ] **Step 6: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/mcpinstall/ ./cmd/herdr-dots/ ./internal/ui/manage/`
Expected: PASS, including every earlier `cmd/herdr-dots` and `manage` test.

- [ ] **Step 7: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/mcpinstall cmd/herdr-dots internal/ui/manage
git commit -m "feat: herdr-dots setup-mcp installs the MCP server into Claude Code and Codex with preview, backup and --remove"
```

---

### Task 7: Engine: `Ask`, `ListDots`, `ListQueue`, `Query`, consulted guard, ask limits

**Depends on:** Task 2 (IPC types, `ipc.Querier`), Task 3 (`config.MCPConfig`, `state.OutcomeAsked`, `view.StatusConsulted`). Wave 2 (runs beside Task 8).

**Files:**
- Create: `internal/engine/ask.go`, `internal/engine/ask_test.go`
- Modify: `internal/engine/filter.go`, `internal/engine/engine.go`, `internal/engine/deliver.go`
- Shared with Phases 2, 3 and 4 (already merged): all three modified files

**Interfaces:**
- Consumes:
  - Task 2: `ipc.Querier`, `ipc.CmdListDots/CmdAsk/CmdListQueue`, `ipc.PaneArgs`, `ipc.AskArgs`, `ipc.ListQueueArgs`, `ipc.DotsResult`, `ipc.DotInfo`, `ipc.AskResult`, `ipc.QueueResult`, `ipc.CardInfo`, `ipc.QueuePending`, `ipc.QueueAll`.
  - Task 3: `config.Config.MCP`, `(config.MCPConfig) Normalized()`, `state.OutcomeAsked`, `view.StatusConsulted`.
  - v1: `Engine` (`mu`, `dots`, `status`, `pausedAll`, `ctx`, `d.Attachments`, `d.Usage`, `d.Queue`), `key`, `runOpts`, `runDot`, `e.spawn`, `e.reader`, `e.config()`, `e.feed`, `e.setStatus`, `processPaneLocked`, `dropPane`, `decode`, `tail`, `Decide`, `FilterInput`, `Reason*`; `textutil.SanitizeModelText`; test helpers `newRig`, `rig` (`e`, `be`, `q`, `usage`, `readers`), `dot`, `pane`, `critical`, `eventually`, `lastSeen`, `(*rig).attach`, `(*rig).turnEnds`, `fakeBackend` (`set`, `fail`, `n`, `last`).
  - Phase 2: `e.projectOf(att *state.Attachment) string`; in `runDot`, the memory block, `touchMemory`, `applyMemoryOps` (asks keep them).
  - Phase 3: `runOpts.sink func(backend.Result, error)`, `runOpts.ctx context.Context` (and `runDot` calling the sink exactly once); `errText(err error) string`.
  - Phase 4: `OverUSD`, `costDetail(d dots.Dot, res backend.Result) string`, `withCost(msg, detail string) string`, `Usage.Cost`, `Usage.PaneCost`, `dots.BudgetSpec.MaxUSDPerSession`, `config.Config.SessionBudgetUSD`, `backend.Result.CostUSD/CostKnown/Model`, `credentials.Redact`.
- Produces:
  - `var engine.ErrAskLimit` ("ask limit reached; continue without the dot"), `ErrNotAttached` ("no attached dot"), `ErrPaused` ("paused"), `ErrBudget` ("budget reached"), `ErrPaneClosed` ("pane closed").
  - `(*Engine) Query(ctx context.Context, cmd string, raw json.RawMessage) (any, error)` (so `*Engine` is an `ipc.Querier`; Task 9 adds `CmdAskCouncil`); unknown query → error; missing pane id → `missing pane id`.
  - `(*Engine) Ask(ctx context.Context, paneID, slug, question string, includeTurns bool) (ipc.AskResult, error)`; `(*Engine) ListDots(paneID string) (ipc.DotsResult, error)`; `(*Engine) ListQueue(paneID, status string) (ipc.QueueResult, error)`.
  - `engine.FilterInput.Consulted bool`; `engine.ReasonConsulted Reason = "consulted"`.
  - `runOpts.ask bool`; Engine fields `consulted map[key]bool` and `asks *askLimiter` (guarded by `e.mu`, created lazily).
  - Unexported helpers Task 9 uses: `(*Engine) limiter() *askLimiter`, `(*askLimiter) take(paneID string, lim config.MCPConfig) error`, `(*Engine) markConsulted(paneID string, slugs ...string)`, `decodePane(raw json.RawMessage, v any, pane *string) error`; also `newAskLimiter(now func() time.Time)`, `endTurn`, `forget`, `forgetAsks`, `askable`, `askQuestion`, `askDetail`, `purposeSummary`, `clipAsk`.
  - Behavior: an ask runs `runDot` with `summon: true, ask: true` and a sink, in a tracked goroutine, ending with the request or the engine; `runDot` with `ask` writes feed `asked` (`askDetail`), sets status `consulted`, never routes, and sets no cooldown. `processPaneLocked` reads and clears `consulted[k]` into `Decide`, sets status `consulted` for `ReasonConsulted`, and resets the per-turn ask count when the pane had new turns. `dropPane` forgets the pane's asks.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/ask_test.go` (Review Focus 2 included):

```go
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

func mcpAsk(t *testing.T, r *rig, slug, q string, include bool) (ipc.AskResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.e.Ask(ctx, pane, slug, q, include)
}

func mcpAskedFeed(r *rig, slug string) []state.FeedEntry {
	p, _ := r.e.Snapshot().Pane(pane)
	var out []state.FeedEntry
	for _, e := range p.Feed {
		if e.Outcome == state.OutcomeAsked && e.Dot == slug {
			out = append(out, e)
		}
	}
	return out
}

func mcpDotStatus(r *rig, slug string) string {
	p, _ := r.e.Snapshot().Pane(pane)
	for _, d := range p.Dots {
		if d.Slug == slug {
			return d.Status
		}
	}
	return ""
}

func TestAskReturnsTheVerdictWithoutACard(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.readers["s1"].add(transcript.Turn{User: "store the token in localStorage"})
	r.be.set(critical)
	res, err := mcpAsk(t, r, "security", "Is localStorage OK for the JWT?", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Slug != "security" || res.Name != "Security" || !res.Relevant || res.Severity != "critical" || res.Message != "JWT in localStorage" {
		t.Fatalf("result %+v", res)
	}
	req := r.be.last()
	if !req.Force || !strings.Contains(req.Question, "Is localStorage OK for the JWT?") || !strings.Contains(req.Question, "main coding agent") || len(req.Turns) != 1 {
		t.Fatalf("request %+v", req)
	}
	if n := len(r.q.Pending(pane)); n != 0 {
		t.Fatalf("an ask must not create a card (%d pending)", n)
	}
	if r.usage.Calls(pane, "security") != 1 {
		t.Fatalf("usage %d", r.usage.Calls(pane, "security"))
	}
	if f := mcpAskedFeed(r, "security"); len(f) != 1 || !strings.HasPrefix(f[0].Detail, "critical") {
		t.Fatalf("feed %+v", f)
	}
	if st := mcpDotStatus(r, "security"); st != view.StatusConsulted {
		t.Fatalf("status %q", st)
	}
}

func TestAskWithoutTurnsSendsNone(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.readers["s1"].add(transcript.Turn{User: "one"})
	if _, err := mcpAsk(t, r, "security", "general question", false); err != nil {
		t.Fatal(err)
	}
	if req := r.be.last(); len(req.Turns) != 0 {
		t.Fatalf("turns %+v", req.Turns)
	}
}

// Spec success criterion 5: a consulted dot skips the turn it was asked in,
// and only that one. A relevant answer sets no cooldown either.
func TestConsultedDotSkipsTheNextTurnOnly(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Trigger.Cooldown = 2
	r := newRig(t, d)
	r.attach(t, "security")
	r.be.set(critical)
	if _, err := mcpAsk(t, r, "security", "check auth.ts", false); err != nil {
		t.Fatal(err)
	}
	r.turnEnds(t, transcript.Turn{User: "write auth.ts"})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "security") == 1 })
	time.Sleep(50 * time.Millisecond)
	if r.be.n() != 1 {
		t.Fatalf("the consulted dot reviewed its turn anyway (%d runs)", r.be.n())
	}
	if st := mcpDotStatus(r, "security"); st != view.StatusConsulted {
		t.Fatalf("status %q", st)
	}
	r.turnEnds(t, transcript.Turn{User: "next change"})
	eventually(t, "the dot reviews the following turn", func() bool { return r.be.n() == 2 })
}

// Review Focus 2: an agent asking in a loop is cut off on the 4th ask of a
// turn, before anything runs or is spent; the next turn starts over.
func TestFourthAskInATurnIsRefusedWithoutARun(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	for i := range 3 {
		if _, err := mcpAsk(t, r, "security", "q", false); err != nil {
			t.Fatalf("ask %d: %v", i+1, err)
		}
	}
	_, err := mcpAsk(t, r, "security", "q", false)
	if !errors.Is(err, ErrAskLimit) || !strings.Contains(err.Error(), "continue without the dot (3 per turn)") {
		t.Fatalf("got %v", err)
	}
	if r.be.n() != 3 || r.usage.Calls(pane, "security") != 3 {
		t.Fatalf("runs %d, usage %d", r.be.n(), r.usage.Calls(pane, "security"))
	}
	r.turnEnds(t, transcript.Turn{User: "next"})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "security") == 1 })
	if _, err := mcpAsk(t, r, "security", "q", false); err != nil {
		t.Fatalf("a new turn resets the per-turn limit: %v", err)
	}
}

func TestAskLimiterPerMinuteWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newAskLimiter(func() time.Time { return now })
	lim := config.MCPConfig{MaxAsksPerTurn: 50, MaxAsksPerMinute: 6}
	for i := range 6 {
		if err := l.take("p", lim); err != nil {
			t.Fatalf("ask %d: %v", i+1, err)
		}
		now = now.Add(time.Second)
	}
	if err := l.take("p", lim); !errors.Is(err, ErrAskLimit) || !strings.Contains(err.Error(), "6 per minute") {
		t.Fatalf("7th: %v", err)
	}
	if err := l.take("other", lim); err != nil {
		t.Fatalf("limits are per pane: %v", err)
	}
	now = now.Add(55 * time.Second) // the first ask is now more than a minute old
	if err := l.take("p", lim); err != nil {
		t.Fatalf("window must slide: %v", err)
	}
	l.endTurn("p")
	l.forget("p")
	if err := l.take("p", config.MCPConfig{MaxAsksPerTurn: 1, MaxAsksPerMinute: 1}); err != nil {
		t.Fatalf("forget clears everything: %v", err)
	}
}

func TestAskRefusesPausedUnattachedAndOverBudget(t *testing.T) {
	sec := dot("Security", dots.ModeQueue)
	sec.Budget.MaxCallsPerSession = 1
	r := newRig(t, sec, dot("Frontend", dots.ModeQueue), dot("Copy", dots.ModeQueue))
	r.attach(t, "security", "copy")

	if _, err := mcpAsk(t, r, "frontend", "q", false); !errors.Is(err, ErrNotAttached) || !strings.Contains(err.Error(), "attached: security, copy") {
		t.Fatalf("unattached: %v", err)
	}
	if err := r.e.SetPaused(pane, "copy", true); err != nil {
		t.Fatal(err)
	}
	if _, err := mcpAsk(t, r, "copy", "q", false); !errors.Is(err, ErrPaused) {
		t.Fatalf("paused: %v", err)
	}
	_ = r.usage.Record(pane, "security", 1, 1)
	if _, err := mcpAsk(t, r, "security", "q", false); !errors.Is(err, ErrBudget) {
		t.Fatalf("budget: %v", err)
	}
	r.e.TogglePauseAll()
	if _, err := mcpAsk(t, r, "security", "q", false); !errors.Is(err, ErrPaused) {
		t.Fatalf("pause all: %v", err)
	}
	if r.be.n() != 0 {
		t.Fatalf("refused asks must not run: %d", r.be.n())
	}
	r2 := newRig(t, dot("Security", dots.ModeQueue))
	if _, err := mcpAsk(t, r2, "security", "q", false); !errors.Is(err, ErrNotAttached) {
		t.Fatalf("no attachment: %v", err)
	}
}

func TestFailedAskIsAnErrorAndDoesNotConsult(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.be.fail(errors.New("claude is not logged in"))
	_, err := mcpAsk(t, r, "security", "q", false)
	if err == nil || !strings.Contains(err.Error(), "Security failed: claude is not logged in") {
		t.Fatalf("got %v", err)
	}
	r.be.fail(nil)
	r.turnEnds(t, transcript.Turn{User: "one"})
	eventually(t, "the dot still reviews the turn", func() bool { return r.be.n() == 2 })
}

func TestListDotsAndListQueue(t *testing.T) {
	sec := dot("Security", dots.ModeQueue)
	sec.Purpose = strings.Repeat("Find auth bugs.\n", 30)
	r := newRig(t, sec)
	if res, err := r.e.ListDots(pane); err != nil || len(res.Dots) != 0 || res.Dots == nil {
		t.Fatalf("unattached pane: %+v %v", res, err)
	}
	r.attach(t, "security")
	res, err := r.e.ListDots(pane)
	if err != nil || len(res.Dots) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	di := res.Dots[0]
	if di.Slug != "security" || di.Name != "Security" || di.Mode != "queue" || di.Budget != sec.Budget.MaxCallsPerSession ||
		len([]rune(di.PurposeSummary)) != 200 || strings.Contains(di.PurposeSummary, "\n") {
		t.Fatalf("dot info %+v", di)
	}

	for i := range 22 {
		c, err := r.q.Add(queue.Card{Dot: "security", DotName: "Security", PaneID: pane, Turn: i, Severity: "warn", Body: strings.Repeat("x", 3000), Mode: "queue"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			_ = r.q.SetStatus(c.ID, queue.StatusDismissed)
		}
	}
	q, err := r.e.ListQueue(pane, "")
	if err != nil || q.Total != 21 || len(q.Cards) != 20 || len([]rune(q.Cards[0].Body)) != 2000 || !strings.HasSuffix(q.Cards[0].Body, "…") {
		t.Fatalf("pending: total %d cards %d %v", q.Total, len(q.Cards), err)
	}
	all, err := r.e.ListQueue(pane, ipc.QueueAll)
	if err != nil || all.Total != 22 || all.Cards[0].Turn != 21 {
		t.Fatalf("all: total %d first turn %d %v", all.Total, all.Cards[0].Turn, err)
	}
	if _, err := r.e.ListQueue(pane, "dismissed"); err == nil {
		t.Fatal("unknown status accepted")
	}
}

// The engine answers queries over its real socket (ipc.Querier).
func TestQueriesOverTheEngineSocket(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.be.set(critical)
	dir, err := os.MkdirTemp("", "engq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "d.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go ipc.Serve(ctx, ln, r.e)
	c := ipc.Client{Path: filepath.Join(dir, "d.sock")}
	qctx, qcancel := context.WithTimeout(ctx, 5*time.Second)
	defer qcancel()
	var res ipc.AskResult
	if err := c.Query(qctx, ipc.CmdAsk, ipc.AskArgs{PaneID: pane, Slug: "security", Question: "q"}, &res); err != nil || res.Severity != "critical" {
		t.Fatalf("ask: %+v %v", res, err)
	}
	var dl ipc.DotsResult
	if err := c.Query(qctx, ipc.CmdListDots, ipc.PaneArgs{PaneID: pane}, &dl); err != nil || len(dl.Dots) != 1 {
		t.Fatalf("list: %+v %v", dl, err)
	}
	if err := c.Query(qctx, ipc.CmdListDots, ipc.PaneArgs{}, &dl); err == nil || !strings.Contains(err.Error(), "missing pane id") {
		t.Fatalf("no pane: %v", err)
	}
	if _, err := r.e.Query(qctx, "nope", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown query accepted")
	}
}

func TestDecideConsulted(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	in := FilterInput{Dot: d, NewTurns: []transcript.Turn{{Index: 1, User: "x"}}, PaneBudget: 100, Consulted: true}
	if dec := Decide(in); dec.Run || dec.Reason != ReasonConsulted {
		t.Fatalf("decision %+v", dec)
	}
	in.Att.Paused = true
	if dec := Decide(in); dec.Reason != ReasonPaused {
		t.Fatalf("paused wins: %+v", dec)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/engine/`
Expected: compile errors (`r.e.Ask undefined`, `undefined: ErrAskLimit`, `unknown field Consulted in struct literal`, `undefined: ReasonConsulted`, `undefined: newAskLimiter`).

- [ ] **Step 3: Add the consulted reason to the filter**

In `internal/engine/filter.go`, find:

```go
	ReasonSessionBudget Reason = "session_budget"
```

Replace with:

```go
	ReasonSessionBudget Reason = "session_budget"
	ReasonConsulted     Reason = "consulted" // asked through MCP during these turns (Phase 5)
```

Find:

```go
type FilterInput struct {
```

Replace with:

```go
type FilterInput struct {
	Consulted bool // the dot was asked through the MCP server during these turns: skip them once

```

Find:

```go
	case !in.Dot.Trigger.OnTurn:
		return Decision{Reason: ReasonOff}
	}
```

Replace with:

```go
	case !in.Dot.Trigger.OnTurn:
		return Decision{Reason: ReasonOff}
	case in.Consulted:
		return Decision{Reason: ReasonConsulted}
	}
```

- [ ] **Step 4: Write `ask.go`**

Create `internal/engine/ask.go`:

```go
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// Errors the Dots MCP server shows the agent (Phase 5).
var (
	ErrAskLimit    = errors.New("ask limit reached; continue without the dot")
	ErrNotAttached = errors.New("no attached dot")
	ErrPaused      = errors.New("paused")
	ErrBudget      = errors.New("budget reached")
	ErrPaneClosed  = errors.New("pane closed")
)

const (
	maxAskQuestion      = 4000 // runes
	purposeSummaryChars = 200
	maxQueueCards       = 20
	maxQueueBody        = 2000
)

// Query implements ipc.Querier: the data-returning commands of the Dots MCP
// server. Each is scoped to the pane in its arguments.
func (e *Engine) Query(ctx context.Context, cmd string, raw json.RawMessage) (any, error) {
	switch cmd {
	case ipc.CmdListDots:
		var a ipc.PaneArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.ListDots(a.PaneID)
		return r, err
	case ipc.CmdAsk:
		var a ipc.AskArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.Ask(ctx, a.PaneID, a.Slug, a.Question, a.IncludeTurns)
		return r, err
	case ipc.CmdListQueue:
		var a ipc.ListQueueArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.ListQueue(a.PaneID, a.Status)
		return r, err
	}
	return nil, fmt.Errorf("unknown query %q", cmd)
}

// decodePane decodes query arguments and requires a pane id.
func decodePane(raw json.RawMessage, v any, pane *string) error {
	if err := decode(raw, v); err != nil {
		return err
	}
	if strings.TrimSpace(*pane) == "" {
		return errors.New("missing pane id")
	}
	return nil
}

// askLimiter enforces [mcp] max_asks_per_turn and max_asks_per_minute per
// pane. Its mutex is a leaf lock.
type askLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	turn   map[string]int
	recent map[string][]time.Time
}

func newAskLimiter(now func() time.Time) *askLimiter {
	return &askLimiter{now: now, turn: map[string]int{}, recent: map[string][]time.Time{}}
}

// take counts one ask for the pane, or refuses with ErrAskLimit.
func (l *askLimiter) take(paneID string, lim config.MCPConfig) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var keep []time.Time
	for _, t := range l.recent[paneID] {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	l.recent[paneID] = keep
	if l.turn[paneID] >= lim.MaxAsksPerTurn {
		return fmt.Errorf("%w (%d per turn)", ErrAskLimit, lim.MaxAsksPerTurn)
	}
	if len(keep) >= lim.MaxAsksPerMinute {
		return fmt.Errorf("%w (%d per minute)", ErrAskLimit, lim.MaxAsksPerMinute)
	}
	l.turn[paneID]++
	l.recent[paneID] = append(keep, now)
	return nil
}

// endTurn starts the pane's per-turn count over.
func (l *askLimiter) endTurn(paneID string) {
	l.mu.Lock()
	delete(l.turn, paneID)
	l.mu.Unlock()
}

func (l *askLimiter) forget(paneID string) {
	l.mu.Lock()
	delete(l.turn, paneID)
	delete(l.recent, paneID)
	l.mu.Unlock()
}

func (e *Engine) limiter() *askLimiter {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.asks == nil {
		e.asks = newAskLimiter(time.Now)
	}
	return e.asks
}

// forgetAsks drops a closed pane's ask counters and consultations.
func (e *Engine) forgetAsks(paneID string) {
	e.limiter().forget(paneID)
	e.mu.Lock()
	for k := range e.consulted {
		if k.pane == paneID {
			delete(e.consulted, k)
		}
	}
	e.mu.Unlock()
}

// markConsulted makes the dot skip the turn it was asked in (Decide's
// ReasonConsulted, cleared by processPaneLocked).
func (e *Engine) markConsulted(paneID string, slugs ...string) {
	e.mu.Lock()
	if e.consulted == nil {
		e.consulted = map[key]bool{}
	}
	for _, s := range slugs {
		e.consulted[key{paneID, s}] = true
	}
	e.mu.Unlock()
}

// askable returns the attached dot if it may be asked now: not paused, and
// with call and dollar budget left (Decide's checks without the turn-based
// ones).
func (e *Engine) askable(paneID, slug string) (dots.Dot, error) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return dots.Dot{}, fmt.Errorf("%w '%s'; no dots are attached to this pane (the developer attaches them with prefix+d)", ErrNotAttached, slug)
	}
	da := att.Find(slug)
	if da == nil {
		names := make([]string, 0, len(att.Dots))
		for _, x := range att.Dots {
			names = append(names, x.Slug)
		}
		return dots.Dot{}, fmt.Errorf("%w '%s'; attached: %s", ErrNotAttached, slug, strings.Join(names, ", "))
	}
	e.mu.Lock()
	d, known := e.dots[slug]
	pausedAll := e.pausedAll
	e.mu.Unlock()
	if !known {
		return dots.Dot{}, fmt.Errorf("%w '%s': its dot file is missing or invalid", ErrNotAttached, slug)
	}
	switch {
	case pausedAll:
		return d, fmt.Errorf("all dots are %w", ErrPaused)
	case da.Paused:
		return d, fmt.Errorf("%s is %w", d.Name, ErrPaused)
	}
	cfg := e.config()
	if e.d.Usage.Calls(paneID, slug) >= d.Budget.MaxCallsPerSession || OverUSD(e.d.Usage.Cost(paneID, slug), d.Budget.MaxUSDPerSession) {
		return d, fmt.Errorf("%s: %w for this agent session", d.Name, ErrBudget)
	}
	if e.d.Usage.PaneCalls(paneID) >= cfg.SessionBudgetCalls || OverUSD(e.d.Usage.PaneCost(paneID), cfg.SessionBudgetUSD) {
		return d, fmt.Errorf("the pane's session %w", ErrBudget)
	}
	return d, nil
}

// Ask runs one dot on the pane now and returns its answer (ask_dot). It is
// a forced run with the dot's memory, backend and budgets; it makes no queue
// card, logs an "asked" feed line, applies the dot's memory ops, and marks
// the dot consulted so it skips the turn it was asked in.
func (e *Engine) Ask(ctx context.Context, paneID, slug, question string, includeTurns bool) (ipc.AskResult, error) {
	q := clipAsk(strings.TrimSpace(textutil.SanitizeModelText(question)), maxAskQuestion)
	if q == "" {
		return ipc.AskResult{}, errors.New("the question is empty")
	}
	d, err := e.askable(paneID, slug)
	if err != nil {
		return ipc.AskResult{}, err
	}
	if err := e.limiter().take(paneID, e.config().MCP.Normalized()); err != nil {
		return ipc.AskResult{}, err
	}
	var turns []transcript.Turn
	if includeTurns {
		att, _ := e.d.Attachments.Get(paneID)
		ts, err := e.reader(att).Turns(ctx)
		if err != nil {
			return ipc.AskResult{}, fmt.Errorf("read conversation: %w", err)
		}
		turns = tail(ts, d.Context.Turns)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel) // an engine shutdown ends the ask
	defer stop()
	type outcome struct {
		res backend.Result
		err error
	}
	got := make(chan outcome, 1)
	o := runOpts{summon: true, ask: true, question: askQuestion(q), ctx: runCtx,
		sink: func(res backend.Result, err error) { got <- outcome{res, err} }}
	e.spawn(func() { e.runDot(paneID, d, turns, o) })
	var out outcome
	select {
	case out = <-got:
	case <-runCtx.Done():
		return ipc.AskResult{}, fmt.Errorf("%s: %w", d.Name, runCtx.Err())
	}
	if out.err != nil {
		return ipc.AskResult{}, fmt.Errorf("%s failed: %s", d.Name, errText(errors.New(credentials.Redact(out.err.Error()))))
	}
	if _, ok := e.d.Attachments.Get(paneID); !ok {
		return ipc.AskResult{}, ErrPaneClosed
	}
	e.markConsulted(paneID, slug)
	v := out.res.Verdict
	model := out.res.Model
	if model == "" {
		model = d.Backend.Model
	}
	return ipc.AskResult{Slug: slug, Name: d.Name, Relevant: v.Relevant, Severity: v.Severity,
		Message: strings.TrimSpace(textutil.SanitizeModelText(v.Message)), CostUSD: out.res.CostUSD,
		CostKnown: out.res.CostKnown, Model: model}, nil
}

// askQuestion tells the dot who is asking.
func askQuestion(q string) string {
	return "(The main coding agent is asking you directly, on the developer's behalf, through the Dots MCP server.)\n" + q
}

// askDetail is an "asked" feed line's detail: the severity (or "pass"),
// then the cost detail.
func askDetail(d dots.Dot, res backend.Result) string {
	sev := "pass"
	if res.Verdict.Relevant {
		sev = res.Verdict.Severity
	}
	return withCost(sev, costDetail(d, res))
}

// ListDots is list_dots: the pane's project and its attached dots.
func (e *Engine) ListDots(paneID string) (ipc.DotsResult, error) {
	out := ipc.DotsResult{Dots: []ipc.DotInfo{}}
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return out, nil
	}
	out.ProjectID = e.projectOf(&att) // may run git: outside every engine lock
	out.ProjectName = att.ProjectName
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, da := range att.Dots {
		d, ok := e.dots[da.Slug]
		if !ok {
			d = dots.Dot{Slug: da.Slug, Name: da.Slug}
		}
		st := e.status[key{paneID, da.Slug}]
		if st == "" {
			st = view.StatusIdle
		}
		if da.Paused && st != view.StatusError {
			st = view.StatusPaused
		}
		out.Dots = append(out.Dots, ipc.DotInfo{Slug: da.Slug, Name: d.Name, PurposeSummary: purposeSummary(d.Purpose),
			Mode: string(d.Delivery.Mode), Status: st, Paused: da.Paused || e.pausedAll,
			Calls: e.d.Usage.Calls(paneID, da.Slug), Budget: d.Budget.MaxCallsPerSession, CostUSD: e.d.Usage.Cost(paneID, da.Slug)})
	}
	return out, nil
}

// ListQueue is list_queue: pending (and auto) cards, or every card of the
// pane newest first; at most 20, bodies clipped to 2,000 runes.
func (e *Engine) ListQueue(paneID, status string) (ipc.QueueResult, error) {
	var cards []queue.Card
	switch status {
	case "", ipc.QueuePending:
		cards = e.d.Queue.Pending(paneID)
	case ipc.QueueAll:
		for _, c := range e.d.Queue.All() {
			if c.PaneID == paneID {
				cards = append(cards, c)
			}
		}
		// The store keeps finished cards last, so sort by age, newest first.
		slices.SortStableFunc(cards, func(a, b queue.Card) int {
			if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
				return c
			}
			x, _ := strconv.Atoi(a.ID)
			y, _ := strconv.Atoi(b.ID)
			return y - x
		})
	default:
		return ipc.QueueResult{}, fmt.Errorf("status %q: want pending or all", status)
	}
	out := ipc.QueueResult{Cards: []ipc.CardInfo{}, Total: len(cards)}
	for _, c := range cards[:min(len(cards), maxQueueCards)] {
		out.Cards = append(out.Cards, ipc.CardInfo{ID: c.ID, Dot: c.Dot, Severity: c.Severity, Turn: c.Turn,
			Status: string(c.Status), Body: clipAsk(c.Body, maxQueueBody)})
	}
	return out, nil
}

// purposeSummary is the purpose's first 200 runes on one line.
func purposeSummary(purpose string) string {
	return clipAsk(strings.Join(strings.Fields(purpose), " "), purposeSummaryChars)
}

// clipAsk cuts s to at most n runes, ending in "…" when cut.
func clipAsk(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(0, n-1)]) + "…"
}

var _ ipc.Querier = (*Engine)(nil)
```

- [ ] **Step 5: Hook the engine**

`internal/engine/engine.go`. Find:

```go
type runOpts struct {
```

Replace with:

```go
type runOpts struct {
	ask bool // MCP ask_dot (ask.go): feed "asked", status consulted, no card, no cooldown

```

Find:

```go
	paneMu map[string]*sync.Mutex // per-pane; guarded by mu
```

Replace with:

```go
	paneMu map[string]*sync.Mutex // per-pane; guarded by mu

	// MCP state (ask.go), guarded by mu; created lazily.
	consulted map[key]bool // asked since the pane's last processed turn
	asks      *askLimiter  // per-pane ask limits; its own leaf lock
```

In `processPaneLocked`, find:

```go
	var jobs []job
	var budgetHit []dots.Dot
```

Replace with:

```go
	var jobs []job
	var budgetHit []dots.Dot
	turnEnded := false
```

Find:

```go
			if len(newTurns) == 0 {
				da.LastSeenTurn = len(turns)
				continue
			}
```

Replace with:

```go
			if len(newTurns) == 0 {
				da.LastSeenTurn = len(turns)
				continue
			}
			turnEnded = true
```

Find (the first two lines of the `Decide` call; Phase 4 added lines after them, keep those):

```go
			dec := Decide(FilterInput{
				Dot: d, Att: *da, NewTurns: newTurns, PausedAll: e.pausedAll,
```

Replace with:

```go
			consulted := e.consulted[k]
			delete(e.consulted, k) // a consultation covers the turn it happened in, once
			dec := Decide(FilterInput{
				Dot: d, Att: *da, NewTurns: newTurns, PausedAll: e.pausedAll, Consulted: consulted,
```

Find:

```go
			case ReasonCooldown:
				da.CooldownLeft--
```

Replace with:

```go
			case ReasonCooldown:
				da.CooldownLeft--
			case ReasonConsulted:
				e.status[k] = view.StatusConsulted
```

Find (the end of `processPaneLocked`):

```go
	e.mu.Unlock()

	return jobs, budgetHit
}
```

Replace with:

```go
	e.mu.Unlock()
	if turnEnded {
		e.limiter().endTurn(paneID) // the per-turn ask limit starts over
	}
	return jobs, budgetHit
}
```

In `runDot`, the cooldown: Phase 2 turned v1's `if res.Verdict.Relevant {` into a `switch`. Find:

```go
			case res.Verdict.Relevant:
```

Replace with:

```go
			case res.Verdict.Relevant && !o.ask:
```

(v1 form, if Phase 2's switch isn't there: change `if res.Verdict.Relevant {` above `da.CooldownLeft = d.Trigger.Cooldown` to `if res.Verdict.Relevant && !o.ask {`.)

In `runDot`, the verdict handling. Find (post-Phase-2 form: Phase 2's last `applyMemoryOps` call sits right before the pass branch):

```go
	e.applyMemoryOps(paneID, d, turn, mem, res.Remember, att)
	if !res.Verdict.Relevant {
```

Replace with:

```go
	e.applyMemoryOps(paneID, d, turn, mem, res.Remember, att)
	if o.ask {
		e.feed(paneID, d.Slug, turn, state.OutcomeAsked, askDetail(d, res))
		e.setStatus(k, view.StatusConsulted)
		return // MCP ask: the answer goes back to the agent, never into the queue
	}
	if !res.Verdict.Relevant {
```

The ask branch must come after Phase 3's `sinkRes, sinkErr = res, nil` and after the memory hooks, and before the pass/flagged feed lines and `route`. Asks are summons, so Phase 2's suppression branch (`res.Verdict.Relevant && !o.summon`) never applies to them.

`internal/engine/deliver.go`. Find:

```go
func (e *Engine) dropPane(paneID string) {
```

Replace with:

```go
func (e *Engine) dropPane(paneID string) {
	e.forgetAsks(paneID)
```

Run `gofmt -w internal/engine`.

- [ ] **Step 6: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race -count=3 ./internal/engine/`
Expected: PASS three times in a row, including every v1, Phase 2, 3 and 4 engine test (`TestDecide`, the budget, memory, council and cost tests).

- [ ] **Step 7: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/engine
git commit -m "feat(engine): synchronous asks for the MCP server, consulted guard and per-pane ask limits"
```

---

### Task 8: MCP server package (`internal/mcpserver`)

**Depends on:** Task 2 (IPC types, `Client.Query`), Task 3 (`config.MCPTools`), Task 4 (`memory.Store.Propose`, `memory.SourceAgent`). Wave 2 (runs beside Task 7).

**Files:**
- Create: `internal/mcpserver/server.go`, `internal/mcpserver/engine.go`, `internal/mcpserver/server_test.go`, `internal/mcpserver/engine_test.go`
- Modify: `go.mod`, `go.sum`
- Shared with Phase 4 (already merged): `go.mod`, `go.sum`

**Interfaces:**
- Consumes: go-sdk v1.8.0 `mcp.NewServer`, `mcp.Implementation`, `mcp.AddTool`, `mcp.Tool`, `mcp.CallToolRequest`, `mcp.CallToolResult`, `mcp.Content`, `mcp.TextContent`, `mcp.NewInMemoryTransports`, `mcp.NewClient`, `mcp.ClientSession` (`ListTools`, `CallTool`, `Close`), `mcp.CallToolParams`; Task 2's `ipc.Client.Query`, `ipc.Client.Call`, query consts and types, `ipc.Serve`; Task 3's `config.MCPTools`; Task 4's `memory.Store.Propose`, `memory.SourceAgent`; Phase 2's `memory.Store.List`, `memory.Filter`, `memory.Open`, `memory.DefaultLimits`, `memory.ValidSlug`, `memory.ValidProjectID`, `memory.ErrSecret`, `(memory.Entry) Ref()`, `ipc.CmdMemoryChanged`; `version.Version`.
- Produces:
  - `const mcpserver.Name = "herdr-dots"`, `DefaultMaxResultChars = 24000`, `MaxQuestionChars = 4000`, `MaxProposalChars = 500`, `NotInHerdr` (the refusal text).
  - `type EngineAPI interface{ ListDots(ctx) (ipc.DotsResult, error); Ask(ctx, slug, question string, includeTurns bool) (ipc.AskResult, error); AskCouncil(ctx, question string) (ipc.CouncilResult, error); ListQueue(ctx, status string) (ipc.QueueResult, error); MemoryChanged(ctx) error }`.
  - `type Deps struct{ Engine EngineAPI; Memory *memory.Store; PaneID string; Tools []string; MaxResultChars int; AskTimeout, CouncilTimeout time.Duration; Logf func(string, ...any) }`; `New(d Deps) *mcp.Server`.
  - `NewIPCEngine(c ipc.Client, paneID string, ensure func() error) EngineAPI` (ensure runs on the first call and once more after a failed dial); `var ErrEngineDown`.
  - Tool input/output types `ListDotsIn/Out`, `AskDotIn/Out`, `AskCouncilIn/Out`, `ListQueueIn/Out`, `ReadMemoryIn/Out`, `MemoryItem`, `ProposeMemoryIn/Out` (JSON as in the code; every output has a `note`).

- [ ] **Step 1: Add the SDK**

```bash
GOTOOLCHAIN=local go get github.com/modelcontextprotocol/go-sdk@v1.8.0
head -3 go.mod
```

Expected: `go 1.25.0` (unchanged since Phase 4) and `github.com/modelcontextprotocol/go-sdk v1.8.0` in `go.mod`. If `go get` wants to raise the `go` line or switch toolchains, stop and report (the module must stay on Go 1.25).

- [ ] **Step 2: Write the failing tests**

Create `internal/mcpserver/server_test.go` (Review Focus 1 and 4 included):

```go
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/config"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
)

const testPID = "g-0123456789abcdef"

// fakeEngine records calls and answers from its fields.
type fakeEngine struct {
	mu       sync.Mutex
	dots     ipc.DotsResult
	ask      ipc.AskResult
	council  ipc.CouncilResult
	queue    ipc.QueueResult
	err      error
	asks     []ipc.AskArgs
	deadline time.Duration // time left on the last Ask's context
	notified int
	statuses []string
}

func (f *fakeEngine) ListDots(context.Context) (ipc.DotsResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dots, f.err
}

func (f *fakeEngine) Ask(ctx context.Context, slug, q string, include bool) (ipc.AskResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asks = append(f.asks, ipc.AskArgs{Slug: slug, Question: q, IncludeTurns: include})
	if d, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(d)
	}
	return f.ask, f.err
}

func (f *fakeEngine) AskCouncil(context.Context, string) (ipc.CouncilResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.council, f.err
}

func (f *fakeEngine) ListQueue(_ context.Context, status string) (ipc.QueueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, status)
	return f.queue, f.err
}

func (f *fakeEngine) MemoryChanged(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notified++
	return nil
}

// connect serves New(d) over in-memory transports and returns a client session.
func connect(t *testing.T, d Deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := New(d).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call returns the tool's text, its structured result as JSON, and whether it
// was an error (a protocol error counts as one, with its message as text).
func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, []byte, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return err.Error(), nil, true
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	js, _ := json.Marshal(res.StructuredContent)
	return text, js, res.IsError
}

func inPane(e EngineAPI) Deps { return Deps{Engine: e, PaneID: "w1:p1"} }

func memStore(t *testing.T) *memory.Store {
	t.Helper()
	return memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
}

func TestToolListFollowsConfig(t *testing.T) {
	names := func(cs *mcp.ClientSession) []string {
		res, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tl := range res.Tools {
			out = append(out, tl.Name)
		}
		slices.Sort(out)
		return out
	}
	all := slices.Sorted(slices.Values(config.MCPTools))
	if got := names(connect(t, inPane(&fakeEngine{}))); !slices.Equal(got, all) {
		t.Fatalf("default tools %v", got)
	}
	d := inPane(&fakeEngine{})
	d.Tools = []string{"ask_dot", "list_queue", "bogus"}
	if got := names(connect(t, d)); !slices.Equal(got, []string{"ask_dot", "list_queue"}) {
		t.Fatalf("configured tools %v", got)
	}
}

// Review Focus 1: outside Herdr (or in a dot's own CLI run) every tool
// refuses, but the tool list stays the same.
func TestEveryToolRefusesOutsideHerdr(t *testing.T) {
	cs := connect(t, Deps{Memory: memStore(t)})
	args := map[string]map[string]any{
		"list_dots":      {},
		"ask_dot":        {"slug": "security", "question": "is this safe?"},
		"ask_council":    {"question": "ready to merge?"},
		"list_queue":     {},
		"read_memory":    {},
		"propose_memory": {"kind": "note", "text": "x"},
	}
	for _, tool := range config.MCPTools {
		text, _, isErr := call(t, cs, tool, args[tool])
		if !isErr || text != NotInHerdr {
			t.Errorf("%s: isError=%v text=%q", tool, isErr, text)
		}
	}
}

func TestAskDotReturnsTheAnswer(t *testing.T) {
	fe := &fakeEngine{ask: ipc.AskResult{Slug: "security", Name: "Security", Relevant: true, Severity: "warn",
		Message: "Keep the JWT in an httpOnly cookie.", CostUSD: 0.0123, CostKnown: true, Model: "sonnet"}}
	cs := connect(t, inPane(fe))
	text, js, isErr := call(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "  Is localStorage OK for the JWT?  "})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if !strings.HasPrefix(text, "[◆ Security] (warn) Keep the JWT in an httpOnly cookie.") || !strings.Contains(text, "cost $0.0123") {
		t.Fatalf("text %q", text)
	}
	var out AskDotOut
	if err := json.Unmarshal(js, &out); err != nil || out.Severity != "warn" || out.Model != "sonnet" || !strings.Contains(out.Note, "approval queue") {
		t.Fatalf("structured %s (%v)", js, err)
	}
	if len(fe.asks) != 1 || fe.asks[0] != (ipc.AskArgs{Slug: "security", Question: "Is localStorage OK for the JWT?", IncludeTurns: true}) {
		t.Fatalf("engine got %+v", fe.asks)
	}
	call(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "q", "include_turns": false})
	if fe.asks[1].IncludeTurns {
		t.Fatal("include_turns=false must reach the engine")
	}
}

func TestAskDotNothingToAdd(t *testing.T) {
	cs := connect(t, inPane(&fakeEngine{ask: ipc.AskResult{Slug: "copy", Name: "Copy", Severity: "info"}}))
	if text, _, _ := call(t, cs, "ask_dot", map[string]any{"slug": "copy", "question": "q"}); !strings.HasPrefix(text, "[◆ Copy] nothing to add") {
		t.Fatalf("text %q", text)
	}
}

func TestAskDotValidatesBeforeAsking(t *testing.T) {
	fe := &fakeEngine{}
	cs := connect(t, inPane(fe))
	for name, args := range map[string]map[string]any{
		"long question":  {"slug": "security", "question": strings.Repeat("a", MaxQuestionChars+1)},
		"empty question": {"slug": "security", "question": "   "},
		"bad slug":       {"slug": "../x", "question": "q"},
		"missing slug":   {"question": "q"},
	} {
		if _, _, isErr := call(t, cs, "ask_dot", args); !isErr {
			t.Errorf("%s accepted", name)
		}
	}
	if text, _, _ := call(t, cs, "ask_dot", map[string]any{"slug": "security", "question": strings.Repeat("é", MaxQuestionChars+1)}); !strings.Contains(text, "the limit is 4000") {
		t.Fatalf("text %q", text)
	}
	if len(fe.asks) != 0 {
		t.Fatalf("the engine must not be asked: %+v", fe.asks)
	}
}

// Review Focus 4: the ask carries a deadline the engine can work within.
func TestAskDotPassesADeadline(t *testing.T) {
	fe := &fakeEngine{ask: ipc.AskResult{Name: "Security"}}
	d := inPane(fe)
	d.AskTimeout = 130 * time.Second
	call(t, connect(t, d), "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if fe.deadline < 120*time.Second || fe.deadline > 130*time.Second {
		t.Fatalf("deadline %v", fe.deadline)
	}
}

func TestEngineErrorsBecomeToolErrors(t *testing.T) {
	fe := &fakeEngine{err: errors.New("ask limit reached; continue without the dot (3 per turn)")}
	text, _, isErr := call(t, connect(t, inPane(fe)), "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if !isErr || text != "ask limit reached; continue without the dot (3 per turn)" {
		t.Fatalf("isError=%v text=%q", isErr, text)
	}
	fe.err = context.DeadlineExceeded
	if text, _, _ := call(t, connect(t, inPane(fe)), "ask_dot", map[string]any{"slug": "security", "question": "q"}); !strings.Contains(text, "no answer in time") {
		t.Fatalf("text %q", text)
	}
}

func TestResultsStayWithinMaxResultChars(t *testing.T) {
	fe := &fakeEngine{ask: ipc.AskResult{Name: "Security", Relevant: true, Severity: "warn", Message: strings.Repeat("long advice ", 2000)}}
	d := inPane(fe)
	d.MaxResultChars = 3000
	text, js, _ := call(t, connect(t, d), "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if n := utf8.RuneCountInString(text) + utf8.RuneCount(js); n > 3000 || !strings.Contains(text, "…") {
		t.Fatalf("ask result is %d characters", n)
	}

	var cards []ipc.CardInfo
	for i := range 20 {
		cards = append(cards, ipc.CardInfo{ID: string(rune('a' + i)), Dot: "security", Severity: "warn", Body: strings.Repeat("b", 2000)})
	}
	fe.queue = ipc.QueueResult{Cards: cards, Total: 35}
	text, js, _ = call(t, connect(t, inPane(fe)), "list_queue", map[string]any{})
	if n := utf8.RuneCountInString(text) + utf8.RuneCount(js); n > DefaultMaxResultChars {
		t.Fatalf("queue result is %d characters", n)
	}
	var out ListQueueOut
	_ = json.Unmarshal(js, &out)
	if len(out.Cards) >= 20 || out.Total != 35 || !strings.Contains(text, "more not shown") {
		t.Fatalf("cards %d total %d text tail %q", len(out.Cards), out.Total, text[max(0, len(text)-200):])
	}
}

func TestListDotsQueueAndCouncil(t *testing.T) {
	fe := &fakeEngine{
		dots:  ipc.DotsResult{ProjectID: testPID, ProjectName: "shop", Dots: []ipc.DotInfo{{Slug: "security", Name: "Security", PurposeSummary: "Find auth bugs.", Mode: "queue", Status: "idle", Calls: 2, Budget: 40}}},
		queue: ipc.QueueResult{Cards: []ipc.CardInfo{{ID: "7", Dot: "security", Severity: "critical", Turn: 4, Status: "pending", Body: "JWT in localStorage"}}, Total: 1},
		council: ipc.CouncilResult{Digest: "## Must fix\n- JWT in localStorage (Security)", CostUSD: 0.05,
			Members: []ipc.CouncilMember{{Slug: "security", Severity: "critical"}, {Slug: "copy", Failed: true}}},
	}
	cs := connect(t, inPane(fe))
	if text, _, isErr := call(t, cs, "list_dots", map[string]any{}); isErr || !strings.Contains(text, "security: Security (queue, idle, 2/40 calls) Find auth bugs.") {
		t.Fatalf("list_dots %q", text)
	}
	if text, _, isErr := call(t, cs, "list_queue", map[string]any{"status": "all"}); isErr || !strings.Contains(text, "#7 security (critical, turn 4, pending): JWT in localStorage") {
		t.Fatalf("list_queue %q", text)
	}
	if fe.statuses[0] != ipc.QueueAll {
		t.Fatalf("status %v", fe.statuses)
	}
	if _, _, isErr := call(t, cs, "list_queue", map[string]any{"status": "dismissed"}); !isErr {
		t.Fatal("unknown status accepted")
	}
	text, js, isErr := call(t, cs, "ask_council", map[string]any{"question": "ready to merge?"})
	if isErr || !strings.Contains(text, "## Must fix") {
		t.Fatalf("ask_council %q", text)
	}
	var out AskCouncilOut
	if err := json.Unmarshal(js, &out); err != nil || len(out.Members) != 2 || !out.Members[1].Failed {
		t.Fatalf("council %s", js)
	}
	empty := connect(t, inPane(&fakeEngine{}))
	if text, _, _ := call(t, empty, "list_dots", map[string]any{}); !strings.Contains(text, "No dots are attached") {
		t.Fatalf("empty list_dots %q", text)
	}
}

func TestReadMemoryReturnsActiveEntriesOnly(t *testing.T) {
	st := memStore(t)
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindConvention, Text: "Use pnpm, never npm.", Pinned: true},
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Adopt zod for validation.", Status: memory.StatusProposed, Source: memory.SourceModel, Dot: "security"},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindFinding, Text: "Tokens belong in httpOnly cookies.", Source: memory.SourceOutcome},
	} {
		if _, err := st.Add(testPID, e); err != nil {
			t.Fatal(err)
		}
	}
	d := inPane(&fakeEngine{dots: ipc.DotsResult{ProjectID: testPID}})
	d.Memory = st
	cs := connect(t, d)
	text, js, isErr := call(t, cs, "read_memory", map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	var out ReadMemoryOut
	_ = json.Unmarshal(js, &out)
	if out.Total != 2 || len(out.Entries) != 2 || out.Entries[0].ID != "p:m1" || out.Entries[1].Dot != "security" || strings.Contains(text, "zod") {
		t.Fatalf("read_memory %s\n%s", js, text)
	}
	_, js, _ = call(t, cs, "read_memory", map[string]any{"scope": "dot", "query": "cookies"})
	_ = json.Unmarshal(js, &out)
	if len(out.Entries) != 1 || out.Entries[0].Kind != "finding" {
		t.Fatalf("filtered %s", js)
	}
	_, js, _ = call(t, cs, "read_memory", map[string]any{"limit": 1})
	_ = json.Unmarshal(js, &out)
	if len(out.Entries) != 1 || out.Total != 2 {
		t.Fatalf("limited %s", js)
	}
	if _, _, isErr := call(t, cs, "read_memory", map[string]any{"scope": "global-ish"}); !isErr {
		t.Fatal("unknown scope accepted")
	}
}

func TestProposeMemoryLandsAsAProposalAndNotifies(t *testing.T) {
	st := memStore(t)
	fe := &fakeEngine{dots: ipc.DotsResult{ProjectID: testPID}}
	d := inPane(fe)
	d.Memory = st
	cs := connect(t, d)
	text, js, isErr := call(t, cs, "propose_memory", map[string]any{"kind": "convention", "text": "API handlers return problem+json errors.", "paths": []string{"api/**"}})
	if isErr {
		t.Fatal(text)
	}
	var out ProposeMemoryOut
	if err := json.Unmarshal(js, &out); err != nil || out.Status != "proposed" || out.ID != "p:m1" {
		t.Fatalf("propose %s", js)
	}
	es, _ := st.List(testPID, memory.Filter{Scope: memory.ScopeProject, Statuses: []memory.Status{memory.StatusProposed}})
	if len(es) != 1 || es[0].Source != memory.SourceAgent || es[0].Evidence.Pane != "w1:p1" {
		t.Fatalf("stored %+v", es)
	}
	if fe.notified != 1 {
		t.Fatalf("engine notified %d times", fe.notified)
	}
	text, _, isErr = call(t, cs, "propose_memory", map[string]any{"kind": "note", "text": "deploy with sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"})
	if !isErr || !strings.Contains(text, "looks like a secret") || strings.Contains(text, "sk-ant") {
		t.Fatalf("secret: isError=%v %q", isErr, text)
	}
	for _, args := range []map[string]any{
		{"kind": "finding", "text": "x"},
		{"kind": "note", "text": strings.Repeat("a", MaxProposalChars+1)},
	} {
		if _, _, isErr := call(t, cs, "propose_memory", args); !isErr {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestMemoryToolsNeedMemoryAndAProject(t *testing.T) {
	cs := connect(t, inPane(&fakeEngine{dots: ipc.DotsResult{ProjectID: testPID}}))
	if text, _, _ := call(t, cs, "read_memory", map[string]any{}); !strings.Contains(text, "memory_enabled = false") {
		t.Fatalf("memory off: %q", text)
	}
	d := inPane(&fakeEngine{})
	d.Memory = memStore(t)
	if text, _, _ := call(t, connect(t, d), "propose_memory", map[string]any{"kind": "note", "text": "x"}); !strings.Contains(text, "project is unknown") {
		t.Fatalf("no project: %q", text)
	}
}
```

Create `internal/mcpserver/engine_test.go` (Review Focus 3):

```go
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

// stubEngine is a minimal engine socket: it answers list_dots for one pane.
type stubEngine struct{ pane string }

func (s stubEngine) Handle(context.Context, string, json.RawMessage) error { return nil }
func (s stubEngine) Snapshot() view.Snapshot                               { return view.Snapshot{} }
func (s stubEngine) Watch() (<-chan view.Snapshot, func()) {
	return make(chan view.Snapshot), func() {}
}
func (s stubEngine) Query(_ context.Context, cmd string, raw json.RawMessage) (any, error) {
	var a ipc.PaneArgs
	_ = json.Unmarshal(raw, &a)
	if cmd != ipc.CmdListDots || a.PaneID != s.pane {
		return nil, errors.New("unexpected " + cmd + " for " + a.PaneID)
	}
	return ipc.DotsResult{ProjectID: testPID, Dots: []ipc.DotInfo{{Slug: "security"}}}, nil
}

func shortSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mcpe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "e.sock")
}

func startStub(t *testing.T, path string) context.CancelFunc {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ipc.Serve(ctx, ln, stubEngine{pane: "w1:p1"})
	return cancel
}

// Review Focus 3: the engine isn't running when the agent first calls a tool.
// Creating the engine client starts nothing; the first call starts the engine
// and succeeds; a dead engine is started again once.
func TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies(t *testing.T) {
	path := shortSock(t)
	ensures := 0
	var stop context.CancelFunc
	e := NewIPCEngine(ipc.Client{Path: path}, "w1:p1", func() error {
		ensures++
		stop = startStub(t, path)
		return nil
	})
	if ensures != 0 {
		t.Fatal("server start must not start the engine")
	}
	res, err := e.ListDots(context.Background())
	if err != nil || ensures != 1 || res.ProjectID != testPID {
		t.Fatalf("first call: %+v %v (ensures %d)", res, err, ensures)
	}
	if _, err := e.ListDots(context.Background()); err != nil || ensures != 1 {
		t.Fatalf("second call: %v (ensures %d)", err, ensures)
	}
	stop() // the engine goes away
	_ = os.Remove(path)
	if _, err := e.ListDots(context.Background()); err != nil || ensures != 2 {
		t.Fatalf("after the engine died: %v (ensures %d)", err, ensures)
	}
	stop()
}

func TestIPCEngineReportsAnEngineThatWontStart(t *testing.T) {
	e := NewIPCEngine(ipc.Client{Path: shortSock(t)}, "w1:p1", func() error { return errors.New("lock held") })
	if _, err := e.ListDots(context.Background()); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("got %v", err)
	}
	e = NewIPCEngine(ipc.Client{Path: shortSock(t)}, "w1:p1", func() error { return nil }) // "starts" but never listens
	if _, err := e.ListDots(context.Background()); !errors.Is(err, ErrEngineDown) {
		t.Fatalf("got %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/mcpserver/`
Expected: compile errors (`undefined: New`, `undefined: Deps`, `undefined: NewIPCEngine`, …).

- [ ] **Step 4: Write the server**

Create `internal/mcpserver/server.go`:

```go
// Package mcpserver is the Dots MCP server (Phase 5): the tools Claude Code
// and Codex call to consult the dots attached to the Herdr pane they run in.
// Dot questions go to the pane's engine (EngineAPI, over IPC); memory tools
// read and propose through memory.Store directly.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/config"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/version"
)

// Name is the server's MCP name (tools appear as mcp__herdr-dots__<tool>).
const Name = "herdr-dots"

const (
	DefaultMaxResultChars = 24000 // well under Claude Code's 10,000-token warning
	MaxQuestionChars      = 4000
	MaxProposalChars      = 500
	defaultMemoryLimit    = 20
	maxMemoryLimit        = 50
	minMessageChars       = 200 // results shrink a message no further than this
)

// NotInHerdr is every tool's error outside a Herdr pane (and inside a dot's
// own CLI run, whose environment has no HERDR_PANE_ID).
const NotInHerdr = "herdr-dots: not running inside a Herdr pane; dots are unavailable here."

const advisory = "Dots are advisory, read-only specialists: they answer from their own expertise and never change files. Their answers did not go through the developer's approval queue."

// EngineAPI is the pane's engine as the tools see it (NewIPCEngine in
// production, fakes in tests). Every call is scoped to the server's pane.
type EngineAPI interface {
	ListDots(ctx context.Context) (ipc.DotsResult, error)
	Ask(ctx context.Context, slug, question string, includeTurns bool) (ipc.AskResult, error)
	AskCouncil(ctx context.Context, question string) (ipc.CouncilResult, error)
	ListQueue(ctx context.Context, status string) (ipc.QueueResult, error)
	MemoryChanged(ctx context.Context) error
}

type Deps struct {
	Engine         EngineAPI     // nil (with PaneID "") outside Herdr
	Memory         *memory.Store // nil = memory off: the memory tools refuse
	PaneID         string        // HERDR_PANE_ID; "" = every tool refuses
	Tools          []string      // tools to offer, from [mcp] tools; nil = all
	MaxResultChars int           // 0 = DefaultMaxResultChars
	AskTimeout     time.Duration // ask_dot deadline; 0 = 130 s
	CouncilTimeout time.Duration // ask_council deadline; 0 = 250 s
	Logf           func(format string, args ...any)
}

func (d Deps) withDefaults() Deps {
	if d.Tools == nil {
		d.Tools = config.MCPTools
	}
	if d.MaxResultChars <= 0 {
		d.MaxResultChars = DefaultMaxResultChars
	}
	if d.AskTimeout <= 0 {
		d.AskTimeout = 130 * time.Second
	}
	if d.CouncilTimeout <= 0 {
		d.CouncilTimeout = 250 * time.Second
	}
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	return d
}

type tools struct{ d Deps }

// New builds the server with the tools d.Tools names. Outside Herdr every
// tool is still listed (the client's tool list stays stable) and refuses.
func New(d Deps) *mcp.Server {
	d = d.withDefaults()
	s := mcp.NewServer(&mcp.Implementation{Name: Name, Version: version.Version}, nil)
	t := &tools{d: d}
	for _, name := range d.Tools {
		switch name {
		case "list_dots":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "List the dots (specialist reviewers) attached to this Herdr pane: slug, name, purpose, mode, status and budget. " + advisory}, t.listDots)
		case "ask_dot":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Ask one attached dot a question and wait for its answer (up to a few minutes). Use it before risky changes, e.g. ask the security dot about token handling. Limited to a few asks per turn. " + advisory}, t.askDot)
		case "ask_council":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Ask every attached dot the same question at once and get one merged, attributed digest. Slower and costlier than ask_dot; counts as one ask. " + advisory}, t.askCouncil)
		case "list_queue":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Read the dots' advice cards waiting for the developer on this pane (read-only). " + advisory}, t.listQueue)
		case "read_memory":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Read the project's approved dot memory: conventions, decisions, notes and remembered findings."}, t.readMemory)
		case "propose_memory":
			mcp.AddTool(s, &mcp.Tool{Name: name, Description: "Propose a project convention, decision or note for the dots to remember. It is stored as a proposal that the developer approves; never include secrets."}, t.proposeMemory)
		default:
			d.Logf("unknown tool %q in [mcp] tools; ignored", name)
		}
	}
	return s
}

// ---- inputs and outputs

type ListDotsIn struct{}

type ListDotsOut struct {
	ProjectName string        `json:"project_name,omitempty"`
	Dots        []ipc.DotInfo `json:"dots"`
	Note        string        `json:"note"`
}

type AskDotIn struct {
	Slug         string `json:"slug" jsonschema:"slug of an attached dot, from list_dots (for example security)"`
	Question     string `json:"question" jsonschema:"what you want the dot's opinion on, at most 4000 characters"`
	IncludeTurns *bool  `json:"include_turns,omitempty" jsonschema:"send the dot the conversation's recent turns (default true)"`
}

type AskDotOut struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Relevant  bool    `json:"relevant"`
	Severity  string  `json:"severity"`
	Message   string  `json:"message"`
	CostUSD   float64 `json:"cost_usd"`
	CostKnown bool    `json:"cost_known"`
	Model     string  `json:"model,omitempty"`
	Note      string  `json:"note"`
}

type AskCouncilIn struct {
	Question string `json:"question" jsonschema:"the question for every attached dot, at most 4000 characters"`
}

type AskCouncilOut struct {
	Digest  string              `json:"digest"`
	Members []ipc.CouncilMember `json:"members"`
	CostUSD float64             `json:"cost_usd"`
	Note    string              `json:"note"`
}

type ListQueueIn struct {
	Status string `json:"status,omitempty" jsonschema:"pending (default) or all"`
}

type ListQueueOut struct {
	Cards []ipc.CardInfo `json:"cards"`
	Total int            `json:"total"`
	Note  string         `json:"note"`
}

type ReadMemoryIn struct {
	Query string `json:"query,omitempty" jsonschema:"only entries whose text or paths contain this"`
	Scope string `json:"scope,omitempty" jsonschema:"project, dot or all (default all)"`
	Dot   string `json:"dot,omitempty" jsonschema:"only this dot's memory (a slug)"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many entries, 1 to 50 (default 20)"`
}

type MemoryItem struct {
	ID     string   `json:"id"`
	Scope  string   `json:"scope"`
	Dot    string   `json:"dot,omitempty"`
	Kind   string   `json:"kind"`
	Text   string   `json:"text"`
	Paths  []string `json:"paths,omitempty"`
	Pinned bool     `json:"pinned"`
}

type ReadMemoryOut struct {
	Entries []MemoryItem `json:"entries"`
	Total   int          `json:"total"`
	Note    string       `json:"note,omitempty"`
}

type ProposeMemoryIn struct {
	Kind  string   `json:"kind" jsonschema:"convention, decision or note"`
	Text  string   `json:"text" jsonschema:"one sentence, at most 500 characters; never secrets"`
	Paths []string `json:"paths,omitempty" jsonschema:"globs the entry applies to, like src/auth/**"`
}

type ProposeMemoryOut struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

// ---- handlers

func (t *tools) guard(tool string) error {
	if t.d.PaneID == "" || t.d.Engine == nil {
		t.d.Logf("%s refused: not inside a Herdr pane", tool)
		return errors.New(NotInHerdr)
	}
	return nil
}

func (t *tools) fail(tool string, err error) error {
	t.d.Logf("%s: %v", tool, err)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: no answer in time; continue without it", tool)
	}
	return err
}

func (t *tools) listDots(ctx context.Context, _ *mcp.CallToolRequest, _ ListDotsIn) (*mcp.CallToolResult, ListDotsOut, error) {
	if err := t.guard("list_dots"); err != nil {
		return nil, ListDotsOut{}, err
	}
	res, err := t.d.Engine.ListDots(ctx)
	if err != nil {
		return nil, ListDotsOut{}, t.fail("list_dots", err)
	}
	out := ListDotsOut{ProjectName: res.ProjectName, Dots: res.Dots, Note: advisory}
	if out.Dots == nil {
		out.Dots = []ipc.DotInfo{}
	}
	if len(out.Dots) == 0 {
		out.Note = "No dots are attached to this pane. The developer attaches them with prefix+d in Herdr."
	}
	text := renderDots(out)
	for !t.fits(text, out) && len(out.Dots) > 0 {
		out.Dots = out.Dots[:len(out.Dots)-1]
		out.Note = "Some dots are not shown (result size limit). " + advisory
		text = renderDots(out)
	}
	return textResult(text), out, nil
}

func (t *tools) askDot(ctx context.Context, _ *mcp.CallToolRequest, in AskDotIn) (*mcp.CallToolResult, AskDotOut, error) {
	if err := t.guard("ask_dot"); err != nil {
		return nil, AskDotOut{}, err
	}
	slug := strings.TrimSpace(in.Slug)
	if !memory.ValidSlug(slug) {
		return nil, AskDotOut{}, fmt.Errorf("%q is not a dot slug; call list_dots to see the attached dots", in.Slug)
	}
	q, err := question(in.Question)
	if err != nil {
		return nil, AskDotOut{}, err
	}
	include := in.IncludeTurns == nil || *in.IncludeTurns
	ctx, cancel := context.WithTimeout(ctx, t.d.AskTimeout)
	defer cancel()
	res, err := t.d.Engine.Ask(ctx, slug, q, include)
	if err != nil {
		return nil, AskDotOut{}, t.fail("ask_dot", err)
	}
	out := AskDotOut{Slug: res.Slug, Name: res.Name, Relevant: res.Relevant, Severity: res.Severity, Message: res.Message,
		CostUSD: res.CostUSD, CostKnown: res.CostKnown, Model: res.Model, Note: advisory}
	text := renderAsk(out)
	for !t.fits(text, out) && utf8.RuneCountInString(out.Message) > minMessageChars {
		out.Message = clip(out.Message, utf8.RuneCountInString(out.Message)/2)
		text = renderAsk(out)
	}
	return textResult(text), out, nil
}

func (t *tools) askCouncil(ctx context.Context, _ *mcp.CallToolRequest, in AskCouncilIn) (*mcp.CallToolResult, AskCouncilOut, error) {
	if err := t.guard("ask_council"); err != nil {
		return nil, AskCouncilOut{}, err
	}
	q, err := question(in.Question)
	if err != nil {
		return nil, AskCouncilOut{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, t.d.CouncilTimeout)
	defer cancel()
	res, err := t.d.Engine.AskCouncil(ctx, q)
	if err != nil {
		return nil, AskCouncilOut{}, t.fail("ask_council", err)
	}
	out := AskCouncilOut{Digest: res.Digest, Members: res.Members, CostUSD: res.CostUSD, Note: advisory}
	if out.Members == nil {
		out.Members = []ipc.CouncilMember{}
	}
	if res.Note != "" {
		out.Note = res.Note + ". " + advisory
	}
	text := renderCouncil(out)
	for !t.fits(text, out) && utf8.RuneCountInString(out.Digest) > minMessageChars {
		out.Digest = clip(out.Digest, utf8.RuneCountInString(out.Digest)/2)
		text = renderCouncil(out)
	}
	return textResult(text), out, nil
}

func (t *tools) listQueue(ctx context.Context, _ *mcp.CallToolRequest, in ListQueueIn) (*mcp.CallToolResult, ListQueueOut, error) {
	if err := t.guard("list_queue"); err != nil {
		return nil, ListQueueOut{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = ipc.QueuePending
	}
	if status != ipc.QueuePending && status != ipc.QueueAll {
		return nil, ListQueueOut{}, fmt.Errorf("status %q: want pending or all", in.Status)
	}
	res, err := t.d.Engine.ListQueue(ctx, status)
	if err != nil {
		return nil, ListQueueOut{}, t.fail("list_queue", err)
	}
	out := ListQueueOut{Cards: res.Cards, Total: res.Total, Note: "Read-only. The developer sends or dismisses cards in the Dots pane. " + advisory}
	if out.Cards == nil {
		out.Cards = []ipc.CardInfo{}
	}
	text := renderQueue(out)
	for !t.fits(text, out) && len(out.Cards) > 0 {
		out.Cards = out.Cards[:len(out.Cards)-1]
		text = renderQueue(out)
	}
	return textResult(text), out, nil
}

func (t *tools) readMemory(ctx context.Context, _ *mcp.CallToolRequest, in ReadMemoryIn) (*mcp.CallToolResult, ReadMemoryOut, error) {
	if err := t.guard("read_memory"); err != nil {
		return nil, ReadMemoryOut{}, err
	}
	pid, err := t.project(ctx)
	if err != nil {
		return nil, ReadMemoryOut{}, err
	}
	f := memory.Filter{Statuses: []memory.Status{memory.StatusActive}, Query: strings.TrimSpace(in.Query)}
	switch strings.TrimSpace(in.Scope) {
	case "", "all":
	case "project":
		f.Scope = memory.ScopeProject
	case "dot":
		f.Scope = memory.ScopeDot
	default:
		return nil, ReadMemoryOut{}, fmt.Errorf("scope %q: want project, dot or all", in.Scope)
	}
	if dot := strings.TrimSpace(in.Dot); dot != "" {
		if !memory.ValidSlug(dot) {
			return nil, ReadMemoryOut{}, fmt.Errorf("%q is not a dot slug", in.Dot)
		}
		f.Dot = dot
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultMemoryLimit
	}
	limit = min(limit, maxMemoryLimit)
	entries, err := t.d.Memory.List(pid, f)
	out := ReadMemoryOut{Entries: []MemoryItem{}, Total: len(entries)}
	if err != nil {
		if len(entries) == 0 {
			return nil, ReadMemoryOut{}, t.fail("read_memory", err)
		}
		out.Note = "Some memory could not be read: " + err.Error()
	}
	for _, e := range entries[:min(len(entries), limit)] {
		out.Entries = append(out.Entries, MemoryItem{ID: e.Ref().String(), Scope: string(e.Scope), Dot: e.Dot,
			Kind: string(e.Kind), Text: e.Text, Paths: e.Paths, Pinned: e.Pinned})
	}
	text := renderMemory(out)
	for !t.fits(text, out) && len(out.Entries) > 0 {
		out.Entries = out.Entries[:len(out.Entries)-1]
		text = renderMemory(out)
	}
	return textResult(text), out, nil
}

func (t *tools) proposeMemory(ctx context.Context, _ *mcp.CallToolRequest, in ProposeMemoryIn) (*mcp.CallToolResult, ProposeMemoryOut, error) {
	if err := t.guard("propose_memory"); err != nil {
		return nil, ProposeMemoryOut{}, err
	}
	kind := memory.Kind(strings.TrimSpace(in.Kind))
	if kind != memory.KindConvention && kind != memory.KindDecision && kind != memory.KindNote {
		return nil, ProposeMemoryOut{}, fmt.Errorf("kind %q: want convention, decision or note", in.Kind)
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return nil, ProposeMemoryOut{}, errors.New("text is empty")
	}
	if n := utf8.RuneCountInString(text); n > MaxProposalChars {
		return nil, ProposeMemoryOut{}, fmt.Errorf("text is %d characters; the limit is %d", n, MaxProposalChars)
	}
	pid, err := t.project(ctx)
	if err != nil {
		return nil, ProposeMemoryOut{}, err
	}
	e, err := t.d.Memory.Propose(pid, memory.Entry{Kind: kind, Text: text, Paths: in.Paths, Evidence: memory.Evidence{Pane: t.d.PaneID}})
	if errors.Is(err, memory.ErrSecret) {
		return nil, ProposeMemoryOut{}, errors.New("that text looks like a secret, so it was not stored")
	}
	if err != nil {
		return nil, ProposeMemoryOut{}, t.fail("propose_memory", err)
	}
	nctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := t.d.Engine.MemoryChanged(nctx); err != nil {
		t.d.Logf("propose_memory: notify the engine: %v", err)
	}
	out := ProposeMemoryOut{ID: e.Ref().String(), Status: string(e.Status),
		Note: "Stored as a proposal; the developer approves or rejects it in the Dots memory screen."}
	if e.Status == memory.StatusActive {
		out.Note = "This matches an entry the developer already approved; nothing new was proposed."
	}
	return textResult(fmt.Sprintf("Memory %s %s: %s\n%s", out.ID, out.Status, text, out.Note)), out, nil
}

// project is the pane's memory project, as the engine resolved it.
func (t *tools) project(ctx context.Context) (string, error) {
	if t.d.Memory == nil {
		return "", errors.New("dot memory is turned off (memory_enabled = false in config.toml)")
	}
	res, err := t.d.Engine.ListDots(ctx)
	if err != nil {
		return "", err
	}
	if !memory.ValidProjectID(res.ProjectID) {
		return "", errors.New("this pane's project is unknown: attach a dot to the pane first (prefix+d)")
	}
	return res.ProjectID, nil
}

// ---- helpers

func question(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", errors.New("question is empty")
	}
	if n := utf8.RuneCountInString(q); n > MaxQuestionChars {
		return "", fmt.Errorf("question is %d characters; the limit is %d", n, MaxQuestionChars)
	}
	return q, nil
}

// fits reports whether the text plus the structured result stay within the
// result budget (clients may show either; Claude Code shows the structured one).
func (t *tools) fits(text string, out any) bool {
	b, err := json.Marshal(out)
	return err == nil && utf8.RuneCountInString(text)+utf8.RuneCount(b) <= t.d.MaxResultChars
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(0, n-1)]) + "…"
}

func cost(usd float64, known bool) string {
	if !known {
		return "cost ?"
	}
	return fmt.Sprintf("cost $%.4f", usd)
}

func renderDots(o ListDotsOut) string {
	var b strings.Builder
	if len(o.Dots) == 0 {
		return o.Note
	}
	for _, d := range o.Dots {
		state := d.Status
		if d.Paused {
			state = "paused"
		}
		fmt.Fprintf(&b, "%s: %s (%s, %s, %d/%d calls) %s\n", d.Slug, d.Name, d.Mode, state, d.Calls, d.Budget, d.PurposeSummary)
	}
	b.WriteString(o.Note)
	return b.String()
}

func renderAsk(o AskDotOut) string {
	if !o.Relevant || strings.TrimSpace(o.Message) == "" {
		return fmt.Sprintf("[◆ %s] nothing to add from its purpose (%s)\n%s", o.Name, cost(o.CostUSD, o.CostKnown), o.Note)
	}
	return fmt.Sprintf("[◆ %s] (%s) %s\n(%s)\n%s", o.Name, o.Severity, o.Message, cost(o.CostUSD, o.CostKnown), o.Note)
}

func renderCouncil(o AskCouncilOut) string {
	return fmt.Sprintf("[⊕ Council] %d members\n%s\n(about $%.4f)\n%s", len(o.Members), o.Digest, o.CostUSD, o.Note)
}

func renderQueue(o ListQueueOut) string {
	var b strings.Builder
	if len(o.Cards) == 0 {
		b.WriteString("The dots' queue is empty.\n")
	}
	for _, c := range o.Cards {
		fmt.Fprintf(&b, "#%s %s (%s, turn %d, %s): %s\n", c.ID, c.Dot, c.Severity, c.Turn, c.Status, c.Body)
	}
	if len(o.Cards) < o.Total {
		fmt.Fprintf(&b, "%d more not shown.\n", o.Total-len(o.Cards))
	}
	b.WriteString(o.Note)
	return b.String()
}

func renderMemory(o ReadMemoryOut) string {
	var b strings.Builder
	if len(o.Entries) == 0 {
		b.WriteString("No matching memory.\n")
	}
	for _, e := range o.Entries {
		fmt.Fprintf(&b, "[%s] %s: %s\n", e.ID, e.Kind, e.Text)
	}
	if len(o.Entries) < o.Total {
		fmt.Fprintf(&b, "%d more not shown (narrow with query, scope or dot).\n", o.Total-len(o.Entries))
	}
	if o.Note != "" {
		b.WriteString(o.Note + "\n")
	}
	return b.String()
}
```

Create `internal/mcpserver/engine.go`:

```go
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"herdr-dots/internal/ipc"
)

// ErrEngineDown: the engine can't be reached even after starting it.
var ErrEngineDown = errors.New("dots engine not reachable; open Herdr's Dots pane (prefix+a) to start it")

type ipcEngine struct {
	c      ipc.Client
	paneID string
	ensure func() error

	mu    sync.Mutex
	ready bool
}

// NewIPCEngine talks to the engine over its socket, scoped to paneID. ensure
// starts the engine; it runs on the first call (never at server start, which
// must stay fast) and again once when a call can't connect.
func NewIPCEngine(c ipc.Client, paneID string, ensure func() error) EngineAPI {
	return &ipcEngine{c: c, paneID: paneID, ensure: ensure}
}

func (e *ipcEngine) up(force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ready && !force {
		return nil
	}
	if e.ensure != nil {
		if err := e.ensure(); err != nil {
			return fmt.Errorf("%w (%v)", ErrEngineDown, err)
		}
	}
	e.ready = true
	return nil
}

func (e *ipcEngine) query(ctx context.Context, cmd string, args, out any) error {
	if err := e.up(false); err != nil {
		return err
	}
	err := e.c.Query(ctx, cmd, args, out)
	if !dialFailed(err) {
		return err
	}
	if err := e.up(true); err != nil {
		return err
	}
	err = e.c.Query(ctx, cmd, args, out)
	if dialFailed(err) {
		return ErrEngineDown
	}
	return err
}

func dialFailed(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func (e *ipcEngine) ListDots(ctx context.Context) (ipc.DotsResult, error) {
	var r ipc.DotsResult
	err := e.query(ctx, ipc.CmdListDots, ipc.PaneArgs{PaneID: e.paneID}, &r)
	return r, err
}

func (e *ipcEngine) Ask(ctx context.Context, slug, question string, includeTurns bool) (ipc.AskResult, error) {
	var r ipc.AskResult
	err := e.query(ctx, ipc.CmdAsk, ipc.AskArgs{PaneID: e.paneID, Slug: slug, Question: question, IncludeTurns: includeTurns}, &r)
	return r, err
}

func (e *ipcEngine) AskCouncil(ctx context.Context, question string) (ipc.CouncilResult, error) {
	var r ipc.CouncilResult
	err := e.query(ctx, ipc.CmdAskCouncil, ipc.AskCouncilArgs{PaneID: e.paneID, Question: question}, &r)
	return r, err
}

func (e *ipcEngine) ListQueue(ctx context.Context, status string) (ipc.QueueResult, error) {
	var r ipc.QueueResult
	err := e.query(ctx, ipc.CmdListQueue, ipc.ListQueueArgs{PaneID: e.paneID, Status: status}, &r)
	return r, err
}

func (e *ipcEngine) MemoryChanged(ctx context.Context) error {
	if err := e.up(false); err != nil {
		return err
	}
	return e.c.Call(ctx, ipc.CmdMemoryChanged, nil)
}
```

Then tidy:

```bash
GOTOOLCHAIN=local go mod tidy
head -3 go.mod
```

Expected: `go 1.25.0`; `github.com/modelcontextprotocol/go-sdk v1.8.0` moves to the direct `require` block.

- [ ] **Step 5: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race -count=3 ./internal/mcpserver/`
Expected: PASS three times.

- [ ] **Step 6: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/mcpserver go.mod go.sum
git commit -m "feat(mcpserver): Dots MCP server tools on the official Go SDK v1.8.0"
```

---

### Task 9: Engine: `AskCouncil`

**Depends on:** Task 7 (`ask.go`: `Query`, `limiter`, `markConsulted`, `decodePane`). Wave 3 (runs beside Task 10).

**Files:**
- Create: `internal/engine/ask_council.go`, `internal/engine/ask_council_test.go`
- Modify: `internal/engine/council.go` (Phase 3's file), `internal/engine/ask.go` (Task 7's file)
- Shared with Phases 3 and 4 (already merged): `internal/engine/council.go`

**Interfaces:**
- Consumes:
  - Task 7: `(*Engine) limiter()`, `(*askLimiter) take`, `(*Engine) markConsulted`, `decodePane`, `ErrAskLimit`, the `Query` switch.
  - Task 2: `ipc.CmdAskCouncil`, `ipc.AskCouncilArgs`, `ipc.CouncilResult`, `ipc.CouncilMember`. Task 3: `config.Config.MCP`, `state.OutcomeAsked`.
  - Phase 3: `councilRun` (fields `id`, `paneID`, `turn`, `ctx`, `cancel`, `abandoned`), `e.councils`, `e.preflights`, `e.councilSeq`, `e.councilBusy`, `e.councilPlan`, `e.runCouncil`, `e.endCouncil`, `e.addCouncilCard`, `councilSlug`, `maxQuestionChars`, `ErrCouncilRunning`, `council.Part` (`Slug`, `Verdict`, `Stance`, `Failed()`, `Relevant()`), `council.Answered`, `council.MaxSeverity`, `view.CouncilMembers`; test helpers `councilRig`, `councilBackend` (`on`, `total`, `running`), `member`, `seat`, `says`, `step`, `goodDigest`, `councilDone`, `feedHas`.
  - Phase 4: `Usage.PaneCost`.
- Produces:
  - `(*Engine) AskCouncil(ctx context.Context, paneID, question string) (ipc.CouncilResult, error)`: one ask against the limits; convenes every eligible attached dot through Phase 3's orchestration; returns the digest, the members (`slug`, `severity` when relevant, `stance`, `failed`), the note and the pane's spend during the council; no card; feed `asked` (Dot `council`, detail `"<severity> · N of M members answered"`); members that answered are marked consulted. Errors: empty question, no attachment, `ErrAskLimit`, `ErrCouncilRunning`, the preflight refusal, `council failed: no member answered`, the context's error (the council is then abandoned).
  - `councilRun.result func(parts []council.Part, body, note string)` and `councilRun.done chan struct{}` (closed when `runCouncil` ends); `(*Engine) startCouncil(ctx context.Context, paneID, question string, slugs []string, result func(parts []council.Part, body, note string)) (*councilRun, error)`; `Council` keeps its signature and behavior; `(*Engine) stopCouncil(run *councilRun)`.
  - `Query` routes `ipc.CmdAskCouncil`.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/ask_council_test.go`:

```go
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

func askCouncilNow(t *testing.T, r *rig, q string) (ipc.CouncilResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return r.e.AskCouncil(ctx, pane, q)
}

func TestAskCouncilReturnsTheDigestWithoutACard(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "JWT in localStorage; also no loading state."))
	cb.on("Copy", step{err: errors.New("copy backend down")})
	cb.on(council.ChairName, says("critical", goodDigest))
	res, err := askCouncilNow(t, r, "Ready to merge the auth refactor?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Digest, "JWT in localStorage (Security, Frontend)") || !strings.Contains(res.Digest, "Not heard from") {
		t.Fatalf("digest:\n%s", res.Digest)
	}
	if len(res.Members) != 3 || res.Members[0].Slug != "security" || res.Members[0].Severity != "critical" || !res.Members[2].Failed {
		t.Fatalf("members %+v", res.Members)
	}
	for _, c := range r.q.Pending(pane) {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("ask_council must not create a card: %+v", c)
		}
	}
	if !feedHas(r, state.OutcomeAsked, "critical · 2 of 3 members answered") || feedHas(r, state.OutcomeCouncil, "") {
		t.Fatal("feed: one asked line and no council line")
	}
	councilDone(t, r)
	r.e.mu.Lock()
	consulted := r.e.consulted[key{pane, "security"}] && r.e.consulted[key{pane, "frontend"}] && !r.e.consulted[key{pane, "copy"}]
	r.e.mu.Unlock()
	if !consulted {
		t.Fatal("members that answered skip the turn; the failed one doesn't")
	}
}

func TestAskCouncilCountsAsOneAsk(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.MCP.MaxAsksPerTurn = 1 }, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	if _, err := askCouncilNow(t, r, "first?"); err != nil {
		t.Fatal(err)
	}
	councilDone(t, r)
	n := cb.total()
	if _, err := askCouncilNow(t, r, "second?"); !errors.Is(err, ErrAskLimit) {
		t.Fatalf("got %v", err)
	}
	if cb.total() != n {
		t.Fatal("a refused council must run nothing")
	}
}

func TestAskCouncilRefusesWithoutDotsOrBudget(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.SessionBudgetCalls = 2 }, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	if _, err := askCouncilNow(t, r, "q"); err == nil || !strings.Contains(err.Error(), "no dots are attached") {
		t.Fatalf("no attachment: %v", err)
	}
	seat(t, r, "security", "frontend")
	if _, err := askCouncilNow(t, r, "q"); err == nil {
		t.Fatal("2 members + chair = 3 calls > the pane's 2: the preflight must refuse")
	}
	if _, err := askCouncilNow(t, r, "  "); err == nil {
		t.Fatal("empty question accepted")
	}
	if cb.total() != 0 {
		t.Fatalf("refused councils ran %d calls", cb.total())
	}
}

func TestAskCouncilIsAbandonedWhenTheAgentGivesUp(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", step{hang: true})
	cb.on("Frontend", step{hang: true})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := r.e.AskCouncil(ctx, pane, "q"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	councilDone(t, r)
	eventually(t, "member runs cancelled", func() bool { return cb.running() == 0 })
	for _, c := range r.q.Pending(pane) {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("an abandoned council makes no card: %+v", c)
		}
	}
	if feedHas(r, state.OutcomeAsked, "members answered") {
		t.Fatal("an abandoned council logs no asked line")
	}
}

func TestQueryRoutesAskCouncil(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("warn", "Rate-limit /login."))
	raw, _ := json.Marshal(ipc.AskCouncilArgs{PaneID: pane, Question: "anything before I ship?"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := r.e.Query(ctx, ipc.CmdAskCouncil, raw)
	res, ok := v.(ipc.CouncilResult)
	if err != nil || !ok || !strings.Contains(res.Digest, "Rate-limit /login") {
		t.Fatalf("query: %+v %v", v, err)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./internal/engine/ -run AskCouncil`
Expected: compile error (`r.e.AskCouncil undefined`).

- [ ] **Step 3: Give Phase 3's council a result callback**

`internal/engine/council.go` (Phase 3). In `type councilRun struct`, find:

```go
	ctx      context.Context
	cancel   context.CancelFunc
```

Replace with:

```go
	ctx      context.Context
	cancel   context.CancelFunc
	result   func(parts []council.Part, body, note string) // ask_council (Phase 5): the digest instead of a card
	done     chan struct{}                                  // closed when runCouncil ends
```

Replace the whole `Council` function (from its doc comment `// Council asks every eligible attached dot` through its closing `return run.id, nil` and `}`) with:

```go
// Council asks every eligible attached dot (or the listed ones) the same
// question in parallel and turns their answers into one council card. It
// returns the council id at once; the council runs in the background.
func (e *Engine) Council(ctx context.Context, paneID, question string, slugs []string) (string, error) {
	run, err := e.startCouncil(ctx, paneID, question, slugs, nil)
	if err != nil {
		return "", err
	}
	return run.id, nil
}

// startCouncil plans and starts a council. With result set (ask_council,
// Phase 5) the merged digest goes to result instead of a queue card; either
// way run.done closes when the council ends.
func (e *Engine) startCouncil(ctx context.Context, paneID, question string, slugs []string, result func(parts []council.Part, body, note string)) (*councilRun, error) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", state.ErrNoAttachment, paneID)
	}
	if e.councilBusy(paneID) {
		return nil, ErrCouncilRunning
	}
	pf, members, skipped := e.councilPlan(att, slugs)
	if !pf.Allowed {
		return nil, errors.New(pf.Refusal)
	}
	turns, err := e.reader(att).Turns(ctx)
	if err != nil {
		return nil, fmt.Errorf("read conversation: %w", err)
	}
	q := []rune(strings.TrimSpace(textutil.SanitizeModelText(question)))
	if len(q) > maxQuestionChars {
		q = q[:maxQuestionChars]
	}
	run := &councilRun{paneID: paneID, question: string(q), rounds: pf.Rounds, turns: turns, members: members, skipped: skipped,
		result: result, done: make(chan struct{})}
	if len(turns) > 0 {
		run.turn = turns[len(turns)-1].Index
	}
	run.ctx, run.cancel = context.WithCancel(e.ctx)
	e.mu.Lock()
	if e.councils[paneID] != nil {
		e.mu.Unlock()
		run.cancel()
		return nil, ErrCouncilRunning
	}
	if e.councils == nil {
		e.councils = map[string]*councilRun{}
	}
	e.councilSeq++
	run.id = fmt.Sprintf("council-%d", e.councilSeq)
	run.phase, run.round, run.total = view.CouncilMembers, 1, len(members)
	e.councils[paneID] = run
	delete(e.preflights, paneID)
	e.mu.Unlock()
	e.broadcast()
	e.spawn(func() { e.runCouncil(run) })
	return run, nil
}
```

This is Phase 3's `Council` body with `return nil, …` errors, the two new fields set, and the run returned. If the reconciled Phase 3 `Council` differs from its plan text (for example Phase 4's Step 7 touched `councilPlan`, not `Council`), keep the real body and make only those three changes.

Find:

```go
func (e *Engine) runCouncil(run *councilRun) {
	defer e.endCouncil(run)
```

Replace with:

```go
func (e *Engine) runCouncil(run *councilRun) {
	defer close(run.done) // runs after endCouncil: the council is forgotten when done closes
	defer e.endCouncil(run)
```

Find (the last line of `runCouncil`):

```go
	e.addCouncilCard(run, all, body, note)
}
```

Replace with:

```go
	if run.result != nil {
		run.result(all, body, note) // ask_council: the digest goes back to the agent, no card
		return
	}
	e.addCouncilCard(run, all, body, note)
}
```

- [ ] **Step 4: Write `AskCouncil` and route it**

Create `internal/engine/ask_council.go`:

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"herdr-dots/internal/council"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/state"
)

// AskCouncil convenes every eligible attached dot for the MCP server's
// ask_council and returns the merged digest instead of adding a council
// card. It counts as one ask against the [mcp] limits; members that answer
// are marked consulted. If ctx ends first, the council is abandoned.
func (e *Engine) AskCouncil(ctx context.Context, paneID, question string) (ipc.CouncilResult, error) {
	q := strings.TrimSpace(question)
	if q == "" {
		return ipc.CouncilResult{}, errors.New("the question is empty")
	}
	if _, ok := e.d.Attachments.Get(paneID); !ok {
		return ipc.CouncilResult{}, errors.New("no dots are attached to this pane (the developer attaches them with prefix+d)")
	}
	if err := e.limiter().take(paneID, e.config().MCP.Normalized()); err != nil {
		return ipc.CouncilResult{}, err
	}
	before := e.d.Usage.PaneCost(paneID)
	type outcome struct {
		parts      []council.Part
		body, note string
	}
	got := make(chan outcome, 1)
	run, err := e.startCouncil(ctx, paneID, q, nil, func(parts []council.Part, body, note string) {
		got <- outcome{parts, body, note}
	})
	if err != nil {
		return ipc.CouncilResult{}, err
	}
	select {
	case <-run.done:
	case <-ctx.Done():
		e.stopCouncil(run)
		return ipc.CouncilResult{}, fmt.Errorf("council: %w", ctx.Err())
	case <-e.ctx.Done():
		return ipc.CouncilResult{}, errors.New("council: the dots engine stopped")
	}
	var o outcome
	select {
	case o = <-got:
	default:
		return ipc.CouncilResult{}, errors.New("council failed: no member answered")
	}
	res := ipc.CouncilResult{Digest: o.body, Note: o.note, CostUSD: max(0, e.d.Usage.PaneCost(paneID)-before), Members: []ipc.CouncilMember{}}
	var heard []string
	for _, p := range o.parts {
		m := ipc.CouncilMember{Slug: p.Slug, Stance: p.Stance, Failed: p.Failed()}
		if p.Relevant() {
			m.Severity = p.Verdict.Severity
		}
		res.Members = append(res.Members, m)
		if !p.Failed() {
			heard = append(heard, p.Slug)
		}
	}
	e.markConsulted(paneID, heard...)
	e.feed(paneID, councilSlug, run.turn, state.OutcomeAsked,
		fmt.Sprintf("%s · %d of %d members answered", council.MaxSeverity(o.parts), council.Answered(o.parts), len(o.parts)))
	return res, nil
}

// stopCouncil abandons a council whose agent gave up: member and chair runs
// are cancelled and no result or card is produced.
func (e *Engine) stopCouncil(run *councilRun) {
	e.mu.Lock()
	run.abandoned = true
	if e.councils[run.paneID] == run {
		delete(e.councils, run.paneID)
	}
	e.mu.Unlock()
	run.cancel()
}
```

In `internal/engine/ask.go` (`Query`), find:

```go
	case ipc.CmdListQueue:
```

Replace with:

```go
	case ipc.CmdAskCouncil:
		var a ipc.AskCouncilArgs
		if err := decodePane(raw, &a, &a.PaneID); err != nil {
			return nil, err
		}
		r, err := e.AskCouncil(ctx, a.PaneID, a.Question)
		return r, err
	case ipc.CmdListQueue:
```

Run `gofmt -w internal/engine`.

- [ ] **Step 5: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race -count=3 ./internal/engine/`
Expected: PASS three times, including every Phase 3 council test (`Council` behaves exactly as before) and Task 7's tests.

- [ ] **Step 6: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add internal/engine
git commit -m "feat(engine): ask_council returns the council digest to the MCP server without a card"
```

---

### Task 10: `herdr-dots mcp` command

**Depends on:** Task 2 (`ipc.Client`, query types), Task 3 (`config.Config.MCP`), Task 8 (`mcpserver.New`, `Deps`, `NewIPCEngine`, `NotInHerdr`). Wave 3 (runs beside Task 9).

**Files:**
- Create: `cmd/herdr-dots/mcp.go`, `cmd/herdr-dots/mcp_test.go`
- Modify: `cmd/herdr-dots/main.go`, `cmd/herdr-dots/engine.go`
- Shared with Phases 2 and 4 (already merged) and Task 6: `cmd/herdr-dots/main.go`, `cmd/herdr-dots/engine.go`

**Interfaces:**
- Consumes: Task 8's `mcpserver.New`, `mcpserver.Deps`, `mcpserver.NewIPCEngine`, `mcpserver.NotInHerdr`; Task 2's `ipc.Client`, `ipc.Serve`, `ipc.AskArgs`, `ipc.AskResult`, `ipc.CmdAsk`; Task 3's `config.Config.MCP`; v1 `config.Load`, `(config.Config) RunTimeout()`, `paths.New`, `paths.SessionKey`, `(paths.Paths) Ensure/EngineSock/ConfigFile/SessionDir`, `ensureEngine(p paths.Paths) error`, `openEngineLog(path string) (*os.File, error)`; Phase 2's `memoryStoreFor(p paths.Paths, cfg config.Config, logf func(string, ...any)) *memory.Store`; go-sdk `mcp.StdioTransport`, `mcp.IOTransport`, `mcp.NewInMemoryTransports`, `mcp.NewClient`.
- Produces: `runMCP(args []string) error` registered as `"mcp"`; testable `serveMCP(ctx context.Context, args []string, getenv func(string) string, t mcp.Transport, ensure func(paths.Paths) error) error` (flags `--config-dir`, `--state-dir`, both required and absolute); `herdrEnv(v string) string` (an unexpanded `${…}` counts as unset); `mcpLogger(p paths.Paths) (*log.Logger, func())`; `engineEnv(env []string, p paths.Paths) []string`, used by `ensureEngine` for the detached engine's environment.

- [ ] **Step 1: Write the failing tests**

Create `cmd/herdr-dots/mcp_test.go` (Review Focus 1 included; the engine is never really started):

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/mcpserver"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/view"
)

// mcpDirs makes config and state dirs and the server's arguments.
func mcpDirs(t *testing.T) (cfg, state string, args []string) {
	t.Helper()
	root := t.TempDir()
	cfg, state = filepath.Join(root, "cfg"), filepath.Join(root, "state")
	return cfg, state, []string{"--config-dir", cfg, "--state-dir", state}
}

func mcpEnvOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func mcpNoEnsure(t *testing.T) func(paths.Paths) error {
	return func(paths.Paths) error { t.Error("the engine must not be started"); return nil }
}

// mcpSession runs serveMCP on in-memory transports and connects a client.
func mcpSession(t *testing.T, args []string, getenv func(string) string, ensure func(paths.Paths) error) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ct, st := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- serveMCP(ctx, args, getenv, st, ensure) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("serveMCP did not stop")
		}
	})
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func mcpToolText(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

// Review Focus 1: a codex dot run has no HERDR_PANE_ID (CleanEnv), so the
// server it spawns refuses every tool and says so in mcp.log.
func TestServeMCPOutsideHerdrRefusesEveryTool(t *testing.T) {
	_, state, args := mcpDirs(t)
	cs := mcpSession(t, args, mcpEnvOf(map[string]string{"HERDR_SOCKET_PATH": "/tmp/herdr.sock"}), mcpNoEnsure(t))
	text, isErr := mcpToolText(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if !isErr || text != mcpserver.NotInHerdr {
		t.Fatalf("isError=%v %q", isErr, text)
	}
	key := paths.SessionKey("/tmp/herdr.sock")
	b, err := os.ReadFile(filepath.Join(state, key, "mcp.log"))
	if err != nil || !strings.Contains(string(b), "outside a Herdr pane") || !strings.Contains(string(b), "ask_dot refused") {
		t.Fatalf("mcp.log %q %v", b, err)
	}
}

func TestServeMCPTreatsUnexpandedPlaceholdersAsUnset(t *testing.T) {
	_, _, args := mcpDirs(t)
	env := mcpEnvOf(map[string]string{"HERDR_PANE_ID": "${HERDR_PANE_ID}", "HERDR_SOCKET_PATH": "${HERDR_SOCKET_PATH:-}"})
	cs := mcpSession(t, args, env, mcpNoEnsure(t))
	if text, isErr := mcpToolText(t, cs, "list_dots", map[string]any{}); !isErr || text != mcpserver.NotInHerdr {
		t.Fatalf("isError=%v %q", isErr, text)
	}
}

func TestServeMCPWritesNothingToStdoutWithoutAClient(t *testing.T) {
	_, _, args := mcpDirs(t)
	stdinR, stdinW := io.Pipe()
	stdinW.Close() // the client went away before saying anything
	var stdout bytes.Buffer
	err := serveMCP(context.Background(), args, mcpEnvOf(nil), &mcp.IOTransport{Reader: stdinR, Writer: mcpNopCloser{&stdout}}, mcpNoEnsure(t))
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must carry protocol only: %q", stdout.String())
	}
}

type mcpNopCloser struct{ io.Writer }

func (mcpNopCloser) Close() error { return nil }

func TestServeMCPNeedsAbsoluteDirs(t *testing.T) {
	err := serveMCP(context.Background(), []string{"--config-dir", "cfg", "--state-dir", "/s"}, mcpEnvOf(nil), &mcp.StdioTransport{}, mcpNoEnsure(t))
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("got %v", err)
	}
}

// mcpFakeEngine answers queries for one pane on the engine socket.
type mcpFakeEngine struct{ asked chan ipc.AskArgs }

func (f mcpFakeEngine) Handle(context.Context, string, json.RawMessage) error { return nil }
func (f mcpFakeEngine) Snapshot() view.Snapshot                               { return view.Snapshot{} }
func (f mcpFakeEngine) Watch() (<-chan view.Snapshot, func()) {
	return make(chan view.Snapshot), func() {}
}
func (f mcpFakeEngine) Query(_ context.Context, cmd string, raw json.RawMessage) (any, error) {
	if cmd != ipc.CmdAsk {
		return nil, errors.New("unexpected " + cmd)
	}
	var a ipc.AskArgs
	_ = json.Unmarshal(raw, &a)
	f.asked <- a
	return ipc.AskResult{Slug: a.Slug, Name: "Security", Relevant: true, Severity: "warn", Message: "Use httpOnly cookies."}, nil
}

func TestServeMCPAsksThePanesEngine(t *testing.T) {
	cfg, state, args := mcpDirs(t)
	sock := "/tmp/herdr-mcp-test-" + filepath.Base(t.TempDir()) + ".sock" // only hashed into the session key
	p := paths.New(cfg, state, sock)
	fe := mcpFakeEngine{asked: make(chan ipc.AskArgs, 1)}
	ensured := 0
	ensure := func(got paths.Paths) error {
		ensured++
		if got.EngineSock() != p.EngineSock() {
			t.Errorf("engine socket %s, want %s", got.EngineSock(), p.EngineSock())
		}
		ln, err := net.Listen("unix", got.EngineSock())
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go ipc.Serve(ctx, ln, fe)
		return nil
	}
	cs := mcpSession(t, args, mcpEnvOf(map[string]string{"HERDR_PANE_ID": "w1:p1", "HERDR_SOCKET_PATH": sock}), ensure)
	text, isErr := mcpToolText(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "JWT storage?"})
	if isErr || !strings.HasPrefix(text, "[◆ Security] (warn) Use httpOnly cookies.") {
		t.Fatalf("isError=%v %q", isErr, text)
	}
	if a := <-fe.asked; a != (ipc.AskArgs{PaneID: "w1:p1", Slug: "security", Question: "JWT storage?", IncludeTurns: true}) {
		t.Fatalf("engine got %+v", a)
	}
	if ensured != 1 {
		t.Fatalf("ensure ran %d times", ensured)
	}
	t.Cleanup(func() { os.Remove(p.EngineSock()) })
}

func TestEngineEnvSetsThePluginDirs(t *testing.T) {
	p := paths.New("/cfg", "/state", "/run/herdr.sock")
	env := engineEnv([]string{"PATH=/bin", "HERDR_PLUGIN_CONFIG_DIR=/old", "HERDR_PANE_ID=w1:p1"}, p)
	for _, want := range []string{"PATH=/bin", "HERDR_PANE_ID=w1:p1", "HERDR_PLUGIN_CONFIG_DIR=/cfg", "HERDR_PLUGIN_STATE_DIR=/state", "HERDR_SOCKET_PATH=/run/herdr.sock"} {
		if !slices.Contains(env, want) {
			t.Errorf("missing %s in %v", want, env)
		}
	}
	if slices.Contains(env, "HERDR_PLUGIN_CONFIG_DIR=/old") {
		t.Fatalf("stale dir kept: %v", env)
	}
}

func TestDispatchKnowsTheMCPCommands(t *testing.T) {
	for _, name := range []string{"mcp", "setup-mcp"} {
		if _, ok := commands[name]; !ok {
			t.Errorf("%s is not routed", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `GOTOOLCHAIN=local go test ./cmd/herdr-dots/`
Expected: compile errors (`undefined: serveMCP`, `undefined: engineEnv`).

- [ ] **Step 3: Write the command**

Create `cmd/herdr-dots/mcp.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/config"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/mcpserver"
	"herdr-dots/internal/paths"
)

// runMCP serves the Dots MCP server on stdio. Claude Code and Codex start it
// (herdr-dots setup-mcp writes their config). Stdout carries only protocol
// messages; logs go to mcp.log in the session's state folder.
func runMCP(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return serveMCP(ctx, args, os.Getenv, &mcp.StdioTransport{}, ensureEngine)
}

// serveMCP is runMCP with its environment, transport and engine starter
// injected (tests never start a real engine).
func serveMCP(ctx context.Context, args []string, getenv func(string) string, t mcp.Transport, ensure func(paths.Paths) error) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr) // never stdout: it is the protocol
	configDir := fs.String("config-dir", "", "the plugin's config dir (absolute)")
	stateDir := fs.String("state-dir", "", "the plugin's state dir (absolute)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !filepath.IsAbs(*configDir) || !filepath.IsAbs(*stateDir) {
		return errors.New("mcp needs absolute --config-dir and --state-dir (herdr-dots setup-mcp writes them)")
	}
	pane, sock := herdrEnv(getenv("HERDR_PANE_ID")), herdrEnv(getenv("HERDR_SOCKET_PATH"))
	p := paths.New(filepath.Clean(*configDir), filepath.Clean(*stateDir), sock)
	logger, closeLog := mcpLogger(p)
	defer closeLog()
	cfg, err := config.Load(p.ConfigFile())
	if err != nil {
		logger.Printf("config: %v (using defaults)", err)
	}
	run := cfg.RunTimeout()
	deps := mcpserver.Deps{
		Tools:          cfg.MCP.Normalized().Tools,
		AskTimeout:     run + 10*time.Second,
		CouncilTimeout: 3*run + 10*time.Second, // two member rounds plus the chair, at worst
		Logf:           logger.Printf,
	}
	if pane == "" || sock == "" {
		logger.Printf("started outside a Herdr pane (HERDR_PANE_ID %q, HERDR_SOCKET_PATH set: %v): every tool refuses", pane, sock != "")
	} else {
		deps.PaneID = pane
		deps.Engine = mcpserver.NewIPCEngine(ipc.Client{Path: p.EngineSock()}, pane, func() error {
			if err := p.Ensure(); err != nil {
				return err
			}
			return ensure(p)
		})
		deps.Memory = memoryStoreFor(p, cfg, logger.Printf)
		logger.Printf("serving pane %s through %s", pane, p.EngineSock())
	}
	err = mcpserver.New(deps).Run(ctx, t)
	logger.Printf("stopped: %v", err)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// herdrEnv reads a Herdr variable the client forwarded. An unexpanded
// "${VAR}" (a client without variable expansion) counts as unset.
func herdrEnv(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "${") {
		return ""
	}
	return v
}

// mcpLogger opens SessionDir/mcp.log (StateDir/mcp.log outside Herdr),
// rotated like engine.log. Logging never fails the server.
func mcpLogger(p paths.Paths) (*log.Logger, func()) {
	dir := p.StateDir
	if p.SocketPath != "" {
		dir = p.SessionDir
	}
	discard := log.New(io.Discard, "", 0)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return discard, func() {}
	}
	f, err := openEngineLog(filepath.Join(dir, "mcp.log"))
	if err != nil {
		return discard, func() {}
	}
	return log.New(f, "herdr-dots mcp ", log.LstdFlags), func() { f.Close() }
}
```

In `cmd/herdr-dots/main.go`, find:

```go
	"setup-mcp":  runSetupMCP,
```

Replace with:

```go
	"setup-mcp":  runSetupMCP,
	"mcp":        runMCP,
```

In `cmd/herdr-dots/engine.go` (`ensureEngine`), find:

```go
	cmd := exec.Command(exe, "engine")
```

Replace with:

```go
	cmd := exec.Command(exe, "engine")
	cmd.Env = engineEnv(os.Environ(), p)
```

and add, right above `func waitForSocket`:

```go
// engineEnv is a detached engine's environment: the caller's, with the
// plugin dirs and Herdr socket taken from p. The MCP server runs under the
// agent CLI, whose environment has no HERDR_PLUGIN_* variables, and the
// engine reads them (paths.FromEnv).
func engineEnv(env []string, p paths.Paths) []string {
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "HERDR_PLUGIN_CONFIG_DIR", "HERDR_PLUGIN_STATE_DIR", "HERDR_SOCKET_PATH":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HERDR_PLUGIN_CONFIG_DIR="+p.ConfigDir, "HERDR_PLUGIN_STATE_DIR="+p.StateDir, "HERDR_SOCKET_PATH="+p.SocketPath)
}
```

Add `"strings"` to `engine.go`'s imports if it isn't there yet. Run `gofmt -w cmd/herdr-dots`.

- [ ] **Step 4: Run the tests to see them pass**

Run: `GOTOOLCHAIN=local go test -race -count=3 ./cmd/herdr-dots/`
Expected: PASS three times, including every earlier `cmd/herdr-dots` test (`TestBuildEngineLoadsDotsFromConfigDir`, the setup-mcp tests, Phase 2's memory CLI tests, Phase 4's keys tests).

- [ ] **Step 5: Smoke-test the binary over real stdio**

```bash
GOTOOLCHAIN=local go build -o bin/herdr-dots ./cmd/herdr-dots
D=$(mktemp -d)
{ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_dots","arguments":{}}}'; sleep 1; } \
  | env -u HERDR_PANE_ID -u HERDR_SOCKET_PATH ./bin/herdr-dots mcp --config-dir "$D/cfg" --state-dir "$D/state"
cat "$D/state/mcp.log"
```

(The `sleep` keeps stdin open: the server stops at end of input, and a request still in flight then gets no answer.) Expected: exactly two lines on stdout, and nothing else: `{"jsonrpc":"2.0","id":1,"result":{…"protocolVersion":"2025-11-25","serverInfo":{"name":"herdr-dots",…}}}` (an initialize-era client is answered through version negotiation) and `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"herdr-dots: not running inside a Herdr pane; dots are unavailable here."}],"isError":true}}`; `mcp.log` has `started outside a Herdr pane`, `list_dots refused: not inside a Herdr pane` and `stopped: <nil>`. (Confirmed with the scratch build while planning.) Record the output in the Task 1 notes under "Smoke test".

- [ ] **Step 6: Full check and commit**

```bash
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add cmd/herdr-dots docs/superpowers/notes/2026-10-08-phase-5-verification.md
git commit -m "feat: herdr-dots mcp serves the Dots MCP server on stdio for the pane it runs in"
```

---

### Task 11: README, roadmap, version 0.5.0 and end-to-end check

**Depends on:** all previous tasks. Wave 4.

**Files:**
- Modify: `README.md`, `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, `internal/version/version.go`, `herdr-plugin.toml`, `docs/superpowers/notes/2026-10-08-phase-5-verification.md`

**Interfaces:**
- Consumes: everything above.
- Produces: user documentation for the MCP server (tools, install, limits, configuration) with the explicit warning that MCP answers bypass the approval queue; version `0.5.0`; Phase 2 stays **VITAL**; the product stays "not done".

- [ ] **Step 1: Bump the version**

`internal/version/version.go`: change `const Version = "0.4.0"` to `const Version = "0.5.0"`. `herdr-plugin.toml`: change `version = "0.4.0"` to `version = "0.5.0"`.

- [ ] **Step 2: Update the README status banner**

Replace the status banner at the top (whatever Phase 4 left) with:

```markdown
> **Status: scaffold (v0.5.0).** Phases 1 (core loop), 2 (dot memory, VITAL), 3 (council), 4 (API and
> Ollama backends, cost) and 5 (Dots MCP server) are in. The project is not done and not ready for
> general use; see the [Roadmap](#roadmap) for what's missing.
```

- [ ] **Step 3: Add the "Dots MCP server" section**

Add this section after the council section (Phase 3) and the "Backends and keys" section (Phase 4), before "## Files":

````markdown
### Dots MCP server: let the main agent ask the dots

Claude Code or Codex can consult your dots themselves while they work ("ask the security dot before you
write the token handling"). Install the server once:

```bash
./bin/herdr-dots setup-mcp                 # Claude Code and/or Codex, whichever is installed
./bin/herdr-dots setup-mcp --client claude # or --client codex
./bin/herdr-dots setup-mcp --remove        # take it out again
```

`setup-mcp` prints the exact commands and the config change, asks before applying (`--yes` skips the
question) and backs up every file it touches (`<file>.bak-herdr-dots-<time>`). For Claude Code it runs
`claude mcp add --scope user …` (Claude Code edits its own `~/.claude.json`). For Codex it adds one
marked block to `~/.codex/config.toml` and leaves every other line alone:

```toml
# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)
[mcp_servers.herdr-dots]
command = "/path/to/herdr-dots/bin/herdr-dots"
args = ["mcp", "--config-dir", "…", "--state-dir", "…"]
env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]
startup_timeout_sec = 20
tool_timeout_sec = 180
# <<< herdr-dots MCP server
```

Restart the agent in its Herdr pane afterwards. If you move or rebuild the plugin somewhere else, run
`setup-mcp` again. The manage popup's footer shows where it is installed.

The agent sees these tools (as `mcp__herdr-dots__<tool>`):

| Tool | What it does |
|---|---|
| `list_dots` | The dots attached to the agent's pane: purpose, mode, status, budget |
| `ask_dot` | Ask one attached dot a question and wait for its answer (up to the run timeout) |
| `ask_council` | Ask every attached dot at once and get the merged council digest |
| `list_queue` | Read the dots' cards waiting for you in this pane |
| `read_memory` | Read the project's approved dot memory |
| `propose_memory` | Propose a convention, decision or note; it waits for your approval in the memory screen |

> **Warning: MCP answers bypass the approval queue.** Whatever `ask_dot`, `ask_council` and `list_queue`
> return goes straight to the main agent, without you approving it first (the queue still gets nothing
> from these calls). The calls show up in the agent's transcript and as `◆ asked by agent` lines in the
> Dots pane feed. If you don't want that, remove the tools you don't want from `[mcp] tools`.

What keeps it in check:

- Only dots **attached** to the agent's own pane can be asked; a server only ever serves the pane it was
  started in. Outside Herdr (or inside a dot's own run) every tool refuses.
- At most 3 asks per turn and 6 per minute per pane (a council counts as one), on top of the normal call
  and dollar budgets. Over the limit the agent is told to continue without the dot.
- A dot that was asked skips its own review of that turn (no double advice); the roster shows `consulted`.
- Memory the agent writes is always a project proposal you approve, and secret-looking text is refused.
- Results are capped at about 24,000 characters.

```toml
[mcp]
tools = ["list_dots", "ask_dot", "ask_council", "list_queue", "read_memory", "propose_memory"]
max_asks_per_turn   = 3
max_asks_per_minute = 6
```

`ask_dot` waits up to `run_timeout_seconds` + 10 s (130 s by default). Codex stops waiting after
`tool_timeout_sec` (180 s as installed); a council can take longer with the default 120 s run timeout,
so raise `tool_timeout_sec` or lower `run_timeout_seconds` if Codex gives up on councils. The server
logs to `mcp.log` in the plugin's state folder; it never prints anything but protocol messages.
````

- [ ] **Step 4: Extend "Privacy and safety", "Known gaps" and the roadmap table**

Add to "Privacy and safety":

```markdown
- With the MCP server installed, the main agent can read dot answers, the queue and approved memory
  **without your approval** (see "Dots MCP server"); dots still never change files, and agent-written
  memory is always a proposal.
```

In "Known gaps in this scaffold", remove "no MCP server" from the bullet that lists missing features (leave what Phases 2–4 left) and add:

```markdown
- The MCP server is verified live with Claude Code; Codex support follows Codex's documented config
  format and has **not been verified live** (see `docs/superpowers/notes/2026-10-08-phase-5-verification.md`).
- No MCP resources or prompts, no HTTP transport, no access for agents outside Herdr; MCP on Windows
  comes with Phase 6.
```

In the "Roadmap" table, change row 5 to:

```markdown
| 5 | Dots MCP server so the main agent can consult dots itself: ask a dot, convene a council, read the queue and memory, propose memory (this release) |
```

Keep row 2 exactly as it is (**Dot memory — VITAL.**). Don't add any wording that calls the product done, finished, complete or ready.

- [ ] **Step 5: Update the roadmap spec**

In `docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`, under "### Phase 5: Dots MCP server", add one line:

```markdown
Implementation plan: `docs/superpowers/plans/2026-10-08-herdr-dots-phase-5-mcp-server.md` (built in 0.5.0; the product is not done until Phase 10).
```

- [ ] **Step 6: End-to-end check (manual, never touching your real agent configs)**

Build and start a test session:

```bash
GOTOOLCHAIN=local go build -o bin/herdr-dots ./cmd/herdr-dots && ./bin/herdr-dots version
env -u CLAUDECODE -u CLAUDE_CODE_ENTRYPOINT -u CLAUDE_CODE_CHILD_SESSION herdr --session dots-test
```

Expected version line: `herdr-dots 0.5.0 (scaffold)`.

1. **Installer, against throwaway configs.** In a shell inside the test session:

   ```bash
   T=$(mktemp -d); mkdir -p "$T/codex" && : > "$T/codex/config.toml"
   CLAUDE_CONFIG_DIR="$T/claude" CODEX_HOME="$T/codex" ./bin/herdr-dots setup-mcp
   CLAUDE_CONFIG_DIR="$T/claude" claude mcp get herdr-dots
   cat "$T/codex/config.toml"
   CLAUDE_CONFIG_DIR="$T/claude" CODEX_HOME="$T/codex" ./bin/herdr-dots setup-mcp --remove --yes
   wc -c "$T/codex/config.toml"
   ```

   Expected: the preview shows the `claude mcp add --scope user --env 'HERDR_PANE_ID=${HERDR_PANE_ID:-}' …` command and the Codex block as a `+` diff; after `y`, `claude mcp get` shows the server with those env values; the Codex file holds exactly the block; after `--remove` the Codex file is 0 bytes again and `claude mcp get herdr-dots` fails. Your real `~/.claude.json` and `~/.codex/config.toml` are untouched (`ls -l` their modification times before and after).

2. **Ask from Claude Code.** Write the entry setup-mcp would install into a temp MCP config and start Claude Code in an agent pane of the test session with it (this adds the server for that session only, writing nothing):

   ```bash
   cat > "$T/mcp.json" <<EOF
   {"mcpServers": {"herdr-dots": {"type": "stdio", "command": "$PWD/bin/herdr-dots",
     "args": ["mcp", "--config-dir", "$(herdr plugin config-dir herdr-dots)", "--state-dir", "$(cat "$(herdr plugin config-dir herdr-dots)/.state-dir")"],
     "env": {"HERDR_PANE_ID": "\${HERDR_PANE_ID:-}", "HERDR_SOCKET_PATH": "\${HERDR_SOCKET_PATH:-}"}}}}
   EOF
   claude --mcp-config "$T/mcp.json"
   ```

   Attach a Security dot to that pane (`prefix+d`), open the Dots pane (`prefix+a`), and ask Claude: "Use the herdr-dots tools: list the dots, then ask the security dot whether storing the JWT in localStorage is OK." Expected: Claude calls `list_dots` and `ask_dot` and reports the dot's answer; no new card appears in the queue; the feed shows `◆ asked by agent · <severity>` and the roster shows `consulted`; after Claude's turn ends, the Security dot does not review that turn (feed has no new pass/flagged line for it), and it reviews the next turn as usual. Ask Claude to call `ask_dot` five times in one turn: the 4th call returns `ask limit reached; continue without the dot (3 per turn)`. Ask it to call `ask_council` (with 2+ dots attached) and `propose_memory` once: the council digest comes back with no card, and the proposal appears in the manage popup's memory screen (`prefix+d`, `P`).

3. **No recursion.** While a dot reviews a turn, check `mcp.log` in the session's state folder: each agent start logs one `serving pane …` line; dot runs add none (claude dots pass `--strict-mcp-config`). If Codex is installed, attach a codex dot and confirm its run either starts no herdr-dots server or logs `… refused: not inside a Herdr pane` only.

Record the results (pass/fail per numbered check, with the feed lines you saw) under "## End-to-end (Task 11)" in `docs/superpowers/notes/2026-10-08-phase-5-verification.md`. Anything that fails is a bug to fix before the commit, not a note.

- [ ] **Step 7: Final check and commit**

```bash
grep -niE '\b(done|finished|complete|ready)\b' README.md | grep -viE 'not (done|finished|complete|ready)|ready to publish' || true
gofmt -l . ; GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...
git add README.md docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md internal/version/version.go herdr-plugin.toml docs/superpowers/notes/2026-10-08-phase-5-verification.md
git commit -m "docs: Phase 5 Dots MCP server in the README (MCP answers bypass the queue); version 0.5.0 (product not done)"
```

Expected: the grep prints nothing that claims the product is done.

---

## Self-review

Run against the spec (`docs/superpowers/specs/2026-10-08-herdr-dots-phase-5-mcp-server-design.md`) and ruling 9 while writing this plan.

**1. Spec coverage.**

| Spec item | Task |
|---|---|
| §1 criterion 1: `setup-mcp` installs into Claude Code and Codex with preview, confirmation, backup; `--remove` | 6 (Plan decisions 2–6) |
| §1 criterion 2: the six tools, answers from the pane's engine with its budgets, memory and backends | 7, 8, 9, 10 |
| §1 criterion 3: outside Herdr / inside a dot run every tool refuses; no recursion | 5, 8, 10 |
| §1 criterion 4: `ask_dot` within the run timeout, reports dot, severity, message, cost; no card | 7, 8 |
| §1 criterion 5: a consulted dot skips that turn | 3, 7 |
| §1 criterion 6: per-turn and per-minute ask limits on top of budgets | 3, 7, 9 |
| §1 criterion 7: MCP memory writes land as proposals | 4, 8 |
| P1 stdio; P2 IPC over `dots.sock`, `--config-dir/--state-dir` baked in, `ensureEngine` | 8, 10 |
| P3 `HERDR_PANE_ID` scoping | 8, 10 |
| P4 `Response.Data`, `Querier`, `Client.Query` with the caller's deadline | 2 |
| P5 synchronous `Ask`, forced, no card, feed `asked` | 7 |
| P6 attached dots only, paused refuses | 7 |
| P7 `consulted[key]`, `ReasonConsulted` once | 7 |
| P8 3/turn, 6/minute | 3, 7 |
| P9 `[mcp] tools`, `list_queue`/`read_memory` on by default | 3, 8 |
| P10 `propose_memory` = project proposals, source `agent` | 4, 8 |
| P11 refuse without pane; codex `-c …enabled=false` | 5, 8, 10 |
| P12 user scope | 6 |
| P13 Codex `tool_timeout_sec = 180`; Claude Code timeout | 6, 8, 10 (Plan decision 14) |
| §4.2 tool inputs/outputs, caps (4,000-char question, 2,000-char bodies × 20, limit ≤ 50, 500-char proposals), advisory descriptions | 7, 8 |
| §4.3 interface changes (`ipc`, `engine`, `FilterInput`, `state`, `memory`, `config`, `CodexCLI.BuildCmd`, `main.dispatch`) | 2, 3, 4, 5, 6, 7, 9, 10 |
| §5.1 install results and `mcp-install.json` | 6 |
| §5.2 `[mcp]` config | 3 |
| §6.1 server start (lazy `ensureEngine`, stable tool list, logs not stdout) | 8, 10 |
| §6.2–6.4 data flow | 7, 8, 9 |
| §7 UI: `setup-mcp` output, manage footer, feed `◆ asked by agent · warn`, roster `consulted` | 3, 6, 7 |
| §8.1 error table | 7, 8 (messages as in the spec; "Security failed: <hint>", "pane closed", "dots engine not reachable…") |
| §8.2 safety (scope, recursion, queue-bypass in README, memory, limits, output size) | 5, 7, 8, 11 |
| §8.3 testing layers (in-memory client, IPC, engine, install, live, `-race`/`vet`) | 2–10, 1 and 11 (live) |
| §9 V1–V6 | 1 (and "Confirmed while planning") |
| Ruling 9 (attached only; `list_queue` on with the README warning) | 7, 8, 11 |

Deviations, each recorded in Plan decisions: Codex installs through a managed block instead of `codex mcp add` (3); `list_dots` returns a `DotsResult` with the project instead of `DotInfo.ProjectID` (8); `ListQueue` returns `QueueResult{Cards, Total}` (so the agent learns how many weren't shown); the limiter lives only in the engine (9).

**2. Placeholder scan.** No "TBD", "TODO", "implement later", "similar to Task N" or code-free code steps. Task 9's `startCouncil` restates Phase 3's `Council` body in full. The only conditional instructions are the reconciliation fallbacks for files earlier phases rewrote, each naming the exact edit.

**3. Type consistency.** Checked across tasks: `ipc.DotsResult`/`AskResult`/`CouncilResult`/`QueueResult` (Task 2) are what `Engine.ListDots/Ask/AskCouncil/ListQueue` (Tasks 7, 9) return, what `ipcEngine` decodes and `EngineAPI` declares (Task 8), and what the cmd test's fake engine returns (Task 10). `config.MCPConfig.Normalized()` (Task 3) is called by `Engine.Ask`, `Engine.AskCouncil` and `serveMCP`. `askLimiter.take(paneID, config.MCPConfig)`, `markConsulted(paneID, slugs...)` and `decodePane(raw, v, &pane)` match between Tasks 7 and 9. `memory.Store.Propose(p string, e Entry) (Entry, error)` (Task 4) matches its use in `proposeMemory` (Task 8). `mcpinstall.Apply(p, func(Command) error, now)` (Task 6) matches `setupMCP`. `mcpserver.NotInHerdr` is the text Tasks 8 and 10 assert. `state.OutcomeAsked`'s detail format (`"<severity> · …"`) is what `askDetail`/`AskCouncil` write and what the pane's `feedOutcome` cuts.

**4. Review Focus.** Each of the five items has its test in the owning task: Task 5/8/10 (codex dot run), Task 7 (ask loop), Task 8 (engine not running), Task 2/8 (long run), Task 6 (user's Codex config). Other inputs checked and covered by tests: an unexpanded `${HERDR_PANE_ID}` (Task 10), oversized results (Task 8), a secret in a proposal (Tasks 4, 8), a file changed between preview and apply (Task 6), an inline-table Codex definition (Task 6), an agent giving up mid-council (Task 9), a failed ask not consulting (Task 7).

# Herdr Dots Phase 5: Dots MCP Server, Implementation Plan Outline

**Status: DRAFT, pending user review**

> **This is an outline, not an executable plan.** Earlier phases' code doesn't exist yet. When Phase 5 starts, write the full step-by-step plan with superpowers:writing-plans against the real code (re-check the MCP Go SDK and the Claude Code / Codex MCP docs on the web), then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Ship a stdio MCP server (`herdr-dots mcp`) and an installer (`herdr-dots setup-mcp`) so Claude Code and Codex can list, ask and convene dots, read the queue and memory, and propose memory, all through the pane's own engine.

**Architecture:** The MCP server (official Go SDK v1.7.0+) runs as a child of the agent CLI inside the Herdr pane, finds the engine socket from `HERDR_SOCKET_PATH` plus install-time `--state-dir`, and calls a new data-returning IPC `Query`. The engine gains a synchronous `Ask`/`AskCouncil`, a consulted-dot guard and ask limits. Memory tools go straight to `memory.Store`.

**Tech Stack:** Go 1.25; new dependency `github.com/modelcontextprotocol/go-sdk` (≥ v1.7.0, MCP 2026-07-28 with backward compatibility); existing `BurntSushi/toml`.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-5-mcp-server-design.md`

## Global Constraints

- The product is **not** done after this phase. Version `0.5.0`. README roadmap keeps Phase 2 **VITAL**.
- Go module `herdr-dots`, `go 1.25`; macOS and Linux only.
- The MCP server never writes to stdout except protocol messages; logs go to `SessionDir/mcp.log`.
- Every tool is scoped to `HERDR_PANE_ID`; without it, every tool returns an error result (no recursion, no cross-pane access).
- `ask_dot` never creates a queue card; agent memory writes are always project-scope proposals.
- Ask limits: 3 per turn and 6 per minute per pane by default; budgets apply.
- Tool results ≤ 24,000 characters.
- The installer never edits client configs without a preview, confirmation (or `--yes`) and a backup.
- v1 `ipc.Client.Call` behavior is unchanged.
- Files `0600`, directories `0700`, atomic writes. `go test -race ./...` and `go vet ./...` before every commit.

## Review Focus

1. **A codex dot run spawning the MCP server** (codex loads user MCP config). Expect the server to refuse every tool (no `HERDR_PANE_ID`), and the dot run unaffected. (Task 4, Task 6)
2. **The agent calling `ask_dot` in a loop.** Expect the 4th call in one turn to return "ask limit reached" without spending budget. (Task 3)
3. **The engine not running when the agent first calls a tool.** Expect `ensureEngine` to start it, the call to succeed within the timeout, and the server's startup itself not blocked. (Task 4)
4. **A 100-second dot run.** Expect the IPC `Query` deadline to follow the context (no 30-second cut-off) and the result returned before the Codex 180 s tool timeout. (Task 2)
5. **The user's existing `~/.codex/config.toml` with other servers and comments.** Expect `setup-mcp` to preserve them, back up the file, and make `--remove` restore exactly. (Task 5)

---

## File Structure

```
internal/ipc/ipc.go                   + Response.Data, Querier, QueryCmds, Client.Query, arg/result types
internal/engine/ask.go                Ask, AskCouncil, ListDots, ListQueue, Query, limiter, consulted
internal/engine/filter.go             + Consulted, ReasonConsulted
internal/engine/council.go            sink variant for AskCouncil
internal/state/feed.go                + OutcomeAsked
internal/memory/store.go              + SourceAgent, Propose
internal/config/config.go             + MCPConfig
internal/backend/codex.go             -c mcp_servers.herdr-dots.enabled=false
internal/mcpserver/server.go          New, tools, EngineAPI, limiter wiring, formatting
internal/mcpinstall/install.go        Plan, Apply, Remove for claude and codex
cmd/herdr-dots/mcp.go                 runMCP
cmd/herdr-dots/setupmcp.go            runSetupMCP
cmd/herdr-dots/main.go                route mcp, setup-mcp
internal/ui/manage/model.go           MCP status footer
README.md                             MCP section, safety note about queue bypass
```

---

## Tasks

### Task 1: Verification spike (V1–V6)

**Files:** Create `docs/superpowers/notes/phase-5-verification.md`.
**Interfaces:** Findings only: environment forwarding (Claude Code V1, Codex V2), the codex `-c` disable override (V3), the Claude Code tool timeout control (V4), negotiated protocol versions (V5), `structuredContent` visibility (V6).
**Tests:** None. Each finding records the command, the evidence, and "assumption holds: yes/no". STOP and report on any "no".

### Task 2: Data-returning IPC

**Files:** Modify `internal/ipc/ipc.go`. Test: extend `internal/ipc/ipc_test.go`.
**Interfaces:**
- Consumes: v1 `Request`, `Response`, `Handler`, `Serve`, `Client`.
- Produces: `Response.Data json.RawMessage`; `type Querier interface{ Query(ctx context.Context, cmd string, args json.RawMessage) (any, error) }`; `var QueryCmds = []string{CmdListDots, CmdAsk, CmdAskCouncil, CmdListQueue}`; `(Client) Query(ctx context.Context, cmd string, args any, out any) error`; consts `CmdListDots = "list_dots"`, `CmdAsk = "ask"`, `CmdAskCouncil = "ask_council"`, `CmdListQueue = "list_queue"`; `AskArgs{PaneID, Slug, Question string; IncludeTurns bool}`; `PaneArgs{PaneID string}`; `ListQueueArgs{PaneID, Status string}`; `AskCouncilArgs{PaneID, Question string}`; `DotInfo{Slug, Name, PurposeSummary, Mode, Status string; Paused bool; Calls, Budget int; CostUSD float64; ProjectID string}`; `AskResult{Slug, Name, Severity, Message, Model string; Relevant bool; CostUSD float64}`; `CouncilResult{Digest string; Members []CouncilMember; CostUSD float64}`; `CouncilMember{Slug, Severity, Stance string; Failed bool}`; `CardInfo{ID, Dot, Severity, Body string; Turn int}`.
**Tests:** `Query` round trip with a fake `Querier`; a handler without `Querier` → error "query not supported"; `Data` omitted for v1 commands; a 45 s server delay succeeds with a 60 s context; an old v1 client decoding a response with `Data` still works.

### Task 3: Engine ask, list, consulted guard, limiter

**Files:** Create `internal/engine/ask.go`. Modify `internal/engine/filter.go`, `internal/engine/engine.go`, `internal/engine/council.go`, `internal/state/feed.go`, `internal/config/config.go`, `internal/memory/store.go`. Test: `internal/engine/ask_test.go`, extend `filter_test.go`, `memory/store_test.go`, `config_test.go`.
**Interfaces:**
- Consumes: v1 `runDot` pieces (request building, `sem`, usage, feed), Phase 2 memory selection and `Apply`, Phase 3 council orchestration, Task 2 types.
- Produces: `(*Engine) Ask(ctx context.Context, paneID, slug, question string, includeTurns bool) (ipc.AskResult, error)`; `(*Engine) AskCouncil(ctx context.Context, paneID, question string) (ipc.CouncilResult, error)`; `(*Engine) ListDots(paneID string) ([]ipc.DotInfo, error)`; `(*Engine) ListQueue(paneID, status string) ([]ipc.CardInfo, error)`; `(*Engine) Query(ctx context.Context, cmd string, raw json.RawMessage) (any, error)`; `engine.FilterInput.Consulted bool`; `engine.ReasonConsulted`; `var ErrAskLimit, ErrNotAttached, ErrPaused`; `state.OutcomeAsked`; `memory.SourceAgent`; `(*memory.Store) Propose(p string, e memory.Entry) (memory.Entry, error)`; `config.MCPConfig{Tools []string; MaxAsksPerTurn, MaxAsksPerMinute int}` on `Config.MCP`.
**Tests:** `Ask` returns the fake verdict, adds no card, records usage, appends `asked` to the feed, sets consulted; the next `processPane` skips that dot with `ReasonConsulted`, and the turn after doesn't; the 4th ask in one turn → `ErrAskLimit` with no backend call; the per-minute limit with a fake clock; paused → `ErrPaused`; unattached → `ErrNotAttached` listing attached slugs; budget exhausted → error; `AskCouncil` returns the digest and creates no card; `Propose` forces proposed + project scope and runs the secret scanner.

### Task 4: MCP server

**Files:** Create `internal/mcpserver/server.go`, `cmd/herdr-dots/mcp.go`. Modify `cmd/herdr-dots/main.go`, `internal/backend/codex.go`. Test: `internal/mcpserver/server_test.go`, `cmd/herdr-dots/mcp_test.go`, extend `internal/backend/codex_test.go`.
**Interfaces:**
- Consumes: go-sdk `mcp.NewServer`, `mcp.AddTool`, `mcp.StdioTransport`, the in-memory transport for tests; `ipc.Client.Query`; `memory.Store.List/Propose`; `ensureEngine`; `paths.New`.
- Produces: `type EngineAPI interface{ ListDots(ctx context.Context) ([]ipc.DotInfo, error); Ask(ctx context.Context, slug, question string, includeTurns bool) (ipc.AskResult, error); AskCouncil(ctx context.Context, question string) (ipc.CouncilResult, error); ListQueue(ctx context.Context, status string) ([]ipc.CardInfo, error) }`; `type Deps struct{ Engine EngineAPI; Memory *memory.Store; PaneID string; Notify func(); Tools []string; MaxResultChars int }`; `New(d Deps) *mcp.Server`; `NewIPCEngine(c ipc.Client, paneID string, ensure func() error) EngineAPI`; `runMCP(args []string) error`; `CodexCLI.BuildCmd` adds `-c mcp_servers.herdr-dots.enabled=false`.
**Tests:** via the in-memory client: the tool list matches `Tools`; each tool's happy path with a fake `EngineAPI`; the question > 4,000 characters → error; results truncated at `MaxResultChars` with a marker; empty `PaneID` → every tool `isError` with the Herdr message; `read_memory` excludes proposals; `propose_memory` calls `Notify`; `runMCP` writes nothing to stdout before the transport starts (capture test); the codex command includes the disable override.

### Task 5: Installer

**Files:** Create `internal/mcpinstall/install.go`, `cmd/herdr-dots/setupmcp.go`. Modify `cmd/herdr-dots/main.go`, `internal/ui/manage/model.go`. Test: `internal/mcpinstall/install_test.go`, `cmd/herdr-dots/setupmcp_test.go`.
**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`, `BurntSushi/toml` (read), a line-preserving TOML block editor (same approach as v1 `keys.Append`: append or replace a delimited block, never re-serialize the whole file), `os/exec` for `claude`/`codex`.
- Produces: `type Client string` (`ClientClaude`, `ClientCodex`); `type Plan struct{ Client Client; Commands [][]string; Edits []FileEdit; Summary string }`; `type FileEdit struct{ Path, Before, After string }`; `PlanInstall(c Client, bin, configDir, stateDir string) (Plan, error)`; `PlanRemove(c Client, record InstallRecord) (Plan, error)`; `Apply(p Plan, run func([]string) error, now time.Time) (backups []string, err error)`; `type InstallRecord struct{ Client Client; Command []string; At time.Time }`; `LoadRecords(path string) ([]InstallRecord, error)`; `runSetupMCP(args []string) error` (`--client`, `--remove`, `--yes`).
**Tests:** the Claude plan equals `claude mcp add --scope user herdr-dots -- <bin> mcp --config-dir … --state-dir …`; the Codex plan runs `codex mcp add` and then adds `env_vars`, `startup_timeout_sec`, `tool_timeout_sec` inside `[mcp_servers.herdr-dots]` while preserving other tables and comments (golden file); backup created; `--remove` plan; idempotent re-install (no duplicate blocks); a missing `claude` binary → a clear error and no partial edits; the manage footer reads `mcp-install.json`.

### Task 6: README, version, end-to-end

**Files:** Modify `README.md`, `internal/version/version.go` (`0.5.0`), append to `docs/superpowers/notes/phase-5-verification.md`.
**Interfaces:** Consumes all of the above.
**Tests:** `go test -race ./... && go vet ./...`; a manual E2E in `herdr --session dots-test`: `setup-mcp --client claude`, ask Claude to use `ask_dot`, confirm the answer, the `asked` feed line and `consulted` on the next turn; run a codex dot with the server installed and confirm no recursion (`mcp.log` shows the refusal); `setup-mcp --remove` restores the configs. README: tools, the explicit note that MCP answers bypass the approval queue, `[mcp] tools` to restrict; Phase 2 still **VITAL**; no "done/ready".

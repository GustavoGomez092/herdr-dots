# Herdr Dots Phase 5: Dots MCP Server, Design Spec

**Status: DRAFT, pending user review**

- **Date:** 2026-10-08
- **Phase:** 5 of 10 (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first.
- **Builds on:** v1 `ipc` (Task 15), `engine` (Tasks 14 and 16), `paths`, `ensureEngine` (Task 17); Phase 2 `memory`; Phase 3 `Council`; Phase 4 cost fields
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-5-mcp-server-outline.md`
- **The product is not done after this phase.** Ships as `0.5.0`.

---

## 1. Intent

### What the user wants

Today, information flows one way: dots watch the main agent and you decide what to pass along. Phase 5 opens the other direction. The main agent (Claude Code or Codex) can **consult dots itself** while it works: "Ask the Security dot whether this token handling is OK before I write it," "What did the team decide about state management?", "What's pending in the dots' queue?" This happens through a **Dots MCP server** installed into Claude Code and Codex, so the agent sees tools like `mcp__herdr-dots__ask_dot`.

### Success criteria

1. `herdr-dots setup-mcp` installs the server into Claude Code (`claude mcp add`) and/or Codex (`codex mcp add` plus config keys), with a preview, confirmation and backup, and removes it with `--remove`.
2. Inside a Herdr pane, the main agent can call `list_dots`, `ask_dot`, `ask_council` (Phase 3), `list_queue`, `read_memory` and `propose_memory`. Answers come from the **same engine** that runs the pane's dots, with the same budgets, memory and backends.
3. Outside Herdr, or inside a dot's own CLI run, the server starts but every tool returns a clear "not inside a Herdr pane" error. It never recurses into dots calling dots.
4. `ask_dot` returns within the dot's run timeout and reports which dot answered, the severity, the message and the cost. It doesn't create a queue card.
5. A dot consulted through `ask_dot` doesn't also review the turn it was consulted in (no double advice).
6. The agent can't burn the budget in a loop: per-turn and per-minute ask limits apply on top of normal budgets.
7. Memory written through MCP lands as a **proposal** that the user approves (Phase 2 rules).

---

## 2. Research summary (looked up 2026-10-08)

- **MCP spec 2026-07-28** (final since July 28, 2026) is the largest revision so far: it removes protocol-level sessions and the `initialize` handshake (replaced by per-request `_meta` and `server/discover`), removes server-initiated requests, and keeps two standard transports: **stdio** (newline-delimited JSON-RPC to a client-launched subprocess) and **Streamable HTTP**. Backward compatibility with initialize-era clients is defined by version detection.
- **Official Go SDK** `github.com/modelcontextprotocol/go-sdk` **v1.7.0** supports 2026-07-28 and negotiates down to 2025-11-25 and earlier. API: `mcp.NewServer(&mcp.Implementation{Name, Version}, nil)`, `mcp.AddTool(server, &mcp.Tool{Name, Description}, handler)` with typed `func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)`, and `server.Run(ctx, &mcp.StdioTransport{})`.
- **Claude Code:** `claude mcp add [--scope local|project|user] [--env K=V] <name> -- <command> [args…]`. `local` and `user` live in `~/.claude.json`, `project` in `.mcp.json`. Tools appear as `mcp__<server>__<tool>`. Output above 10,000 tokens warns; the default cap is 25,000 (`MAX_MCP_OUTPUT_TOKENS`). Startup timeout is `MCP_TIMEOUT`.
- **Codex CLI:** `codex mcp add <name> [--env K=V] -- <command>`, stored as `[mcp_servers.<name>]` in `~/.codex/config.toml` with keys `command`, `args`, `env`, **`env_vars`** (variables to permit and forward), `enabled`, `startup_timeout_sec` (default 10), **`tool_timeout_sec` (default 60)**, `enabled_tools`, `disabled_tools`. Codex may not forward the parent environment unless it's listed in `env_vars`, so `HERDR_PANE_ID` and `HERDR_SOCKET_PATH` must be listed.
- **v1 IPC** returns only `OK`/`Error`/`Snapshot`, with no per-command data, and `Client.Call` has a 30-second default deadline. `Engine.Summon` is asynchronous (the result becomes a card). So Phase 5 needs a **data-returning IPC call** and a **synchronous ask** in the engine.
- **v1 dot runs:** `claude -p` already passes `--strict-mcp-config` (no MCP servers load), and `CleanEnv` strips `HERDR_*`. `codex exec` loads `~/.codex/config.toml` MCP servers, so a codex dot would spawn our server, which must then refuse (no `HERDR_PANE_ID`).

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| P1 | Transport | **stdio**, launched by the agent CLI: `herdr-dots mcp` | Streamable HTTP served by the engine (needs a port, auth tokens and lifecycle management) |
| P2 | How the server reaches the engine | v1 local IPC over `dots.sock`. The server computes `paths.New(configDir, stateDir, HERDR_SOCKET_PATH).EngineSock()`, with `--config-dir`/`--state-dir` baked into the install args (the agent's environment has no `HERDR_PLUGIN_*`), and calls `ensureEngine` if the engine isn't running | Reading state files directly for everything (no live engine; can't run dots) |
| P3 | Which pane | `HERDR_PANE_ID` from the server's environment, which is the pane the agent runs in. All tools are scoped to that pane and its project | A pane argument on every tool (lets one agent query another pane) |
| P4 | IPC change | `ipc.Response` gains `Data json.RawMessage`; new optional handler interface `ipc.Querier`; new `Client.Query(ctx, cmd, args, out)` honoring the caller's deadline | Snapshots for results (too coarse); a second socket |
| P5 | Ask semantics | New synchronous `Engine.Ask` → one forced run (`Force=true`) with the dot's memory, budgets and backend; returns the verdict; **no card**; feed outcome `asked` | Reusing `Summon` (asynchronous, makes cards) |
| P6 | Which dots can be asked | **Attached** dots on the pane only (paused ones refuse with "paused") | Any library dot (needs a transient attachment; deferred to the open decisions) |
| P7 | Double-advice guard | The engine remembers dots asked via MCP since the pane's last processed turn (`consulted[key]`). On the next turn, `Decide` returns `ReasonConsulted` for them once | Parsing tool calls from the transcript (no inputs in v1's `ToolUse`) |
| P8 | Agent-loop guard | `mcp.max_asks_per_turn = 3` per pane (reset when a turn ends) and `mcp.max_asks_per_minute = 6`; over the limit → tool error "ask limit reached; continue without the dot" | Budgets only (a loop could drain a whole session budget in one turn) |
| P9 | Queue and memory exposure | `list_queue` and `read_memory` on by default, configurable via `[mcp] tools = [...]` | Off by default (the user explicitly asked for them) |
| P10 | Memory writes | `propose_memory` creates **project-scope `proposed`** entries attributed to source `agent` | Direct active writes |
| P11 | Recursion | The server refuses all tools when `HERDR_PANE_ID` is empty. Codex dot runs also get `-c mcp_servers.herdr-dots.enabled=false` (if V3 confirms the syntax) | Trusting env stripping alone |
| P12 | Install scope | Claude Code `--scope user`; Codex user config. One install serves every project | Project scope (writes into the user's repo) |
| P13 | Timeouts | Codex `tool_timeout_sec = 180`; Claude Code via `MCP_TOOL_TIMEOUT` if V4 confirms it exists. The engine's `Ask` is bounded by `run_timeout_seconds` (120) | Default 60 s (too short for a CLI dot run) |

---

## 4. Architecture

```
Claude Code / Codex (in Herdr pane w1:p1, env: HERDR_PANE_ID, HERDR_SOCKET_PATH)
   │ stdio JSON-RPC (MCP 2026-07-28 or older, negotiated by go-sdk)
   ▼
herdr-dots mcp --config-dir C --state-dir S
   │  internal/mcpserver: tools → EngineAPI (ipc.Client.Query) + memory.Store (direct, read + proposals)
   ▼
dots.sock (v1 IPC) ── engine: Ask / AskCouncil / ListDots / ListQueue
```

### 4.1 Packages

- **`internal/mcpserver`** (new): builds the `mcp.Server` and tools, validates inputs, enforces size caps, formats results. It depends on an `EngineAPI` interface (fakeable) and `*memory.Store`.
- **`internal/mcpinstall`** (new): computes and applies install and remove actions for Claude Code and Codex (command lines and TOML edits), with backup and preview.
- **`cmd/herdr-dots/mcp.go`**: `runMCP(args)`. **`cmd/herdr-dots/setupmcp.go`**: `runSetupMCP(args)`.

### 4.2 Tools

| Tool | Input | Output | Notes |
|---|---|---|---|
| `list_dots` | none | `[{slug, name, purpose_summary, mode, status, paused, calls, budget, cost_usd}]` | `purpose_summary` = the first 200 characters of the purpose |
| `ask_dot` | `slug` (string), `question` (≤ 4,000 characters), `include_turns` (bool, default true) | `{slug, name, relevant, severity, message, cost_usd, model}` | Synchronous; ≤ `run_timeout_seconds` |
| `ask_council` | `question` (≤ 4,000) | `{digest, members:[{slug, severity, stance, failed}], cost_usd}` | Phase 3 required; synchronous variant of `Council` that returns the digest instead of a card |
| `list_queue` | `status` (`pending` default \| `all`) | `[{id, dot, severity, turn, body}]` | Read-only; bodies capped at 2,000 characters each, 20 cards max |
| `read_memory` | `query` (optional), `scope` (`project` \| `dot` \| `all`), `dot` (optional), `limit` (≤ 50) | `[{id, scope, dot, kind, text, pinned}]` | Active entries only; proposals excluded |
| `propose_memory` | `kind` (`convention` \| `decision` \| `note`), `text` (≤ 500), `paths` (optional) | `{id, status: "proposed"}` | Secret scanner applies; shown in the memory screen for approval |

All tool descriptions state that dots are advisory, read-only, and answer from their own expertise.

### 4.3 Changes to earlier interfaces (exact)

| Symbol | Change |
|---|---|
| `ipc.Response` | Add `Data json.RawMessage` (omitted when empty; v1 clients ignore it) |
| `ipc` | Add `type Querier interface{ Query(ctx context.Context, cmd string, args json.RawMessage) (any, error) }`. `serveConn` calls `Query` for commands in `QueryCmds` when the handler implements it, and marshals the result into `Data` |
| `ipc.Client` | Add `Query(ctx context.Context, cmd string, args any, out any) error`. It uses the context deadline (no 30-second default when `ctx` has a deadline) |
| `ipc` consts | `CmdListDots`, `CmdAsk`, `CmdAskCouncil`, `CmdListQueue`; `QueryCmds = []string{…}`; arg types `AskArgs{PaneID, Slug, Question string; IncludeTurns bool}`, `PaneArgs{PaneID string}`, `ListQueueArgs{PaneID, Status string}`, `AskCouncilArgs{PaneID, Question string}`; result types `DotInfo`, `AskResult`, `CouncilResult`, `CardInfo` (fields as in §4.2) |
| `engine` | Add `(*Engine) Query(ctx, cmd, raw) (any, error)` (so `*Engine` satisfies `ipc.Querier`); `Ask(ctx context.Context, paneID, slug, question string, includeTurns bool) (ipc.AskResult, error)`; `AskCouncil(ctx context.Context, paneID, question string) (ipc.CouncilResult, error)` (wraps Phase 3 orchestration with a channel sink instead of a card); `ListDots(paneID string) ([]ipc.DotInfo, error)`; `ListQueue(paneID, status string) ([]ipc.CardInfo, error)` |
| `engine` internals | `runOpts` gains `ask chan<- askResult`; `consulted map[key]bool`; the `asks` limiter per pane |
| `engine.FilterInput` | Add `Consulted bool`; new `ReasonConsulted` |
| `state` feed | Add `OutcomeAsked = "asked"` |
| `memory` | Add `SourceAgent = "agent"`; `Store.Propose(p string, e Entry) (Entry, error)` (forces `StatusProposed`, `ScopeProject`) |
| `config.Config` | Add `MCP MCPConfig{Tools []string; MaxAsksPerTurn, MaxAsksPerMinute int}` (TOML `[mcp]`; defaults: all tools, 3, 6) |
| `backend.CodexCLI.BuildCmd` | Append `-c mcp_servers.herdr-dots.enabled=false` (pending V3) |
| `main.dispatch` | Route `mcp` → `runMCP`, `setup-mcp` → `runSetupMCP` |

---

## 5. Data model

### 5.1 Install results

Claude Code (user scope):

```
claude mcp add --scope user herdr-dots -- /abs/plugin/bin/herdr-dots mcp \
  --config-dir /abs/CONFIG_DIR --state-dir /abs/STATE_DIR
```

Codex (`~/.codex/config.toml`, written by `codex mcp add`, then keys added by our TOML edit with a backup):

```toml
[mcp_servers.herdr-dots]
command = "/abs/plugin/bin/herdr-dots"
args = ["mcp", "--config-dir", "/abs/CONFIG_DIR", "--state-dir", "/abs/STATE_DIR"]
env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]
startup_timeout_sec = 20
tool_timeout_sec = 180
```

`setup-mcp` records what it installed in `CONFIG_DIR/mcp-install.json` (client, scope, command, timestamp) so `--remove` and later upgrades know what to undo.

### 5.2 Config

```toml
[mcp]
tools = ["list_dots", "ask_dot", "ask_council", "list_queue", "read_memory", "propose_memory"]
max_asks_per_turn   = 3
max_asks_per_minute = 6
```

---

## 6. Data flow

### 6.1 Server start

1. Parse `--config-dir`/`--state-dir`; read `HERDR_PANE_ID` and `HERDR_SOCKET_PATH`.
2. If either is missing: register the tools anyway (so the client's tool list is stable), but every call returns `isError: true` with "herdr-dots: not running inside a Herdr pane; dots are unavailable here."
3. Otherwise `paths.New(...)`, `ensureEngine(p)` (lazily on the first tool call, so server start stays fast and under `startup_timeout_sec`), and resolve the pane's project through `CmdListDots` (the response includes `project_id`).
4. `server.Run(ctx, &mcp.StdioTransport{})`. Logs go to `STATE_DIR/<key>/mcp.log`, never stdout (stdout is the protocol).

### 6.2 `ask_dot`

1. Validate: the slug exists among attached dots; the question is within limits; the ask limiter allows it.
2. `ipc.Client.Query(ctx(deadline = run_timeout + 10 s), CmdAsk, AskArgs{…}, &res)`.
3. Engine `Ask`: checks the attachment, the paused state and budgets (`Decide`-equivalent checks without the turn-based conditions); builds the request exactly like `runDot` (memory block, turns tail when `include_turns`, `Question`, `Force: true`); runs under `sem`; records usage; feed `OutcomeAsked`; sets `consulted[key] = true`; applies `remember` ops (Phase 2 rules); returns `AskResult`.
4. The server returns `structuredContent` (`AskResult`) plus a text rendering: `[◆ Security] (warn) …`.

### 6.3 Turn after a consultation

`processPane` passes `Consulted: e.consulted[k]` into `Decide`; consulted dots skip that turn with `ReasonConsulted`; `consulted` is cleared for the pane after `processPane` runs. The ask limiter's per-turn counter resets on the same event.

### 6.4 `read_memory` / `propose_memory`

These go **directly** to `memory.Store` (Phase 2 decision M4), not through IPC: `List` with the filter, or `Propose`. The server sends `CmdMemoryChanged` afterwards so the pane's proposal badge updates.

---

## 7. UI

- `herdr-dots setup-mcp [--client claude|codex|both] [--remove] [--yes]` prints exactly what it will run and edit, asks for confirmation, backs up `~/.codex/config.toml`, runs the CLIs, and verifies with `claude mcp list` / `codex mcp list`.
- The manage popup shows "MCP: installed for Claude Code, Codex" in its footer (from `mcp-install.json`).
- Dots pane feed: `asked` outcomes show `◆ asked by agent · warn`. The roster shows `consulted` in place of `passed` for the turn that follows.

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Tool result |
|---|---|
| Not in Herdr / no pane | `isError`: "not running inside a Herdr pane" |
| Engine unreachable after `ensureEngine` | `isError`: "dots engine not reachable; open Herdr's Dots pane (prefix+a) to start it" |
| Unknown or unattached slug | `isError`: "no attached dot 'x'; attached: security, frontend" |
| Paused / budget / ask limit | `isError` with the reason |
| Dot run error or timeout | `isError`: "Security failed: <hint>" |
| Pane closed while asking | `isError`: "pane closed" |

### 8.2 Safety

- **Scope:** a server instance only ever serves its own `HERDR_PANE_ID`. The IPC socket is `0600` inside a `0700` directory (v1), so other users can't connect.
- **No recursion:** refuse without a pane; `claude -p` dot runs use `--strict-mcp-config`; codex dot runs disable the server by config override.
- **Approval queue bypass is explicit:** `ask_dot` and `list_queue` give the agent unreviewed dot output by design. The README says so, and `[mcp] tools` lets the user remove `list_queue` (or anything else).
- **Memory:** agent-proposed entries are always proposals and pass the secret scanner.
- **Budgets and limits:** normal budgets plus the per-turn and per-minute ask limits.
- **Output size:** every tool result is capped well under Claude Code's 10,000-token warning (about 24,000 characters).

### 8.3 Testing

| Layer | Tests |
|---|---|
| `mcpserver` unit | An in-memory MCP client (go-sdk in-memory transport) against the server with a fake `EngineAPI`: each tool's happy path, input validation, size caps, not-in-Herdr refusal, ask limiter |
| IPC | `Query` round trip with `Data`; v1 `Call` unaffected; deadline honored past 30 s |
| Engine | `Ask` returns a verdict, creates no card, records usage and the `asked` feed entry; `ReasonConsulted` on the next turn only; `AskCouncil` returns the digest; budgets and paused state refuse |
| Install | `mcpinstall` produces the exact commands; Codex TOML edit adds `env_vars` and timeouts, keeps other servers, writes a backup; `--remove` restores; idempotent re-run |
| Live (manual) | In `herdr --session dots-test`: install for Claude Code, ask Claude "use the herdr-dots tools to ask the Security dot about auth.ts", confirm the answer and the `asked` feed line |
| Always | `go test -race ./...`, `go vet ./...` |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | Does Claude Code pass its environment (`HERDR_PANE_ID`, `HERDR_SOCKET_PATH`) to stdio MCP servers by default? | P2/P3 |
| V2 | With `env_vars` set, does Codex forward those variables? Without it, does Codex pass the full environment? | Codex install |
| V3 | Does `codex exec -c mcp_servers.herdr-dots.enabled=false` disable a configured server for that run? | P11 |
| V4 | What controls Claude Code's per-tool-call timeout for stdio MCP servers (`MCP_TOOL_TIMEOUT`?), and what's the default? Does a 120 s call survive? | P13 |
| V5 | Which MCP protocol versions do the installed Claude Code and Codex negotiate with go-sdk v1.7.0? | Compatibility |
| V6 | Are `structuredContent` results shown to the model by both clients, or only the text content? | Result formatting |

---

## 10. Out of scope

A remote or HTTP MCP server; MCP resources and prompts; exposing dots to agents outside Herdr; agent-triggered write-capable dots (Phase 7 stays user-approved only); MCP for Windows (Phase 6 ports it).

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Which dots `ask_dot` can reach | (a) attached only; (b) any library dot | **(a)** | Keeps the user in control of who's "in the room"; no transient state |
| O2 | `list_queue` exposure | (a) on by default; (b) off by default | **(a)** with a README warning | The user asked for it; it's read-only and visible in the agent's tool calls |
| O3 | Install scope | (a) user scope; (b) per project | **(a)** | One install; no files written into repos |
| O4 | Ask limits | 3 per turn / 6 per minute (recommended), or configurable only | **3 / 6** | Prevents runaway loops while allowing a few consults per turn |
| O5 | Does `ask_dot` write memory? | (a) the dot's `remember` ops apply (Phase 2 rules); (b) asks never write memory | **(a)** | Consistency with all other runs |

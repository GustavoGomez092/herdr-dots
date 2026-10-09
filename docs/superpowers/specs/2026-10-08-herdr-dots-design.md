# Herdr Dots — Design Spec

- **Date:** 2026-10-08
- **Status:** Approved design, pending written-spec review
- **Scope of this spec:** Increment 1 (v1, "core loop"). v1 is a **scaffold**: when it is complete, the project is *not* done or ready. Later increments are listed in [Roadmap](#roadmap). **Dot memory is VITAL for Phase 2.**

---

## 1. Intent

### What the user wants

Herdr is a terminal multiplexer for AI coding agents. The user runs it inside Ghostty. Herdr Dots is a **Herdr plugin** that brings the idea of OpenAI's ChatGPT Dots (an always-on agent) into Herdr, with three differences:

- **Many dots, not one.** You attach several specialist dots (frontend, backend, security, copywriter, marketing, …) to a main agent conversation.
- **Provider-agnostic.** Each dot picks its own backend and model, independent of the main agent's provider.
- **Inside the terminal.** Dots join a Claude Code or Codex CLI session running in a Herdr pane, read its context, and contribute from their own expertise. The goal is to refine specs and plans, catch security issues, and improve copy.

A session starts with zero dots. You add one or several dots with a keyboard shortcut. Each dot is created through a UI with a name, a purpose, and the relevant settings.

### Success criteria for v1

1. You can create, edit, and delete dots (from scratch or from built-in templates) in a Herdr popup.
2. You can attach one or more dots to a running Claude Code or Codex pane with a keyboard shortcut.
3. After each main-agent turn, every attached dot checks the turn against its purpose and speaks only when it has something relevant to add.
4. Dot output appears in a single Dots side pane. Delivery to the main agent follows each dot's mode, and the approval queue is the default.
5. Dots run through the `claude` and `codex` CLIs you already have, with no API keys, behind a backend interface that allows adding more backends later.
6. Cost stays bounded by per-dot and per-session budgets.

### Assumptions (confirmed)

- "ChatGPT / Claude chat" means **Codex CLI / Claude Code running in Herdr panes**. The ChatGPT web and desktop apps are out of reach of a Herdr plugin.
- "Keyboard shortcut to add dots" means Herdr `[[keys.command]]` bindings to plugin actions.

---

## 2. Research summary

### OpenAI Dots (DevDay 2026)

- An always-on agent in ChatGPT with its own cloud computer and browser, connected to about 4,000 apps.
- Setup: name, avatar, `@handle`.
- Four per-action autonomy levels: act freely / act if pre-approved / ask first / hand off.
- Has its own memory and does read-only "proactive research" while you're away.
- One dot per user at launch. Teams of dots are the stated long-term direction.

Herdr Dots borrows the autonomy-level idea, which becomes [delivery modes](#delivery-modes), and the team-of-specialists direction.

### Herdr plugin platform (herdr 0.8.2)

- A plugin is a `herdr-plugin.toml` manifest plus any argv command. It is not sandboxed.
- **The entire Herdr CLI and socket API is the plugin API.** The socket speaks newline-delimited JSON at `HERDR_SOCKET_PATH`.
- Manifest entries: `[[build]]`, `[[startup]]` (runs once, not supervised), `[[events]]`, `[[actions]]`, `[[panes]]` (placement: `overlay` | `popup` | `split` | `tab` | `zoomed`), `[[link_handlers]]`.
- Runtime environment variables: `HERDR_PLUGIN_ROOT`, `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR`, `HERDR_PLUGIN_CONTEXT_JSON`, `HERDR_PANE_ID`, `HERDR_SOCKET_PATH`, `HERDR_BIN_PATH`, and others.

Key primitives:

| Primitive | Purpose |
|---|---|
| `pane.read` (`visible`, `recent`, `recent-unwrapped`, `detection`) | Read main-agent output (scrollback fallback) |
| `agent.get`, `agent.list` | Agent kind and status; session identity reported by Herdr's integrations |
| `events.subscribe` → `pane.agent_status_changed` | Detect when a turn ends (`working` → `idle`) |
| `agent.prompt` (with `wait`) | Deliver a dot message into the main agent |
| `pane.report_metadata`, `pane.report_agent` | Show dot status and pending counts in Herdr's sidebar |
| `notification.show` | Toasts |

Platform limitations this design accounts for:

1. **Terminal-only UI.** There is no native plugin UI in v1. Popups are singletons without pane IDs.
2. **No supervised daemon.** `[[startup]]` runs once. The plugin manages its own background engine.
3. **No runtime action registration.** There's a fixed set of actions, so one picker serves many dots.
4. **No storage API.** The plugin owns its files in the config and state directories.
5. **Keybindings live in the user's `config.toml`, not in the plugin manifest.** The installed herdr-plus plugin confirms this: its docs say "herdr-plus never touches it", and its bindings were pasted in by hand. Actions are usable from Herdr's action menu without any binding.
6. **Typing into an agent can collide with the user's input.** Delivery has guard rules.
7. **Scrollback is lossy.** Transcript files are preferred.

---

## 3. Decisions

| # | Decision | Choice |
|---|---|---|
| D1 | How dot input reaches the main conversation | **Per-dot delivery mode**, default **queue** (approval) |
| D2 | Model access | **Pluggable backends**. CLI backends (`claude-cli`, `codex-cli`) ship first; API and Ollama come later |
| D3 | Screen layout | **One Dots side pane** (split) plus a **manage popup** |
| D4 | When dots speak | **Every turn, only if relevant**, plus optional file-glob triggers, budget caps, and manual summon |
| D5 | Stack | **Go + Bubble Tea + Lip Gloss**, one binary, packaged as a Herdr plugin (same approach as herdr-plus) |
| D6 | v1 scope | **Core loop only.** Everything else is incremental |
| D7 | Keybindings | README snippet plus an optional `herdr-dots setup-keys` helper (checks conflicts, asks for confirmation, writes the block) |
| D8 | Context source | Agent transcript file when a session ID is available, otherwise scrollback |
| D9 | Plugin id | `herdr-dots`, so actions are named `herdr-dots.<action>` |

---

## 4. Architecture

### 4.1 One binary, several roles

| Subcommand | Launched by | Role |
|---|---|---|
| `herdr-dots engine` | `[[startup]]`; also started on demand by the other subcommands | Long-running background process that owns all live state: event subscription, triggers, backend runs, queue, delivery |
| `herdr-dots pane` | `[[panes]]` with `placement = "split"` | The Dots side pane (Bubble Tea), a thin client of the engine |
| `herdr-dots manage` | `[[panes]]` with `placement = "popup"` (80% × 80%) | Dot library, editor, and attach/detach (Bubble Tea) |
| `herdr-dots action <name>` | `[[actions]]` | Short commands: `manage`, `toggle-pane`, `summon`, `pause-all` |
| `herdr-dots summon-ui` | `[[panes]]` with `placement = "popup"` (small) | Pick an attached dot and type an optional question |
| `herdr-dots setup-keys` | The user, from a shell | Optional keybinding installer |

### 4.2 Communication

```
Herdr socket ──events/requests──▶ engine ◀──local socket (STATE_DIR/<key>/dots.sock)──▶ pane / manage UIs
                                    │
                                    ├─▶ transcript readers (claude JSONL | codex session | scrollback)
                                    ├─▶ backends (claude -p | codex exec)
                                    └─▶ delivery (agent.prompt → main agent)
```

- **The engine is the single owner of live state.** The UIs connect to `dots.sock` (newline-delimited JSON), receive state snapshots and updates, and send commands. Closing the Dots pane doesn't stop the dots.
- **One engine per Herdr session.** Plugin directories are shared across named Herdr sessions, so all runtime files live under `STATE_DIR/<key>/`, where `<key>` is a short hash of `HERDR_SOCKET_PATH`.
- **Idempotent start.** Any subcommand calls `ensureEngine()`, which takes `STATE_DIR/<key>/engine.lock`. If no live engine holds the lock, it re-executes `herdr-dots engine` detached (new session, stdio to the log file).
- **Self-managed lifetime.** The engine exits when the Herdr socket has been unreachable for more than 30 seconds.

### 4.3 Packages

```
cmd/herdr-dots/          main, subcommand dispatch
internal/herdr/          socket client, typed requests, event subscription
internal/dots/           Dot definition model, TOML load/save/validate, embedded templates
internal/session/        attachments: main pane ↔ dots, agent kind, session id, last-seen turn
internal/transcript/     Turn model + readers: claude, codex, scrollback
internal/backend/        Backend interface + claudecli, codexcli
internal/engine/         turn detection, trigger filter, scheduling, budget, verdict routing
internal/queue/          card store + state machine, persistence
internal/delivery/       guard rules, batching, loop guard, agent.prompt
internal/ipc/            engine ↔ UI protocol (server + client)
internal/ui/pane/        Bubble Tea model for the side pane
internal/ui/manage/      Bubble Tea model for library and editor
internal/keys/           setup-keys: config.toml parse, conflict check, append
```

Rule: `engine`, `delivery`, and the UIs depend on **interfaces** (`HerdrClient`, `Backend`, `TranscriptReader`, `Clock`), never on concrete implementations. This lets everything run against fakes in tests.

### 4.4 Manifest (shape)

```toml
id = "herdr-dots"
name = "Herdr Dots"
version = "0.1.0"
min_herdr_version = "0.8.0"
description = "Always-on specialist agents (dots) that join your Claude Code / Codex sessions."
platforms = ["macos", "linux"]

[[build]]
command = ["sh", "scripts/build.sh"]       # go build -o bin/herdr-dots ./cmd/herdr-dots

[[startup]]
command = ["./bin/herdr-dots", "engine", "--ensure"]

[[actions]]
id = "manage"
title = "Dots: manage"
command = ["./bin/herdr-dots", "action", "manage"]

[[actions]]
id = "toggle-pane"
title = "Dots: toggle pane"
command = ["./bin/herdr-dots", "action", "toggle-pane"]

[[actions]]
id = "summon"
title = "Dots: summon"
command = ["./bin/herdr-dots", "action", "summon"]

[[actions]]
id = "pause-all"
title = "Dots: pause all"
command = ["./bin/herdr-dots", "action", "pause-all"]

[[panes]]
id = "pane"
title = "Dots"
placement = "split"
command = ["./bin/herdr-dots", "pane"]

[[panes]]
id = "manage"
title = "Dots: manage"
placement = "popup"
width = "80%"
height = "80%"
command = ["./bin/herdr-dots", "manage"]

[[panes]]
id = "summon"
title = "Dots: summon"
placement = "popup"
width = "60%"
height = 12
command = ["./bin/herdr-dots", "summon-ui"]
```

Windows is out of scope for v1. The exact split-pane options are verified in [V3](#9-verification-tasks-first-in-the-plan).

---

## 5. Data model and storage

### 5.1 Dot definitions: `CONFIG_DIR/dots/<slug>.toml` (user-editable)

```toml
name    = "Security"
glyph   = "◆"
color   = "red"
purpose = """
You are a senior application security reviewer. ...
"""

[backend]
kind  = "claude-cli"        # v1: claude-cli | codex-cli
model = "sonnet"
args  = []

[delivery]
mode = "queue"              # observe | suggest | queue | inject   (default: queue)

[trigger]
on_turn    = true
file_globs = []             # empty = any turn
cooldown   = 0              # turns to skip after speaking

[context]
turns     = 3
repo_read = true            # read-only file tools for the backend
verbosity = "normal"        # terse | normal | detailed

[budget]
max_calls_per_session = 40
```

- **Validation:** `name` and `purpose` must be non-empty; `slug` is derived from `name` and must be unique; enums are checked; `turns` must be between 1 and 20; globs must compile. An invalid file is reported with its line number and skipped. Other dots still load.
- **Templates** are embedded with `go:embed`: `frontend`, `backend`, `security`, `copywriter`, `marketing`, `spec-critic`, `test-writer`. "From template" copies one into `dots/`.

### 5.2 Global settings: `CONFIG_DIR/config.toml`

```toml
default_backend = "claude-cli"
default_model   = "sonnet"
session_budget_calls = 120     # across all dots, per attached main pane
max_parallel_runs    = 3
run_timeout_seconds  = 120
turn_debounce_ms     = 1500
inject_enabled       = true    # global kill switch for inject mode
```

### 5.3 Runtime state: `STATE_DIR/<key>/` (engine-owned)

| File | Contents |
|---|---|
| `engine.lock`, `dots.sock`, `engine.log` | Process control, IPC, rotated log |
| `attachments.json` | Per main pane: `pane_id`, `agent_kind`, `agent_session_id`, `cwd`, `transcript_source`, and per attached dot: `slug`, `last_seen_turn`, `status`, `paused` |
| `queue.json` | Cards: `id`, `dot`, `pane_id`, `turn`, `severity`, `body`, `status` (`pending` \| `auto` \| `sent` \| `dismissed` \| `orphaned`), `error`, `created_at` |
| `feed.jsonl` | Append-only record of every run outcome (`flagged` \| `pass` \| `error` \| `budget`) |
| `usage.json` | Calls and tokens per dot and per pane |

All files are created with mode `0600` and directories with `0700`. Writes are atomic (temp file + rename). On startup the engine drops attachments whose pane no longer exists and marks their pending cards as `orphaned`.

### 5.4 Core types

```go
type Turn struct {
    Index     int
    User      string
    Assistant string
    Tools     []ToolUse   // name + touched file paths
    FromDot   string      // non-empty if the user message was a dot delivery
}

type Verdict struct {
    Relevant bool   `json:"relevant"`
    Severity string `json:"severity"` // info | warn | critical
    Message  string `json:"message"`
}

type Backend interface {
    Name() string
    Check(ctx context.Context) error                 // CLI present + authenticated
    Run(ctx context.Context, req Request) (Result, error)
}

type TranscriptReader interface {
    Turns(ctx context.Context, sinceIndex int) ([]Turn, error)
}
```

---

## 6. Engine data flow

### 6.1 Attach

1. Manage popup → IPC `attach{pane_id, slugs[]}`.
2. The engine calls `agent.get(pane_id)` to get the agent kind, session id, and cwd, then picks a transcript reader in this order: Claude JSONL, Codex session file, scrollback.
3. **Catch-up pass:** each newly attached dot reviews the last `context.turns` turns. This goes through the normal pipeline, so a dot only produces a card if it's relevant.

### 6.2 Turn detection

- Subscribe to `pane.agent_status_changed` for every attached pane.
- On `working` → `idle` (or `done`): wait `turn_debounce_ms`. If the status is still idle, read the transcript. If there are new turns past `last_seen_turn`, trigger.
- `blocked` never counts as the end of a turn.

### 6.3 Trigger filter (per dot, in order)

1. The dot isn't paused, `on_turn` is true, and its mode isn't `observe`. Observe dots never run on turns or in the catch-up pass; they only run when summoned (see 6.5).
2. The dot is out of cooldown.
3. `file_globs` is empty, or matches a path in the new turns' `Tools`.
4. The dot isn't the `FromDot` of the turn (loop guard).
5. Per-dot and per-session budget remain.

If a dot is already running, new turns are merged into **one** follow-up run.

### 6.4 Run

- **Prompt:** system = purpose + verbosity + Verdict JSON contract ("respond `relevant:false` if you have nothing important to add"); user = the last N turns, formatted.
- **`claude-cli`:** `claude -p --output-format json --model <m> --append-system-prompt <system>`. With `repo_read`, it adds `--allowedTools Read,Grep,Glob` and `--disallowedTools Bash,Edit,Write`. Without `repo_read`, it gets no tools.
- **`codex-cli`:** `codex exec --json --sandbox read-only -m <m>`.
- **Environment:** `cwd` is the main pane's cwd. The child environment is stripped of all `HERDR_*` variables, so the dot's own CLI never runs Herdr integration hooks against your pane.
- **Limits:** `max_parallel_runs` concurrent runs; `run_timeout_seconds` per run, after which the whole process group is killed.
- **Usage:** tokens are recorded when the CLI JSON output includes them.

### 6.5 Delivery modes

| Mode | Runs every turn? | Output |
|---|---|---|
| `observe` | No. It tracks turns but makes no model calls | Speaks only when summoned. (In Phase 2 it will build memory silently.) |
| `suggest` | Yes | A card in the Dots pane with no send action. `warn` and `critical` also raise a toast |
| `queue` (default) | Yes | A card with send, edit, and dismiss actions |
| `inject` | Yes | Delivered automatically under the guard rules in 6.6 |

`relevant:false` is logged to the feed as `pass` and never becomes a card.

### 6.6 Delivery

- **Format:** `[<glyph> <Name> dot] <message>`. Batched cards are combined under one header per dot.
- **Idle only:** the target must be idle. Otherwise delivery waits for the next idle.
- **Inject guard:** inject fires only if the target is idle **and** not the focused pane. If the target is focused, the card becomes `auto` and fires when focus leaves the pane, or when you press `⏎`. Inject messages are capped at 2,000 characters. `inject_enabled = false` turns every inject dot into a queue dot.
- **Loop guard:** turns started by a delivery are tagged `FromDot`. They don't re-trigger that dot, and they never cause an inject (cards from those turns are forced to queue).
- **Mechanism:** `agent.prompt(pane_id, text)`. If it fails, the card goes back to pending with an error marker.

### 6.7 Manual summon and budget

- **Summon:** the action opens a small popup (or you press `s` in the pane): pick an attached dot and type an optional question. The dot runs on recent turns plus the question. The result always becomes a card, whatever the dot's mode, and the dot answers even if it would otherwise pass.
- **Budget:** when a dot's budget runs out, its status becomes `budget` and it's skipped, with one toast. The session budget works the same way across all dots on a pane.

---

## 7. UI

### 7.1 Dots side pane

```
┌ DOTS → claude (w1:p1) ──── budget 12/120 ┐
│ ◆ Security   queue    ● thinking…        │
│ ▲ Frontend   suggest  ○ idle · passed    │
│ ✎ Copy       observe  ○ idle             │
├─ QUEUE (2) ──────────────────────────────┤
│▸◆ CRITICAL  Security · turn 14           │
│  JWT is stored in localStorage; any XSS  │
│  can steal it. Use an httpOnly cookie…   │
│  ⏎ send  e edit  x dismiss               │
│ ▲ info      Frontend · turn 14           │
├─ FEED (f) ───────────────────────────────┤
│ 14  ◆ flagged   ▲ flagged   ✎ —          │
└ j/k move  a send all  s summon  ? help ──┘
```

| Key | Action |
|---|---|
| `j`/`k`, `tab` | Move; switch focus between roster and queue |
| `⏎` / `a` | Send the selected card / send all (batched) |
| `e` | Edit the card inline before sending |
| `x` | Dismiss |
| `s` | Summon the selected dot |
| `m` | Cycle mode: observe → suggest → queue → inject |
| `p` / `d` | Pause/resume or detach the selected dot |
| `r` | Reset a dot that's in error |
| `t` | Switch target agent when the tab has several |
| `f`, `?` | Feed view, help |

The pane binds to the main agent pane it was opened next to (from `HERDR_PLUGIN_CONTEXT_JSON`). The engine also writes `pane.report_metadata` (source `herdr-dots`) on the main pane, so Herdr's sidebar shows the pending-card count (for example `◆2`).

### 7.2 Manage popup

- **Library screen:** a checkbox list of all dots, showing backend/model and mode, with the target agent in the header. Keys: `space` toggles attach, `⏎` applies (attach/detach in one batch), `n` new, `t` from template, `e` edit, `D` delete (with confirmation), `esc` close.
- **Editor screen:** a form with one field per dot setting; `purpose` is a multi-line text area. **Saving checks that the backend's CLI is on PATH** (`Backend.Check`) and refuses to save with a clear message if it isn't.
- If the focused pane isn't an agent, the popup opens in library-only mode with the note "focus an agent pane to attach dots."

### 7.3 Keybindings

Suggested defaults, chosen to avoid Herdr's built-in bindings (checked against `herdr --default-config`: `prefix+s` is settings and `prefix+shift+d` is close-workspace, so neither is used):

| Action | Key |
|---|---|
| `herdr-dots.manage` | `prefix+d` |
| `herdr-dots.toggle-pane` | `prefix+a` |
| `herdr-dots.summon` | `prefix+shift+s` |
| `herdr-dots.pause-all` | (unbound) |

`herdr-dots setup-keys`:
1. Finds the config (`HERDR_CONFIG_PATH`, then `$XDG_CONFIG_HOME/herdr/config.toml`, then `~/.config/herdr/config.toml`).
2. Parses the existing `[[keys.command]]` entries and the Herdr defaults, and reports conflicts.
3. Prints the exact block to append and asks for confirmation.
4. Backs up the file, appends the block, and runs `herdr server reload-config`.

The plugin never edits `config.toml` without this explicit command and confirmation.

---

## 8. Errors, safety, testing

### 8.1 Error handling

| Failure | Behavior |
|---|---|
| Herdr socket drops | Reconnect with backoff; exit cleanly after 30 seconds unreachable |
| Engine crash | UIs show "engine down, restarting" and call `ensureEngine()`; state is persisted |
| Backend missing or logged out | Dot status `error` with a fix you can act on; backoff; stops after 3 consecutive failures until reset (`r`) |
| Run timeout | Kill the process group; log `error` to the feed |
| Malformed Verdict JSON | Log `error` to the feed; no card |
| Transcript read fails | Fall back to scrollback; the roster shows "(scrollback)" |
| Main pane closed | Remove attachments; pending cards become `orphaned` but stay viewable |
| Delivery fails | Card goes back to `pending` with an error marker |
| Invalid dot TOML | Shown as invalid in the library, with its line number; other dots load |

### 8.2 Safety

- Dots are **read-only** (claude tool allowlist; codex `--sandbox read-only`). Nothing ever runs with `--dangerously-skip-permissions`. Herdr variables are stripped from child processes.
- **Prompt injection:** transcripts can contain hostile text. This is contained by read-only dots and the queue (you approve) as the default mode. Inject is opt-in per dot, has a global kill switch, caps message length, and is never triggered by dot-started turns.
- **Privacy:** dots send transcript excerpts to their configured provider (documented in the README). State files are `0600`.
- **Cost:** per-dot and per-session budgets with conservative defaults.

### 8.3 Testing

| Layer | Approach |
|---|---|
| Unit | Verdict parsing, the trigger filter, the queue state machine, delivery guards and loop guard, TOML round-trip and validation, `setup-keys` conflict detection and append |
| Transcript readers | Fixture files: real captured Claude JSONL and Codex session samples, with secrets removed |
| Engine integration | In-process **fake Herdr socket server** (`events.subscribe`, `agent.get`, `pane.read`, `agent.prompt`, `notification.show`, `pane.report_metadata`) plus a **fake backend** returning scripted Verdicts; covers status change → card → delivery |
| CLI backends | Live tests behind `-tags live` |
| UI | Bubble Tea model tests (`teatest`) for key handling and rendering |
| End to end | Manual smoke test in an isolated named session (`herdr --session dots-test`) via `herdr plugin link .`, never the user's main session |
| Always | `go test -race ./...`, `go vet ./...` |

---

## 9. Verification tasks (first in the plan)

These must be answered before the code that depends on them. If any answer breaks an assumption, stop and revise this spec with the user.

| # | Question | Affects |
|---|---|---|
| V1 | Do `agent.get` / `pane.get` expose the agent session id, and in which field? | Transcript reader selection (D8) |
| V2 | How do Claude Code and Codex status transitions behave during tool calls and permission prompts? | Turn detection, debounce default |
| V3 | Which options does `[[panes]] placement="split"` accept (direction, size)? Can it open beside a specific pane, or does it need a `pane.move`/`pane.resize` afterwards? | Dots pane placement |
| V4 | Current `claude -p` flags (`--output-format json`, `--allowedTools`, `--disallowedTools`, `--append-system-prompt`, avoiding saved session history) and `codex exec --json --sandbox read-only` output format | Backends |
| V5 | Where the Claude and Codex transcript files live, and their turn structure (user, assistant, and tool-use records) | Transcript readers |
| V6 | Does `agent.prompt` submit cleanly into the Claude Code and Codex TUIs? | Delivery |
| V7 | Is stripping `HERDR_*` enough to keep Herdr's integration hooks from firing for the dots' own CLI runs? | Backend isolation |
| V8 | Is `prefix+d` / `prefix+a` / `prefix+shift+s` free in the user's live config (beyond the defaults)? | setup-keys defaults |

---

## 10. Out of scope for v1

Dot memory, council mode, API and Ollama backends, the MCP server, Windows support, dot-to-dot conversation, write-capable dots, marketplace publishing, and dots outside Herdr.

---

## Roadmap

v1 is **scaffolded at best**. Do not call the project done or ready when v1 ships. Versions stay at `0.x`.

| Phase | Increment | Notes |
|---|---|---|
| **1** | Core loop (this spec) | Scaffold |
| **2** | **Dot memory: VITAL** | Per-project and per-dot memory (conventions, decisions, past findings), stored under the plugin state directory. Observe-mode dots build memory silently. This is the most important next increment: without memory, dots repeat themselves and can't learn a project. |
| 3 | Council mode | Ask all dots at once and get one merged digest |
| 4 | API and Ollama backends | Anthropic, OpenAI, OpenRouter, and Ollama behind the same `Backend` interface |
| 5 | Dots MCP server | The main agent can consult dots itself (`ask_dot`) |
| Later | Windows support; marketplace publishing (reconsider the "Dots" name, which overlaps with OpenAI's brand); write-capable dots in sandboxed worktrees | |

# Herdr Dots

> **Status: scaffold (v0.4.0).** Phases 1 (core loop), 2 (dot memory, VITAL), 3 (council) and 4 (API
> and Ollama backends, cost) are in. The project is not done and not ready for general use; see the
> [Roadmap](#roadmap) for what's missing.

Herdr Dots is a [Herdr](https://herdr.dev) plugin that adds **dots**: specialist agents (security,
frontend, backend, copywriter, ...) that join a Claude Code or Codex session running in a Herdr
pane. After every turn of the main agent, each attached dot checks the turn against its purpose and
speaks up only when it has something worth saying.

## Requirements

- Herdr ≥ 0.8.0 on macOS or Linux
- Go ≥ 1.25 (to build)
- `claude` (Claude Code) logged in, for `claude-cli` dots; `codex` logged in, for `codex-cli` dots

## Install

```bash
git clone <this repo> && cd herdr-dots
make build
herdr plugin link .        # development install
```

The engine starts automatically with Herdr (startup hook), or the first time you use a Dots action.

## Keybindings

Run `./bin/herdr-dots setup-keys` from a shell inside Herdr. It shows what it will add, skips keys
that are already taken, backs up your config, and reloads Herdr. Or paste this into
`~/.config/herdr/config.toml` yourself and run `herdr server reload-config`:

```toml
[[keys.command]]
key = "prefix+d"
type = "plugin_action"
command = "herdr-dots.manage"
description = "herdr-dots: manage dots"

[[keys.command]]
key = "prefix+a"
type = "plugin_action"
command = "herdr-dots.toggle-pane"
description = "herdr-dots: toggle Dots pane"

[[keys.command]]
key = "prefix+shift+s"
type = "plugin_action"
command = "herdr-dots.summon"
description = "herdr-dots: summon a dot"

[[keys.command]]
key = "prefix+shift+c"
type = "plugin_action"
command = "herdr-dots.council"
description = "herdr-dots: council (ask every dot)"
```

All actions also work from Herdr's action menu without any binding.

## Using it

1. Focus a Claude Code or Codex pane and press `prefix+d`.
2. Press `t` to create a dot from a template (or `n` for a blank one), edit it, `ctrl+s` to save.
3. Check the dots you want with `space`, press `enter` to attach them.
4. Press `prefix+a` to open the Dots pane next to the agent.

Dots pane keys: `⏎` send card · `a` send all · `e` edit · `x` dismiss · `s` summon · `m` cycle mode ·
`p` pause · `d` detach · `r` reset an errored dot · `t` next agent · `f` feed · `C` council · `space` expand a council card · `?` help.

### Delivery modes

| Mode | Runs every turn? | Output |
|---|---|---|
| `observe` | no | Answers only when summoned |
| `suggest` | yes | Card you read and copy; toast for warnings |
| `queue` (default) | yes | Card you send with `⏎` |
| `inject` | yes | Sent automatically when the agent is idle and you're not in that pane |

### Dot memory

Dots remember each project. Memory has three scopes:

| Scope | Holds | Written by |
|---|---|---|
| Project | Conventions and decisions every dot on the project reads | You; dots *propose* entries that wait for your approval |
| Dot | One dot's accepted findings, dismissed findings and notes for one project | Card outcomes, the dot itself, you |
| Global | One dot's preferences on every project ("answer in British English") | You only |

A project is a git repository: clones, moves and worktrees of one repo share memory (identified by
its first commit; forks share it too). A folder that isn't a git repo, or a shallow clone, is
identified by its path.

How dots learn:
- **Sending a card** remembers it as an accepted finding; **dismissing a card** remembers it as
  "don't raise this again". If a dot later says the same thing (0.7 similarity), the card is
  suppressed and the feed shows `suppressed (d:m17)`. Summoned answers are never suppressed.
- A dot's reply may carry up to 3 `remember` items (no extra model call). Items for the whole project
  wait as proposals until you approve them.
- **Observe-mode dots** run a memory-only pass every 5 new turns (at most 10 per agent session; `/clear`
  starts a new count), so they learn without ever making a card. Set `learn = false` under `[memory]`
  to stop that.
- In the Dots pane, `M` on a card remembers it on purpose: `f` accepted finding, `r` rule (with an
  optional reason), `c` project convention, `g` this dot everywhere.

Each run gets a memory block of at most 6,000 characters, picked by relevance (pinned entries
first). It is sent as reference data in the user prompt, never as instructions.

**Seeing and changing memory.** In the manage popup press `M` (the selected dot's memory) or `P`
(project memory): `a` approve, `e` edit, `p` pin, `x` forget, `X` forget everything in a scope,
`n` new, `/` filter, `tab` switch scope. Deleting a dot asks whether to delete its memory too.
From any shell:

```bash
herdr-dots memory list --dot security
herdr-dots memory add --scope project "Auth tokens live in httpOnly cookies"
herdr-dots memory forget d:m17 --dot security --scrub
herdr-dots memory edit --dot security        # opens $EDITOR
herdr-dots memory export > memory.toml
herdr-dots memory purge --project . --yes
```

Per-dot settings (`[memory]` in the dot's TOML file): `enabled`, `learn`, `read_project`,
`every_turns` (5), `max_passes_per_session` (10), `prompt_chars` (6000). Global settings in
`config.toml`: `memory_enabled`, `memory_retention_days` (180), `memory_max_entries` (200),
`memory_auto_approve_project` (false). `memory_enabled = false` takes effect within a few seconds;
turning memory back on and changing the other `memory_*` settings need an engine restart.

### Council mode

Ask every attached dot the same question at once and get **one** card back. Press `C` in the Dots
pane, run the `herdr-dots.council` action (`prefix+shift+c` after `setup-keys`), or pick
"⊕ Council (all attached)" at the top of the summon popup.

The popup has an optional question (empty means "review the recent turns") and a checklist of
members: every attached dot that isn't paused or out of budget, observe-mode dots included. It shows
the cost before anything runs (members × rounds, plus one call for the chair). If the pane's session
budget can't cover it, Enter stays disabled and the popup says why.

Members answer in parallel (within `max_parallel_runs`) and may pass. A **chair** run merges the
answers into a digest with `## Must fix`, `## Should fix`, `## Consider`, `## Disagreements` and
`## Not heard from`, each point attributed to the dots that raised it. If the chair fails or returns
something malformed, a deterministic plain merge is used and the card says so. Press `space` on the
council card to see each member's own answer; a member that failed shows `✗ timed out`.

The council card is always a queue card: nothing is sent until you press `⏎`. It arrives in the
agent as `[⊕ Council dot] …`, and no dot reviews the turn it starts. Sending it records an accepted
finding for each dot that contributed; dismissing it records a dismissal for each (see Dot memory).

Settings in `config.toml`:

```toml
[council]
chair_backend = ""      # empty = default_backend
chair_model   = ""      # empty = default_model
rounds        = 1       # 2 = dot-to-dot cross-review: each dot sees the others' answers and replies AGREE/OBJECT/ADD
max_members   = 8       # 2-16
merge         = "chair" # chair | plain
```

`rounds = 2` roughly doubles the cost. One council runs per pane at a time. A council in progress is
lost if the engine restarts (calls already made still count against the budgets).

### Backends and keys

Each dot picks its backend in the editor (`prefix+d`, then `e` or `n`) or in its TOML file:

| Kind | Talks to | Key | Cost shown |
|---|---|---|---|
| `claude-cli` | your `claude` login | none | from Claude Code (`total_cost_usd`) |
| `codex-cli` | your `codex` login | none | `?` (tokens only) |
| `anthropic-api` | Anthropic Messages API | `ANTHROPIC_API_KEY` or `keys set anthropic` | price table |
| `openai-api` | OpenAI Responses API | `OPENAI_API_KEY` or `keys set openai` | price table |
| `openrouter` | OpenRouter | `OPENROUTER_API_KEY` or `keys set openrouter` | reported by OpenRouter |
| `ollama` | a local Ollama server | none | $0 |

```toml
[backend]
kind       = "anthropic-api"
model      = "claude-opus-5-5"   # the default; set effort = "low" or pick a cheaper model to spend less
effort     = "low"               # low | medium | high | xhigh | max (anthropic-api, openai-api)
max_tokens = 4096
fallbacks  = "default"           # default | off: on a safety refusal Anthropic retries on another model

[budget]
max_calls_per_session = 40
max_usd_per_session   = 0.50     # 0 = off
```

`openai-api`, `openrouter` and `ollama` need a model (there is no OpenAI default; ids and prices change
often). Ollama dots take `base_url` and `context_window` (default 16384, always sent as `num_ctx`;
long prompts are shrunk to fit and the engine log says so).

**Keys.** `herdr-dots keys set <anthropic|openai|openrouter>` reads the key from stdin without echo
and stores it in `$(herdr plugin config-dir herdr-dots)/credentials.toml` (mode 0600, refused if group-
or world-readable). Environment variables win over the file; `[providers.<name>] api_key_env` in
`config.toml` renames the variable. `keys list` shows where each key comes from and its last 4
characters; `keys test` checks them; `keys remove` deletes one. The manage popup's `K` screen does the
same, with a masked entry field. Keys never go in dot files or `config.toml`, never appear on the
command line, and are scrubbed from errors, logs and the feed. Keys sit in a file on disk: keep it out
of dotfile repos. OS keychain storage is planned.

**Repo access.** With "Read repo" on, API dots can call `read_file`, `grep` and `list_files`, jailed
to the pane's working directory (symlinks can't escape it), honoring `.gitignore`, at most 8 calls per
run. These tools are read-only and are switched off when the pane's directory is your home directory
or `/` (the dot is told it has no repo tools). File names must match the names on disk exactly: a
case variant of a file name is "no such file". Secret files are always denied (`.env*`, `*.pem`,
`*.key`, `id_rsa*`, `id_ed25519*`, `*.p12`, `.git/**`, `**/secrets/**`, `.netrc`, `*.kdbx`);
`tool_deny_globs` in `config.toml` adds more.

**Cost.** Every run records tokens (including cached input) and a dollar cost: Anthropic and OpenAI
from an embedded price table dated 2026-10-08 (override entries in `prices.toml` next to
`config.toml`), OpenRouter from its own `usage.cost`, Ollama a known $0. OpenRouter keys that bring
your own provider key (BYOK) may report a cost of 0, so treat those figures as a lower bound. Models
without a price show `?` and never count toward dollar budgets (the editor warns). The Dots pane
header shows `$spent/budget`, the roster shows each dot's session cost, and the feed shows each run's
cost. When Anthropic's refusal fallback answers, the run is priced at the fallback model's rates and
the feed shows `model=…`; a declined first attempt isn't priced, so such a run's cost can be
slightly low.

**Councils.** Members and the chair can be API dots; their runs are priced like any other (the chair
counts against the pane). With `session_budget_usd` set, the council popup shows `$spent of $budget`,
members past their `max_usd_per_session` sit out, and a pane past its dollar budget can't start a
council. An API `chair_backend` without `chair_model` uses that kind's default (`claude-opus-5-5` for
`anthropic-api`); `openai-api`, `openrouter` and `ollama` chairs need `chair_model`.

## Files

- Dots: `$(herdr plugin config-dir herdr-dots)/dots/<slug>.toml` (hand-editable)
- Settings: `$(herdr plugin config-dir herdr-dots)/config.toml`
- Runtime state and `engine.log`: the plugin state dir, under a per-session folder
- Memory: the plugin state dir, under `memory/<project-id>/` (shared by every Herdr session; `0600` files)

## Privacy and safety

- Dots send recent conversation turns, their memory block and (with repo access) the files they read to
  **their own** provider: Anthropic for `claude-cli` and `anthropic-api`, OpenAI for `codex-cli` and
  `openai-api`, OpenRouter and the model vendor it routes to for `openrouter`. `ollama` stays on your
  machine unless its `base_url` points elsewhere.
- OpenAI requests use `store: false`; nothing is kept on OpenAI's side between tool turns.
- Dots are read-only: claude dots get only `Read,Grep,Glob`; codex dots run with `--sandbox read-only`.
- Hooks are disabled for dot runs, and Herdr variables are stripped, so dots never affect your panes.
- Queue mode (you approve) is the default. Inject mode is opt-in per dot and can be turned off for
  all dots with `inject_enabled = false` in `config.toml`.
- Changes to `config.toml` apply within a few seconds, without a restart (except `max_parallel_runs`, the `memory_*` limits and turning `memory_enabled` back on);
  if the file can't be parsed, the last good settings stay and inject is turned off until it is fixed.
- Per-dot and per-session call budgets and dollar budgets (`max_usd_per_session`, `session_budget_usd`)
  cap cost. Runs already in flight can overshoot a dollar budget slightly.
- Council members in round 2 see the other dots' answers, framed as opinions, never as instructions.
  The council chair sees the members' answers, the recent turns and the project's approved
  conventions and decisions; it has no repo access and writes no memory.
- Council cards are never sent without your approval, whatever the members' delivery modes are.
- Memory stays on this machine. It leaves it only inside the prompts a dot sends to **its own**
  provider, and a dot on a remote provider receives project memory written by other dots and by you.
- Dots can't plant shared conventions: model-written project memory waits for your approval, and
  dots can't write "don't raise this" rules or global memory.
- Text that looks like a credential is refused when a dot proposes it, and needs `--force` (CLI) or a
  second save (popup) when you add it. A card that quotes a secret is never remembered.
- Forgetting is real: `forget` removes the entry from disk, `--scrub` also removes its ledger lines,
  and `purge` deletes a project's whole memory.
- Memory text is stripped of control characters (terminal escape sequences) when it is written and
  when it is read, like the feed.

## Known gaps in this scaffold

- Codex transcript reading and the codex backend are built from Codex's documented formats and
  have **not been verified live**.
- Verified live: Herdr reports `done` (not `idle`) after an agent turn; the engine handles both.
- The first `agent start` in a new directory may stop at Claude's folder-trust prompt.
- The Dots pane opens at Herdr's default split width.
- Memory retrieval is lexical (paths, keywords, recency), not semantic.
- The Codex backend's `remember` support follows Codex's documented output format and has not been
  verified live.
- Draft specs for later phases live in `docs/superpowers/specs/` (roadmap: `2026-10-08-herdr-dots-roadmap.md`), all pending review.
- No MCP server, no Windows support.
- API backends are tested against recorded-style fakes; live runs need your keys (see
  `docs/superpowers/notes/2026-10-08-phase-4-verification.md`). Only Ollama has been run live; the
  Anthropic, OpenAI and OpenRouter paths are not live-verified. OpenAI model ids and prices change
  often: check `prices.toml` against the pricing page.
- No streaming for API dots; no OS keychain; no Bedrock/Vertex/Foundry providers (Gemini and others
  are reachable through OpenRouter).
- A council in progress doesn't survive an engine restart.
- Long council deliveries were verified live with Claude Code only; Codex is untested.

## Roadmap

| Phase | Increment |
|---|---|
| 1 | Core loop (in this scaffold) |
| **2** | **Dot memory — VITAL.** Per-project, per-dot memory of conventions, decisions and past findings (in this scaffold since 0.2.0; still being hardened). Without it, dots repeat themselves and can't learn a project. |
| 3 | Council mode: ask all dots at once, get one merged digest, with optional dot-to-dot cross-review (in this scaffold since 0.3.0) |
| 4 | API and Ollama backends (Anthropic, OpenAI, OpenRouter, Ollama), key storage, USD cost and dollar budgets (in this scaffold since 0.4.0) |
| 5 | Dots MCP server so the main agent can consult dots itself |
| 6–10 | Windows, write-capable dots, proactive dots, profiles and sharing, then hardening and release. The project is feature complete only after Phase 10. |

See the draft roadmap for Phases 2–10.

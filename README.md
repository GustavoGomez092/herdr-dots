# Herdr Dots

> **Status: scaffold (v0.2.0).** Phases 1 (core loop) and 2 (dot memory, VITAL) are in. The project is
> not done and not ready for general use; see the [Roadmap](#roadmap) for what's missing.

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
```

All actions also work from Herdr's action menu without any binding.

## Using it

1. Focus a Claude Code or Codex pane and press `prefix+d`.
2. Press `t` to create a dot from a template (or `n` for a blank one), edit it, `ctrl+s` to save.
3. Check the dots you want with `space`, press `enter` to attach them.
4. Press `prefix+a` to open the Dots pane next to the agent.

Dots pane keys: `⏎` send card · `a` send all · `e` edit · `x` dismiss · `s` summon · `m` cycle mode ·
`p` pause · `d` detach · `r` reset an errored dot · `t` next agent · `f` feed · `?` help.

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

## Files

- Dots: `$(herdr plugin config-dir herdr-dots)/dots/<slug>.toml` (hand-editable)
- Settings: `$(herdr plugin config-dir herdr-dots)/config.toml`
- Runtime state and `engine.log`: the plugin state dir, under a per-session folder
- Memory: the plugin state dir, under `memory/<project-id>/` (shared by every Herdr session; `0600` files)

## Privacy and safety

- Dots send recent conversation turns to **their own** configured provider (Anthropic for
  `claude-cli`, OpenAI for `codex-cli`).
- Dots are read-only: claude dots get only `Read,Grep,Glob`; codex dots run with `--sandbox read-only`.
- Hooks are disabled for dot runs, and Herdr variables are stripped, so dots never affect your panes.
- Queue mode (you approve) is the default. Inject mode is opt-in per dot and can be turned off for
  all dots with `inject_enabled = false` in `config.toml`.
- Changes to `config.toml` apply within a few seconds, without a restart (except `max_parallel_runs`, the `memory_*` limits and turning `memory_enabled` back on);
  if the file can't be parsed, the last good settings stay and inject is turned off until it is fixed.
- Per-dot and per-session call budgets cap cost.
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
- No council mode, no API/Ollama backends, no MCP server, no Windows support.

## Roadmap

| Phase | Increment |
|---|---|
| 1 | Core loop (in this scaffold) |
| **2** | **Dot memory — VITAL.** Per-project, per-dot memory of conventions, decisions and past findings (in this scaffold since 0.2.0; still being hardened). Without it, dots repeat themselves and can't learn a project. |
| 3 | Council mode: ask all dots at once, get one merged digest |
| 4 | API and Ollama backends behind the same backend interface |
| 5 | Dots MCP server so the main agent can consult dots itself |
| 6–10 | Windows, write-capable dots, proactive dots, profiles and sharing, then hardening and release. The project is feature complete only after Phase 10. |

See the draft roadmap for Phases 2–10.

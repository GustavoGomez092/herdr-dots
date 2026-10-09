# Herdr Dots

> **Status: scaffold (v0.1.0).** This is the first increment, the core loop. It is not finished
> and not ready for general use. See [Roadmap](#roadmap) for what's missing.

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

## Files

- Dots: `$(herdr plugin config-dir herdr-dots)/dots/<slug>.toml` (hand-editable)
- Settings: `$(herdr plugin config-dir herdr-dots)/config.toml`
- Runtime state and `engine.log`: the plugin state dir, under a per-session folder

## Privacy and safety

- Dots send recent conversation turns to **their own** configured provider (Anthropic for
  `claude-cli`, OpenAI for `codex-cli`).
- Dots are read-only: claude dots get only `Read,Grep,Glob`; codex dots run with `--sandbox read-only`.
- Hooks are disabled for dot runs, and Herdr variables are stripped, so dots never affect your panes.
- Queue mode (you approve) is the default. Inject mode is opt-in per dot and can be turned off for
  all dots with `inject_enabled = false` in `config.toml`.
- Changes to `config.toml` apply within a few seconds, without a restart (except `max_parallel_runs`);
  if the file can't be parsed, the last good settings stay and inject is turned off until it is fixed.
- Per-dot and per-session call budgets cap cost.

## Known gaps in this scaffold

- Codex transcript reading and the codex backend are built from Codex's documented formats and
  have **not been verified live**.
- Verified live: Herdr reports `done` (not `idle`) after an agent turn; the engine handles both.
- The first `agent start` in a new directory may stop at Claude's folder-trust prompt.
- The Dots pane opens at Herdr's default split width.
- No dot memory yet: dots don't learn a project between turns (see Phase 2).
- Draft specs for later phases live in `docs/superpowers/specs/` (roadmap: `2026-10-08-herdr-dots-roadmap.md`), all pending review.
- No council mode, no API/Ollama backends, no MCP server, no Windows support.

## Roadmap

| Phase | Increment |
|---|---|
| 1 | Core loop (this scaffold) |
| **2** | **Dot memory — VITAL.** Per-project, per-dot memory of conventions, decisions and past findings. Without it, dots repeat themselves and can't learn a project. This is the most important next increment. |
| 3 | Council mode: ask all dots at once, get one merged digest |
| 4 | API and Ollama backends behind the same backend interface |
| 5 | Dots MCP server so the main agent can consult dots itself |
| Later | Windows; marketplace publishing (reconsider the name, which overlaps with OpenAI's "Dots"); write-capable dots in sandboxed worktrees |

See the draft roadmap for Phases 2–10.

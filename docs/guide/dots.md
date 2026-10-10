# Dots: create, edit, attach

A dot is a specialist agent you attach to a Claude Code or Codex pane in Herdr; this page covers creating and attaching dots, the keys, and where the files live.

## Using it

1. Focus a Claude Code or Codex pane and press `prefix+d`.
2. Press `t` to create a dot from a template (or `n` for a blank one), edit it, `ctrl+s` to save.
3. Check the dots you want with `space`, press `enter` to attach them.
4. Press `prefix+a` to open the Dots pane next to the agent.

Dots pane keys: `⏎` send card · `a` send all · `e` edit · `x` dismiss · `s` summon · `m` cycle mode ·
`p` pause · `d` detach · `r` reset an errored dot · `t` next agent · `f` feed · `C` council · `space` expand a council card · `w` draft a fix (write-capable dots only) · `v` view a patch · `A` apply a patch · `?` help.

## Keybindings

The first time you open Dots ('Dots: manage' in Herdr's action menu), the Setup screen offers these
keybindings along with the MCP server, when none of them is in Herdr's config yet. It shows the four
bindings and the config file; `k` leaves them out, `y` installs, `n` asks again in 7 days, `x` stops
asking. `S` in the manage popup opens the screen again.

Or run `./bin/herdr-dots setup-keys` from a shell inside Herdr. It shows what it will add, skips keys
that are already taken and actions you already bound to another key, backs up your config (a new
`config.toml.bak-<time>` file each run; none when there was no config yet), and reloads Herdr. Or paste this into
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

## Files

- Dots: `$(herdr plugin config-dir herdr-dots)/dots/<slug>.toml` (hand-editable)
- Settings: `$(herdr plugin config-dir herdr-dots)/config.toml`. A key no setting reads (a typo)
  or a `[providers.<name>]` table for a provider that doesn't exist is ignored, and `engine.log`
  names it once, when a dot next runs.
- Runtime state and `engine.log`: the plugin state dir, under a per-session folder
- Memory: the plugin state dir, under `memory/<project-id>/` (shared by every Herdr session; `0600` files)
- `daily.json` in the plugin state dir: today's background runs and cost, shared by every session (proactive dots).
- `inbox.json` in the plugin state dir: scheduled results waiting for a pane on their project.
- `memory/<project-id>/schedule.json` in the plugin state dir: the last slot each scheduled dot ran on that project.
- `STATE_DIR/history/<slug>.jsonl`: per-dot run and card history, shared by every session (`0600`, capped at 20,000 lines).
- Trusted project dot files: `$(herdr plugin config-dir herdr-dots)/trusted.json`
- Project dots: `.herdr-dots/dots/*.toml` in a repository (used only after you trust each file)

## Long queues and long purposes

The Dots pane receives a bounded snapshot: at most 100 cards and 50 feed
entries per pane, 256 KB in all, and at most 10 updates a second. A card is
shown whole or not at all. When a pane has more cards than fit, the pane shows
the most severe, oldest ones first and a dim line `+N not shown` after them;
the `QUEUE (n)` count in the header includes the hidden cards. The rest become
visible as earlier cards are resolved. In the manage popup, a dot's profile
shows `Purpose (cut):` when the purpose in the snapshot is a shortened copy
rather than the whole text.

## Deleting a dot

Deleting a dot in the manage popup then asks "Also delete its memory in all
projects? (y/N)". The default keeps the memory: `enter`, `n`, `esc` and any
other key keep it, and only `y` deletes it in every project, with no undo. A
kept memory comes back if you re-create a dot with the same name.

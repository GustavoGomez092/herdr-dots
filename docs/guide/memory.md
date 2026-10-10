# Dot memory

Dots remember each project: conventions, decisions and past findings, so they stop repeating themselves (the heart of the product).

**Memory is VITAL to Herdr Dots.** Everything else builds on it, and its rules (what dots may write, what waits for your approval, what is never remembered) are the strictest in the product.

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
(project memory). The memory screen's keys:

| Key | Does |
|---|---|
| `a` | Approve a proposal (its whole text is shown first) |
| `e` / `n` | Edit the selected entry / add a new one to the scope you are viewing |
| `p` | Pin or unpin |
| `x` / `X` | Forget one entry / forget everything in the scope you are viewing (type its name) |
| `E` | Export the entries of the scope you are viewing, archived ones too, to a new TOML file (default `herdr-dots-memory-<project>-<YYYYMMDD>.toml` in the pane's checkout, or your home directory when the popup doesn't know it; private to you; an existing file is never overwritten). A relative path starts there too; `~` is your home directory |
| `I` | Import a TOML export: a preview first, then `y` |
| `P` | Purge all memory of this project (its dots' memory too); type the project id to confirm. Global memory is kept |
| `/` / `tab` | Filter / switch scope |

**Import confirm rules.** `I` reads the file (at most 4 MiB) and shows a preview before anything
is stored: the counts by scope and status (for example `1 project proposal from pack "team"`)
and how many entries look like secrets (in their text, paths or pack name), by entry number
only, never their text. `y` or `Y` imports; when any entry looks like a secret, the preview says
so, shows every pack name as `<hidden>`, and only `Y` (capital) imports. A file with project or
dot memory is refused on a screen with no project. Proposals from a dot pack stay
proposals and wait for your approval. In a popup too small to show the whole preview (or the
export, import or purge screen), the screen says "Too small" and its confirm key does nothing.

Deleting a dot asks whether to delete its memory too. Every screen action also works from any
shell:

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

## Deleting a dot's memory

Deleting a dot asks whether to delete its memory in all projects, and `enter`
answers yes today (a decision still open for the developer). See
[Dots](dots.md#deleting-a-dot).

# Herdr Dots

> **Status: 0.8.0, in development (scaffold, not done).** It is not finished and not ready for
> general use. New in 0.8.0: proactive dots (opt-in, read-only) can research while you are away,
> run on a schedule while Herdr runs and review new commits, under a daily dollar budget; this is
> built, unit-tested and soak-tested once in an isolated Herdr session on macOS (driven by a test
> client, not by hand). Write-capable dots (0.7.0) have **not been run end to end inside Herdr**
> yet, and Windows support (0.6.0) has not been run on Windows yet. Phase 2 (dot memory) stays
> the VITAL part. See the [Roadmap](#roadmap) for what's missing.

Herdr Dots is a [Herdr](https://herdr.dev) plugin that adds **dots**: specialist agents (security,
frontend, backend, copywriter, ...) that join a Claude Code or Codex session running in a Herdr
pane. After every turn of the main agent, each attached dot checks the turn against its purpose and
speaks up only when it has something worth saying.

## Requirements

- Herdr ≥ 0.8.0 on macOS or Linux, or Herdr ≥ 0.9.1 (the Windows beta, preview channel) on
  Windows 10 1809+ or Windows 11
- Go ≥ 1.25 (to build)
- `claude` (Claude Code) logged in, for `claude-cli` dots; `codex` logged in, for `codex-cli` dots

## Install

```bash
git clone <this repo> && cd herdr-dots
make build
herdr plugin link .        # development install
```

On Windows (PowerShell, from a clone of this repo):

```powershell
herdr plugin link .        # runs scripts\build.ps1, which builds bin\herdr-dots.exe
```

If your machine's policy blocks PowerShell scripts, build by hand with
`go build -o bin\herdr-dots.exe .\cmd\herdr-dots` and link again.

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
`p` pause · `d` detach · `r` reset an errored dot · `t` next agent · `f` feed · `C` council · `space` expand a council card · `w` draft a fix (write-capable dots only) · `v` view a patch · `A` apply a patch · `?` help.

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
so raise `tool_timeout_sec` or lower `run_timeout_seconds` if Codex gives up on councils. Like any
council, an `ask_council` in progress ends (with an error for the agent, and no card) if the pane closes
or the engine restarts. If `config.toml` can't be parsed when the agent starts the server, only
`list_dots` is offered until you fix it and restart the agent (the same fail-closed rule that turns
inject and memory off). The server logs to `mcp.log` in the plugin's state folder; it never prints
anything but protocol messages.

### Write-capable dots (drafts)

A dot you trust can draft a fix instead of only describing it. It edits an isolated copy of your
repository; nothing changes in your files until you read the diff and apply it. Drafts are built and
unit-tested; a live Claude draft has **not been run** on the developer's machine yet, and on Windows
drafts are compile-checked and unit-tested only.

**Turn it on (both switches are off by default):**

1. In the plugin's `config.toml`: `write_enabled = true`. A malformed `config.toml` turns it off
   again until you fix the file. `max_concurrent_drafts` (default 2) limits drafts at once.
2. In the dot's file, `dots/<slug>.toml` (there is no switch for this in the manage popup, on
   purpose):

   ```toml
   [capabilities]
   write                 = true
   write_timeout_seconds = 600     # 60-3600
   max_patch_bytes       = 200000  # larger patches are rejected
   max_files             = 50      # patches touching more files are rejected
   ```

   `claude-cli` and the API backends can draft. `codex-cli` dots can't: `codex exec` always has a
   shell, and write-capable dots never get one. A `claude-cli` draft needs a Claude Code version
   that supports `--restricted` (2.1.295 is verified); an older one refuses the flag and the draft
   fails without running.

**Draft.** In the Dots pane, select a card and press `w`, or select a write-capable dot in the
roster and press `w` and type what to fix. The confirm shows the dot, backend, model, time limit,
the tools it gets (no shell) and the full instruction; `y` starts it, `e` edits the instruction,
`tab` picks another write-capable dot, `esc` cancels. An instruction over 4,000 characters is
refused, not shortened, so what you confirmed is exactly what runs. While it runs the roster shows
`✎ Security drafting… 1:12`. A draft counts against the dot's and the pane's budget like any call,
and closing the agent's pane cancels its draft.

**Review.** The result is a `PATCH` card with a diffstat and flags such as `[binary]`, `[mode]`
or `[symlink]`. A patch that changes agent or CI configuration (CI workflows, `.vscode` tasks and
settings, `.envrc`, `.gitattributes`, `.gitmodules`, pre-commit and husky hooks, `Makefile`,
`package.json`, `.npmrc`) is flagged `[agent/CI config]` first: those files run code the next time
CI, your editor, git or a package script starts.

- `v` opens the patch viewer: the changed files on the left, the diff on the right, `n`/`p` for
  the next and previous hunk. Long lines wrap onto rows marked `↪`, never cut, and a file counts
  as viewed only once every row of it has been on screen.
- `A` opens the viewer with *Apply N files to your working tree? Nothing is staged or committed.
  (y/N)*; `y` applies. The confirm lists the files you haven't viewed yet, so you can see what you
  are skipping. The apply request carries the hash of the patch you reviewed, and the engine
  refuses it if the patch changed since. Apply runs only while the agent pane is idle and only into
  the repository the patch was drafted for. If the files moved since the draft, it merges three
  ways; files it can't merge keep conflict markers, and the card, the feed and a toast list them.
  Your index, branches and commits never change: review and commit as usual.
- `⏎` sends the agent a short note instead: *Patch ready: 3 files +42 −7. Review with
  `git apply --stat "<path>"`, apply with `git apply "<path>"`.* The diff itself never goes into
  the agent's context.
- `x` discards the patch.

Memory: an applied patch becomes a finding ("Applied patch: …") that keeps the dot from repeating
the advice; a discarded one leaves only a ledger line.

### Proactive dots (0.8.0)

Dots can keep working when you stop typing. Everything here is **off by default** for every dot,
read-only (even for a write-capable dot: background runs never draft), and capped by a daily dollar
budget shared by every Herdr session. Turn it on per dot in the editor (`prefix+d`, then `e`) or in
the dot's file. `proactive_enabled` in `config.toml` switches all of it off at once; a malformed
`config.toml` switches it off too, until you fix the file.

- **Idle research.** With `[research] enabled = true`, a dot looks into the open questions it noted
  earlier, its standing `topics` and the files touched in the last turns once the agent pane has been
  idle for `idle_minutes` (default 10), at most once per idle period and `max_runs_per_day` times a day
  across every Herdr session (default 6). "Idle" means no new turn or agent session, the agent idle or
  done (not working), and the pane not refocused: Herdr has no keyboard-activity signal. Relevant
  results wait while you are away and become **one** card per pane per idle period, a `☾ Away` card
  labeled `N dots · M min`, when you come back (your next turn, the agent working again, or focusing
  the pane). A critical result makes that card at once while you are away, and later results from the
  same idle period join it while it is unsent. `space` expands it into each dot's answer. `⏎` on a
  digest you haven't expanded expands it first (a second `⏎` sends it), `a` skips digests you haven't
  expanded, and the engine refuses to send a digest that changed since you read it. Observe-mode dots
  research for their memory only and never make a card. Research can write `research` and `question`
  memory entries (dot scope; project scope stays a proposal until you approve it), and you can add a
  question yourself to steer the next pass:
  `herdr-dots memory add --scope dot --dot <slug> --kind question "…"`.
- **Web research.** `[research] web = true` gives research passes web tools: web search only (no
  page fetching) for `claude-cli` dots, search and fetch for `anthropic-api` dots, hosted search for
  `openai-api` dots. Other backends research without the web (the editor says so). `claude-cli`
  research runs `--restricted` (your user and project Claude settings can't add tools or allow
  rules), and secret files (`.env`, keys, `.ssh`, `.aws`, credentials, `.git`, `.claude`, …) are
  denied to its reads. **With web on, research sends search queries to that
  provider's search service, and the queries are derived from your code and conversation.** The dot
  is told never to put code, secrets or private names in a query, but that is an instruction to a
  model, not a filter. Results are treated as untrusted data. `anthropic-api` and `openai-api` runs
  stop at 5 paid web uses (searches and fetches) per run, and web searches are counted in the cost
  ($0.01 per Anthropic search, $0.025 per OpenAI call, an upper bound). `claude-cli` web use has no
  per-run cap in the plugin; its cost comes from Claude Code's `total_cost_usd` like any other run.
- **Schedules.** `[schedule] cron = "0 9 * * 1-5"` with a `task` runs the dot on its projects
  (`projects = ["attached"]`, the default, means the projects the dot was attached to in the last 14
  days, most recent first; or absolute paths; at most 10 projects per slot). Schedules run **only
  while Herdr is running**: there is no system scheduler and no background daemon. A cron that fires
  more often than every 15 minutes is refused, and a dot gets at most 96 scheduled runs a day. A slot
  missed while Herdr was closed runs once at the next start if it is within `catch_up_hours` (default
  12); older misses are skipped and logged, and a laptop that slept through several slots runs only
  the latest. A brand-new schedule starts with its next slot, and slots that pass while proactive work
  or memory is switched off never run. Two Herdr sessions don't run the same slot twice: the slot is
  claimed in the project's memory, so schedules need memory turned on. A claim dated in the future
  (left by a clock that ran ahead) expires and is overwritten, so a clock correction can cause one
  repeat run. Cron is local time: a slot in the hour skipped by a daylight-saving change doesn't run
  that day, and a slot in a repeated hour runs once. A project that isn't on this machine is skipped,
  with one toast a day. The result goes to a pane on the project (one where the dot is attached
  first); when no pane on the project is open, it waits in an inbox (with a toast) and shows up in the
  queue the next time a pane with dots is attached there. Scheduled results are always cards you send
  yourself (a `suggest` dot's stay suggestions), never typed into the agent.
- **Commit reviews.** `[trigger] on_commit = true` reviews new commits in the attached pane's repo.
  There are no hooks: the engine polls `HEAD` every `commit_poll_seconds` (default 15, 5-600). The
  review gets the commit message, the stat and the patch (the patch capped at 20,000 characters),
  quoted as data. There is one review per new `HEAD`: a burst of commits, a reset or a branch switch is
  one review of the commit `HEAD` points at. The first `HEAD` seen after the engine starts or the dot
  is attached is only recorded, going back to a `HEAD` already seen is not reviewed again, and commits
  made while proactive work is off are never reviewed later. A dot gets at most 24 commit reviews a
  day. Reviews go through the dot's turn filter (pause, cooldown, `file_globs` matched against the
  commit's files, call and dollar budgets) and arrive like a turn review, in the dot's delivery mode.
  Nothing is installed in your repository: git runs with hooks, fsmonitor, external diff and textconv
  drivers off, submodule diffs kept short and every transport refused, so a repository's own config
  can't make a read-only dot run a program. Commit reviews need git 2.31 or newer; with an older git
  the review fails and `engine.log` says why.

```toml
# in a dot file
[research]
enabled          = true
idle_minutes     = 10
max_runs_per_day = 6
web              = false
topics           = ["dependency advisories for packages in package.json"]

[schedule]
cron           = "0 9 * * 1-5"
task           = "Review commits since the last run for security issues."
projects       = ["attached"]
catch_up_hours = 12

[trigger]
on_commit = true

# in config.toml
proactive_enabled   = true   # kill switch (on by default); a malformed config.toml turns it off
daily_budget_usd    = 1.00   # every background run, every session, per local day; always a cap
commit_poll_seconds = 15
```

The roster shows what is next (`research in 6m`, `scheduled Mon 09:00`, `paused · daily budget`) and
what is running (`◌ researching…`, `◌ scheduled run…`, `◌ reviewing commit…`); background runs go
one at a time per engine. The pane header shows `today $0.37/1.00` once anything ran today. A
background run on a pane also counts toward the dot's and the pane's session budgets, and a run with
an unknown cost counts as one run and $0. When the budget is reached, background work stops until
local midnight and each Herdr session toasts once. A run already going can take the day slightly over
the cap. If `daily.json` is corrupt, it is set aside (`daily.json.corrupt-<time>`) and background
work stops for the rest of that local day; while it can't be read or written, nothing runs. The
budget never becomes unlimited.

## Files

- Dots: `$(herdr plugin config-dir herdr-dots)/dots/<slug>.toml` (hand-editable)
- Settings: `$(herdr plugin config-dir herdr-dots)/config.toml`
- Runtime state and `engine.log`: the plugin state dir, under a per-session folder
- Memory: the plugin state dir, under `memory/<project-id>/` (shared by every Herdr session; `0600` files)
- `daily.json` in the plugin state dir: today's background runs and cost, shared by every session (proactive dots).
- `inbox.json` in the plugin state dir: scheduled results waiting for a pane on their project.
- `memory/<project-id>/schedule.json` in the plugin state dir: the last slot each scheduled dot ran on that project.

## Windows (beta)

Herdr Dots is built to run on the Herdr Windows beta with the same features as on macOS and
Linux, but it has not been run on Windows yet.

- **Not run-verified yet.** Every Windows code path is compiled and unit-tested on macOS, and
  `.github/workflows/ci.yml` runs the full test suite on `windows-latest`, but the developer has
  not run Herdr Dots on a Windows machine, and the CI workflow has not been pushed yet. The
  checklist in `docs/superpowers/notes/2026-10-08-phase-6-verification.md` ("User gates") lists
  what to try first: whether Herdr resolves `./bin/herdr-dots` to the `.exe`, the first
  `build.ps1` run, named-pipe owner checks (including an elevated/UAC prompt), process-tree kills,
  file locking, owner-only ACLs, the repo-tool jail on NTFS, and `.cmd` shims.
- **Engine IPC** is a named pipe, `\\.\pipe\herdr-dots-<session key>`, that only your Windows user
  can open. The engine checks that the pipe really belongs to you before trusting it.
- **Privacy:** `credentials.toml`, memory, settings and session state get an owner-only ACL (you,
  SYSTEM and Administrators). A credentials file that other users can read is refused with an
  `icacls` command that fixes it.
- **Dot processes** run in a Job Object, so a timeout or an engine crash ends the CLI and
  everything it started (node, git).
- **`claude.cmd` / `codex.cmd`** (npm installs) work: their command lines are quoted for cmd.exe
  and the system prompt goes through a private temp file. A native `claude.exe` is preferred when
  both are on `PATH`. A path containing `%`, `"` or `!` can't be passed to a `.cmd` program; such a
  run fails with a message saying so.
- **Long system prompts** go through a private temp file (`--append-system-prompt-file`) on every
  OS, not argv. This was verified live with Claude Code on macOS only.
- **Repo tools for API dots** are confined to the project folder, including NTFS tricks (8.3 short
  names, alternate data streams, junctions). Cloud placeholder files (for example OneDrive
  files that are not downloaded) may be refused as irregular files.
- **Keybindings:** `setup-keys` edits `%APPDATA%\herdr\config.toml` (or `HERDR_CONFIG_PATH`).
- **Glyphs:** if the dot symbols show as boxes, set `HERDR_DOTS_ASCII=1` for ASCII stand-ins.
- Herdr's own Windows limits apply (no direct terminal attach, no live server handoff).

## Privacy and safety

- Dots send recent conversation turns, their memory block and (with repo access) the files they read to
  **their own** provider: Anthropic for `claude-cli` and `anthropic-api`, OpenAI for `codex-cli` and
  `openai-api`, OpenRouter and the model vendor it routes to for `openrouter`. `ollama` stays on your
  machine unless its `base_url` points elsewhere.
- OpenAI requests use `store: false`; nothing is kept on OpenAI's side between tool turns.
- Dots are read-only unless you turn on writing twice (`write_enabled` and a dot's
  `[capabilities] write`). Reviews, councils, MCP asks, summons and memory passes always stay
  read-only: claude dots get only `Read,Grep,Glob`, codex dots run with `--sandbox read-only`, API
  dots get read-only tools.
- A draft runs in a git worktree outside your repository, made from a snapshot of your working
  tree (uncommitted and untracked files included) through a private index. Your index, stash,
  branches, `HEAD` and commits never change. What is added to the repository: objects, a private
  ref (`refs/herdr-dots/snapshots/<id>`), and, while the draft runs, git's own registration files
  for the worktree under `.git/worktrees/`. The ref goes when the patch is applied or discarded,
  stays 7 days after the patch is sent to the agent, and stays for a pending card, or one whose
  pane closed, until you apply or discard it. A daily sweep removes refs no patch owns.
  Git hooks and fsmonitor programs never run for it.
- No dot ever gets a shell. A claude draft gets `Read, Grep, Glob, Edit, Write` and runs with
  `--restricted` (your and the repository's Claude settings files are ignored), and only edits
  inside its worktree are accepted; API drafts get `write_file`/`edit_file` jailed to the worktree.
  Symlinks that point outside the repository are replaced while the dot works.
- Patches are validated before you see them. They are rejected when they touch `.git` or agent
  configuration (`.claude/`, `.mcp.json`, `.codex/`, for every backend), reach outside the
  repository, hit the deny list (secret-looking files such as `.env`), create or change
  any symlink, or go over the size or file caps. (Deleting a symlink, or replacing it with a regular
  file, is allowed.) Every draft needs two approvals: one to start, one (with the diff on screen)
  to apply. Applying touches the working tree only; it never stages or commits.
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
- With the MCP server installed, the main agent can read dot answers, the queue and approved memory
  **without your approval** (see "Dots MCP server"); dots still never change files, and agent-written
  memory is always a proposal.
- Memory stays on this machine. It leaves it only inside the prompts a dot sends to **its own**
  provider, and a dot on a remote provider receives project memory written by other dots and by you.
- Dots can't plant shared conventions: model-written project memory waits for your approval, and
  dots can't write "don't raise this" rules or global memory.
- Text that looks like a credential is refused when a dot proposes it, and needs `--force` (CLI) or a
  second save (popup) when you add it. A card that quotes a secret is never remembered.
- Forgetting is real: `forget` removes the entry from disk, `--scrub` also removes its ledger lines,
  and `purge` deletes a project's whole memory.
  Deleting a dot's memory in all projects (the manage popup) also removes the ledger lines its
  cards left and the project conventions it proposed that you never approved. It also deletes the
  copies of that dot's memory file that were set aside as damaged (`<dot>.json.corrupt-*`).
  A damaged project file set aside as `project.json.corrupt-*` mixes every dot's entries and is
  never edited, so it may still hold a deleted dot's text: `herdr-dots memory purge` is what
  clears those.
- Memory text is stripped of control characters (terminal escape sequences) when it is written and
  when it is read, like the feed.
- Proactive dots are opt-in per dot and read-only, even when the dot is write-capable: research,
  scheduled runs and commit reviews never draft. With `[research] web = true`, research sends search
  queries derived from your code to the backend's web search provider. `claude-cli` research uses web
  search only, with no page fetching, and secret files are denied to its reads. Background spend is capped by
  `daily_budget_usd` (default $1.00) across every Herdr session.
- Commit reviews never install git hooks; they only read `HEAD` and `git log`/`git show` output, with
  the repository's hooks, fsmonitor, diff drivers and transports disabled.

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
- Windows support (0.6.0) has not been run on Windows by the developer; it is cross-compiled and
  unit-tested only. The three-OS CI workflow is written but not yet pushed anywhere.
- The MCP server is verified live with Claude Code; Codex support follows Codex's documented config
  format and has **not been verified live** (see `docs/superpowers/notes/2026-10-08-phase-5-verification.md`).
  That includes the `-c mcp_servers.herdr-dots.enabled=false` override codex dot runs get.
- No MCP resources or prompts, no HTTP transport, no access for agents outside Herdr.
- API backends are tested against recorded-style fakes; live runs need your keys (see
  `docs/superpowers/notes/2026-10-08-phase-4-verification.md`). Only Ollama has been run live; the
  Anthropic, OpenAI and OpenRouter paths are not live-verified. OpenAI model ids and prices change
  often: check `prices.toml` against the pricing page.
- No streaming for API dots; no OS keychain; no Bedrock/Vertex/Foundry providers (Gemini and others
  are reachable through OpenRouter).
- A council in progress doesn't survive an engine restart.
- Long council deliveries were verified live with Claude Code only; Codex is untested.
- Write-capable dots (0.7.0) are unit-tested, with real git, on macOS only; a real draft inside
  Herdr, Ubuntu's git and Git for Windows are still to be tried (checklist in
  `docs/superpowers/notes/2026-10-08-phase-7-verification.md`, "User gates"). A live Claude draft
  has not been run on this machine, and on Windows drafts are compile-checked and unit-tested only.
- `codex-cli` dots can't draft (Codex has no verified way to run without a shell).
- Drafts can't run tests or builds: write-capable dots get no shell.
- Proactive dots (0.8.0) are unit-tested on macOS only, with a fake clock and real git, and were
  soak-tested once in an isolated Herdr session on macOS with `claude-cli` haiku dots (results in
  `docs/superpowers/notes/2026-10-08-phase-8-verification.md`). The soak drove the engine with a
  test client: the Dots pane was not used by hand, the daily budget was checked by lowering it, and
  a non-critical "away, then one card when you come back" digest was not seen live (every research
  result in the soak was critical). Web research was not part of the soak; Codex and OpenAI web
  search are not live-verified. Schedules run only while Herdr runs.

## Roadmap

| Phase | Increment |
|---|---|
| 1 | Core loop (in this scaffold) |
| **2** | **Dot memory — VITAL.** Per-project, per-dot memory of conventions, decisions and past findings (in this scaffold since 0.2.0; still being hardened). Without it, dots repeat themselves and can't learn a project. |
| 3 | Council mode: ask all dots at once, get one merged digest, with optional dot-to-dot cross-review (in this scaffold since 0.3.0) |
| 4 | API and Ollama backends (Anthropic, OpenAI, OpenRouter, Ollama), key storage, USD cost and dollar budgets (in this scaffold since 0.4.0) |
| 5 | Dots MCP server so the main agent can consult dots itself: ask a dot, convene a council, read the queue and memory, propose memory (in this scaffold since 0.5.0) |
| 6 | Windows support: named pipes, Job Objects, ACL privacy, `.cmd` shims (in this scaffold since 0.6.0; built and unit-tested, **not run on Windows yet**) |
| 7 | Write-capable dots (0.7.0): drafts in isolated git worktrees, patch cards, a diff viewer, apply to the working tree only. Built and unit-tested; **not run end to end in Herdr yet** |
| 8 | Proactive dots (0.8.0): idle research with a while-you-were-away digest, cron schedules while Herdr runs (with catch-up), commit reviews, a daily dollar budget. Built and unit-tested; soak-tested once in an isolated Herdr session on macOS |
| 9–10 | Profiles and sharing, then hardening and release. The project is feature complete only after Phase 10. |

See the draft roadmap for Phases 2–10.

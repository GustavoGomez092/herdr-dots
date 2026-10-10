# Proactive dots

Proactive dots research while you are away, run on a schedule and review commits.

Dots can keep working when you stop typing. Everything here is **off by default** for every dot,
read-only (even for a write-capable dot: background runs never draft), and capped by a daily dollar
budget shared by every Herdr session. Turn it on per dot in the editor (`prefix+d`, then `e`) or in
the dot's file. `proactive_enabled` in `config.toml` switches all of it off at once; a malformed
`config.toml` switches it off too, until you fix the file.

- **Idle research.** With `[research] enabled = true`, a dot looks into the open questions it noted
  earlier, its standing `topics` and the files touched in the last turns once the agent pane has been
  idle for `idle_minutes` (default 10), at most once per idle period and `max_runs_per_day` times a day
  across every Herdr session (default 6). "Idle" means no new turn or agent session, the agent idle or Herdr reports `done`
  (not working), and the pane not refocused: Herdr has no keyboard-activity signal. Relevant
  results wait while you are away and become **one** card per pane per idle period, a `☾ Away` card
  labeled `N dots · M min`, when you come back (your next turn, the agent working again, or focusing
  the pane). A critical result makes that card at once while you are away, and later results from the
  same idle period join it while it is unsent. `space` expands it into each dot's answer. `⏎` on a
  digest you haven't expanded expands it first (a second `⏎` sends it), `a` skips digests you haven't
  expanded, and the engine refuses to send a digest that changed since you read it. Observe-mode dots
  research for their memory only and never make a card. Research can write `research` and `question`
  memory entries (dot scope; project scope stays a proposal until you approve it, unless
  `memory_auto_approve_project` is on), and you can add a question yourself to steer the next pass:
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
local midnight and each Herdr session toasts once. A run already going can take the day over the cap,
by how much depends on the backend: a `claude-cli` background run is given the budget left, at most
$0.50 a run (`--max-budget-usd`), and Claude Code stops it there; API backends are bounded only by
their per-run token and web-use limits; a run with an unknown cost counts $0. A `claude-cli` run
that fails still counts the cost Claude Code reports, but one killed by `run_timeout_seconds`
reports no cost and counts $0. If `daily.json` is corrupt, it is set aside
(`daily.json.corrupt-<time>`) and background work stops for the rest of that local day. While it
can't be read, no background run starts. If a finished run can't be recorded in it (another session
holding its lock, say), the write is retried three times, after 100 ms, 300 ms and 900 ms; only if
every attempt fails does background work in that Herdr session stop until local midnight, with one
log line and one toast. The budget never becomes unlimited.

## Commit reviews leave denied files out

A commit review sends the commit's patch to the dot's backend, but the content
of every file the deny set covers (`.env*`, `*.pem`, the built-in secret globs
and your `tool_deny_globs`) is left out. The review is told "N denied file(s)
left out of this review". The file list may still name them. If
`tool_deny_globs` holds an invalid pattern, the patch is left out altogether
until you fix it.

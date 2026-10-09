# Herdr Dots Phase 8: Proactive Dots (Background Research, Scheduled Dots, Commit Triggers), Design Spec

**Status: DRAFT, pending user review**

- **Proposed by drafter.** This phase isn't in the v1 roadmap. Rationale: OpenAI's Dots are defined by being *always on*. They do proactive, read-only research while you're away. v1 dots only react to finished turns. Without proactive behavior, Herdr Dots is a turn reviewer, not the always-on teammate the product promises. That makes this a requirement for feature complete.
- **Date:** 2026-10-08
- **Phase:** 8 of 10 (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first, and this phase depends on it heavily.
- **Builds on:** v1 engine poll loop and `runDot`; Phase 2 memory (research results and open questions live there); Phase 4 cost tracking and dollar budgets (background work must be capped in dollars) and API web tools; Phase 6 platform helpers; Phase 7's hook-free `worktree.Git` runner (reused for commit polling)
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-8-proactive-dots-outline.md`
- **The product is not done after this phase.** Ships as `0.8.0`.

---

## 1. Intent

### What the user wants

Dots should keep working when you stop typing. While the main agent sits idle, a dot can look into the questions it noted earlier ("is the session cookie rotated on privilege change?"), read the code around recent changes, and optionally search the web for advisories. When you come back, one **"while you were away"** digest is waiting. Dots can also run on a **schedule** ("every weekday at 9:00, review yesterday's commits for security issues") and on **commits** ("after every commit, the test-writer checks what the commit left untested").

### Success criteria

1. **Idle research:** a dot with `[research] enabled = true` runs at most one research pass per idle period (the target pane idle with no new turns for `idle_minutes`, default 10), up to `max_runs_per_day`. Results become memory entries (`research`, `question`) and, when relevant, a part of one digest card per pane per idle period.
2. **Scheduled dots:** `[schedule] cron` runs a dot's standing `task` on its projects while Herdr is running. Missed runs catch up once at engine start (within `catch_up_hours`). Two Herdr sessions never double-run a schedule.
3. **Commit triggers:** `[trigger] on_commit = true` runs the dot on each new commit in an attached pane's repo, using the commit's diff as context.
4. Results for a project with no attached pane wait in a **project inbox** and appear in the queue the next time a pane on that project attaches.
5. Background work is capped by per-dot daily run limits and a global **`daily_budget_usd`**, and is read-only.
6. Web research is opt-in per dot (`research.web = true`) and supported only where the backend has a web tool.

---

## 2. Research summary

- **OpenAI Dots:** "proactive research" is read-only and happens "while you're away". Results surface when the user returns. We mirror that: read-only, idle-gated, digest on return.
- **Herdr platform:** no supervised daemon; `[[startup]]` is one-shot; the v1 engine is started by the startup hook and exits 30 s after Herdr goes away. So **schedules can only run while Herdr runs.** Installing launchd/systemd/Task Scheduler jobs would violate the plugin model, so it's out of scope (open decision O1). Herdr exposes agent status and `PaneInfo.Focused` but, as far as the v1 research found, no user-input activity timestamps (V1).
- **Web tools:** Claude Code headless can be given `WebSearch`/`WebFetch` tools; the Anthropic API has the server tools `web_search_20260209`/`web_fetch_20260209` on Opus 5.5 / Sonnet 5.5 (from the bundled claude-api skill); the OpenAI Responses API has a hosted web-search tool; Ollama and most OpenRouter models have none. Codex's web search flag for `exec` is V3.
- **Cron:** the standard 5-field syntax; parse with `github.com/robfig/cron/v3`'s parser (parser only; our own loop schedules).

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| R1 | Idle definition | Target pane `AgentStatus` idle/done **and** no new turns for `idle_minutes` **and** (if V1 finds a signal) no user input; one research pass per dot per idle period | Wall-clock timers (would run while you're actively reading output) |
| R2 | Research inputs | Open `question` entries from the dot's memory, files touched in the last 10 turns, the dot's purpose, and optional `topics` from the dot config | Free exploration of the whole repo (unbounded cost) |
| R3 | Research output | The verdict contract plus `remember` (Phase 2), with model-writable kinds widened to `research` and `question` in research passes only. The verdict message is the dot's "away" note | A new reply format |
| R4 | Digest | One `Kind:"digest"` card per pane per idle period, combining all research results that were `relevant` (parts like council cards). Created when the pane becomes active again (first new turn or focus), or immediately if any part is `critical` | One card per dot (noisy on return) |
| R5 | Scheduled runs | Cron in local time; standing `task` text; targets `projects = ["/path", …]` or `"attached"` (projects where this dot was attached in the last 14 days, from memory `meta.json`) | OS schedulers (O1) |
| R6 | Schedule de-duplication | `memory/<project>/schedule.json` (`{dot: last_run}`) under the project memory lock; a run claims its slot before starting | Per-engine state (two sessions would double-run) |
| R7 | Catch-up | At engine start, a schedule whose last slot was missed within `catch_up_hours` (default 12) runs once; older misses are skipped and logged | Running every missed slot |
| R8 | Commit trigger | The v1 poll loop also checks `git rev-parse HEAD` for each attached pane's repo every 15 s (hooks off). A new HEAD runs `on_commit` dots with `Question` = commit message + `git show --stat` + a patch capped at 20,000 characters | Git hooks (we never install hooks into user repos) |
| R9 | Where results go | With an attached pane on the project: that pane's queue. Otherwise: the **project inbox** (cards with `PaneID = ""`, `ProjectID` set), claimed into a pane's queue on attach | Dropping results; toasts only |
| R10 | Budgets | Per dot: `research.max_runs_per_day` (default 6), schedule and commit runs counted in the normal per-session call budget **and** a daily count. Global `daily_budget_usd` (default 1.00) for all background runs. Unknown-cost runs count 1 call but $0 (Phase 4 rule) | Session budgets only (background runs could span sessions) |
| R11 | Web | `research.web = true` adds web tools where supported (claude CLI `WebSearch,WebFetch`; Anthropic server web tools; OpenAI hosted web search). Unsupported backends → warning in the editor; research runs without web | Web for all research by default |
| R12 | Defaults | Research and schedules are **off per dot** by default; templates ship with them off. A global `proactive_enabled = true` kill switch | On by default (silent spending) |

---

## 4. Architecture

```
engine
 ├─ pollLoop (v1) ── + commitWatch (HEAD per attached repo) ──▶ runDot(Pass=review, Question=commit)
 ├─ idleWatch (new) ── idle period per pane ──▶ runDot(Pass=research) ──▶ memory ops + digest collector
 └─ scheduler (new) ── next cron slot ──▶ claim slot (memory lock) ──▶ runDot(Pass=review, Question=task)
                                                       │
                                     pane attached? ── yes ─▶ queue (pane)
                                                       └─ no ─▶ queue inbox (project) ── claimed on Attach
```

New files: `internal/engine/idle.go`, `internal/engine/schedule.go`, `internal/engine/commits.go`, `internal/schedule` (cron parsing and next-slot math, pure), `internal/state/daily.go` (cross-session daily counters under `STATE_DIR`, flock-guarded).

### 4.1 Changes to earlier interfaces (exact)

| Symbol | Change |
|---|---|
| `dots.Dot` | Add `Research ResearchSpec{Enabled bool; IdleMinutes, MaxRunsPerDay int; Web bool; Topics []string}` (TOML `[research]`; defaults false, 10, 6, false, `[]`), `Schedule ScheduleSpec{Cron, Task string; Projects []string; CatchUpHours int}` (TOML `[schedule]`; empty cron = off; `CatchUpHours` default 12). `TriggerSpec` gains `OnCommit bool` |
| `config.Config` | Add `ProactiveEnabled bool` (true), `DailyBudgetUSD float64` (1.00), `CommitPollSeconds int` (15) |
| `backend.Request` | Add `Web bool`; `Pass` gains `PassResearch = "research"` (research contract: "You are working in the background while the developer is away. Investigate the open questions and recent changes below. Report only what is worth their attention on return.") |
| `backend.ClaudeCLI.BuildCmd` | `Web` → tools add `WebSearch,WebFetch` (still no Bash/Edit/Write) |
| `backend.CodexCLI.BuildCmd` | `Web` → codex web-search flag per V3 |
| Phase 4 adapters | Anthropic: add `web_search_20260209` / `web_fetch_20260209` server tools when `Web` (don't also declare `code_execution`); OpenAI: hosted web search tool; others: ignore `Web` and set `Result.Warnings` |
| `backend.Result` | Add `Warnings []string` |
| `memory` (Phase 2) | Add `KindResearch`, `KindQuestion`; `Store.Apply` accepts them from model ops only when the call passes `ApplyOptions{Research: true}` (new variadic options parameter) |
| `memory.Store` | Add `ClaimSchedule(p, dot string, slot time.Time) (bool, error)` and `LastSchedule(p, dot string) (time.Time, error)` (file `schedule.json`) |
| `queue.Card` | Add `ProjectID string`; Kind `"digest"`. `queue.Store` gains `Inbox(projectID string) []Card` and `Claim(projectID, paneID string) (int, error)` |
| `state` | New `Daily` store: `OpenDaily(path string) (*Daily, error)`; `(d *Daily) Add(slug, kind string, costUSD float64, now time.Time) error`; `Count(slug, kind string, now time.Time) int`; `CostToday(now time.Time) float64`. File `STATE_DIR/daily.json`, flock-guarded (shared across sessions) |
| `state` feed | `OutcomeResearch`, `OutcomeScheduled`, `OutcomeCommit` |
| `engine.Deps` | Add `Clock func() time.Time`, `Daily *state.Daily`, `Git worktree.Git` (reuse the Phase 7 hook-free runner) |
| `engine` | `DecideResearch(in ResearchInput) bool`; `DecideBackground(in BackgroundInput) Reason` (shared daily and dollar caps); `Attach` claims inbox cards for the pane's project |
| `view.DotState` | Add `Next string` (e.g. `research in 6m`, `scheduled 09:00`) |
| `view.Pane` | Add `Away *AwayState{Since time.Time; Pending int}` (research results collected but not yet carded) |
| `paths.Paths` | Add `DailyFile() string` (`StateDir/daily.json`) |

---

## 5. Data model

```toml
# dot TOML
[research]
enabled         = true
idle_minutes    = 10
max_runs_per_day = 6
web             = false
topics          = ["dependency advisories for packages in package.json"]

[schedule]
cron           = "0 9 * * 1-5"
task           = "Review commits since the last run for security issues."
projects       = ["attached"]
catch_up_hours = 12

[trigger]
on_commit = true
```

`memory/<project>/schedule.json`: `{"version":1,"last":{"security":"2026-10-21T09:00:00-05:00"}}`.

`STATE_DIR/daily.json`: `{"version":1,"day":"2026-10-21","runs":{"security":{"research":3,"scheduled":1}},"cost_usd":0.37}` (resets when the local date changes).

---

## 6. Data flow

### 6.1 Idle research

1. `idleWatch` ticks every 30 s per attached pane: idle since the last turn ≥ `idle_minutes` → a new idle period id.
2. For each research-enabled dot: `DecideResearch` checks `proactive_enabled`, not paused, not already run this period, daily count < max, daily dollars < budget, session budget.
3. Build the request: `Pass: PassResearch`, `Turns: tail(10)`, `Question:` the rendered open questions (memory `question` entries, newest 5) + topics, `Memory:` the normal block, `RepoRead: true`, `Web: research.web`.
4. On result: `Store.Apply(..., ApplyOptions{Research: true})`; `Daily.Add`; feed `OutcomeResearch`; if relevant → add a part to the pane's away collector.
5. When the pane becomes active (a new turn arrives or the pane gains focus): build one digest card from the collected parts (Phase 3 `PlainMerge` formatting, no chair call) and clear the collector. A `critical` part creates the card immediately.

### 6.2 Scheduled runs

1. `scheduler` computes the next slot for every scheduled dot (`internal/schedule.Next(cron, after, loc)`), sleeps until the earliest slot (re-planned on `ReloadDots`).
2. At a slot: resolve target projects (`attached` → memory `Projects()` where the dot appears in a ledger or attachment in the last 14 days). For each: `ClaimSchedule` (false → another engine took it; skip).
3. Run with `Question: task + "\nSince: <last run>"`, the turns of an attached pane on that project if any (else none), `Cwd` = the project root (from `meta.json` roots; first existing path), `RepoRead: true`.
4. Relevant → card to the attached pane, or to the project inbox with a toast "Security (scheduled) has a finding for herdr-dots".
5. Engine start: for each scheduled dot and project, `LastSchedule` vs the previous slot → catch up once if within `catch_up_hours`.

### 6.3 Commit trigger

1. `commitWatch` (every `commit_poll_seconds`) runs `git rev-parse HEAD` per attached pane repo; it remembers the last HEAD per repo in memory (not persisted; the first poll after start only records).
2. A changed HEAD → for each attached dot with `on_commit`, through the normal `Decide` (paused, budgets, cooldown) → run with `Question` = commit context. Mode routing as for turns (inject allowed only if the dot is inject mode; commit-triggered cards count as `forceQueue = false`).

---

## 7. UI

- Roster: `◆ Security  queue  ○ idle · research in 6m` / `· scheduled Mon 09:00` / `◌ researching…`.
- Digest card: `☾ AWAY  3 dots · 42 min` with `space` to expand parts, like a council card.
- Editor: new sections "Research", "Schedule", and `on_commit` under Trigger, with a cron preview ("next: Mon 21 Oct 09:00").
- The pane header shows `today $0.37/1.00` when any background activity happened today.

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Behavior |
|---|---|
| Invalid cron | Editor validation error; an invalid file is flagged in the library like any invalid dot |
| Project root missing for a schedule | Skip that project; log; toast once per day |
| Daily budget reached | All background work stops for the day; one toast; roster shows `paused · daily budget` |
| Web tool unsupported | Editor warning; runs without web; `Result.Warnings` logged |
| Engine down at slot time | Catch-up rule R7 |

### 8.2 Safety

- Background runs are read-only (no write tools; Phase 7 drafts can never be scheduled).
- Web research sends search queries derived from your code to the search provider: documented, opt-in per dot.
- Daily dollar cap plus per-dot daily counts plus session budgets.
- Inbox cards obey normal queue approval; never injected without an attached pane.

### 8.3 Testing

| Layer | Tests |
|---|---|
| `schedule` | Next-slot math across DST transitions and month ends; invalid expressions |
| `state.Daily` | Day rollover with an injected clock; cross-process counting (re-exec test) |
| Engine | Idle research fires once per idle period with a fake clock; the digest card appears on the next turn, immediately for critical; daily cap stops research; scheduler claims slots so two engines on one memory dir run a slot exactly once; catch-up runs once within 12 h and skips older; the commit trigger runs `on_commit` dots on a new HEAD (real temp git repo) and not on the first poll; inbox cards are claimed on attach |
| Backend | `Web` flags per backend; Anthropic request includes the web server tools and no `code_execution` |
| UI | Roster `Next` strings; digest expand; editor cron preview |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | Does Herdr expose any user-activity signal (last input time, client attached/detached, focus changes as events)? | R1 idle definition |
| V2 | `claude -p --tools Read,Grep,Glob,WebSearch,WebFetch`: does web search work in print mode with subscription auth? | R11 |
| V3 | Does `codex exec` support web search (flag name), and is it allowed under `--sandbox read-only`? | R11 |
| V4 | Is the 15 s `git rev-parse HEAD` poll cheap enough with 4 panes on large repos (fsmonitor off)? | R8 |

---

## 10. Out of scope

OS-level schedulers that run without Herdr (O1); write-capable background work; notifications outside Herdr (email, push); research across projects the user never attached a dot to.

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Schedules without Herdr running | (a) only while Herdr runs, with catch-up; (b) install OS scheduler entries | **(a)** | Respects the plugin model (no daemons, nothing outside the plugin dirs) |
| O2 | Default daily budget | $0.50 / **$1.00** / $5.00 / none | **$1.00** | Background work should never surprise you on a bill |
| O3 | Digest timing | (a) on return (next turn/focus), immediate for critical; (b) immediately for everything | **(a)** | Matches "while you were away" and avoids interrupting |
| O4 | Commit trigger mechanism | (a) HEAD polling; (b) installing a git `post-commit` hook | **(a)** | Never modify user repos |
| O5 | Is this phase in scope for feature complete? | (a) yes; (b) defer beyond 1.0 | **(a)** | "Always-on" and proactive research are the core of the Dots idea |

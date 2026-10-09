# Herdr Dots Phase 8: Proactive Dots, Implementation Plan Outline

**Status: DRAFT, pending user review** (phase proposed by drafter)

> **This is an outline, not an executable plan.** Earlier phases' code doesn't exist yet. When Phase 8 starts, write the full step-by-step plan with superpowers:writing-plans against the real code, then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Make dots always-on: idle-time read-only research with a "while you were away" digest, cron-scheduled standing tasks, and commit-triggered reviews, all capped by daily run and dollar budgets.

**Architecture:** Three engine watchers (`idleWatch`, `scheduler`, `commitWatch`) feed the existing `runDot`. Research results go to Phase 2 memory (`research`/`question` kinds) and a per-pane away collector that becomes one digest card. Schedules claim slots in project memory so two sessions never double-run. Results for unattached projects wait in a queue inbox. A cross-session `state.Daily` enforces daily caps.

**Tech Stack:** Go 1.25; new dependency `github.com/robfig/cron/v3` (parser only); existing dependencies.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-8-proactive-dots-design.md`

## Global Constraints

- The product is **not** done after this phase. Version `0.8.0`. README roadmap keeps Phase 2 **VITAL**.
- Platforms: macOS, Linux, Windows (CI matrix).
- All background work is read-only; Phase 7 write runs can never be triggered by idle, schedule or commit events.
- Research and schedules are off per dot by default; global `proactive_enabled` kill switch; `daily_budget_usd` default 1.00.
- No OS schedulers, no git hooks installed in user repos; git commands hook-free.
- All time logic takes an injected clock; tests never sleep in real time beyond 100 ms.
- `go test -race ./...` and `go vet ./...` on all CI OSes before every commit.

## Review Focus

1. **A DST transition or a laptop sleeping through a scheduled slot.** Expect exactly one catch-up run within 12 h, none older, and no burst of missed slots on wake. (Tasks 2, 4)
2. **Two Herdr sessions with the same scheduled dot and project.** Expect exactly one run per slot. (Task 4)
3. **The user coming back mid-research.** A new turn arrives while research runs. Expect the research result folded into the digest (or discarded if irrelevant), no duplicate digest, and the normal turn review unaffected. (Task 3)
4. **A force-push or branch switch changing HEAD to an older commit.** Expect the commit trigger to run once, labeled with the commit, and not to treat history rewrites as many commits. (Task 5)
5. **The daily budget hit at 23:59 and the date rolling over.** Expect background work resumed after local midnight, with the counter reset atomically across sessions. (Task 2)

---

## File Structure

```
internal/schedule/schedule.go        Parse, Next (pure)
internal/state/daily.go              Daily store (flock, cross-session)
internal/paths/paths.go              + DailyFile
internal/dots/dot.go                 + ResearchSpec, ScheduleSpec, TriggerSpec.OnCommit
internal/config/config.go            + ProactiveEnabled, DailyBudgetUSD, CommitPollSeconds
internal/backend/backend.go          + Request.Web, PassResearch, Result.Warnings
internal/backend/prompt.go           research contract
internal/backend/claude.go, codex.go web tools
internal/backend/anthropicapi/, openaiapi/   web tools
internal/memory/types.go, store.go   + KindResearch, KindQuestion, ApplyOptions, ClaimSchedule, LastSchedule
internal/queue/queue.go              + ProjectID, KindDigest, Inbox, Claim
internal/engine/idle.go              idleWatch, DecideResearch, away collector, digest
internal/engine/schedule.go          scheduler, catch-up
internal/engine/commits.go           commitWatch
internal/engine/background.go        DecideBackground (daily + dollar caps)
internal/engine/engine.go            Deps.Clock/Daily/Git, Attach claims inbox
internal/view/view.go                + DotState.Next, Pane.Away
internal/state/feed.go               + outcomes
internal/ui/manage/form.go           research, schedule, on_commit fields, cron preview
internal/ui/pane/model.go            Next strings, digest card, today's cost
README.md
```

---

## Tasks

### Task 1: Verification spike (V1–V4)

**Files:** Create `docs/superpowers/notes/phase-8-verification.md`.
**Interfaces:** Findings only.
**Tests:** None. STOP on a "no" for V2/V3 only if it removes web research entirely (then mark R11 "Anthropic API / OpenAI only").

### Task 2: Schedule math and the daily store

**Files:** Create `internal/schedule/schedule.go`, `internal/state/daily.go`. Modify `internal/paths/paths.go`. Test: `internal/schedule/schedule_test.go`, `internal/state/daily_test.go`.
**Interfaces:**
- Produces: `schedule.Parse(expr string) (Spec, error)`; `type Spec struct{ … }`; `(Spec) Next(after time.Time) time.Time`; `(Spec) Prev(before time.Time) time.Time`; `state.OpenDaily(path string) (*Daily, error)`; `(*Daily) Add(slug, kind string, costUSD float64, now time.Time) error`; `(*Daily) Count(slug, kind string, now time.Time) int`; `(*Daily) CostToday(now time.Time) float64`; `(paths.Paths) DailyFile() string`.
**Tests:** `Next`/`Prev` for weekday, monthly and DST cases (America/New_York spring-forward and fall-back); invalid expressions; `Daily` rollover at local midnight; cross-process `Add` (re-exec test) sums correctly; file `0600`.

### Task 3: Idle research and the digest

**Files:** Create `internal/engine/idle.go`, `internal/engine/background.go`. Modify `internal/dots/dot.go`, `internal/config/config.go`, `internal/backend/backend.go`, `prompt.go`, `internal/memory/types.go`, `store.go`, `internal/queue/queue.go`, `internal/view/view.go`, `internal/state/feed.go`, `internal/engine/engine.go`. Test: `internal/engine/idle_test.go`, extend `dot_test.go`, `prompt_test.go`, `store_test.go`.
**Interfaces:**
- Consumes: v1 `runDot`, `Queue.Add`; Phase 2 `Store.Apply`, `Select`; Phase 3 `council.PlainMerge` (for digest formatting); Task 2 `Daily`.
- Produces: `dots.ResearchSpec{Enabled bool; IdleMinutes, MaxRunsPerDay int; Web bool; Topics []string}`; `config.Config.ProactiveEnabled bool`, `DailyBudgetUSD float64`; `backend.Request.Web bool`; `backend.PassResearch`; `backend.Result.Warnings []string`; `memory.KindResearch`, `memory.KindQuestion`; `type memory.ApplyOptions struct{ Research bool }`; `(*memory.Store) Apply(p, dot string, ops []backend.MemoryOp, ev Evidence, opts ...ApplyOptions) (ApplyResult, error)`; `queue.KindDigest = "digest"`; `engine.ResearchInput{…}`; `engine.DecideResearch(in ResearchInput) bool`; `engine.BackgroundInput{Slug, Kind string; DailyCount, MaxPerDay int; CostToday, DailyBudget float64; Proactive bool}`; `engine.DecideBackground(in BackgroundInput) Reason`; `engine.Deps.Clock`, `engine.Deps.Daily`; `view.Pane.Away *AwayState`; `state.OutcomeResearch`.
**Tests (fake clock, fake backend):** no research before `idle_minutes`; exactly one per idle period; a new turn starts a new period; the digest card is created on the next turn with all relevant parts; immediate for a critical part; a model `question` op accepted only in research passes; daily cap and dollar cap stop research; `proactive_enabled=false` stops everything; research requests carry `RepoRead=true`, the rendered open questions, `Web` per dot.

### Task 4: Scheduler and catch-up

**Files:** Create `internal/engine/schedule.go`. Modify `internal/dots/dot.go`, `internal/memory/store.go`, `internal/queue/queue.go`, `internal/engine/engine.go` (`Attach` claims inbox). Test: `internal/engine/schedule_test.go`, extend `store_test.go`, `queue_test.go`.
**Interfaces:**
- Consumes: Task 2 `schedule.Spec`; memory `Projects()`, project meta roots.
- Produces: `dots.ScheduleSpec{Cron, Task string; Projects []string; CatchUpHours int}`; `(*memory.Store) ClaimSchedule(p, dot string, slot time.Time) (bool, error)`; `(*memory.Store) LastSchedule(p, dot string) (time.Time, error)`; `queue.Card.ProjectID string`; `(*queue.Store) Inbox(projectID string) []Card`; `(*queue.Store) Claim(projectID, paneID string) (int, error)`; `state.OutcomeScheduled`.
**Tests:** a slot fires once with a fake clock; two engines sharing a memory dir → one run (`ClaimSchedule` false for the second); catch-up within 12 h runs once, older is skipped; `projects=["attached"]` resolves from memory; no attached pane → an inbox card and a toast; `Attach` claims inbox cards into the pane queue; `ReloadDots` re-plans the next slot.

### Task 5: Commit trigger

**Files:** Create `internal/engine/commits.go`. Modify `internal/dots/dot.go` (`TriggerSpec.OnCommit`), `internal/config/config.go` (`CommitPollSeconds`), `internal/engine/engine.go` (`Deps.Git`). Test: `internal/engine/commits_test.go` (real temp git repo).
**Interfaces:**
- Consumes: Phase 7 `worktree.Git` runner (hook-free).
- Produces: `dots.TriggerSpec.OnCommit bool`; `config.Config.CommitPollSeconds int`; `engine.Deps.Git worktree.Git`; `state.OutcomeCommit`.
**Tests:** the first poll records HEAD without running; a new commit runs `on_commit` dots once with the commit context in `Question` (patch capped at 20,000 characters); a reset to an older commit runs once; paused or over-budget dots skip through `Decide`; dots without `on_commit` don't run.

### Task 6: Web tools in backends

**Files:** Modify `internal/backend/claude.go`, `codex.go`, `internal/backend/anthropicapi/anthropic.go`, `internal/backend/openaiapi/openai.go`, `openrouter`, `ollama`. Test: extend each backend's tests.
**Interfaces:**
- Produces: `Web` handling per spec R11; `Result.Warnings` for unsupported backends.
**Tests:** claude argv includes `WebSearch,WebFetch` only when `Web`, never `Bash`/`Edit`/`Write`; the codex flag per V3; the Anthropic request includes `web_search_20260209` and `web_fetch_20260209` and no `code_execution`; the OpenAI request includes the hosted web search tool; Ollama/OpenRouter return a warning.

### Task 7: UI and README

**Files:** Modify `internal/ui/manage/form.go`, `internal/ui/pane/model.go`, `README.md`, `internal/version/version.go` (`0.8.0`). Test: extend the UI tests.
**Interfaces:**
- Consumes: `view.DotState.Next`, `view.Pane.Away`, `schedule.Parse` (preview).
- Produces: editor sections Research and Schedule plus the `on_commit` toggle with the cron preview; roster `Next` strings; digest card rendering and expand; the header's today cost.
**Tests:** invalid cron blocks save with a message; the preview shows the next slot; roster strings; digest card expand; README: proactive features, opt-in steps, budgets, web privacy note; Phase 2 still **VITAL**; no "done/ready"; a manual soak test (2 h idle with research on a test repo) recorded in the notes.

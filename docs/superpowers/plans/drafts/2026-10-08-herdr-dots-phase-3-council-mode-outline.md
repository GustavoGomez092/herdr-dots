# Herdr Dots Phase 3: Council Mode, Implementation Plan Outline

**Status: DRAFT, pending user review**

> **This is an outline, not an executable plan.** v1 and Phase 2 code don't exist yet. When Phase 3 starts, write the full step-by-step plan with superpowers:writing-plans against the real code, then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Let the user convene all attached dots at once and receive one merged, attributed digest card, with an optional second round of dot-to-dot cross-review.

**Architecture:** A pure `internal/council` package builds prompts, parses stances and digests, and does a plain merge. `internal/engine/council.go` orchestrates member runs through v1's `runDot` (with a council result sink), runs the chair through the normal `backend.Backend`, and adds one `Kind:"council"` queue card. The summon popup gains a council mode, and the pane renders and expands council cards.

**Tech Stack:** Go 1.25 and existing dependencies only.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-3-council-mode-design.md`

## Global Constraints

- The product is **not** done after this phase. Version `0.3.0`. Phase 2 (dot memory) remains marked **VITAL** in the README roadmap.
- Go module `herdr-dots`, `go 1.25`; macOS and Linux only.
- Council cards are always queue mode: never inject, never suggest.
- Dot name/slug `council` is reserved.
- No changes to the `backend.Backend` interface.
- Member runs go through v1's `sem` (`max_parallel_runs`); one council per pane at a time.
- Files `0600`, directories `0700`, atomic writes (inherited).
- Run `go test -race ./...` and `go vet ./...` before every commit; tests never touch the user's real Herdr session.

## Review Focus

1. **A member hangs until timeout while the others finish.** Expect the council to finish at the overall deadline with the member marked `✗ timed out`, not a council stuck "running" forever. (Task 3)
2. **The user closes the target pane mid-council.** Expect the in-flight council to be abandoned, no card created for a dead pane (or the card marked orphaned if created), and no goroutine leak. (Task 3)
3. **The chair returns prose instead of the JSON verdict, or drops attribution.** Expect a plain merge or an `unverified` footer. The user always gets a card. (Tasks 2 and 3)
4. **A council delivery triggering every dot again.** Expect zero member runs on the turn that follows a council delivery. (Task 1)
5. **A tight budget.** With 2 calls left and a 4-member council, expect preflight to refuse with a clear reason before any call is spent. (Task 3)

---

## File Structure

```
internal/transcript/turn.go          + CouncilName, CouncilGlyph, IsFromDot council rule
internal/dots/dot.go                 reserve "council"
internal/queue/queue.go              + Card.Kind, Card.Parts, Card.Question, CardPart
internal/config/config.go            + CouncilConfig
internal/council/council.go          Part, Preflight, prompts, PlainMerge, ParseDigest, stances
internal/engine/council.go           Council, CouncilPreflight, collector, chair run
internal/engine/engine.go            runDot council sink, runOpts fields
internal/engine/deliver.go           memory outcomes per part
internal/engine/handle.go            CmdCouncil, CmdCouncilPreflight
internal/ipc/ipc.go                  + CmdCouncil, CmdCouncilPreflight, CouncilArgs
internal/view/view.go                + CouncilState, Pane.Council, Pane.CouncilPreflight
internal/state/feed.go               + OutcomeCouncil
internal/ui/summon/model.go          council mode
internal/ui/pane/model.go            council card rendering, expand, C key, progress
cmd/herdr-dots/action.go             action "council"
herdr-plugin.toml                    [[actions]] council, [[panes]] council popup
internal/keys/keys.go                Defaults + prefix+shift+c
README.md                            council section
```

---

## Tasks

### Task 1: Reserved council identity and the loop guard

**Files:** Modify `internal/transcript/turn.go`, `internal/dots/dot.go`, `internal/queue/queue.go`, `internal/state/feed.go`. Test: extend `internal/transcript/turn_test.go`, `internal/dots/dot_test.go`, `internal/queue/queue_test.go`.
**Interfaces:**
- Consumes: v1 `Turn`, `FromDots`, `DotPrefix`, `dots.Validate`, `queue.Card`.
- Produces: `transcript.CouncilName = "Council"`, `transcript.CouncilGlyph = "⊕"`; `(Turn) IsFromDot(name string) bool` (council rule); `queue.Card.Kind string`, `queue.Card.Parts []queue.CardPart`, `queue.Card.Question string`; `queue.CardPart{Dot, DotName, Glyph, Severity, Body, Stance string; Failed bool}`; `queue.KindCouncil = "council"`; `state.OutcomeCouncil = "council"`.
**Tests:** `FromDots("[⊕ Council dot] …") == ["Council"]`; `IsFromDot("Security")` is true for a council turn; `Validate` rejects name "Council" and " council "; an old `queue.json` without the new fields loads; a council card round-trips through persistence with its parts.

### Task 2: `internal/council` package

**Files:** Create `internal/council/council.go`. Test: `internal/council/council_test.go`.
**Interfaces:**
- Consumes: `backend.Verdict`, `queue.CardPart`.
- Produces: `type Part struct{ Slug, Name, Glyph string; Verdict backend.Verdict; Stance string; Err string }`; `type Preflight struct{ Members, Skipped []string; Reasons map[string]string; EstimatedCalls int; Allowed bool; Refusal string }`; `const ChairPurpose string`; `StanceRule() string`; `OthersBlock(parts []Part, self string) string`; `ChairQuestion(question string, parts []Part) string`; `ParseStance(message string) (stance, rest string)`; `ParseDigest(message string) (body string, verified bool)`; `PlainMerge(parts []Part) string`; `MaxSeverity(parts []Part) string`; `ToCardParts(parts []Part) []queue.CardPart`.
**Tests:** `PlainMerge` orders critical → warn → info, attributes each line, lists failed parts under "Not heard from"; `ChairQuestion` includes every relevant part and excludes passes; `OthersBlock` excludes self; `ParseStance` table; `ParseDigest` verified/unverified cases; `MaxSeverity` with no relevant parts → `info`.

### Task 3: Engine orchestration

**Files:** Create `internal/engine/council.go`. Modify `internal/engine/engine.go` (`runOpts.council`, `runOpts.noForce`, the sink in `runDot`), `internal/engine/deliver.go` (per-part memory outcomes), `internal/config/config.go`, `internal/view/view.go`. Test: `internal/engine/council_test.go`, extend `internal/config/config_test.go`.
**Interfaces:**
- Consumes: Task 1 and Task 2 symbols; v1 `runDot`, `runOpts`, `sem`, `Deps.Backend`, `Usage`, `Queue.Add`, `reportPending`, `broadcast`; Phase 2 `memory.Select/Render` (project scope for the chair), `memory.Store.RecordOutcome`.
- Produces: `config.CouncilConfig{ChairBackend, ChairModel string; Rounds, MaxMembers int; Merge string}` on `Config.Council`; `(*Engine) CouncilPreflight(paneID string, slugs []string) (council.Preflight, error)`; `(*Engine) Council(ctx context.Context, paneID, question string, slugs []string) (string, error)`; `view.CouncilState{ID, Phase string; Done, Total, Round int}`; `view.Pane.Council *CouncilState`; `view.Pane.CouncilPreflight *council.Preflight`; `var ErrCouncilRunning`.
**Tests (fake Herdr + scripted fake backends per dot and for the chair):** happy path with 3 members → 1 card, 3 parts, the chair digest, severity = max; member timeout → failed part plus card; all members fail → no card, feed error; chair garbage → `PlainMerge` with footer; 0 relevant → "No dot had anything to add."; `rounds=2` → 2 calls per member and the round-2 `Question` contains `OTHER DOTS SAID`; `ErrCouncilRunning`; preflight refusal when `PaneCalls + estimate > SessionBudgetCalls`; skipped members listed with reasons (paused, budget); the pane closed mid-council → no card and no leak (`goleak`-style check via `runtime.NumGoroutine` settle); send/dismiss → `RecordOutcome` called once per relevant part (fake memory store); observe dots included.

### Task 4: IPC commands

**Files:** Modify `internal/ipc/ipc.go`, `internal/engine/handle.go`. Test: extend `internal/ipc/ipc_test.go`, `internal/engine/handle_test.go`.
**Interfaces:**
- Consumes: Task 3 engine methods.
- Produces: `ipc.CmdCouncil = "council"`, `ipc.CmdCouncilPreflight = "council_preflight"`, `ipc.CouncilArgs{PaneID, Question string; Slugs []string}`; `Engine.Handle` routes both.
**Tests:** JSON round-trip of `CouncilArgs`; `Handle(CmdCouncil)` starts a council (snapshot shows `Council.Phase == "members"`); `Handle(CmdCouncilPreflight)` stores the preflight in the snapshot.

### Task 5: Council popup and plugin action

**Files:** Modify `internal/ui/summon/model.go`, `cmd/herdr-dots/action.go`, `cmd/herdr-dots/ui.go`, `herdr-plugin.toml`, `internal/keys/keys.go`. Test: extend `internal/ui/summon/model_test.go`, `cmd/herdr-dots/action_test.go`, `internal/keys/keys_test.go`.
**Interfaces:**
- Consumes: `view.Pane.CouncilPreflight`, `ipc.CmdCouncilPreflight`, `ipc.CmdCouncil`, v1 `OpenPluginPane`.
- Produces: `summon.NewCouncil(target string, c Caller) Model`; `runSummonUI` accepts `--council`; `runAction("council")` opens entrypoint `council`; manifest `[[actions]] id="council"` and `[[panes]] id="council" placement="popup" width="70%" height=16 command=["./bin/herdr-dots","summon-ui","--council"]`; `keys.Defaults()` includes `prefix+shift+c`.
**Tests:** on open the popup sends preflight; members preselected and ineligible ones disabled; `space` toggles; Enter sends `council` with the selected slugs and the question; Enter disabled when `Allowed == false`; the summon popup's first entry "⊕ Council" switches to council mode; manifest parse test includes the new action; keys plan detects a `prefix+shift+c` conflict.

### Task 6: Pane rendering and README

**Files:** Modify `internal/ui/pane/model.go`, `README.md`, `internal/version/version.go` (`0.3.0`). Test: extend `internal/ui/pane/model_test.go`.
**Interfaces:**
- Consumes: `queue.Card.Kind/Parts`, `view.Pane.Council`.
- Produces: council card rendering, `space` expand/collapse, the `C` key (opens the council popup through the same path as the action), the progress line.
**Tests:** council card header shows `Council · turn N · K dots`; `space` expands to per-member lines with stances; a failed member shows `✗`; the progress line during `members` and `merging`; `C` triggers the open-council call; `go test -race ./... && go vet ./...`; a manual smoke test in `herdr --session dots-test` with 3 dots, recorded in the notes file; the README roadmap keeps Phase 2 **VITAL** and no "done/ready" language.

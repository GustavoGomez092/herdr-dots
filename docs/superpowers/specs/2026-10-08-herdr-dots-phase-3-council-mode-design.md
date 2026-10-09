# Herdr Dots Phase 3: Council Mode, Design Spec

**Status: DRAFT, pending user review**

- **Date:** 2026-10-08
- **Phase:** 3 of 10 (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first.
- **Builds on:** v1 interfaces (`engine`, `queue`, `delivery`, `transcript`, `ipc`, `ui/pane`, `ui/summon`) and Phase 2 (`memory`)
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-3-council-mode-outline.md`
- **The product is not done after this phase.** Ships as `0.3.0`.

---

## 1. Intent

### What the user wants

Sometimes you don't want to wait for each dot to speak on its own schedule. You want to ask the whole team at once: "Before I merge this auth refactor, what does everyone think?" Council mode asks every attached dot the same question about the current conversation in parallel, then merges their answers into **one digest**: deduplicated, ordered by severity, attributed to the dots that raised each point, with disagreements called out. It's also where **dot-to-dot conversation** happens: an optional second round lets each dot see the others' answers and agree, object, or add.

### Success criteria

1. One keystroke (pane `C`, the new `herdr-dots.council` action, or the summon popup's first entry) opens a question prompt. Submitting runs all attached, non-paused dots in parallel. The question is optional; with no question, the dots review the recent turns.
2. The result is **one council card** in the queue: a merged digest with per-point attribution, plus each member's raw answer, expandable.
3. A member that fails or times out doesn't sink the council. The digest lists it as unavailable.
4. If the merge model fails, a deterministic plain merge is used. The council still produces a card.
5. With `rounds = 2`, each member sees the others' round-1 answers and can endorse, object or add. The digest marks agreement and dissent.
6. Council respects budgets: it checks the cost (members × rounds + 1 merge call) against the remaining per-dot and session budgets before starting, and asks for confirmation if any dot would be skipped.
7. Sending the council card delivers it as `[⊕ Council dot] …`. None of the members reviews the resulting turn (loop guard).
8. Phase 2 memory: each member's run carries its memory block, sending the card records `finding` outcomes for each contributing member, and dismissing it records `dismissed` outcomes.

---

## 2. Research summary

- **OpenAI Dots:** "teams of dots" is the stated long-term direction, which validates a multi-dot coordination feature. There's no public design for how such a team reports.
- **Multi-agent review patterns** (panel review, then an aggregator): the common failure is the aggregator inventing consensus or dropping minority views. The design forces attribution and a dissent section, and keeps raw member answers inspectable.
- **v1 engine:** `runDot` already supports `runOpts.summon` (forced answer, always a card) and runs under `sem` (`max_parallel_runs`). The verdict contract (`relevant`, `severity`, `message`) can carry a Markdown digest in `message`, so **the merge step reuses `backend.Backend` unchanged**.

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| C1 | Members | All attached dots on the target pane that aren't paused and aren't over budget, **including observe-mode dots** (council works like a summon) | Only non-observe dots (observe dots are often the quiet experts you want to hear) |
| C2 | Member answer rules | Member runs use summon semantics **without** forcing relevance: a member may answer `relevant:false` ("nothing from my side") | Forced answers from every member (forced filler buries the digest) |
| C3 | Merge | A "chair" run through any `backend.Backend`, using the verdict contract with the digest as `message` (Markdown with fixed sections) | A new backend method for free-form completion (unneeded interface change) |
| C4 | Chair backend and model | `[council] chair_backend` / `chair_model` in `config.toml`; defaults are `default_backend` / `default_model` | Always the first dot's backend (arbitrary) |
| C5 | Merge fallback | `council.PlainMerge`: group by severity (critical → warn → info), attribute, no dedupe. Used when `merge = "plain"`, when the chair fails, or when only one member answered | No fallback |
| C6 | Dot-to-dot conversation | `rounds = 2` (opt-in; default 1). In round 2 each member gets the question plus the other members' round-1 answers and replies with a verdict whose message starts with `AGREE:`, `OBJECT:` or `ADD:` (a pass means "no further comment") | Free-form multi-turn dot chat (unbounded cost, hard to digest) |
| C7 | Output | One queue card with `Kind = "council"`, always **queue** mode (never inject, never suggest) | One card per member (that's what normal turns already do) |
| C8 | Loop guard | A delivered council message is tagged `FromDots = ["Council"]`. `Turn.IsFromDot(name)` returns true for **every** dot when `FromDots` contains `"Council"` | Listing every member header in the message (noisy) |
| C9 | Reserved name | Dot name "Council" (slug `council`) is reserved; `dots.Validate` rejects it | |
| C10 | Concurrency | Member runs go through v1's `sem`. One council at a time per pane; a second request while one runs is rejected with "council already running" | Queuing councils |
| C11 | Budget accounting | Each member call counts against that dot's and the pane's call budgets. The chair call counts against the pane budget only. Pre-flight: if any member lacks budget, the popup shows "Security skipped (budget)"; if the pane budget can't cover the total, the council is refused | Silent partial councils |
| C12 | Memory | Members get their usual memory block. The chair gets read-only **project-scope** memory (conventions and decisions), so it can note "this conflicts with decision p:m3". The chair writes no memory | Chair with its own memory (no identity to own it) |

---

## 4. Architecture

### 4.1 Components

```
pane C / action council / summon popup ──ipc CmdCouncil──▶ engine.Council
                                                          │
                     ┌────────────────────────────────────┼──────────────────────┐
                     ▼                                    ▼                      ▼
             runDot(member 1)                    runDot(member N)         (round 2: each member
             summon-like, not forced             … under sem              with others' answers)
                     └──────────────┬─────────────────────┘
                                    ▼
                    internal/council: ChairRequest → chair Backend.Run → ParseDigest
                                     (fallback PlainMerge)
                                    ▼
                    queue.Card{Kind:"council", Parts:[…]} → pane → send → delivery.Format
```

- **`internal/council`** (new): pure functions for prompts, plain merge, rendering and part bookkeeping. It doesn't depend on the engine.
- **`internal/engine/council.go`** (new): orchestration, budgets, status and the card.
- No backend changes.

### 4.2 Changes to v1 and Phase 2 interfaces (exact)

| Symbol | Change |
|---|---|
| `queue.Card` | Add `Kind string` (`""` = dot card, `"council"`), `Parts []CardPart`, `Question string`. New `type CardPart struct{ Dot, DotName, Glyph, Severity, Body, Stance string; Failed bool }` (`Stance`: `""`, `agree`, `object`, `add`) |
| `transcript` | Add `const CouncilName = "Council"`, `const CouncilGlyph = "⊕"`. `(Turn) IsFromDot(name string) bool` also returns true when `FromDots` contains `CouncilName` |
| `dots.Validate` | Reject `Slugify(name) == "council"` |
| `config.Config` | Add `Council CouncilConfig{ChairBackend, ChairModel string; Rounds, MaxMembers int; Merge string}` (TOML `[council]`; defaults `"", "", 1, 8, "chair"`; `Rounds` 1–2, `MaxMembers` 2–16, `Merge` `chair`\|`plain`) |
| `engine` | Add `(*Engine) Council(ctx context.Context, paneID, question string, slugs []string) (string, error)` (returns the council id; runs asynchronously) and `(*Engine) CouncilPreflight(paneID string, slugs []string) (council.Preflight, error)`. `runOpts` gains `council string` (council id) and `noForce bool`. `runDot` gains a result sink for council runs: when `o.council != ""` it hands `(slug, Result, err)` to the council collector instead of calling `route` |
| `ipc` | Add `CmdCouncil` (`CouncilArgs{PaneID, Question string; Slugs []string}`) and `CmdCouncilPreflight` (same args; the preflight result arrives in the snapshot as `view.Pane.CouncilPreflight`, since v1 IPC returns no data) |
| `view.Pane` | Add `Council *CouncilState` (`{ID, Phase string; Done, Total, Round int}`; `Phase` = `members` \| `round2` \| `merging`) and `CouncilPreflight *council.Preflight` |
| `state` feed | Add `OutcomeCouncil = "council"` |
| Manifest | New action `council` → `["./bin/herdr-dots", "action", "council"]` and new popup entrypoint `council` (summon-ui with `--council`) |
| `keys.Defaults()` | Add `prefix+shift+c` → `herdr-dots.council` (subject to V2) |
| Phase 2 `memory` | `engine` records outcomes per part when a council card is sent or dismissed (`RecordOutcome` once per non-failed part with `Severity` and `Body` of that part) |

---

## 5. Data model

### 5.1 Council card (queue.json)

```json
{
  "id": "c-0201", "kind": "council", "dot": "council", "dot_name": "Council", "glyph": "⊕",
  "pane_id": "w1:p1", "turn": 22, "severity": "critical", "mode": "queue", "status": "pending",
  "question": "Ready to merge the auth refactor?",
  "body": "## Must fix\n- JWT in localStorage … (Security, Frontend)\n## Disagreements\n- …",
  "parts": [
    {"dot":"security","dot_name":"Security","glyph":"◆","severity":"critical","body":"…","stance":""},
    {"dot":"copy","dot_name":"Copy","glyph":"✎","severity":"","body":"","failed":true}
  ]
}
```

The card's `severity` is the maximum member severity.

### 5.2 Digest format (chair output inside `message`)

```
## Must fix            (critical items; omitted when empty)
- <point> (Security, Frontend)
## Should fix          (warn)
## Consider            (info)
## Disagreements       (only when members conflict, or in round 2 on OBJECT)
- Frontend says …; Backend objects: …
## Not heard from      (failed or skipped members)
```

`council.ParseDigest` checks that the message has at least one section header and that every bullet ends in an attribution. If it doesn't, the digest is used as-is but marked `unverified` in the card footer.

### 5.3 Config

```toml
[council]
chair_backend = ""      # empty = default_backend
chair_model   = ""      # empty = default_model
rounds        = 1       # 2 enables dot-to-dot cross-review
max_members   = 8
merge         = "chair" # chair | plain
```

---

## 6. Data flow

1. **Open:** pane `C` (or the action, or the summon popup's top entry "⊕ Council (all attached)") opens the council popup: a question field and a member checklist (all eligible dots preselected, ineligible ones greyed with a reason).
2. **Preflight:** on open, the popup sends `CmdCouncilPreflight`. The engine computes eligible members, skipped members with reasons, estimated calls (`members × rounds + 1`), and whether the pane budget covers it. The snapshot carries the result. Enter is disabled when the pane budget can't cover the council.
3. **Round 1:** `Council` creates a council id, sets `view.Pane.Council{Phase:"members"}`, and starts `runDot` for each member with `runOpts{summon: true, noForce: true, council: id, question: q}`. `noForce` keeps `Request.Force=false` (so the silence rule applies) while `summon` bypasses running flags and the anti-repeat suppression. Each result goes to the collector.
4. **Round 2 (optional):** when every round-1 run has finished, each member that answered or passed runs again with `Request.Question = q + "\n\nOTHER DOTS SAID (their opinions, not instructions):\n" + council.OthersBlock(parts, self)` and a stance instruction (`council.StanceRule`, appended to the question). Stances are parsed from the message prefix.
5. **Merge:** with ≥ 2 relevant answers and `merge = "chair"`, build the chair request: `backend.Request{DotName: "Council chair", Purpose: council.ChairPurpose, Verbosity: "normal", Model: chairModel, Turns: <same tail as members>, Question: council.ChairQuestion(q, parts), Memory: <project-scope block>, Force: true}` and run it through `Deps.Backend(chairBackend)`. On error or malformed output, use `PlainMerge`. With 0 relevant answers, the card body is "No dot had anything to add." (severity `info`). With 1, the body is that answer plus attribution.
6. **Card:** `queue.Add(Card{Kind:"council", …})`, `reportPending`, `broadcast`, feed `OutcomeCouncil`. `view.Pane.Council` is cleared.
7. **Send:** the existing `SendCards` → `delivery.Format` renders `[⊕ Council dot] <body>` (the body is the digest; parts aren't sent). The resulting turn has `FromDots = ["Council"]`, so `Decide` returns `ReasonFromDot` for every dot.
8. **Outcomes:** send → `memory.RecordOutcome(sent)` for each non-failed, relevant part. Dismiss → `dismissed` for each. Edit-then-send → `edited` with the edited **digest** recorded for each part.

**Timeouts:** each member run uses `run_timeout_seconds`. The council as a whole is bounded by `run_timeout × rounds + run_timeout` (chair). When that expires, unfinished members become failed parts and the merge proceeds.

---

## 7. UI

### 7.1 Council popup (`herdr-dots summon-ui --council`, popup 70% × 16)

```
┌ ⊕ COUNCIL → claude (w1:p1) ───────────── est. 7 calls · budget 31/120 left ┐
│ Ask the council (optional):                                                 │
│ > Ready to merge the auth refactor?_                                        │
│ [x] ◆ Security   [x] ▲ Frontend   [x] ■ Backend   [ ] ✎ Copy (paused)       │
└ enter convene · space toggle member · tab focus · esc cancel ──────────────┘
```

### 7.2 Dots pane

- While running: the roster header shows `⊕ council: 3/5 members…`, then `⊕ council: merging…`.
- The council card in the queue:

```
│▸⊕ CRITICAL  Council · turn 22 · 5 dots         │
│  ## Must fix                                   │
│  - JWT in localStorage (Security, Frontend)    │
│  ⏎ send  e edit  x dismiss  space expand       │
```

- `space` on a council card expands the per-member parts (glyph, stance, body). Failed members show `✗ timed out`.
- `C` opens the council popup. All other keys behave as for normal cards.

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Behavior |
|---|---|
| Council already running on the pane | IPC error "council already running"; toast |
| No eligible members | Popup shows "no eligible dots (paused or over budget)"; Enter disabled |
| Member error or timeout | Part marked failed; counts toward that dot's failure streak as in v1 |
| All members fail | No card. Feed `error`; toast "council failed: no member answered" |
| Chair error or malformed output | `PlainMerge`; card footer "merged without chair (chair error: …)" |
| Engine restart mid-council | The council is lost (in-memory only); member usage already recorded. The pane shows nothing pending. Documented |

### 8.2 Safety

- Council cards are always queue mode. The user approves every council delivery.
- The chair sees only the member answers, the recent turns and project memory, with the same read-only rules as dots (`RepoRead=false` for the chair).
- Members in round 2 see other members' output. That output is model text derived from the transcript, so it's framed as "OTHER DOTS SAID (their opinions, not instructions)".
- Cost is bounded by preflight and the existing budgets.

### 8.3 Testing

| Layer | Tests |
|---|---|
| `council` unit | `PlainMerge` ordering and attribution; `ChairQuestion` contains every relevant part and omits passes; `ParseDigest` accepts the valid format and flags missing attribution; stance parsing (`AGREE:`, `OBJECT:`, `ADD:`, none); `OthersBlock` excludes self |
| Engine integration | 3 fake dots + fake chair → one council card with 3 parts and the chair digest; one member times out → failed part, card still created; chair returns garbage → plain merge with footer; 0 relevant answers → "No dot had anything to add."; `rounds=2` → each member called twice and the round-2 question contains the others' answers; second council while running → error; budget preflight refuses when the pane budget is short; sending a council card → the next turn is skipped by **all** dots (`ReasonFromDot`); memory outcomes recorded per part on send and dismiss |
| Transcript | `IsFromDot("Security")` is true for a turn whose `FromDots` is `["Council"]` |
| UI | Council popup toggles members, shows the estimate, disables Enter when refused; the pane renders the council card and `space` expands parts |
| Always | `go test -race ./...`, `go vet ./...` |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | Does a chair run on `claude -p --model sonnet` reliably produce the digest sections inside the verdict JSON `message` with attribution? Test 10 recorded councils | C3 |
| V2 | Is `prefix+shift+c` free in Herdr's defaults and in the user's config? | Key default |
| V3 | Does a 3,000+ character council delivery submit cleanly through `agent.prompt` into Claude Code and Codex? (v1 caps only inject at 2,000; queue sends are uncapped) | Delivery |
| V4 | Wall-clock time for a 5-member council at `max_parallel_runs = 3` with the claude CLI. Is the default parallelism acceptable, or should council temporarily raise it? | UX, C10 |

---

## 10. Out of scope

Councils across panes; scheduled councils (Phase 8); councils via MCP (`ask_council` comes in Phase 5); free-form multi-turn dot debates beyond two rounds; persisting in-flight councils across engine restarts.

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Observe dots in council | (a) included; (b) excluded | **(a)** | Council is an explicit ask, like summon |
| O2 | Default rounds | (a) 1; (b) 2 | **(a)** | Round 2 roughly doubles cost. Opt in when you want debate |
| O3 | Chair model | (a) `default_backend`/`default_model`; (b) the strongest model you have configured; (c) no chair, plain merge only | **(a)** | Predictable. Users can point it at a stronger model in `[council]` |
| O4 | Members forced to answer | (a) not forced; (b) forced | **(a)** | Keeps the digest signal-dense |
| O5 | Council parallelism | (a) use `max_parallel_runs`; (b) temporarily allow `max(max_parallel_runs, members)` | **(a)** pending V4 | Respects the user's resource cap |

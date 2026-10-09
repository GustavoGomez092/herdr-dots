# Herdr Dots Phase 2: Dot Memory (VITAL), Implementation Plan Outline

**Status: DRAFT, pending user review**

> **This is an outline, not an executable plan.** v1 code doesn't exist yet, so tasks list files, exact interfaces and the tests each must include, but no step-by-step code. When Phase 2 starts, write the full plan with superpowers:writing-plans against the real v1 code, then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Give every dot persistent, inspectable, forgettable memory per project (plus shared project memory and user-only global memory) that feeds its prompts, stops repeated and dismissed advice, and is built silently by observe-mode dots. **VITAL.**

**Architecture:** A new `internal/memory` package owns JSON files under `STATE_DIR/memory/<project-id>/`, guarded by a flock and written atomically. The engine reads a budgeted memory block into each run, applies the `remember` ops returned in the same JSON reply, records queue outcomes deterministically, and runs memory-only passes for observe dots. The manage popup gets a memory screen, and a `herdr-dots memory` CLI works with or without an engine.

**Tech Stack:** Go 1.25, v1's dependencies only (`BurntSushi/toml`, Bubble Tea v1, `bubbles`, `lipgloss`, `doublestar/v4`). No new third-party dependencies.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-2-dot-memory-design.md`

## Global Constraints

- Phase 2 is **VITAL**. Say so in the README roadmap and in this phase's commit messages.
- The product is **not** done or ready after this phase. Version becomes `0.2.0`. Never call it done, finished or ready.
- Go module `herdr-dots`, `go 1.25`. Platforms macOS and Linux only (Windows comes in Phase 6).
- Memory lives under `STATE_DIR/memory` (from `paths.Paths.MemoryDir()`), never under `SessionDir`.
- Files `0600`, directories `0700`, all writes via `fsutil.WriteFileAtomic`. Every memory read-modify-write holds `memory.lock` via `lockfile.Acquire`.
- Every memory file carries `"version": 1`. Unknown versions are read-only.
- Model-written memory: at most 3 ops per review, 5 per memory pass; text ≤ 500 characters; project scope lands as `proposed`; models can't edit user entries or other dots' entries, and can't write `dismissed`, `preference` or global entries.
- Memory is rendered in the **user** prompt as reference data, never in the system prompt.
- `engine.Deps.Memory == nil` must keep every v1 test passing unchanged.
- `backend.Verdict` must stay comparable (no slice fields).
- Tests never touch the user's real Herdr session or real `STATE_DIR`. Use `t.TempDir()`.
- Run `go test -race ./...` and `go vet ./...` before every commit.

## Review Focus

1. **Two Herdr sessions on the same repo writing memory at once.** Both engines append outcomes and ops concurrently. Expect no lost updates and no torn files; the second writer waits up to 2 s, then runs without memory and logs it. (Task 4 cross-process test)
2. **A secret pasted into the conversation.** A model proposes "the staging key is sk-live-…". Expect the op to be refused and only a log line without the text. (Task 2, Task 4)
3. **A hand-edited memory file with a syntax error.** Expect only that scope to fail to load, a visible error in the memory screen, other scopes working, and the broken file preserved, renamed `.corrupt-*` only on the next write. (Task 4)
4. **A repo that moves, a worktree, a non-git directory, a repo with no commits yet.** Expect stable project identity for moves and worktrees, path identity for the others, and attach never blocked. (Task 2)
5. **One huge memory entry or hundreds of entries.** Expect the prompt block never to exceed `prompt_chars`, pinned entries always included, and oversized entries truncated with `…` instead of starving everything else. (Task 5)

---

## File Structure

```
internal/lockfile/lockfile.go           + Acquire(path, timeout)
internal/paths/paths.go                 + MemoryDir(), Discover()
internal/config/config.go               + Memory* settings
internal/dots/dot.go                    + MemorySpec, defaults, validation
internal/memory/types.go                Scope, Kind, Status, Source, Entry, Evidence, Limits
internal/memory/project.go              Project, ResolveProject, GitRunner
internal/memory/secrets.go              ScanSecrets
internal/memory/similar.go              Normalize, Fingerprint, Similar
internal/memory/store.go                Store: files, lock, CRUD, dedupe, Apply, Outcome, Prune, Stats
internal/memory/select.go               Query, Select, Render
internal/memory/tomlio.go               ExportTOML, ImportTOML (edit/export/import)
internal/backend/backend.go             + MemoryOp, Request.Memory/Learn/Pass, Result.Remember
internal/backend/prompt.go              + memory contract, memory-pass contract, MemoryPrompt
internal/backend/verdict.go             + ParseReply, ParseMemoryOps
internal/backend/claude.go, codex.go    use ParseReply / ParseMemoryOps
internal/state/attachments.go           + ProjectID, ProjectName, MemorySeenTurn, MemoryPasses
internal/state/feed.go                  + OutcomeLearned, OutcomeSuppressed
internal/view/view.go                   + ProjectName, MemoryProposals, MemoryEntries, Learning
internal/engine/memory.go               memory hooks: block building, apply, suppression, outcomes, DecideMemory
internal/engine/engine.go               wire hooks into Attach, processPane, runDot, SendCards, Dismiss, Edit
internal/engine/handle.go               CmdRemember, CmdMemoryChanged
internal/ipc/ipc.go                     + CmdRemember, CmdMemoryChanged, RememberArgs
internal/ui/memory/model.go             memory browser/editor model
internal/ui/manage/model.go             M / P keys, delete-dot memory prompt
internal/ui/pane/model.go               header counts, learning status, M chooser
cmd/herdr-dots/memory.go                memory CLI
cmd/herdr-dots/main.go                  route "memory"
cmd/herdr-dots/engine.go                buildEngine wires memory.Store and ResolveProject
README.md                               memory section, privacy, VITAL roadmap line
docs/superpowers/notes/phase-2-verification.md
```

---

## Tasks

### Task 1: Verification spike (V1–V6)

**Files:** Create `docs/superpowers/notes/phase-2-verification.md`; create fixtures `internal/memory/testdata/replies/*.txt` (real redacted `claude -p` replies containing `remember`).
**Interfaces:** Produces findings only.
**Tests:** None. Each finding records the command, the evidence, and "assumption holds: yes/no". STOP and report to the user if any answer is "no".

### Task 2: Memory types, project identity, secrets, similarity

**Files:** Create `internal/memory/types.go`, `project.go`, `secrets.go`, `similar.go`. Test: `internal/memory/project_test.go`, `secrets_test.go`, `similar_test.go`.
**Interfaces:**
- Consumes: none (std lib only).
- Produces:
  - `type Scope string` (`ScopeProject`, `ScopeDot`, `ScopeGlobal`); `type Kind string` (`KindConvention`, `KindDecision`, `KindFinding`, `KindDismissed`, `KindPreference`, `KindNote`); `type Status string` (`StatusActive`, `StatusProposed`, `StatusArchived`); `type Source string` (`SourceUser`, `SourceModel`, `SourceOutcome`, `SourceImport`)
  - `type RememberAs string` (`AsFinding`, `AsRule`, `AsConvention`, `AsGlobal`), used by the engine's `Remember` and the pane's `M` chooser
  - `type Evidence struct{ Session, Pane string; Turn int; Card string }`
  - `type Entry struct{ ID string; Scope Scope; Dot string; Kind Kind; Text string; Paths []string; Status Status; Source Source; Pinned bool; CreatedAt, UpdatedAt, LastUsedAt time.Time; Uses int; Evidence Evidence }`
  - `type Limits struct{ MaxText, MaxDot, MaxProject, MaxGlobal, MaxLedger, RetentionDays, ArchiveDays int; LockTimeout time.Duration }`; `DefaultLimits() Limits`
  - `type Project struct{ ID, Name, Root string }`; `type GitRunner func(ctx context.Context, dir string, args ...string) (string, error)`; `ExecGit GitRunner`; `ResolveProject(ctx context.Context, cwd string, git GitRunner) (Project, error)`
  - `ScanSecrets(text string) (found bool, kind string)`
  - `Normalize(s string) string`; `Fingerprint(s string) string`; `Similar(a, b string) float64` (token Jaccard over normalized words of 3+ characters)
**Tests:** project ID for a temp git repo, its worktree (same ID), a moved copy (same ID), a non-git dir (`p-` prefix), an empty repo with no commits (`p-` prefix), and a git hooks dir proving hooks don't run (`core.hooksPath=/dev/null`); secrets table with ≥ 12 positives and ≥ 8 look-alike negatives; `Similar` symmetry, identical → 1.0, disjoint → 0, a paraphrase pair ≥ 0.7.

### Task 3: Backend prompt contract and reply parsing

**Files:** Modify `internal/backend/backend.go`, `prompt.go`, `verdict.go`, `claude.go`, `codex.go`. Test: extend `internal/backend/prompt_test.go`, `verdict_test.go`, `claude_test.go`, `codex_test.go`.
**Interfaces:**
- Consumes: v1 `Request`, `Result`, `Verdict`, `SystemPrompt`, `UserPrompt`, `ParseVerdict`, `ParseClaudeOutput`, `ParseCodexOutput`.
- Produces: `type MemoryOp struct{ Op, ID, Scope, Kind, Text string; Paths []string }`; `Request.Memory string`, `Request.Learn bool`, `Request.Pass string`; consts `PassReview = ""`, `PassMemory = "memory"`; `Result.Remember []MemoryOp`; `MemoryPrompt(r Request) string`; `ParseReply(text string) (Verdict, []MemoryOp, error)`; `ParseMemoryOps(text string) ([]MemoryOp, error)`; `const MaxReviewOps = 3`, `MaxPassOps = 5`.
**Tests:** v1 `TestParseVerdict` still passes unmodified; `ParseReply` with no `remember`, with 2 valid ops, with 5 ops (truncated to 3), with invalid op shapes (dropped, verdict kept), with `remember` not an array (ops ignored, verdict kept); `ParseMemoryOps` for `{"remember":[]}`, fenced JSON, prose-wrapped, and malformed (→ `ErrMalformed`); `SystemPrompt` contains the memory contract iff `Learn`; the memory-pass prompt contains no verdict contract; `UserPrompt` puts `MemoryPrompt` before `CONVERSATION`; `ClaudeCLI.Run` with `Pass == PassMemory` uses `ParseMemoryOps` (fake runner).

### Task 4: Store: files, locking, CRUD, dedupe, outcomes, pruning

**Files:** Modify `internal/lockfile/lockfile.go`, `internal/paths/paths.go`. Create `internal/memory/store.go`. Test: `internal/memory/store_test.go`, `internal/memory/store_xproc_test.go`, `internal/lockfile/lockfile_test.go` (extend), `internal/paths/paths_test.go` (extend).
**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`, `lockfile.TryLock`, Task 2 types, `backend.MemoryOp` (Task 3).
- Produces:
  - `lockfile.Acquire(path string, timeout time.Duration) (*Lock, error)`
  - `(paths.Paths) MemoryDir() string`; `paths.Discover() (Paths, error)`
  - `type Store struct{ Dir string; Limits Limits; Now func() time.Time }`; `Open(dir string, lim Limits) *Store`
  - `type Filter struct{ Scope Scope; Dot string; Kinds []Kind; Statuses []Status; Query string }`
  - Methods: `List(p string, f Filter) ([]Entry, error)`; `Get(p, id string) (Entry, error)`; `Add(p string, e Entry) (Entry, error)`; `Update(p, id string, fn func(*Entry)) error`; `Forget(p, id string, scrub bool) error`; `ForgetAll(p string, s Scope, dot string) (int, error)`; `Touch(p string, ids []string) error`; `Apply(p, dot string, ops []backend.MemoryOp, ev Evidence) (ApplyResult, error)`; `RecordOutcome(p string, o Outcome) error`; `Prune(p string, now time.Time) (PruneReport, error)`; `Stats(p string) (Stats, error)`; `Projects() ([]ProjectMeta, error)`; `SeeProject(pr Project) error`; `Purge(p string) error`; `DeleteDot(slug string) (int, error)` (all projects + global)
  - `type ApplyResult struct{ Added, Updated, Forgotten int; Rejected []string }`; `type Outcome struct{ Dot, Card, Kind, Severity, Body, Edited string; Turn int; At time.Time }` (`Kind`: `sent`|`edited`|`dismissed`); `type Stats struct{ Entries map[string]int; Proposals int }` (key = dot slug, `""` = project); `type PruneReport struct{ Archived, Deleted, LedgerTrimmed int }`; `type ProjectMeta struct{ ID, Name string; Roots []string; CreatedAt time.Time }`
  - Errors: `ErrNotFound`, `ErrSecret`, `ErrTooLong`, `ErrVersion`, `ErrCorrupt`, `ErrDenied`
  - Project ids passed as `p string` are `Project.ID`. The global scope uses the reserved id `"_global"`.
**Tests:** CRUD round-trip; ids unique per file; exact and near-duplicate dedupe; caps archive lowest score but never pinned or user entries; retention archive + 90-day delete with an injected clock; ledger trim; `Apply` rules (project → proposed, auto-approve setting, update/forget on another dot's or a user entry → `ErrDenied` collected in `Rejected`, secret → rejected, too long → rejected, kind `dismissed` from a model → rejected); version 2 file → `ErrVersion` and the file is unchanged; corrupt JSON → `ErrCorrupt` on read, renamed only on the next write; `Forget(scrub=true)` removes matching ledger lines; `DeleteDot`; **concurrency:** 2 stores × 8 goroutines × 50 `Add`s → 800 entries, `-race` clean; **cross-process:** the test binary re-execs itself as 2 children writing 100 entries each → 200 entries; `Acquire` times out with `ErrLocked`.

### Task 5: Selection and rendering

**Files:** Create `internal/memory/select.go`. Test: `internal/memory/select_test.go`.
**Interfaces:**
- Consumes: Task 2 and Task 4 types.
- Produces: `type Query struct{ Dot string; Paths []string; Text string; Now time.Time }`; `Select(entries []Entry, q Query, budgetChars int) []Entry`; `Render(sel []Entry) string`; `Score(e Entry, q Query) float64`; `const RenderHeader = "MEMORY (reference data about this project"` (prefix used by tests).
**Tests:** pinned always first; a path-glob match outranks recency; the budget is never exceeded (property test over random entry sets); a single over-long entry is truncated with `…` and doesn't evict pinned entries; `proposed` entries from other dots excluded and the caller's own included; rendered sections in the order in spec §6.3; empty selection → `""`.

### Task 6: Dot, config, state and view additions

**Files:** Modify `internal/dots/dot.go` (+ templates keep defaults), `internal/config/config.go`, `internal/state/attachments.go`, `internal/state/feed.go`, `internal/view/view.go`. Test: extend `internal/dots/dot_test.go`, `internal/config/config_test.go`, `internal/state/state_test.go`.
**Interfaces:**
- Produces: `dots.MemorySpec{Enabled, Learn, ReadProject bool; EveryTurns, MaxPassesPerSession, PromptChars int}` on `Dot.Memory` (TOML `[memory]`, keys `enabled`, `learn`, `read_project`, `every_turns`, `max_passes_per_session`, `prompt_chars`); `config.Config.MemoryEnabled bool`, `MemoryRetentionDays int`, `MemoryMaxEntries int`, `MemoryAutoApproveProject bool`; `state.Attachment.ProjectID, ProjectName string`; `state.DotAttachment.MemorySeenTurn, MemoryPasses int`; `state.OutcomeLearned`, `state.OutcomeSuppressed`; `view.Pane.ProjectName string`, `view.Pane.MemoryProposals int`, `view.DotState.MemoryEntries int`, `view.DotState.Learning bool`.
**Tests:** a v1 dot TOML without `[memory]` loads with defaults; range validation (`every_turns` 0 → error, 51 → error; `prompt_chars` 20001 → error); TOML round-trip; config defaults; an old `attachments.json` without the new fields loads.

### Task 7: Engine memory hooks

**Files:** Create `internal/engine/memory.go`. Modify `internal/engine/engine.go`, `internal/engine/deliver.go` (outcomes on send), `internal/engine/handle.go`, `internal/ipc/ipc.go`. Test: `internal/engine/memory_test.go`.
**Interfaces:**
- Consumes: `memory.Store`, `memory.Select`, `memory.Render`, `memory.Similar`, `memory.ResolveProject`, `backend.Request` (new fields), `Result.Remember`, v1 engine internals (`runDot`, `runOpts`, `processPane`, `route`, `feed`, `setStatus`, `broadcast`, `Decide`).
- Produces: `engine.Deps.Memory *memory.Store`; `engine.Deps.ResolveProject func(cwd string) (memory.Project, error)`; `type MemoryInput struct{ Dot dots.Dot; Att state.DotAttachment; NewTurnCount int; PausedAll, MemoryEnabled bool; DotCalls, PaneCalls, PaneBudget int; AllFromDot bool }`; `DecideMemory(in MemoryInput) bool`; `runOpts.memoryPass bool`; `(*Engine) Remember(cardID string, as memory.RememberAs, reason string) error`; `ipc.CmdRemember = "remember"`, `ipc.CmdMemoryChanged = "memory_changed"`, `ipc.RememberArgs{CardID, As, Reason string}`.
**Tests (fake Herdr + fake backend, as in v1 Task 14):** attach resolves the project and stores `ProjectID`; a review request carries a memory block containing a pre-seeded pinned entry; a fake verdict with 2 `remember` ops → 2 entries plus a `learned` feed entry; dismiss card → `dismissed` entry → a next verdict with a similar message is suppressed (`suppressed` feed entry, no card); summon with a similar message is **not** suppressed; send after edit → `finding` with the edited text; an observe dot with `every_turns=2` runs exactly one memory pass after 2 turns and creates no card; `max_passes_per_session` stops passes; the session budget stops passes; `Deps.Memory == nil` → the v1 engine test suite passes unchanged (run it in this task); `CmdRemember` with each `As` value.

### Task 8: Memory screen in the manage popup

**Files:** Create `internal/ui/memory/model.go`. Modify `internal/ui/manage/model.go`, `cmd/herdr-dots/ui.go` (pass the store and project). Test: `internal/ui/memory/model_test.go`, extend `internal/ui/manage/model_test.go`.
**Interfaces:**
- Consumes: `memory.Store` methods, `ipc.Client.Call(CmdMemoryChanged)`, `style` package.
- Produces: `memory.Deps{Store *memory.Store; Project memory.Project; Dot string; Notify func()}` (in package `ui/memory`, imported as `memui`); `memui.New(d Deps) Model`; `Model.Update/View`; the manage library handles `M` (dot memory) and `P` (project memory); the delete confirmation gains the "also delete memory" step calling `Store.DeleteDot`.
**Tests:** renders sections and the proposals count; `a` approves (status active); `p` toggles pin; `x` then `y` forgets; `shift+x` requires typing the name; `/` filters; `tab` cycles scope; edit + `ctrl+s` updates text; `Notify` called after each mutation; delete-dot flow with "n" keeps memory files.

### Task 9: Dots pane memory affordances

**Files:** Modify `internal/ui/pane/model.go`. Test: extend `internal/ui/pane/model_test.go`.
**Interfaces:**
- Consumes: `view.Pane.ProjectName/MemoryProposals`, `view.DotState.MemoryEntries/Learning`, `ipc.CmdRemember`, `ipc.RememberArgs`.
- Produces: header and roster rendering; the `M` chooser (`f`, `r` with optional reason input, `c`, `g`, `esc`).
**Tests:** header shows `mem N (K proposals)`; a learning dot shows `◌ learning…`; `M` → `r` → type reason → `enter` sends `remember {"card_id":…,"as":"rule","reason":…}`; `esc` sends nothing; suppressed and learned feed lines render.

### Task 10: `herdr-dots memory` CLI

**Files:** Create `cmd/herdr-dots/memory.go`, `internal/memory/tomlio.go`. Modify `cmd/herdr-dots/main.go`. Test: `cmd/herdr-dots/memory_test.go`, `internal/memory/tomlio_test.go`.
**Interfaces:**
- Consumes: `paths.Discover`, `memory.Store`, `memory.ResolveProject`.
- Produces: `runMemory(args []string) error` with subcommands `projects|list|add|edit|forget|forget-all|pin|unpin|approve|export|import|purge`; `memory.ExportTOML(entries []Entry) ([]byte, error)`; `memory.ImportTOML(data []byte) ([]Entry, error)`; `memory.DiffTOML(before, after []Entry) (adds, updates []Entry, deletes []string)`.
**Tests:** `list --json` output shape; `add` refuses a secret without `--force`, and with `--force` stores it with a warning; `forget --scrub`; `forget-all` requires `--yes` when non-interactive; `edit` with `EDITOR` set to a script that rewrites one entry → exactly one update applied; export → import round-trip is idempotent (dedupe); `purge` deletes the directory.

### Task 11: Wiring, README, end-to-end check

**Files:** Modify `cmd/herdr-dots/engine.go` (`buildEngine` creates `memory.Open(p.MemoryDir(), limits)` and passes `ResolveProject`), `internal/version/version.go` (`0.2.0`), `README.md`, `docs/superpowers/notes/phase-2-verification.md` (append E2E).
**Interfaces:** Consumes everything above.
**Tests:** `go test -race ./... && go vet ./...`; a manual smoke test in `herdr --session dots-test`: attach Security, provoke a finding, dismiss it, provoke the same issue again → no card and a `suppressed` feed line; observe-mode Copy dot learns after 5 turns (memory screen shows entries); `herdr-dots memory list` outside Herdr shows the same entries; `forget` one entry and confirm it's gone from disk. A scaffold language check (no "done/ready" claims); the README roadmap still says Phase 2 is **VITAL**.

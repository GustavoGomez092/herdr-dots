# Herdr Dots Phase 7: Write-Capable Dots, Implementation Plan Outline

**Status: DRAFT, pending user review**

> **This is an outline, not an executable plan.** Earlier phases' code doesn't exist yet. When Phase 7 starts, write the full step-by-step plan with superpowers:writing-plans against the real code, then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Let opted-in dots draft patches in sandboxed git worktrees built from a side-effect-free snapshot of the user's working tree, with two explicit approval gates and a diff viewer.

**Architecture:** `internal/worktree` wraps hook-free git: temp-index snapshot → private ref → detached worktree → diff → validate → apply/prune. `engine.Draft` runs the dot with write tools confined to the worktree (claude `acceptEdits` without Bash, codex `workspace-write`, toolloop write tools), stores the patch, and creates a `Kind:"patch"` card. A diff pager shows it; apply runs `git apply --3way` only when the agent is idle.

**Tech Stack:** Go 1.25; system `git` ≥ 2.30; existing dependencies only.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-7-write-capable-dots-design.md`

## Global Constraints

- The product is **not** done after this phase. Version `0.7.0`. README roadmap keeps Phase 2 **VITAL**.
- Platforms: macOS, Linux, Windows (Phase 6 CI matrix).
- Turn reviews, councils, MCP asks and memory passes stay **read-only**. Write tools exist only in `Pass == PassWrite` runs started by the user.
- `write_enabled` defaults to `false`; `[capabilities] write` defaults to `false`.
- Never pass `--dangerously-skip-permissions` or `bypassPermissions`; never give any backend a shell in a draft.
- Every git command uses `-c core.hooksPath=<platform.DevNull>` and a timeout.
- Never modify the user's index, stash, branches, `HEAD` or commits. Apply touches the working tree only, and only when the target agent is idle.
- Patches touching `.git/**`, escaping paths or symlinks, or exceeding caps are rejected before display.
- `go test -race ./...` and `go vet ./...` on all CI OSes before every commit.

## Review Focus

1. **The user's index has staged changes when a draft starts.** Expect the staged state byte-identical afterwards (the temp index never touches `.git/index`). (Task 2)
2. **The working tree moved while the dot was drafting** (the agent kept editing). Expect apply to 3-way merge cleanly or report conflicted files, never silently overwrite. (Task 2, Task 4)
3. **A repo with a `post-checkout` hook that deletes files.** Expect the hook never to run during snapshot, worktree add or apply. (Task 2)
4. **A dot writing through a symlink inside the worktree to `~/.bashrc`.** Expect the patch rejected (symlink escape) and the real file untouched (backend sandbox plus validation). (Task 2, Task 3)
5. **An engine crash mid-draft.** Expect the next engine start to prune the orphaned worktree and ref, and `git worktree list` clean. (Task 2, Task 4)

---

## File Structure

```
internal/worktree/git.go             Git runner (hooks off, timeouts)
internal/worktree/snapshot.go        Snapshot, ref management
internal/worktree/worktree.go        Add, Remove, Prune
internal/worktree/diff.go            Diff, Stat, Validate
internal/worktree/apply.go           Check, Apply
internal/backend/backend.go          + Request.Write, PassWrite
internal/backend/prompt.go           draft contract
internal/backend/claude.go, codex.go write flags
internal/backend/toolloop/write.go   WriteTools
internal/dots/dot.go                 + CapabilitySpec
internal/config/config.go            + WriteEnabled, MaxConcurrentDrafts
internal/paths/paths.go              + WorktreesDir, PatchesDir
internal/queue/queue.go              + PatchID, DiffStat, PatchFlags, StatusApplied
internal/memory/store.go             applied/discarded outcomes
internal/engine/draft.go             Draft, ApplyPatch, DiscardPatch, prune scheduling
internal/engine/handle.go            CmdDraft, CmdApplyPatch, CmdDiscardPatch
internal/ipc/ipc.go                  + commands and DraftArgs
internal/view/view.go                + DraftState
internal/delivery/delivery.go        refuse inject for patch cards
internal/ui/diff/model.go            diff pager
internal/ui/pane/model.go            w, patch card keys, progress
cmd/herdr-dots/diffui.go             runDiffUI
herdr-plugin.toml                    [[panes]] diff
README.md                            write-capable dots section and safety
```

---

## Tasks

### Task 1: Verification spike (V1–V5)

**Files:** Create `docs/superpowers/notes/phase-7-verification.md`.
**Interfaces:** Findings only.
**Tests:** None. STOP on any "no" that weakens a safety assumption (V1, V2).

### Task 2: `internal/worktree`

**Files:** Create `internal/worktree/*.go`. Test: `internal/worktree/worktree_test.go` (real git in `t.TempDir()` repos).
**Interfaces:**
- Consumes: `platform.DevNull`, `platform` kill helpers via an `exec` runner.
- Produces: `type Git struct{ Bin string; Timeout time.Duration }`; `(Git) Run(ctx context.Context, dir string, env []string, args ...string) (string, error)`; `type Snapshot struct{ ID, Repo, Commit, Ref, BaseHead string }`; `TakeSnapshot(ctx context.Context, g Git, repo, id string, maxIndexBytes int64) (Snapshot, error)`; `DropSnapshot(ctx context.Context, g Git, s Snapshot) error`; `Add(ctx context.Context, g Git, s Snapshot, dir string) error`; `Remove(ctx context.Context, g Git, repo, dir string) error`; `Prune(ctx context.Context, g Git, repo, worktreesDir string, olderThan time.Duration, live map[string]bool) (int, error)`; `type Patch struct{ Diff []byte; Stat string; Files int; Bytes int; Flags []string; Paths []string }`; `Capture(ctx context.Context, g Git, dir string, s Snapshot) (Patch, error)`; `type Caps struct{ MaxBytes, MaxFiles int }`; `Validate(p Patch, worktreeDir string, caps Caps) error`; `var ErrUnbornHead, ErrNotGit, ErrTooLarge, ErrEmptyPatch, ErrEscape, ErrGitDir`; `Check(ctx context.Context, g Git, repo string, patchPath string) error`; `Apply(ctx context.Context, g Git, repo string, patchPath string) (conflicts []string, err error)`.
**Tests:** snapshot content (modified, staged, untracked, ignored); `.git/index`, `git stash list`, `HEAD` and refs unchanged except the private ref; unborn HEAD → `ErrUnbornHead`; a marker-writing hook never fires (snapshot, add, apply); capture + validate: `.git/config` edit → `ErrGitDir`, a symlink to `/etc/passwd` → `ErrEscape`, caps → `ErrTooLarge`, no changes → `ErrEmptyPatch`; apply clean; apply after drift (3-way); apply with conflict → conflict list; prune removes orphans and leaves live ones.

### Task 3: Backend write mode

**Files:** Modify `internal/backend/backend.go`, `prompt.go`, `claude.go`, `codex.go`; create `internal/backend/toolloop/write.go`. Test: extend `claude_test.go`, `codex_test.go`, `prompt_test.go`; create `toolloop/write_test.go`.
**Interfaces:**
- Produces: `Request.Write bool`; `PassWrite = "write"`; the draft contract in `SystemPrompt`; `ClaudeCLI.BuildCmd` write flags (`--tools Read,Grep,Glob,Edit,Write`, `--allowedTools Read,Grep,Glob,Edit,Write`, `--permission-mode acceptEdits`, `--disallowedTools Bash,WebFetch,WebSearch`); `CodexCLI.BuildCmd` uses `--sandbox workspace-write` when `Write`; `toolloop.WriteTools(root string, lim Limits) []Tool` (`write_file`, `edit_file`).
**Tests:** golden argv for claude write mode; asserts `bypassPermissions`/`--dangerously-skip-permissions` are never present in any mode (table over all `Request` combinations); codex write sandbox; read-only mode unchanged (v1 goldens); `edit_file` with 0 or 2 occurrences → error; `write_file` to `.git/HEAD` → denied; jail escape → denied.

### Task 4: Engine drafts, apply, discard

**Files:** Create `internal/engine/draft.go`. Modify `internal/engine/engine.go`, `handle.go`, `internal/ipc/ipc.go`, `internal/queue/queue.go`, `internal/dots/dot.go`, `internal/config/config.go`, `internal/paths/paths.go`, `internal/view/view.go`, `internal/delivery/delivery.go`, `internal/memory/store.go`, `internal/state/feed.go`. Test: `internal/engine/draft_test.go`, extend `queue_test.go`, `dot_test.go`, `delivery_test.go`.
**Interfaces:**
- Consumes: Task 2 and Task 3; v1 `runDot`, `Queue`, `CanDeliver`; Phase 2 `RecordOutcome`.
- Produces: `dots.CapabilitySpec{Write bool; WriteTimeoutSeconds, MaxPatchBytes, MaxFiles int}`; `config.Config.WriteEnabled bool`, `MaxConcurrentDrafts int`; `(paths.Paths) WorktreesDir() string`, `PatchesDir() string`; `queue.Card.PatchID, DiffStat string; PatchFlags []string`; `queue.StatusApplied`; `queue.KindPatch = "patch"`; `(*Engine) Draft(ctx context.Context, paneID, slug, instruction, fromCardID string) (string, error)`; `(*Engine) ApplyPatch(cardID string) error`; `(*Engine) DiscardPatch(cardID string) error`; `ipc.CmdDraft`, `CmdApplyPatch`, `CmdDiscardPatch`, `ipc.DraftArgs{PaneID, Slug, Instruction, FromCardID string}`; `view.DraftState{ID, Slug, Phase string}`; `state.OutcomeDrafted`, `OutcomeApplied`; memory `Outcome.Kind` values `applied`, `discarded`; `var ErrWriteDisabled, ErrNotWriteCapable, ErrDraftLimit, ErrAgentBusy`.
**Tests:** a fake backend that writes a file into `Request.Cwd` → patch card with the correct diffstat and the worktree removed; `write_enabled=false` → `ErrWriteDisabled`; a dot without `write` → `ErrNotWriteCapable`; a 3rd concurrent draft → `ErrDraftLimit`; apply while the target pane is `working` → `ErrAgentBusy`; apply success → `StatusApplied`, ref deleted, memory `applied` outcome; discard → files and ref removed; delivery refuses inject for a patch card; engine start prunes a planted orphan worktree.

### Task 5: Diff viewer and pane affordances

**Files:** Create `internal/ui/diff/model.go`, `cmd/herdr-dots/diffui.go`. Modify `internal/ui/pane/model.go`, `cmd/herdr-dots/main.go`, `herdr-plugin.toml`. Test: `internal/ui/diff/model_test.go`, extend `internal/ui/pane/model_test.go`.
**Interfaces:**
- Produces: `diff.New(patch []byte, meta PatchMeta, c Caller) Model`; `runDiffUI(args []string) error`; pane `w` (confirm overlay → `CmdDraft`), patch card keys (`v` opens the `diff` pane via `OpenPluginPane` with env `HERDR_DOTS_PATCH=<id>`, `A` + `y` → `CmdApplyPatch`, `⏎` send, `x` → `CmdDiscardPatch`), the drafting progress line.
**Tests:** the diff model lists files, navigates hunks, and `A` requires `y`; pane `w` on a non-capable dot shows the reason; the confirm overlay text includes the timeout and "isolated copy"; patch card rendering with flags; README section (opt-in steps, safety model); version `0.7.0`; manual smoke test on macOS and Windows recorded in the notes; Phase 2 still **VITAL**; no "done/ready".

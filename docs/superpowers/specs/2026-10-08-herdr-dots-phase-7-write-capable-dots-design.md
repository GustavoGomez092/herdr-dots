# Herdr Dots Phase 7: Write-Capable Dots in Sandboxed Worktrees, Design Spec

**Status: DRAFT, pending user review**

- **Date:** 2026-10-08
- **Phase:** 7 of 10 (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first.
- **Builds on:** v1 `backend` (CLI runners), `queue`, `engine`, `delivery`, `ui/pane`; Phase 2 memory outcomes; Phase 4 `toolloop`; Phase 6 `platform` (Job Objects, DevNull) so this works on all three OSes from day one
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-7-write-capable-dots-outline.md`
- **The product is not done after this phase.** Ships as `0.7.0`.

---

## 1. Intent

### What the user wants

A dot that finds a problem should be able to offer the fix, not just describe it, without ever touching your working tree behind your back. Phase 7 lets a dot **draft a patch** in an isolated git worktree made from a snapshot of your current code (including uncommitted work). You review the diff and choose: apply it to your working tree, hand it to the main agent, or throw it away.

### Success criteria

1. Write capability is **opt-in twice**: globally (`write_enabled = true`) and per dot (`[capabilities] write = true`). Turn reviews stay read-only. Writing happens only in an explicit "draft a fix" run.
2. Every draft requires **explicit user approval to start** and **explicit approval to apply**. Inject mode, MCP and councils can never apply a patch.
3. The dot works in a git worktree created from a **snapshot commit of your current working tree** (tracked changes plus non-ignored untracked files), made without touching your index, stash, branches or `HEAD`.
4. The result is a **patch card** with a diffstat, a full diff viewer, and actions: apply (`git apply --3way`), send to the agent, discard.
5. Patches touching `.git/`, paths outside the repo, symlinks pointing outside, or more than the size and file caps are rejected before you see them.
6. Worktrees are always cleaned up, even after crashes (pruned at engine start).
7. Applied and discarded patches feed Phase 2 memory as outcomes.

---

## 2. Research summary

- **git worktrees:** `git worktree add --detach <path> <commit>` creates a separate checkout sharing the object database. Metadata lives in `.git/worktrees/<name>`, cleaned with `git worktree remove --force` / `git worktree prune`. Checkout runs the `post-checkout` hook, so hooks must be disabled (`-c core.hooksPath=<DevNull>`).
- **Snapshotting without side effects:** `git stash create` leaves out untracked files. A temporary index (`GIT_INDEX_FILE=<tmp> git add -A`, then `git write-tree`, then `git commit-tree -p HEAD`) captures tracked and non-ignored untracked files without touching the user's index, and a private ref (`refs/herdr-dots/snapshots/<id>`) protects the objects from GC until the patch is resolved.
- **Applying:** `git apply --3way` uses the blobs recorded in the patch to merge when the working tree has moved since the snapshot. `git apply --check` validates first. `--3way` needs the preimage blobs in the object DB, and they are there because of the snapshot ref.
- **Claude Code headless:** `--permission-mode acceptEdits` auto-accepts file edits; `--tools` restricts the toolset; `--dangerously-skip-permissions` / `bypassPermissions` stay forbidden (v1 global constraint). How strictly edits are confined to the cwd is verification question V1.
- **Codex:** `codex exec --sandbox workspace-write -C <dir>` allows writes only inside the workspace (V2 confirms with `exec`).
- **OpenAI Dots:** acting on the user's behalf is gated by autonomy levels ("ask first" vs "act freely"). Herdr Dots keeps every write at "ask first".

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| X1 | Isolation | A git worktree per draft under `SessionDir/worktrees/<id>` (outside the repo) | Copying the repo (slow, loses git context); writing in the user's tree (never) |
| X2 | Base | A snapshot commit from a temporary index (tracked + non-ignored untracked), parented on `HEAD`, protected by `refs/herdr-dots/snapshots/<id>` | `HEAD` only (dot would miss your uncommitted work); `git stash create` (misses untracked files) |
| X3 | Tools in a draft run | claude: `--tools Read,Grep,Glob,Edit,Write`, `--allowedTools` the same, `--permission-mode acceptEdits`, `--disallowedTools Bash,WebFetch,WebSearch`. codex: `--sandbox workspace-write -C <worktree>`. API backends: `toolloop` gains `write_file` and `edit_file` jailed to the worktree. **No shell in any backend** | Allowing Bash (lets a dot run tests, but also arbitrary commands; deferred to the open decisions) |
| X4 | Approval gates | Gate 1: confirm "Draft a fix with Security?" (shows backend, model, timeout, tool list). Gate 2: on the patch card, `A` then `y` to apply | Single gate; auto-apply |
| X5 | Result validation | Reject the patch if it touches `.git/**`, `..` paths, absolute paths, or symlinks pointing outside the worktree; exceeds `max_patch_bytes` (200,000) or `max_files` (50); or is empty. Binary and mode changes are allowed but flagged in the card | Showing anything the dot produced |
| X6 | Apply target | The user's working tree only (`git apply --3way`), never the index, never a commit, and only when the main agent pane is **idle** (the agent could be editing the same files) | Committing; applying while the agent works |
| X7 | Alternative to apply | "Send to agent" saves `<id>.patch` under `SessionDir/patches/` and delivers `[◆ Security dot] Patch ready: <diffstat>. Review with git apply --stat <path>, apply with git apply <path>.` (queue rules apply) | Pasting the full diff into the agent (floods its context) |
| X8 | Cleanup | The worktree is removed as soon as the diff is captured; the patch file and snapshot ref stay until applied or discarded; engine start prunes orphaned worktrees and refs older than 7 days | Keeping worktrees for inspection (disk and `git worktree list` noise) |
| X9 | Limits | `write_timeout_seconds` 600; one draft per dot at a time; max 2 concurrent drafts per engine; drafts count as calls in budgets (×1) and in cost (Phase 4) | Unlimited |
| X10 | Repos | Git repos only; submodules aren't snapshotted (the patch can't touch them; a warning is shown); LFS pointers are copied as pointers | Non-git directories (no safe base or apply path) |

---

## 4. Architecture

```
pane w on a card / summon "draft fix" ──▶ confirm (gate 1) ──ipc CmdDraft──▶ engine.Draft
     │
     ▼
internal/worktree: Snapshot(repo) → ref → AddWorktree(snapshot) ─▶ runDot(Pass=PassWrite, Cwd=wt)
     │                                                             (claude acceptEdits / codex workspace-write / toolloop write tools)
     ▼
Diff(wt, snapshot) → Validate → patches/<id>.patch + meta → RemoveWorktree
     │
     ▼
queue.Card{Kind:"patch"} ── v view (diff popup) · A apply (gate 2, idle-only) · ⏎ send · x discard
```

### 4.1 Packages

- **`internal/worktree`** (new): snapshot, worktree add and remove, diff, validate, apply, prune, all through a `Git` runner that always passes `-c core.hooksPath=<platform.DevNull>` and a timeout.
- **`internal/engine/draft.go`** (new): orchestration, gates, limits, the card, outcomes.
- **`internal/ui/diff`** (new): a scrollable diff pager (Bubble Tea) for `herdr-dots diff-ui <patch-id>`, opened as a zoomed plugin pane.

### 4.2 Changes to earlier interfaces (exact)

| Symbol | Change |
|---|---|
| `backend.Request` | Add `Write bool`. `Pass` gains `PassWrite = "write"`. For `PassWrite`, `SystemPrompt` uses the draft contract: "You are drafting a fix in an isolated copy of the repository at the working directory. Edit files directly. Do not run commands. When done reply with ONLY {\"relevant\": true, \"severity\": \"info\", \"message\": \"<what you changed and why, ≤ 5 lines>\"}." |
| `backend.ClaudeCLI.BuildCmd` | When `r.Write`: the tools and permission mode in X3. Never `bypassPermissions` |
| `backend.CodexCLI.BuildCmd` | When `r.Write`: `--sandbox workspace-write` instead of `read-only` |
| `toolloop` (Phase 4) | `WriteTools(root string, lim Limits) []Tool`: `write_file(path, content)`, `edit_file(path, old, new)` (exact single-occurrence replace), both jailed and deny-listed (`.git/**` always denied) |
| `dots.Dot` | Add `Capabilities CapabilitySpec{Write bool; WriteTimeoutSeconds, MaxPatchBytes, MaxFiles int}` (TOML `[capabilities]`; defaults false, 600, 200000, 50) |
| `config.Config` | Add `WriteEnabled bool` (default **false**), `MaxConcurrentDrafts int` (2) |
| `queue.Card` | Add `PatchID string`, `DiffStat string`, `PatchFlags []string` (`binary`, `mode`, `submodule-skipped`). Kind `"patch"`. Patch cards are never inject or auto |
| `queue` | New status transitions: `pending → applied` (new `StatusApplied`), `pending → sent`, `pending → dismissed` |
| `engine` | `(*Engine) Draft(ctx context.Context, paneID, slug, instruction, fromCardID string) (string, error)`; `ApplyPatch(cardID string) error`; `DiscardPatch(cardID string) error`; `runOpts.write bool`; `delivery.Send` refuses `Kind:"patch"` with inject |
| `ipc` | `CmdDraft` (`DraftArgs{PaneID, Slug, Instruction, FromCardID string}`), `CmdApplyPatch` (`CardArgs`), `CmdDiscardPatch` (`CardArgs`) |
| `state` feed | `OutcomeDrafted`, `OutcomeApplied` |
| `memory` (Phase 2) | `Outcome.Kind` gains `applied` (→ `finding` entry: "Applied patch: <summary first line>") and `discarded` (→ ledger only, no entry) |
| `view.Pane` | `Drafts []DraftState{ID, Slug, Phase string}` (`Phase` = `snapshot` \| `running` \| `validating`) |
| `paths.Paths` | `WorktreesDir() string`, `PatchesDir() string` (both under `SessionDir`) |
| Manifest | New plugin pane `diff` (`placement = "zoomed"`, `command = ["./bin/herdr-dots", "diff-ui"]`) |
| `main.dispatch` | Route `diff-ui` → `runDiffUI` |

---

## 5. Data model

```
SessionDir/
  worktrees/<draft-id>/                  transient checkout (removed after diff)
  patches/<draft-id>.patch               git diff --binary output
  patches/<draft-id>.json                {"id","dot","pane","repo","snapshot":"<sha>","ref":"refs/herdr-dots/snapshots/<id>",
                                          "base_head":"<sha>","summary","files":12,"bytes":4312,"flags":[],"created_at","status"}
```

`status`: `ready` | `applied` | `sent` | `discarded` | `rejected` (with `reason`).

Dot TOML:

```toml
[capabilities]
write                 = true
write_timeout_seconds = 600
max_patch_bytes       = 200000
max_files             = 50
```

---

## 6. Data flow

1. **Start (gate 1):** pane `w` on a card whose dot has `write = true` (or `w` on any card, then pick a write-capable dot). A confirm overlay shows dot, backend/model, timeout, "edits happen in an isolated copy; nothing changes in your files until you apply." `y` sends `CmdDraft{FromCardID}`. The instruction defaults to the card body; `e` edits it first.
2. **Preconditions:** `write_enabled`, the dot's `write`, the repo is git, the concurrency limit, budget. Failure → toast with the reason.
3. **Snapshot:** `worktree.Snapshot(ctx, repoRoot)` → temp index in `os.TempDir()` (private), `git add -A`, `write-tree`, `commit-tree -p HEAD -m "herdr-dots snapshot <id>"`, `update-ref refs/herdr-dots/snapshots/<id>`. An unborn `HEAD` (no commits) → refuse ("commit once first").
4. **Worktree:** `git worktree add --detach <WorktreesDir>/<id> <snapshot>`.
5. **Run:** `runDot` with `runOpts{write: true}` → `Request{Pass: PassWrite, Write: true, Cwd: wt, Question: instruction, Turns: tail, Memory: block}`, timeout `write_timeout_seconds`.
6. **Diff:** `git -C wt add -A` (inside the worktree's own index) then `git -C wt diff --cached --binary <snapshot>`, plus `--stat` and `--numstat` for the diffstat and flags.
7. **Validate (X5):** parse paths from `--numstat -z` and `diff --name-status -z`; check symlink targets in the worktree; enforce caps. Rejected → card-less feed entry `error: patch rejected (<reason>)` and a toast.
8. **Store and clean:** write the patch and meta; `git worktree remove --force wt`; card `Kind:"patch"` with `Body = summary`, `DiffStat`, `PatchFlags`.
9. **Review:** `v` opens `diff-ui <id>` (zoomed popup). `A` → confirm "Apply 12 files to your working tree? (y/N)" → `ApplyPatch`: the target pane must be idle (`delivery.CanDeliver`-style check; otherwise "agent is working; try when idle") → `git apply --check --3way` → `git apply --3way`. Conflicts: leave conflict markers only if `--3way` produced them; the card shows "applied with conflicts in N files" and the files are listed. Success → status `applied`, snapshot ref deleted, memory outcome `applied`, feed `OutcomeApplied`.
10. **Send:** `⏎` delivers the "Patch ready" message (X7). Status `sent`; the patch file and ref stay for 7 days.
11. **Discard:** `x` → delete the patch, meta and ref; memory outcome `discarded`.
12. **Prune (engine start + daily):** `git worktree prune` for each known repo; remove `worktrees/*` older than 1 hour without a live draft; delete refs and patches older than 7 days with status `sent` or `rejected`.

---

## 7. UI

Patch card in the Dots pane:

```
│▸◆ PATCH  Security · turn 31 · 3 files +42 −7           │
│  Moved JWT to an httpOnly cookie; added SameSite=Lax.  │
│  [binary] none                                         │
│  v view  A apply  ⏎ send to agent  x discard           │
```

Draft progress in the roster: `◆ Security  ✎ drafting… 2:14`.

Diff viewer (`diff-ui`): file list on the left (`j`/`k`), unified diff on the right with syntax-agnostic `+`/`-` coloring, `n`/`p` next/previous hunk, `A` apply (the same gate-2 confirm), `q` close.

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Behavior |
|---|---|
| Not a git repo / unborn HEAD | Refuse with reason |
| Snapshot too large (`git add -A` over 60 s or temp index > 500 MB) | Abort; "working tree too large to snapshot; commit or ignore build output" |
| Dot timeout | Kill the tree (Phase 6 helpers), remove the worktree, feed error |
| Empty diff | Feed `pass` "draft produced no changes" |
| Validation reject | Feed error with reason; worktree removed; no card |
| Apply conflicts | Card notes conflict files; status `applied` with flag `conflicts` |
| Apply refused (agent busy) | Toast; card stays pending |
| Engine crash mid-draft | The next start prunes the worktree and ref; no card |

### 8.2 Safety

- Two explicit gates; both global and per-dot opt-ins default off.
- No shell for any backend; claude never gets `bypassPermissions`; codex's write sandbox is limited to the worktree.
- Hooks are disabled for every git command (`core.hooksPath=<DevNull>`), so a hostile repo hook never runs.
- Patch validation blocks `.git/` and escapes even if a backend's own sandbox fails.
- Apply only when the main agent is idle, never commits, never touches the index.
- Prompt injection: a hostile transcript could steer a draft. That is contained by gate 2 (you read the diff) and the path validation.

### 8.3 Testing

| Layer | Tests |
|---|---|
| `worktree` unit (real git in temp repos) | Snapshot includes modified, staged, untracked non-ignored files and excludes ignored ones; the user's index, stash list, `HEAD` and branches are unchanged (compare before/after); unborn HEAD refused; hooks never run (a hook that writes a marker file); diff/validate rejects `.git/config` changes, symlink escapes and caps; apply clean, apply with drift (3-way merge succeeds), apply with conflict (reported); prune removes orphans |
| Backend | `BuildCmd` with `Write` (exact flags, no `bypassPermissions`, `Bash` disallowed); codex `workspace-write`; toolloop write tools jailed, `edit_file` requires a single occurrence |
| Engine | Draft happy path with a fake backend that edits files in `Cwd` → patch card; disabled globally → refused; dot without `write` → refused; concurrency limit; apply refused while the target is working; outcomes recorded |
| UI | Confirm overlay; patch card keys; diff viewer navigation and apply confirm |
| Platforms | The suite runs on the Phase 6 CI matrix (macOS, Linux, Windows) |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | With `claude -p --permission-mode acceptEdits --tools Read,Grep,Glob,Edit,Write`, can Claude edit files outside the cwd (absolute paths, `../`)? Are additional directories blocked without `--add-dir`? | X3 safety (validation still backstops) |
| V2 | Does `codex exec --sandbox workspace-write -C <wt>` confine writes to `<wt>`, and does `exec` need an approval flag to proceed non-interactively? | X3 |
| V3 | `git apply --3way` behavior when the target files have unstaged changes in the user's tree, on the git versions shipped with macOS, Ubuntu LTS and Git for Windows | X6 |
| V4 | Snapshot time and temp-index size on a large repo (100k files, node_modules ignored) | Limits |
| V5 | Does `git worktree add` on a repo with `core.fsmonitor`, sparse-checkout or partial clone behave? | X1 robustness |

---

## 10. Out of scope

Dots running tests or builds (shell access); dots committing or opening PRs; multi-dot collaborative patches (a council patch); applying patches to other worktrees or branches; non-git projects.

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Shell access in drafts | (a) none; (b) allow a fixed command allowlist (e.g. the project's test command) in the worktree | **(a)** now | Arbitrary commands from a prompt-injectable model are the largest risk; revisit with a sandboxing story |
| O2 | Global default | (a) `write_enabled = false`; (b) true | **(a)** | Opt-in for the most powerful capability |
| O3 | Apply semantics | (a) working tree only via `git apply --3way`; (b) also stage (`--index`) | **(a)** | Leaves review and commit fully to you and your agent |
| O4 | Who may start drafts | (a) only the user from the pane; (b) also the main agent via MCP (with the user's confirm) | **(a)** | Keeps write power behind a human action |

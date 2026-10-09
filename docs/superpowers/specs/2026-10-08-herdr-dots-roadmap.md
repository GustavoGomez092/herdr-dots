# Herdr Dots Roadmap: v1 to Feature Complete

**Status: DRAFT, pending user review**

- **Date:** 2026-10-08
- **Scope:** every phase after v1 until the product is feature complete
- **Source:** the v1 spec's Roadmap (`2026-10-08-herdr-dots-design.md`) and the v1 plan's Interfaces blocks (`docs/superpowers/plans/2026-10-08-herdr-dots-v1.md`)
- **Binding rules:** v1 is a scaffold at best. **No phase makes the product done or ready except the last one (Phase 10), and only when the [feature-complete checklist](#feature-complete-checklist) passes.** Every phase ships on its own on top of v1's real interfaces, and respects Herdr's plugin limits (terminal-only UI, no native UI, no runtime action registration, no storage API, one-shot startup hooks).
- **Phase 2, dot memory, is VITAL.**

---

## Phases

| Phase | Version | Increment | Origin | Spec | Plan outline |
|---|---|---|---|---|---|
| 1 | 0.1.0 | Core loop (scaffold) | v1 roadmap | [v1 spec](2026-10-08-herdr-dots-design.md) | [v1 plan](../plans/2026-10-08-herdr-dots-v1.md) |
| **2** | 0.2.0 | **Dot memory: VITAL** | v1 roadmap | [spec](2026-10-08-herdr-dots-phase-2-dot-memory-design.md) | [plan](../plans/2026-10-08-herdr-dots-phase-2-dot-memory.md) |
| 3 | 0.3.0 | Council mode (with dot-to-dot cross-review) | v1 roadmap | [spec](2026-10-08-herdr-dots-phase-3-council-mode-design.md) | [plan](../plans/2026-10-08-herdr-dots-phase-3-council-mode.md) |
| 4 | 0.4.0 | API and Ollama backends, keys, cost | v1 roadmap | [spec](2026-10-08-herdr-dots-phase-4-api-backends-design.md) | [plan](../plans/2026-10-08-herdr-dots-phase-4-api-backends.md) |
| 5 | 0.5.0 | Dots MCP server | v1 roadmap | [spec](2026-10-08-herdr-dots-phase-5-mcp-server-design.md) | [plan](../plans/2026-10-08-herdr-dots-phase-5-mcp-server.md) |
| 6 | 0.6.0 | Windows support | v1 "Later" | [spec](2026-10-08-herdr-dots-phase-6-windows-design.md) | [outline](../plans/drafts/2026-10-08-herdr-dots-phase-6-windows-outline.md) |
| 7 | 0.7.0 | Write-capable dots in sandboxed worktrees | v1 "Later" | [spec](2026-10-08-herdr-dots-phase-7-write-capable-dots-design.md) | [outline](../plans/drafts/2026-10-08-herdr-dots-phase-7-write-capable-dots-outline.md) |
| 8 | 0.8.0 | Proactive dots: background research, schedules, commit triggers | **Proposed by drafter** | [spec](2026-10-08-herdr-dots-phase-8-proactive-dots-design.md) | [outline](../plans/drafts/2026-10-08-herdr-dots-phase-8-proactive-dots-outline.md) |
| 9 | 0.9.0 | Dot profiles, history and sharing | **Proposed by drafter** | [spec](2026-10-08-herdr-dots-phase-9-profiles-sharing-design.md) | [outline](../plans/drafts/2026-10-08-herdr-dots-phase-9-profiles-sharing-outline.md) |
| 10 | 1.0.0 | Rename, hardening, marketplace release; **feature complete only when the checklist passes** | v1 "Later" (marketplace, rename) + drafter (hardening) | [spec](2026-10-08-herdr-dots-phase-10-release-design.md) | [outline](../plans/drafts/2026-10-08-herdr-dots-phase-10-release-outline.md) |

Each plan outline lists files, exact interfaces (consumes/produces) and required tests per task. Full step-by-step plans get written with superpowers:writing-plans when each phase starts, against the real code of the phases before it.

### Phase 2: Dot memory (VITAL)

The increment that turns dots from stateless turn reviewers into colleagues who know the project. Three scopes: per-project shared memory (conventions, decisions), per-dot-per-project memory (accepted findings, dismissed findings, notes), and user-only global memory (personal preferences across projects). Files live under `STATE_DIR/memory/<project-id>/`, where the project id is the git root commit hash, so moves, clones and worktrees share memory. Memory is learned three ways: deterministic queue outcomes (sent → finding, dismissed → do-not-repeat), an optional `remember` array piggybacked on the normal verdict JSON (no extra call), and budgeted memory passes that let **observe-mode dots build memory silently**. A bounded, relevance-ranked memory block enters every run as reference data, and a deterministic similarity filter guarantees dismissed advice isn't raised again. Model-written project memory waits as a proposal until you approve it. You can list, edit, pin, approve, forget one item, forget all, export, import and purge from the manage popup and a `herdr-dots memory` CLI. OpenAI's dots can only delete memory by deleting the dot; here every entry is individually forgettable.

### Phase 3: Council mode

Ask every attached dot the same question at once and get one merged digest card: deduplicated, ordered by severity, attributed per point, disagreements called out. A chair run merges through any existing backend (no interface change), with a deterministic plain merge as a fallback. An optional second round is the product's **dot-to-dot conversation**: each dot sees the others' answers and agrees, objects or adds. Councils are always queue mode, respect budgets with a preflight, and leave every member out of the next turn's review (loop guard).

### Phase 4: API and Ollama backends

Four new backends behind v1's `backend.Backend`: Anthropic Messages API (official Go SDK, default model `claude-opus-5-5`, structured outputs, refusal handling, server-side fallbacks), OpenAI Responses API (official Go SDK v3), OpenRouter (Chat Completions with authoritative `usage.cost`) and Ollama (local, `num_ctx` handled). A shared read-only tool loop (`read_file`, `grep`, `list_files`, jailed and deny-listed) gives API dots the same repo access CLI dots have. Keys live in a `0600` `credentials.toml` in the plugin config dir, or come from environment variables, and never reach logs or snapshots. Cost is reported in dollars per run, dot and session (provider-reported or from an embedded, dated price table), with dollar budgets next to call budgets. No streaming (verdicts are short).

### Phase 5: Dots MCP server

Implementation plan: `docs/superpowers/plans/2026-10-08-herdr-dots-phase-5-mcp-server.md` (built in 0.5.0; the product is not done until Phase 10).

A stdio MCP server (`herdr-dots mcp`, official Go SDK v1.7+, MCP 2026-07-28 with backward compatibility) installed into Claude Code and Codex by `herdr-dots setup-mcp`. The main agent can `list_dots`, `ask_dot`, `ask_council`, `list_queue`, `read_memory` and `propose_memory`. The server reaches the pane's engine over v1's local IPC (`dots.sock`, found from `HERDR_SOCKET_PATH` plus install-time state dir) through a new data-returning `ipc.Client.Query`. Answers are synchronous, budgeted and rate-limited per turn. A consulted dot skips reviewing that turn. Outside a Herdr pane, or inside a dot's own CLI run, every tool refuses, so dots can't recurse.

### Phase 6: Windows support

**Status (0.6.0):** implemented. Cross-compiled for Windows and Linux and unit-tested on macOS; Windows-only tests compile but have not run. A real Windows run (CI on `windows-latest` and the Herdr Windows beta) is a user gate. The product is not done.

Native support for the Herdr Windows beta, before the remaining feature phases so they're built cross-platform once. Named pipes (Herdr dial and engine IPC with a per-user ACL), `LockFileEx`, Job Objects for process-tree kills, detached engine start, DACL-based privacy for credentials and memory, PATHEXT and `.cmd` shim handling, system prompts moved off argv (on every OS) to avoid command-line limits, Windows paths for transcripts and client configs, and a three-OS CI matrix.

### Phase 7: Write-capable dots in sandboxed worktrees

**Status (0.7.0):** implemented (plan: `docs/superpowers/plans/2026-10-08-herdr-dots-phase-7-write-capable-dots.md`). Unit-tested with real git on macOS; codex-cli can't draft (no shell, ruling 5); a real draft inside Herdr and the Linux/Windows git versions are user gates. The product is not done.

Opted-in dots can draft a fix in an isolated git worktree built from a side-effect-free snapshot of your working tree (temporary index, private ref; your index, stash, branches and `HEAD` are untouched). Two explicit approval gates: start the draft, then apply the patch. Patches are validated (no `.git/`, no escapes, size caps), shown in a diff viewer, and applied with `git apply --3way` only when the main agent is idle, or handed to the agent, or discarded. No shell access for any backend; hooks are disabled for every git command; never `bypassPermissions`.

### Phase 8: Proactive dots (proposed by drafter)

What makes OpenAI's Dots "always on": read-only research while you're away. Idle-time research passes investigate the open questions dots noted in memory and the code around recent changes (optional web search where the backend supports it), and produce one "while you were away" digest. Also cron-scheduled standing tasks (run while Herdr runs, with catch-up and cross-session de-duplication) and commit-triggered reviews (HEAD polling, no hooks installed in repos). Results for unattached projects wait in a project inbox. Everything is capped by per-dot daily runs and a global daily dollar budget, and is off per dot by default.

### Phase 9: Dot profiles, history and sharing (proposed by drafter)

A persistent per-dot history of every run and card resolution across sessions; a profile screen with 7- and 30-day stats (flagged rate, acceptance rate, suppressions, cost, latency), a memory summary and deterministic tuning hints ("mostly dismissed", "rarely speaks", "most expensive"). Export and import of `.dotpack.toml` files (optionally with memory, secret-scanned, never credentials), with risky capabilities clamped on import. Repo-local team dots in `.herdr-dots/dots/` behind a direnv-style, content-hashed trust prompt. A larger template gallery.

### Phase 10: Rename, hardening and marketplace release (final phase)

A new name that doesn't collide with OpenAI's "Dots" (recommended: **Herdr Outriders**; alternatives Herdr Kibitz, Herdr Sheepdogs, Herdr Scouts) with lossless migration of dots, settings, credentials, memory, history, trust, keybindings and MCP registrations. Schema versions and migrations for every persisted file, fuzz tests for every parser, a threat model and final security review, resource budgets proven by an 8-hour soak test, checksum-verified prebuilt binaries for six targets (so marketplace users don't need Go), docs, and a listing on herdr.dev/plugins via the `herdr-plugin` GitHub topic. `1.0.0` is tagged and "feature complete" is said **only after** the acceptance run passes the checklist below on macOS, Linux and Windows.

---

## Dependency order

```
Phase 1 (v1 scaffold)
   │
   ▼
Phase 2  Dot memory (VITAL) ─────────────────────────────────────┐
   │                                                             │
   ├──▶ Phase 3  Council ──┐                                     │
   │                       │                                     │
   ├──▶ Phase 4  API backends + cost ──┐                         │
   │                       │           │                         │
   │                       ▼           ▼                         │
   └──────────────▶ Phase 5  MCP server (uses 2, 3, 4)           │
                                │                                │
                                ▼                                │
                         Phase 6  Windows (ports 1–5)            │
                                │                                │
                                ▼                                │
                         Phase 7  Write-capable dots (2, 4, 6)   │
                                │                                │
                                ▼                                │
                         Phase 8  Proactive dots (2, 4, 6, 7) ◀──┘
                                │
                                ▼
                         Phase 9  Profiles, history, sharing (2–8)
                                │
                                ▼
                         Phase 10 Rename, hardening, release → feature complete
```

| Phase | Hard dependencies | Soft dependencies (works without, better with) |
|---|---|---|
| 2 | 1 | none |
| 3 | 1 | 2 (members carry memory; outcomes recorded) |
| 4 | 1 | 2 (memory block placement and caching), 3 (chair can use API models) |
| 5 | 1 | 2 (`read_memory`, `propose_memory`), 3 (`ask_council`), 4 (cost in results). Tools for phases not yet shipped are omitted |
| 6 | 1–5 (ports what exists) | none |
| 7 | 1, 6 (platform helpers) | 2 (outcomes), 4 (tool loop write tools for API dots) |
| 8 | 2 (questions, research memory), 4 (dollar caps, web tools), 7 (hook-free git runner) | 3 (digest formatting reuses `PlainMerge`) |
| 9 | 2, 4 | 3, 5, 7, 8 (their run kinds appear in history; their capabilities are clamped on import) |
| 10 | 1–9 | none |

The sequence is linear for planning simplicity. Phases 3 and 4 are independent of each other and can be built in either order or in parallel.

---

## Feature-complete checklist

The product is **feature complete** only when every item below passes in the Phase 10 acceptance run on macOS, Linux and Windows (Herdr Windows beta), with evidence recorded in `docs/superpowers/notes/1.0-acceptance.md`. Until then, nothing is called done, finished, ready or feature complete.

**Core loop (Phase 1)**
- [ ] Create, edit, delete dots from scratch and from templates in the manage popup; invalid files are reported with their line number.
- [ ] Attach one or more dots to a Claude Code pane and a Codex pane with a keybinding.
- [ ] After each turn, dots speak only when relevant; all four delivery modes behave as specified (observe, suggest, queue, inject with guards).
- [ ] Loop guard: a delivered dot message never re-triggers that dot and never causes an inject.
- [ ] Per-dot and per-session call budgets stop runs; failures stop a dot after 3 and `r` resets it.
- [ ] `/clear` or an agent restart in the same pane is followed (new session id).
- [ ] `setup-keys` installs bindings with conflict detection, confirmation and a backup.

**Memory: VITAL (Phase 2)**
- [ ] Memory persists across engine restarts, Herdr restarts and named sessions, and is shared across clones, moves and worktrees of a repo.
- [ ] A dismissed finding isn't raised again by the same dot on the same project (suppression logged).
- [ ] Observe-mode dots build memory silently within their pass budget and never create cards.
- [ ] The memory block never exceeds its budget; pinned entries are always included.
- [ ] Model-written project memory waits for approval; models can't edit user entries or other dots' entries.
- [ ] List, edit, pin, approve, forget one, forget all, export, import, purge: all from the TUI and the CLI. Deleting a dot offers to delete its memory.
- [ ] Secrets are refused by the scanner; two concurrent sessions never lose memory writes.

**Council (Phase 3)**
- [ ] One keystroke convenes all attached dots; one digest card arrives with attribution and expandable parts.
- [ ] Member failures and chair failures still produce a card; the second round records stances.
- [ ] Budget preflight refuses when the budget is short; members skip the turn after a council delivery.

**Backends (Phase 4)**
- [ ] Dots run on `anthropic-api`, `openai-api`, `openrouter` and `ollama`, with `Check` on save.
- [ ] API dots read the repo through the jailed read-only tool loop; secret files are denied.
- [ ] Keys are stored `0600` (DACL on Windows), never appear in logs, feed, snapshots or errors.
- [ ] Per-run, per-dot and per-session costs are shown in dollars; dollar budgets stop runs.

**MCP (Phase 5)**
- [ ] `setup-mcp` installs into Claude Code and Codex with a backup; `--remove` restores.
- [ ] The main agent can list, ask, convene, read the queue, read memory and propose memory; answers come from the pane's engine within the timeout.
- [ ] Ask limits hold; a consulted dot skips that turn; tools refuse outside Herdr and inside dot runs.

**Windows (Phase 6)**
- [ ] Every checklist item in this list passes on Windows, with a three-OS CI matrix green.

**Write-capable dots (Phase 7)**
- [ ] Off by default globally and per dot; two explicit gates (start, apply).
- [ ] The snapshot leaves the user's index, stash, branches and `HEAD` untouched; hooks never run.
- [ ] Invalid patches (`.git/`, escapes, caps) are rejected; apply works only when the agent is idle and reports conflicts.
- [ ] Orphaned worktrees and refs are pruned after a crash.

**Proactive (Phase 8)**
- [ ] Idle research runs once per idle period within caps and yields one away digest.
- [ ] Scheduled dots run once per slot across sessions, with catch-up; commit triggers fire once per new HEAD.
- [ ] Inbox results appear when a pane on that project attaches; the daily dollar budget stops all background work.

**Profiles and sharing (Phase 9)**
- [ ] Profiles show 7- and 30-day stats, memory summary, history and hints.
- [ ] Dot packs export and import with clamps and secret scanning; slug collisions never overwrite.
- [ ] Project dots require content-hashed trust; a changed file revokes trust and pauses the dot.

**Release (Phase 10)**
- [ ] The product name is chosen and applied; an existing `herdr-dots` install migrates without loss.
- [ ] `herdr plugin install owner/repo` works without Go on all six targets, with verified binaries.
- [ ] All persisted files are versioned and migrate; fuzz targets run clean; `govulncheck` is clean.
- [ ] The threat model is written and reviewed; the final security review's findings are fixed.
- [ ] The 8-hour soak meets the CPU, RSS and snapshot budgets.
- [ ] README, guide, SECURITY, CHANGELOG, CONTRIBUTING and LICENSE exist; the repo has the `herdr-plugin` topic and shows on herdr.dev/plugins.

---

## Cross-phase changes to v1 interfaces (summary)

Each phase spec lists its exact changes. The biggest ones:

| v1 symbol | Changed in | Change |
|---|---|---|
| `backend.Request` | 2, 4, 6, 7, 8 | + `Memory`, `Learn`, `Pass` (2); + `Effort`, `MaxTokens`, `BaseURL`, `ContextWindow`, `Fallbacks` (4); + `Write` (7); + `Web` (8) |
| `backend.Result` | 2, 4, 8 | + `Remember` (2); + cache tokens, `CostKnown`, `Model`, `ToolCalls`, `Duration` (4); + `Warnings` (8). `Verdict` stays comparable |
| `backend.ParseVerdict` | 2 | Unchanged; new `ParseReply` and `ParseMemoryOps` beside it |
| `backend.New` | 4 | Kept; new `NewWith(kind, Options)` |
| `backend.ClaudeCLI.BuildCmd` | 6, 7, 8 | Long system prompts via file (6); write flags (7); web tools (8) |
| `dots.Dot` | 2, 4, 7, 8, 9 | + `[memory]` (2); backend fields and USD budget (4); `[capabilities]` (7); `[research]`, `[schedule]`, `on_commit` (8); `Origin` (9) |
| `config.Config` | 2, 3, 4, 5, 7, 8 | Memory settings, `[council]`, providers and USD budget, `[mcp]`, `write_enabled`, proactive settings |
| `paths.Paths` | 2, 4, 6, 7, 8, 9 | `MemoryDir`, `Discover`, `CredentialsFile`, `PricesFile`, `EngineEndpoint`, `WorktreesDir`, `PatchesDir`, `DailyFile`, `HistoryDir`, `TrustFile` |
| `lockfile` | 2, 6 | + `Acquire(path, timeout)`; Windows implementation |
| `transcript.Turn.IsFromDot` | 3, 10 | Council rule (3); legacy and new header forms (10) |
| `queue.Card` | 3, 7, 8 | + `Kind`, `Parts`, `Question` (3); patch fields (7); `ProjectID` and inbox (8) |
| `state.Usage` | 4 | + `Delta`, `Add`, `Cost`, `PaneCost` (`Record` kept) |
| `engine.Deps` | 2, 7–9 | + `Memory`, `ResolveProject` (2); `Clock`, `Daily`, `Git` (8); `History`, `Trust` (9) |
| `engine.FilterInput` / `Decide` | 4, 5 | Dollar budgets (4); `Consulted` / `ReasonConsulted` (5) |
| `ipc` | 2, 3, 5, 7, 9 | New commands; `Response.Data`, `Querier`, `Client.Query` (5); `Listen`/`Dial` endpoints (6) |
| Manifest | 3, 6, 7, 10 | `council` action and popup (3); Windows platform and commands (6); `diff` pane (7); new id (10) |

---

## Top open decisions for the user (across phases)

Every spec ends with its own "Open decisions for the user" table. These are the ones that shape the roadmap most:

| # | Decision | Recommendation | Where |
|---|---|---|---|
| 1 | Product name before marketplace publishing | **Herdr Outriders** (`herdr-outriders`); alternatives Kibitz, Sheepdogs, Scouts | Phase 10 O1 |
| 2 | Model-written project memory: proposed until approved, or active immediately | **Proposed until approved** | Phase 2 O2 |
| 3 | Include the drafter-proposed Phases 8 (proactive) and 9 (profiles/sharing) in "feature complete" | **Yes, both** | Phase 8 O5, Phase 9 O1 |
| 4 | Windows before or after the remaining feature phases | **Before (Phase 6)** so later phases are built cross-platform | Phase 6 O1 |
| 5 | Shell access for write-capable dots | **None** for now | Phase 7 O1 |
| 6 | API key storage | **`credentials.toml` (0600) + env indirection**; keychain later | Phase 4 O1 |
| 7 | Default Anthropic model for API dots | **`claude-opus-5-5`**, with per-dot `effort` for cost control | Phase 4 O2 |
| 8 | Which dots the main agent can ask over MCP | **Attached dots only** | Phase 5 O1 |
| 9 | Schedules only while Herdr runs, or OS schedulers | **Only while Herdr runs**, with catch-up | Phase 8 O1 |
| 10 | Project identity for memory | **Git root commit**, path fallback | Phase 2 O1 |

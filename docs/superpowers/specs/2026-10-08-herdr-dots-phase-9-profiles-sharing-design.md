# Herdr Dots Phase 9: Dot Profiles, History and Sharing, Design Spec

**Status: DRAFT, pending user review**

- **Proposed by drafter.** This phase isn't in the v1 roadmap. Rationale: once dots have memory, proactive behavior and costs, users need to **see how each dot is doing** (is it useful or noisy? what does it cost?) and to **reuse good dots** across machines and teams. v1's feed is per Herdr session and capped, so there's no long-term history. Dots are only shareable by copying TOML by hand, with no trust model for prompts written by others. Without these, a user can't tune a team of dots or bring it to a new project safely, so I count them as required for feature complete.
- **Date:** 2026-10-08
- **Phase:** 9 of 10 (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first.
- **Builds on:** v1 `dots` (load/save/templates), `state` feed, `ui/manage`; Phase 2 memory (export, profile summary); Phase 3–8 run kinds (all are recorded in history); Phase 4 cost; Phase 7/8 capabilities (clamped on import)
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-9-profiles-sharing-outline.md`
- **The product is not done after this phase.** Ships as `0.9.0`.

---

## 1. Intent

### What the user wants

- **A profile for every dot:** what it is, how often it speaks, how often you accept vs dismiss its advice, what it costs, what it remembers, and its recent history across all sessions and projects.
- **Tuning hints:** plain, deterministic suggestions when a dot is noisy, silent or expensive.
- **Sharing:** export a dot (optionally with its memory) as a single file, import one from a file or URL with a clear trust preview, and **check team dots into a repo** (`.herdr-dots/dots/`) so everyone working on it gets the same specialists, behind a direnv-style trust prompt.
- **More starting points:** a larger template gallery.

### Success criteria

1. Every run of every kind (review, summon, council member, ask, research, scheduled, commit, draft) and every card resolution (sent, edited, dismissed, applied, suppressed) is recorded in a persistent per-dot history, kept across sessions and capped.
2. The manage popup has a profile screen per dot with 7- and 30-day stats, a memory summary, recent history and tuning hints.
3. `herdr-dots dots export` / `import` round-trip a dot (and optionally its memory) through a `.dotpack.toml` file. Import shows a preview and clamps risky capabilities (inject, write, research, schedule, web) unless you confirm each one.
4. Repo-local dots in `.herdr-dots/dots/*.toml` appear in the library for panes on that repo, marked "project". They can't be attached until trusted, and trust is revoked when the file changes.
5. Exports never contain credentials, and memory in exports passes the secret scanner.
6. At least 7 new templates.

---

## 2. Research summary

- **v1:** `state.Feed` is per session (`SessionDir/feed.jsonl`), `Recent(paneID, n)` only. `dots.LoadAll(dir)` loads one directory. Templates are embedded with `go:embed`.
- **Trust prompts for repo-provided config:** direnv's `allow` model (a hash of the file stored per user, re-prompt on change) is familiar to terminal users and fits a TUI.
- **Prompt files from strangers are code-adjacent:** a dot's `purpose` is a system prompt; with inject, write, web or schedule capabilities it can act. So import and project dots default to the safest mode and require explicit, per-capability confirmation.

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| S1 | History storage | `STATE_DIR/history/<slug>.jsonl`, append-only, one record per run and one per card resolution (joined by card id), flock-guarded appends, ≤ 20,000 lines per dot (trimmed to 15,000 when over) | Extending the session feed (per-session, wrong lifetime); SQLite |
| S2 | Stats | Computed on demand from history (7- and 30-day windows): runs by kind, flagged rate, acceptance rate = (sent + edited + applied) / (those + dismissed), suppressed count, cost, median latency | Precomputed counters (drift, migration pain) |
| S3 | Tuning hints | Deterministic rules (table in §6.2); never model-generated | An LLM "coach" (cost, nondeterminism) |
| S4 | Pack format | `.dotpack.toml`, `format = 1`, containing `[dot]` and optional `[[memory]]`; no credentials, no history | JSON (less hand-editable); zip bundles (YAGNI) |
| S5 | Import safety | Clamp on import: `delivery.mode` inject → queue; `capabilities.write` → false; `research.enabled`, `research.web` → false; `schedule.cron` → ""; `trigger.on_commit` → false. Each clamp listed in the preview; `--keep <capability>` or a per-item toggle in the TUI restores it after confirmation | Importing as-is |
| S6 | URL import | `https://` only, ≤ 256 KB, `Content-Type` text, shows the final URL after redirects; no auto-updates | Git-based dot registries (YAGNI) |
| S7 | Project dots | `.herdr-dots/dots/*.toml` at the repo root; loaded for panes whose project root contains it; slug namespaced `proj:<slug>`; the same clamps as import until trusted with capabilities | Auto-trust (a malicious repo could ship an inject dot) |
| S8 | Trust store | `CONFIG_DIR/trusted.json`: `{path, sha256, capabilities[], trusted_at}`; trust is per file content; a changed hash → untrusted again | Trust per repo (one later change would ride on old trust) |
| S9 | Memory in exports | `--with-memory dot` or `dot+project`; active entries only; user-sourced entries included only with `--include-user`; secret scan blocks export of flagged entries unless `--force` | Exporting everything by default |
| S10 | Imported memory | Lands as `source: import`, `status: proposed` for project scope, active for dot scope | All active (imports are someone else's conclusions) |

---

## 4. Architecture

### 4.1 Packages

- **`internal/history`** (new): append, query, stats, prune.
- **`internal/dotpack`** (new): export, parse, clamp, fetch.
- **`internal/projectdots`** (new): discovery and trust.
- **`internal/ui/profile`** (new): the profile screen.
- `internal/dots/templates/*.toml`: new templates.

### 4.2 Changes to earlier interfaces (exact)

| Symbol | Change |
|---|---|
| `dots.Dot` | Add `Origin string` (`user` \| `project` \| `template`; not serialized for user dots, derived at load) and `SourcePath string` (load path; not serialized) |
| `dots` | Add `LoadProject(root string) ([]Dot, []LoadError)` (namespaced slugs `proj:<slug>`); `Validate` accepts the `proj:` prefix only for `Origin == "project"` |
| `engine.Deps` | Add `History *history.Store`, `Trust *projectdots.TrustStore`, `LoadProjectDots func(root string) ([]dots.Dot, []dots.LoadError)` |
| `engine` | Every `feed(...)` call also appends a `history.Record`; `SendCards`, `Dismiss`, `Edit`, `ApplyPatch` (Phase 7) and suppression (Phase 2) append resolution records; `ReloadDots` merges project dots for the projects of attached panes; `Attach` refuses an untrusted project dot (`ErrUntrusted`) |
| `view.LibraryDot` | Add `Origin string`, `Trusted bool`, `Clamped []string` |
| `ipc` | Add `CmdTrust` (`TrustArgs{Slug string; Capabilities []string}`), `CmdUntrust` (`DotArgs`) |
| `memory.Store` (Phase 2) | Add `ImportPack(p string, entries []Entry) (ApplyResult, error)` (applies S10) |
| `main.dispatch` | Route `dots` → `runDots(args)` with `export`, `import`, `list`, `trust`, `untrust` |
| `paths.Paths` | Add `HistoryDir() string` (`StateDir/history`), `TrustFile() string` (`ConfigDir/trusted.json`) |

---

## 5. Data model

### 5.1 History records

```json
{"t":"run","at":"…","dot":"security","project":"g-9f2c…","pane":"w1:p1","kind":"review","turn":14,
 "outcome":"flagged","severity":"warn","card":"c-0193","cost_usd":0.0041,"in":2310,"out":180,"ms":8120}
{"t":"resolve","at":"…","dot":"security","card":"c-0193","resolution":"dismissed"}
```

`kind`: `review`, `summon`, `council`, `ask`, `research`, `scheduled`, `commit`, `draft`, `memory`. `resolution`: `sent`, `edited`, `dismissed`, `applied`, `discarded`, `suppressed`, `orphaned`.

### 5.2 Dot pack

```toml
[pack]
format             = 1
exported_at        = "2026-10-21T10:00:00Z"
herdr_dots_version = "0.9.0"
note               = "Security reviewer tuned for Next.js apps"

[dot]
name    = "Security"
glyph   = "◆"
color   = "red"
purpose = """…"""
[dot.backend]
kind  = "claude-cli"
model = "sonnet"
[dot.delivery]
mode = "queue"
# … all other dot sections as in the dot TOML …

[[memory]]
scope = "dot"
kind  = "convention"
text  = "Prefer zod schemas at API boundaries."
paths = ["src/api/**"]
```

### 5.3 Trust file

```json
{"version":1,"entries":[{"path":"/Users/g/src/app/.herdr-dots/dots/a11y.toml","sha256":"…","capabilities":["inject"],"trusted_at":"…"}]}
```

---

## 6. Data flow

### 6.1 Profile

The manage library's `i` → `profile.New(slug)` → `history.Stats(slug, now, 7d/30d)`, `history.Query(slug, since, 50)`, `memory.Stats` across projects → render. `enter` on a history row shows the full message (from the run record's card if still in the queue, otherwise the stored first 300 characters).

### 6.2 Tuning hints (deterministic)

| Rule (30-day window) | Hint |
|---|---|
| ≥ 10 resolved cards and acceptance < 20% | "Mostly dismissed. Tighten the purpose, raise cooldown, or switch to suggest." |
| ≥ 50 runs and flagged < 3% | "Rarely speaks. Consider observe mode or narrower file_globs." |
| Cost share ≥ 50% of all dots' cost and ≥ $1 | "Your most expensive dot. Try a cheaper model or lower effort." |
| ≥ 5 suppressed (Phase 2) | "Keeps repeating dismissed advice. Review its dismissed memories." |
| Error rate ≥ 20% over ≥ 10 runs | "Fails often. Check the backend (press r to reset)." |

### 6.3 Export and import

- **Export:** `dots.Load` → `memory.List` (filters per S9) → `ScanSecrets` → `dotpack.Export` → write the file (`0600`) or stdout.
- **Import:** read the file or fetch the URL → `dotpack.Parse` (format check, `dots.Validate`) → `dotpack.Clamp` → preview (TUI or CLI) listing purpose, backend, clamps and memory count → confirm → `dots.Save(create=true)` (slug collision → ask for a new name) → `memory.ImportPack` for the current project (or `--project`).

### 6.4 Project dots

1. On `Attach` and `ReloadDots`, for each attached project root with `.herdr-dots/dots/`, `LoadProject(root)`.
2. For each file: `Trust.Check(path, sha256)` → `trusted` (with its capability list) or `untrusted`.
3. Untrusted: shown in the library as `⚠ project · untrusted`, not attachable. `T` opens a trust preview (purpose, capabilities requested, clamps). Confirm → `CmdTrust`.
4. Trusted without capabilities: loaded with clamps. Trusted with capabilities: as written.
5. File changed (hash differs): back to untrusted; already-attached project dots are paused with the toast "project dot a11y changed: review to re-trust".

---

## 7. UI

Profile screen (`i` from the library):

```
┌ ◆ SECURITY · claude-cli/sonnet · queue ───────────────────────── project: user ┐
│ Purpose: You are a senior application security reviewer…                      │
├─ LAST 30 DAYS ────────────────────────────────────────────────────────────────┤
│ runs 214 (review 180 · council 12 · ask 9 · research 13)   flagged 11%         │
│ accepted 64% (16 / 25)   suppressed 4   cost $1.82   median 7.9 s              │
├─ MEMORY ──────────────────────────────────────────────────────────────────────┤
│ 3 projects · 41 entries (12 findings · 9 dismissed · 6 conventions) · 2 pinned │
├─ HINTS ───────────────────────────────────────────────────────────────────────┤
│ Keeps repeating dismissed advice. Review its dismissed memories.               │
├─ HISTORY ─────────────────────────────────────────────────────────────────────┤
│▸ 10-21 14:03  review   warn   dismissed  CSRF on /admin…                       │
└ enter details  M memory  x export  tab 7d/30d  esc back ──────────────────────┘
```

Library additions: an origin badge (`project`, `template-derived`), `⚠ untrusted`, `T` trust, `I` import (file path or URL prompt), `x` export from the profile.

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Behavior |
|---|---|
| Corrupt history line | Skipped during queries (counted in a "skipped" footnote) |
| Pack format unknown / invalid dot | Import refused with the validation errors |
| URL fetch fails / too large / not https | Refused with the reason |
| Secret found in export memory | Export refused unless `--force` (CLI) or per-entry exclusion (TUI) |
| Slug collision on import | Prompt for a new name (CLI: `--as <name>`) |

### 8.2 Safety

- Imports and project dots start clamped; capabilities need per-item confirmation; trust is content-hashed.
- Exports never include credentials or history; memory is filtered and scanned.
- History stays local and holds only 300-character excerpts.

### 8.3 Testing

| Layer | Tests |
|---|---|
| `history` | Append and query with an injected clock; stats math (acceptance, flagged, median); trim at 20,000; concurrent appends from two processes |
| `dotpack` | Export → parse round trip equals the original dot; clamps table; secret-flagged memory blocks export; unknown format refused; URL fetch with `httptest` (https only; size cap; redirect shown) |
| `projectdots` | Discovery; namespacing; trust and hash change → untrusted; untrusted can't attach (`ErrUntrusted`); a changed file pauses an attached dot |
| Engine | Every run kind writes a history record; resolutions recorded; project dots merge on attach |
| UI | Profile renders stats and hints from fixture history; import preview lists clamps; the trust preview flow |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | History file sizes after a simulated heavy month (10 dots, 4 panes, 8 h/day): is the 20,000-line cap the right size for profile queries under 100 ms? | S1 |
| V2 | Should project dots also be discoverable in non-git directories (path identity)? | S7 |
| V3 | Do team members expect project dots to share **project memory** via the repo (e.g. `.herdr-dots/memory.toml`)? Confirm with the user before scoping anything | Out of scope boundary |

---

## 10. Out of scope

A hosted dot registry or marketplace for dots; syncing memory between machines or teammates beyond pack files; collaborative live memory; analytics leaving the machine.

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Include this phase in feature complete | (a) yes; (b) defer | **(a)** | A team of dots needs tuning and sharing to be practical |
| O2 | Project dots | (a) supported with direnv-style trust; (b) not supported | **(a)** | Teams want shared specialists; trust keeps it safe |
| O3 | Import clamps | (a) clamp all risky capabilities, confirm per item; (b) import as-is with a warning | **(a)** | Purpose text plus capabilities from strangers is a real attack surface |
| O4 | Committed project memory | (a) not in this phase; (b) a `.herdr-dots/memory.toml` read-only layer | **(a)** pending V3 | Avoid scope creep; packs cover sharing for now |

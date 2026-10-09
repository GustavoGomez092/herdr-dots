# Herdr Dots Phase 9: Dot Profiles, History and Sharing, Implementation Plan Outline

**Status: DRAFT, pending user review** (phase proposed by drafter)

> **This is an outline, not an executable plan.** Earlier phases' code doesn't exist yet. When Phase 9 starts, write the full step-by-step plan with superpowers:writing-plans against the real code, then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Add persistent per-dot history with a profile screen and deterministic tuning hints, export/import of dot packs with capability clamps, trusted repo-local project dots, and a larger template gallery.

**Architecture:** `internal/history` records every run and card resolution in append-only per-dot JSONL under `STATE_DIR/history`. `internal/dotpack` exports and imports `.dotpack.toml` with secret scanning and clamps. `internal/projectdots` discovers `.herdr-dots/dots/*.toml` and gates them behind a content-hashed trust store. The manage popup gains profile, import and trust flows; a `herdr-dots dots` CLI mirrors them.

**Tech Stack:** Go 1.25; existing dependencies only.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-9-profiles-sharing-design.md`

## Global Constraints

- The product is **not** done after this phase. Version `0.9.0`. README roadmap keeps Phase 2 **VITAL**.
- Platforms: macOS, Linux, Windows (CI matrix).
- Imported and untrusted project dots are clamped: no inject, no write, no research/web, no schedule, no commit trigger, until the user confirms each capability.
- Exports never contain credentials or history; exported memory passes `memory.ScanSecrets`.
- Trust is per file content hash; any change revokes it.
- History holds at most 300-character excerpts and stays local.
- Files `0600`, directories `0700` (DACLs on Windows), atomic writes; `go test -race ./...` and `go vet ./...` before every commit.

## Review Focus

1. **A malicious repo shipping `.herdr-dots/dots/helper.toml` with `mode = "inject"` and `write = true`.** Expect the dot to appear untrusted, be impossible to attach, and the trust preview to list both capabilities with each off by default. (Task 4)
2. **A trusted project dot edited by a teammate's commit while attached.** Expect it paused with a toast and re-trust required. (Task 4)
3. **An import from a URL that redirects to a 50 MB file or to `http://`.** Expect refusal with the reason. (Task 3)
4. **A slug collision** (importing "Security" when one exists). Expect a prompt for a new name, never an overwrite. (Task 3)
5. **History from a busy month.** Expect the profile to open in under 100 ms and trimming not to lose resolution records for cards still visible. (Task 2)

---

## File Structure

```
internal/history/history.go          Store, Record, Append, Query, Stats, Trim
internal/dotpack/dotpack.go          Pack, Export, Parse, Clamp, Fetch
internal/projectdots/projectdots.go  Discover, TrustStore
internal/dots/dot.go                 + Origin, SourcePath, LoadProject, proj: slugs
internal/dots/templates/*.toml       + accessibility, performance, docs, i18n, dependency-auditor, api-design, data-privacy
internal/memory/store.go             + ImportPack
internal/paths/paths.go              + HistoryDir, TrustFile
internal/engine/engine.go            history hooks, project dot merge, ErrUntrusted
internal/engine/handle.go            CmdTrust, CmdUntrust
internal/ipc/ipc.go                  + commands, TrustArgs
internal/view/view.go                + LibraryDot.Origin/Trusted/Clamped
internal/ui/profile/model.go         profile screen
internal/ui/manage/model.go          i, I, T keys, origin badges
cmd/herdr-dots/dots.go               dots CLI (export, import, list, trust, untrust)
README.md
```

---

## Tasks

### Task 1: Verification (V1–V3)

**Files:** Create `docs/superpowers/notes/phase-9-verification.md` (a V1 synthetic benchmark; V2/V3 are user questions to record answers to).
**Interfaces:** Findings only.
**Tests:** A benchmark `BenchmarkStats30d` over a synthetic 20,000-line history.

### Task 2: History store and engine hooks

**Files:** Create `internal/history/history.go`. Modify `internal/paths/paths.go`, `internal/engine/engine.go` (and the Phase 3/5/7/8 run paths through the shared `feed` wrapper). Test: `internal/history/history_test.go`, `internal/engine/history_test.go`.
**Interfaces:**
- Consumes: `fsutil`, `lockfile.Acquire`.
- Produces: `type Record struct{ T string; At time.Time; Dot, Project, Pane, Kind string; Turn int; Outcome, Severity, Card, Resolution, Excerpt string; CostUSD float64; In, Out int; Ms int64 }`; `Open(dir string) *Store`; `(*Store) Append(r Record) error`; `(*Store) Query(slug string, since time.Time, limit int) ([]Record, error)`; `type Stats struct{ Runs map[string]int; Flagged, Resolved, Accepted, Dismissed, Suppressed, Errors int; CostUSD float64; MedianMs int64; Skipped int }`; `(*Store) Stats(slug string, now time.Time, window time.Duration) (Stats, error)`; `(*Store) Trim(slug string) error`; `(paths.Paths) HistoryDir() string`; `engine.Deps.History *history.Store`.
**Tests:** append/query order; stats math against fixtures; acceptance counts sent+edited+applied; a corrupt line is skipped and counted; trim at 20,000 to 15,000 keeps resolution records whose run is kept; a cross-process append (re-exec); engine: a review run, summon, council member, ask, research, scheduled, commit and draft each write `t:"run"` with the correct `kind`; send, dismiss, apply and suppress write `t:"resolve"`.

### Task 3: Dot packs (export/import)

**Files:** Create `internal/dotpack/dotpack.go`, `cmd/herdr-dots/dots.go`. Modify `internal/memory/store.go`, `cmd/herdr-dots/main.go`. Test: `internal/dotpack/dotpack_test.go`, `cmd/herdr-dots/dots_test.go`, extend `memory/store_test.go`.
**Interfaces:**
- Consumes: `dots.Dot`, `dots.Validate`, `dots.Save`, `memory.Store.List`, `memory.ScanSecrets`.
- Produces: `type Pack struct{ Format int; ExportedAt time.Time; Version, Note string; Dot dots.Dot; Memory []memory.Entry }`; `Export(d dots.Dot, mem []memory.Entry, note string) ([]byte, error)`; `Parse(data []byte) (Pack, error)`; `type Clamp struct{ Field, From, To string }`; `ClampPack(p Pack, keep []string) (Pack, []Clamp)`; `Fetch(ctx context.Context, c *http.Client, url string, maxBytes int64) ([]byte, string, error)`; `var ErrFormat, ErrNotHTTPS, ErrTooLarge, ErrSecretInExport`; `(*memory.Store) ImportPack(p string, entries []memory.Entry) (memory.ApplyResult, error)`; `runDots(args []string) error` (`export <slug> [--with-memory dot|dot+project] [--include-user] [--force] [-o file]`, `import <file|url> [--as name] [--keep cap…] [--yes]`, `list`).
**Tests:** a round trip equals the original (minus derived fields); each clamp applied and listed; `--keep inject` restores only inject; secret memory → `ErrSecretInExport`; `--force` exports; unknown `format` → `ErrFormat`; `Fetch` refuses `http://`, caps size, reports the final URL; a slug collision prompts (`--as` required non-interactively); imported project memory lands proposed.

### Task 4: Project dots and trust

**Files:** Create `internal/projectdots/projectdots.go`. Modify `internal/dots/dot.go`, `internal/engine/engine.go`, `internal/engine/handle.go`, `internal/ipc/ipc.go`, `internal/view/view.go`, `internal/paths/paths.go`. Test: `internal/projectdots/projectdots_test.go`, `internal/engine/projectdots_test.go`.
**Interfaces:**
- Consumes: `dotpack.ClampPack` (same clamp rules), `fsutil`.
- Produces: `dots.Dot.Origin, SourcePath string`; `dots.LoadProject(root string) ([]dots.Dot, []dots.LoadError)`; `type TrustStore struct{ Path string }`; `(TrustStore) Check(path string, sum [32]byte) (trusted bool, caps []string, err error)`; `(TrustStore) Trust(path string, sum [32]byte, caps []string) error`; `(TrustStore) Revoke(path string) error`; `engine.Deps.Trust`, `engine.Deps.LoadProjectDots`; `engine.ErrUntrusted`; `ipc.CmdTrust`, `ipc.CmdUntrust`, `ipc.TrustArgs{Slug string; Capabilities []string}`; `view.LibraryDot.Origin string; Trusted bool; Clamped []string`; `(paths.Paths) TrustFile() string`.
**Tests:** discovery with `proj:` slugs; untrusted → `ErrUntrusted` on attach; trust without caps → loaded clamped; trust with `inject` → inject allowed; a file edit → hash mismatch → untrusted and the attached dot paused with a toast; `Revoke`.

### Task 5: Profile screen, library flows, templates, README

**Files:** Create `internal/ui/profile/model.go`, new templates in `internal/dots/templates/`. Modify `internal/ui/manage/model.go`, `README.md`, `internal/version/version.go` (`0.9.0`). Test: `internal/ui/profile/model_test.go`, extend `internal/ui/manage/model_test.go`, `internal/dots/templates_test.go`.
**Interfaces:**
- Consumes: `history.Stats/Query`, `memory.Stats`, `dotpack`, `projectdots`.
- Produces: `profile.New(d Deps) Model` with `Deps{History *history.Store; Memory *memory.Store; Dot dots.Dot; Now func() time.Time}`; `profile.Hints(s history.Stats, share float64) []string`; library keys `i` (profile), `I` (import prompt), `T` (trust preview), `x` (export from profile); origin badges.
**Tests:** profile renders the fixture stats; `Hints` table per §6.2; `tab` switches 7d/30d; the import preview lists clamps and toggles; the trust preview lists requested capabilities, each off by default; all 14 templates validate and load; README sections (profiles, packs, project dots and trust); Phase 2 still **VITAL**; no "done/ready".

# Herdr Dots Phase 9: Dot Profiles, History and Sharing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep a persistent history of every dot run and card resolution, show it as a per-dot profile with 7- and 30-day stats and deterministic tuning hints, let people export and import dots (optionally with memory) as `.dotpack.toml` files with every risky capability clamped, let repositories ship team dots in `.herdr-dots/dots/` behind a content-hashed trust store, add seven templates, and ship it as `0.9.0`.

**Architecture:** `internal/history` appends one JSON line per run and per card resolution to `STATE_DIR/history/<slug>.jsonl` (per-file lock, capped at 20,000 lines) and computes stats on demand; the engine writes run records from one `runTrace` per `runDot` and resolutions through `spawnOutcome`/`spawnResolve`, never under an engine lock. `dots.ClampDot` is the single clamp table for everything written by someone else; `internal/dotpack` exports, parses and fetches packs, and `memory.Store.ImportPack` lands their memory as clearly attributed, never-suppressing, someone-else's conclusions. `internal/projectdots` reads repository dot files once, hashes exactly the parsed bytes and checks them against `CONFIG_DIR/trusted.json`; the engine keeps scanned repositories per root, merges trusted (clamped) project dots into its dot map, and pauses an attached project dot whose file changes. The manage popup gains a profile screen, import and export screens and a trust preview; a `herdr-dots dots` CLI mirrors them.

**Tech Stack:** Go 1.25 (`go 1.25.0` directive since Phase 4); existing dependencies only (`BurntSushi/toml`, `bubbletea`, `bubbles`, `lipgloss`, `doublestar`). No new modules.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-9-profiles-sharing-design.md`, approved with the amendments in `docs/superpowers/decisions/2026-10-08-phase-decisions.md` (ruling 3: this phase is in scope; ruling 11: project identity is the git root commit; ruling 17: project dots are supported with content-hash trust and risky capabilities are clamped on import). This plan replaces the outline `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-9-profiles-sharing-outline.md`.

**Preconditions (check before Task 1; stop and report if any fails):**
- Phases 2–8 are merged into the base branch, **including Phase 8's Task 16** (`internal/version/version.go` says `0.8.0`, `cmd/herdr-dots/memwire_test.go` has `TestVersionIsPhaseEight`, and the README has Phase 8's status block and `### Proactive dots (0.8.0)` section) and Phase 8's final fix wave. This plan was reconciled against `feat/dots-v1` at `c4d3827` (every Phase 8 code task merged, version still 0.7.0): every task was replayed in wave order and passed `gofmt -l internal cmd`, `go vet`, `go test -race ./...`, the Linux build, the Windows build and vet, and `GOOS=windows go test -c`; Task 12 was replayed on top of a simulation of Phase 8 Task 16's version and README edits. If Phase 8's fix wave moved a line a Find block quotes, make the same change by hand: every block shows the exact lines to add, and anchors are whole lines or symbols.
- **Names this plan uses from Phases 2–8 (all verified at `c4d3827`):**
  - Phase 2: `memory.Store.mutate`, `insert` (results `inserted`, `merged`, Phase 8's `yielded`), `dotFor`, `prepare`, `Candidates`, `ScanSecrets`, `ExecGit` (through `platform.CommandContext` and `ChildEnv`), `ResolveProject`; `engine.suppressedBy`, `spawnOutcomeOf` (`spawnOutcome` wraps it); `cmd/herdr-dots` `memoryStoreFor`, `resolveProject`, `splitCSV`.
  - Phase 3: `dots.ReservedSlug`; `runOpts.noForce`, `sink`; `queue.KindCouncil`, `Card.Parts`; `startCouncil(ctx, paneID, question, slugs, result, admit)` applies its eligibility rules to an explicit list (`councilPlan` → `Preflight.Select`).
  - Phase 4: `dots.BackendSpec.BaseURL`; `backend.Result.CostUSD`, `CostKnown`; `engine.deltaOf`, `redactErr`, `costDetail`, `withCost`; `credentials.Redact(s string, known ...string) string` (variadic: called, never assigned to a `func(string) string`), which covers provider keys and GitHub, AWS, Slack, Stripe, GitLab and URL credentials.
  - Phase 5: `engine.askable` (`ask.go`), `AskCouncil` (`ask_council.go`), `ErrNotAttached`, `runOpts.ask`, `state.OutcomeAsked`, `memory.SourceAgent` (memui and `author` have its case).
  - Phase 6: `lockfile.Acquire`; `platform.SecureDir` in `paths.Ensure`; `platformtest.AssertPrivateFile`/`AssertPrivateDir`/`Abs`; `style.Sym`; the portability guard.
  - Phase 7: `dots.CapabilitySpec{Write, WriteTimeoutSeconds, MaxPatchBytes, MaxFiles}` in `internal/dots/capabilities.go`, `DefaultCapabilities()`, `CanWrite`; `engine.runDraft`, `patchSent` (records "sent" itself), `ApplyPatch`/`DiscardPatch` → `spawnOutcomeOf(…, memory.OutcomeApplied|OutcomeDiscarded, secret)`; patchstore `Meta.Summary` (redacted) and `SummaryHadSecret`; `view.LibraryDot.Write`/`WriteTimeoutSeconds`; the write-tool AST guard `TestOnlyDraftsAskForWriteTools` (no assignment to a `.Write` field outside `internal/engine/draft.go`, exactly one `req.Pass = backend.PassResearch` in `background.go`).
  - Phase 8: `dots.ResearchSpec{Enabled, Web, Topics, …}`, `ScheduleSpec{Cron, Task, Projects, CatchUpHours}`, `TriggerSpec.OnCommit`, `ScheduleAttached`, `Dot.Proactive()`; `engine.runBackground(j bgJob)` (`j.kind` = `state.RunResearch|RunScheduled|RunCommit`), `bgFeed`, `bgFailed`, `keepPane`, `e.plan`, `e.gens`; `queue.KindDigest` (Dot `"away"`, one part per dot); `dropPane` orphans cards under `e.mu`; `config.ProactiveEnabled` on the fail-closed line; `ReloadDots` resets `e.plan`.
- `GOTOOLCHAIN=local go vet ./... && GOTOOLCHAIN=local go test -race ./...` passes on the base.

## Global Constraints

Phase 9 constraints:
- **The product is NOT done after Phase 9.** The version becomes `0.9.0` (`internal/version/version.go` and `herdr-plugin.toml`). Never call the project done, finished, complete or ready in code, docs, commits or reports; feature complete is defined only by Phase 10's final checklist. The README keeps "scaffold … not done" and the roadmap keeps Phase 2 marked **VITAL**.
- **Phase 2 memory is VITAL and this phase must respect it:** memory text goes only in the USER prompt (`backend.UserPrompt`); model writes keep the op rules (project scope proposed until approved, models never write dismissed, preference or global entries); only outcome and user entries suppress repeats; secrets are scanned (`memory.ScanSecrets`) on every path that stores or exports memory.
- **Shared content is someone else's work** (binding decision #17 and the Phase 2 final review): imported dots and untrusted or partly trusted project dots run with `delivery.mode` inject, `capabilities.write`, `capabilities.mcp`, `research.enabled`, `research.web`, `schedule.cron`, `trigger.on_commit` and `backend.base_url` turned off until the developer keeps each one, and denied backend args (`dots.DeniedArgs`) and absolute `schedule.projects` paths are always removed. Imported memory is labelled "imported", never "developer"; it never suppresses repeats; **every imported entry (dot and project scope) lands proposed** and stays out of prompts until approved (Plan decision 26). An untrusted or changed project dot never runs, in a turn review or in the background (Plan decision 30).
- Trust is per file content (SHA-256 of the exact bytes parsed) and per project (git root commit, ruling 11). Any change makes a file untrusted again and pauses attached copies.
- Exports never contain credentials, history or global memory; memory in exports passes the secret scanner; imports never overwrite an existing dot.
- History stays on this machine and holds only 300-rune excerpts, sanitized (`textutil.SanitizeModelText`, which strips bidi controls) and redacted (`credentials.Redact`, the history store's default). It is cross-session safe: a per-file lock, atomic trims, `0600` files.
- **Write tools stay in drafts** (Phase 7's AST guard): no Phase 9 code assigns to a `.Write` field or builds a `backend.Request` with `Write`/`Pass`; `dots.ClampDot` rebuilds `CapabilitySpec` instead (Plan decision 25).
- **Every child process goes through `platform.CommandContext`/`ChildEnv`** (the portability guard): Phase 9 runs git only through Phase 2's `memory.ExecGit` (`ResolveRoot`), and fetches packs with `net/http`.
- Model text is sanitized before display, storage or delivery (`textutil.SanitizeModelText`, `style.Clean`).
- **Engine lock order:** paneLock → e.mu → stores; dmu → e.mu/stores. No history, trust, project-file, git or network I/O under paneLock, e.mu or dmu. Background work goes through `e.spawn` (drained by `Engine.Wait`).
- **UI rule:** never act on an unseen selection. Import and trust previews show every word (purpose, every memory entry, every capability) in a scrolling view, and `y` is refused until the end has been on screen. Selections are by slug, never by row index after a refresh.
- **Config:** `config.toml` reloads at runtime. The new safety switch `project_dots_enabled` defaults to true and is false on a malformed or unreadable config (fails closed).
- Files `0600`, directories `0700` (owner-only DACLs on Windows via Phase 6), atomic replaces (`fsutil.WriteFileAtomic`); history appends hold the per-file lock.
- **Live steps:** only `herdr --session dots-test`; never `herdr plugin link` (global); never type Claude slash commands such as `/model` into panes; snapshot and diff `~/.claude/settings.json` around every live run; never touch `~/.config/herdr/config.toml` (read only) or the user's sessions.
- Codex is not installed on this machine: nothing in this phase is Codex-specific; Codex paths stay "documented-format, not live-verified".
- **No outward-facing action:** no push, remote tag, publish, license or rename (user gates).
- Every task's verification runs, from the repo root: `gofmt -l internal cmd` (prints nothing), `GOTOOLCHAIN=local go vet ./...`, `GOTOOLCHAIN=local go test -race ./...`, `GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...`, `GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./...`, and `GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p9-win/" <the task's packages>`. Tests never touch the real `STATE_DIR`, `CONFIG_DIR` or Herdr; use `t.TempDir()`.

All earlier Global Constraints still apply (v1 through Phase 8): plugin id `herdr-dots`; module `herdr-dots`; dots are read-only outside Phase 7's explicit draft runs; child environments are stripped (`backend.CleanEnv`); keys never reach logs, the feed, snapshots, IPC, memory, history or argv; default delivery mode `queue`.

## Plan decisions

No human was available; every ambiguity below was resolved here. Each can be reversed.

1. **Project slugs are `proj--<file base>--<8 hex of the project id>`, shown as `proj:<file base>`.** The spec's `proj:<slug>` contains a colon, which Windows refuses in file names (history files, memory scope files) and memory's slug check refuses outright. `Slugify` never produces `--`, so no user dot can take a project slug; the 8 hex characters (from the git root commit, ruling 11) make the same file name in two repositories two different dots, so one global dot map keeps working and every later-phase lookup (`e.dots[slug]` in council, MCP, drafts, schedules) needs no change. `dots.DisplaySlug` gives the short form for people; IPC and files use the engine slug. `Validate` reserves the `proj--` prefix for `Origin == "project"`.
2. **Trust key is (project id, path relative to the root, SHA-256), not the absolute path (spec S8).** Ruling 11 makes clones and worktrees of one repository one project; the same bytes at the same place in any of them are the same thing the developer reviewed. A fork with identical bytes is the same content, so sharing trust with it is safe. The absolute path is kept in the entry for display. A trust file that can't be parsed leaves every project dot untrusted and is never overwritten (`ErrCorrupt`).
3. **Project dots need a git repository with commits (V2 answered).** Path identities (no git, no commits, shallow clones) have no stable project id, so they get no project dots. `engine.resolveRoot` accepts only `g-` ids.
4. **The clamp table is `dots.ClampDot`, one function for imports and project dots** (spec S5 plus binding decision #17; decision 27 adds the non-keepable `projects` clamp): `inject` (`delivery.mode` → `queue`), `write` (Phase 7 `capabilities.write`), `mcp` (new `capabilities.mcp`, default true so existing dots keep working; enforced in Phase 5's `askable` and `AskCouncil`), `research` and `web` (Phase 8 `research.enabled`, `research.web`), `schedule` (Phase 8 `schedule.cron` → `""`), `commit` (Phase 8 `trigger.on_commit`), `base_url` (Phase 4 `backend.base_url`, which would send prompts and, for OpenRouter, the API key to another host). Denied backend args are removed with `dots.SafeArgs` and can't be kept (`args`). If Phase 7 or 8 renamed a field, the rename happens in `ClampDot` and its test only.
5. **Unknown keys are refused in packs and project dot files** (`dots.DecodeStrict`, `dotpack.Parse`): a capability this version doesn't know can't be clamped. User dot files keep today's lenient decoding.
6. **Provenance and trust of shared content (the Phase 2 final review item).** Today `memory.author` labels `source = import` entries "developer". From this phase:
   - a. Imported entries render as `(imported)`: conventions and decisions as `convention (imported): …`, every other kind with a trailing ` (imported)`. The intro line is unchanged, so existing prompts and tests stay byte-identical.
   - b. Imported entries never suppress repeats: `suppressedBy` skips `source = import` for dismissed entries as well as findings (only outcome and user entries suppress, as Phase 2 requires).
   - c. Imported project entries land `proposed` even with `memory_auto_approve_project`, attributed to the importing dot, and stay out of every prompt (even that dot's) until approved; approval keeps `source = import`, so the label stays. The developer makes an entry their own by editing it in the memory browser, which already sets `source = user`. *(Extended by decision 26: imported dot-scope entries land proposed too.)*
   - d. Imported entries never carry the exporter's evidence (session, pane, turn, card) or pin, and never get the developer's +5 retrieval score.
   - e. Packs carry only convention, decision, finding, dismissed and note entries of dot and project scope; preferences (personal) and global memory never travel. Entries that look like secrets are refused on import, with no `--force`.
   - f. The memory browser marks imported rows `(imported)` and its approval screen says `proposed by <dot> (imported from a dot pack)`.
   - g. An imported dot is an ordinary user dot after the developer confirms the preview: its file keeps `origin = "import"` for the profile badge; the developer may edit it freely.
   - h. Project dots are the repository's: Herdr Dots never edits, deletes or re-modes them (`ErrProjectDot`); their memory is per project like any dot's; their model writes follow the same op rules. Cards from imported and project dots carry the dot's name like any other card; the library badge shows provenance. A repository could ship a dot with the same display name as one of yours; the trust preview and the `proj:` slug are how you tell them apart (recorded as a known limitation).
7. **One history record per run, written when the run ends** from a `runTrace` that the run marks next to each of its `e.feed`/`e.runFailed` calls, instead of a record per `feed` call (spec §4.2). A run has one outcome, and the trace also carries usage, cost and latency that `feed` never sees. `runDot` (reviews, summons, council members, MCP asks, memory passes), Phase 7's `runDraft` and Phase 8's `runBackground` each open a trace with their kind. A run that never reached its backend (cancelled while waiting for a slot, memory pass without a project) writes nothing. Background (Phase 8) cards are not linked to their run record; their resolutions still count.
8. **History joins on `CardKey(session, id)`** (`<SessionKey>/<id>`): queue card ids restart in every Herdr session. The join is two-pass, because an inject card is delivered (and resolved) while the run that made it is still finishing, so its resolve line can come first.
9. **Resolutions:** `sent`, `edited` (sent after an edit), `dismissed`, `applied`, `discarded` (Phase 7), `orphaned` (pane closed with the card waiting), plus `injected` (delivered by inject mode, never approved). Suppression has no card, so it is a run outcome, not a resolution record. Acceptance counts `sent + edited + applied` over those plus `dismissed`; `injected`, `discarded` and `orphaned` count for neither.
10. **Stats definitions:** `Runs` counts every run by kind; `Verdicts` counts non-memory runs ending flagged, pass or suppressed, and the flagged rate is `Flagged / Verdicts`; `Errors / TotalRuns` is the error rate; the median latency covers runs that didn't fail; cost sums known costs and counts unpriced runs separately. The hint "most expensive" needs a 50% share and at least $1 of this dot's own cost.
11. **V1 answered, the 20,000-line cap stays.** `history.Store.Profile` reads a file once for both windows and the rows: 35 ms per full 20,000-line file on this machine (Apple M4 Pro, `BenchmarkStats30d`). The cost share reads every dot's file (370 ms for ten full files), so the profile shows first and adds the cost hint when the share arrives.
12. **History files:** a per-file lock `<slug>.lock` plus a process mutex; a size check on the first append of each file in a process and every 500 appends after; a trim keeps the newest 15,000 lines and drops resolve lines whose run was cut.
13. **Engine dependencies** are `History`, `SessionKey`, `Trust` and `ResolveRoot`; the engine calls `projectdots.ScanProject` itself instead of the spec's `LoadProjectDots func(root)`, because the trust check needs the hash of the very bytes that were parsed. `ResolveRoot` is separate from Phase 2's `ResolveProject`, so with memory off the engine still never runs git for memory (Phase 2's test stays true); it runs git for project dots only when a trust store exists and `project_dots_enabled` is on.
14. **New safety switch `project_dots_enabled`** (default true, false on a malformed config), applied at runtime by the poll loop.
15. **Change detection by content.** The poll compares a fingerprint of each repository's project dot files built from their SHA-256 (files are at most 64 KB, a repository ships a handful) plus the trust file's hash, so an edit that keeps the size and modification time is still caught.
16. **IPC:** `TrustArgs` adds `PaneID` (which repository) and `SHA256` (the bytes the developer reviewed; a file changed since is refused) to the spec's `{Slug, Capabilities}`; `reload_dots` takes an optional `{pane_id}` so the manage popup can scan its target's repository before anything is attached (spec §6.4 scans only attached panes, which would hide a team's dots from a fresh pane). `Snapshot.PaneProjects` maps scanned panes to project ids, and the popup lists only the target repository's project dots.
17. **Origins:** `origin` is written in a user dot file only for `template` and `import`; a user origin is never written; a user file can't claim `project`. Profiles show the origin; packs never carry it.
18. **Pack format:** a header comment, `[pack]`, `[dot]` with `purpose` as a multi-line string before the first `[dot.` table, then `[[memory]]` (scope, kind, text, paths); entries over 500 runes are refused; `exported_at` is whole seconds.
19. **Export secrets:** the whole dot (name, purpose, backend args and URL) and every memory entry are scanned. The CLI has `--force`. The popup lists what matched by position (`purpose`, `memory 2`) and offers ctrl+e to export without the flagged memory entries, the bulk form of the spec's per-entry exclusion; a secret in the dot itself must be removed first.
20. **Import confirmation:** the CLI prints the full preview and needs `--yes` (or `y` at a terminal); a name collision always needs `--as`. The popup needs the preview read to the end, and `n` renames.
21. **Templates:** Accessibility, Performance, Docs, Internationalization, Dependency Auditor, API Design and Data Privacy (14 in total). Four manage tests navigated templates by position (three in `model_test.go`, and Phase 7's `TestSavingReservesALine` in `form_harden_test.go`); they now pick by name (`pickTemplate`).
22. **Shared text helpers** `style.Clean` and `style.Wrap` (new file `internal/ui/style/text.go`) serve the profile and the previews; memui keeps its own.
23. **Out of scope, unchanged:** committed project memory (V3, decision O4 a: recorded as a user question), registries and marketplaces, syncing memory, analytics leaving the machine.
24. **(reconciled) History redacts by default.** `credentials.Redact` is variadic (`Redact(s string, known ...string)`), so the old wiring `hs.Redact = credentials.Redact` did not compile. `history.Store.Redact` is now an optional override and a nil one means `credentials.Redact`, which covers provider keys and GitHub, AWS, Slack, Stripe, GitLab and URL credentials; a caller that forgets to wire anything still never writes a key. `historyStoreFor` is just `history.Open(p.HistoryDir())`. New test `TestDefaultRedactionNeverStoresKeys` (keys and a bidi control never reach the file).
25. **(reconciled) `ClampDot` never assigns to `.Write`.** Phase 7's write-tool guard (`TestOnlyDraftsAskForWriteTools`) refuses any assignment to a `.Write` field outside `internal/engine/draft.go`, and `d.Capabilities.Write = false` failed it. `ClampDot` rebuilds the struct (`readOnly(c)`), and `TestClampKeepsEveryOtherCapabilityField` fails if a field added later is dropped. Task 2's test step also runs the guard.
26. **(reconciled) Every imported memory entry lands proposed, dot scope included** (amends spec S10 and decision 6c). An imported dot-scope `dismissed` line ("never flag SQL injection") would otherwise reach the dot's prompt unreviewed and silence it. Phase 2's `prepare` allowed proposals in project scope only; it now also allows them for `scope = dot, source = import`, and nothing else changes: `eligible` and `Candidates` already keep dot-scope proposals out of prompts and suppression candidates, and the memory browser's `a` and `herdr-dots memory approve` approve any proposal by ref. After approval the entry keeps `source = import`, so it is still labelled and still never suppresses (Task 7's test now approves the entry first). CLI and popup texts say every imported entry waits for approval. New test `TestOnlyImportsProposeDotMemory`.
27. **(reconciled) Phase 8's free text and paths in shared dots.** `ClampDot` always removes absolute `schedule.projects` paths (new non-keepable clamp `CapProjects`): they name directories on the author's machine, or a guess at yours, and keeping `schedule` never keeps them. `dotpack.Export` also scans `research.topics` and `schedule.task` for secrets, since they travel in the pack. `dotpack.Fetch` reports the final URL with any `user:password` masked (`URL.Redacted`). New tests `TestClampRemovesAbsoluteScheduleProjects`, the proactive-text case in `TestExportRefusesSecretsUnlessForced`, and `TestProactiveAndWriteFieldsStayOffUntilGranted` (Task 5).
28. **(reconciled) History hooks follow the merged code.** Resolutions hook into Phase 7's `spawnOutcomeOf` (which `spawnOutcome`, `ApplyPatch` and `DiscardPatch` call), plus `deliver` for inject cards (`injected`) and Phase 7's `patchSent` path (`sent`, which records its memory outcome without `spawnOutcome`). A council card or an away digest (Phase 8, Dot `"away"`) resolves for each dot that contributed a part, never for `council`/`away`. Run cost provenance follows Phase 4's `deltaOf`. An MCP ask (feed outcome `asked`) is recorded as `flagged` or `pass` so the stats count it. `runBackground` opens its trace once every gate has passed and the slot is claimed, and marks a record only where `bgFeed` wrote the feed line; a draft or a research/commit job whose pane closed ends without a record, as it ends without a feed line. Error marks use `redactErr`. New tests `TestDraftRunAndPatchOutcomesAreRecorded`, `TestScheduledRunIsRecorded`, `TestMultiDotCardsResolveForEachContributor`; `TestBackgroundKindsAreHistoryKinds` moved into Step 1.
29. **(reconciled) `dropPane` (Phase 8) orphans cards under `e.mu`.** Task 7 reads `Queue.Pending(paneID)` in the same section, right before `OrphanPane` (`e.mu → stores`), and spawns the `orphaned` resolutions after the unlock; Task 9 also forgets the pane's repository there (`paneRoots`), so a reused pane id is scanned again.
30. **(reconciled) An untrusted or changed project dot never runs, in the background either.** Phase 8's jobs and Phase 3's council members carry a copy of the dot, so a run queued before its file changed could still start. `runDot` (after it takes a run slot) and `runBackground` (before it claims a slot) call `projectDotGone(d)`: a project dot no longer loaded exactly as it was (`reflect.DeepEqual` with `e.dots`) is skipped with a log line and no record. Proactive scheduling itself only sees `e.dots`, which holds trusted, clamped project dots. `scanRoot` also drops the schedule plans of a repository's dots when its files or trust change, so a newly granted or changed cron is planned afresh. New test `TestARevokedProjectDotsQueuedRunNeverStarts`.
31. **(reconciled) The MCP clamp in Phase 5's real files.** `askable` lives in `ask.go` and ends `return d, err` after the blocked check; the `capabilities.mcp` check goes right before that return. `AskCouncil` lives in `ask_council.go`; it now passes the non-empty list of MCP-allowed members when any attached dot lacks the capability (`startCouncil` applies its eligibility rules to an explicit list through `councilPlan`'s `Preflight.Select`), and its doc comment says so.
32. **(reconciled) Previews show Phase 8's text.** The popup's trust preview and `herdr-dots dots trust` show the whole file (research topics, a schedule task, backend args and any key a later field adds are prompt-bearing or risky, and trust covers every byte); import previews (CLI and popup) add research topics, the schedule task and backend args next to the purpose. Task 11's trust test scrolls further for the longer preview.
33. **(reconciled) Phase 7 and 8 snapshot fields.** `Snapshot` keeps Phase 7's `Write`/`WriteTimeoutSeconds` on library entries, and project library entries fill them from the clamped dot (so a write grant shows); `view.Snapshot.PaneProjects` goes after Phase 8's daily fields; `config.ProjectDotsEnabled` is turned off on Phase 7/8's combined fail-closed lines. The manage popup keeps Phase 4's providers screen and `K providers` help; `memDeletedMsg` is Phase 2's `{name, del}`. The profile's memory line counts Phase 8's research and question kinds, and its ellipses go through Phase 6's `style.Sym`.
34. **(reconciled) Task 12 against the post-Phase-8 README.** The version pin moves first (`TestVersionIsPhaseEight` → `TestVersionIsPhaseNine`, test first), the manifest edit anchors on `name`/`version` (a bare `version = "0.8.0"` also matches `min_herdr_version`), and the six README edits anchor on Phase 8 Task 16's status block, `### Proactive dots (0.8.0)`, its last `## Files`, privacy and known-gaps lines, and its `| 9–10 |` roadmap row. The roadmap spec is not edited: its phase table has no status column, and Phases 2–8 left it alone. The wording check is Phase 8's.
35. **(reconciled) Verification.** `gofmt -l internal cmd` replaces `gofmt -l .`; the Windows build, vet and `go test -c` join every task's checks (Phase 6 and 8 practice). Every prose-only edit of the old plan ("add after it", "replace every …", "add imports") became a Find/Replace block or an explicit import line, so the plan can be replayed mechanically.

## Review Focus

The five inputs the spec implies but its test list doesn't exercise, most likely to bite first. Each has a test in its owning task.

1. **A repository shipping `.herdr-dots/dots/helper.toml` with `mode = "inject"` (or a symlink, an unknown `[capabilities] shell = true`, or a 1 MB file).** Expect it listed as `⚠ project · untrusted`, refused on attach with `ErrUntrusted`, every requested capability off in the trust preview, and the link, unknown key and oversized file refused outright. Tests: `TestLoadProjectRefusesUnknownKeysLinksBigFilesAndBadNames`, `TestLoadProjectRefusesALinkedDotsDirectory` (Task 2), `TestUntrustedFileIsClampedAndListsWhatItAsks` (Task 5), `TestUntrustedProjectDotIsListedButCantAttach`, `TestTrustedWithoutCapabilitiesRunsClamped` (Task 9), `TestTrustPreviewNeedsTheWholeFileAndSendsTheHash` (Task 11).
2. **A trusted project dot edited (same size, same second) by a teammate's commit while attached, or edited between the preview and `y`.** Expect the attached copy paused with one toast, the edited file untrusted, and a trust request for stale bytes refused. Tests: `TestAnyEditRevokesTrustAndChangesTheStamp` (Task 5), `TestEditedTrustedDotIsPausedWithAToast`, `TestTrustRefusesAFileChangedSinceReview` (Task 9).
3. **An import from a URL that redirects to a 50 MB file or to `http://`.** Expect a refusal naming the reason, the final URL shown for a good redirect. Test: `TestFetchHTTPSOnlyCappedAndReportsTheFinalURL` (Task 6).
4. **Importing "Security" when you already have a Security dot.** Expect a request for a new name and the original untouched. Tests: `TestExportImportRoundTripNeverOverwrites` (Task 10), `TestImportPreviewCollisionRenameAndClamps` (Task 11).
5. **A pack's memory treated as the developer's own.** Expect `(imported)` labels, no suppression from an imported "don't raise this" (even once approved), and every imported entry, dot scope included, out of prompts until approved. Tests: `TestImportedMemoryIsLabelledNeverDeveloper`, `TestImportedProposalsStayOutOfPrompts`, `TestOnlyImportsProposeDotMemory` (Task 4), `TestImportedDismissedMemoryNeverSuppresses` (Task 7).

Also covered: an inject card resolved before its run record (`TestResolutionWrittenBeforeItsRunStillJoins`, Task 1), keys in model text (`TestDefaultRedactionNeverStoresKeys`, Task 1), a busy month (`BenchmarkStats30d`, `TestTrimKeepsTheNewestLinesAndTheirResolutions`, Task 1), a repository dot asking for Phase 7 and 8 powers (`TestProactiveAndWriteFieldsStayOffUntilGranted`, Task 5; `TestClampRemovesAbsoluteScheduleProjects`, Task 2), a project dot revoked while a run was queued (`TestARevokedProjectDotsQueuedRunNeverStarts`, Task 9), drafts, patches, scheduled runs and digests in history (`TestDraftRunAndPatchOutcomesAreRecorded`, `TestScheduledRunIsRecorded`, `TestMultiDotCardsResolveForEachContributor`, Task 7), and a malformed config (`TestProjectDotsSwitch`, Task 9).

---

## Parallel execution: dependencies and waves

Each task runs in its own git worktree branched from the wave's base (the base branch with Phases 2–8 merged, plus every earlier wave). Merge every task of a wave, in task order, before starting the next wave. Tasks in the same wave never touch the same file.

| Task | Title | Depends on | Wave |
|---|---|---|---|
| 1 | `internal/history` and the history and trust paths | — | 1 |
| 2 | Dot origins, project dot files, `ClampDot`, `Encode` | — | 1 |
| 3 | Seven new templates | — | 1 |
| 4 | Memory provenance: `ImportPack` and "imported" labels | — | 1 |
| 5 | `internal/projectdots`: trust store and scan | Task 2 | 2 |
| 6 | `internal/dotpack`: export, parse, prepare, fetch | Task 2, Task 4 | 2 |
| 7 | Engine history hooks | Task 1, Task 4 | 2 |
| 8 | Profile screen (`internal/ui/profile`) | Task 1, Task 2 | 2 |
| 9 | Engine project dots, trust IPC, MCP clamp, wiring | Task 2, Task 5, Task 7 | 3 |
| 10 | `herdr-dots dots` CLI | Task 1, Task 2, Task 5, Task 6 | 3 |
| 11 | Manage popup: profile, import, export, trust | Task 3, Task 6, Task 8, Task 9 | 4 |
| 12 | README, notes, version 0.9.0 | all (and Phase 8 Task 16) | 5 |

**Wave schedule:** W1 = {1, 2, 3, 4} → W2 = {5, 6, 7, 8} → W3 = {9, 10} → W4 = {11} → W5 = {12}.

**Files per task** (disjoint within each wave):
- Task 1: `internal/history/history.go`, `stats.go`, `history_test.go`, `xproc_test.go`, `bench_test.go`; `internal/paths/paths.go`, `internal/paths/history_test.go`.
- Task 2: `internal/dots/dot.go`, `store.go`, `templates.go`, `project.go`, `clamp.go`, `project_test.go`, `clamp_test.go`.
- Task 3: `internal/dots/templates/{accessibility,performance,docs,i18n,dependency-auditor,api-design,data-privacy}.toml`, `internal/dots/templates_test.go`, `internal/ui/manage/model_test.go`, `internal/ui/manage/form_harden_test.go` (Phase 7's position-based pick).
- Task 4: `internal/memory/importpack.go`, `importpack_test.go`, `select.go`, `store.go`; `internal/ui/memui/model.go`, `internal/ui/memui/provenance_test.go`.
- Task 5: `internal/projectdots/trust.go`, `scan.go`, `projectdots_test.go`.
- Task 6: `internal/dotpack/dotpack.go`, `fetch.go`, `dotpack_test.go`.
- Task 7: `internal/engine/history.go`, `history_test.go`, `engine.go`, `deliver.go`, `memory.go`; Phase 7's `draft.go` (`runDraft`) and Phase 8's `background.go` (`runBackground`).
- Task 8: `internal/ui/style/text.go`, `text_test.go`; `internal/ui/profile/model.go`, `model_test.go`; `internal/view/view.go` (one field: `LibraryDot.Origin`).
- Task 9: `internal/engine/projectdots.go`, `projectdots_test.go`, `engine.go`, `control.go`, `deliver.go`, `handle.go`, Phase 5's `ask.go` (`askable`) and `ask_council.go` (`AskCouncil`), Phase 8's `background.go` (`runBackground` gate); `internal/ipc/ipc.go`; `internal/view/view.go`; `internal/config/config.go`, `config_test.go`, `projectdots_test.go`; `cmd/herdr-dots/engine.go`.
- Task 10: `cmd/herdr-dots/dots.go`, `dots_test.go`, `main.go`.
- Task 11: `internal/ui/manage/model.go`, `pager.go`, `trust.go`, `share.go`, `share_test.go`; `cmd/herdr-dots/ui.go`.
- Task 12: `README.md`, `docs/superpowers/notes/2026-10-08-phase-9-verification.md`, `internal/version/version.go`, `herdr-plugin.toml`, `cmd/herdr-dots/memwire_test.go`.

**Test-name hygiene:** helpers added here, per package: `history`: `hNow`, `hstore`, `runRec`, `resRec`, `mustAppend`, `fillHistory`. `dots`: `projID`, `writeProjectDot`, `a11yTOML`, `riskyDot`. `memory`: `packEntries`. `memui`: none (uses `open`, `press`). `projectdots`: `pdProject`, `helperTOML`, `pdRepo`, `pdTrust`. `dotpack`: `dpNow`, `dpDot`, `dpMem`. `engine`: `histRig`, `histRuns`, `repoProject`, `helperSlug`, `repoHelperTOML`, `projRig`, `newProjRig`, `(*projRig).libDot`, `(*projRig).trustAs`. `config`: none. `style`: none. `profile`: `pNow`, `pPID`, `pdrive`, `pkey`, `ppress`, `fixture`, `openProfile`. `manage`: `pickTemplate` (Task 3), `shareHelperTOML`, `shareLib`, `sharePress`, `shareDeps`. `cmd/herdr-dots`: `dotsTestPID`, `dotsRig`, `newDotsCLI`, `(*dotsRig).run`, `(*dotsRig).saveDot`. Product names that could meet a later phase's: engine `runTrace`, `trace`, `verdictOutcome`, `resolutionFor`, `spawnResolve`, `projectState`, `projectDotGone`, `knowPane`, `scanRoot`, `rescanProjects`, `pauseRevoked`, `findProjectItem`, `untrustedLocked`, `projectLibraryLocked`, `paneProjectsLocked`, `resolveRoot`, `rebuildLocked`, `projectDotsOn`, `mcpMembers`, `ErrUntrusted`, `ErrProjectDot`; dots `readOnly`, `CapProjects`; `cmd/herdr-dots` `dotsCLI`, `runDots`, `parseAnywhere`, `indentBlock`, `historyStoreFor`. (Reconcile check at `c4d3827`: none of these exists in Phases 2–8, and the new tests' names don't collide.) Before adding any of them, run `grep -rn "func <name>\b\|type <name>\b" <package dir>`; if Phases 2–8 already define it, add the suffix `P9` everywhere this plan uses it.

### Shared files with Phases 2–8

Phases 2–8 merge before Phase 9, so these files already carry their changes. Edits below are **Find / Replace** blocks reconciled against the merged Phase 2–8 code (`c4d3827`); only Phase 8's final fix wave and its Task 16 can still move a line, and then you make the same change by hand: every block shows the exact lines to add.

| File | Earlier phases | Phase 9 task |
|---|---|---|
| `internal/dots/dot.go` | 2 `[memory]`, 4 backend fields, 7 `[capabilities]`, 8 `[research]`/`[schedule]`/`on_commit` | 2 |
| `internal/dots/store.go` | — | 2 |
| `internal/memory/select.go` | 2 (created) | 4 |
| `internal/ui/memui/model.go` | 2 (created) | 4 |
| `internal/engine/engine.go` | 2, 3 council, 4 cost, 5 ask, 7 draft, 8 proactive | 7, then 9 |
| `internal/engine/deliver.go`, `memory.go`, `control.go`, `handle.go` | 2, 3, 5, 7, 8 | 7 (`deliver.go`, `memory.go`), 9 (`deliver.go`, `control.go`, `handle.go`) |
| `internal/ipc/ipc.go`, `internal/view/view.go` | 2, 3, 4, 5, 7, 8 | 9 (`view.go` also 8: one field) |
| `internal/engine/draft.go` (Phase 7), `background.go` (Phase 8), `ask.go` and `ask_council.go` (Phase 5) | created there | 7 (`draft.go`, `background.go`), 9 (`ask.go`, `ask_council.go`, `background.go`) |
| `internal/memory/store.go` | 2 (created), 5, 8 | 4 (`prepare`) |
| `internal/ui/manage/form_harden_test.go` | 7 | 3 |
| `cmd/herdr-dots/memwire_test.go` | 2, 7, 8 (version pin) | 12 |
| `internal/config/config.go`, `config_test.go` | 2, 3, 4, 5, 7, 8 | 9 |
| `cmd/herdr-dots/engine.go`, `ui.go`, `main.go` | 2–8 | 9, 11, 10 |
| `internal/ui/manage/model.go`, `model_test.go` | 2, 4, 7, 8 | 11, 3 |
| `internal/paths/paths.go` | 2, 4, 6, 7, 8 | 1 |
| `README.md`, version, manifest | every phase | 12 (the roadmap spec is not edited) |

## File Structure

```
internal/history/history.go         (new)  Record, Store, Append, Query, Trim, Slugs, CardKey, kinds, resolutions
internal/history/stats.go           (new)  Stats (rates), Store.Stats, Store.Profile (one read), Store.CostShare
internal/paths/paths.go             (mod)  HistoryDir, TrustFile; Ensure creates HistoryDir
internal/dots/dot.go                (mod)  Dot.Origin, Dot.SourcePath, CapabilitySpec.MCP (default true), origin/slug checks in Validate
internal/dots/project.go            (new)  origins, ProjectPrefix, ProjectTag/Slug/Split/Display, ReadProjectFiles, DecodeProject, DecodeStrict, LoadProject
internal/dots/clamp.go              (new)  capability names, Clamp, ClampDot, Requested, CheckCapabilities
internal/dots/store.go              (mod)  user origin on load, Encode, PurposeBlock
internal/dots/templates.go          (mod)  templates get Origin "template"
internal/dots/templates/*.toml      (new)  accessibility, performance, docs, i18n, dependency-auditor, api-design, data-privacy
internal/memory/importpack.go       (new)  ImportKinds, Store.ImportPack
internal/memory/select.go           (mod)  "(imported)" labels; imported proposals out of prompts
internal/ui/memui/model.go          (mod)  "(imported)" rows, provenance on approval
internal/projectdots/trust.go       (new)  TrustStore (Check, Trust, Revoke, List), Sum, Hex, ErrCorrupt
internal/projectdots/scan.go        (new)  Item, Scan, ScanProject, Stamp
internal/dotpack/dotpack.go         (new)  Pack, Meta, Export, Parse, Prepare, SecretError, errors
internal/dotpack/fetch.go           (new)  Fetch (https only), SelectMemory (S9)
internal/engine/history.go          (new)  runOpts.historyKind, runTrace, resolutionFor, spawnResolve
internal/engine/projectdots.go      (new)  project dot state, scan/rescan, pause on change, Trust/Untrust, library, mcpMembers
internal/engine/engine.go           (mod)  Deps, runOpts.kind/trace, Engine fields, runDot marks, route card link, ReloadDots merge, Attach checks, Snapshot
internal/engine/{deliver,memory,control,handle}.go (mod) resolutions, import never suppresses, poll rescan, set-mode refusal, trust IPC
internal/ipc/ipc.go                 (mod)  CmdTrust, CmdUntrust, TrustArgs, ReloadArgs
internal/view/view.go               (mod)  LibraryDot origin/trust fields, Snapshot.PaneProjects
internal/config/config.go           (mod)  ProjectDotsEnabled
internal/ui/style/text.go           (new)  Clean, Wrap
internal/ui/profile/model.go        (new)  profile screen, Hints, Summarize
internal/ui/manage/{pager,trust,share}.go (new) review pager, trust preview, profile/import/export screens
internal/ui/manage/model.go         (mod)  library filter and badges, i/I/T keys, new screens, Init scan
cmd/herdr-dots/dots.go              (new)  herdr-dots dots list|export|import|trust|untrust
cmd/herdr-dots/{main,engine,ui}.go  (mod)  dispatch, engine deps, manage deps
```

**Common verification block** (the "full checks" step of every task):

```bash
gofmt -l internal cmd
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 go build ./...
GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go build ./... && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go vet ./...
mkdir -p "${TMPDIR:-/tmp}/p9-win" && GOTOOLCHAIN=local GOOS=windows GOARCH=amd64 go test -c -o "${TMPDIR:-/tmp}/p9-win/" ./...
```

Expected: `gofmt` prints nothing; every other command exits 0 (`go test` prints only `ok` and `[no test files]` lines). (`gofmt -l .` would also list the `.claude/worktrees` checkouts some setups keep inside the repository.)

---
### Task 1: `internal/history` and the history and trust paths

A persistent, per-dot record of every run and card resolution, with stats computed on demand (spec S1, S2, §5.1), and the two new paths.

**Depends on:** none. Wave 1 (runs beside Tasks 2, 3 and 4).

**Files:**
- Create: `internal/history/history.go`, `internal/history/stats.go`
- Create: `internal/history/history_test.go`, `internal/history/xproc_test.go`, `internal/history/bench_test.go`
- Modify: `internal/paths/paths.go` (two methods; `Ensure` list)
- Create: `internal/paths/history_test.go`

**Interfaces:**
- Consumes: `lockfile.Acquire(path string, timeout time.Duration) (*lockfile.Lock, error)`, `lockfile.ErrLocked`; `fsutil.WriteFileAtomic`; `textutil.SanitizeModelText` (strips bidi controls since Phase 8); `credentials.Redact(s string, known ...string) string` (variadic, so it is called, never assigned to the `func(string) string` field); `state.OutcomeFlagged`, `OutcomePass`, `OutcomeError`, `OutcomeSuppressed`; Phase 6 `platformtest.AssertPrivateFile`, `AssertPrivateDir`.
- Produces (package `history`):
  - consts `TypeRun = "run"`, `TypeResolve = "resolve"`; kinds `KindReview`, `KindSummon`, `KindCouncil`, `KindAsk`, `KindResearch`, `KindScheduled`, `KindCommit`, `KindDraft`, `KindMemory` (values `review`, `summon`, `council`, `ask`, `research`, `scheduled`, `commit`, `draft`, `memory`); resolutions `ResSent`, `ResEdited`, `ResDismissed`, `ResApplied`, `ResDiscarded`, `ResInjected`, `ResOrphaned`; `DefaultMaxLines = 20000`, `DefaultTrimTo = 15000`, `DefaultTrimEvery = 500`, `ExcerptRunes = 300`; `ErrInvalid`, `ErrLocked`.
  - `type Record struct{ T string; At time.Time; Dot, Project, Pane, Kind string; Turn int; Outcome, Severity, Card, Resolution, Excerpt string; CostUSD float64; CostUnknown bool; In, Out int; Ms int64 }` (JSON `t`, `at`, `dot`, `project`, `pane`, `kind`, `turn`, `outcome`, `severity`, `card`, `resolution`, `excerpt`, `cost_usd`, `cost_unknown`, `in`, `out`, `ms`).
  - `func CardKey(session, id string) string` (`session + "/" + id`; `""` for no id); `func ValidSlug(slug string) bool`.
  - `type Store struct{ Dir string; MaxLines, TrimTo, TrimEvery int; LockTimeout time.Duration; Redact func(string) string /* nil = credentials.Redact */; Now func() time.Time }`; `func Open(dir string) *Store`; `(*Store) Append(r Record) error`; `(*Store) Query(slug string, since time.Time, limit int) ([]Record, error)` (runs newest first, `Resolution` joined); `(*Store) Trim(slug string) error`; `(*Store) Slugs() ([]string, error)`.
  - `type Stats struct{ Runs map[string]int; Verdicts, Flagged, Suppressed, Errors, Resolved, Accepted, Dismissed int; CostUSD float64; CostUnknown int; MedianMs int64; Skipped int }` with `TotalRuns() int`, `FlaggedRate() float64`, `AcceptanceRate() float64`, `ErrorRate() float64`; `(*Store) Stats(slug string, now time.Time, window time.Duration) (Stats, error)`; `type Profile struct{ Week, Month Stats; Rows []Record }`; `(*Store) Profile(slug string, now time.Time, rows int) (Profile, error)`; `(*Store) CostShare(slug string, now time.Time, window time.Duration) (share, total float64, err error)`.
  - `(paths.Paths) HistoryDir() string` (`StateDir/history`), `(paths.Paths) TrustFile() string` (`ConfigDir/trusted.json`); `Ensure` creates `HistoryDir` private.

- [ ] **Step 1: Write the failing tests**

Create `internal/history/history_test.go`:

```go
package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform/platformtest"
	"herdr-dots/internal/state"
)

var hNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func hstore(t *testing.T) *Store {
	t.Helper()
	s := Open(filepath.Join(t.TempDir(), "history"))
	s.Now = func() time.Time { return hNow }
	return s
}

func runRec(slug, kind, outcome, card string, ago time.Duration) Record {
	return Record{T: TypeRun, At: hNow.Add(-ago), Dot: slug, Kind: kind, Outcome: outcome, Card: card, Pane: "w1:p1", Turn: 3}
}

func resRec(slug, card, res string, ago time.Duration) Record {
	return Record{T: TypeResolve, At: hNow.Add(-ago), Dot: slug, Card: card, Resolution: res}
}

func mustAppend(t *testing.T, s *Store, rs ...Record) {
	t.Helper()
	for _, r := range rs {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppendQueryNewestFirstWithResolution(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s,
		runRec("security", KindReview, state.OutcomeFlagged, "s1/1", 3*time.Hour),
		runRec("security", KindSummon, state.OutcomeFlagged, "s1/2", 2*time.Hour),
		resRec("security", "s1/1", ResDismissed, time.Hour),
		runRec("security", KindReview, state.OutcomePass, "", 30*time.Minute),
	)
	got, err := s.Query("security", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Outcome != state.OutcomePass || got[2].Card != "s1/1" {
		t.Fatalf("order: %+v", got)
	}
	if got[2].Resolution != ResDismissed || got[1].Resolution != "" {
		t.Fatalf("resolutions: %+v", got)
	}
	limited, _ := s.Query("security", hNow.Add(-150*time.Minute), 1)
	if len(limited) != 1 || limited[0].Outcome != state.OutcomePass {
		t.Fatalf("since/limit: %+v", limited)
	}
}

func TestResolutionJoinsTheLatestRunWithThatCardKey(t *testing.T) {
	s := hstore(t)
	// Card ids restart per session; the key carries the session, and a reused
	// key resolves the most recent run before it.
	mustAppend(t, s,
		runRec("security", KindReview, state.OutcomeFlagged, CardKey("s1", "1"), 3*time.Hour),
		resRec("security", CardKey("s1", "1"), ResSent, 170*time.Minute),
		runRec("security", KindReview, state.OutcomeFlagged, CardKey("s2", "1"), 2*time.Hour),
		resRec("security", CardKey("s2", "1"), ResDismissed, time.Hour),
	)
	got, _ := s.Query("security", time.Time{}, 0)
	if got[0].Resolution != ResDismissed || got[1].Resolution != ResSent {
		t.Fatalf("joins: %+v", got)
	}
	if CardKey("s1", "") != "" {
		t.Fatal("no card, no key")
	}
}

// An inject card is delivered (and resolved) while its run is still
// finishing, so its resolve line comes first.
func TestResolutionWrittenBeforeItsRunStillJoins(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s,
		resRec("fast", "s1/4", ResInjected, time.Minute),
		runRec("fast", KindReview, state.OutcomeFlagged, "s1/4", time.Minute),
	)
	if got, _ := s.Query("fast", time.Time{}, 0); len(got) != 1 || got[0].Resolution != ResInjected {
		t.Fatalf("join: %+v", got)
	}
}

func TestStatsMath(t *testing.T) {
	s := hstore(t)
	day := 24 * time.Hour
	r1 := runRec("security", KindReview, state.OutcomeFlagged, "s/1", day)
	r1.Ms, r1.CostUSD = 8000, 0.5
	r2 := runRec("security", KindReview, state.OutcomePass, "", day)
	r2.Ms, r2.CostUSD = 6000, 0.25
	r3 := runRec("security", KindCouncil, state.OutcomeFlagged, "s/2", 2*day)
	r3.Ms = 10000
	r4 := runRec("security", KindReview, state.OutcomeSuppressed, "", 2*day)
	r4.Ms = 7000
	r5 := runRec("security", KindMemory, state.OutcomePass, "", 2*day)
	r5.Ms = 1000
	r6 := runRec("security", KindReview, state.OutcomeError, "", 3*day)
	r6.Ms, r6.CostUnknown = 99999, true
	old := runRec("security", KindReview, state.OutcomeFlagged, "s/9", 40*day)
	mustAppend(t, s, r1, r2, r3, r4, r5, r6, old,
		resRec("security", "s/1", ResEdited, day),
		resRec("security", "s/2", ResDismissed, day),
		resRec("security", "s/3", ResApplied, day),
		resRec("security", "s/4", ResInjected, day),
		resRec("security", "s/9", ResSent, 39*day),
	)
	st, err := s.Stats("security", hNow, 30*day)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalRuns() != 6 || st.Runs[KindReview] != 4 || st.Runs[KindCouncil] != 1 || st.Runs[KindMemory] != 1 {
		t.Fatalf("runs %+v", st.Runs)
	}
	if st.Verdicts != 4 || st.Flagged != 2 || st.Suppressed != 1 || st.Errors != 1 {
		t.Fatalf("verdicts %+v", st)
	}
	if st.Resolved != 3 || st.Accepted != 2 || st.Dismissed != 1 {
		t.Fatalf("resolutions %+v", st)
	}
	if st.FlaggedRate() != 0.5 || st.AcceptanceRate() != 2.0/3.0 || st.ErrorRate() != 1.0/6.0 {
		t.Fatalf("rates %v %v %v", st.FlaggedRate(), st.AcceptanceRate(), st.ErrorRate())
	}
	if st.CostUSD != 0.75 || st.CostUnknown != 1 {
		t.Fatalf("cost %+v", st)
	}
	// Latencies of non-error runs: 1000 6000 7000 8000 10000 → median 7000.
	if st.MedianMs != 7000 {
		t.Fatalf("median %d", st.MedianMs)
	}
	week, _ := s.Stats("security", hNow, 7*24*time.Hour)
	if week.TotalRuns() != 6 || week.Resolved != 3 {
		t.Fatalf("7d %+v", week)
	}
	empty, _ := s.Stats("nobody", hNow, 30*day)
	if empty.TotalRuns() != 0 || empty.FlaggedRate() != 0 || empty.AcceptanceRate() != 0 || empty.MedianMs != 0 {
		t.Fatalf("empty %+v", empty)
	}
}

func TestCorruptLineIsSkippedAndCounted(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s, runRec("security", KindReview, state.OutcomeFlagged, "s/1", time.Hour))
	f, _ := os.OpenFile(filepath.Join(s.Dir, "security.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{not json\n{\"t\":\"weird\"}\n")
	f.Close()
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	st, err := s.Stats("security", hNow, 24*time.Hour)
	if err != nil || st.TotalRuns() != 2 || st.Skipped != 2 {
		t.Fatalf("stats %+v %v", st, err)
	}
}

func TestTrimKeepsTheNewestLinesAndTheirResolutions(t *testing.T) {
	s := hstore(t)
	s.MaxLines, s.TrimTo, s.TrimEvery = 10, 6, 1
	for i := 0; i < 8; i++ {
		mustAppend(t, s, runRec("security", KindReview, state.OutcomeFlagged, CardKey("s", string(rune('a'+i))), time.Duration(20-i)*time.Minute))
	}
	// "a" was run first; its resolution arrives last, after "a" is trimmed.
	mustAppend(t, s,
		resRec("security", CardKey("s", "h"), ResSent, 5*time.Minute),
		resRec("security", CardKey("s", "g"), ResDismissed, 4*time.Minute),
		resRec("security", CardKey("s", "a"), ResSent, 3*time.Minute),
	)
	data, _ := os.ReadFile(filepath.Join(s.Dir, "security.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 10 {
		t.Fatalf("%d lines after trim", len(lines))
	}
	got, _ := s.Query("security", time.Time{}, 0)
	if got[0].Card != CardKey("s", "h") || got[0].Resolution != ResSent || got[1].Resolution != ResDismissed {
		t.Fatalf("kept runs lost their resolutions: %+v", got)
	}
	for _, r := range got {
		if r.Card == CardKey("s", "a") {
			t.Fatal("the oldest run should have been trimmed")
		}
	}
	if err := s.Trim("security"); err != nil {
		t.Fatal(err)
	}
}

func TestAppendRefusesBadRecordsAndCleansExcerpts(t *testing.T) {
	s := hstore(t)
	s.Redact = func(x string) string { return strings.ReplaceAll(x, "sk-SECRET", "[redacted]") }
	if err := s.Append(Record{T: "other", Dot: "security"}); err == nil {
		t.Fatal("unknown type accepted")
	}
	if err := s.Append(Record{T: TypeRun, Dot: "../etc"}); err == nil {
		t.Fatal("path-like slug accepted")
	}
	long := "\x1b[31mkey sk-SECRET\x1b[0m\n" + strings.Repeat("é", 400)
	r := runRec("security", KindReview, state.OutcomeFlagged, "", time.Minute)
	r.Excerpt = long
	mustAppend(t, s, r)
	got, _ := s.Query("security", time.Time{}, 0)
	ex := got[0].Excerpt
	if strings.Contains(ex, "\x1b") || strings.Contains(ex, "sk-SECRET") || strings.Contains(ex, "\n") {
		t.Fatalf("excerpt not cleaned: %q", ex)
	}
	if n := len([]rune(ex)); n != ExcerptRunes {
		t.Fatalf("excerpt %d runes", n)
	}
}

// With no Redact of its own the store uses credentials.Redact, so a caller
// that forgets to wire one still never writes a key (GitHub, AWS, Slack,
// Stripe, GitLab, URL credentials, provider keys) or a bidi control.
func TestDefaultRedactionNeverStoresKeys(t *testing.T) {
	s := hstore(t)
	r := runRec("security", KindReview, state.OutcomeFlagged, "", time.Minute)
	keys := []string{
		"sk-ant-api03-abcdefghijklmnop",
		"ghp_" + strings.Repeat("a", 36),
		"AKIA" + strings.Repeat("B", 16),
		"xoxb-1234567890-abcdef",
		"sk_live_" + strings.Repeat("c", 24),
		"glpat-" + strings.Repeat("d", 20),
		"https://bob:hunter2pass@example.com/x",
	}
	r.Excerpt = "‮mix " + strings.Join(keys, " ")
	mustAppend(t, s, r)
	data, err := os.ReadFile(filepath.Join(s.Dir, "security.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range append(keys[:6:6], "hunter2pass", "‮", `‮`) {
		if strings.Contains(string(data), k) {
			t.Fatalf("history file holds %q: %s", k, data)
		}
	}
}

func TestCostShareAcrossDots(t *testing.T) {
	s := hstore(t)
	a := runRec("security", KindReview, state.OutcomePass, "", time.Hour)
	a.CostUSD = 3
	b := runRec("frontend", KindReview, state.OutcomePass, "", time.Hour)
	b.CostUSD = 1
	mustAppend(t, s, a, b)
	share, total, err := s.CostShare("security", hNow, 30*24*time.Hour)
	if err != nil || total != 4 || share != 0.75 {
		t.Fatalf("share %v total %v err %v", share, total, err)
	}
	if slugs, _ := s.Slugs(); len(slugs) != 2 {
		t.Fatalf("slugs %v", slugs)
	}
}

func TestProfileReadsBothWindowsAndRowsInOnePass(t *testing.T) {
	s := hstore(t)
	day := 24 * time.Hour
	mustAppend(t, s,
		runRec("security", KindReview, state.OutcomeFlagged, "s/1", 20*day),
		runRec("security", KindReview, state.OutcomeFlagged, "s/2", 2*day),
		resRec("security", "s/2", ResSent, day),
		runRec("security", KindAsk, state.OutcomePass, "", time.Hour),
	)
	p, err := s.Profile("security", hNow, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Week.TotalRuns() != 2 || p.Month.TotalRuns() != 3 || p.Week.Accepted != 1 {
		t.Fatalf("windows %+v / %+v", p.Week, p.Month)
	}
	if len(p.Rows) != 2 || p.Rows[0].Kind != KindAsk || p.Rows[1].Resolution != ResSent {
		t.Fatalf("rows %+v", p.Rows)
	}
}

func TestFilesArePrivate(t *testing.T) {
	s := hstore(t)
	mustAppend(t, s, runRec("security", KindReview, state.OutcomePass, "", time.Minute))
	platformtest.AssertPrivateFile(t, filepath.Join(s.Dir, "security.jsonl"))
}
```

Create `internal/history/xproc_test.go`:

```go
package history

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"herdr-dots/internal/state"
)

// Two engines (or an engine and the CLI) append to one dot's history from two
// processes: the lock must keep every line whole and lose none.
func TestCrossProcessAppendsLoseNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "history")
	const procs, each = 2, 150
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*bytes.Buffer, procs)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHistoryXProcChild$", "-test.count=1")
		cmd.Env = append(os.Environ(), "HISTORY_XPROC_DIR="+dir, "HISTORY_XPROC_ID="+strconv.Itoa(i), "HISTORY_XPROC_N="+strconv.Itoa(each))
		outs[i] = &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = outs[i], outs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("writer %d: %v\n%s", i, err, outs[i])
		}
	}
	st, err := Open(dir).Stats("security", time.Now().Add(time.Minute), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalRuns() != procs*each || st.Skipped != 0 {
		t.Fatalf("runs %d skipped %d, want %d and 0", st.TotalRuns(), st.Skipped, procs*each)
	}
}

// TestHistoryXProcChild is the writer process; it does nothing in a normal run.
func TestHistoryXProcChild(t *testing.T) {
	dir := os.Getenv("HISTORY_XPROC_DIR")
	if dir == "" {
		t.Skip("helper process")
	}
	id := os.Getenv("HISTORY_XPROC_ID")
	n, _ := strconv.Atoi(os.Getenv("HISTORY_XPROC_N"))
	s := Open(dir)
	s.LockTimeout = 10 * time.Second
	for i := 0; i < n; i++ {
		r := Record{T: TypeRun, Dot: "security", Kind: KindReview, Outcome: state.OutcomePass, Card: fmt.Sprintf("p%s/%d", id, i)}
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}
```

Create `internal/history/bench_test.go` (the V1 benchmark):

```go
package history

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"herdr-dots/internal/state"
)

// fillHistory writes n lines spread over 30 days: three runs to every
// resolution, kinds and outcomes mixed like a busy dot.
func fillHistory(b testing.TB, s *Store, slug string, n int) {
	b.Helper()
	kinds := []string{KindReview, KindReview, KindReview, KindCouncil, KindAsk, KindResearch}
	outcomes := []string{state.OutcomePass, state.OutcomePass, state.OutcomeFlagged, state.OutcomeSuppressed}
	step := 30 * 24 * time.Hour / time.Duration(n)
	for i := 0; i < n; i++ {
		at := hNow.Add(-30 * 24 * time.Hour).Add(time.Duration(i) * step)
		card := CardKey("s", fmt.Sprint(i/4))
		r := Record{T: TypeRun, At: at, Dot: slug, Kind: kinds[i%len(kinds)], Outcome: outcomes[i%len(outcomes)],
			Card: card, Pane: "w1:p1", Turn: i, Ms: int64(5000 + i%4000), CostUSD: 0.004, In: 2300, Out: 180,
			Excerpt: "Session cookie is not rotated after a privilege change; rotate it in the login handler."}
		if i%4 == 3 {
			r = Record{T: TypeResolve, At: at, Dot: slug, Card: card, Resolution: ResDismissed}
		}
		if err := s.Append(r); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStats30d is V1: one full-size history file (20,000 lines) read
// once for the profile screen (7- and 30-day stats and 50 rows).
func BenchmarkStats30d(b *testing.B) {
	s := Open(filepath.Join(b.TempDir(), "history"))
	s.TrimEvery = 1 << 30 // keep all 20,000 lines
	fillHistory(b, s, "security", DefaultMaxLines)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := s.Profile("security", hNow, 50)
		if err != nil || len(p.Rows) != 50 || p.Month.TotalRuns() == 0 {
			b.Fatalf("profile %d rows, %d runs, %v", len(p.Rows), p.Month.TotalRuns(), err)
		}
	}
}

// BenchmarkCostShareTenDots is the profile's cost hint over ten full files.
func BenchmarkCostShareTenDots(b *testing.B) {
	s := Open(filepath.Join(b.TempDir(), "history"))
	s.TrimEvery = 1 << 30
	for d := 0; d < 10; d++ {
		fillHistory(b, s, fmt.Sprintf("dot-%d", d), DefaultMaxLines)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.CostShare("dot-3", hNow, 30*24*time.Hour); err != nil {
			b.Fatal(err)
		}
	}
}
```

Create `internal/paths/history_test.go`:

```go
package paths

import (
	"os"
	"path/filepath"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

// Phase 9: history is shared by every Herdr session (like memory); trust is
// user configuration.
func TestHistoryAndTrustPaths(t *testing.T) {
	p := New("/c", "/s", "/sock")
	if p.HistoryDir() != filepath.Join("/s", "history") {
		t.Fatalf("history dir %q", p.HistoryDir())
	}
	if p.TrustFile() != filepath.Join("/c", "trusted.json") {
		t.Fatalf("trust file %q", p.TrustFile())
	}
}

func TestEnsureCreatesThePrivateHistoryDir(t *testing.T) {
	root := t.TempDir()
	p := New(filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "herdr.sock"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p.HistoryDir())
	if err != nil || !st.IsDir() {
		t.Fatalf("history dir: %v", err)
	}
	platformtest.AssertPrivateDir(t, p.HistoryDir())
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/history ./internal/paths`
Expected: FAIL to build: `undefined: Open` / `undefined: Record` (history) and `p.HistoryDir undefined` (paths).

- [ ] **Step 3: Write the history store**

Create `internal/history/history.go`:

```go
// Package history keeps a persistent, per-dot record of every run and every
// card resolution, across Herdr sessions and projects, under
// STATE_DIR/history/<slug>.jsonl. Files are append-only JSON lines guarded by a
// per-file lock, capped at MaxLines (trimmed to TrimTo when over). History
// stays on this machine and holds only short excerpts of model text.
package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/textutil"
)

// Record types.
const (
	TypeRun     = "run"
	TypeResolve = "resolve"
)

// Run kinds (Record.Kind).
const (
	KindReview    = "review"
	KindSummon    = "summon"
	KindCouncil   = "council"
	KindAsk       = "ask"
	KindResearch  = "research"
	KindScheduled = "scheduled"
	KindCommit    = "commit"
	KindDraft     = "draft"
	KindMemory    = "memory"
)

// Card resolutions (Record.Resolution).
const (
	ResSent      = "sent"
	ResEdited    = "edited" // sent after the developer edited it
	ResDismissed = "dismissed"
	ResApplied   = "applied"   // Phase 7 patch applied to the working tree
	ResDiscarded = "discarded" // Phase 7 patch thrown away
	ResInjected  = "injected"  // delivered by inject mode, never approved
	ResOrphaned  = "orphaned"  // its pane closed while the card waited
)

// Defaults.
const (
	DefaultMaxLines  = 20000
	DefaultTrimTo    = 15000
	DefaultTrimEvery = 500
	ExcerptRunes     = 300
)

var (
	ErrInvalid = errors.New("invalid history record")
	ErrLocked  = errors.New("history is locked by another process")
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

// ValidSlug reports whether slug can name a history file.
func ValidSlug(slug string) bool { return slugRe.MatchString(slug) }

// Record is one history line. Runs use T "run" and fill Kind, Outcome and the
// usage fields; resolutions use T "resolve" and fill Card and Resolution.
// Query returns runs with Resolution copied from their latest resolution.
type Record struct {
	T           string    `json:"t"`
	At          time.Time `json:"at"`
	Dot         string    `json:"dot"`
	Project     string    `json:"project,omitempty"`
	Pane        string    `json:"pane,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Turn        int       `json:"turn,omitempty"`
	Outcome     string    `json:"outcome,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	Card        string    `json:"card,omitempty"` // CardKey(session, id)
	Resolution  string    `json:"resolution,omitempty"`
	Excerpt     string    `json:"excerpt,omitempty"`
	CostUSD     float64   `json:"cost_usd,omitempty"`
	CostUnknown bool      `json:"cost_unknown,omitempty"`
	In          int       `json:"in,omitempty"`
	Out         int       `json:"out,omitempty"`
	Ms          int64     `json:"ms,omitempty"`
}

// CardKey joins a run and its resolutions. Queue card ids restart in every
// Herdr session, so the key carries the session.
func CardKey(session, id string) string {
	if id == "" {
		return ""
	}
	return session + "/" + id
}

// Store reads and writes history files under Dir. The zero limits mean the
// defaults. Safe for concurrent use; separate processes are serialized by a
// per-file lock (<slug>.lock).
type Store struct {
	Dir         string
	MaxLines    int
	TrimTo      int
	TrimEvery   int                 // appends (per process and file) between size checks
	LockTimeout time.Duration       // 0 → 2s
	Redact      func(string) string // scrubs key-shaped text from excerpts; nil = credentials.Redact
	Now         func() time.Time    // nil = time.Now

	mu      sync.Mutex
	appends map[string]int
}

func Open(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) limits() (maxLines, trimTo, every int) {
	maxLines, trimTo, every = s.MaxLines, s.TrimTo, s.TrimEvery
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if trimTo <= 0 || trimTo > maxLines {
		trimTo = min(DefaultTrimTo, maxLines)
	}
	if every <= 0 {
		every = DefaultTrimEvery
	}
	return maxLines, trimTo, every
}

func (s *Store) path(slug string) string { return filepath.Join(s.Dir, slug+".jsonl") }

// withLock runs fn holding this process's mutex and the file's lock.
func (s *Store) withLock(slug string, fn func(path string) error) error {
	if !ValidSlug(slug) {
		return fmt.Errorf("%w: dot %q", ErrInvalid, slug)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	timeout := s.LockTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	l, err := lockfile.Acquire(filepath.Join(s.Dir, slug+".lock"), timeout)
	if errors.Is(err, lockfile.ErrLocked) {
		return fmt.Errorf("%w: %s", ErrLocked, slug)
	}
	if err != nil {
		return err
	}
	defer l.Release()
	return fn(s.path(slug))
}

// Excerpt cleans model text for history: control and bidi characters
// stripped, keys redacted (credentials.Redact unless the store overrides it:
// history never holds a key), whitespace flattened, at most ExcerptRunes
// runes.
func (s *Store) excerpt(text string) string {
	text = textutil.SanitizeModelText(text)
	if s.Redact != nil {
		text = s.Redact(text)
	} else {
		text = credentials.Redact(text)
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > ExcerptRunes {
		text = string(r[:ExcerptRunes-1]) + "…"
	}
	return text
}

// Append writes one record. At defaults to now. It trims the file when a
// size check (every TrimEvery appends, and on the first append of each file in
// this process) finds it over MaxLines.
func (s *Store) Append(r Record) error {
	if r.T != TypeRun && r.T != TypeResolve {
		return fmt.Errorf("%w: type %q", ErrInvalid, r.T)
	}
	if r.At.IsZero() {
		r.At = s.now()
	}
	r.At = r.At.UTC()
	r.Excerpt = s.excerpt(r.Excerpt)
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.withLock(r.Dot, func(path string) error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		_, werr := f.Write(append(line, '\n'))
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if cerr != nil {
			return cerr
		}
		if s.appends == nil {
			s.appends = map[string]int{}
		}
		s.appends[r.Dot]++
		_, _, every := s.limits()
		if n := s.appends[r.Dot]; n == 1 || n%every == 0 {
			return s.trimLocked(path)
		}
		return nil
	})
}

// Trim cuts slug's file to TrimTo lines when it is over MaxLines.
func (s *Store) Trim(slug string) error {
	return s.withLock(slug, s.trimLocked)
}

func (s *Store) trimLocked(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	maxLines, trimTo, _ := s.limits()
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	if len(lines) <= maxLines {
		return nil
	}
	keep := lines[len(lines)-trimTo:]
	runs := map[string]bool{}
	recs := make([]*Record, len(keep))
	for i, l := range keep {
		var r Record
		if json.Unmarshal(l, &r) != nil {
			continue // a corrupt line is dropped by the trim
		}
		recs[i] = &r
		if r.T == TypeRun && r.Card != "" {
			runs[r.Card] = true
		}
	}
	var out bytes.Buffer
	for i, l := range keep {
		r := recs[i]
		if r == nil {
			continue
		}
		if r.T == TypeResolve && !runs[r.Card] {
			continue // its run was trimmed away
		}
		out.Write(l)
		out.WriteByte('\n')
	}
	return fsutil.WriteFileAtomic(path, out.Bytes(), 0o600)
}

// read returns every parseable record of slug in file order and how many
// lines could not be parsed. A missing file is empty.
func (s *Store) read(slug string) ([]Record, int, error) {
	var data []byte
	err := s.withLock(slug, func(path string) error {
		var err error
		data, err = os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	var out []Record
	skipped := 0
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(l, &r); err != nil || (r.T != TypeRun && r.T != TypeResolve) {
			skipped++
			continue
		}
		out = append(out, r)
	}
	return out, skipped, nil
}

// joined returns slug's runs in file order, each with the resolution of its
// card's latest resolve record, plus the resolve records and the skip count.
// A resolution can be written before its run: an inject card is delivered
// while the run that made it is still finishing.
func (s *Store) joined(slug string) (runs, resolves []Record, skipped int, err error) {
	all, skipped, err := s.read(slug)
	if err != nil {
		return nil, nil, 0, err
	}
	byCard := map[string]int{}
	for _, r := range all {
		if r.T == TypeRun {
			r.Resolution = ""
			runs = append(runs, r)
			if r.Card != "" {
				byCard[r.Card] = len(runs) - 1
			}
		}
	}
	for _, r := range all {
		if r.T == TypeResolve {
			resolves = append(resolves, r)
			if i, ok := byCard[r.Card]; ok {
				runs[i].Resolution = r.Resolution
			}
		}
	}
	return runs, resolves, skipped, nil
}

// Query returns up to limit runs of slug at or after since, newest first,
// each with its card's resolution ("" while unresolved). limit <= 0 = all.
func (s *Store) Query(slug string, since time.Time, limit int) ([]Record, error) {
	runs, _, _, err := s.joined(slug)
	if err != nil {
		return nil, err
	}
	var out []Record
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].At.Before(since) {
			continue
		}
		out = append(out, runs[i])
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// Slugs lists the dots that have a history file.
func (s *Store) Slugs() ([]string, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if slug, ok := strings.CutSuffix(e.Name(), ".jsonl"); ok && !e.IsDir() && ValidSlug(slug) {
			out = append(out, slug)
		}
	}
	return out, nil
}
```

Create `internal/history/stats.go`:

```go
package history

import (
	"errors"
	"slices"
	"time"

	"herdr-dots/internal/state"
)

// Stats summarizes one dot over a window, computed from its history file.
//
//   - Runs counts every run by kind (memory passes included).
//   - Verdicts counts non-memory runs that ended flagged, pass or suppressed;
//     Flagged and Suppressed are subsets of it.
//   - Resolved counts sent, edited, applied and dismissed resolutions;
//     Accepted is sent + edited + applied. Injected, discarded and orphaned
//     cards are neither.
//   - Errors counts runs that ended in an error (every kind).
//   - CostUSD sums the runs' known costs; CostUnknown counts runs whose cost
//     the backend could not report.
//   - MedianMs is the median latency of runs that did not fail (0 = none).
//   - Skipped counts unreadable lines in the whole file.
type Stats struct {
	Runs        map[string]int
	Verdicts    int
	Flagged     int
	Suppressed  int
	Errors      int
	Resolved    int
	Accepted    int
	Dismissed   int
	CostUSD     float64
	CostUnknown int
	MedianMs    int64
	Skipped     int
}

// TotalRuns is the number of runs of every kind.
func (st Stats) TotalRuns() int {
	n := 0
	for _, v := range st.Runs {
		n += v
	}
	return n
}

// FlaggedRate is Flagged / Verdicts (0 with no verdicts).
func (st Stats) FlaggedRate() float64 { return ratio(st.Flagged, st.Verdicts) }

// AcceptanceRate is Accepted / Resolved (0 with nothing resolved).
func (st Stats) AcceptanceRate() float64 { return ratio(st.Accepted, st.Resolved) }

// ErrorRate is Errors / TotalRuns (0 with no runs).
func (st Stats) ErrorRate() float64 { return ratio(st.Errors, st.TotalRuns()) }

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func inWindow(t, now time.Time, window time.Duration) bool {
	return !t.Before(now.Add(-window)) && !t.After(now)
}

// Stats computes slug's stats for [now-window, now].
func (s *Store) Stats(slug string, now time.Time, window time.Duration) (Stats, error) {
	runs, resolves, skipped, err := s.joined(slug)
	if err != nil {
		return Stats{Runs: map[string]int{}}, err
	}
	return statsFrom(runs, resolves, skipped, now, window), nil
}

// Profile is what the profile screen shows, read from one pass over the file.
type Profile struct {
	Week, Month Stats
	Rows        []Record // newest runs first, with resolutions
}

// Profile reads slug's file once and returns the 7- and 30-day stats and the
// newest rows runs (rows <= 0 = all).
func (s *Store) Profile(slug string, now time.Time, rows int) (Profile, error) {
	runs, resolves, skipped, err := s.joined(slug)
	if err != nil {
		return Profile{Week: Stats{Runs: map[string]int{}}, Month: Stats{Runs: map[string]int{}}}, err
	}
	p := Profile{
		Week:  statsFrom(runs, resolves, skipped, now, 7*24*time.Hour),
		Month: statsFrom(runs, resolves, skipped, now, 30*24*time.Hour),
	}
	for i := len(runs) - 1; i >= 0 && (rows <= 0 || len(p.Rows) < rows); i-- {
		p.Rows = append(p.Rows, runs[i])
	}
	return p, nil
}

func statsFrom(runs, resolves []Record, skipped int, now time.Time, window time.Duration) Stats {
	st := Stats{Runs: map[string]int{}, Skipped: skipped}
	var ms []int64
	for _, r := range runs {
		if !inWindow(r.At, now, window) {
			continue
		}
		st.Runs[r.Kind]++
		st.CostUSD += r.CostUSD
		if r.CostUnknown {
			st.CostUnknown++
		}
		switch r.Outcome {
		case state.OutcomeError:
			st.Errors++
		case state.OutcomeFlagged, state.OutcomePass, state.OutcomeSuppressed:
			if r.Kind != KindMemory {
				st.Verdicts++
				if r.Outcome == state.OutcomeFlagged {
					st.Flagged++
				}
				if r.Outcome == state.OutcomeSuppressed {
					st.Suppressed++
				}
			}
		}
		if r.Outcome != state.OutcomeError && r.Ms > 0 {
			ms = append(ms, r.Ms)
		}
	}
	for _, r := range resolves {
		if !inWindow(r.At, now, window) {
			continue
		}
		switch r.Resolution {
		case ResSent, ResEdited, ResApplied:
			st.Resolved++
			st.Accepted++
		case ResDismissed:
			st.Resolved++
			st.Dismissed++
		}
	}
	if len(ms) > 0 {
		slices.Sort(ms)
		if n := len(ms); n%2 == 1 {
			st.MedianMs = ms[n/2]
		} else {
			st.MedianMs = (ms[n/2-1] + ms[n/2]) / 2
		}
	}
	return st
}

// CostShare returns slug's share of every dot's cost in the window and the
// total. Unreadable files are skipped; their error is returned joined.
func (s *Store) CostShare(slug string, now time.Time, window time.Duration) (share, total float64, err error) {
	slugs, err := s.Slugs()
	if err != nil {
		return 0, 0, err
	}
	var mine float64
	var errs []error
	for _, other := range slugs {
		st, err := s.Stats(other, now, window)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		total += st.CostUSD
		if other == slug {
			mine = st.CostUSD
		}
	}
	if total > 0 {
		share = mine / total
	}
	return share, total, errors.Join(errs...)
}
```

- [ ] **Step 4: Add the paths**

In `internal/paths/paths.go`, find:

```go
func (p Paths) DotsPanesFile() string   { return filepath.Join(p.SessionDir, "dotspanes.json") }
```

Replace with:

```go
func (p Paths) DotsPanesFile() string   { return filepath.Join(p.SessionDir, "dotspanes.json") }

// HistoryDir holds per-dot history (Phase 9). Like memory it is shared by
// every named Herdr session, so it is not under SessionDir.
func (p Paths) HistoryDir() string { return filepath.Join(p.StateDir, "history") }

// TrustFile records which project dot files the developer trusts (Phase 9).
func (p Paths) TrustFile() string { return filepath.Join(p.ConfigDir, "trusted.json") }
```

In `Ensure`, find the directory list (Phase 6 secures each entry with `platform.SecureDir`; the list itself is unchanged since Phase 2):

```go
	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir, p.MemoryDir()} {
```

Replace with (the list is unchanged since Phase 2; Phase 6 secures each entry):

```go
	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir, p.MemoryDir(), p.HistoryDir()} {
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/history ./internal/paths`
Expected: `ok  	herdr-dots/internal/history` and `ok  	herdr-dots/internal/paths`.

- [ ] **Step 6: Measure V1**

Run: `GOTOOLCHAIN=local go test -run '^$' -bench . -benchtime 10x ./internal/history`
Expected: two lines like `BenchmarkStats30d-12  10  35352212 ns/op` and `BenchmarkCostShareTenDots-12  10  368462683 ns/op` (planning machine, Apple M4 Pro). Write both numbers into your task report; Task 12 records them in the notes. If `BenchmarkStats30d` is over 100 ms/op on this machine, stop and report: Plan decision 11 assumes the profile opens within 100 ms.

- [ ] **Step 7: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 8: Commit**

```bash
git add internal/history internal/paths/paths.go internal/paths/history_test.go
git commit -m "feat(history): per-dot run and resolution history with stats, profile reads and trimming (Phase 9)"
```

---

### Task 2: Dot origins, project dot files, `ClampDot`, `Encode`

Everything Phase 9 needs to know about a dot: where it came from, how a repository's dot file is read and named, which capabilities are risky and how to turn them off, and how a dot is written as TOML (for packs).

**Depends on:** none. Wave 1 (runs beside Tasks 1, 3 and 4).

**Files:**
- Modify: `internal/dots/dot.go` (Dot fields, `Validate`), Phase 7's `internal/dots/capabilities.go` (`CapabilitySpec`, `DefaultCapabilities`), `internal/dots/store.go` (`decode`, `Save`, `Encode`, `PurposeBlock`, `insertPurpose`), `internal/dots/templates.go` (origin)
- Create: `internal/dots/project.go`, `internal/dots/clamp.go`
- Create: `internal/dots/project_test.go`, `internal/dots/clamp_test.go`

**Interfaces:**
- Consumes: v1 `Dot`, `New`, `Slugify`, `Validate`, `Save`, `Load`, `LoadAll`, `LoadError`, `DeniedArgs`, `SafeArgs`, `multilineBasic`; Phase 4 `BackendSpec.BaseURL`; Phase 7 `CapabilitySpec{Write…}`, `DefaultCapabilities()`; Phase 8 `ResearchSpec{Enabled, Web}`, `ScheduleSpec{Cron}`, `TriggerSpec.OnCommit`.
- Produces (package `dots`):
  - `Dot.Origin string` (TOML `origin,omitempty`), `Dot.SourcePath string` (not serialized); `CapabilitySpec.MCP bool` (TOML `mcp`, true in `DefaultCapabilities()`).
  - consts `OriginUser = "user"`, `OriginTemplate = "template"`, `OriginImport = "import"`, `OriginProject = "project"`, `ProjectPrefix = "proj--"`, `MaxProjectFileBytes = 64 << 10`; `var ProjectDir = filepath.Join(".herdr-dots", "dots")`.
  - `ProjectTag(projectID string) string`; `ProjectSlug(base, projectID string) string`; `SplitProjectSlug(slug string) (base, tag string, ok bool)`; `IsProjectSlug(slug string) bool`; `DisplaySlug(slug string) string`.
  - `type ProjectFile struct{ Base, Path, Rel string; Data []byte }`; `ReadProjectFiles(root string) ([]ProjectFile, []LoadError)`; `DecodeProject(f ProjectFile, projectID string) (Dot, error)`; `DecodeStrict(data []byte) (Dot, error)`; `LoadProject(root, projectID string) ([]Dot, []LoadError)`.
  - capability consts `CapInject`, `CapWrite`, `CapMCP`, `CapResearch`, `CapWeb`, `CapSchedule`, `CapCommit`, `CapBaseURL` (values `inject`, `write`, `mcp`, `research`, `web`, `schedule`, `commit`, `base_url`) and the non-keepable `CapArgs = "args"`, `CapProjects = "projects"` (absolute `schedule.projects` paths removed); `var Capabilities []string` (display order); `type Clamp struct{ Capability, Field, From, To string; Keepable bool }` with `String()`; `ClampDot(d Dot, keep []string) (Dot, []Clamp)`; `Requested(d Dot) []string`; `CheckCapabilities(caps []string) error`.
  - `Encode(d Dot) ([]byte, error)`; `PurposeBlock(purpose string) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/dots/project_test.go`:

```go
package dots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const projID = "g-9f2c1a7b00112233"

func writeProjectDot(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, ".herdr-dots", "dots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const a11yTOML = `name = "A11y"
glyph = "♿"
color = "green"
purpose = """
Check accessibility."""

[delivery]
mode = "inject"
`

func TestProjectSlugsAreNamespacedAndFileSafe(t *testing.T) {
	s := ProjectSlug("a11y", projID)
	if s != "proj--a11y--9f2c1a7b" {
		t.Fatalf("slug %q", s)
	}
	base, tag, ok := SplitProjectSlug(s)
	if !ok || base != "a11y" || tag != "9f2c1a7b" || !IsProjectSlug(s) || DisplaySlug(s) != "proj:a11y" {
		t.Fatalf("split %q %q %v", base, tag, ok)
	}
	if Slugify("proj--a11y--9f2c1a7b") == s || IsProjectSlug(Slugify("Proj  A11y")) {
		t.Fatal("a user dot name must never produce a project slug")
	}
	if DisplaySlug("security") != "security" || ProjectTag("bogus") != "" || ProjectTag("g-XYZ12345abc") != "" {
		t.Fatal("non-project inputs")
	}
}

func TestLoadProjectNamespacesAndMarksOrigin(t *testing.T) {
	root := t.TempDir()
	p := writeProjectDot(t, root, "a11y.toml", a11yTOML)
	ds, bad := LoadProject(root, projID)
	if len(bad) != 0 || len(ds) != 1 {
		t.Fatalf("dots %+v bad %+v", ds, bad)
	}
	d := ds[0]
	if d.Slug != "proj--a11y--9f2c1a7b" || d.Origin != OriginProject || d.SourcePath != p || d.Delivery.Mode != ModeInject {
		t.Fatalf("dot %+v", d)
	}
	if none, bad := LoadProject(t.TempDir(), projID); none != nil || bad != nil {
		t.Fatal("a repo without .herdr-dots has no project dots and no errors")
	}
}

func TestLoadProjectRefusesUnknownKeysLinksBigFilesAndBadNames(t *testing.T) {
	root := t.TempDir()
	writeProjectDot(t, root, "future.toml", a11yTOML+"\n[capabilities]\nshell = true\n")
	writeProjectDot(t, root, "Bad_Name.toml", a11yTOML)
	writeProjectDot(t, root, "big.toml", a11yTOML+"# "+strings.Repeat("x", MaxProjectFileBytes)+"\n")
	target := writeProjectDot(t, root, "real.toml", a11yTOML)
	if err := os.Symlink(target, filepath.Join(root, ".herdr-dots", "dots", "link.toml")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	ds, bad := LoadProject(root, projID)
	if len(ds) != 1 || ds[0].Slug != "proj--real--9f2c1a7b" {
		t.Fatalf("only real.toml loads: %+v", ds)
	}
	msgs := map[string]string{}
	for _, le := range bad {
		msgs[le.File] = le.Err.Error()
	}
	for file, want := range map[string]string{
		".herdr-dots/dots/future.toml":   "unknown keys capabilities.shell",
		".herdr-dots/dots/Bad_Name.toml": "file name",
		".herdr-dots/dots/big.toml":      "cap",
		".herdr-dots/dots/link.toml":     "not a link",
	} {
		if !strings.Contains(msgs[file], want) {
			t.Errorf("%s: got %q, want it to mention %q", file, msgs[file], want)
		}
	}
}

func TestLoadProjectRefusesALinkedDotsDirectory(t *testing.T) {
	root, elsewhere := t.TempDir(), t.TempDir()
	writeProjectDot(t, elsewhere, "a11y.toml", a11yTOML)
	if err := os.Symlink(filepath.Join(elsewhere, ".herdr-dots"), filepath.Join(root, ".herdr-dots")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	ds, bad := LoadProject(root, projID)
	if len(ds) != 0 || len(bad) != 1 || !strings.Contains(bad[0].Err.Error(), "directory") {
		t.Fatalf("dots %+v bad %+v", ds, bad)
	}
}

func TestValidateReservesProjectSlugsAndChecksOrigin(t *testing.T) {
	d := New("Spoof")
	d.Purpose = "p"
	d.Slug = "proj--a11y--9f2c1a7b"
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "reserved for project dots") {
		t.Fatalf("user dot with a project slug: %v", err)
	}
	d.Origin = OriginProject
	if err := d.Validate(); err != nil {
		t.Fatalf("project dot: %v", err)
	}
	d.Slug = "a11y"
	if err := d.Validate(); err == nil {
		t.Fatal("project origin needs a project slug")
	}
	d.Slug, d.Origin = "spoof", "elsewhere"
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("unknown origin: %v", err)
	}
}

func TestUserFilesKeepTemplateAndImportOriginsOnly(t *testing.T) {
	dir := t.TempDir()
	tpl, _ := FromTemplate("security")
	if tpl.Origin != OriginTemplate {
		t.Fatalf("template origin %q", tpl.Origin)
	}
	if err := Save(dir, tpl, true); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "security")
	if err != nil || got.Origin != OriginTemplate {
		t.Fatalf("template origin lost: %+v %v", got, err)
	}
	plain := New("Plain")
	plain.Purpose = "p"
	if err := Save(dir, plain, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "plain.toml"))
	if strings.Contains(string(raw), "origin") {
		t.Fatalf("a user origin is never written:\n%s", raw)
	}
	if got, _ := Load(dir, "plain"); got.Origin != OriginUser {
		t.Fatalf("plain origin %q", got.Origin)
	}
	os.WriteFile(filepath.Join(dir, "sneaky.toml"), []byte("origin = \"project\"\n"+strings.TrimPrefix(a11yTOML, "")), 0o600)
	if got, err := Load(dir, "sneaky"); err != nil || got.Origin != OriginUser {
		t.Fatalf("a user file can't claim to be a project dot: %+v %v", got, err)
	}
}

func TestEncodeKeepsPurposeReadableAndRoundTrips(t *testing.T) {
	d := New("Round")
	d.Purpose = "Line one with \"quotes\" and a \\ backslash.\n[not a table]\nends with a quote\""
	d.Origin = OriginImport
	data, err := Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "purpose = \"\"\"\n") {
		t.Fatalf("purpose not multi-line:\n%s", data)
	}
	got, err := DecodeStrict(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != d.Purpose || got.Origin != OriginImport || got.Name != "Round" {
		t.Fatalf("round trip: %+v", got)
	}
}
```

Create `internal/dots/clamp_test.go`:

```go
package dots

import (
	"slices"
	"testing"

	"herdr-dots/internal/platform/platformtest"
)

// riskyDot asks for every capability a shared dot can ask for.
func riskyDot() Dot {
	d := New("Risky")
	d.Purpose = "p"
	d.Delivery.Mode = ModeInject
	d.Capabilities.Write = true
	d.Capabilities.MCP = true
	d.Research.Enabled, d.Research.Web = true, true
	d.Schedule.Cron = "0 9 * * 1-5"
	d.Trigger.OnCommit = true
	d.Backend.BaseURL = "https://example.invalid/v1"
	d.Backend.Args = []string{"--model", "x", "--dangerously-skip-permissions", "--add-dir", "/etc"}
	return d
}

func TestClampDotTurnsEveryRiskyCapabilityOff(t *testing.T) {
	d := riskyDot()
	got, cs := ClampDot(d, nil)
	if got.Delivery.Mode != ModeQueue || got.Capabilities.Write || got.Capabilities.MCP || got.Research.Enabled ||
		got.Research.Web || got.Schedule.Cron != "" || got.Trigger.OnCommit || got.Backend.BaseURL != "" {
		t.Fatalf("not clamped: %+v", got)
	}
	if !slices.Equal(got.Backend.Args, []string{"--model", "x"}) {
		t.Fatalf("args %v", got.Backend.Args)
	}
	var caps []string
	for _, c := range cs {
		caps = append(caps, c.Capability)
	}
	if want := append(slices.Clone(Capabilities), CapArgs); !slices.Equal(caps, want) {
		t.Fatalf("clamps %v, want %v", caps, want)
	}
	if cs[len(cs)-1].Keepable || !cs[0].Keepable {
		t.Fatal("args can't be kept; inject can")
	}
	if d.Delivery.Mode != ModeInject || len(d.Backend.Args) != 5 {
		t.Fatal("ClampDot changed its input")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("a clamped dot validates: %v", err)
	}
}

// A shared dot's schedule may only target the projects it is attached to:
// absolute paths name directories on the author's machine (or a guess at
// yours), and keeping "schedule" never keeps them.
func TestClampRemovesAbsoluteScheduleProjects(t *testing.T) {
	d := riskyDot()
	d.Backend.BaseURL = "" // claude-cli has none; keeping it would not validate
	d.Schedule.Task = "t"
	d.Schedule.Projects = []string{ScheduleAttached, platformtest.Abs("home", "me", "secret-repo")}
	got, cs := ClampDot(d, Capabilities)
	if !slices.Equal(got.Schedule.Projects, []string{ScheduleAttached}) || got.Schedule.Cron == "" {
		t.Fatalf("schedule %+v", got.Schedule)
	}
	if len(cs) != 2 || cs[0].Capability != CapArgs || cs[1].Capability != CapProjects || cs[1].Keepable {
		t.Fatalf("clamps %+v", cs)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestKeepRestoresOnlyTheNamedCapability(t *testing.T) {
	got, cs := ClampDot(riskyDot(), []string{CapInject})
	if got.Delivery.Mode != ModeInject || got.Capabilities.Write {
		t.Fatalf("keep inject: %+v", got)
	}
	for _, c := range cs {
		if c.Capability == CapInject {
			t.Fatal("a kept capability is not a clamp")
		}
	}
	all, _ := ClampDot(riskyDot(), Capabilities)
	if len(all.Backend.Args) != 2 {
		t.Fatal("keeping everything still removes denied args")
	}
}

// Clearing write rebuilds CapabilitySpec (the write-tool guard allows no
// assignment to .Write outside drafts); every other field must survive.
func TestClampKeepsEveryOtherCapabilityField(t *testing.T) {
	d := riskyDot()
	d.Capabilities.WriteTimeoutSeconds, d.Capabilities.MaxPatchBytes, d.Capabilities.MaxFiles = 900, 5000, 7
	got, _ := ClampDot(d, []string{CapMCP})
	want := d.Capabilities
	want.Write = false
	if got.Capabilities != want {
		t.Fatalf("capabilities %+v, want %+v", got.Capabilities, want)
	}
}

func TestRequestedAndCheckCapabilities(t *testing.T) {
	if got := Requested(riskyDot()); !slices.Equal(got, Capabilities) {
		t.Fatalf("requested %v", got)
	}
	plain := New("Plain")
	if got := Requested(plain); !slices.Equal(got, []string{CapMCP}) {
		t.Fatalf("a default dot only asks for mcp (on by default): %v", got)
	}
	if CheckCapabilities([]string{"inject", "web"}) != nil || CheckCapabilities([]string{"shell"}) == nil {
		t.Fatal("CheckCapabilities")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/dots`
Expected: FAIL to build: `undefined: ProjectSlug`, `undefined: ClampDot`, `undefined: OriginTemplate`, `d.Origin undefined`.

- [ ] **Step 3: Add the Dot fields and checks**

In `internal/dots/dot.go`, find the start of the `Dot` struct:

```go
type Dot struct {
	Slug     string       `toml:"-"`
	Name     string       `toml:"name"`
	Glyph    string       `toml:"glyph"`
	Color    string       `toml:"color"`
```

Replace with (gofmt re-aligns the rest of the struct):

```go
type Dot struct {
	Slug       string `toml:"-"`
	Name       string `toml:"name"`
	Glyph      string `toml:"glyph"`
	Color      string `toml:"color"`
	Origin     string `toml:"origin,omitempty"` // template or import in a user file; "" = user; project is set at load
	SourcePath string `toml:"-"`                // project dots: the file the dot was loaded from
```

In `Validate`, find:

```go
	if strings.TrimSpace(d.Purpose) == "" {
		add("purpose is required")
	}
```

Replace with:

```go
	if strings.TrimSpace(d.Purpose) == "" {
		add("purpose is required")
	}
	switch d.Origin {
	case "", OriginUser, OriginTemplate, OriginImport:
		if IsProjectSlug(d.Slug) {
			add("names starting with %q are reserved for project dots", ProjectPrefix)
		}
	case OriginProject:
		if _, _, ok := SplitProjectSlug(d.Slug); !ok {
			add("project dot slug %q must look like %s<name>--<project>", d.Slug, ProjectPrefix)
		}
	default:
		add("origin %q must be %s, %s or %s", d.Origin, OriginUser, OriginTemplate, OriginImport)
	}
```

In Phase 7's `CapabilitySpec` (`internal/dots/capabilities.go`), find its last field:

```go
	MaxFiles            int  `toml:"max_files"`             // patches touching more files are rejected
}
```

Replace with:

```go
	MaxFiles            int  `toml:"max_files"`             // patches touching more files are rejected
	MCP                 bool `toml:"mcp"`                   // Phase 9: the MCP server may ask this dot (ask_dot, ask_council)
}
```

and in `DefaultCapabilities()`, find:

```go
	return CapabilitySpec{WriteTimeoutSeconds: 600, MaxPatchBytes: 200000, MaxFiles: 50}
```

Replace with (every existing dot file without the key keeps MCP access, as before Phase 9):

```go
	return CapabilitySpec{WriteTimeoutSeconds: 600, MaxPatchBytes: 200000, MaxFiles: 50, MCP: true}
```

`New` uses `DefaultCapabilities()`, so every dot without the key keeps MCP access.

- [ ] **Step 4: Write project dot files and the clamp table**

Create `internal/dots/project.go`:

```go
package dots

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Dot origins (Dot.Origin). User files store template and import; a user
// origin is never written. Project is set only by DecodeProject.
const (
	OriginUser     = "user"
	OriginTemplate = "template"
	OriginImport   = "import"
	OriginProject  = "project"
)

// ProjectPrefix starts every project dot slug: proj--<name>--<project tag>.
// Slugify never produces "--", so no user dot can take a project slug, and
// the slug stays a valid file name and memory slug on every OS.
const ProjectPrefix = "proj--"

// ProjectDir is where a repository keeps its team dots, relative to its root.
var ProjectDir = filepath.Join(".herdr-dots", "dots")

// MaxProjectFileBytes caps one project dot file.
const MaxProjectFileBytes = 64 << 10

var (
	projectBaseRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	projectTagRe  = regexp.MustCompile(`^[0-9a-f]{8}$`)
)

// ProjectTag is the 8 hex characters of a memory project id (g-/p- + hex)
// that make project slugs unique per repository. "" for anything else.
func ProjectTag(projectID string) string {
	if len(projectID) < 10 || (projectID[:2] != "g-" && projectID[:2] != "p-") {
		return ""
	}
	if tag := projectID[2:10]; projectTagRe.MatchString(tag) {
		return tag
	}
	return ""
}

// ProjectSlug is the engine slug of a project dot file base name in a project.
func ProjectSlug(base, projectID string) string {
	return ProjectPrefix + base + "--" + ProjectTag(projectID)
}

// SplitProjectSlug returns a project slug's file base name and project tag.
func SplitProjectSlug(slug string) (base, tag string, ok bool) {
	rest, found := strings.CutPrefix(slug, ProjectPrefix)
	if !found {
		return "", "", false
	}
	i := strings.LastIndex(rest, "--")
	if i <= 0 {
		return "", "", false
	}
	base, tag = rest[:i], rest[i+2:]
	if !projectBaseRe.MatchString(base) || !projectTagRe.MatchString(tag) {
		return "", "", false
	}
	return base, tag, true
}

// IsProjectSlug reports whether slug belongs to a project dot.
func IsProjectSlug(slug string) bool {
	_, _, ok := SplitProjectSlug(slug)
	return ok || strings.HasPrefix(slug, ProjectPrefix)
}

// DisplaySlug is how people see a slug: proj:<name> for project dots.
func DisplaySlug(slug string) string {
	if base, _, ok := SplitProjectSlug(slug); ok {
		return "proj:" + base
	}
	return slug
}

// ProjectFile is one project dot file as read from disk. Data is exactly the
// bytes that were parsed, so a hash of Data is a hash of what runs.
type ProjectFile struct {
	Base string // file name without .toml
	Path string // absolute path
	Rel  string // path from the repository root, with forward slashes
	Data []byte
}

// ReadProjectFiles reads root/.herdr-dots/dots/*.toml. Symlinked directories
// or files, files over MaxProjectFileBytes and names that can't become a slug
// are refused with a LoadError. A missing directory is not an error.
func ReadProjectFiles(root string) ([]ProjectFile, []LoadError) {
	dir := filepath.Join(root, ProjectDir)
	for _, d := range []string{filepath.Join(root, ".herdr-dots"), dir} {
		st, err := os.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, []LoadError{{File: d, Err: err}}
		}
		if !st.IsDir() {
			return nil, []LoadError{{File: d, Err: errors.New("must be a directory, not a link or a file")}}
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, []LoadError{{File: dir, Err: err}}
	}
	var out []ProjectFile
	var bad []LoadError
	for _, e := range ents {
		base, ok := strings.CutSuffix(e.Name(), ".toml")
		if !ok || e.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(ProjectDir, e.Name()))
		if !projectBaseRe.MatchString(base) || len(base) > 64 {
			bad = append(bad, LoadError{File: rel, Err: errors.New("file name must be lowercase letters, digits and single dashes (at most 64)")})
			continue
		}
		if e.Type()&fs.ModeType != 0 {
			bad = append(bad, LoadError{File: rel, Err: errors.New("must be a regular file, not a link")})
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := readCapped(p, MaxProjectFileBytes)
		if err != nil {
			bad = append(bad, LoadError{File: rel, Err: err})
			continue
		}
		out = append(out, ProjectFile{Base: base, Path: p, Rel: rel, Data: data})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out, bad
}

func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file is over %d bytes, the cap for project dots", limit)
	}
	return data, nil
}

// DecodeProject parses one project dot file. Keys this version doesn't know
// are refused (a newer capability can't be clamped if it isn't understood),
// and the dot must validate.
func DecodeProject(f ProjectFile, projectID string) (Dot, error) {
	if ProjectTag(projectID) == "" {
		return Dot{}, fmt.Errorf("%s: project id %q can't name project dots", f.Rel, projectID)
	}
	d, err := DecodeStrict(f.Data)
	if err != nil {
		return Dot{}, err
	}
	d.Slug = ProjectSlug(f.Base, projectID)
	d.Origin = OriginProject
	d.SourcePath = f.Path
	return d, d.Validate()
}

// DecodeStrict decodes a dot from TOML over the defaults and refuses keys it
// doesn't know. It does not validate.
func DecodeStrict(data []byte) (Dot, error) {
	d := New("")
	md, err := toml.Decode(string(data), &d)
	if err != nil {
		return Dot{}, err
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return Dot{}, fmt.Errorf("unknown keys %s: this herdr-dots can't tell what they do, so it won't load them", strings.Join(keys, ", "))
	}
	return d, nil
}

// LoadProject loads root's project dots with project-namespaced slugs.
func LoadProject(root, projectID string) ([]Dot, []LoadError) {
	files, bad := ReadProjectFiles(root)
	var out []Dot
	for _, f := range files {
		d, err := DecodeProject(f, projectID)
		if err != nil {
			le := LoadError{File: f.Rel, Err: err}
			var pe toml.ParseError
			if errors.As(err, &pe) {
				le.Line = pe.Position.Line
			}
			bad = append(bad, le)
			continue
		}
		out = append(out, d)
	}
	return out, bad
}
```

Create `internal/dots/clamp.go`:

```go
package dots

import (
	"fmt"
	"slices"
	"strings"
)

// Capabilities a dot written by someone else (an imported pack, a project
// dot) may ask for. Each starts clamped and is restored only when the
// developer keeps it, one by one.
const (
	CapInject   = "inject"   // delivery.mode = "inject": text reaches the agent unapproved
	CapWrite    = "write"    // capabilities.write (Phase 7): drafts edits in a worktree
	CapMCP      = "mcp"      // capabilities.mcp: the agent can ask the dot over MCP, unapproved
	CapResearch = "research" // research.enabled (Phase 8): runs while you're away
	CapWeb      = "web"      // research.web (Phase 8): fetches from the internet
	CapSchedule = "schedule" // schedule.cron (Phase 8): runs on a timer
	CapCommit   = "commit"   // trigger.on_commit (Phase 8): runs on every commit
	CapBaseURL  = "base_url" // backend.base_url (Phase 4): sends prompts (and an API key) to another endpoint
)

// CapArgs is the clamp that removes denied backend args; CapProjects removes
// absolute schedule.projects paths (Phase 8), which name directories on the
// author's machine: someone else's dot may run only on the projects it is
// attached to. Neither can be kept.
const (
	CapArgs     = "args"
	CapProjects = "projects"
)

// Capabilities lists the keepable capabilities in display order.
var Capabilities = []string{CapInject, CapWrite, CapMCP, CapResearch, CapWeb, CapSchedule, CapCommit, CapBaseURL}

// Clamp is one change ClampDot made.
type Clamp struct {
	Capability string // one of Capabilities, or CapArgs
	Field      string // the TOML key, e.g. "delivery.mode"
	From, To   string // display values
	Keepable   bool
}

func (c Clamp) String() string {
	to := c.To
	if to == "" {
		to = "(off)"
	}
	return fmt.Sprintf("%s: %s %s → %s", c.Capability, c.Field, c.From, to)
}

// CheckCapabilities refuses names that aren't keepable capabilities.
func CheckCapabilities(caps []string) error {
	for _, c := range caps {
		if !slices.Contains(Capabilities, c) {
			return fmt.Errorf("unknown capability %q: want one of %s", c, strings.Join(Capabilities, ", "))
		}
	}
	return nil
}

// ClampDot returns d with every risky capability turned off except those in
// keep, and the clamps it applied. Denied backend args are always removed.
// d itself is not changed.
func ClampDot(d Dot, keep []string) (Dot, []Clamp) {
	d.Backend.Args = slices.Clone(d.Backend.Args)
	var out []Clamp
	kept := func(c string) bool { return slices.Contains(keep, c) }
	clamp := func(c, field, from, to string) {
		out = append(out, Clamp{Capability: c, Field: field, From: from, To: to, Keepable: true})
	}
	if d.Delivery.Mode == ModeInject && !kept(CapInject) {
		d.Delivery.Mode = ModeQueue
		clamp(CapInject, "delivery.mode", string(ModeInject), string(ModeQueue))
	}
	if d.Capabilities.Write && !kept(CapWrite) {
		d.Capabilities = readOnly(d.Capabilities)
		clamp(CapWrite, "capabilities.write", "true", "false")
	}
	if d.Capabilities.MCP && !kept(CapMCP) {
		d.Capabilities.MCP = false
		clamp(CapMCP, "capabilities.mcp", "true", "false")
	}
	if d.Research.Enabled && !kept(CapResearch) {
		d.Research.Enabled = false
		clamp(CapResearch, "research.enabled", "true", "false")
	}
	if d.Research.Web && !kept(CapWeb) {
		d.Research.Web = false
		clamp(CapWeb, "research.web", "true", "false")
	}
	if d.Schedule.Cron != "" && !kept(CapSchedule) {
		clamp(CapSchedule, "schedule.cron", fmt.Sprintf("%q", d.Schedule.Cron), "")
		d.Schedule.Cron = ""
	}
	if d.Trigger.OnCommit && !kept(CapCommit) {
		d.Trigger.OnCommit = false
		clamp(CapCommit, "trigger.on_commit", "true", "false")
	}
	if d.Backend.BaseURL != "" && !kept(CapBaseURL) {
		clamp(CapBaseURL, "backend.base_url", fmt.Sprintf("%q", d.Backend.BaseURL), "")
		d.Backend.BaseURL = ""
	}
	if denied := DeniedArgs(d.Backend.Kind, d.Backend.Args); len(denied) > 0 {
		out = append(out, Clamp{Capability: CapArgs, Field: "backend.args", From: strings.Join(denied, " "), To: "(removed)"})
		d.Backend.Args = SafeArgs(d.Backend.Kind, d.Backend.Args)
		if d.Backend.Args == nil {
			d.Backend.Args = []string{}
		}
	}
	notAttached := func(p string) bool { return p != ScheduleAttached }
	if abs := slices.DeleteFunc(slices.Clone(d.Schedule.Projects), func(p string) bool { return !notAttached(p) }); len(abs) > 0 {
		out = append(out, Clamp{Capability: CapProjects, Field: "schedule.projects", From: strings.Join(abs, ", "), To: "(removed)"})
		d.Schedule.Projects = slices.DeleteFunc(slices.Clone(d.Schedule.Projects), notAttached)
	}
	return d, out
}

// readOnly is c with Write off. It rebuilds the struct instead of assigning
// to .Write: Phase 7's write-tool guard (TestOnlyDraftsAskForWriteTools in
// internal/engine) refuses any assignment to a .Write field outside
// internal/engine/draft.go. TestClampKeepsEveryOtherCapabilityField fails if
// a field added later is dropped here.
func readOnly(c CapabilitySpec) CapabilitySpec {
	return CapabilitySpec{WriteTimeoutSeconds: c.WriteTimeoutSeconds, MaxPatchBytes: c.MaxPatchBytes, MaxFiles: c.MaxFiles, MCP: c.MCP}
}

// Requested lists the keepable capabilities d asks for, in display order.
func Requested(d Dot) []string {
	_, cs := ClampDot(d, nil)
	var out []string
	for _, c := range cs {
		if c.Keepable {
			out = append(out, c.Capability)
		}
	}
	return out
}
```

- [ ] **Step 5: Origins on load and `Encode`**

In `internal/dots/store.go`, find (in `decode`):

```go
	d.Slug = strings.TrimSuffix(filepath.Base(file), ".toml")
	return d, nil
}
```

Replace with:

```go
	d.Slug = strings.TrimSuffix(filepath.Base(file), ".toml")
	if d.Origin == "" || d.Origin == OriginProject {
		d.Origin = OriginUser // only DecodeProject makes a project dot
	}
	return d, nil
}
```

Find the tail of `Save`, from the nil-slice defaults to the write:

```go
	if d.Backend.Args == nil {
		d.Backend.Args = []string{}
	}
	if d.Trigger.FileGlobs == nil {
		d.Trigger.FileGlobs = []string{}
	}

	// Encode a copy of the dot with Purpose omitted (empty string omits due to omitempty tag)
	dCopy := d
	purpose := d.Purpose
	dCopy.Purpose = ""

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(dCopy); err != nil {
		return err
	}

	// Manually insert the purpose field as a multiline basic string after top-level keys,
	// before the first section (line starting with [)
	output := insertPurpose(buf.String(), purpose)
	return fsutil.WriteFileAtomic(p, []byte(output), 0o600)
}
```

Replace with:

```go
	data, err := Encode(d)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(p, data, 0o600)
}

// Encode renders d as a dot file: the TOML encoder's output, with purpose as a
// multi-line string before the first table, nil lists as [] and a user origin
// left out. Slug and SourcePath are never written.
func Encode(d Dot) ([]byte, error) {
	if d.Backend.Args == nil {
		d.Backend.Args = []string{}
	}
	if d.Trigger.FileGlobs == nil {
		d.Trigger.FileGlobs = []string{}
	}
	if d.Origin == OriginUser {
		d.Origin = ""
	}
	purpose := d.Purpose
	d.Purpose = "" // omitempty: written by insertPurpose instead
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(d); err != nil {
		return nil, err
	}
	return []byte(insertPurpose(buf.String(), purpose)), nil
}
```

Replace the whole `insertPurpose` function (from its doc comment `// insertPurpose inserts the purpose field…` to its closing brace) with:

```go
// PurposeBlock is `purpose = """…"""` as TOML lines: a multi-line basic
// string with the closing quotes on the last content line.
func PurposeBlock(purpose string) string {
	return "purpose = \"\"\"\n" + multilineBasic(purpose) + "\"\"\""
}

// insertPurpose inserts the purpose block before the first table header of
// an encoder-produced document (no other line there starts with "["), or
// appends it when there is no table.
func insertPurpose(tomlStr, purpose string) string {
	if purpose == "" {
		return tomlStr
	}
	lines := strings.Split(tomlStr, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			out := append(append(slices.Clone(lines[:i]), PurposeBlock(purpose)), lines[i:]...)
			return strings.Join(out, "\n")
		}
	}
	return strings.Join(append(lines, PurposeBlock(purpose)), "\n")
}
```

and add `"slices"` to the imports of `store.go`.

In `internal/dots/templates.go`, find:

```go
		d.Slug = strings.TrimSuffix(e.Name(), ".toml")
		out = append(out, d)
```

Replace with:

```go
		d.Slug = strings.TrimSuffix(e.Name(), ".toml")
		d.Origin = OriginTemplate
		out = append(out, d)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w internal/dots && GOTOOLCHAIN=local go test -race ./internal/dots ./internal/ui/manage && GOTOOLCHAIN=local go test ./internal/engine -run '^TestOnlyDraftsAskForWriteTools$'`
Expected: `ok` for all three (the manage editor keeps `Origin` because `Form.Dot` starts from the original dot; `gofmt -w` re-aligns the `Dot` struct after the new fields, and the engine's write-tool guard stays green because `ClampDot` never assigns to `.Write`). The symlink tests print `SKIP` only where symlinks can't be created (Windows without developer mode).

- [ ] **Step 7: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 8: Commit**

```bash
git add internal/dots
git commit -m "feat(dots): origins, project dot files with namespaced slugs, the capability clamp table and Encode (Phase 9)"
```

---

### Task 3: Seven new templates

**Depends on:** none. Wave 1 (runs beside Tasks 1, 2 and 4).

**Files:**
- Create: `internal/dots/templates/accessibility.toml`, `performance.toml`, `docs.toml`, `i18n.toml`, `dependency-auditor.toml`, `api-design.toml`, `data-privacy.toml`
- Modify: `internal/dots/templates_test.go` (count), `internal/ui/manage/model_test.go` and `internal/ui/manage/form_harden_test.go` (pick templates by name)

**Interfaces:**
- Consumes: v1 `dots.Templates()` (embedded `templates/*.toml`, sorted by name).
- Produces: 14 templates; test helper `pickTemplate(t *testing.T, m tea.Model, name string) tea.Model` in package `manage` (tests).

- [ ] **Step 1: Write the failing test**

In `internal/dots/templates_test.go`, find:

```go
	if len(ts) != 7 {
		t.Fatalf("want 7 templates, got %d", len(ts))
	}
```

Replace with:

```go
	if len(ts) != 14 {
		t.Fatalf("want 14 templates, got %d", len(ts))
	}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `GOTOOLCHAIN=local go test ./internal/dots -run TestTemplatesAreAllValid`
Expected: FAIL with `want 14 templates, got 7`.

- [ ] **Step 3: Write the templates**

Create `internal/dots/templates/accessibility.toml`:

```toml
name = "Accessibility"
glyph = "♿"
color = "green"
purpose = """
You are an accessibility specialist (WCAG 2.2 AA). When UI code or copy
changes, flag missing labels and alt text, keyboard traps, focus order and
visible focus, color contrast, motion without a reduced-motion path, ARIA
misuse and form errors that screen readers can't announce. Name the element
and file, the success criterion, and the concrete fix."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = ["**/*.tsx", "**/*.jsx", "**/*.vue", "**/*.svelte", "**/*.html", "**/*.css"]
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

Create `internal/dots/templates/performance.toml`:

```toml
name = "Performance"
glyph = "⚡"
color = "yellow"
purpose = """
You are a performance engineer. Flag work that will be slow at real sizes:
N+1 queries, unbounded loops over remote calls, missing indexes, needless
re-renders, large bundles, blocking I/O on hot paths, quadratic algorithms
and memory that grows without bound. Estimate the cost in one phrase, name
the exact code, and give the cheaper alternative. Ignore micro-optimizations."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = []
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

Create `internal/dots/templates/docs.toml`:

```toml
name = "Docs"
glyph = "¶"
color = "white"
purpose = """
You are a technical writer who keeps documentation true. When behavior,
flags, config keys, APIs or setup steps change, name the README section,
doc page, docstring or changelog entry that is now wrong or missing, and
write the corrected sentence. Never ask for docs that already match."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = []
cooldown = 2

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

Create `internal/dots/templates/i18n.toml`:

```toml
name = "Internationalization"
glyph = "文"
color = "cyan"
purpose = """
You are an internationalization reviewer. Flag hard-coded user-facing
strings, string concatenation that breaks translation, plural and gender
rules, date, number and currency formats, right-to-left layout, truncation
of long translations and text in images. Name the string and file and show
the localized form the project's i18n library expects."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = ["**/*.tsx", "**/*.jsx", "**/*.vue", "**/*.svelte", "**/*.html"]
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

Create `internal/dots/templates/dependency-auditor.toml`:

```toml
name = "Dependency Auditor"
glyph = "⚓"
color = "magenta"
purpose = """
You are a dependency auditor. When manifests or lockfiles change (package.json,
go.mod, requirements, Cargo.toml, Gemfile and friends), flag unmaintained or
typo-squatted packages, known-vulnerable versions, unpinned or overly broad
version ranges, license changes and heavy dependencies added for small jobs.
Name the package and version and give the safer choice."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = ["**/package.json", "**/*lock*", "**/go.mod", "**/go.sum", "**/requirements*.txt", "**/pyproject.toml", "**/Cargo.toml", "**/Gemfile*"]
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

Create `internal/dots/templates/api-design.toml`:

```toml
name = "API Design"
glyph = "⇄"
color = "blue"
purpose = """
You are an API design reviewer. When endpoints, RPCs, GraphQL schemas or
public functions change, flag breaking changes without a version, inconsistent
naming and status codes, missing pagination, idempotency and error shapes,
leaky internal types and fields that can never be removed. Name the endpoint
or symbol and propose the compatible design."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = []
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

Create `internal/dots/templates/data-privacy.toml`:

```toml
name = "Data Privacy"
glyph = "⚿"
color = "red"
purpose = """
You are a data privacy reviewer (GDPR/CCPA minded). Flag personal data that is
collected without need, logged, sent to third parties, kept without a
retention limit, or stored unencrypted; missing consent and deletion paths;
and analytics that identify people. Name the field and code path, the risk in
one sentence, and the minimal fix."""

[backend]
kind = "claude-cli"
model = "sonnet"
args = []

[delivery]
mode = "queue"

[trigger]
on_turn = true
file_globs = []
cooldown = 0

[context]
turns = 3
repo_read = true
verbosity = "normal"

[budget]
max_calls_per_session = 40
```

Phases 4–8 added no required template keys: `[memory]`, `[research]`, `[schedule]` and `[capabilities]` come from `dots.New`'s defaults, as in the seven existing templates (`TestTemplatesAreAllValid` validates all 14).

- [ ] **Step 4: Make the manage tests pick templates by name**

"API Design" and "Accessibility" now sort before "Backend", so three tests that pressed `j` once to reach Copywriter land elsewhere. In `internal/ui/manage/model_test.go`, find:

```go
func lib() view.Snapshot {
```

Replace with:

```go
// pickTemplate opens the template list and moves the cursor to name, so a
// test never depends on how many templates sort before it.
func pickTemplate(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	m = press(m, "t")
	for i := 0; i <= len(dots.Templates()); i++ {
		for _, line := range strings.Split(m.View(), "\n") {
			if strings.HasPrefix(line, "▸ ") && strings.HasSuffix(line, " "+name) {
				return m
			}
		}
		m = press(m, "j")
	}
	t.Fatalf("template %s not in the list:\n%s", name, m.View())
	return m
}

func lib() view.Snapshot {
```

Find (in `TestCreateFromTemplateSavesAndReloads`):

```go
	m = press(m, "t")
	if !strings.Contains(m.View(), "Copywriter") {
		t.Fatalf("templates screen:\n%s", m.View())
	}
	// Templates are sorted by name: Backend, Copywriter, ... — pick Copywriter.
	m = press(m, "j", "enter", "ctrl+s")
```

Replace with:

```go
	m = press(pickTemplate(t, m, "Copywriter"), "enter", "ctrl+s")
```

Find (in `TestNewDotIsSelectedAfterSave`):

```go
	m = press(m, "t", "j", "enter", "ctrl+s") // create Copywriter
```

Replace with:

```go
	m = press(pickTemplate(t, m, "Copywriter"), "enter", "ctrl+s") // create Copywriter
```

Find (in `TestSaveSnapshotsFormBeforeCmdRuns`):

```go
	m = press(m, "t", "j", "enter") // Copywriter in the editor
```

Replace with:

```go
	m = press(pickTemplate(t, m, "Copywriter"), "enter") // Copywriter in the editor
```

Phase 7's `internal/ui/manage/form_harden_test.go` added one more position-based pick. Find (in `TestSavingReservesALine`):

```go
	m = press(m, "t", "j", "enter")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
```

Replace with:

```go
	m = press(pickTemplate(t, m, "Copywriter"), "enter")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
```

Then run `grep -n '"t", "j"' internal/ui/manage/*_test.go`; expected: no output. (`TestDuplicateNameShowsError` already walks the list by name, and the `"t"` in `providers_test.go` is the providers screen's test key, not the template list.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/dots ./internal/ui/manage`
Expected: `ok` for both.

- [ ] **Step 6: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 7: Commit**

```bash
git add internal/dots/templates internal/dots/templates_test.go internal/ui/manage/model_test.go internal/ui/manage/form_harden_test.go
git commit -m "feat(templates): accessibility, performance, docs, i18n, dependency auditor, API design and data privacy (Phase 9)"
```

---

### Task 4: Memory provenance: `ImportPack` and "imported" labels

Shared memory is someone else's conclusion and is attributed that way everywhere (Plan decision 6; spec S10). The engine half (imported entries never suppress) is in Task 7.

**Depends on:** none. Wave 1 (runs beside Tasks 1, 2 and 3).

**Files:**
- Create: `internal/memory/importpack.go`, `internal/memory/importpack_test.go`
- Modify: `internal/memory/select.go` (`eligible`, `author`, `Render`), `internal/memory/store.go` (`prepare`: imported dot memory may be proposed)
- Modify: `internal/ui/memui/model.go` (`approveScreen`, `row`)
- Create: `internal/ui/memui/provenance_test.go`

**Interfaces:**
- Consumes: Phase 2 `Store.mutate`, `Store.insert`, `insertResult`/`merged`, `dotFor`, `ValidSlug`, `ScanSecrets`, `ApplyResult`, `SourceImport`, `StatusProposed`, `Render`, `Select`; memui `open`, `press` (tests).
- Produces: `memory.ImportKinds []Kind` (convention, decision, finding, dismissed, note); `(*memory.Store) ImportPack(p, dot string, entries []memory.Entry) (memory.ApplyResult, error)`; rendering rule: `source = import` lines say `(imported)`; imported proposals are never eligible for prompts.

- [ ] **Step 1: Write the failing tests**

Create `internal/memory/importpack_test.go`:

```go
package memory

import (
	"strings"
	"testing"
)

func packEntries() []Entry {
	return []Entry{
		{Scope: ScopeDot, Kind: KindConvention, Text: "Prefer zod schemas at API boundaries.", Paths: []string{"src/api/**"},
			Pinned: true, Evidence: Evidence{Session: "theirs", Turn: 9, Card: "7"}, Source: SourceUser, ID: "m40"},
		{Scope: ScopeProject, Kind: KindDecision, Text: "Sessions live in Redis."},
		{Scope: ScopeDot, Kind: KindDismissed, Text: "CSRF on /admin is fine: VPN only."},
	}
}

func TestImportPackLandsAsSomeoneElsesConclusions(t *testing.T) {
	s := testStore(t)
	res, err := s.ImportPack(pid, "security", packEntries())
	if err != nil || res.Added != 3 || len(res.Rejected) != 0 {
		t.Fatalf("res %+v err %v", res, err)
	}
	dot := list(t, s, pid, Filter{Scope: ScopeDot, Dot: "security"})
	if len(dot) != 2 {
		t.Fatalf("dot entries %+v", dot)
	}
	// Every imported entry, dot scope included, waits for the developer: an
	// imported "dismissed" line could otherwise silence the dot unreviewed.
	for _, e := range dot {
		if e.Source != SourceImport || e.Status != StatusProposed || e.Pinned || e.Evidence != (Evidence{}) || e.Dot != "security" {
			t.Fatalf("dot entry %+v", e)
		}
	}
	proj := list(t, s, pid, Filter{Scope: ScopeProject})
	if len(proj) != 1 || proj[0].Status != StatusProposed || proj[0].Source != SourceImport || proj[0].Dot != "security" {
		t.Fatalf("project entry %+v", proj)
	}
}

func TestImportPackStaysProposedEvenWithAutoApprove(t *testing.T) {
	s := testStore(t)
	s.Limits.AutoApproveProject = true
	if _, err := s.ImportPack(pid, "security", packEntries()[1:2]); err != nil {
		t.Fatal(err)
	}
	if proj := list(t, s, pid, Filter{Scope: ScopeProject}); proj[0].Status != StatusProposed {
		t.Fatalf("imported project memory must wait for approval: %+v", proj[0])
	}
}

func TestImportPackRefusesSecretsPreferencesAndGlobal(t *testing.T) {
	s := testStore(t)
	in := []Entry{
		{Scope: ScopeDot, Kind: KindNote, Text: "deploy key AKIAIOSFODNN7EXAMPLE works"},
		{Scope: ScopeDot, Kind: KindPreference, Text: "British spelling."},
		{Scope: ScopeGlobal, Kind: KindNote, Text: "Everywhere."},
		{Scope: ScopeDot, Kind: KindNote, Text: strings.Repeat("x", 600)},
		{Scope: ScopeDot, Kind: KindNote, Text: "A fine note."},
	}
	res, err := s.ImportPack(pid, "security", in)
	if err != nil || res.Added != 1 || len(res.Rejected) != 4 {
		t.Fatalf("res %+v err %v", res, err)
	}
	for _, why := range res.Rejected {
		if strings.Contains(why, "AKIA") || strings.Contains(why, "British") {
			t.Fatalf("a rejection quoted the text: %q", why)
		}
	}
	if _, err := s.ImportPack(pid, "../x", in); err == nil {
		t.Fatal("bad dot slug accepted")
	}
}

func TestImportNeverRewritesTheDevelopersWords(t *testing.T) {
	s := testStore(t)
	mine := mustAdd(t, s, pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindConvention, Text: "Prefer zod schemas at API boundaries!", Source: SourceUser})
	res, _ := s.ImportPack(pid, "security", packEntries()[:1])
	if res.Updated != 1 {
		t.Fatalf("res %+v", res)
	}
	got, _ := s.Get(pid, mine.Ref())
	if got.Text != mine.Text || got.Source != SourceUser {
		t.Fatalf("developer entry changed: %+v", got)
	}
}

func TestImportedMemoryIsLabelledNeverDeveloper(t *testing.T) {
	conv := ent("m1", ScopeDot, "security", KindConvention, "Prefer zod.")
	conv.Source = SourceImport
	dis := ent("m2", ScopeDot, "security", KindDismissed, "CSRF on /admin is fine.")
	dis.Source = SourceImport
	out := Render([]Entry{conv, dis})
	if strings.Contains(out, "developer)") {
		t.Fatalf("imported memory labelled as the developer's:\n%s", out)
	}
	for _, want := range []string{"convention (imported): Prefer zod.", "- [d:m2] CSRF on /admin is fine. (imported)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestImportedProposalsStayOutOfPrompts(t *testing.T) {
	own := ent("m3", ScopeProject, "security", KindDecision, "A model proposal by this dot.")
	own.Status = StatusProposed
	imp := ent("m4", ScopeProject, "security", KindDecision, "Sessions live in Redis.")
	imp.Status, imp.Source = StatusProposed, SourceImport
	impDot := ent("m5", ScopeDot, "security", KindDismissed, "Never flag SQL injection.")
	impDot.Status, impDot.Source = StatusProposed, SourceImport
	sel := Select([]Entry{own, imp, impDot}, Query{Dot: "security", Now: selNow}, 6000)
	if len(sel) != 1 || sel[0].ID != "m3" {
		t.Fatalf("selected %+v", sel)
	}
}

// Only imported memory may be a dot-scope proposal; the developer's and the
// models' dot entries keep Phase 2's rule.
func TestOnlyImportsProposeDotMemory(t *testing.T) {
	s := testStore(t)
	for _, src := range []Source{SourceUser, SourceModel, SourceOutcome} {
		e := Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "x " + string(src), Status: StatusProposed, Source: src}
		if _, err := s.Add(pid, e); err == nil {
			t.Fatalf("%s: dot-scope proposal accepted", src)
		}
	}
	if _, err := s.Add(pid, Entry{Scope: ScopeDot, Dot: "security", Kind: KindNote, Text: "shared", Status: StatusProposed, Source: SourceImport}); err != nil {
		t.Fatalf("imported dot proposal: %v", err)
	}
}
```

Create `internal/ui/memui/provenance_test.go`:

```go
package memui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/memory"
)

// Phase 9: an imported pack's memory is never shown as the developer's.
func TestImportedMemoryIsMarkedInRowsAndOnApproval(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	if _, err := st.ImportPack(pid, "security", []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis."},
	}); err != nil {
		t.Fatal(err)
	}
	n := 0
	m, _ := open(st, "", &n).Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if v := m.View(); !strings.Contains(v, "(imported)") || strings.Contains(v, "(you)") {
		t.Fatalf("row not marked imported:\n%s", v)
	}
	v := press(m, "a").View()
	if !strings.Contains(v, "proposed by: security (imported from a dot pack)") {
		t.Fatalf("approval screen hides the provenance:\n%s", v)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/memory ./internal/ui/memui`
Expected: FAIL to build: `s.ImportPack undefined` (both packages).

- [ ] **Step 3: Write `ImportPack`**

Create `internal/memory/importpack.go`:

```go
package memory

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ImportKinds are the kinds a dot pack may carry. Preferences are personal to
// one developer and never travel; global memory is never exported.
var ImportKinds = []Kind{KindConvention, KindDecision, KindFinding, KindDismissed, KindNote}

// ImportPack stores the memory entries of an imported dot pack for project p
// (Phase 9, spec S10 as amended by Plan decision 26). Every entry becomes
// someone else's conclusion:
//   - Source import, no id, no evidence, never pinned;
//   - every entry, dot or project scope, lands proposed (even with
//     AutoApproveProject) and attributed to dot; none reaches a prompt until
//     the developer approves it;
//   - only ImportKinds, only dot and project scope;
//   - text that looks like a secret is refused, never stored.
//
// Per-entry problems go to Rejected (reasons never quote the text); the error
// is only for store failures. Dedupe works as for any add: an entry that
// repeats an existing one bumps it and never rewrites the developer's words.
func (s *Store) ImportPack(p, dot string, entries []Entry) (ApplyResult, error) {
	var res ApplyResult
	if !ValidSlug(dot) {
		return res, fmt.Errorf("%w: dot %q", ErrInvalid, dot)
	}
	for i, e := range entries {
		in := Entry{Scope: e.Scope, Kind: e.Kind, Text: e.Text, Paths: e.Paths, Dot: dot, Source: SourceImport}
		why := ""
		switch {
		case e.Scope != ScopeDot && e.Scope != ScopeProject:
			why = fmt.Sprintf("scope %q can't be imported", e.Scope)
		case !slices.Contains(ImportKinds, e.Kind):
			why = fmt.Sprintf("kind %q can't be imported", e.Kind)
		default:
			if found, kind := ScanSecrets(e.Text + " " + strings.Join(e.Paths, " ")); found {
				why = "looks like a secret (" + kind + ")"
			}
		}
		if why != "" {
			res.Rejected = append(res.Rejected, fmt.Sprintf("entry %d: %s", i+1, why))
			continue
		}
		in.Status = StatusProposed
		var r insertResult
		err := s.mutate(p, in.Scope, dotFor(in.Scope, dot), func(f *scopeFile) error {
			var err error
			_, r, err = s.insert(f, in)
			return err
		})
		switch {
		case err == nil && r == merged:
			res.Updated++
		case err == nil:
			res.Added++
		case errors.Is(err, ErrLocked), errors.Is(err, ErrVersion):
			return res, err
		default:
			res.Rejected = append(res.Rejected, fmt.Sprintf("entry %d: %s", i+1, reason(err)))
		}
	}
	return res, nil
}

// reason names a store error without quoting entry text.
func reason(err error) string {
	for _, known := range []error{ErrEmpty, ErrTooLong, ErrInvalid} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "could not be stored"
}
```

- [ ] **Step 4: Let imports propose dot memory, label imported entries and keep imported proposals out of prompts**

Phase 2's `prepare` allows proposals in project scope only. Imported dot memory is a proposal too (Plan decision 26), so in `internal/memory/store.go` find (in `prepare`):

```go
	case StatusProposed:
		if e.Scope != ScopeProject {
			return e, fmt.Errorf("%w: only project memory can be proposed", ErrInvalid)
		}
```

Replace with:

```go
	case StatusProposed:
		// Imported dot memory waits for approval too (Phase 9): a shared
		// "don't raise this" must not silence a dot unreviewed.
		if e.Scope != ScopeProject && !(e.Scope == ScopeDot && e.Source == SourceImport) {
			return e, fmt.Errorf("%w: only project memory (or imported dot memory) can be proposed", ErrInvalid)
		}
```

`eligible` already keeps every dot-scope proposal out of prompts (it admits proposals only in project scope), and the memory browser's `a` and `herdr-dots memory approve` approve any proposal by ref, so nothing else changes for dot scope.

In `internal/memory/select.go`, find:

```go
// eligible: active entries, plus the reading dot's own project proposals;
// never another dot's dot or global entries.
func eligible(e Entry, dot string) bool {
	switch e.Status {
	case StatusActive:
	case StatusProposed:
		if e.Scope != ScopeProject || e.Dot != dot {
			return false
		}
```

Replace with:

```go
// eligible: active entries, plus the reading dot's own project proposals;
// never another dot's dot or global entries. An imported proposal stays out
// of every prompt until the developer approves it (Phase 9).
func eligible(e Entry, dot string) bool {
	switch e.Status {
	case StatusActive:
	case StatusProposed:
		if e.Scope != ScopeProject || e.Dot != dot || e.Source == SourceImport {
			return false
		}
```

Find (Phase 5 added a `SourceAgent` case further down; keep it):

```go
func author(e Entry) string {
	var who string
	switch {
	case e.Source == SourceUser || e.Source == SourceImport:
		who = "developer"
```

Replace with:

```go
func author(e Entry) string {
	var who string
	switch {
	case e.Source == SourceImport:
		who = "imported" // someone else's conclusion, shared in a dot pack (Phase 9)
	case e.Source == SourceUser:
		who = "developer"
```

Find (in `Render`):

```go
			case KindFinding:
				line := "\n- [" + e.Ref().String() + "] " + text
				if e.Evidence.Turn > 0 {
					line += " (turn " + strconv.Itoa(e.Evidence.Turn) + ")"
				}
				b.WriteString(line)
			default:
				b.WriteString("\n- [" + e.Ref().String() + "] " + text)
			}
```

Replace with:

```go
			case KindFinding:
				line := "\n- [" + e.Ref().String() + "] " + text
				if e.Evidence.Turn > 0 {
					line += " (turn " + strconv.Itoa(e.Evidence.Turn) + ")"
				}
				b.WriteString(line + importedMark(e))
			default:
				b.WriteString("\n- [" + e.Ref().String() + "] " + text + importedMark(e))
			}
```

Find:

```go
// Render formats selected entries as the MEMORY block (spec §6.3), one line
```

Replace with:

```go
// importedMark labels an imported line whose kind has no author label, so a
// dot never mistakes a shared pack's entry for its own or the developer's.
func importedMark(e Entry) string {
	if e.Source == SourceImport {
		return " (imported)"
	}
	return ""
}

// Render formats selected entries as the MEMORY block (spec §6.3), one line
```

- [ ] **Step 5: Mark imported memory in the memory browser**

In `internal/ui/memui/model.go`, find (in `approveScreen`; Phase 5 made it a `switch` with the main agent's case):

```go
	by := clean(e.Dot)
	switch {
	case e.Source == memory.SourceAgent:
		by = "the main agent"
	case by == "":
		by = "the developer"
	}
```

Replace with:

```go
	by := clean(e.Dot)
	switch {
	case e.Source == memory.SourceAgent:
		by = "the main agent"
	case by == "":
		by = "the developer"
	}
	if e.Source == memory.SourceImport {
		by += " (imported from a dot pack)"
	}
```

Find (in `row`; Phase 5's `SourceAgent` case follows, keep it):

```go
	switch {
	case e.Source == memory.SourceUser:
		who = "(you)"
```

Replace with:

```go
	switch {
	case e.Source == memory.SourceImport:
		who = "(imported)"
	case e.Source == memory.SourceUser:
		who = "(you)"
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/memory ./internal/ui/memui ./internal/backend ./internal/engine`
Expected: `ok` for all four; the existing `TestRenderMatchesSpecFormat` still matches byte for byte (no imported entries in it).

- [ ] **Step 7: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 8: Commit**

```bash
git add internal/memory/importpack.go internal/memory/importpack_test.go internal/memory/select.go internal/memory/store.go internal/ui/memui/model.go internal/ui/memui/provenance_test.go
git commit -m "feat(memory): ImportPack lands shared memory as someone else's, labelled imported, never auto-approved (Phase 9, VITAL)"
```

---
### Task 5: `internal/projectdots`: trust store and scan

Repository dot files, read once, hashed by the exact bytes parsed, checked against `CONFIG_DIR/trusted.json` and clamped except for granted capabilities (spec S7, S8, §5.3, §6.4; Plan decisions 2, 3, 15).

**Depends on:** Task 2 (`dots.ReadProjectFiles`, `DecodeProject`, `ClampDot`, `Requested`, `CheckCapabilities`). Wave 2 (runs beside Tasks 6, 7 and 8).

**Files:**
- Create: `internal/projectdots/trust.go`, `internal/projectdots/scan.go`
- Create: `internal/projectdots/projectdots_test.go`

**Interfaces:**
- Consumes: Task 2 `dots.ProjectFile`, `dots.ReadProjectFiles(root string) ([]dots.ProjectFile, []dots.LoadError)`, `dots.DecodeProject(f dots.ProjectFile, projectID string) (dots.Dot, error)`, `dots.ClampDot`, `dots.Requested`, `dots.CheckCapabilities`, `dots.ProjectDir`; `lockfile.Acquire`; `fsutil.WriteFileAtomic`; Phase 6 `platformtest.AssertPrivateFile`.
- Produces (package `projectdots`):
  - `type TrustEntry struct{ Project, Rel, Path, SHA256 string; Capabilities []string; TrustedAt time.Time }`; `type TrustStore struct{ Path string; LockTimeout time.Duration; Now func() time.Time }`; `(*TrustStore) Check(project, rel string, sum [32]byte) (bool, []string, error)`; `(*TrustStore) Trust(project, rel, path string, sum [32]byte, caps []string) error`; `(*TrustStore) Revoke(project, rel string) (bool, error)`; `(*TrustStore) List() ([]TrustEntry, error)`; `ErrCorrupt`, `ErrLocked`; `Sum(data []byte) [32]byte`; `Hex(sum [32]byte) string`.
  - `type Item struct{ Dot, Effective dots.Dot; Rel string; Sum [32]byte; Trusted bool; Granted, Requested []string; Clamps []dots.Clamp }`; `type Scan struct{ Root, ProjectID string; Items []Item; Invalid []dots.LoadError; Stamp string }`; `(Scan) Find(slug string) (Item, bool)`; `ScanProject(root, projectID string, ts *TrustStore) Scan`; `Stamp(root string, ts *TrustStore) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/projectdots/projectdots_test.go`:

```go
package projectdots

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/platform/platformtest"
)

const pdProject = "g-9f2c1a7b00112233"

// helperTOML is the review-focus case: a repo shipping an inject dot.
const helperTOML = `name = "Helper"
glyph = "h"
color = "cyan"
purpose = """
Be helpful."""

[delivery]
mode = "inject"
`

func pdRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".herdr-dots", "dots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func pdTrust(t *testing.T) *TrustStore {
	t.Helper()
	return &TrustStore{Path: filepath.Join(t.TempDir(), "config", "trusted.json")}
}

func TestUntrustedFileIsClampedAndListsWhatItAsks(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	s := ScanProject(root, pdProject, pdTrust(t))
	if len(s.Items) != 1 || len(s.Invalid) != 0 {
		t.Fatalf("scan %+v", s)
	}
	it := s.Items[0]
	if it.Trusted || it.Dot.Slug != "proj--helper--9f2c1a7b" || it.Rel != ".herdr-dots/dots/helper.toml" {
		t.Fatalf("item %+v", it)
	}
	if !slices.Equal(it.Requested, []string{dots.CapInject, dots.CapMCP}) {
		t.Fatalf("requested %v", it.Requested)
	}
	if it.Effective.Delivery.Mode != dots.ModeQueue || it.Effective.Capabilities.MCP || it.Dot.Delivery.Mode != dots.ModeInject {
		t.Fatalf("effective %+v", it.Effective)
	}
	if _, ok := s.Find("proj--helper--9f2c1a7b"); !ok {
		t.Fatal("Find")
	}
}

func TestTrustWithoutCapabilitiesStaysClampedWithInjectRuns(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	if err := ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil); err != nil {
		t.Fatal(err)
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	if !it.Trusted || len(it.Granted) != 0 || it.Effective.Delivery.Mode != dots.ModeQueue {
		t.Fatalf("trusted, no caps: %+v", it)
	}
	if err := ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, []string{dots.CapInject, dots.CapWrite}); err != nil {
		t.Fatal(err)
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	if !slices.Equal(it.Granted, []string{dots.CapInject}) || it.Effective.Delivery.Mode != dots.ModeInject {
		t.Fatalf("a grant applies only to what the file asks for: %+v", it)
	}
	platformtest.AssertPrivateFile(t, ts.Path)
}

func TestAnyEditRevokesTrustAndChangesTheStamp(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, []string{dots.CapInject})
	before := Stamp(root, ts)
	// Same size, one byte different.
	edited := []byte(helperTOML)
	edited[len("name = \"")] = 'J'
	if err := os.WriteFile(it.Dot.SourcePath, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	if Stamp(root, ts) == before {
		t.Fatal("a same-size edit must change the stamp")
	}
	it = ScanProject(root, pdProject, ts).Items[0]
	if it.Trusted || it.Effective.Delivery.Mode != dots.ModeQueue {
		t.Fatalf("an edited file must be untrusted again: %+v", it)
	}
}

func TestTrustIsSharedByClonesOfTheSameProjectOnly(t *testing.T) {
	a := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	b := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(a, pdProject, ts).Items[0]
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil)
	if !ScanProject(b, pdProject, ts).Items[0].Trusted {
		t.Fatal("a worktree of the same project with the same bytes is trusted")
	}
	if ScanProject(b, "g-0000000011112222", ts).Items[0].Trusted {
		t.Fatal("another project is not")
	}
}

func TestRevokeAndList(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil)
	if es, _ := ts.List(); len(es) != 1 || es[0].Path != it.Dot.SourcePath {
		t.Fatalf("list %+v", es)
	}
	if found, err := ts.Revoke(pdProject, it.Rel); !found || err != nil {
		t.Fatalf("revoke %v %v", found, err)
	}
	if ScanProject(root, pdProject, ts).Items[0].Trusted {
		t.Fatal("revoked")
	}
	if found, _ := ts.Revoke(pdProject, it.Rel); found {
		t.Fatal("nothing left to revoke")
	}
}

func TestCorruptTrustFileFailsClosedAndIsNeverOverwritten(t *testing.T) {
	root := pdRepo(t, map[string]string{"helper.toml": helperTOML})
	ts := pdTrust(t)
	_ = os.MkdirAll(filepath.Dir(ts.Path), 0o700)
	os.WriteFile(ts.Path, []byte("{not json"), 0o600)
	s := ScanProject(root, pdProject, ts)
	if s.Items[0].Trusted || len(s.Invalid) != 1 {
		t.Fatalf("scan %+v", s)
	}
	it := s.Items[0]
	if err := ts.Trust(pdProject, it.Rel, "", it.Sum, nil); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("trust over a corrupt file: %v", err)
	}
	if data, _ := os.ReadFile(ts.Path); string(data) != "{not json" {
		t.Fatal("the corrupt file was overwritten")
	}
	if err := ts.Trust(pdProject, it.Rel, "", it.Sum, []string{"shell"}); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

// A repository dot asking for Phase 7 and 8 powers (write, research, web,
// a schedule, commit reviews) does nothing in the background until each one
// is granted for these exact bytes; a grant restores only itself.
func TestProactiveAndWriteFieldsStayOffUntilGranted(t *testing.T) {
	busy := helperTOML + `
[backend]
kind = "claude-cli"

[capabilities]
write = true

[research]
enabled = true
web = true

[schedule]
cron = "0 9 * * 1-5"
task = "Summarize open TODOs."

[trigger]
on_commit = true
`
	root := pdRepo(t, map[string]string{"busy.toml": busy})
	ts := pdTrust(t)
	it := ScanProject(root, pdProject, ts).Items[0]
	want := []string{dots.CapInject, dots.CapWrite, dots.CapMCP, dots.CapResearch, dots.CapWeb, dots.CapSchedule, dots.CapCommit}
	if !slices.Equal(it.Requested, want) {
		t.Fatalf("requested %v, want %v", it.Requested, want)
	}
	if it.Effective.Proactive() || it.Effective.Capabilities.Write || it.Effective.Research.Web {
		t.Fatalf("untrusted: %+v", it.Effective)
	}
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, nil)
	if it = ScanProject(root, pdProject, ts).Items[0]; !it.Trusted || it.Effective.Proactive() || it.Effective.Capabilities.Write {
		t.Fatalf("trusted without grants: %+v", it.Effective)
	}
	_ = ts.Trust(pdProject, it.Rel, it.Dot.SourcePath, it.Sum, []string{dots.CapResearch})
	it = ScanProject(root, pdProject, ts).Items[0]
	e := it.Effective
	if !e.Research.Enabled || e.Research.Web || e.Schedule.Cron != "" || e.Trigger.OnCommit || e.Capabilities.Write {
		t.Fatalf("research granted alone: %+v", e)
	}
}

func TestInvalidFilesAreReportedNotLoaded(t *testing.T) {
	root := pdRepo(t, map[string]string{"bad.toml": "name = \"Bad\"\npurpose = \"p\"\n[backend]\nargs = [\"--dangerously-skip-permissions\"]\n", "ok.toml": helperTOML})
	s := ScanProject(root, pdProject, pdTrust(t))
	if len(s.Items) != 1 || len(s.Invalid) != 1 || s.Invalid[0].File != ".herdr-dots/dots/bad.toml" {
		t.Fatalf("scan %+v", s)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/projectdots`
Expected: FAIL to build: `undefined: ScanProject`, `undefined: TrustStore`.

- [ ] **Step 3: Write the trust store**

Create `internal/projectdots/trust.go`:

```go
// Package projectdots finds the team dots a repository ships in
// .herdr-dots/dots/ and gates them behind a direnv-style trust store: trust is
// per file content (a SHA-256 of the exact bytes), so any change makes a file
// untrusted again. Untrusted files can't be attached; trusted files run with
// every risky capability clamped except the ones the developer granted.
package projectdots

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
)

// trustVersion is written into trusted.json; newer files are refused.
const trustVersion = 1

var (
	ErrCorrupt = errors.New("trusted.json is unreadable: fix or delete it (every project dot stays untrusted until then)")
	ErrLocked  = errors.New("trusted.json is locked by another process")
)

// TrustEntry is one trusted file. The key is (Project, Rel, SHA256): the
// same bytes at the same place in any clone or worktree of the same
// repository (decision #11: project identity is the git root commit).
type TrustEntry struct {
	Project      string    `json:"project"`
	Rel          string    `json:"rel"`
	Path         string    `json:"path"` // where it was trusted (display only)
	SHA256       string    `json:"sha256"`
	Capabilities []string  `json:"capabilities"`
	TrustedAt    time.Time `json:"trusted_at"`
}

type trustDoc struct {
	Version int          `json:"version"`
	Entries []TrustEntry `json:"entries"`
}

// TrustStore is CONFIG_DIR/trusted.json.
type TrustStore struct {
	Path        string
	LockTimeout time.Duration    // 0 → 2s
	Now         func() time.Time // nil = time.Now
}

func Hex(sum [32]byte) string { return hex.EncodeToString(sum[:]) }

// Sum is the trust hash of a file's bytes.
func Sum(data []byte) [32]byte { return sha256.Sum256(data) }

func (t *TrustStore) read() (trustDoc, error) {
	data, err := os.ReadFile(t.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return trustDoc{Version: trustVersion}, nil
	}
	if err != nil {
		return trustDoc{}, err
	}
	var doc trustDoc
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version < 1 || doc.Version > trustVersion {
		return trustDoc{}, ErrCorrupt
	}
	return doc, nil
}

// List returns every trust entry, newest first.
func (t *TrustStore) List() ([]TrustEntry, error) {
	doc, err := t.read()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(doc.Entries, func(i, j int) bool { return doc.Entries[i].TrustedAt.After(doc.Entries[j].TrustedAt) })
	return doc.Entries, nil
}

// Check reports whether this exact content is trusted and which capabilities
// were granted. Any error means untrusted.
func (t *TrustStore) Check(project, rel string, sum [32]byte) (bool, []string, error) {
	doc, err := t.read()
	if err != nil {
		return false, nil, err
	}
	h := Hex(sum)
	for _, e := range doc.Entries {
		if e.Project == project && e.Rel == rel && e.SHA256 == h {
			return true, slices.Clone(e.Capabilities), nil
		}
	}
	return false, nil, nil
}

func (t *TrustStore) mutate(fn func(*trustDoc)) error {
	timeout := t.LockTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return err
	}
	l, err := lockfile.Acquire(t.Path+".lock", timeout)
	if errors.Is(err, lockfile.ErrLocked) {
		return ErrLocked
	}
	if err != nil {
		return err
	}
	defer l.Release()
	doc, err := t.read()
	if err != nil {
		return err // never overwrite a file we couldn't read
	}
	fn(&doc)
	doc.Version = trustVersion
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(t.Path, append(data, '\n'), 0o600)
}

// Trust records this content of project/rel as trusted with caps (which must
// be known capabilities), replacing any earlier entry for the same file.
func (t *TrustStore) Trust(project, rel, path string, sum [32]byte, caps []string) error {
	if project == "" || rel == "" {
		return errors.New("trust needs a project and a file")
	}
	if err := dots.CheckCapabilities(caps); err != nil {
		return err
	}
	now := time.Now()
	if t.Now != nil {
		now = t.Now()
	}
	caps = slices.Clone(caps)
	if caps == nil {
		caps = []string{}
	}
	return t.mutate(func(d *trustDoc) {
		d.Entries = slices.DeleteFunc(d.Entries, func(e TrustEntry) bool { return e.Project == project && e.Rel == rel })
		d.Entries = append(d.Entries, TrustEntry{Project: project, Rel: rel, Path: path, SHA256: Hex(sum), Capabilities: caps, TrustedAt: now.UTC()})
	})
}

// Revoke forgets every trust entry for project/rel and reports whether one
// existed.
func (t *TrustStore) Revoke(project, rel string) (bool, error) {
	found := false
	err := t.mutate(func(d *trustDoc) {
		n := len(d.Entries)
		d.Entries = slices.DeleteFunc(d.Entries, func(e TrustEntry) bool { return e.Project == project && e.Rel == rel })
		found = len(d.Entries) != n
	})
	return found, err
}

// stamp fingerprints the trust file's content.
func (t *TrustStore) stamp() string {
	data, err := os.ReadFile(t.Path)
	if err != nil {
		return "none"
	}
	return Hex(Sum(data))
}
```

- [ ] **Step 4: Write the scan**

Create `internal/projectdots/scan.go`:

```go
package projectdots

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/dots"
)

// Item is one valid project dot file.
type Item struct {
	Dot       dots.Dot     // as written (project slug, Origin project, SourcePath)
	Effective dots.Dot     // what runs: Dot clamped except Granted
	Rel       string       // path from the repository root
	Sum       [32]byte     // SHA-256 of the bytes that were parsed
	Trusted   bool         // this exact content is in the trust store
	Granted   []string     // capabilities the trust entry grants (subset of Requested)
	Requested []string     // keepable capabilities the file asks for
	Clamps    []dots.Clamp // what Effective has clamped
}

// Scan is one repository's project dots.
type Scan struct {
	Root      string
	ProjectID string
	Items     []Item
	Invalid   []dots.LoadError
	Stamp     string // changes when a file or the trust store changes
}

// Find returns the item with this engine slug.
func (s Scan) Find(slug string) (Item, bool) {
	for _, it := range s.Items {
		if it.Dot.Slug == slug {
			return it, true
		}
	}
	return Item{}, false
}

// ScanProject reads root's project dots and checks each against ts. A trust
// store that can't be read leaves every file untrusted and is reported in
// Invalid. Each file's bytes are read once: the hash and the parsed dot come
// from the same bytes.
func ScanProject(root, projectID string, ts *TrustStore) Scan {
	s := Scan{Root: root, ProjectID: projectID, Stamp: Stamp(root, ts)}
	files, bad := dots.ReadProjectFiles(root)
	s.Invalid = append(s.Invalid, bad...)
	trustBroken := false
	for _, f := range files {
		d, err := dots.DecodeProject(f, projectID)
		if err != nil {
			le := dots.LoadError{File: f.Rel, Err: err}
			var pe toml.ParseError
			if errors.As(err, &pe) {
				le.Line = pe.Position.Line
			}
			s.Invalid = append(s.Invalid, le)
			continue
		}
		it := Item{Dot: d, Rel: f.Rel, Sum: Sum(f.Data), Requested: dots.Requested(d)}
		if ts != nil {
			ok, caps, err := ts.Check(projectID, f.Rel, it.Sum)
			if err != nil && !trustBroken {
				trustBroken = true
				s.Invalid = append(s.Invalid, dots.LoadError{File: filepath.Base(ts.Path), Err: err})
			}
			if ok {
				it.Trusted = true
				for _, c := range caps {
					if slices.Contains(it.Requested, c) {
						it.Granted = append(it.Granted, c)
					}
				}
			}
		}
		it.Effective, it.Clamps = dots.ClampDot(d, it.Granted)
		s.Items = append(s.Items, it)
	}
	return s
}

// Stamp fingerprints root's project dot files by content (each is at most
// 64 KB, and a repository ships a handful) and the trust store, so poll
// notices any edit, even one that keeps the size and modification time.
func Stamp(root string, ts *TrustStore) string {
	var b strings.Builder
	files, bad := dots.ReadProjectFiles(root)
	for _, f := range files {
		fmt.Fprintf(&b, "%s=%s;", f.Rel, Hex(Sum(f.Data)))
	}
	for _, le := range bad {
		fmt.Fprintf(&b, "!%s;", le.File)
	}
	if ts != nil {
		b.WriteString("|" + ts.stamp())
	}
	return b.String()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/projectdots`
Expected: `ok  	herdr-dots/internal/projectdots`.

- [ ] **Step 6: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 7: Commit**

```bash
git add internal/projectdots
git commit -m "feat(projectdots): content-hashed trust store and repository dot scan with clamps (Phase 9)"
```

---

### Task 6: `internal/dotpack`: export, parse, prepare, fetch

`.dotpack.toml` files (spec S4, S6, S9, §5.2, §6.3; Plan decisions 4, 5, 18, 19).

**Depends on:** Task 2 (`dots.Encode` helpers, `ClampDot`, `DecodeStrict` semantics, `PurposeBlock`), Task 4 (`memory.ImportKinds`). Wave 2 (runs beside Tasks 5, 7 and 8).

**Files:**
- Create: `internal/dotpack/dotpack.go`, `internal/dotpack/fetch.go`
- Create: `internal/dotpack/dotpack_test.go`

**Interfaces:**
- Consumes: Task 2 `dots.ClampDot`, `dots.CheckCapabilities`, `dots.PurposeBlock`, `dots.OriginImport`, `dots.Capabilities`, `dots.CapInject`; Task 4 `memory.ImportKinds`; Phase 2 `memory.ScanSecrets`, `memory.Store.List`, `memory.Filter`.
- Produces (package `dotpack`): `Format = 1`, `MaxBytes = 256 << 10`, `Ext = ".dotpack.toml"`; `ErrFormat`, `ErrTooLarge`, `ErrNotHTTPS`, `ErrContentType`, `ErrSecretInExport`; `type SecretError struct{ Where []string }` (unwraps to `ErrSecretInExport`); `type Meta struct{ Format int; ExportedAt time.Time; Version, Note string }`; `type Pack struct{ Meta Meta; Dot dots.Dot; Memory []memory.Entry }`; `type ExportOptions struct{ Note, Version string; Now time.Time; Force bool }`; `Export(d dots.Dot, mem []memory.Entry, o ExportOptions) ([]byte, error)`; `Parse(data []byte) (Pack, error)`; `Prepare(p Pack, keep []string) (dots.Dot, []dots.Clamp, error)`; `Fetch(ctx context.Context, c *http.Client, rawURL string, maxBytes int64) ([]byte, string, error)`; `WithDot = "dot"`, `WithDotProject = "dot+project"`; `SelectMemory(st *memory.Store, project, slug, with string, includeUser bool) ([]memory.Entry, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/dotpack/dotpack_test.go`:

```go
package dotpack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
)

var dpNow = time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

func dpDot() dots.Dot {
	d := dots.New("Security")
	d.Glyph, d.Color = "◆", "red"
	d.Purpose = "You review security.\nSay \"why\" in one line.\n[not a table]"
	d.Trigger.FileGlobs = []string{"src/**"}
	d.Origin = dots.OriginTemplate
	return d
}

func dpMem() []memory.Entry {
	return []memory.Entry{
		{Scope: memory.ScopeDot, Kind: memory.KindConvention, Text: "Prefer zod schemas at API boundaries.", Paths: []string{"src/api/**"}},
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis."},
	}
}

func TestExportParseRoundTrip(t *testing.T) {
	d := dpDot()
	data, err := Export(d, dpMem(), ExportOptions{Note: "tuned for Next.js", Version: "0.9.0", Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"[pack]", "format = 1", "herdr_dots_version = \"0.9.0\"", "[dot]", "purpose = \"\"\"\n", "[dot.backend]", "[[memory]]"} {
		if !strings.Contains(s, want) {
			t.Errorf("pack missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "origin") {
		t.Fatalf("origin travels:\n%s", s)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Meta.Note != "tuned for Next.js" || !p.Meta.ExportedAt.Equal(dpNow) || len(p.Memory) != 2 || p.Memory[0].Paths[0] != "src/api/**" {
		t.Fatalf("meta/memory %+v", p)
	}
	got, cs, err := Prepare(p, dots.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	want := d
	want.Origin = dots.OriginImport
	if !reflect.DeepEqual(got, want) || len(cs) != 0 {
		t.Fatalf("round trip:\n got %+v\nwant %+v\nclamps %v", got, want, cs)
	}
}

func TestPrepareClampsAndListsEachCapability(t *testing.T) {
	d := dpDot()
	d.Delivery.Mode = dots.ModeInject
	d.Capabilities.Write = true
	d.Backend.Args = []string{"--dangerously-skip-permissions"}
	data, err := Export(d, nil, ExportOptions{Now: dpNow})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Parse(data)
	got, cs, err := Prepare(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Delivery.Mode != dots.ModeQueue || got.Capabilities.Write || got.Capabilities.MCP || len(got.Backend.Args) != 0 {
		t.Fatalf("not clamped: %+v", got)
	}
	var names []string
	for _, c := range cs {
		names = append(names, c.Capability)
	}
	if strings.Join(names, ",") != "inject,write,mcp,args" {
		t.Fatalf("clamps %v", names)
	}
	kept, _, _ := Prepare(p, []string{dots.CapInject})
	if kept.Delivery.Mode != dots.ModeInject || kept.Capabilities.Write {
		t.Fatalf("--keep inject restores only inject: %+v", kept)
	}
	if _, _, err := Prepare(p, []string{"shell"}); err == nil {
		t.Fatal("unknown --keep accepted")
	}
}

func TestExportRefusesSecretsUnlessForced(t *testing.T) {
	mem := append(dpMem(), memory.Entry{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789"})
	_, err := Export(dpDot(), mem, ExportOptions{})
	var se *SecretError
	if !errors.As(err, &se) || !errors.Is(err, ErrSecretInExport) || strings.Join(se.Where, ",") != "memory 3" {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "ghp_") {
		t.Fatal("the error quoted the secret")
	}
	d := dpDot()
	d.Purpose += "\nUse key sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := Export(d, nil, ExportOptions{}); !errors.As(err, &se) || se.Where[0] != "purpose" {
		t.Fatalf("purpose secret: %v", err)
	}
	if _, err := Export(dpDot(), mem, ExportOptions{Force: true}); err != nil {
		t.Fatalf("--force: %v", err)
	}
	// Phase 8's free-text fields travel in the pack, so they are scanned too.
	d = dpDot()
	d.Schedule.Task = "Post to https://hooks.example/x with xoxb-1234567890-abcdefghij"
	d.Research.Topics = []string{"AKIAIOSFODNN7EXAMPLE rotation"}
	if _, err := Export(d, nil, ExportOptions{}); !errors.As(err, &se) || strings.Join(se.Where, ",") != "research topics,schedule task" {
		t.Fatalf("proactive text secrets: %v", err)
	}
}

func TestParseRefusesWhatItCantCheck(t *testing.T) {
	good, _ := Export(dpDot(), nil, ExportOptions{Now: dpNow})
	cases := map[string]string{
		"format":     strings.Replace(string(good), "format = 1", "format = 2", 1),
		"no pack":    strings.Replace(string(good), "[pack]", "[other]", 1),
		"future key": string(good) + "\n[dot.capabilities]\nshell = true\n",
		"preference": string(good) + "\n[[memory]]\nscope = \"dot\"\nkind = \"preference\"\ntext = \"x\"\npaths = []\n",
		"global":     string(good) + "\n[[memory]]\nscope = \"global\"\nkind = \"note\"\ntext = \"x\"\npaths = []\n",
		"not toml":   "{",
	}
	for name, data := range cases {
		if _, err := Parse([]byte(data)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Parse(make([]byte, MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size: %v", err)
	}
}

func TestFetchHTTPSOnlyCappedAndReportsTheFinalURL(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("[pack]\nformat = 1\n"))
	}))
	defer plain.Close()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, srv.URL+"/new.dotpack.toml", http.StatusFound)
		case "/new.dotpack.toml":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("[pack]\nformat = 1\n"))
		case "/downgrade":
			http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
		case "/huge":
			w.Header().Set("Content-Type", "text/plain")
			w.Write(make([]byte, 50<<20))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, final, err := Fetch(ctx, srv.Client(), srv.URL+"/old", MaxBytes)
	if err != nil || final != srv.URL+"/new.dotpack.toml" || !strings.HasPrefix(string(data), "[pack]") {
		t.Fatalf("redirect: %q %q %v", data, final, err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), plain.URL+"/x", MaxBytes); !errors.Is(err, ErrNotHTTPS) {
		t.Fatalf("http URL: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/downgrade", MaxBytes); !errors.Is(err, ErrNotHTTPS) {
		t.Fatalf("redirect to http: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/huge", MaxBytes); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("50 MB: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/json", MaxBytes); !errors.Is(err, ErrContentType) {
		t.Fatalf("json: %v", err)
	}
	if _, _, err := Fetch(ctx, srv.Client(), srv.URL+"/missing", MaxBytes); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
}

func TestSelectMemoryFollowsS9(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	const pid = "g-0123456789abcdef"
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Model convention.", Source: memory.SourceModel},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindDismissed, Text: "My own rule.", Source: memory.SourceUser},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindFinding, Text: "Archived finding.", Source: memory.SourceOutcome, Status: memory.StatusArchived},
		{Scope: memory.ScopeDot, Dot: "other", Kind: memory.KindNote, Text: "Another dot.", Source: memory.SourceModel},
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Project decision.", Source: memory.SourceModel},
		{Scope: memory.ScopeGlobal, Dot: "security", Kind: memory.KindPreference, Text: "British spelling.", Source: memory.SourceUser},
	} {
		if _, err := st.Add(pid, e); err != nil {
			t.Fatal(err)
		}
	}
	texts := func(es []memory.Entry) string {
		var out []string
		for _, e := range es {
			out = append(out, e.Text)
		}
		return strings.Join(out, "|")
	}
	if es, _ := SelectMemory(st, pid, "security", "", true); es != nil {
		t.Fatal("no --with-memory, no memory")
	}
	if es, _ := SelectMemory(st, pid, "security", WithDot, false); texts(es) != "Model convention." {
		t.Fatalf("dot: %s", texts(es))
	}
	if es, _ := SelectMemory(st, pid, "security", WithDot, true); texts(es) != "Model convention.|My own rule." {
		t.Fatalf("dot + user: %s", texts(es))
	}
	if es, _ := SelectMemory(st, pid, "security", WithDotProject, false); texts(es) != "Project decision.|Model convention." {
		t.Fatalf("dot+project: %s", texts(es))
	}
	if _, err := SelectMemory(st, "", "security", WithDot, false); err == nil {
		t.Fatal("no project")
	}
	if _, err := SelectMemory(st, pid, "security", "everything", false); err == nil {
		t.Fatal("bad choice")
	}
}
```

`TestExportParseRoundTrip`'s `reflect.DeepEqual` passes with the Phase 4–8 fields as they are (`Research.Topics` and `Schedule.Projects` default to empty, non-nil slices).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/dotpack`
Expected: FAIL to build: `undefined: Export`, `undefined: Parse`, `undefined: Fetch`.

- [ ] **Step 3: Write export, parse and prepare**

Create `internal/dotpack/dotpack.go`:

```go
// Package dotpack exports a dot (and optionally some of its memory) as one
// .dotpack.toml file and reads such files back, from disk or an https URL.
// Packs never carry credentials or history. Everything in an imported pack is
// someone else's work: Prepare clamps every risky capability (dots.ClampDot)
// and memory lands through memory.Store.ImportPack.
package dotpack

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
)

// Format is the pack format this version writes and reads.
const Format = 1

// MaxBytes caps a pack file or download.
const MaxBytes = 256 << 10

// Ext is the conventional file extension.
const Ext = ".dotpack.toml"

var (
	ErrFormat         = errors.New("not a dot pack this herdr-dots can read")
	ErrTooLarge       = fmt.Errorf("dot pack is larger than %d KB", MaxBytes>>10)
	ErrNotHTTPS       = errors.New("dot packs are fetched over https:// only")
	ErrContentType    = errors.New("the server did not send text")
	ErrSecretInExport = errors.New("the export looks like it contains a secret")
)

// SecretError names what looked like a secret ("purpose", "memory 2", …),
// never the text.
type SecretError struct{ Where []string }

func (e *SecretError) Error() string {
	return ErrSecretInExport.Error() + " (" + strings.Join(e.Where, ", ") + ")"
}

func (e *SecretError) Unwrap() error { return ErrSecretInExport }

// Meta is the [pack] table.
type Meta struct {
	Format     int       `toml:"format"`
	ExportedAt time.Time `toml:"exported_at"`
	Version    string    `toml:"herdr_dots_version"`
	Note       string    `toml:"note,omitempty"`
}

// MemoryItem is one [[memory]] block: only what travels.
type MemoryItem struct {
	Scope string   `toml:"scope"`
	Kind  string   `toml:"kind"`
	Text  string   `toml:"text"`
	Paths []string `toml:"paths"`
}

// Pack is a parsed pack. Dot is exactly as written (not clamped or
// validated); Memory entries carry Scope, Kind, Text and Paths only.
type Pack struct {
	Meta   Meta
	Dot    dots.Dot
	Memory []memory.Entry
}

type fileDoc struct {
	Pack   Meta         `toml:"pack"`
	Dot    dots.Dot     `toml:"dot"`
	Memory []MemoryItem `toml:"memory"`
}

// ExportOptions tune Export.
type ExportOptions struct {
	Note    string
	Version string    // herdr_dots_version
	Now     time.Time // zero = time.Now
	Force   bool      // export even if something looks like a secret
}

const header = "# herdr-dots dot pack. Import it with: herdr-dots dots import <file>\n" +
	"# Imported dots start with inject, write, MCP, research, web, schedules, commit\n" +
	"# triggers and custom endpoints turned off until you keep each one.\n"

// Export renders d and mem as a pack. It refuses (unless Force) when the
// dot or any entry looks like it holds a secret; mem should already be the
// entries the developer chose (SelectMemory).
func Export(d dots.Dot, mem []memory.Entry, o ExportOptions) ([]byte, error) {
	var where []string
	scan := func(label, text string) {
		if found, _ := memory.ScanSecrets(text); found {
			where = append(where, label)
		}
	}
	scan("name", d.Name)
	scan("purpose", d.Purpose)
	scan("backend", strings.Join(d.Backend.Args, " ")+" "+d.Backend.BaseURL)
	scan("research topics", strings.Join(d.Research.Topics, " ")) // Phase 8 free text travels too
	scan("schedule task", d.Schedule.Task)
	items := make([]MemoryItem, 0, len(mem))
	for i, e := range mem {
		scan(fmt.Sprintf("memory %d", i+1), e.Text+" "+strings.Join(e.Paths, " "))
		paths := e.Paths
		if paths == nil {
			paths = []string{}
		}
		items = append(items, MemoryItem{Scope: string(e.Scope), Kind: string(e.Kind), Text: e.Text, Paths: paths})
	}
	if len(where) > 0 && !o.Force {
		return nil, &SecretError{Where: where}
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(struct {
		Pack Meta `toml:"pack"`
	}{Meta{Format: Format, ExportedAt: now.UTC().Truncate(time.Second), Version: o.Version, Note: o.Note}}); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	d.Slug, d.Origin, d.SourcePath = "", "", ""
	if d.Backend.Args == nil {
		d.Backend.Args = []string{}
	}
	if d.Trigger.FileGlobs == nil {
		d.Trigger.FileGlobs = []string{}
	}
	purpose := d.Purpose
	d.Purpose = ""
	var dotBuf bytes.Buffer
	denc := toml.NewEncoder(&dotBuf)
	denc.Indent = ""
	if err := denc.Encode(struct {
		Dot dots.Dot `toml:"dot"`
	}{d}); err != nil {
		return nil, err
	}
	buf.WriteString(insertBeforeSubtable(dotBuf.String(), dots.PurposeBlock(purpose)))
	if len(items) > 0 {
		buf.WriteString("\n")
		menc := toml.NewEncoder(&buf)
		menc.Indent = ""
		if err := menc.Encode(struct {
			Memory []MemoryItem `toml:"memory"`
		}{items}); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// insertBeforeSubtable puts block after [dot]'s own keys: before the first
// "[dot." header the encoder wrote (no string value of its output spans
// lines, so no other line starts with "[").
func insertBeforeSubtable(doc, block string) string {
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[dot.") {
			return strings.Join(append(append(slices.Clone(lines[:i]), block), lines[i:]...), "\n")
		}
	}
	return strings.TrimRight(doc, "\n") + "\n" + block + "\n"
}

// Parse reads a pack. It refuses oversized files, other formats, keys this
// version doesn't know (a capability it doesn't know can't be clamped) and
// memory it wouldn't import. The dot is returned as written.
func Parse(data []byte) (Pack, error) {
	if len(data) > MaxBytes {
		return Pack{}, ErrTooLarge
	}
	doc := fileDoc{Dot: dots.New("")}
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return Pack{}, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return Pack{}, fmt.Errorf("%w: unknown keys %s (exported by a newer herdr-dots?)", ErrFormat, strings.Join(keys, ", "))
	}
	if doc.Pack.Format != Format {
		return Pack{}, fmt.Errorf("%w: format %d (this version reads format %d)", ErrFormat, doc.Pack.Format, Format)
	}
	var errs []error
	p := Pack{Meta: doc.Pack, Dot: doc.Dot}
	p.Dot.Slug, p.Dot.Origin, p.Dot.SourcePath = dots.Slugify(p.Dot.Name), "", ""
	for i, it := range doc.Memory {
		e := memory.Entry{Scope: memory.Scope(strings.TrimSpace(it.Scope)), Kind: memory.Kind(strings.TrimSpace(it.Kind)),
			Text: strings.TrimSpace(it.Text), Paths: it.Paths}
		switch {
		case e.Scope != memory.ScopeDot && e.Scope != memory.ScopeProject:
			errs = append(errs, fmt.Errorf("memory %d: scope must be dot or project", i+1))
		case !slices.Contains(memory.ImportKinds, e.Kind):
			errs = append(errs, fmt.Errorf("memory %d: kind must be one of %v", i+1, memory.ImportKinds))
		case e.Text == "":
			errs = append(errs, fmt.Errorf("memory %d: text is empty", i+1))
		case utf8.RuneCountInString(e.Text) > 500:
			errs = append(errs, fmt.Errorf("memory %d: text is over 500 characters", i+1))
		default:
			p.Memory = append(p.Memory, e)
		}
	}
	if len(errs) > 0 {
		return Pack{}, fmt.Errorf("%w: %w", ErrFormat, errors.Join(errs...))
	}
	return p, nil
}

// Prepare turns a parsed pack's dot into the dot to save: every risky
// capability clamped except keep, origin "import", slug from the name, and
// validated. It returns the clamps it applied.
func Prepare(p Pack, keep []string) (dots.Dot, []dots.Clamp, error) {
	if err := dots.CheckCapabilities(keep); err != nil {
		return dots.Dot{}, nil, err
	}
	d, cs := dots.ClampDot(p.Dot, keep)
	d.Origin, d.Slug, d.SourcePath = dots.OriginImport, dots.Slugify(d.Name), ""
	if err := d.Validate(); err != nil {
		return dots.Dot{}, cs, err
	}
	return d, cs, nil
}
```

- [ ] **Step 4: Write fetch and memory selection**

Create `internal/dotpack/fetch.go`:

```go
package dotpack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"herdr-dots/internal/memory"
)

// Fetch downloads a pack over https (every redirect must stay https, at most
// five), refuses anything but a 200 with a text (or application/toml) body,
// and reads at most maxBytes. It returns the body and the final URL after
// redirects, which the preview shows. There are no automatic updates.
func Fetch(ctx context.Context, c *http.Client, rawURL string, maxBytes int64) ([]byte, string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, "", ErrNotHTTPS
	}
	if c == nil {
		c = http.DefaultClient
	}
	cl := *c
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return ErrNotHTTPS
		}
		if len(via) >= 5 {
			return errors.New("more than 5 redirects")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := cl.Do(req)
	if err != nil {
		if errors.Is(err, ErrNotHTTPS) {
			return nil, "", fmt.Errorf("%w: a redirect left https", ErrNotHTTPS)
		}
		return nil, "", err
	}
	defer resp.Body.Close()
	final := resp.Request.URL.Redacted() // a user:password in a redirect never reaches the preview
	if resp.StatusCode != http.StatusOK {
		return nil, final, fmt.Errorf("%s: HTTP %d", final, resp.StatusCode)
	}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(mt, "text/") && mt != "application/toml" {
		return nil, final, fmt.Errorf("%w (%q from %s)", ErrContentType, mt, final)
	}
	if resp.ContentLength > maxBytes {
		return nil, final, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, final, err
	}
	if int64(len(data)) > maxBytes {
		return nil, final, ErrTooLarge
	}
	return data, final, nil
}

// Memory choices for an export (spec S9).
const (
	WithDot        = "dot"         // the dot's own memory for the project
	WithDotProject = "dot+project" // plus the project's shared memory
)

// SelectMemory picks what an export may carry: active entries of the
// ImportKinds only, the dot's scope (and with dot+project the project's),
// never global memory, and the developer's own entries only with
// includeUser. with "" means no memory.
func SelectMemory(st *memory.Store, project, slug, with string, includeUser bool) ([]memory.Entry, error) {
	if with == "" {
		return nil, nil
	}
	if with != WithDot && with != WithDotProject {
		return nil, fmt.Errorf("--with-memory %q: want %s or %s", with, WithDot, WithDotProject)
	}
	if st == nil {
		return nil, errors.New("memory is turned off (memory_enabled = false)")
	}
	if project == "" {
		return nil, errors.New("no project: run this in the repository or pass --project")
	}
	active := []memory.Status{memory.StatusActive}
	es, err := st.List(project, memory.Filter{Scope: memory.ScopeDot, Dot: slug, Statuses: active, Kinds: memory.ImportKinds})
	if err != nil {
		return nil, err
	}
	if with == WithDotProject {
		pe, err := st.List(project, memory.Filter{Scope: memory.ScopeProject, Statuses: active, Kinds: memory.ImportKinds})
		if err != nil {
			return nil, err
		}
		es = append(pe, es...)
	}
	out := es[:0]
	for _, e := range es {
		if e.Source == memory.SourceUser && !includeUser {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/dotpack`
Expected: `ok  	herdr-dots/internal/dotpack`.

- [ ] **Step 6: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 7: Commit**

```bash
git add internal/dotpack
git commit -m "feat(dotpack): dot pack export, strict parse, clamped prepare and https-only fetch (Phase 9)"
```

---

### Task 7: Engine history hooks

Every run of every kind writes one history record and every card resolution writes one (spec success criterion 1; Plan decisions 7–9); imported memory never suppresses (Plan decision 6b).

**Depends on:** Task 1 (`history`), Task 4 (`ImportPack`, used by a test). Wave 2 (runs beside Tasks 5, 6 and 8).

**Files:**
- Create: `internal/engine/history.go`, `internal/engine/history_test.go`
- Modify: `internal/engine/engine.go` (`Deps`, `runOpts`, `runDot`, `route`), `internal/engine/deliver.go` (`deliver`, `dropPane`), `internal/engine/memory.go` (`spawnOutcome`, `suppressedBy`)
- Modify: Phase 7's `internal/engine/draft.go` (`runDraft`) and Phase 8's `internal/engine/background.go` (`runBackground`)

**Interfaces:**
- Consumes: Task 1 `history.Store`, `history.Record`, kinds and resolutions, `history.CardKey`, `history.ValidSlug`; Phase 2 `memory.OutcomeSent`, `OutcomeEdited`, `OutcomeDismissed`, `memory.SourceImport`; Phase 3 `runOpts.noForce`, `dots.ReservedSlug`, `queue.KindCouncil`, `Card.Parts`; Phase 4 `backend.Result.CostUSD`, `CostKnown`, `deltaOf`, `redactErr`; Phase 5 `runOpts.ask`, `state.OutcomeAsked`; Phase 7 `runDraft`, `patchSent`, `spawnOutcomeOf(c, memory.OutcomeApplied | memory.OutcomeDiscarded, secret)`; Phase 8 `runBackground(j bgJob)` with `j.kind` in `state.RunResearch | RunScheduled | RunCommit`, `bgFeed`, `queue.KindDigest`; test rigs `newDraftRig`/`draftOpts.deps`/`writeDot`/`fixJWT`, `newProRig`/`proOpts.deps`/`scheduledDot`/`proResult`.
- Produces: `engine.Deps.History *history.Store`, `engine.Deps.SessionKey string`; `runOpts.trace *runTrace`; `(runOpts) historyKind() string`; `(*Engine) trace(paneID string, d dots.Dot, kind, project string) *runTrace` with `mark(turn int, outcome, detail string)`, `markVerdict(turn int, outcome string, v backend.Verdict)`, `usage(res backend.Result, took time.Duration)`, `card(c queue.Card)`, `done()` (all nil-safe); `verdictOutcome(v backend.Verdict) string`; `resolutionFor(kind string, edited bool) string`; `(*Engine) spawnResolve(c queue.Card, resolution string)` (council and digest cards resolve for each contributing dot).

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/history_test.go`:

```go
package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// histRig is a rig whose engine keeps history in a temp dir.
func histRig(t *testing.T, ds ...dots.Dot) (*rig, *history.Store) {
	t.Helper()
	hs := history.Open(filepath.Join(t.TempDir(), "history"))
	r := newRigWith(t, func(d *Deps) { d.History, d.SessionKey = hs, "sess1" }, ds...)
	return r, hs
}

func histRuns(hs *history.Store, slug string) []history.Record {
	rs, _ := hs.Query(slug, time.Time{}, 0)
	return rs
}

func TestReviewRunAndSendAreRecorded(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "a card", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	eventually(t, "run recorded", func() bool { return len(histRuns(hs, "security")) == 1 })
	run := histRuns(hs, "security")[0]
	if run.Kind != history.KindReview || run.Outcome != state.OutcomeFlagged || run.Severity != "critical" ||
		run.Card != history.CardKey("sess1", c.ID) || run.Pane != pane || run.In != 10 || run.Out != 5 ||
		run.Excerpt != "JWT in localStorage" || !run.CostUnknown {
		t.Fatalf("run %+v", run)
	}
	if err := r.e.SendCards([]string{c.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "sent recorded", func() bool { return histRuns(hs, "security")[0].Resolution == history.ResSent })
}

func TestPassSummonAndErrorRunsAreRecorded(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "rename a var"})
	eventually(t, "pass recorded", func() bool { rs := histRuns(hs, "security"); return len(rs) == 1 && rs[0].Outcome == state.OutcomePass })
	r.be.set(critical)
	if err := r.e.Summon(context.Background(), pane, "security", "anything?"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "summon recorded", func() bool { rs := histRuns(hs, "security"); return len(rs) == 2 && rs[0].Kind == history.KindSummon })
	r.be.fail(errors.New("claude exited 1"))
	r.turnEnds(t, transcript.Turn{User: "next"})
	eventually(t, "error recorded", func() bool {
		rs := histRuns(hs, "security")
		return len(rs) == 3 && rs[0].Outcome == state.OutcomeError && strings.Contains(rs[0].Excerpt, "exited 1")
	})
}

func TestDismissEditAndInjectResolutions(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue), dot("Fast", dots.ModeInject))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security", "fast")
	eventually(t, "security card", func() bool { return len(r.q.Pending(pane)) == 1 && r.q.Pending(pane)[0].Dot == "security" })
	eventually(t, "inject resolution", func() bool {
		rs := histRuns(hs, "fast")
		return len(rs) == 1 && rs[0].Resolution == history.ResInjected
	})
	id := r.q.Pending(pane)[0].ID
	if err := r.e.Dismiss(id); err != nil {
		t.Fatal(err)
	}
	eventually(t, "dismissed", func() bool { return histRuns(hs, "security")[0].Resolution == history.ResDismissed })

	r.turnEnds(t, transcript.Turn{User: "more"})
	eventually(t, "second security card", func() bool {
		for _, c := range r.q.Pending(pane) {
			if c.Dot == "security" {
				id = c.ID
				return true
			}
		}
		return false
	})
	if err := r.e.Edit(id, "edited text"); err != nil {
		t.Fatal(err)
	}
	if err := r.e.SendCards([]string{id}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "edited", func() bool { return histRuns(hs, "security")[0].Resolution == history.ResEdited })
	st, _ := hs.Stats("security", time.Now().Add(time.Minute), time.Hour)
	if st.Resolved != 2 || st.Accepted != 1 || st.Dismissed != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestClosedPaneRecordsOrphanedCards(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.srv.RemovePane(pane)
	eventually(t, "orphaned", func() bool {
		rs := histRuns(hs, "security")
		return len(rs) == 1 && rs[0].Resolution == history.ResOrphaned
	})
}

func TestNoHistoryStoreKeepsEngineBehavior(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "a card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if err := r.e.Dismiss(r.q.Pending(pane)[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryKindForEveryRunFlavor(t *testing.T) {
	for want, o := range map[string]runOpts{
		history.KindReview:  {},
		history.KindSummon:  {summon: true},
		history.KindMemory:  {memoryPass: true},
		history.KindCouncil: {summon: true, noForce: true},
		history.KindAsk:     {summon: true, ask: true},
	} {
		if got := o.historyKind(); got != want {
			t.Errorf("%+v: got %s, want %s", o, got, want)
		}
	}
}

// Phase 9 provenance rule: an imported dismissed entry, even once the
// developer approved it, guides the prompt but never silences the dot; only
// outcome and user entries suppress repeats.
func TestImportedDismissedMemoryNeverSuppresses(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	if _, err := r.mem.ImportPack(testProject, "security", []memory.Entry{
		{Scope: memory.ScopeDot, Kind: memory.KindDismissed, Text: jwtFirst.Message},
	}); err != nil {
		t.Fatal(err)
	}
	es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"})
	if len(es) != 1 || es[0].Status != memory.StatusProposed {
		t.Fatalf("an imported entry waits for approval: %+v", es)
	}
	if _, err := r.mem.Update(testProject, es[0].Ref(), func(x *memory.Entry) error { x.Status = memory.StatusActive; return nil }); err != nil {
		t.Fatal(err)
	}
	r.be.set(backend.Result{Verdict: jwtAgain})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "a card despite the imported rule", func() bool { return len(r.q.Pending(pane)) == 1 })
	if !strings.Contains(r.be.last().Memory, "(imported)") {
		t.Fatalf("the imported rule is in the prompt, labelled:\n%s", r.be.last().Memory)
	}
}

// Phase 8's background run kinds are history kinds verbatim.
func TestBackgroundKindsAreHistoryKinds(t *testing.T) {
	if state.RunResearch != history.KindResearch || state.RunScheduled != history.KindScheduled || state.RunCommit != history.KindCommit {
		t.Fatal("state.Run* and history.Kind* must match")
	}
}

// Phase 7: a draft is one "draft" run linked to its patch card; discarding
// the patch and sending another are its resolutions.
func TestDraftRunAndPatchOutcomesAreRecorded(t *testing.T) {
	hs := history.Open(filepath.Join(t.TempDir(), "history"))
	r := newDraftRig(t, draftOpts{deps: func(d *Deps) { d.History, d.SessionKey = hs, "sess1" }}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	var cards []string
	for i := 0; i < 2; i++ {
		if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
			t.Fatal(err)
		}
		c := r.patchCard(t)
		r.draftsDone(t)
		cards = append(cards, c.ID)
		if i == 0 {
			if err := r.e.DiscardPatch(c.ID); err != nil {
				t.Fatal(err)
			}
		} else if err := r.e.SendCards([]string{c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "two draft runs with their resolutions", func() bool {
		var drafts []history.Record
		for _, rec := range histRuns(hs, "security") {
			if rec.Kind == history.KindDraft {
				drafts = append(drafts, rec)
			}
		}
		return len(drafts) == 2 && drafts[1].Card == history.CardKey("sess1", cards[0]) && drafts[1].Resolution == history.ResDiscarded &&
			drafts[0].Card == history.CardKey("sess1", cards[1]) && drafts[0].Resolution == history.ResSent &&
			drafts[0].Outcome == state.OutcomeDrafted && !strings.Contains(drafts[0].Excerpt, "diff --git")
	})
}

// Phase 8: a scheduled run is a "scheduled" run in the history.
func TestScheduledRunIsRecorded(t *testing.T) {
	hs := history.Open(filepath.Join(t.TempDir(), "history"))
	r := newProRig(t, proOpts{cwd: t.TempDir(), deps: func(d *Deps) { d.History, d.SessionKey = hs, "sess1" }},
		scheduledDot("Security", "0 11 * * *"))
	r.attach(t, "security")
	eventually(t, "the next slot is planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	r.be.set(proResult("warn", "Commit 3f2a skips the CSRF check."))
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "the scheduled run recorded", func() bool {
		for _, rec := range histRuns(hs, "security") {
			if rec.Kind == history.KindScheduled && rec.Outcome == state.OutcomeFlagged && rec.Severity == "warn" && rec.Project == testProject {
				return true
			}
		}
		return false
	})
}

// A council card or an away digest speaks for several dots: each dot that
// contributed a part gets the resolution.
func TestMultiDotCardsResolveForEachContributor(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.e.spawnResolve(queue.Card{ID: "9", Kind: queue.KindDigest, Dot: "away", Parts: []queue.CardPart{
		{Dot: "security", Severity: "warn"}, {Dot: "frontend", Severity: "info"}, {Dot: "docs", Failed: true}, {Dot: "perf"},
	}}, history.ResSent)
	eventually(t, "two resolutions", func() bool {
		slugs, _ := hs.Slugs()
		return len(slugs) == 2
	})
	for _, slug := range []string{"security", "frontend"} {
		if st, _ := hs.Stats(slug, time.Now().Add(time.Minute), time.Hour); st.Accepted != 1 {
			t.Fatalf("%s: %+v", slug, st)
		}
	}
}

func TestResolutionForEveryOutcome(t *testing.T) {
	for _, c := range []struct {
		kind   string
		edited bool
		want   string
	}{
		{memory.OutcomeSent, false, history.ResSent},
		{memory.OutcomeSent, true, history.ResEdited},
		{memory.OutcomeEdited, false, history.ResEdited},
		{memory.OutcomeDismissed, false, history.ResDismissed},
		{"applied", false, history.ResApplied},
		{"discarded", false, history.ResDiscarded},
		{"something else", false, ""},
	} {
		if got := resolutionFor(c.kind, c.edited); got != c.want {
			t.Errorf("%s/%v: got %q, want %q", c.kind, c.edited, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/engine -run 'History|Recorded|Resolutions|Orphaned|ResolutionFor|ImportedDismissed'`
Expected: FAIL to build: `unknown field History in struct literal of type Deps`, `o.historyKind undefined`, `undefined: resolutionFor`.

- [ ] **Step 3: Write the trace and resolution helpers**

Create `internal/engine/history.go`:

```go
package engine

import (
	"slices"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

// historyKind is the run kind of a runDot run. Runs that don't go through
// runDot (Phase 7 drafts, Phase 8 background runs) pass their kind to trace.
func (o runOpts) historyKind() string {
	switch {
	case o.memoryPass:
		return history.KindMemory
	case o.ask: // Phase 5 MCP ask_dot
		return history.KindAsk
	case o.noForce: // Phase 3 council member
		return history.KindCouncil
	case o.summon:
		return history.KindSummon
	}
	return history.KindReview
}

// runTrace collects one run's history record. The run marks it next to each
// of its feed lines and appends it once when it ends; a run that never
// reached its backend (cancelled while waiting for a slot) leaves no record.
type runTrace struct {
	e      *Engine
	rec    history.Record
	marked bool
}

func (e *Engine) trace(paneID string, d dots.Dot, kind, project string) *runTrace {
	return &runTrace{e: e, rec: history.Record{T: history.TypeRun, Dot: d.Slug, Pane: paneID, Kind: kind, Project: project}}
}

// mark records how the run ended; the last mark wins.
func (t *runTrace) mark(turn int, outcome, detail string) {
	if t == nil {
		return
	}
	t.rec.Turn, t.rec.Outcome, t.rec.Excerpt, t.marked = turn, outcome, detail, true
}

// markVerdict is mark for a verdict: the excerpt is the dot's message.
func (t *runTrace) markVerdict(turn int, outcome string, v backend.Verdict) {
	if t == nil {
		return
	}
	t.mark(turn, outcome, v.Message)
	t.rec.Severity = v.Severity
}

// usage records the backend's tokens, cost and latency. Cost provenance
// follows Phase 4's deltaOf: a run that reported no usage at all cost nothing
// known; one with tokens but no price is unpriced.
func (t *runTrace) usage(res backend.Result, took time.Duration) {
	if t == nil {
		return
	}
	d := deltaOf(res)
	t.rec.In, t.rec.Out, t.rec.Ms = res.InputTokens, res.OutputTokens, took.Milliseconds()
	t.rec.CostUSD, t.rec.CostUnknown = 0, !d.CostKnown
	if d.CostKnown {
		t.rec.CostUSD = res.CostUSD
	}
}

// verdictOutcome is the history outcome of a verdict that ends a run with no
// feed outcome of its own (an MCP ask's feed line says "asked").
func verdictOutcome(v backend.Verdict) string {
	if v.Relevant {
		return state.OutcomeFlagged
	}
	return state.OutcomePass
}

// card links the run to the card it produced.
func (t *runTrace) card(c queue.Card) {
	if t == nil {
		return
	}
	t.rec.Card, t.rec.Severity = history.CardKey(t.e.d.SessionKey, c.ID), c.Severity
}

// done appends the record. It runs on the run's own goroutine, which holds
// no engine lock (history I/O never happens under paneLock, mu or dmu).
func (t *runTrace) done() {
	if t == nil || !t.marked || t.e.d.History == nil {
		return
	}
	if err := t.e.d.History.Append(t.rec); err != nil {
		t.e.d.Logf("history %s: %v", t.rec.Dot, err)
	}
}

// resolutionFor maps a memory outcome kind to a history resolution.
func resolutionFor(kind string, edited bool) string {
	switch kind {
	case memory.OutcomeSent:
		if edited {
			return history.ResEdited
		}
		return history.ResSent
	case memory.OutcomeEdited:
		return history.ResEdited
	case memory.OutcomeDismissed:
		return history.ResDismissed
	case "applied": // Phase 7 patch outcomes (memory.Outcome kinds)
		return history.ResApplied
	case "discarded":
		return history.ResDiscarded
	}
	return ""
}

// spawnResolve records what happened to a card, off the caller's locks
// (Dismiss and deliver hold dmu). A council card (Phase 3) or an away digest
// (Phase 8, Dot "away") speaks for several dots: its resolution is recorded
// for each dot that contributed a part, as memory records its outcomes.
func (e *Engine) spawnResolve(c queue.Card, resolution string) {
	if e.d.History == nil || resolution == "" {
		return
	}
	slugs := []string{c.Dot}
	if c.Kind == queue.KindCouncil || c.Kind == queue.KindDigest {
		slugs = nil
		for _, p := range c.Parts {
			if !p.Failed && p.Severity != "" && !slices.Contains(slugs, p.Dot) {
				slugs = append(slugs, p.Dot)
			}
		}
	}
	var recs []history.Record
	for _, slug := range slugs {
		if slug == "" || slug == dots.ReservedSlug || !history.ValidSlug(slug) {
			continue
		}
		recs = append(recs, history.Record{T: history.TypeResolve, Dot: slug, Pane: c.PaneID, Project: c.Project,
			Card: history.CardKey(e.d.SessionKey, c.ID), Resolution: resolution})
	}
	if len(recs) == 0 {
		return
	}
	e.spawn(func() {
		for _, r := range recs {
			if err := e.d.History.Append(r); err != nil {
				e.d.Logf("history %s: %v", r.Dot, err)
			}
		}
	})
}
```

- [ ] **Step 4: Wire the trace into `runDot` and `route`**

In `internal/engine/engine.go`, add `"herdr-dots/internal/history"` to the imports. Find the memory fields of `Deps` (Phases 7 and 8 added fields below them; keep those):

```go
	Memory         *memory.Store                            // nil = memory off (v1 behavior)
	ResolveProject func(cwd string) (memory.Project, error) // nil = memory.ResolveProject with git
```

Replace with:

```go
	Memory         *memory.Store                            // nil = memory off (v1 behavior)
	ResolveProject func(cwd string) (memory.Project, error) // nil = memory.ResolveProject with git

	History    *history.Store // Phase 9: per-dot run and card history; nil = none
	SessionKey string         // Phase 9: this Herdr session's key; card ids are unique only within it
```

Find the last field of `type runOpts struct`:

```go
	seenTo     int    // memory pass: MemorySeenTurn once it succeeds
}
```

Replace with:

```go
	seenTo     int    // memory pass: MemorySeenTurn once it succeeds

	trace *runTrace // Phase 9: set by runDot; route links the card to the run
}
```

In `runDot`, find:

```go
	att, _ := e.d.Attachments.Get(paneID)
	turn := 0
	if len(turns) > 0 {
		turn = turns[len(turns)-1].Index
	}
	be, err := e.d.Backend(d.Backend.Kind)
	if err != nil {
		err = redactErr(err)
		sinkErr = err
		e.runFailed(k, d, turn, err)
		return
	}
```

Replace with:

```go
	att, _ := e.d.Attachments.Get(paneID)
	tr := e.trace(paneID, d, o.historyKind(), att.ProjectID)
	o.trace = tr
	defer tr.done()
	turn := 0
	if len(turns) > 0 {
		turn = turns[len(turns)-1].Index
	}
	be, err := e.d.Backend(d.Backend.Kind)
	if err != nil {
		err = redactErr(err)
		sinkErr = err
		tr.mark(turn, state.OutcomeError, err.Error())
		e.runFailed(k, d, turn, err)
		return
	}
```

Find:

```go
	res, err := be.Run(ctx, req)
	e.charge(paneID, d.Slug, deltaOf(res))
```

Replace with:

```go
	started := time.Now()
	res, err := be.Run(ctx, req)
	tr.usage(res, time.Since(started))
	e.charge(paneID, d.Slug, deltaOf(res))
```

Find (the end of the run-error branch; a cancelled council member or engine returns earlier in that branch without a mark, so it leaves no record, as it leaves no feed line):

```go
		e.runFailed(k, d, turn, err)
		return
	}
	sinkRes, sinkErr = res, nil
```

Replace with:

```go
		tr.mark(turn, state.OutcomeError, err.Error())
		e.runFailed(k, d, turn, err)
		return
	}
	sinkRes, sinkErr = res, nil
```

Find:

```go
		e.clearError(k) // a pass sets no status of its own; drop a stale failure
		if e.applyMemoryOps(paneID, d, turn, mem, res.Remember, att, true) == 0 {
			e.feed(paneID, d.Slug, turn, state.OutcomePass, "memory pass: nothing new")
		}
		return
```

Replace with:

```go
		e.clearError(k) // a pass sets no status of its own; drop a stale failure
		if n := e.applyMemoryOps(paneID, d, turn, mem, res.Remember, att, true); n == 0 {
			e.feed(paneID, d.Slug, turn, state.OutcomePass, "memory pass: nothing new")
			tr.mark(turn, state.OutcomePass, "memory pass: nothing new")
		} else {
			tr.mark(turn, state.OutcomeLearned, fmt.Sprintf("learned %d", n))
		}
		return
```

Find (Phase 5's MCP ask; its feed outcome is `asked`, and history records the verdict so the stats count it):

```go
		e.feed(paneID, d.Slug, turn, state.OutcomeAsked, askDetail(d, res))
```

Replace with:

```go
		e.feed(paneID, d.Slug, turn, state.OutcomeAsked, askDetail(d, res))
		tr.markVerdict(turn, verdictOutcome(res.Verdict), res.Verdict)
```

Find:

```go
			e.feed(paneID, d.Slug, turn, state.OutcomeSuppressed, "similar to "+ref)
```

Replace with:

```go
			e.feed(paneID, d.Slug, turn, state.OutcomeSuppressed, "similar to "+ref)
			tr.markVerdict(turn, state.OutcomeSuppressed, res.Verdict)
```

Find:

```go
		e.feed(paneID, d.Slug, turn, state.OutcomePass, costDetail(d, res))
```

Replace with:

```go
		e.feed(paneID, d.Slug, turn, state.OutcomePass, costDetail(d, res))
		tr.mark(turn, state.OutcomePass, "")
```

Find:

```go
	e.feed(paneID, d.Slug, turn, state.OutcomeFlagged, withCost(res.Verdict.Message, costDetail(d, res)))
```

Replace with (a council member returns right after this line, so its record is `flagged` with no card: the council turns the answers into one card):

```go
	e.feed(paneID, d.Slug, turn, state.OutcomeFlagged, withCost(res.Verdict.Message, costDetail(d, res)))
	tr.markVerdict(turn, state.OutcomeFlagged, res.Verdict)
```

Run `grep -n 'e.feed(\|e.runFailed(' internal/engine/engine.go` and check that every line inside `runDot` now has its mark: the rule is that every `e.feed(…)` that ends a run gets a `tr.mark` (or `tr.markVerdict`) right after it, and every `e.runFailed(…)` a `tr.mark(turn, state.OutcomeError, …)` right before it.

In `route`, find:

```go
	if _, err := e.d.Queue.Add(c); err != nil {
		e.d.Logf("queue add: %v", err)
		return
	}
```

Replace with:

```go
	added, err := e.d.Queue.Add(c)
	if err != nil {
		e.d.Logf("queue add: %v", err)
		return
	}
	o.trace.card(added)
```

- [ ] **Step 5: Record resolutions**

In `internal/engine/deliver.go`, add `"herdr-dots/internal/history"` to the imports. Find (in `deliver`):

```go
			if err := e.d.Queue.SetStatus(c.ID, queue.StatusSent); err == nil && c.Approved {
				if c.Kind == queue.KindPatch {
					e.patchSent(c) // marks the patch sent and records the outcome
				} else {
					e.spawnOutcome(c, memory.OutcomeSent)
				}
			}
```

Replace with (Phase 7's `patchSent` records its memory outcome itself, without `spawnOutcome`, so a sent patch gets its resolution here):

```go
			if err := e.d.Queue.SetStatus(c.ID, queue.StatusSent); err == nil {
				switch {
				case !c.Approved:
					e.spawnResolve(c, history.ResInjected) // Phase 9: delivered by inject mode, never approved
				case c.Kind == queue.KindPatch:
					e.patchSent(c) // marks the patch sent and records the outcome
					e.spawnResolve(c, history.ResSent)
				default:
					e.spawnOutcome(c, memory.OutcomeSent)
				}
			}
```

In `dropPane` (Phase 8 orphans the pane's cards under `e.mu`, in the same section as the attachment delete), find:

```go
	delete(e.idle, paneID) // collected research for a closed pane is dropped (idle.go)
	orphaned, oerr := e.d.Queue.OrphanPane(paneID)
```

Replace with:

```go
	delete(e.idle, paneID) // collected research for a closed pane is dropped (idle.go)
	waiting := e.d.Queue.Pending(paneID) // Phase 9: OrphanPane orphans exactly these
	orphaned, oerr := e.d.Queue.OrphanPane(paneID)
```

Find (after `e.mu.Unlock()`):

```go
	} else if orphaned > 0 {
		e.d.Logf("pane %s closed: %d card(s) orphaned", paneID, orphaned)
	}
```

Replace with:

```go
	} else if orphaned > 0 {
		e.d.Logf("pane %s closed: %d card(s) orphaned", paneID, orphaned)
		for _, c := range waiting {
			e.spawnResolve(c, history.ResOrphaned)
		}
	}
```

In `internal/engine/memory.go`, find (in `spawnOutcomeOf`, which `spawnOutcome` calls and which Phase 7's `ApplyPatch` and `DiscardPatch` call with `memory.OutcomeApplied` and `memory.OutcomeDiscarded`):

```go
func (e *Engine) spawnOutcomeOf(c queue.Card, kind string, secret bool) {
	e.mu.Lock()
	orig, edited := e.origBody[c.ID]
	delete(e.origBody, c.ID)
	e.mu.Unlock()
	if !e.memEnabled() {
		return
	}
```

Replace with:

```go
func (e *Engine) spawnOutcomeOf(c queue.Card, kind string, secret bool) {
	e.mu.Lock()
	orig, edited := e.origBody[c.ID]
	delete(e.origBody, c.ID)
	e.mu.Unlock()
	e.spawnResolve(c, resolutionFor(kind, edited)) // history is kept with memory on or off
	if !e.memEnabled() {
		return
	}
```

Find (in `suppressedBy`):

```go
	for _, en := range mr.cands {
		if en.Kind != memory.KindFinding && en.Kind != memory.KindDismissed {
			continue // research and question entries never suppress
		}
```

Replace with:

```go
	for _, en := range mr.cands {
		if en.Kind != memory.KindFinding && en.Kind != memory.KindDismissed {
			continue // research and question entries never suppress
		}
		if en.Source == memory.SourceImport {
			continue // a shared pack's entries guide the prompt but never silence a dot (Phase 9)
		}
```

- [ ] **Step 6: Trace Phase 7 drafts and Phase 8 background runs**

These run paths don't use `runDot`.

In `internal/engine/draft.go`, add `"herdr-dots/internal/history"` to the imports (`time` is already there). Find (in `runDraft`):

```go
	quiet := func() bool { return e.ctx.Err() != nil || e.draftAbandoned(id) }
	fail := func(err error) {
		if quiet() {
			// The engine is stopping or the pane is gone: nobody to tell.
			e.d.Logf("draft %s by %s on %s ended: %v", id, d.Slug, att.PaneID, err)
			return
		}
		// Run errors can carry provider text: keys redacted, no control
		// characters in the log, the feed or a toast (as runFailed does).
		msg := textutil.SanitizeModelText(redactErr(err).Error())
		e.d.Logf("draft %s by %s on %s: %s", id, d.Slug, att.PaneID, msg)
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "draft failed: "+msg)
```

Replace with (a draft whose pane closed ends quietly, with no feed line and no record):

```go
	quiet := func() bool { return e.ctx.Err() != nil || e.draftAbandoned(id) }
	tr := e.trace(att.PaneID, d, history.KindDraft, att.ProjectID) // Phase 9
	defer tr.done()
	fail := func(err error) {
		if quiet() {
			// The engine is stopping or the pane is gone: nobody to tell.
			e.d.Logf("draft %s by %s on %s ended: %v", id, d.Slug, att.PaneID, err)
			return
		}
		// Run errors can carry provider text: keys redacted, no control
		// characters in the log, the feed or a toast (as runFailed does).
		msg := textutil.SanitizeModelText(redactErr(err).Error())
		e.d.Logf("draft %s by %s on %s: %s", id, d.Slug, att.PaneID, msg)
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "draft failed: "+msg)
		tr.mark(turn, state.OutcomeError, "draft failed: "+msg)
```

Find:

```go
	res, err := be.Run(rctx, req)
```

Replace with:

```go
	started := time.Now()
	res, err := be.Run(rctx, req)
	tr.usage(res, time.Since(started))
```

Find:

```go
				e.feed(att.PaneID, d.Slug, turn, state.OutcomePass, "draft produced no changes")
```

Replace with:

```go
				e.feed(att.PaneID, d.Slug, turn, state.OutcomePass, "draft produced no changes")
				tr.mark(turn, state.OutcomePass, "draft produced no changes")
```

Find:

```go
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "patch rejected ("+reason+")")
```

Replace with:

```go
		e.feed(att.PaneID, d.Slug, turn, state.OutcomeError, "patch rejected ("+reason+")")
		tr.mark(turn, state.OutcomeError, "patch rejected ("+reason+")")
```

Find:

```go
	e.feed(att.PaneID, d.Slug, turn, state.OutcomeDrafted, p.Stat)
```

Replace with (the excerpt is the redacted summary on the card, never the raw model text):

```go
	e.feed(att.PaneID, d.Slug, turn, state.OutcomeDrafted, p.Stat)
	tr.mark(turn, state.OutcomeDrafted, summary)
	tr.card(c)
```

In Phase 8's `runBackground` (`internal/engine/background.go`; no new imports), the run starts once every gate has passed and a scheduled slot is claimed. Find:

```go
	if j.claim != nil && !j.claim() {
		return
	}
	be, err := e.d.Backend(j.dot.Backend.Kind)
	if err != nil {
		e.bgFailed(j, err)
		return
	}
```

Replace with:

```go
	if j.claim != nil && !j.claim() {
		return
	}
	tr := e.trace(j.paneID, j.dot, j.kind, j.project) // Phase 9: j.kind is research, scheduled or commit
	defer tr.done()
	be, err := e.d.Backend(j.dot.Backend.Kind)
	if err != nil {
		tr.mark(j.turn, state.OutcomeError, j.label+": "+redactErr(err).Error())
		e.bgFailed(j, err)
		return
	}
```

Find:

```go
	res, err := be.Run(ctx, req)
	e.chargeBackground(j, deltaOf(res)) // not to a pane that closed (or whose id was reused) meanwhile
```

Replace with:

```go
	started := time.Now()
	res, err := be.Run(ctx, req)
	tr.usage(res, time.Since(started))
	e.chargeBackground(j, deltaOf(res)) // not to a pane that closed (or whose id was reused) meanwhile
```

Find (after the "ended" `keepPane` check: a research or commit job whose pane closed mid-run is dropped there without a feed line, and so without a record):

```go
	if err != nil {
		e.bgFailed(j, err)
		return
	}
	e.touchMemory(mem)
```

Replace with:

```go
	if err != nil {
		tr.mark(j.turn, state.OutcomeError, j.label+": "+redactErr(err).Error())
		e.bgFailed(j, err)
		return
	}
	e.touchMemory(mem)
```

Find:

```go
	if !v.Relevant {
		e.bgFeed(&j, outcomeFor(j.kind), withCost(j.label+": nothing new", detail))
		return
	}
	if ref, ok := suppressedBy(mem, v.Message, j.turn, j.session); ok {
		e.bgFeed(&j, state.OutcomeSuppressed, "similar to "+ref)
		return
	}
	if !e.bgFeed(&j, outcomeFor(j.kind), withCost(j.label+": "+firstLine(v.Message), detail)) {
		return
	}
```

Replace with (a record only where the feed line was written: `bgFeed` drops the job when its pane is gone):

```go
	if !v.Relevant {
		if e.bgFeed(&j, outcomeFor(j.kind), withCost(j.label+": nothing new", detail)) {
			tr.mark(j.turn, state.OutcomePass, j.label+": nothing new")
		}
		return
	}
	if ref, ok := suppressedBy(mem, v.Message, j.turn, j.session); ok {
		if e.bgFeed(&j, state.OutcomeSuppressed, "similar to "+ref) {
			tr.markVerdict(j.turn, state.OutcomeSuppressed, v)
		}
		return
	}
	if !e.bgFeed(&j, outcomeFor(j.kind), withCost(j.label+": "+firstLine(v.Message), detail)) {
		return
	}
	tr.markVerdict(j.turn, state.OutcomeFlagged, v)
```

Background cards are not linked to their run (`deliverScheduled`, `deliverCommit` and the away digest add their own cards); their resolutions still count in the stats, and a digest's resolution is recorded for each dot that contributed a part.

Run `GOTOOLCHAIN=local go test ./internal/engine -run '^TestOnlyDraftsAskForWriteTools$'`. Expected: `ok` (the guard still finds exactly one `req.Pass = backend.PassResearch` in `background.go`, and `draft.go` is the only file that may ask for write tools).

- [ ] **Step 7: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/engine`
Expected: `ok  	herdr-dots/internal/engine` (all earlier engine tests still pass: a nil `History` writes nothing).

- [ ] **Step 8: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 9: Commit**

```bash
git add internal/engine
git commit -m "feat(engine): history record for every run and card resolution; imported memory never suppresses (Phase 9)"
```

---

### Task 8: Profile screen (`internal/ui/profile`)

The per-dot profile (spec §6.1, §6.2, §7): 7- and 30-day stats, memory summary, deterministic hints, recent history with the full message of a row.

**Depends on:** Task 1 (`history.Store.Profile`, `CostShare`, `Stats`), Task 2 (`dots.OriginUser`, `OriginProject`, `DisplaySlug`). Wave 2 (runs beside Tasks 5, 6 and 7).

**Files:**
- Create: `internal/ui/style/text.go`, `internal/ui/style/text_test.go`
- Create: `internal/ui/profile/model.go`, `internal/ui/profile/model_test.go`
- Modify: `internal/view/view.go` (one field, `LibraryDot.Origin`; Task 9 adds the rest)

**Interfaces:**
- Consumes: Task 1 `history.Store`, `history.Profile`, `history.Stats`, `history.Record`, kinds and resolutions; Phase 2 `memory.Store.Projects`, `List`, `memory.ValidSlug`, `memory.Store.SeeProject` (test); Task 2 `dots.OriginUser`, `OriginProject`, `DisplaySlug`; `view.LibraryDot` with the `Origin` field this task adds (Step 3); `style.Glyph`, `Title`, `Dim`, `Err`, `Selected`.
- Produces: `style.Clean(s string) string`, `style.Wrap(s string, w int) []string`; package `profile`: `const Rows = 50`; `type Deps struct{ History *history.Store; Memory *memory.Store; Dot view.LibraryDot; Purpose string; Now func() time.Time; CardBody func(cardKey string) (string, bool) }`; `New(d Deps) Model`; `(Model) Init() tea.Cmd`; `(Model) Update(tea.Msg) (Model, tea.Cmd)`; `(Model) View() string`; messages `BackMsg{}`, `ExportMsg{Slug string}`, `MemoryMsg{Slug, Name string}`; `type MemSummary struct{ Projects, Entries, Pinned int; ByKind map[memory.Kind]int }`; `Summarize(st *memory.Store, slug string) (MemSummary, error)`; `Hints(s history.Stats, share float64) []string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/style/text_test.go`:

```go
package style

import (
	"strings"
	"testing"
)

func TestWrapKeepsParagraphsAndNeverCuts(t *testing.T) {
	in := "first line \x1b[31mred\x1b[0m\n\nthird " + strings.Repeat("x", 25)
	got := Wrap(in, 10)
	want := []string{"first line", "[31mred[0m", "", "third", "xxxxxxxxxx", "xxxxxxxxxx", "xxxxx"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if Clean(" a\n\tb\x07 ") != "a b" {
		t.Fatalf("clean %q", Clean(" a\n\tb\x07 "))
	}
}
```

Create `internal/ui/profile/model_test.go`:

```go
package profile

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

var pNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

const pPID = "g-0123456789abcdef"

// pdrive runs cmd like the Bubble Tea runtime, batches included, and stops at
// a message for the parent.
func pdrive(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Msg) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case BackMsg, ExportMsg, MemoryMsg:
			return m, msg
		case nil:
		default:
			var next tea.Cmd
			m, next = m.Update(msg)
			queue = append(queue, next)
		}
	}
	return m, nil
}

func pkey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func ppress(t *testing.T, m Model, k string) (Model, tea.Msg) {
	t.Helper()
	m, cmd := m.Update(pkey(k))
	return pdrive(t, m, cmd)
}

func fixture(t *testing.T) (*history.Store, *memory.Store) {
	t.Helper()
	dir := t.TempDir()
	hs := history.Open(filepath.Join(dir, "history"))
	day := 24 * time.Hour
	recs := []history.Record{
		{T: history.TypeRun, At: pNow.Add(-20 * day), Dot: "security", Kind: history.KindReview, Outcome: state.OutcomeFlagged, Severity: "warn", Card: "s/1", Excerpt: "Old finding.", Ms: 9000, CostUSD: 0.5},
		{T: history.TypeResolve, At: pNow.Add(-20 * day), Dot: "security", Card: "s/1", Resolution: history.ResSent},
		{T: history.TypeRun, At: pNow.Add(-2 * day), Dot: "security", Kind: history.KindCouncil, Outcome: state.OutcomePass, Ms: 5000, CostUSD: 0.25},
		{T: history.TypeRun, At: pNow.Add(-time.Hour), Dot: "security", Kind: history.KindReview, Outcome: state.OutcomeFlagged, Severity: "critical", Card: "s/2", Excerpt: "CSRF on /admin is open.", Ms: 7000, CostUSD: 0.25},
		{T: history.TypeResolve, At: pNow.Add(-time.Hour), Dot: "security", Card: "s/2", Resolution: history.ResDismissed},
		{T: history.TypeRun, At: pNow.Add(-time.Hour), Dot: "frontend", Kind: history.KindReview, Outcome: state.OutcomePass, CostUSD: 3},
	}
	for _, r := range recs {
		if err := hs.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	st := memory.Open(filepath.Join(dir, "memory"), memory.DefaultLimits())
	st.SeeProject(memory.Project{ID: pPID, Name: "app", Root: "/app"})
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindFinding, Text: "Rate limit added.", Source: memory.SourceOutcome},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindDismissed, Text: "CSRF fine.", Source: memory.SourceUser, Pinned: true},
	} {
		if _, err := st.Add(pPID, e); err != nil {
			t.Fatal(err)
		}
	}
	return hs, st
}

func openProfile(t *testing.T, cardBody func(string) (string, bool)) Model {
	t.Helper()
	hs, st := fixture(t)
	m := New(Deps{History: hs, Memory: st, Now: func() time.Time { return pNow }, CardBody: cardBody, Purpose: "You review security.\nMore lines.",
		Dot: view.LibraryDot{Slug: "security", Name: "Security", Glyph: "◆", Color: "red", Backend: "claude-cli", Model: "sonnet", Mode: "queue", Origin: "template"}})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m, _ = pdrive(t, m, m.Init())
	return m
}

func TestProfileShowsStatsMemoryAndHistory(t *testing.T) {
	m := openProfile(t, nil)
	v := m.View()
	for _, want := range []string{
		"SECURITY · claude-cli/sonnet · queue", "origin: template", "Purpose: You review security.…",
		"LAST 30 DAYS", "runs 3 (review 2 · council 1)", "flagged 67%",
		"accepted 50% (1 / 2)", "cost $1.00", "median 7.0 s",
		"1 project · 2 entries (1 finding · 1 dismissed) · 1 pinned",
		"review", "critical", "dismissed", "CSRF on /admin is open.",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("profile missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "HINTS") {
		t.Fatalf("no hint applies to this fixture:\n%s", v)
	}
	m, _ = ppress(t, m, "tab")
	if v := m.View(); !strings.Contains(v, "LAST 7 DAYS") || !strings.Contains(v, "runs 2 (council 1 · review 1)") || !strings.Contains(v, "accepted 0% (0 / 1)") {
		t.Fatalf("7-day view:\n%s", v)
	}
}

func TestEnterShowsTheFullMessage(t *testing.T) {
	full := "CSRF on /admin is open. " + strings.Repeat("Explain the attack and the fix. ", 20)
	m := openProfile(t, func(key string) (string, bool) { return full, key == "s/2" })
	m, _ = ppress(t, m, "enter")
	if got := strings.Join(strings.Fields(m.View()), " "); !strings.Contains(got, strings.Join(strings.Fields(full), " ")) {
		t.Fatalf("detail must show the whole card:\n%s", m.View())
	}
	m, _ = ppress(t, m, "esc")
	m, _ = ppress(t, m, "j")
	m, _ = ppress(t, m, "j")
	m, _ = ppress(t, m, "enter")
	if v := m.View(); !strings.Contains(v, "Old finding.") || !strings.Contains(v, "first 300 characters") {
		t.Fatalf("an old run shows its stored excerpt:\n%s", v)
	}
}

func TestKeysSendExportMemoryAndBack(t *testing.T) {
	m := openProfile(t, nil)
	if _, msg := ppress(t, m, "x"); msg != (ExportMsg{Slug: "security"}) {
		t.Fatalf("x: %#v", msg)
	}
	if _, msg := ppress(t, m, "M"); msg != (MemoryMsg{Slug: "security", Name: "Security"}) {
		t.Fatalf("M: %#v", msg)
	}
	if _, msg := ppress(t, m, "esc"); msg != (BackMsg{}) {
		t.Fatalf("esc: %#v", msg)
	}
	m.d.Dot.Origin = "project"
	if m2, msg := ppress(t, m, "x"); msg != nil || !strings.Contains(m2.View(), "shared through their repository") {
		t.Fatalf("project dot export: %#v", msg)
	}
}

func TestHintsTable(t *testing.T) {
	base := func() history.Stats { return history.Stats{Runs: map[string]int{}} }
	cases := []struct {
		name  string
		st    func() history.Stats
		share float64
		want  string
	}{
		{"mostly dismissed", func() history.Stats { s := base(); s.Resolved, s.Accepted = 10, 1; return s }, 0, "Mostly dismissed."},
		{"rarely speaks", func() history.Stats { s := base(); s.Verdicts, s.Flagged = 50, 1; return s }, 0, "Rarely speaks."},
		{"expensive", func() history.Stats { s := base(); s.CostUSD = 1; return s }, 0.5, "Your most expensive dot."},
		{"repeats", func() history.Stats { s := base(); s.Suppressed = 5; return s }, 0, "Keeps repeating dismissed advice."},
		{"fails", func() history.Stats { s := base(); s.Runs["review"], s.Errors = 10, 2; return s }, 0, "Fails often."},
	}
	for _, c := range cases {
		hs := Hints(c.st(), c.share)
		if len(hs) != 1 || !strings.HasPrefix(hs[0], c.want) {
			t.Errorf("%s: %v", c.name, hs)
		}
	}
	quiet := []struct {
		name  string
		st    func() history.Stats
		share float64
	}{
		{"9 resolved", func() history.Stats { s := base(); s.Resolved = 9; return s }, 0},
		{"49 verdicts", func() history.Stats { s := base(); s.Verdicts = 49; return s }, 0},
		{"cheap but all of it", func() history.Stats { s := base(); s.CostUSD = 0.99; return s }, 1},
		{"4 suppressed", func() history.Stats { s := base(); s.Suppressed = 4; return s }, 0},
		{"9 runs failing", func() history.Stats { s := base(); s.Runs["review"], s.Errors = 9, 9; return s }, 0},
	}
	for _, c := range quiet {
		if hs := Hints(c.st(), c.share); len(hs) != 0 {
			t.Errorf("%s: %v", c.name, hs)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/ui/style ./internal/ui/profile`
Expected: FAIL to build: `undefined: Wrap` (style) and `undefined: New` / `undefined: Hints` (profile).

- [ ] **Step 3: Write the text helpers and the profile**

Create `internal/ui/style/text.go`:

```go
package style

import (
	"strings"

	"herdr-dots/internal/textutil"
)

// Clean makes stored or model text safe for one terminal line: control
// characters stripped and whitespace collapsed.
func Clean(s string) string {
	return strings.Join(strings.Fields(textutil.SanitizeModelText(s)), " ")
}

// Wrap breaks each paragraph of s (split on newlines, each Cleaned) into
// lines of at most w runes, on spaces where it can. Blank paragraphs stay as
// empty lines, so a purpose keeps its shape. Nothing is ever cut off.
func Wrap(s string, w int) []string {
	w = max(w, 10)
	var lines []string
	for _, para := range strings.Split(textutil.SanitizeModelText(s), "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		var cur []rune
		for _, word := range words {
			r := []rune(word)
			for len(r) > w {
				if len(cur) > 0 {
					lines, cur = append(lines, string(cur)), nil
				}
				lines, r = append(lines, string(r[:w])), r[w:]
			}
			switch {
			case len(cur) == 0:
				cur = r
			case len(cur)+1+len(r) <= w:
				cur = append(append(cur, ' '), r...)
			default:
				lines, cur = append(lines, string(cur)), r
			}
		}
		if len(cur) > 0 {
			lines = append(lines, string(cur))
		}
	}
	return lines
}
```

Create `internal/ui/profile/model.go`:

```go
// Package profile is the dot profile screen in the manage popup: 7- and
// 30-day stats from the dot's history, a memory summary, deterministic tuning
// hints and the recent history, with the full message of any row.
package profile

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// Rows is how many recent runs the profile reads.
const Rows = 50

type Deps struct {
	History  *history.Store
	Memory   *memory.Store // nil: memory off
	Dot      view.LibraryDot
	Purpose  string
	Now      func() time.Time                    // nil = time.Now
	CardBody func(cardKey string) (string, bool) // full text of a card still in this session's queue; may be nil
}

// Messages the profile sends to its parent.
type (
	BackMsg   struct{}
	ExportMsg struct{ Slug string }
	MemoryMsg struct{ Slug, Name string }
)

type (
	profileMsg struct {
		p   history.Profile
		err error
	}
	shareMsg struct {
		share float64
		err   error
	}
	memMsg struct {
		sum MemSummary
		err error
	}
)

// MemSummary counts a dot's live memory across every project.
type MemSummary struct {
	Projects, Entries, Pinned int
	ByKind                    map[memory.Kind]int
}

type Model struct {
	d          Deps
	prof       history.Profile
	loaded     bool
	err        error
	share      float64
	shareReady bool
	mem        MemSummary
	memErr     error
	week       bool // false: last 30 days (default); tab toggles
	cursor     int
	detail     bool
	status     string
	width      int
	height     int
}

func New(d Deps) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Model{d: d}
}

// Init loads the profile, the cost share (it reads every dot's history, so
// it comes second) and the memory summary off the UI goroutine.
func (m Model) Init() tea.Cmd {
	hs, slug, now, st := m.d.History, m.d.Dot.Slug, m.d.Now(), m.d.Memory
	return tea.Batch(
		func() tea.Msg {
			if hs == nil {
				return profileMsg{err: fmt.Errorf("history is not available")}
			}
			p, err := hs.Profile(slug, now, Rows)
			return profileMsg{p: p, err: err}
		},
		func() tea.Msg {
			if hs == nil {
				return shareMsg{}
			}
			share, _, err := hs.CostShare(slug, now, 30*24*time.Hour)
			return shareMsg{share: share, err: err}
		},
		func() tea.Msg {
			if st == nil {
				return memMsg{}
			}
			sum, err := Summarize(st, slug)
			return memMsg{sum: sum, err: err}
		},
	)
}

// Summarize counts slug's live dot and global memory over every project.
func Summarize(st *memory.Store, slug string) (MemSummary, error) {
	sum := MemSummary{ByKind: map[memory.Kind]int{}}
	if !memory.ValidSlug(slug) {
		return sum, nil
	}
	ps, err := st.Projects()
	add := func(es []memory.Entry) {
		for _, e := range es {
			sum.Entries++
			sum.ByKind[e.Kind]++
			if e.Pinned {
				sum.Pinned++
			}
		}
	}
	for _, p := range ps {
		es, _ := st.List(p.ID, memory.Filter{Scope: memory.ScopeDot, Dot: slug})
		if len(es) > 0 {
			sum.Projects++
		}
		add(es)
	}
	g, _ := st.List("", memory.Filter{Scope: memory.ScopeGlobal, Dot: slug})
	add(g)
	return sum, err
}

// Hints are the deterministic tuning hints (spec §6.2) for a 30-day window.
// share is this dot's part of every dot's cost in the same window.
func Hints(s history.Stats, share float64) []string {
	var out []string
	if s.Resolved >= 10 && s.AcceptanceRate() < 0.20 {
		out = append(out, "Mostly dismissed. Tighten the purpose, raise cooldown, or switch to suggest.")
	}
	if s.Verdicts >= 50 && s.FlaggedRate() < 0.03 {
		out = append(out, "Rarely speaks. Consider observe mode or narrower file_globs.")
	}
	if share >= 0.5 && s.CostUSD >= 1 {
		out = append(out, "Your most expensive dot. Try a cheaper model or lower effort.")
	}
	if s.Suppressed >= 5 {
		out = append(out, "Keeps repeating dismissed advice. Review its dismissed memories.")
	}
	if s.TotalRuns() >= 10 && s.ErrorRate() >= 0.20 {
		out = append(out, "Fails often. Check the backend (press r to reset).")
	}
	return out
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case profileMsg:
		m.prof, m.err, m.loaded = msg.p, msg.err, true
		m.cursor = min(m.cursor, max(0, len(m.prof.Rows)-1))
	case shareMsg:
		m.share, m.shareReady = msg.share, msg.err == nil
	case memMsg:
		m.mem, m.memErr = msg.sum, msg.err
	case tea.KeyMsg:
		return m.keys(msg)
	}
	return m, nil
}

func (m Model) keys(k tea.KeyMsg) (Model, tea.Cmd) {
	m.status = ""
	if m.detail {
		switch k.String() {
		case "esc", "q", "enter":
			m.detail = false
		}
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		return m, func() tea.Msg { return BackMsg{} }
	case "tab":
		m.week = !m.week
	case "j", "down":
		m.cursor = min(m.cursor+1, max(0, len(m.prof.Rows)-1))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "enter":
		if m.cursor < len(m.prof.Rows) {
			m.detail = true
		}
	case "x":
		if m.d.Dot.Origin == dots.OriginProject {
			m.status = "project dots are shared through their repository"
			return m, nil
		}
		slug := m.d.Dot.Slug
		return m, func() tea.Msg { return ExportMsg{Slug: slug} }
	case "M":
		if m.d.Memory == nil {
			m.status = "memory is turned off (memory_enabled = false)"
			return m, nil
		}
		slug, name := m.d.Dot.Slug, m.d.Dot.Name
		return m, func() tea.Msg { return MemoryMsg{Slug: slug, Name: name} }
	}
	return m, nil
}

func (m Model) w() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func pct(r float64) string { return fmt.Sprintf("%.0f%%", r*100) }

func runsLine(st history.Stats) string {
	type kv struct {
		k string
		n int
	}
	var kinds []kv
	for k, n := range st.Runs {
		kinds = append(kinds, kv{k, n})
	}
	sort.Slice(kinds, func(i, j int) bool {
		if kinds[i].n != kinds[j].n {
			return kinds[i].n > kinds[j].n
		}
		return kinds[i].k < kinds[j].k
	})
	parts := make([]string, len(kinds))
	for i, x := range kinds {
		parts[i] = fmt.Sprintf("%s %d", x.k, x.n)
	}
	line := fmt.Sprintf("runs %d", st.TotalRuns())
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, " · ") + ")"
	}
	return line + "   flagged " + pct(st.FlaggedRate())
}

func (m Model) memLine() string {
	if m.d.Memory == nil {
		return "memory is turned off"
	}
	if m.memErr != nil {
		return "memory unavailable: " + style.Clean(m.memErr.Error())
	}
	s := m.mem
	var kinds []string
	for _, k := range []memory.Kind{memory.KindFinding, memory.KindDismissed, memory.KindConvention, memory.KindDecision, memory.KindNote, memory.KindPreference, memory.KindResearch, memory.KindQuestion} {
		if n := s.ByKind[k]; n > 0 {
			kinds = append(kinds, fmt.Sprintf("%d %s", n, plural(string(k), n)))
		}
	}
	line := fmt.Sprintf("%d %s · %d entries", s.Projects, plural("project", s.Projects), s.Entries)
	if len(kinds) > 0 {
		line += " (" + strings.Join(kinds, " · ") + ")"
	}
	return line + fmt.Sprintf(" · %d pinned", s.Pinned)
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func section(title string, w int) string {
	return style.Dim.Render("── " + title + " " + strings.Repeat("─", max(0, w-len(title)-5)))
}

func (m Model) row(r history.Record) string {
	res := r.Resolution
	if res == "" && r.Card != "" {
		res = "pending"
	}
	sev := r.Severity
	if sev == "" {
		sev = r.Outcome
	}
	line := fmt.Sprintf("%s  %-9s %-9s %-10s %s", r.At.Local().Format("01-02 15:04"), r.Kind, sev, res, style.Clean(r.Excerpt))
	if rs := []rune(line); len(rs) > m.w()-2 {
		line = string(rs[:max(10, m.w()-3)]) + style.Sym("…") // Phase 6: ASCII stand-in with HERDR_DOTS_ASCII=1
	}
	return line
}

func (m Model) View() string {
	w := m.w()
	d := m.d.Dot
	var b strings.Builder
	origin := d.Origin
	if origin == "" {
		origin = dots.OriginUser
	}
	b.WriteString(style.Title.Render(fmt.Sprintf("%s %s · %s/%s · %s", style.Glyph(d.Glyph, d.Color), strings.ToUpper(style.Clean(d.Name)), d.Backend, d.Model, d.Mode)))
	b.WriteString(style.Dim.Render("   origin: "+origin+"   "+dots.DisplaySlug(d.Slug)) + "\n")
	if m.detail && m.cursor < len(m.prof.Rows) {
		r := m.prof.Rows[m.cursor]
		text, full := r.Excerpt, false
		if m.d.CardBody != nil && r.Card != "" {
			if body, ok := m.d.CardBody(r.Card); ok {
				text, full = body, true
			}
		}
		b.WriteString(section("RUN "+r.At.Local().Format("2006-01-02 15:04:05"), w) + "\n")
		b.WriteString(fmt.Sprintf("kind %s · outcome %s · severity %s · resolution %s · turn %d\n", r.Kind, r.Outcome, r.Severity, r.Resolution, r.Turn))
		for _, l := range style.Wrap(text, w-2) {
			b.WriteString(l + "\n")
		}
		if !full && r.Excerpt != "" {
			b.WriteString(style.Dim.Render("(history keeps the first 300 characters; the card is no longer in the queue)") + "\n")
		}
		b.WriteString("\n" + style.Dim.Render("esc back"))
		return b.String()
	}
	if p := style.Wrap(m.d.Purpose, w-11); len(p) > 0 {
		line := "Purpose: " + p[0]
		if len(p) > 1 {
			line += style.Sym("…")
		}
		b.WriteString(line + "\n")
	}
	title, st := "LAST 30 DAYS", m.prof.Month
	if m.week {
		title, st = "LAST 7 DAYS", m.prof.Week
	}
	b.WriteString(section(title, w) + "\n")
	switch {
	case !m.loaded:
		b.WriteString(style.Dim.Render("loading"+style.Sym("…")) + "\n")
	case m.err != nil:
		b.WriteString(style.Err.Render("history unavailable: "+style.Clean(m.err.Error())) + "\n")
	default:
		b.WriteString(runsLine(st) + "\n")
		cost := fmt.Sprintf("cost $%.2f", st.CostUSD)
		if st.CostUnknown > 0 {
			cost += fmt.Sprintf(" (+%d unpriced %s)", st.CostUnknown, plural("run", st.CostUnknown))
		}
		b.WriteString(fmt.Sprintf("accepted %s (%d / %d)   suppressed %d   %s   median %.1f s\n",
			pct(st.AcceptanceRate()), st.Accepted, st.Resolved, st.Suppressed, cost, float64(st.MedianMs)/1000))
	}
	b.WriteString(section("MEMORY", w) + "\n" + m.memLine() + "\n")
	if m.loaded && m.err == nil {
		share := 0.0
		if m.shareReady {
			share = m.share
		}
		if hs := Hints(m.prof.Month, share); len(hs) > 0 {
			b.WriteString(section("HINTS", w) + "\n")
			for _, h := range hs {
				b.WriteString(h + "\n")
			}
		}
	}
	b.WriteString(section("HISTORY", w) + "\n")
	if m.loaded && len(m.prof.Rows) == 0 {
		b.WriteString(style.Dim.Render("no runs yet") + "\n")
	}
	n := max(5, m.height-14)
	if m.height == 0 {
		n = 10
	}
	start := max(0, min(m.cursor-n/2, len(m.prof.Rows)-n))
	for i := start; i < min(len(m.prof.Rows), start+n); i++ {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		line := marker + m.row(m.prof.Rows[i])
		if i == m.cursor {
			line = style.Selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if m.loaded && m.prof.Month.Skipped > 0 {
		b.WriteString(style.Dim.Render(fmt.Sprintf("(%d unreadable history lines skipped)", m.prof.Month.Skipped)) + "\n")
	}
	if m.status != "" {
		b.WriteString(m.status + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("enter details · M memory · x export · tab 7d/30d · esc back"))
	return b.String()
}
```

**`view.LibraryDot.Origin`:** the profile reads `m.d.Dot.Origin`. Add that one field now, as the last field of `LibraryDot`. In `internal/view/view.go`, find (Phase 7's write fields end the struct):

```go
	Write               bool `json:"write,omitempty"`                 // [capabilities] write (Phase 7)
	WriteTimeoutSeconds int  `json:"write_timeout_seconds,omitempty"` // shown in the draft confirm
}
```

Replace with:

```go
	Write               bool `json:"write,omitempty"`                 // [capabilities] write (Phase 7)
	WriteTimeoutSeconds int  `json:"write_timeout_seconds,omitempty"` // shown in the draft confirm

	Origin string `json:"origin,omitempty"` // Phase 9: user, template, import or project
}
```

and list `internal/view/view.go` in this task's commit. Task 9 then adds the remaining Phase 9 fields below it (its Find block anchors on this line). No other Wave 2 task touches `view.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/ui/style ./internal/ui/profile ./internal/view`
Expected: `ok` for all three.

- [ ] **Step 5: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/style/text.go internal/ui/style/text_test.go internal/ui/profile internal/view/view.go
git commit -m "feat(ui): dot profile screen with 7/30-day stats, memory summary, tuning hints and history (Phase 9)"
```

---
### Task 9: Engine project dots, trust IPC, MCP clamp, wiring

The engine side of project dots (spec §6.4, S7, S8; Plan decisions 1–4, 13–16): scan a pane's repository, merge trusted (clamped) project dots, refuse untrusted ones, pause a changed one, trust and untrust over IPC, enforce the `mcp` clamp in Phase 5's MCP paths, and wire every Phase 9 dependency into the real engine.

**Depends on:** Task 2 (`dots` project helpers, `ClampDot`), Task 5 (`projectdots`), Task 7 (`engine.go` history edits land first). Wave 3 (runs beside Task 10).

**Files:**
- Create: `internal/engine/projectdots.go`, `internal/engine/projectdots_test.go`
- Modify: `internal/engine/engine.go` (imports, `Deps`, `Engine` fields, `New`, `ReloadDots`, `Start`, `Attach`, `Snapshot`, `runDot`), `internal/engine/control.go` (`SetMode`), `internal/engine/deliver.go` (`poll`, `dropPane`), `internal/engine/handle.go` (`reload_dots` args, `trust`, `untrust`), Phase 5's `internal/engine/ask.go` (`askable`) and `internal/engine/ask_council.go` (`AskCouncil`), Phase 8's `internal/engine/background.go` (`runBackground`)
- Modify: `internal/ipc/ipc.go`, `internal/view/view.go`, `internal/config/config.go`, `internal/config/config_test.go`
- Create: `internal/config/projectdots_test.go`
- Modify: `cmd/herdr-dots/engine.go` (`buildEngine`, new `historyStoreFor`)

**Interfaces:**
- Consumes: Task 2 `dots.OriginProject`, `DisplaySlug`, `SplitProjectSlug`, `ProjectTag`, `CheckCapabilities`, `CapabilitySpec.MCP`, `dots.CanWrite` (Phase 7); Task 5 `projectdots.TrustStore`, `ScanProject`, `Stamp`, `Scan.Find`, `Item`, `Hex`; Task 7 `engine.Deps.History`, `SessionKey`, the `waiting` line in `dropPane`; Task 8 `view.LibraryDot.Origin`; Phase 2 `memory.Project`, `memory.ResolveProject`, `memory.ExecGit`, `cmd/herdr-dots` `resolveProject`; Phase 5 `askable`, `AskCouncil` (`ask_council.go`), `startCouncil` (explicit slugs go through `councilPlan`'s eligibility via `Preflight.Select`), `ErrNotAttached`; Phase 8 `e.plan` (schedule plans), `runBackground`.
- Produces:
  - `engine.Deps.Trust *projectdots.TrustStore`, `engine.Deps.ResolveRoot func(cwd string) (memory.Project, error)`; `engine.ErrUntrusted`, `engine.ErrProjectDot`; `(*Engine) ScanPane(ctx context.Context, paneID string) error`; `(*Engine) Trust(ctx context.Context, paneID, slug, sumHex string, caps []string) error`; `(*Engine) Untrust(ctx context.Context, paneID, slug string) error`; `(*Engine) mcpMembers(paneID string) (allowed []string, blocked bool)`; `(*Engine) projectDotGone(d dots.Dot) bool` (checked by `runDot` and `runBackground` before the backend).
  - `ipc.CmdTrust = "trust"`, `ipc.CmdUntrust = "untrust"`, `type ipc.TrustArgs struct{ PaneID, Slug, SHA256 string; Capabilities []string }` (JSON `pane_id`, `slug`, `sha256`, `capabilities`), `type ipc.ReloadArgs struct{ PaneID string }` (JSON `pane_id`, optional on `reload_dots`).
  - `view.LibraryDot` adds `Trusted bool`, `Clamped, Requested []string`, `ProjectID, ProjectName, Path, SHA256 string`; `view.Snapshot.PaneProjects map[string]string`.
  - `config.Config.ProjectDotsEnabled bool` (TOML `project_dots_enabled`, default true, false on a malformed or unreadable file).
  - `cmd/herdr-dots`: `historyStoreFor(p paths.Paths) *history.Store`.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/projectdots_test.go`:

```go
package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

const (
	repoProject = "g-9f2c1a7b00112233"
	helperSlug  = "proj--helper--9f2c1a7b"
)

// Review Focus 1: a repository ships an inject dot.
const repoHelperTOML = `name = "Helper"
glyph = "h"
color = "cyan"
purpose = """
Be helpful."""

[delivery]
mode = "inject"
`

type projRig struct {
	*rig
	root  string
	trust *projectdots.TrustStore
}

func newProjRig(t *testing.T, ds ...dots.Dot) *projRig {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".herdr-dots", "dots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.toml"), []byte(repoHelperTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := &projectdots.TrustStore{Path: filepath.Join(t.TempDir(), "trusted.json")}
	r := newRigWith(t, func(d *Deps) {
		d.Trust = ts
		d.ResolveRoot = func(string) (memory.Project, error) {
			return memory.Project{ID: repoProject, Name: "app", Root: root}, nil
		}
	}, ds...)
	return &projRig{rig: r, root: root, trust: ts}
}

func (r *projRig) libDot(slug string) (view.LibraryDot, bool) {
	for _, d := range r.e.Snapshot().Library {
		if d.Slug == slug {
			return d, true
		}
	}
	return view.LibraryDot{}, false
}

func (r *projRig) trustAs(t *testing.T, caps ...string) {
	t.Helper()
	d, ok := r.libDot(helperSlug)
	if !ok {
		t.Fatal("helper not in the library")
	}
	if err := r.e.Trust(context.Background(), pane, helperSlug, d.SHA256, caps); err != nil {
		t.Fatal(err)
	}
}

func TestUntrustedProjectDotIsListedButCantAttach(t *testing.T) {
	r := newProjRig(t)
	if err := r.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	d, ok := r.libDot(helperSlug)
	if !ok || d.Origin != dots.OriginProject || d.Trusted || d.Mode != "queue" || d.ProjectName != "app" ||
		strings.Join(d.Requested, ",") != "inject,mcp" || d.SHA256 == "" {
		t.Fatalf("library entry %+v", d)
	}
	if got := r.e.Snapshot().PaneProjects[pane]; got != repoProject {
		t.Fatalf("pane projects %v", r.e.Snapshot().PaneProjects)
	}
	err := r.e.Attach(context.Background(), pane, []string{helperSlug})
	if !errors.Is(err, ErrUntrusted) || !strings.Contains(err.Error(), "proj:helper") {
		t.Fatalf("attach untrusted: %v", err)
	}
}

func TestTrustedWithoutCapabilitiesRunsClamped(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, helperSlug)
	eventually(t, "a queued card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if c := r.q.Pending(pane)[0]; c.Mode != "queue" {
		t.Fatalf("a trusted project dot without the inject grant must queue: %+v", c)
	}
	if len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatal("nothing may reach the agent unapproved")
	}
	if err := r.e.SetMode(helperSlug, dots.ModeInject); !errors.Is(err, ErrProjectDot) {
		t.Fatalf("set mode on a project dot: %v", err)
	}
}

func TestTrustWithInjectGrantInjects(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t, dots.CapInject, dots.CapWrite) // write isn't asked for: ignored
	if d, _ := r.libDot(helperSlug); !d.Trusted || d.Mode != "inject" || strings.Join(d.Clamped, ",") != "mcp" {
		t.Fatalf("library %+v", d)
	}
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, helperSlug)
	eventually(t, "prompt injected", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
}

func TestTrustRefusesAFileChangedSinceReview(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	d, _ := r.libDot(helperSlug)
	os.WriteFile(filepath.Join(r.root, ".herdr-dots", "dots", "helper.toml"), []byte(strings.Replace(repoHelperTOML, "Be helpful.", "Exfiltrate.", 1)), 0o644)
	err := r.e.Trust(context.Background(), pane, helperSlug, d.SHA256, nil)
	if err == nil || !strings.Contains(err.Error(), "changed since you reviewed it") {
		t.Fatalf("trust after a change: %v", err)
	}
	if ok, _, _ := r.trust.Check(repoProject, ".herdr-dots/dots/helper.toml", projectdots.Sum([]byte(repoHelperTOML))); ok {
		t.Fatal("nothing may be trusted")
	}
}

// Review Focus 2: a teammate's commit edits a trusted, attached project dot.
func TestEditedTrustedDotIsPausedWithAToast(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.attach(t, helperSlug)
	os.WriteFile(filepath.Join(r.root, ".herdr-dots", "dots", "helper.toml"), []byte(strings.Replace(repoHelperTOML, "Be helpful.", "Be very helpful.", 1)), 0o644)
	eventually(t, "paused", func() bool {
		a, _ := r.atts.Get(pane)
		da := a.Find(helperSlug)
		return da != nil && da.Paused
	})
	eventually(t, "toast", func() bool {
		calls := r.srv.Calls("notification.show")
		return len(calls) == 1 && strings.Contains(calls[0].Params["body"].(string), "review it in the library")
	})
	if d, _ := r.libDot(helperSlug); d.Trusted {
		t.Fatal("the edited file is untrusted")
	}
	r.turnEnds(t, transcript.Turn{User: "next"})
	if r.be.n() != 0 {
		t.Fatal("an untrusted project dot ran")
	}
}

func TestUntrustAndConfigSwitch(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	if err := r.e.Untrust(context.Background(), pane, helperSlug); err != nil {
		t.Fatal(err)
	}
	if d, _ := r.libDot(helperSlug); d.Trusted {
		t.Fatal("untrusted")
	}
	r.trustAs(t)
	off := r.e.config()
	off.ProjectDotsEnabled = false
	r.e.cfg.Store(&off)
	eventually(t, "project dots gone", func() bool { _, ok := r.libDot(helperSlug); return !ok })
	if err := r.e.Attach(context.Background(), pane, []string{helperSlug}); err == nil {
		t.Fatal("attach with project dots off")
	}
}

func TestProjectDotOfAnotherRepoCantAttach(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	other := "proj--helper--00000000"
	r.e.mu.Lock()
	d := r.e.dots[helperSlug]
	d.Slug = other
	r.e.dots[other] = d
	r.e.mu.Unlock()
	if err := r.e.Attach(context.Background(), pane, []string{other}); err == nil || !strings.Contains(err.Error(), "another repository") {
		t.Fatalf("attach: %v", err)
	}
}

func TestNoTrustStoreMeansNoProjectDots(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	if err := r.e.ScanPane(context.Background(), pane); err != nil {
		t.Fatal(err)
	}
	if s := r.e.Snapshot(); len(s.Library) != 1 || s.PaneProjects != nil {
		t.Fatalf("snapshot %+v", s)
	}
}

// Binding decision #17: MCP is a clamped capability.
func TestMCPMembersLeaveOutClampedDots(t *testing.T) {
	r := newProjRig(t, dot("Security", dots.ModeQueue))
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t) // mcp not granted
	r.attach(t, "security", helperSlug)
	allowed, blocked := r.e.mcpMembers(pane)
	if !blocked || strings.Join(allowed, ",") != "security" {
		t.Fatalf("allowed %v blocked %v", allowed, blocked)
	}
}
```

Create `internal/config/projectdots_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// project_dots_enabled defaults on, can be turned off, and fails closed.
func TestProjectDotsSwitch(t *testing.T) {
	if !Default().ProjectDotsEnabled {
		t.Fatal("project dots are on by default (each file still needs trust)")
	}
	dir := t.TempDir()
	off := filepath.Join(dir, "off.toml")
	os.WriteFile(off, []byte("project_dots_enabled = false\n"), 0o600)
	if c, err := Load(off); err != nil || c.ProjectDotsEnabled {
		t.Fatalf("off: %+v %v", c, err)
	}
	bad := filepath.Join(dir, "bad.toml")
	os.WriteFile(bad, []byte("project_dots_enabled = = true\n"), 0o600)
	if c, err := Load(bad); err == nil || c.ProjectDotsEnabled {
		t.Fatalf("a malformed file must turn project dots off: %+v %v", c, err)
	}
	unreadable := filepath.Join(dir, "dir.toml")
	os.Mkdir(unreadable, 0o700)
	if c, err := Load(unreadable); err == nil || c.ProjectDotsEnabled {
		t.Fatalf("an unreadable file must turn project dots off: %+v %v", c, err)
	}
}
```

In `internal/config/config_test.go`, find (in `TestLoadBadTOMLReturnsDefaultsWithInjectOffAndError`; Phase 8 added the proactive line):

```go
	want.InjectEnabled, want.MemoryEnabled = false, false
	want.ProactiveEnabled = false
```

Replace with:

```go
	want.InjectEnabled, want.MemoryEnabled = false, false
	want.ProactiveEnabled = false
	want.ProjectDotsEnabled = false
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/engine ./internal/config`
Expected: FAIL to build: `unknown field Trust in struct literal of type Deps`, `r.e.ScanPane undefined`, `c.ProjectDotsEnabled undefined`.

- [ ] **Step 3: Config, view and IPC fields**

In `internal/config/config.go`, find the last field of `Config` (Phase 4's):

```go
	PricesFile       string                    `toml:"prices_file"`        // "" = CONFIG_DIR/prices.toml
}
```

Replace with:

```go
	PricesFile       string                    `toml:"prices_file"`        // "" = CONFIG_DIR/prices.toml

	ProjectDotsEnabled bool `toml:"project_dots_enabled"` // Phase 9: load trusted .herdr-dots/dots from repositories; false on a malformed file
}
```

In `Default()`, find:

```go
		MaxConcurrentDrafts: 2,
	}
}
```

Replace with:

```go
		MaxConcurrentDrafts: 2,

		ProjectDotsEnabled: true,
	}
}
```

In `Load`, both fail-closed branches turn the switches off (Phases 7 and 8 added write and proactive to the same line). Find:

```go
	if err != nil {
		c.InjectEnabled, c.MemoryEnabled, c.WriteEnabled, c.ProactiveEnabled = false, false, false, false
		return c, err
	}
```

Replace with:

```go
	if err != nil {
		c.InjectEnabled, c.MemoryEnabled, c.WriteEnabled, c.ProactiveEnabled = false, false, false, false
		c.ProjectDotsEnabled = false
		return c, err
	}
```

Find:

```go
		c = Default()
		c.InjectEnabled, c.MemoryEnabled, c.WriteEnabled, c.ProactiveEnabled = false, false, false, false
		return c, fmt.Errorf("config %s: %w", path, err)
```

Replace with:

```go
		c = Default()
		c.InjectEnabled, c.MemoryEnabled, c.WriteEnabled, c.ProactiveEnabled = false, false, false, false
		c.ProjectDotsEnabled = false
		return c, fmt.Errorf("config %s: %w", path, err)
```

Find (the doc comment of `Load`):

```go
// yields Default() with InjectEnabled, MemoryEnabled, WriteEnabled and
// ProactiveEnabled false (the kill switches fail closed) plus an error so the
```

Replace with:

```go
// yields Default() with InjectEnabled, MemoryEnabled, WriteEnabled,
// ProactiveEnabled and ProjectDotsEnabled false (the kill switches fail
// closed) plus an error so the
```

In `internal/view/view.go`, find the line Task 8 added at the end of `LibraryDot`:

```go
	Origin string `json:"origin,omitempty"` // Phase 9: user, template, import or project
```

Replace with:

```go
	Origin      string   `json:"origin,omitempty"`       // Phase 9: user, template, import or project
	Trusted     bool     `json:"trusted,omitempty"`      // project dots: this exact file is trusted
	Clamped     []string `json:"clamped,omitempty"`      // project dots: capabilities turned off now
	Requested   []string `json:"requested,omitempty"`    // project dots: capabilities the file asks for
	ProjectID   string   `json:"project_id,omitempty"`   // project dots: the repository's project id
	ProjectName string   `json:"project_name,omitempty"` // project dots: the repository's name
	Path        string   `json:"path,omitempty"`         // project dots: the file
	SHA256      string   `json:"sha256,omitempty"`       // project dots: hex hash of the file as scanned
```

Find the end of `Snapshot` (Phase 8's daily fields):

```go
	DailyBudgetUSD float64 `json:"daily_budget_usd"`
}
```

Replace with:

```go
	DailyBudgetUSD float64 `json:"daily_budget_usd"`

	PaneProjects map[string]string `json:"pane_projects,omitempty"` // Phase 9: pane id → project id of its scanned repository
}
```

In `internal/ipc/ipc.go`, find:

```go
// RememberArgs.As is finding, rule, convention or global.
```

Replace with:

```go
// Project dot commands (Phase 9).
const (
	CmdTrust   = "trust"   // TrustArgs: trust a project dot file as reviewed
	CmdUntrust = "untrust" // DotArgs: forget a project dot's trust (attached copies pause)
)

// TrustArgs trusts the project dot Slug of PaneID's repository. SHA256 is the
// hex hash of the file the developer reviewed; the engine refuses a file that
// changed since. Capabilities are the ones the developer kept (each must be
// one the file asks for).
type TrustArgs struct {
	PaneID       string   `json:"pane_id"`
	Slug         string   `json:"slug"`
	SHA256       string   `json:"sha256"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// ReloadArgs is reload_dots' optional argument: also scan this pane's
// repository for project dots (the manage popup sends its target).
type ReloadArgs struct {
	PaneID string `json:"pane_id,omitempty"`
}

// RememberArgs.As is finding, rule, convention or global.
```

- [ ] **Step 4: The engine's project dot state**

Create `internal/engine/projectdots.go`:

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"sort"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

var (
	// ErrUntrusted: a project dot whose exact file content isn't trusted.
	ErrUntrusted = errors.New("project dot is not trusted")
	// ErrProjectDot: project dots are changed in their repository, not here.
	ErrProjectDot = errors.New("project dots are defined by their repository")
)

// projectState is one repository's scanned project dots.
type projectState struct {
	pr   memory.Project
	scan projectdots.Scan
}

// projectDotsOn: a trust store exists and config.toml allows project dots
// (project_dots_enabled, false on a malformed config).
func (e *Engine) projectDotsOn() bool {
	return e.d.Trust != nil && e.config().ProjectDotsEnabled
}

// projectDotGone reports whether d is a project dot that is no longer loaded
// as it was when its run was queued (its file changed, lost its trust, or
// project dots were turned off): such a run never starts. Background jobs
// (Phase 8) and council members carry a copy of the dot, so runDot and
// runBackground check this right before they call the backend.
func (e *Engine) projectDotGone(d dots.Dot) bool {
	if d.Origin != dots.OriginProject {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cur, ok := e.dots[d.Slug]
	return !ok || !reflect.DeepEqual(cur, d)
}

// rebuildLocked recomputes e.dots and e.invalid from the user dots and the
// trusted project dots. Callers hold e.mu. The first repository (by root) to
// define a slug wins; another copy is reported as invalid.
func (e *Engine) rebuildLocked() {
	m := maps.Clone(e.userDots)
	if m == nil {
		m = map[string]dots.Dot{}
	}
	inv := slices.Clone(e.userBad)
	e.projOn = e.projectDotsOn()
	if e.projOn {
		owner := map[string]string{}
		for _, root := range slices.Sorted(maps.Keys(e.projects)) {
			ps := e.projects[root]
			label := ps.pr.Name
			if label == "" {
				label = filepath.Base(root)
			}
			for _, le := range ps.scan.Invalid {
				le.File = label + ": " + le.File
				inv = append(inv, le)
			}
			for _, it := range ps.scan.Items {
				if !it.Trusted {
					continue
				}
				if other, dup := owner[it.Dot.Slug]; dup {
					inv = append(inv, dots.LoadError{File: label + ": " + it.Rel, Err: fmt.Errorf("the same project dot is loaded from %s; only that copy runs", other)})
					continue
				}
				owner[it.Dot.Slug] = root
				m[it.Dot.Slug] = it.Effective
			}
		}
	}
	e.dots, e.invalid = m, inv
}

// resolveRoot finds a working directory's git repository. Project dots need
// a real repository identity (decision #11: the root commit), so path
// identities (no git, no commits, shallow clones) have none.
func (e *Engine) resolveRoot(cwd string) memory.Project {
	if cwd == "" || e.d.ResolveRoot == nil {
		return memory.Project{}
	}
	pr, err := e.d.ResolveRoot(cwd)
	if err != nil || pr.Root == "" || dots.ProjectTag(pr.ID) == "" || pr.ID[:2] != "g-" {
		return memory.Project{}
	}
	return pr
}

// knowPane records the pane's repository and scans it the first time. It
// runs git and reads files: never under a lock.
func (e *Engine) knowPane(paneID, cwd string) memory.Project {
	pr := e.resolveRoot(cwd)
	if pr.Root == "" {
		return pr
	}
	e.mu.Lock()
	e.paneRoots[paneID] = pr.Root
	_, known := e.projects[pr.Root]
	e.mu.Unlock()
	if !known {
		e.scanRoot(pr)
	}
	return pr
}

// ScanPane makes the pane's repository's project dots visible in the library
// (the manage popup calls it through reload_dots before anything is attached).
func (e *Engine) ScanPane(ctx context.Context, paneID string) error {
	if !e.projectDotsOn() {
		return nil
	}
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return fmt.Errorf("pane %s: %w", paneID, err)
	}
	e.knowPane(paneID, p.WorkDir())
	e.broadcast()
	return nil
}

// scanRoot reads one repository's project dots (outside every lock), swaps
// them in, and pauses attached dots whose trusted file changed, vanished or
// lost its trust. It reports whether anything changed.
func (e *Engine) scanRoot(pr memory.Project) bool {
	s := projectdots.ScanProject(pr.Root, pr.ID, e.d.Trust)
	e.mu.Lock()
	old := e.projects[pr.Root]
	e.projects[pr.Root] = &projectState{pr: pr, scan: s}
	var revoked []dots.Dot
	if old != nil {
		for _, it := range old.scan.Items {
			if !it.Trusted {
				continue
			}
			if now, ok := s.Find(it.Dot.Slug); !ok || !now.Trusted || now.Sum != it.Sum {
				revoked = append(revoked, it.Dot)
			}
		}
		if old.scan.Stamp != s.Stamp {
			// Phase 8: a changed or newly trusted schedule is planned afresh.
			for _, it := range append(slices.Clone(old.scan.Items), s.Items...) {
				delete(e.plan, it.Dot.Slug)
			}
		}
	}
	e.rebuildLocked()
	e.mu.Unlock()
	for _, d := range revoked {
		e.pauseRevoked(d)
	}
	return old == nil || old.scan.Stamp != s.Stamp
}

// pauseRevoked pauses a project dot on every pane it is attached to and says
// why once.
func (e *Engine) pauseRevoked(d dots.Dot) {
	attached := false
	for _, a := range e.d.Attachments.List() {
		if a.Find(d.Slug) == nil {
			continue
		}
		attached = true
		_ = e.d.Attachments.Update(a.PaneID, func(x *state.Attachment) {
			if da := x.Find(d.Slug); da != nil {
				da.Paused = true
			}
		})
	}
	if attached {
		e.notify(d, fmt.Sprintf("project dot %s changed or lost its trust and is paused: review it in the library (T) to trust it again", dots.DisplaySlug(d.Slug)))
	}
}

// rescanProjects scans every known repository whose files or trust store
// changed (all of them with force). It also applies a project_dots_enabled
// change. It reports whether the library changed.
func (e *Engine) rescanProjects(force bool) bool {
	e.mu.Lock()
	var prs []memory.Project
	var stamps []string
	for _, ps := range e.projects {
		prs = append(prs, ps.pr)
		stamps = append(stamps, ps.scan.Stamp)
	}
	flipped := e.projOn != e.projectDotsOn()
	if flipped {
		e.rebuildLocked()
	}
	e.mu.Unlock()
	changed := flipped
	if !e.projectDotsOn() {
		return changed
	}
	for i, pr := range prs {
		if force || projectdots.Stamp(pr.Root, e.d.Trust) != stamps[i] {
			if e.scanRoot(pr) {
				changed = true
			}
		}
	}
	return changed
}

// findProjectItem returns the pane's repository and the scanned item for
// slug, reading the files fresh.
func (e *Engine) findProjectItem(ctx context.Context, paneID, slug string) (memory.Project, projectdots.Item, error) {
	if !e.projectDotsOn() {
		return memory.Project{}, projectdots.Item{}, errors.New("project dots are turned off (project_dots_enabled = false)")
	}
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return memory.Project{}, projectdots.Item{}, fmt.Errorf("pane %s: %w", paneID, err)
	}
	pr := e.knowPane(paneID, p.WorkDir())
	if pr.Root == "" {
		return pr, projectdots.Item{}, fmt.Errorf("pane %s is not in a git repository with commits", paneID)
	}
	it, ok := projectdots.ScanProject(pr.Root, pr.ID, e.d.Trust).Find(slug)
	if !ok {
		return pr, it, fmt.Errorf("no project dot %s in %s", dots.DisplaySlug(slug), pr.Name)
	}
	return pr, it, nil
}

// Trust trusts a project dot exactly as the developer reviewed it: sumHex is
// the hash of the bytes they saw; a file changed since is refused. Only
// capabilities the file asks for can be granted.
func (e *Engine) Trust(ctx context.Context, paneID, slug, sumHex string, caps []string) error {
	if err := dots.CheckCapabilities(caps); err != nil {
		return err
	}
	pr, it, err := e.findProjectItem(ctx, paneID, slug)
	if err != nil {
		return err
	}
	if projectdots.Hex(it.Sum) != sumHex {
		return fmt.Errorf("%s changed since you reviewed it: review it again", it.Rel)
	}
	var grant []string
	for _, c := range caps {
		if slices.Contains(it.Requested, c) {
			grant = append(grant, c)
		}
	}
	if err := e.d.Trust.Trust(pr.ID, it.Rel, it.Dot.SourcePath, it.Sum, grant); err != nil {
		return err
	}
	e.scanRoot(pr)
	e.broadcast()
	return nil
}

// Untrust forgets a project dot's trust; attached copies are paused.
func (e *Engine) Untrust(ctx context.Context, paneID, slug string) error {
	pr, it, err := e.findProjectItem(ctx, paneID, slug)
	if err != nil {
		return err
	}
	if _, err := e.d.Trust.Revoke(pr.ID, it.Rel); err != nil {
		return err
	}
	e.scanRoot(pr)
	e.broadcast()
	return nil
}

// untrustedLocked reports whether slug is a scanned, untrusted project dot.
func (e *Engine) untrustedLocked(slug string) bool {
	for _, ps := range e.projects {
		if it, ok := ps.scan.Find(slug); ok && !it.Trusted {
			return true
		}
	}
	return false
}

// projectLibraryLocked lists project dots for the snapshot: trusted and
// untrusted, one entry per slug, repositories in root order.
func (e *Engine) projectLibraryLocked() []view.LibraryDot {
	if !e.projOn {
		return nil
	}
	var out []view.LibraryDot
	seen := map[string]bool{}
	for _, root := range slices.Sorted(maps.Keys(e.projects)) {
		ps := e.projects[root]
		for _, it := range ps.scan.Items {
			if seen[it.Dot.Slug] {
				continue
			}
			seen[it.Dot.Slug] = true
			var clamped []string
			for _, c := range it.Clamps {
				clamped = append(clamped, c.Capability)
			}
			d := it.Effective
			out = append(out, view.LibraryDot{Slug: d.Slug, Name: d.Name, Glyph: d.Glyph, Color: d.Color,
				Backend: d.Backend.Kind, Model: d.Backend.Model, Mode: string(d.Delivery.Mode),
				Write: d.Capabilities.Write && dots.CanWrite(d.Backend.Kind), WriteTimeoutSeconds: d.Capabilities.WriteTimeoutSeconds,
				Origin: dots.OriginProject, Trusted: it.Trusted, Clamped: clamped, Requested: it.Requested,
				ProjectID: ps.pr.ID, ProjectName: ps.pr.Name, Path: it.Dot.SourcePath, SHA256: projectdots.Hex(it.Sum)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// paneProjectsLocked maps scanned panes to their repository's project id.
func (e *Engine) paneProjectsLocked() map[string]string {
	if !e.projOn || len(e.paneRoots) == 0 {
		return nil
	}
	out := map[string]string{}
	for pane, root := range e.paneRoots {
		if ps, ok := e.projects[root]; ok {
			out[pane] = ps.pr.ID
		}
	}
	return out
}

// mcpMembers splits the pane's attached dots into those the MCP server may
// ask (capabilities.mcp) and reports whether any was left out. Imported and
// project dots start without the capability (dots.ClampDot).
func (e *Engine) mcpMembers(paneID string) (allowed []string, blocked bool) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, da := range att.Dots {
		if d, ok := e.dots[da.Slug]; ok && !d.Capabilities.MCP {
			blocked = true
			continue
		}
		allowed = append(allowed, da.Slug)
	}
	return allowed, blocked
}
```

- [ ] **Step 5: Wire project dots into the engine**

In `internal/engine/engine.go`, add `"herdr-dots/internal/projectdots"` to the imports. In `Deps`, find the two Task 7 fields:

```go
	History    *history.Store // Phase 9: per-dot run and card history; nil = none
	SessionKey string         // Phase 9: this Herdr session's key; card ids are unique only within it
```

Replace with:

```go
	History    *history.Store // Phase 9: per-dot run and card history; nil = none
	SessionKey string         // Phase 9: this Herdr session's key; card ids are unique only within it

	Trust       *projectdots.TrustStore                  // Phase 9: project dot trust; nil = project dots off
	ResolveRoot func(cwd string) (memory.Project, error) // Phase 9: a pane's git repository; nil = memory.ResolveProject with git
```

In `type Engine struct`, find:

```go
	memToast bool                    // "memory write failed" already shown
```

Replace with:

```go
	memToast bool                    // "memory write failed" already shown

	// Project dots (Phase 9), guarded by mu. Their I/O never happens under mu.
	userDots  map[string]dots.Dot      // CONFIG_DIR/dots
	userBad   []dots.LoadError         // invalid user dot files
	projects  map[string]*projectState // by repository root
	paneRoots map[string]string        // pane id → repository root
	projOn    bool                     // project dots were on at the last rebuild
```

In `New`, find:

```go
	if d.Memory != nil && d.ResolveProject == nil {
		d.ResolveProject = func(cwd string) (memory.Project, error) {
			return memory.ResolveProject(context.Background(), cwd, memory.ExecGit)
		}
	}
```

Replace with (`memory.ExecGit` runs git through `platform.CommandContext` and `platform.ChildEnv`, hooks off):

```go
	if d.Memory != nil && d.ResolveProject == nil {
		d.ResolveProject = func(cwd string) (memory.Project, error) {
			return memory.ResolveProject(context.Background(), cwd, memory.ExecGit)
		}
	}
	if d.Trust != nil && d.ResolveRoot == nil {
		d.ResolveRoot = func(cwd string) (memory.Project, error) {
			return memory.ResolveProject(context.Background(), cwd, memory.ExecGit)
		}
	}
```

Find the last maps of the `&Engine{…}` literal (Phase 7's):

```go
		applying: map[string]bool{},
		gens:     map[string]uint64{},
	}
```

Replace with (gofmt re-aligns the literal):

```go
		applying: map[string]bool{},
		gens:     map[string]uint64{},
		projects:  map[string]*projectState{},
		paneRoots: map[string]string{},
	}
```

In `ReloadDots`, find (Phase 8 re-plans every schedule here):

```go
	e.mu.Lock()
	e.dots, e.invalid = m, invalid
	e.plan = map[string]time.Time{} // re-plan every schedule from the reloaded dots
	e.mu.Unlock()
	e.broadcast()
```

Replace with:

```go
	e.mu.Lock()
	e.userDots, e.userBad = m, invalid
	e.rebuildLocked()
	e.plan = map[string]time.Time{} // re-plan every schedule from the reloaded dots
	e.mu.Unlock()
	e.rescanProjects(true) // project dot files and trust are read outside the lock
	e.broadcast()
```

In `Start`, find (Phase 7 starts the draft sweep last):

```go
	if e.d.Patches != nil {
		e.spawn(e.draftLoop)
	}
}
```

Replace with:

```go
	if e.d.Patches != nil {
		e.spawn(e.draftLoop)
	}
	if e.projectDotsOn() {
		// Restored attachments' repositories (git runs here, off every lock).
		atts := e.d.Attachments.List()
		e.spawn(func() {
			for _, a := range atts {
				e.knowPane(a.PaneID, a.Cwd)
			}
			e.broadcast()
		})
	}
}
```

In `Attach`, find:

```go
	var pr memory.Project
	if prev, ok := e.d.Attachments.Get(paneID); e.memEnabled() && (!ok || prev.ProjectID == "") {
		pr = e.resolve(p.WorkDir())
	}
```

Replace with:

```go
	var pr memory.Project
	if prev, ok := e.d.Attachments.Get(paneID); e.memEnabled() && (!ok || prev.ProjectID == "") {
		pr = e.resolve(p.WorkDir())
	}
	// Project dots: the pane's repository decides which ones it may attach.
	var repo memory.Project
	if e.projectDotsOn() {
		repo = e.knowPane(paneID, p.WorkDir())
	}
```

Find (in the slug loop of `Attach`):

```go
		d, ok := e.dots[slug]
		if !ok {
			e.mu.Unlock()
			return fmt.Errorf("unknown dot %q", slug)
		}
```

Replace with:

```go
		d, ok := e.dots[slug]
		if !ok {
			err := fmt.Errorf("unknown dot %q", slug)
			if e.untrustedLocked(slug) {
				err = fmt.Errorf("%w: %s (review it in the library with T, or run herdr-dots dots trust)", ErrUntrusted, dots.DisplaySlug(slug))
			}
			e.mu.Unlock()
			return err
		}
		if d.Origin == dots.OriginProject {
			if _, tag, _ := dots.SplitProjectSlug(slug); tag != dots.ProjectTag(repo.ID) {
				e.mu.Unlock()
				return fmt.Errorf("%s belongs to another repository", dots.DisplaySlug(slug))
			}
		}
```

In `Snapshot`, find (Phase 7 fills the write fields):

```go
	for _, slug := range slugs {
		d := e.dots[slug]
		s.Library = append(s.Library, view.LibraryDot{Slug: slug, Name: d.Name, Glyph: d.Glyph, Color: d.Color,
			Backend: d.Backend.Kind, Model: d.Backend.Model, Mode: string(d.Delivery.Mode),
			Write: d.Capabilities.Write && dots.CanWrite(d.Backend.Kind), WriteTimeoutSeconds: d.Capabilities.WriteTimeoutSeconds})
	}
```

Replace with:

```go
	for _, slug := range slugs {
		d := e.dots[slug]
		if d.Origin == dots.OriginProject {
			continue // listed below with their trust state
		}
		s.Library = append(s.Library, view.LibraryDot{Slug: slug, Name: d.Name, Glyph: d.Glyph, Color: d.Color,
			Backend: d.Backend.Kind, Model: d.Backend.Model, Mode: string(d.Delivery.Mode),
			Write: d.Capabilities.Write && dots.CanWrite(d.Backend.Kind), WriteTimeoutSeconds: d.Capabilities.WriteTimeoutSeconds,
			Origin: d.Origin})
	}
	s.Library = append(s.Library, e.projectLibraryLocked()...)
	s.PaneProjects = e.paneProjectsLocked()
```

In `runDot`, find (the run slot is taken; nothing has run yet):

```go
	defer func() { <-e.sem }()

	var prev string // the status before this run's "thinking", restored when an ask is cancelled
```

Replace with:

```go
	defer func() { <-e.sem }()
	if e.projectDotGone(d) {
		e.d.Logf("dot %s on %s: its project file changed or lost its trust; run skipped", d.Slug, paneID)
		return
	}

	var prev string // the status before this run's "thinking", restored when an ask is cancelled
```

In Phase 8's `runBackground` (`internal/engine/background.go`), find:

```go
	// A scheduled slot is claimed (across sessions) only now, after every gate:
	// a slot skipped for a budget stays unclaimed, so a later catch-up may run it.
	if j.claim != nil && !j.claim() {
```

Replace with (an untrusted or changed project dot never runs in the background either; its slot stays unclaimed):

```go
	if e.projectDotGone(j.dot) {
		e.d.Logf("dot %s: %s skipped (its project file changed or lost its trust)", j.dot.Slug, j.label)
		return
	}
	// A scheduled slot is claimed (across sessions) only now, after every gate:
	// a slot skipped for a budget stays unclaimed, so a later catch-up may run it.
	if j.claim != nil && !j.claim() {
```

In `internal/engine/control.go` (`SetMode`), find:

```go
	if !ok {
		return fmt.Errorf("unknown dot %q", slug)
	}
	d.Delivery.Mode = mode
```

Replace with:

```go
	if !ok {
		return fmt.Errorf("unknown dot %q", slug)
	}
	if d.Origin == dots.OriginProject {
		return fmt.Errorf("%w: edit %s in the repository", ErrProjectDot, d.SourcePath)
	}
	d.Delivery.Mode = mode
```

In `internal/engine/deliver.go` (`poll`), find:

```go
func (e *Engine) poll() {
	e.maybeReloadConfig()
```

Replace with:

```go
func (e *Engine) poll() {
	e.maybeReloadConfig()
	if e.rescanProjects(false) {
		e.broadcast()
	}
```

In `dropPane`, find (Task 7's line):

```go
	waiting := e.d.Queue.Pending(paneID) // Phase 9: OrphanPane orphans exactly these
```

Replace with (a reused pane id is scanned again):

```go
	waiting := e.d.Queue.Pending(paneID) // Phase 9: OrphanPane orphans exactly these
	delete(e.paneRoots, paneID)
```

In `internal/engine/handle.go`, find:

```go
	case ipc.CmdReloadDots:
		e.ReloadDots()
		e.ReloadConfig()
		return nil
```

Replace with:

```go
	case ipc.CmdReloadDots:
		e.ReloadDots()
		e.ReloadConfig()
		var a ipc.ReloadArgs
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &a); err != nil {
				return err
			}
		}
		if a.PaneID != "" {
			return e.ScanPane(ctx, a.PaneID)
		}
		return nil
	case ipc.CmdTrust:
		var a ipc.TrustArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Trust(ctx, a.PaneID, a.Slug, a.SHA256, a.Capabilities)
	case ipc.CmdUntrust:
		var a ipc.DotArgs
		if err := decode(raw, &a); err != nil {
			return err
		}
		return e.Untrust(ctx, a.PaneID, a.Slug)
```

- [ ] **Step 6: Enforce the `mcp` clamp in Phase 5's MCP paths**

In Phase 5's `askable` (`internal/engine/ask.go`), find:

```go
	if !known {
		return dots.Dot{}, fmt.Errorf("%w '%s': its dot file is missing or invalid", ErrNotAttached, slug)
	}
	return d, err
```

Replace with:

```go
	if !known {
		return dots.Dot{}, fmt.Errorf("%w '%s': its dot file is missing or invalid", ErrNotAttached, slug)
	}
	if !d.Capabilities.MCP {
		return d, fmt.Errorf("%w '%s': it is not available to the MCP server (capabilities.mcp = false; imported and project dots start that way)", ErrNotAttached, slug)
	}
	return d, err
```

In `AskCouncil` (`internal/engine/ask_council.go`), find:

```go
// are marked consulted. If ctx ends first, the council is abandoned. It
// always passes nil slugs (every eligible dot) and never relies on telling an
// empty list from an absent one.
```

Replace with:

```go
// are marked consulted. If ctx ends first, the council is abandoned. It
// passes nil slugs (every eligible dot) unless an attached dot lacks
// capabilities.mcp (Phase 9), and then the non-empty list of the others; it
// never relies on telling an empty list from an absent one.
```

Find:

```go
	run, err := e.startCouncil(ctx, paneID, q, nil, func(parts []council.Part, body, note string) {
```

Replace with:

```go
	var slugs []string // nil: every eligible attached dot
	if allowed, blocked := e.mcpMembers(paneID); blocked {
		if len(allowed) == 0 {
			return ipc.CouncilResult{}, errors.New("no attached dot is available to the MCP server (capabilities.mcp = false)")
		}
		slugs = allowed // startCouncil still applies its eligibility rules
	}
	run, err := e.startCouncil(ctx, paneID, q, slugs, func(parts []council.Part, body, note string) {
```

Append to `internal/engine/projectdots_test.go`:

```go
func TestMCPCannotAskAClampedDot(t *testing.T) {
	r := newProjRig(t)
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t) // mcp not granted
	r.attach(t, helperSlug)
	if _, err := r.e.askable(pane, helperSlug); !errors.Is(err, ErrNotAttached) || !strings.Contains(err.Error(), "capabilities.mcp") {
		t.Fatalf("askable: %v", err)
	}
}

// A run queued before its project file changed (a council member, a Phase 8
// background job) carries a copy of the dot: it must not start.
func TestARevokedProjectDotsQueuedRunNeverStarts(t *testing.T) {
	r := newProjRig(t, dot("Security", dots.ModeQueue))
	_ = r.e.ScanPane(context.Background(), pane)
	r.trustAs(t)
	r.e.mu.Lock()
	queued, user := r.e.dots[helperSlug], r.e.dots["security"]
	r.e.mu.Unlock()
	if r.e.projectDotGone(queued) || r.e.projectDotGone(user) {
		t.Fatal("a loaded dot is not gone")
	}
	os.WriteFile(filepath.Join(r.root, ".herdr-dots", "dots", "helper.toml"), []byte(strings.Replace(repoHelperTOML, "Be helpful.", "Be sneaky.", 1)), 0o644)
	eventually(t, "the poll notices the edit", func() bool { return r.e.projectDotGone(queued) })
	r.e.runDot(pane, queued, nil, runOpts{summon: true})
	if r.be.n() != 0 {
		t.Fatal("a revoked project dot reached its backend")
	}
}
```

- [ ] **Step 7: Wire the real engine**

In `cmd/herdr-dots/engine.go`, add `"herdr-dots/internal/history"` and `"herdr-dots/internal/projectdots"` to the imports. In `buildEngine`, find:

```go
		Memory:         memoryStoreFor(p, cfg, logf),
		ResolveProject: resolveProject,
```

Replace with (`resolveProject` is Phase 2's `memwire.go` resolver: `memory.ExecGit` through the platform helpers, 5 s timeout):

```go
		Memory:         memoryStoreFor(p, cfg, logf),
		ResolveProject: resolveProject,

		History:     historyStoreFor(p),
		SessionKey:  paths.SessionKey(p.SocketPath),
		Trust:       &projectdots.TrustStore{Path: p.TrustFile()},
		ResolveRoot: resolveProject,
```

Find:

```go
func serveEngine(p paths.Paths) error {
```

Replace with:

```go
// historyStoreFor opens per-dot history (Phase 9). Excerpts are redacted with
// credentials.Redact, the history store's default (credentials.Redact is
// variadic, so it can't be assigned to Store.Redact).
func historyStoreFor(p paths.Paths) *history.Store {
	return history.Open(p.HistoryDir())
}

func serveEngine(p paths.Paths) error {
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/engine ./internal/config ./internal/ipc ./internal/view ./cmd/herdr-dots`
Expected: `ok` for all five. `TestAttachWithMemoryDisabledResolvesNoProject` (Phase 2) still passes: its rig has no `Trust`, so no git runs.

- [ ] **Step 9: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 10: Commit**

```bash
git add internal/engine internal/ipc/ipc.go internal/view/view.go internal/config cmd/herdr-dots/engine.go
git commit -m "feat(engine): trusted project dots with clamps, pause on change, trust IPC and the MCP clamp; a revoked project dot never runs, in the background either (Phase 9)"
```

---

### Task 10: `herdr-dots dots` CLI

`list`, `export`, `import`, `trust`, `untrust` (spec §4.2 `main.dispatch`, §6.3; Plan decisions 19, 20).

**Depends on:** Task 1 (`paths.TrustFile`), Task 2, Task 5, Task 6. Wave 3 (runs beside Task 9).

**Files:**
- Create: `cmd/herdr-dots/dots.go`, `cmd/herdr-dots/dots_test.go`
- Modify: `cmd/herdr-dots/main.go` (dispatch)

**Interfaces:**
- Consumes: Task 2 `dots.LoadAll`, `Load`, `Save`, `ErrExists`, `ReadProjectFiles`, `DecodeProject`, `Requested`, `ClampDot`, `CheckCapabilities`, `DisplaySlug`, `ProjectTag`, `ProjectDir`, `OriginUser`, `OriginImport`; Task 5 `projectdots.TrustStore`, `ScanProject`, `Sum`, `Hex`; Task 6 `dotpack.Parse`, `Prepare`, `Export`, `Fetch`, `SelectMemory`, `SecretError`, `MaxBytes`, `ErrTooLarge`; Task 4 `memory.Store.ImportPack`; existing `memoryStoreFor(p, cfg, logf)` (`memwire.go`; nil `logf` is fine), `resolveProject` (`memwire.go`), `splitCSV` (`memory.go`), `paths.Discover`, `memory.ValidProjectID`, `config.Load`, `version.Version`, the `commands` map in `main.go`.
- Produces: `runDots(args []string) error` registered as `"dots"`; `type dotsCLI struct{…}` with `run(args []string) error`; `parseAnywhere(fs *flag.FlagSet, args []string) ([]string, error)`; `indentBlock(text string) string`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/herdr-dots/dots_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
)

const dotsTestPID = "g-9f2c1a7b00112233"

type dotsRig struct {
	c           *dotsCLI
	out, errOut *bytes.Buffer
	repo        string
}

func newDotsCLI(t *testing.T) *dotsRig {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	r := &dotsRig{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, repo: repo}
	r.c = &dotsCLI{
		dotsDir: filepath.Join(root, "config", "dots"),
		mem:     memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits()),
		trust:   &projectdots.TrustStore{Path: filepath.Join(root, "config", "trusted.json")},
		resolve: func(string) (memory.Project, error) {
			return memory.Project{ID: dotsTestPID, Name: "repo", Root: repo}, nil
		},
		fetch: func(context.Context, string) ([]byte, string, error) { return nil, "", os.ErrNotExist },
		cwd:   repo, out: r.out, errOut: r.errOut, in: strings.NewReader(""),
		now: func() time.Time { return time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC) },
	}
	return r
}

func (r *dotsRig) run(t *testing.T, args ...string) {
	t.Helper()
	if err := r.c.run(args); err != nil {
		t.Fatalf("dots %v: %v\nout:\n%s\nerr:\n%s", args, err, r.out, r.errOut)
	}
}

func (r *dotsRig) saveDot(t *testing.T, d dots.Dot) {
	t.Helper()
	if err := dots.Save(r.c.dotsDir, d, true); err != nil {
		t.Fatal(err)
	}
}

func TestDotsCommandIsRegistered(t *testing.T) {
	if commands["dots"] == nil {
		t.Fatal(`"dots" must be in the commands map`)
	}
}

func TestExportImportRoundTripNeverOverwrites(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Security")
	d.Purpose = "Review security."
	d.Delivery.Mode = dots.ModeInject
	r.saveDot(t, d)
	if _, err := r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Prefer zod.", Source: memory.SourceModel}); err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(r.repo, "security.dotpack.toml")
	r.run(t, "export", "security", "--with-memory", "dot", "-o", pack, "--note", "for the team")
	if err := r.c.run([]string{"export", "security", "-o", pack}); err == nil {
		t.Fatal("export over an existing file")
	}
	err := r.c.run([]string{"import", pack, "--yes"})
	if err == nil || !strings.Contains(err.Error(), "--as") {
		t.Fatalf("a slug collision must ask for --as: %v", err)
	}
	r.out.Reset()
	r.run(t, "import", pack, "--as", "Team Security", "--yes")
	o := r.out.String()
	for _, want := range []string{"IMPORT Team Security", "note: for the team", "Review security.", "inject: delivery.mode inject → queue", "--keep inject", "1. [dot convention] Prefer zod.", "imported Team Security as team-security"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
	got, err := dots.Load(r.c.dotsDir, "team-security")
	if err != nil || got.Origin != dots.OriginImport || got.Delivery.Mode != dots.ModeQueue || got.Capabilities.MCP {
		t.Fatalf("imported %+v %v", got, err)
	}
	es, _ := r.c.mem.List(dotsTestPID, memory.Filter{Scope: memory.ScopeDot, Dot: "team-security"})
	if len(es) != 1 || es[0].Source != memory.SourceImport {
		t.Fatalf("memory %+v", es)
	}
	if orig, _ := dots.Load(r.c.dotsDir, "security"); orig.Delivery.Mode != dots.ModeInject {
		t.Fatal("the original dot changed")
	}
}

func TestImportNeedsYesWithoutATerminal(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Docs")
	d.Purpose = "Docs."
	r.saveDot(t, d)
	pack := filepath.Join(r.repo, "docs.dotpack.toml")
	r.run(t, "export", "docs", "-o", pack)
	err := r.c.run([]string{"import", pack, "--as", "Docs Two"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err %v", err)
	}
	if _, err := dots.Load(r.c.dotsDir, "docs-two"); err == nil {
		t.Fatal("imported without confirmation")
	}
	r.c.interactive, r.c.in = true, strings.NewReader("y\n")
	r.run(t, "import", pack, "--as", "Docs Two", "--keep", "mcp")
	if got, _ := dots.Load(r.c.dotsDir, "docs-two"); !got.Capabilities.MCP {
		t.Fatal("--keep mcp")
	}
}

func TestExportRefusesSecretMemoryUnlessForced(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Security")
	d.Purpose = "p"
	r.saveDot(t, d)
	r.c.mem.Add(dotsTestPID, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "token: ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789", Source: memory.SourceModel})
	err := r.c.run([]string{"export", "security", "--with-memory", "dot"})
	if err == nil || !strings.Contains(err.Error(), "memory 1") || strings.Contains(err.Error(), "ghp_") {
		t.Fatalf("err %v", err)
	}
	r.run(t, "export", "security", "--with-memory", "dot", "--force")
	if !strings.Contains(r.out.String(), "[[memory]]") {
		t.Fatalf("forced export:\n%s", r.out)
	}
}

func TestImportFromURLShowsTheFinalURL(t *testing.T) {
	r := newDotsCLI(t)
	d := dots.New("Docs")
	d.Purpose = "Docs."
	r.saveDot(t, d)
	r.run(t, "export", "docs")
	data := append([]byte(nil), r.out.Bytes()...)
	r.c.fetch = func(_ context.Context, url string) ([]byte, string, error) {
		return data, "https://raw.example.invalid/team/docs.dotpack.toml", nil
	}
	r.out.Reset()
	r.run(t, "import", "https://example.invalid/short", "--as", "Docs Web", "--yes")
	if !strings.Contains(r.out.String(), "from https://raw.example.invalid/team/docs.dotpack.toml") {
		t.Fatalf("final URL not shown:\n%s", r.out)
	}
}

func TestTrustListAndUntrustAProjectDot(t *testing.T) {
	r := newDotsCLI(t)
	dir := filepath.Join(r.repo, ".herdr-dots", "dots")
	os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, "helper.toml")
	os.WriteFile(file, []byte("name = \"Helper\"\nglyph = \"h\"\npurpose = \"Be helpful.\"\n[delivery]\nmode = \"inject\"\n"), 0o644)
	r.run(t, "list")
	if !strings.Contains(r.out.String(), "proj:helper") || !strings.Contains(r.out.String(), "untrusted") {
		t.Fatalf("list:\n%s", r.out)
	}
	if err := r.c.run([]string{"trust", file, "--allow", "write", "--yes"}); err == nil || !strings.Contains(err.Error(), "doesn't ask for write") {
		t.Fatalf("granting what isn't asked: %v", err)
	}
	r.out.Reset()
	r.run(t, "trust", file, "--allow", "inject", "--yes")
	for _, want := range []string{"TRUST .herdr-dots/dots/helper.toml", "Be helpful.", "asks for: inject, mcp", "granting: inject", "stays off: mcp"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("trust preview missing %q:\n%s", want, r.out)
		}
	}
	data, _ := os.ReadFile(file)
	ok, caps, _ := r.c.trust.Check(dotsTestPID, ".herdr-dots/dots/helper.toml", projectdots.Sum(data))
	if !ok || strings.Join(caps, ",") != "inject" {
		t.Fatalf("trust %v %v", ok, caps)
	}
	r.run(t, "untrust", file)
	if ok, _, _ := r.c.trust.Check(dotsTestPID, ".herdr-dots/dots/helper.toml", projectdots.Sum(data)); ok {
		t.Fatal("still trusted")
	}
	if err := r.c.run([]string{"trust", filepath.Join(r.repo, "elsewhere.toml"), "--yes"}); err == nil {
		t.Fatal("a file outside .herdr-dots/dots")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./cmd/herdr-dots`
Expected: FAIL to build: `undefined: dotsCLI`.

- [ ] **Step 3: Write the CLI**

Create `cmd/herdr-dots/dots.go`:

```go
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/version"
)

const dotsUsage = `usage: herdr-dots dots <command> [flags]
  list                                         your dots, and this repository's project dots with their trust
  export SLUG [--with-memory dot|dot+project] [--include-user] [--force] [--project P] [--note TEXT] [-o FILE]
  import FILE|URL [--as NAME] [--keep CAP,...] [--no-memory] [--project P] [--yes]
  trust FILE [--allow CAP,...] [--yes]         trust a .herdr-dots/dots/*.toml file exactly as it is now
  untrust FILE
Imported and project dots start with every risky capability off. CAP is one of
inject, write, mcp, research, web, schedule, commit, base_url.
P is a project path or id (default: the current directory's project).`

// dotsCLI is `herdr-dots dots`: dot packs and project dot trust. It works on
// the files directly, with or without a running engine (the engine notices
// trust changes on its next poll; open the library to see an imported dot).
type dotsCLI struct {
	dotsDir     string
	mem         *memory.Store // nil: memory off (imports skip memory, exports can't carry it)
	trust       *projectdots.TrustStore
	resolve     func(dir string) (memory.Project, error)
	fetch       func(ctx context.Context, url string) ([]byte, string, error)
	cwd         string
	out, errOut io.Writer
	in          io.Reader
	interactive bool
	now         func() time.Time
}

func runDots(args []string) error {
	p, err := paths.Discover()
	if err != nil {
		return err
	}
	cfg, cerr := config.Load(p.ConfigFile())
	if cerr != nil {
		fmt.Fprintln(os.Stderr, "herdr-dots: config:", cerr, "(using defaults)")
	}
	cwd, _ := os.Getwd()
	st, _ := os.Stdin.Stat()
	client := &http.Client{Timeout: 20 * time.Second}
	c := &dotsCLI{
		dotsDir: p.DotsDir(),
		mem:     memoryStoreFor(p, cfg, nil),
		trust:   &projectdots.TrustStore{Path: p.TrustFile()},
		resolve: resolveProject,
		fetch: func(ctx context.Context, url string) ([]byte, string, error) {
			return dotpack.Fetch(ctx, client, url, dotpack.MaxBytes)
		},
		cwd: cwd, out: os.Stdout, errOut: os.Stderr, in: os.Stdin,
		interactive: st != nil && st.Mode()&os.ModeCharDevice != 0,
		now:         time.Now,
	}
	return c.run(args)
}

func (c *dotsCLI) run(args []string) error {
	if len(args) == 0 {
		return errors.New(dotsUsage)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		return c.list()
	case "export":
		return c.export(rest)
	case "import":
		return c.importPack(rest)
	case "trust":
		return c.trustFile(rest)
	case "untrust":
		return c.untrustFile(rest)
	case "help", "-h", "--help":
		fmt.Fprintln(c.out, dotsUsage)
		return nil
	}
	return fmt.Errorf("unknown dots command %q\n%s", cmd, dotsUsage)
}

// parseAnywhere accepts flags before and after positional arguments.
func parseAnywhere(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}

func (c *dotsCLI) flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("dots "+name, flag.ContinueOnError)
	fs.SetOutput(c.errOut)
	return fs
}

// ask returns true when --yes was given or the developer typed y.
func (c *dotsCLI) ask(yes bool, question string) (bool, error) {
	if yes {
		return true, nil
	}
	if !c.interactive {
		return false, errors.New("not confirmed: read the preview above, then rerun with --yes")
	}
	fmt.Fprint(c.out, question+" [y/N]: ")
	line, _ := bufio.NewReader(c.in).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes", nil
}

// projectID resolves --project (an id or a path) or the current directory.
func (c *dotsCLI) projectID(arg string) (memory.Project, error) {
	if memory.ValidProjectID(arg) {
		return memory.Project{ID: arg, Name: arg}, nil
	}
	dir := arg
	if dir == "" {
		dir = c.cwd
	}
	pr, err := c.resolve(dir)
	if pr.ID == "" {
		return pr, fmt.Errorf("can't identify the project for %s: %v", dir, err)
	}
	return pr, nil
}

func indentBlock(text string) string {
	lines := strings.Split(textutil.SanitizeModelText(strings.TrimSpace(text)), "\n")
	return "    " + strings.Join(lines, "\n    ")
}

func (c *dotsCLI) list() error {
	ds, bad := dots.LoadAll(c.dotsDir)
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "YOUR DOTS\t\t\t\t")
	for _, d := range ds {
		origin := ""
		if d.Origin != dots.OriginUser {
			origin = "(" + d.Origin + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s/%s\t%s\t%s\n", d.Slug, d.Name, d.Backend.Kind, d.Backend.Model, d.Delivery.Mode, origin)
	}
	for _, le := range bad {
		fmt.Fprintf(tw, "  ⚠ %s\t\t\t\t\n", le.Error())
	}
	pr, err := c.resolve(c.cwd)
	if err == nil && pr.Root != "" && dots.ProjectTag(pr.ID) != "" {
		s := projectdots.ScanProject(pr.Root, pr.ID, c.trust)
		if len(s.Items)+len(s.Invalid) > 0 {
			fmt.Fprintf(tw, "PROJECT DOTS (%s)\t\t\t\t\n", pr.Name)
		}
		for _, it := range s.Items {
			trust := "untrusted"
			if it.Trusted {
				trust = "trusted"
				if len(it.Granted) > 0 {
					trust += " with " + strings.Join(it.Granted, ", ")
				}
			}
			asks := "asks for nothing risky"
			if len(it.Requested) > 0 {
				asks = "asks for " + strings.Join(it.Requested, ", ")
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", dots.DisplaySlug(it.Dot.Slug), it.Dot.Name, trust, asks, it.Rel)
		}
		for _, le := range s.Invalid {
			fmt.Fprintf(tw, "  ⚠ %s\t\t\t\t\n", le.Error())
		}
	}
	return tw.Flush()
}

func (c *dotsCLI) export(args []string) error {
	fs := c.flagSet("export")
	with := fs.String("with-memory", "", "include memory: dot or dot+project")
	includeUser := fs.Bool("include-user", false, "also include memory you wrote yourself")
	force := fs.Bool("force", false, "export even if something looks like a secret")
	project := fs.String("project", "", "project path or id for --with-memory")
	note := fs.String("note", "", "a one-line note for whoever imports it")
	outFile := fs.String("o", "", "write to this file (default: stdout)")
	pos, err := parseAnywhere(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: herdr-dots dots export SLUG [flags]")
	}
	d, err := dots.Load(c.dotsDir, pos[0])
	if err != nil {
		return fmt.Errorf("dot %s: %w", pos[0], err)
	}
	var mem []memory.Entry
	if *with != "" {
		pr, err := c.projectID(*project)
		if err != nil {
			return err
		}
		if mem, err = dotpack.SelectMemory(c.mem, pr.ID, d.Slug, *with, *includeUser); err != nil {
			return err
		}
	}
	data, err := dotpack.Export(d, mem, dotpack.ExportOptions{Note: *note, Version: version.Version, Now: c.now(), Force: *force})
	var se *dotpack.SecretError
	if errors.As(err, &se) {
		return fmt.Errorf("%w: check those parts, drop --with-memory, or rerun with --force", err)
	}
	if err != nil {
		return err
	}
	if *outFile == "" {
		_, err := c.out.Write(data)
		return err
	}
	if _, err := os.Stat(*outFile); err == nil {
		return fmt.Errorf("%s exists: choose another file name", *outFile)
	}
	if err := fsutil.WriteFileAtomic(*outFile, data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(c.errOut, "exported %s with %d memory entries to %s\n", d.Name, len(mem), *outFile)
	return nil
}

func (c *dotsCLI) readPack(src string) ([]byte, string, error) {
	if strings.Contains(src, "://") {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return c.fetch(ctx, src)
	}
	st, err := os.Stat(src)
	if err != nil {
		return nil, "", err
	}
	if st.Size() > dotpack.MaxBytes {
		return nil, "", dotpack.ErrTooLarge
	}
	data, err := os.ReadFile(src)
	abs, _ := filepath.Abs(src)
	return data, abs, err
}

func (c *dotsCLI) importPack(args []string) error {
	fs := c.flagSet("import")
	as := fs.String("as", "", "save under this name (needed when the name is taken)")
	keepCSV := fs.String("keep", "", "capabilities to keep, comma-separated")
	noMemory := fs.Bool("no-memory", false, "don't import the pack's memory")
	project := fs.String("project", "", "project path or id for the pack's memory")
	yes := fs.Bool("yes", false, "import without asking (after reading the preview)")
	pos, err := parseAnywhere(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: herdr-dots dots import FILE|URL [flags]")
	}
	data, from, err := c.readPack(pos[0])
	if err != nil {
		return err
	}
	p, err := dotpack.Parse(data)
	if err != nil {
		return err
	}
	if *as != "" {
		p.Dot.Name = *as
	}
	keep := splitCSV(*keepCSV)
	d, clamps, err := dotpack.Prepare(p, keep)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(c.dotsDir, d.Slug+".toml")); err == nil {
		return fmt.Errorf("a dot named %q already exists: rerun with --as \"<new name>\" (nothing is overwritten)", d.Name)
	}
	var pr memory.Project
	memNote := ""
	switch {
	case len(p.Memory) == 0:
	case *noMemory:
		memNote = "not imported (--no-memory)"
	case c.mem == nil:
		memNote = "not imported (memory is off)"
	default:
		if pr, err = c.projectID(*project); err != nil {
			memNote = "not imported (" + err.Error() + ")"
		}
	}
	c.preview(p, d, from, clamps, keep, pr, memNote)
	ok, err := c.ask(*yes, "Import "+d.Name+"?")
	if err != nil || !ok {
		if err == nil {
			err = errors.New("not imported")
		}
		return err
	}
	if err := dots.Save(c.dotsDir, d, true); err != nil {
		if errors.Is(err, dots.ErrExists) {
			return fmt.Errorf("a dot named %q already exists: rerun with --as \"<new name>\"", d.Name)
		}
		return err
	}
	fmt.Fprintf(c.out, "imported %s as %s; open the library (prefix+d) to attach it\n", d.Name, d.Slug)
	if memNote == "" && len(p.Memory) > 0 {
		res, err := c.mem.ImportPack(pr.ID, d.Slug, p.Memory)
		fmt.Fprintf(c.out, "memory: %d added, %d merged into existing entries; they wait for your approval (herdr-dots memory approve)\n", res.Added, res.Updated)
		for _, why := range res.Rejected {
			fmt.Fprintln(c.errOut, "memory: skipped", why)
		}
		if err != nil {
			return fmt.Errorf("memory import: %w", err)
		}
	}
	return nil
}

// preview prints everything the import will do, in full.
func (c *dotsCLI) preview(p dotpack.Pack, d dots.Dot, from string, clamps []dots.Clamp, keep []string, pr memory.Project, memNote string) {
	w := c.out
	fmt.Fprintf(w, "IMPORT %s from %s\n", textutil.SanitizeModelText(d.Name), from)
	if p.Meta.Note != "" {
		fmt.Fprintf(w, "note: %s\n", textutil.SanitizeModelText(p.Meta.Note))
	}
	fmt.Fprintf(w, "exported %s by herdr-dots %s\n", p.Meta.ExportedAt.Format("2006-01-02"), textutil.SanitizeModelText(p.Meta.Version))
	fmt.Fprintf(w, "saves as: %s (%s)\nbackend:  %s/%s\nmode:     %s\npurpose (becomes this dot's system prompt):\n%s\n",
		d.Name, d.Slug, d.Backend.Kind, d.Backend.Model, d.Delivery.Mode, indentBlock(d.Purpose))
	// Phase 8 text that reaches a prompt once research or a schedule is kept.
	if len(d.Research.Topics) > 0 {
		fmt.Fprintf(w, "research topics:\n%s\n", indentBlock(strings.Join(d.Research.Topics, "\n")))
	}
	if strings.TrimSpace(d.Schedule.Task) != "" {
		fmt.Fprintf(w, "schedule task:\n%s\n", indentBlock(d.Schedule.Task))
	}
	if len(keep) > 0 {
		fmt.Fprintf(w, "kept (you asked): %s\n", strings.Join(keep, ", "))
	}
	if len(clamps) > 0 {
		fmt.Fprintln(w, "turned off:")
		for _, cl := range clamps {
			hint := "  (keep with --keep " + cl.Capability + ")"
			if !cl.Keepable {
				hint = "  (always removed)"
			}
			fmt.Fprintf(w, "  - %s%s\n", cl, hint)
		}
	}
	if len(p.Memory) > 0 {
		where := memNote
		if where == "" {
			where = "into " + pr.Name + "; every entry arrives as a proposal for you to approve"
		}
		fmt.Fprintf(w, "memory: %d entries, %s\n", len(p.Memory), where)
		for i, e := range p.Memory {
			fmt.Fprintf(w, "  %d. [%s %s] %s\n", i+1, e.Scope, e.Kind, textutil.SanitizeModelText(e.Text))
		}
	}
}

// projectFile finds a project dot file and its repository.
func (c *dotsCLI) projectFile(path string) (memory.Project, dots.ProjectFile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return memory.Project{}, dots.ProjectFile{}, err
	}
	pr, err := c.resolve(filepath.Dir(abs))
	if pr.Root == "" || !strings.HasPrefix(pr.ID, "g-") {
		return pr, dots.ProjectFile{}, fmt.Errorf("%s is not in a git repository with commits (%v)", path, err)
	}
	rel, err := filepath.Rel(pr.Root, abs)
	if err != nil || filepath.Dir(rel) != dots.ProjectDir {
		return pr, dots.ProjectFile{}, fmt.Errorf("%s is not in %s of %s", path, dots.ProjectDir, pr.Root)
	}
	files, bad := dots.ReadProjectFiles(pr.Root)
	want := filepath.ToSlash(rel)
	for _, f := range files {
		if f.Rel == want {
			return pr, f, nil
		}
	}
	for _, le := range bad {
		if le.File == want {
			return pr, dots.ProjectFile{}, le
		}
	}
	return pr, dots.ProjectFile{}, fmt.Errorf("%s: no such project dot", path)
}

func (c *dotsCLI) trustFile(args []string) error {
	fs := c.flagSet("trust")
	allowCSV := fs.String("allow", "", "capabilities to grant, comma-separated (each must be one the file asks for)")
	yes := fs.Bool("yes", false, "trust without asking (after reading the preview)")
	pos, err := parseAnywhere(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: herdr-dots dots trust FILE [--allow CAP,...] [--yes]")
	}
	pr, f, err := c.projectFile(pos[0])
	if err != nil {
		return err
	}
	d, err := dots.DecodeProject(f, pr.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Rel, err)
	}
	allow := splitCSV(*allowCSV)
	if err := dots.CheckCapabilities(allow); err != nil {
		return err
	}
	requested := dots.Requested(d)
	for _, a := range allow {
		if !slices.Contains(requested, a) {
			return fmt.Errorf("%s doesn't ask for %s", f.Rel, a)
		}
	}
	sum := projectdots.Sum(f.Data)
	_, clamps := dots.ClampDot(d, allow)
	// The whole file, not just the purpose: research topics and a schedule
	// task (Phase 8) reach prompts too, and trust covers every byte.
	fmt.Fprintf(c.out, "TRUST %s in %s\nsha256:  %s\nname:    %s (%s)\nbackend: %s/%s\nthe file, exactly as it will be trusted:\n%s\n",
		f.Rel, pr.Name, projectdots.Hex(sum), textutil.SanitizeModelText(d.Name), dots.DisplaySlug(d.Slug), d.Backend.Kind, d.Backend.Model, indentBlock(string(f.Data)))
	if len(requested) > 0 {
		fmt.Fprintf(c.out, "asks for: %s\n", strings.Join(requested, ", "))
	}
	if len(allow) > 0 {
		fmt.Fprintf(c.out, "granting: %s\n", strings.Join(allow, ", "))
	}
	for _, cl := range clamps {
		fmt.Fprintf(c.out, "stays off: %s\n", cl)
	}
	fmt.Fprintln(c.out, "Any change to this file makes it untrusted again.")
	ok, err := c.ask(*yes, "Trust it?")
	if err != nil || !ok {
		if err == nil {
			err = errors.New("not trusted")
		}
		return err
	}
	abs, _ := filepath.Abs(pos[0])
	if err := c.trust.Trust(pr.ID, f.Rel, abs, sum, allow); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "trusted %s; a running engine picks it up within seconds\n", dots.DisplaySlug(d.Slug))
	return nil
}

func (c *dotsCLI) untrustFile(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: herdr-dots dots untrust FILE")
	}
	abs, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	pr, err := c.resolve(filepath.Dir(abs))
	if pr.Root == "" {
		return fmt.Errorf("%s is not in a git repository (%v)", args[0], err)
	}
	rel, err := filepath.Rel(pr.Root, abs)
	if err != nil {
		return err
	}
	found, err := c.trust.Revoke(pr.ID, filepath.ToSlash(rel))
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintf(c.out, "%s was not trusted\n", args[0])
		return nil
	}
	fmt.Fprintf(c.out, "untrusted %s; attached copies pause\n", args[0])
	return nil
}
```

In `cmd/herdr-dots/main.go`, find:

```go
	"memory":     runMemory,
```

Replace with:

```go
	"memory":     runMemory,
	"dots":       runDots,
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./cmd/herdr-dots`
Expected: `ok  	herdr-dots/cmd/herdr-dots`.

- [ ] **Step 5: Try it on a scratch config (no Herdr session involved)**

```bash
sh scripts/build.sh
export HERDR_PLUGIN_CONFIG_DIR="$(mktemp -d)" HERDR_PLUGIN_STATE_DIR="$(mktemp -d)"
mkdir -p "$HERDR_PLUGIN_CONFIG_DIR/dots"
./bin/herdr-dots dots list
./bin/herdr-dots dots import /nonexistent.dotpack.toml; echo "exit $?"
unset HERDR_PLUGIN_CONFIG_DIR HERDR_PLUGIN_STATE_DIR
```

Expected: `YOUR DOTS` (and, if the repository you run this in has `.herdr-dots/dots/`, its project dots); then `herdr-dots: stat /nonexistent.dotpack.toml: no such file or directory` and `exit 1`. The real config and state dirs are never touched.

- [ ] **Step 6: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 7: Commit**

```bash
git add cmd/herdr-dots/dots.go cmd/herdr-dots/dots_test.go cmd/herdr-dots/main.go
git commit -m "feat(cli): herdr-dots dots list, export, import, trust and untrust (Phase 9)"
```

---

### Task 11: Manage popup: profile, import, export, trust

The popup flows (spec §7): `i` profile (with `x` export and `M` memory), `I` import with a full preview and per-capability keeps, `T` trust preview, origin badges, the target repository's project dots only, untrusted dots not attachable.

**Depends on:** Task 3 (manage test helper and template count), Task 6 (`dotpack`), Task 8 (`profile`, `style.Wrap`), Task 9 (`view` fields, `ipc` commands, `historyStoreFor`). Wave 4.

**Files:**
- Create: `internal/ui/manage/pager.go`, `internal/ui/manage/trust.go`, `internal/ui/manage/share.go`, `internal/ui/manage/share_test.go`
- Modify: `internal/ui/manage/model.go`, `cmd/herdr-dots/ui.go`

**Interfaces:**
- Consumes: Task 8 `profile.New`, `profile.Deps`, `profile.BackMsg`, `ExportMsg`, `MemoryMsg`, `style.Clean`, `style.Wrap`; Task 9 `view.LibraryDot{Origin, Trusted, Clamped, Requested, ProjectID, ProjectName, Path, SHA256}`, `view.Snapshot.PaneProjects`, `ipc.CmdTrust`, `ipc.CmdUntrust`, `ipc.TrustArgs`, `ipc.ReloadArgs`, `historyStoreFor`; Phase 4 manage `scrProviders`/`providersUpdate`/`providersView` and the `K providers` help; Phase 2 `memDeletedMsg{name, del}`; `textutil.SanitizeModelText`; Task 6 `dotpack.Parse`, `Prepare`, `Export`, `Fetch`, `SelectMemory`, `SecretError`, `MaxBytes`, `Ext`, `WithDot`, `WithDotProject`; Task 5 `projectdots.Sum`, `Hex`; Task 4 `memory.Store.ImportPack`; Task 2 `dots.DecodeProject`, `DecodeStrict`, `SplitProjectSlug`, `ClampDot`, `Requested`, `Capabilities`, origins; Task 1 `history.CardKey`; existing manage `key`, `press`, `setup`, `lib`, `fakeCaller` (tests).
- Produces: `manage.Deps` adds `History *history.Store`, `Fetch func(ctx context.Context, url string) ([]byte, string, error)`, `SessionKey string`, `ExportDir string`; screens `scrProfile`, `scrImportSource`, `scrImportPreview`, `scrImportRename`, `scrExport`, `scrTrust`; `(Model) Init()` sends `reload_dots {pane_id}`; `(Model) library() []view.LibraryDot`; `badge(view.LibraryDot) string`; `type pager struct{ offset int; seenEnd bool }`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/manage/share_test.go`:

```go
package manage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

const shareHelperTOML = "name = \"Helper\"\nglyph = \"h\"\npurpose = \"\"\"\nBe helpful.\nSecond line of the purpose.\"\"\"\n[delivery]\nmode = \"inject\"\n"

// shareLib is lib() plus a project dot of the target's repository, one of
// another repository, and origins on the user dots.
func shareLib(t *testing.T) (view.Snapshot, string) {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, ".herdr-dots", "dots")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "helper.toml")
	os.WriteFile(path, []byte(shareHelperTOML), 0o644)
	s := lib()
	s.Library[0].Origin = dots.OriginImport
	s.Library[1].Origin = dots.OriginTemplate
	s.Library = append(s.Library,
		view.LibraryDot{Slug: "proj--helper--9f2c1a7b", Name: "Helper", Glyph: "h", Backend: "claude-cli", Model: "sonnet", Mode: "queue",
			Origin: dots.OriginProject, Requested: []string{"inject", "mcp"}, Clamped: []string{"inject", "mcp"},
			ProjectID: "g-9f2c1a7b00112233", ProjectName: "app", Path: path, SHA256: projectdots.Hex(projectdots.Sum([]byte(shareHelperTOML)))},
		view.LibraryDot{Slug: "proj--other--00000000", Name: "Elsewhere", Glyph: "o", Origin: dots.OriginProject, ProjectID: "g-0000000011112222"},
	)
	s.PaneProjects = map[string]string{"w1:p1": "g-9f2c1a7b00112233"}
	return s, path
}

func TestLibraryShowsOriginsAndOnlyThisRepositorysProjectDots(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	v := m.View()
	for _, want := range []string{"Backend", "imported", "template-derived", "Helper", "⚠ project · untrusted", "i profile · I import · T trust"} {
		if !strings.Contains(v, want) {
			t.Errorf("library missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Elsewhere") {
		t.Fatalf("another repository's project dot is listed:\n%s", v)
	}
	m = press(m, "j", "j", "space")
	if !strings.Contains(m.View(), "untrusted project dot: press T") {
		t.Fatalf("space on an untrusted dot:\n%s", m.View())
	}
	press(m, "enter")
	for _, c := range fc.calls {
		if strings.Contains(c, "proj--helper") {
			t.Fatalf("an untrusted dot was attached: %v", fc.calls)
		}
	}
}

func TestInitScansTheTargetsRepository(t *testing.T) {
	fc := &fakeCaller{}
	m := New("w1:p1", Deps{Caller: fc, DotsDir: t.TempDir()})
	m.Init()()
	if len(fc.calls) != 1 || fc.calls[0] != ipc.CmdReloadDots+` {"pane_id":"w1:p1"}` {
		t.Fatalf("calls %v", fc.calls)
	}
	if New("", Deps{Caller: fc}).Init() != nil {
		t.Fatal("no target, no scan")
	}
}

// Review Focus 1: the trust preview lists what the file asks for, each off,
// and needs the whole file seen before y.
func TestTrustPreviewNeedsTheWholeFileAndSendsTheHash(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 8}) // a short popup: the preview scrolls
	m = press(m, "j", "j", "T")
	v := m.View()
	if !strings.Contains(v, "TRUST proj:helper") {
		t.Fatalf("trust preview:\n%s", v)
	}
	fc.calls = nil
	m = press(m, "y")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "scroll to the end") {
		t.Fatalf("y before the end: %v\n%s", fc.calls, m.View())
	}
	m = press(m, "space", "space", "space", "space", "space", "space", "space", "space", "space", "1") // read to the end, keep inject
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	full := m.View()
	for _, want := range []string{"Be helpful.", "Second line of the purpose.", "[x] 1 inject: delivery.mode inject → queue", "[ ] 2 mcp",
		"THE FILE AS WRITTEN", `  mode = "inject"`, "untrusted again"} {
		if !strings.Contains(full, want) {
			t.Errorf("trust preview missing %q:\n%s", want, full)
		}
	}
	m = press(m, "y")
	sum := projectdots.Hex(projectdots.Sum([]byte(shareHelperTOML)))
	want := ipc.CmdTrust + `{"pane_id":"w1:p1","slug":"proj--helper--9f2c1a7b","sha256":"` + sum + `","capabilities":["inject"]}`
	if len(fc.calls) != 1 || strings.ReplaceAll(fc.calls[0], " ", "") != want {
		t.Fatalf("trust call:\n%v\nwant\n%s", fc.calls, want)
	}
	if !strings.Contains(m.View(), "trusted Helper") {
		t.Fatalf("status:\n%s", m.View())
	}
}

func TestProjectDotsCantBeEditedOrDeletedHere(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	s, path := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m = press(m, "j", "j", "e")
	if !strings.Contains(m.View(), "edited in their repository: "+path) {
		t.Fatalf("e:\n%s", m.View())
	}
	m = press(m, "D")
	if strings.Contains(m.View(), "Delete Helper?") {
		t.Fatal("D must not delete a project dot")
	}
}

// sharePress is press for screens that batch commands (the profile loads
// three things at once): it runs every command of a tea.BatchMsg.
func sharePress(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		queue := []tea.Cmd{cmd}
		for len(queue) > 0 {
			c := queue[0]
			queue = queue[1:]
			if c == nil {
				continue
			}
			switch msg := c().(type) {
			case nil, tea.QuitMsg:
			case tea.BatchMsg:
				queue = append(queue, msg...)
			default:
				var next tea.Cmd
				m, next = m.Update(msg)
				queue = append(queue, next)
			}
		}
	}
	return m
}

func shareDeps(t *testing.T, fc *fakeCaller) (Deps, *history.Store, *memory.Store) {
	t.Helper()
	root := t.TempDir()
	hs := history.Open(filepath.Join(root, "history"))
	st := memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits())
	return Deps{Caller: fc, DotsDir: filepath.Join(root, "dots"), Check: func(context.Context, string) error { return nil },
		History: hs, Memory: st, SessionKey: "sess", ExportDir: root,
		Project: func(string) (memory.Project, error) {
			return memory.Project{ID: "g-0123456789abcdef", Name: "app"}, nil
		}}, hs, st
}

func TestProfileOpensFromTheLibraryAndExportsAPack(t *testing.T) {
	fc := &fakeCaller{}
	d, hs, st := shareDeps(t, fc)
	sec := dots.New("Security")
	sec.Purpose = "Review security."
	if err := dots.Save(d.DotsDir, sec, true); err != nil {
		t.Fatal(err)
	}
	hs.Append(history.Record{T: history.TypeRun, At: time.Now().Add(-time.Hour), Dot: "security", Kind: history.KindReview, Outcome: state.OutcomeFlagged, Card: "sess/1", Excerpt: "CSRF on /admin."})
	st.Add("g-0123456789abcdef", memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Prefer zod.", Source: memory.SourceModel})
	snap := view.Snapshot{Library: []view.LibraryDot{{Slug: "security", Name: "Security", Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Mode: "queue"}}}
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(snap))
	m = sharePress(m, "i")
	if v := m.View(); !strings.Contains(v, "LAST 30 DAYS") || !strings.Contains(v, "CSRF on /admin.") {
		t.Fatalf("profile:\n%s", v)
	}
	m = press(m, "x")
	if !strings.Contains(m.View(), "EXPORT Security") {
		t.Fatalf("export screen:\n%s", m.View())
	}
	m = press(m, "tab", "enter") // memory: dot
	out := filepath.Join(filepath.Dir(d.DotsDir), "security"+dotpack.Ext)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("export not written: %v\n%s", err, m.View())
	}
	if p, err := dotpack.Parse(data); err != nil || p.Dot.Name != "Security" || len(p.Memory) != 1 {
		t.Fatalf("pack %+v %v", p, err)
	}
	if !strings.Contains(m.View(), "exported to") {
		t.Fatalf("status:\n%s", m.View())
	}
	m = press(m, "x", "enter")
	if !strings.Contains(m.View(), "exists") {
		t.Fatalf("an export never overwrites:\n%s", m.View())
	}
}

// Review Focus 4: importing "Security" when one exists asks for a new name.
func TestImportPreviewCollisionRenameAndClamps(t *testing.T) {
	fc := &fakeCaller{}
	d, _, st := shareDeps(t, fc)
	sec := dots.New("Security")
	sec.Purpose = "Mine."
	dots.Save(d.DotsDir, sec, true)
	theirs := dots.New("Security")
	theirs.Purpose = "Theirs: review security."
	theirs.Delivery.Mode = dots.ModeInject
	data, _ := dotpack.Export(theirs, []memory.Entry{{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis."}}, dotpack.ExportOptions{Note: "team pack"})
	pack := filepath.Join(t.TempDir(), "sec"+dotpack.Ext)
	os.WriteFile(pack, data, 0o600)

	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{}))
	m = press(m, "I")
	for _, r := range pack {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = press(m, "enter")
	v := m.View()
	for _, want := range []string{"IMPORT Security from " + pack, "note:     team pack", "a dot named \"Security\" exists", "Theirs: review security.", "[ ] 1 inject: delivery.mode inject → queue", "Sessions live in Redis.", "arrive as proposals"} {
		if !strings.Contains(v, want) {
			t.Errorf("preview missing %q:\n%s", want, v)
		}
	}
	m = press(m, "y")
	if !strings.Contains(m.View(), "name is taken") {
		t.Fatalf("y over a taken name:\n%s", m.View())
	}
	m = press(m, "n", "ctrl+u")
	for _, r := range "Team Security" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = press(m, "enter", "y")
	got, err := dots.Load(d.DotsDir, "team-security")
	if err != nil || got.Origin != dots.OriginImport || got.Delivery.Mode != dots.ModeQueue || got.Purpose != "Theirs: review security." {
		t.Fatalf("imported %+v %v\n%s", got, err, m.View())
	}
	if mine, _ := dots.Load(d.DotsDir, "security"); mine.Purpose != "Mine." {
		t.Fatal("the existing dot was overwritten")
	}
	es, _ := st.List("g-0123456789abcdef", memory.Filter{Scope: memory.ScopeProject})
	if len(es) != 1 || es[0].Status != memory.StatusProposed || es[0].Source != memory.SourceImport {
		t.Fatalf("memory %+v", es)
	}
	if !strings.Contains(m.View(), "imported Team Security as team-security") || !strings.HasPrefix(fc.calls[len(fc.calls)-1], ipc.CmdReloadDots) {
		t.Fatalf("status %q calls %v", m.View(), fc.calls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOTOOLCHAIN=local go test ./internal/ui/manage`
Expected: FAIL to build: `unknown field History in struct literal of type Deps` (and `SessionKey`, `ExportDir`).

- [ ] **Step 3: Write the review pager, the trust preview and the share screens**

Create `internal/ui/manage/pager.go`:

```go
package manage

// pager scrolls a long review screen (an import or trust preview). seenEnd
// turns true once the last line has been on screen: approving needs it, so
// nothing is approved unseen.
type pager struct {
	offset  int
	seenEnd bool
}

// fit clamps the offset for total lines in a height-line window and notes
// whether the end is visible.
func (p *pager) fit(total, height int) {
	height = max(1, height)
	p.offset = max(0, min(p.offset, total-height))
	if p.offset+height >= total {
		p.seenEnd = true
	}
}

// key scrolls for j/k/down/up/space/pgdown/pgup/b and reports whether it
// handled the key.
func (p *pager) key(k string, total, height int) bool {
	switch k {
	case "j", "down":
		p.offset++
	case "k", "up":
		p.offset--
	case " ", "pgdown", "ctrl+d":
		p.offset += max(1, height-1)
	case "pgup", "b", "ctrl+u":
		p.offset -= max(1, height-1)
	default:
		return false
	}
	p.fit(total, height)
	return true
}

func (p pager) window(lines []string, height int) []string {
	height = max(1, height)
	end := min(len(lines), p.offset+height)
	return lines[min(p.offset, end):end]
}

// bodyHeight is the number of preview lines that fit above the footer.
func (m Model) bodyHeight() int {
	if m.height <= 0 {
		return 30
	}
	return max(3, m.height-4)
}

func (m Model) bodyWidth() int {
	if m.width <= 0 {
		return 80
	}
	return max(20, m.width-2)
}
```

Create `internal/ui/manage/trust.go`:

```go
package manage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/view"
)

// trustState is the trust preview of one project dot file, read by this
// popup: the hash sent with the trust command is the hash of these bytes.
type trustState struct {
	lib  view.LibraryDot
	dot  dots.Dot
	raw  string // the file's bytes, shown in full
	sum  string
	rel  string
	keep map[string]bool // per requested capability; all off at first
	pg   pager
}

type (
	trustOpenMsg struct {
		st  *trustState
		err error
	}
	trustDoneMsg struct {
		name    string
		trusted bool
	}
)

// openTrust reads the project dot file named in the snapshot and parses it.
func (m Model) openTrust(d view.LibraryDot) tea.Cmd {
	return func() tea.Msg {
		base, _, ok := dots.SplitProjectSlug(d.Slug)
		if !ok || d.Path == "" {
			return trustOpenMsg{err: fmt.Errorf("%s is not a project dot", d.Name)}
		}
		data, err := os.ReadFile(d.Path)
		if err != nil {
			return trustOpenMsg{err: err}
		}
		if len(data) > dots.MaxProjectFileBytes {
			return trustOpenMsg{err: fmt.Errorf("%s is over the project dot size cap", d.Path)}
		}
		rel := filepath.ToSlash(filepath.Join(dots.ProjectDir, filepath.Base(d.Path)))
		parsed, err := dots.DecodeProject(dots.ProjectFile{Base: base, Path: d.Path, Rel: rel, Data: data}, d.ProjectID)
		if err != nil {
			return trustOpenMsg{err: err}
		}
		return trustOpenMsg{st: &trustState{lib: d, dot: parsed, raw: string(data), sum: projectdots.Hex(projectdots.Sum(data)), rel: rel, keep: map[string]bool{}}}
	}
}

func (m Model) trustLines() []string {
	t, w := m.trust, m.bodyWidth()
	d := t.dot
	status := "untrusted"
	if t.lib.Trusted {
		status = "trusted"
		if t.lib.SHA256 != t.sum {
			status = "the file changed since the engine last read it"
		}
	}
	lines := []string{
		style.Title.Render(fmt.Sprintf("TRUST %s (%s) from %s", dots.DisplaySlug(d.Slug), style.Clean(d.Name), style.Clean(t.lib.ProjectName))),
		"file:     " + t.lib.Path,
		"sha256:   " + t.sum,
		fmt.Sprintf("backend:  %s/%s   mode as written: %s", d.Backend.Kind, d.Backend.Model, d.Delivery.Mode),
		"status:   " + status,
		"",
		"PURPOSE (becomes this dot's system prompt):",
	}
	for _, l := range style.Wrap(d.Purpose, w-2) {
		lines = append(lines, "  "+l)
	}
	lines = append(lines, "")
	_, all := dots.ClampDot(d, nil)
	requested := dots.Requested(d)
	if len(requested) == 0 {
		lines = append(lines, "It asks for no risky capability.")
	} else {
		lines = append(lines, "CAPABILITIES it asks for (off unless you turn them on with the number):")
	}
	for i, c := range requested {
		mark := "[ ]"
		if t.keep[c] {
			mark = "[x]"
		}
		desc := ""
		for _, cl := range all {
			if cl.Capability == c {
				desc = cl.String()
			}
		}
		lines = append(lines, fmt.Sprintf("  %s %d %s", mark, i+1, desc))
	}
	for _, cl := range all {
		if !cl.Keepable {
			lines = append(lines, "  always removed: "+cl.String())
		}
	}
	// Every byte is trusted, so every byte is shown: research topics, a
	// schedule task and its projects (Phase 8), backend args, and keys a
	// later field adds. Control characters are stripped for the terminal.
	lines = append(lines, "", "THE FILE AS WRITTEN (every byte you trust):")
	for _, l := range strings.Split(strings.TrimRight(textutil.SanitizeModelText(t.raw), "\n"), "\n") {
		lines = append(lines, "  "+l)
	}
	return append(lines, "", "Any change to this file makes it untrusted again, and attached copies pause.")
}

func (m Model) keysTrust(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.trust
	lines := m.trustLines()
	if t.pg.key(k.String(), len(lines), m.bodyHeight()) {
		return m, nil
	}
	requested := dots.Requested(t.dot)
	switch s := k.String(); s {
	case "esc", "q":
		m.scr, m.trust = scrLibrary, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i, _ := strconv.Atoi(s); i <= len(requested) {
			t.keep[requested[i-1]] = !t.keep[requested[i-1]]
		}
	case "y":
		if !t.pg.seenEnd {
			m.status = "scroll to the end of the file first (j or space)"
			return m, nil
		}
		var caps []string
		for _, c := range requested {
			if t.keep[c] {
				caps = append(caps, c)
			}
		}
		args := ipc.TrustArgs{PaneID: m.target, Slug: t.dot.Slug, SHA256: t.sum, Capabilities: caps}
		name := t.dot.Name
		return m, func() tea.Msg {
			if err := m.call(ipc.CmdTrust, args); err != nil {
				return errMsg{err}
			}
			return trustDoneMsg{name: name, trusted: true}
		}
	case "u":
		if !t.lib.Trusted {
			return m, nil
		}
		args := ipc.DotArgs{PaneID: m.target, Slug: t.dot.Slug}
		name := t.dot.Name
		return m, func() tea.Msg {
			if err := m.call(ipc.CmdUntrust, args); err != nil {
				return errMsg{err}
			}
			return trustDoneMsg{name: name}
		}
	}
	return m, nil
}

func (m Model) trustView() string {
	lines := m.trustLines()
	var b strings.Builder
	b.WriteString(strings.Join(m.trust.pg.window(lines, m.bodyHeight()), "\n") + "\n")
	if m.status != "" {
		b.WriteString(m.status + "\n")
	}
	help := "j/k scroll · 1-9 toggle a capability · y trust · esc back"
	if m.trust.lib.Trusted {
		help += " · u untrust"
	}
	b.WriteString(style.Dim.Render(help))
	return b.String()
}
```

Create `internal/ui/manage/share.go`:

```go
package manage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/ui/profile"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/version"
	"herdr-dots/internal/view"
)

// ---- profile

type profileOpenMsg struct {
	deps profile.Deps
	err  error
}

// openProfile loads the dot's purpose (from its file) off the UI goroutine.
func (m Model) openProfile(d view.LibraryDot) tea.Cmd {
	dir, hs, st, session := m.d.DotsDir, m.d.History, m.d.Memory, m.d.SessionKey
	cards := map[string]string{}
	for _, p := range m.snap.Panes {
		for _, c := range p.Cards {
			cards[history.CardKey(session, c.ID)] = c.Body
		}
	}
	return func() tea.Msg {
		var full dots.Dot
		var err error
		if d.Origin == dots.OriginProject {
			var data []byte
			if data, err = os.ReadFile(d.Path); err == nil {
				full, err = dots.DecodeStrict(data)
			}
		} else {
			full, err = dots.Load(dir, d.Slug)
		}
		if err != nil {
			return profileOpenMsg{err: err}
		}
		body := func(key string) (string, bool) { b, ok := cards[key]; return b, ok }
		return profileOpenMsg{deps: profile.Deps{History: hs, Memory: st, Dot: d, Purpose: full.Purpose, CardBody: body}}
	}
}

// ---- import

// importState is the import preview of one parsed pack.
type importState struct {
	pack    dotpack.Pack
	from    string
	name    string          // what it will be saved as
	keep    map[string]bool // capabilities to keep; all off at first
	project memory.Project  // where its memory goes ("" = nowhere)
	memNote string          // why memory isn't imported, if it isn't
	withMem bool            // import the pack's memory
	taken   bool            // a dot with this name exists
	pg      pager
}

type (
	importLoadedMsg struct {
		st  *importState
		err error
	}
	importedMsg struct {
		name, slug string
		res        memory.ApplyResult
		memErr     error
	}
)

func newPrompt(placeholder, value string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 2048
	ti.Width = 60
	ti.SetValue(value)
	ti.Focus()
	return ti
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// loadPack reads a pack file or URL, parses it and works out where its memory
// would go, off the UI goroutine.
func (m Model) loadPack(src string) tea.Cmd {
	fetch, dir, st, target, resolve := m.d.Fetch, m.d.DotsDir, m.d.Memory, m.target, m.d.Project
	pv, _ := m.snap.Pane(m.target)
	return func() tea.Msg {
		var data []byte
		var from string
		var err error
		if strings.Contains(src, "://") {
			if fetch == nil {
				return importLoadedMsg{err: errors.New("downloads are not available here")}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			data, from, err = fetch(ctx, src)
		} else {
			from = expandHome(src)
			var fi os.FileInfo
			if fi, err = os.Stat(from); err == nil {
				if fi.Size() > dotpack.MaxBytes {
					err = dotpack.ErrTooLarge
				} else {
					data, err = os.ReadFile(from)
				}
			}
		}
		if err != nil {
			return importLoadedMsg{err: err}
		}
		p, err := dotpack.Parse(data)
		if err != nil {
			return importLoadedMsg{err: err}
		}
		is := &importState{pack: p, from: from, name: p.Dot.Name, keep: map[string]bool{}}
		_, statErr := os.Stat(filepath.Join(dir, dots.Slugify(is.name)+".toml"))
		is.taken = statErr == nil
		switch {
		case len(p.Memory) == 0:
		case st == nil:
			is.memNote = "memory is off"
		default:
			pr := memory.Project{ID: pv.ProjectID, Name: pv.ProjectName}
			if pr.ID == "" && target != "" && resolve != nil {
				pr, _ = resolve(target)
			}
			if pr.ID == "" {
				is.memNote = "no project: focus an agent pane in the repository first"
			} else {
				is.project, is.withMem = pr, true
			}
		}
		return importLoadedMsg{st: is}
	}
}

func (m Model) importLines() []string {
	is, w := m.imp, m.bodyWidth()
	p := is.pack
	lines := []string{style.Title.Render("IMPORT " + style.Clean(is.name) + " from " + style.Clean(is.from))}
	if p.Meta.Note != "" {
		lines = append(lines, "note:     "+style.Clean(p.Meta.Note))
	}
	lines = append(lines,
		fmt.Sprintf("exported: %s by herdr-dots %s", p.Meta.ExportedAt.Format("2006-01-02"), style.Clean(p.Meta.Version)),
		fmt.Sprintf("saves as: %s (%s)", style.Clean(is.name), dots.Slugify(is.name)))
	if is.taken {
		lines = append(lines, style.Err.Render(fmt.Sprintf("⚠ a dot named %q exists: press n to choose another name (nothing is overwritten)", is.name)))
	}
	lines = append(lines,
		fmt.Sprintf("backend:  %s/%s   mode as written: %s", p.Dot.Backend.Kind, p.Dot.Backend.Model, p.Dot.Delivery.Mode),
		"", "PURPOSE (becomes this dot's system prompt):")
	for _, l := range style.Wrap(p.Dot.Purpose, w-2) {
		lines = append(lines, "  "+l)
	}
	// Phase 8 text reaches a prompt once research or a schedule is kept.
	if len(p.Dot.Research.Topics) > 0 {
		lines = append(lines, "", "RESEARCH TOPICS:")
		for _, tp := range p.Dot.Research.Topics {
			for _, l := range style.Wrap(tp, w-4) {
				lines = append(lines, "  - "+l)
			}
		}
	}
	if strings.TrimSpace(p.Dot.Schedule.Task) != "" {
		lines = append(lines, "", "SCHEDULE TASK:")
		for _, l := range style.Wrap(p.Dot.Schedule.Task, w-2) {
			lines = append(lines, "  "+l)
		}
	}
	if len(p.Dot.Backend.Args) > 0 {
		lines = append(lines, "", "backend args: "+style.Clean(strings.Join(p.Dot.Backend.Args, " ")))
	}
	lines = append(lines, "")
	_, all := dots.ClampDot(p.Dot, nil)
	requested := dots.Requested(p.Dot)
	if len(requested) > 0 {
		lines = append(lines, "CAPABILITIES it asks for (off unless you turn them on with the number):")
	}
	for i, c := range requested {
		mark := "[ ]"
		if is.keep[c] {
			mark = "[x]"
		}
		for _, cl := range all {
			if cl.Capability == c {
				lines = append(lines, fmt.Sprintf("  %s %d %s", mark, i+1, cl))
			}
		}
	}
	for _, cl := range all {
		if !cl.Keepable {
			lines = append(lines, "  always removed: "+cl.String())
		}
	}
	if len(p.Memory) > 0 {
		lines = append(lines, "")
		switch {
		case is.memNote != "":
			lines = append(lines, fmt.Sprintf("MEMORY: %d entries, not imported (%s)", len(p.Memory), is.memNote))
		case !is.withMem:
			lines = append(lines, fmt.Sprintf("MEMORY: %d entries, not imported (m to include)", len(p.Memory)))
		default:
			lines = append(lines, fmt.Sprintf("MEMORY: %d entries into %s; they arrive as proposals for you to approve (m to leave out):", len(p.Memory), style.Clean(is.project.Name)))
		}
		for i, e := range p.Memory {
			for j, l := range style.Wrap(fmt.Sprintf("[%s %s] %s", e.Scope, e.Kind, e.Text), w-6) {
				prefix := "     "
				if j == 0 {
					prefix = fmt.Sprintf("  %2d.", i+1)
				}
				lines = append(lines, prefix+" "+l)
			}
		}
	}
	return lines
}

func (m Model) keysImportSource(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.scr = scrLibrary
		return m, nil
	case "enter":
		src := strings.TrimSpace(m.input.Value())
		if src == "" {
			return m, nil
		}
		m.status = "reading " + src + "…"
		return m, m.loadPack(src)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) keysImportPreview(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	is := m.imp
	lines := m.importLines()
	if is.pg.key(k.String(), len(lines), m.bodyHeight()) {
		return m, nil
	}
	requested := dots.Requested(is.pack.Dot)
	switch s := k.String(); s {
	case "esc", "q":
		m.scr, m.imp = scrLibrary, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i, _ := strconv.Atoi(s); i <= len(requested) {
			is.keep[requested[i-1]] = !is.keep[requested[i-1]]
		}
	case "m":
		if is.memNote == "" && len(is.pack.Memory) > 0 {
			is.withMem = !is.withMem
		}
	case "n":
		m.scr, m.input = scrImportRename, newPrompt("new name", is.name)
	case "y":
		switch {
		case !is.pg.seenEnd:
			m.status = "scroll to the end of the preview first (j or space)"
			return m, nil
		case is.taken:
			m.status = "that name is taken: press n to choose another"
			return m, nil
		}
		return m, m.doImport()
	}
	return m, nil
}

func (m Model) keysImportRename(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.scr = scrImportPreview
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			return m, nil
		}
		m.imp.name = name
		_, err := os.Stat(filepath.Join(m.d.DotsDir, dots.Slugify(name)+".toml"))
		m.imp.taken = err == nil
		m.imp.pg = pager{} // a new name: read the preview again
		m.imp.pg.fit(len(m.importLines()), m.bodyHeight())
		m.scr = scrImportPreview
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// doImport saves the clamped dot (never over an existing one) and imports
// its memory as someone else's conclusions.
func (m Model) doImport() tea.Cmd {
	is, dir, st := *m.imp, m.d.DotsDir, m.d.Memory
	var keep []string
	for _, c := range dots.Capabilities {
		if is.keep[c] {
			keep = append(keep, c)
		}
	}
	return func() tea.Msg {
		p := is.pack
		p.Dot.Name = is.name
		d, _, err := dotpack.Prepare(p, keep)
		if err != nil {
			return errMsg{err}
		}
		if err := dots.Save(dir, d, true); err != nil {
			if errors.Is(err, dots.ErrExists) {
				return errMsg{fmt.Errorf("a dot named %q already exists: choose another name", d.Name)}
			}
			return errMsg{err}
		}
		msg := importedMsg{name: d.Name, slug: d.Slug}
		if is.withMem && st != nil && is.project.ID != "" {
			msg.res, msg.memErr = st.ImportPack(is.project.ID, d.Slug, p.Memory)
		}
		return msg
	}
}

func (m Model) importView() string {
	var b strings.Builder
	switch m.scr {
	case scrImportSource:
		b.WriteString(style.Title.Render("IMPORT A DOT PACK") + "\n\nA .dotpack.toml file path or an https:// URL:\n" + m.input.View() + "\n")
		if m.status != "" {
			b.WriteString(m.status + "\n")
		}
		b.WriteString("\n" + style.Dim.Render("enter read · esc back"))
	case scrImportRename:
		b.WriteString(style.Title.Render("SAVE AS") + "\n\n" + m.input.View() + "\n\n" + style.Dim.Render("enter use · esc back"))
	default:
		b.WriteString(strings.Join(m.imp.pg.window(m.importLines(), m.bodyHeight()), "\n") + "\n")
		if m.status != "" {
			b.WriteString(m.status + "\n")
		}
		b.WriteString(style.Dim.Render("j/k scroll · 1-9 keep a capability · m memory · n rename · y import · esc cancel"))
	}
	return b.String()
}

// ---- export

type exportState struct {
	slug, name  string
	with        string // "", dotpack.WithDot, dotpack.WithDotProject
	includeUser bool
	dropSecrets bool     // leave out memory entries that look like secrets
	secrets     []string // what looked like a secret on the last try
}

type exportedMsg struct {
	path    string
	entries int
}

type exportSecretMsg struct{ where []string }

func (m Model) startExport(slug string) (Model, tea.Cmd) {
	var name string
	for _, d := range m.snap.Library {
		if d.Slug == slug {
			name = d.Name
		}
	}
	dir := m.d.ExportDir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	m.exp = &exportState{slug: slug, name: name}
	m.input = newPrompt("file", filepath.Join(dir, slug+dotpack.Ext))
	m.scr = scrExport
	return m, nil
}

func (m Model) doExport() tea.Cmd {
	ex, dir, st, target, resolve := *m.exp, m.d.DotsDir, m.d.Memory, m.target, m.d.Project
	path := expandHome(strings.TrimSpace(m.input.Value()))
	pv, _ := m.snap.Pane(m.target)
	return func() tea.Msg {
		d, err := dots.Load(dir, ex.slug)
		if err != nil {
			return errMsg{err}
		}
		var mem []memory.Entry
		if ex.with != "" {
			pr := memory.Project{ID: pv.ProjectID}
			if pr.ID == "" && target != "" && resolve != nil {
				pr, _ = resolve(target)
			}
			if mem, err = dotpack.SelectMemory(st, pr.ID, ex.slug, ex.with, ex.includeUser); err != nil {
				return errMsg{err}
			}
			if ex.dropSecrets {
				kept := mem[:0]
				for _, e := range mem {
					if found, _ := memory.ScanSecrets(e.Text + " " + strings.Join(e.Paths, " ")); !found {
						kept = append(kept, e)
					}
				}
				mem = kept
			}
		}
		data, err := dotpack.Export(d, mem, dotpack.ExportOptions{Version: version.Version})
		var se *dotpack.SecretError
		if errors.As(err, &se) {
			return exportSecretMsg{where: se.Where}
		}
		if err != nil {
			return errMsg{err}
		}
		if _, err := os.Stat(path); err == nil {
			return errMsg{fmt.Errorf("%s exists: choose another file name", path)}
		}
		if err := fsutil.WriteFileAtomic(path, data, 0o600); err != nil {
			return errMsg{err}
		}
		return exportedMsg{path: path, entries: len(mem)}
	}
}

func (m Model) keysExport(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	ex := m.exp
	switch k.String() {
	case "esc":
		m.scr, m.exp = scrProfile, nil
		return m, nil
	case "tab":
		switch ex.with {
		case "":
			ex.with = dotpack.WithDot
		case dotpack.WithDot:
			ex.with = dotpack.WithDotProject
		default:
			ex.with = ""
		}
		ex.secrets, ex.dropSecrets = nil, false
		return m, nil
	case "ctrl+u":
		ex.includeUser = !ex.includeUser
		return m, nil
	case "ctrl+e":
		if len(ex.secrets) == 0 {
			return m, nil
		}
		ex.dropSecrets = true
		return m, m.doExport()
	case "enter":
		return m, m.doExport()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) exportView() string {
	ex := m.exp
	with := "none"
	if ex.with != "" {
		with = ex.with
	}
	user := "no"
	if ex.includeUser {
		user = "yes"
	}
	var b strings.Builder
	b.WriteString(style.Title.Render("EXPORT "+style.Clean(ex.name)) + "\n\n")
	b.WriteString(fmt.Sprintf("memory:              %s   (tab: none → dot → dot+project)\n", with))
	b.WriteString(fmt.Sprintf("include your entries: %s   (ctrl+u)\n", user))
	b.WriteString("never included: credentials, history, global memory, preferences\n\nfile: " + m.input.View() + "\n")
	if len(ex.secrets) > 0 {
		b.WriteString(style.Err.Render("looks like a secret: "+strings.Join(ex.secrets, ", ")) + "\n")
		b.WriteString("ctrl+e exports without those memory entries; a secret in the dot itself must be removed first\n")
	}
	if m.status != "" {
		b.WriteString(m.status + "\n")
	}
	b.WriteString("\n" + style.Dim.Render("enter export · esc back"))
	return b.String()
}
```

- [ ] **Step 4: Wire them into the library**

In `internal/ui/manage/model.go`, add `"github.com/charmbracelet/bubbles/textinput"`, `"herdr-dots/internal/history"` and `"herdr-dots/internal/ui/profile"` to the imports (gofmt puts the bubbles one in the third-party group if you add it there by hand).

Find the memory fields of `Deps` in `internal/ui/manage/model.go` (Phase 4's optional fields follow them; keep those):

```go
	Memory  *memory.Store                               // nil: memory off (no M/P, no memory step on delete)
	Project func(target string) (memory.Project, error) // the target pane's project when the snapshot lacks it; may be nil
```

Replace with:

```go
	Memory  *memory.Store                               // nil: memory off (no M/P, no memory step on delete)
	Project func(target string) (memory.Project, error) // the target pane's project when the snapshot lacks it; may be nil

	// Phase 9: profiles and sharing.
	History    *history.Store                                                // nil: no profiles
	Fetch      func(ctx context.Context, url string) ([]byte, string, error) // dot pack downloads; nil: files only
	SessionKey string                                                        // this Herdr session (joins history rows to queued cards)
	ExportDir  string                                                        // default folder for exports; "" = home
```

Find the last screen (Phase 4's):

```go
	scrProviders     // API keys and the Ollama server (Phase 4)
)
```

Replace with:

```go
	scrProviders     // API keys and the Ollama server (Phase 4)
	scrProfile       // a dot's profile (Phase 9)
	scrImportSource  // import: file path or URL prompt
	scrImportPreview // import: everything the pack would do
	scrImportRename  // import: a new name after a collision
	scrExport        // export: memory choice and file
	scrTrust         // a project dot's trust preview
)
```

Find the last field of `Model` (Phase 4's):

```go
	fetching  bool // a ctrl+space model fetch is in flight
}
```

Replace with:

```go
	fetching  bool // a ctrl+space model fetch is in flight

	prof  *profile.Model  // scrProfile
	imp   *importState    // scrImportPreview, scrImportRename
	exp   *exportState    // scrExport
	trust *trustState     // scrTrust
	input textinput.Model // the import, rename and export prompts
}
```

Find:

```go
func (m Model) Init() tea.Cmd { return nil }
```

Replace with:

```go
// Init asks the engine to reload dots and scan the target pane's repository,
// so its project dots show up before anything is attached.
func (m Model) Init() tea.Cmd {
	if m.target == "" {
		return nil
	}
	return func() tea.Msg {
		if err := m.call(ipc.CmdReloadDots, ipc.ReloadArgs{PaneID: m.target}); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// library is what this popup lists: your dots, plus the project dots of the
// target pane's repository (never another repository's).
func (m Model) library() []view.LibraryDot {
	pid := m.snap.PaneProjects[m.target]
	var out []view.LibraryDot
	for _, d := range m.snap.Library {
		if d.Origin == dots.OriginProject && (pid == "" || d.ProjectID != pid) {
			continue
		}
		out = append(out, d)
	}
	return out
}
```

At the top of `Update`, find:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scr == scrMemory && m.mem != nil {
```

Replace with:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.scr == scrProfile && m.prof != nil {
		switch msg := msg.(type) {
		case SnapshotMsg: // the library keeps tracking snapshots below
		case profile.BackMsg:
			m.scr, m.prof = scrLibrary, nil
			return m, nil
		case profile.ExportMsg:
			return m.startExport(msg.Slug)
		case profile.MemoryMsg:
			return m, m.openMemory(msg.Slug, msg.Name)
		case memOpenMsg: // handled below: the memory browser replaces the profile
		default:
			if ws, ok := msg.(tea.WindowSizeMsg); ok {
				m.width, m.height = ws.Width, ws.Height
			}
			pm, cmd := m.prof.Update(msg)
			m.prof = &pm
			return m, cmd
		}
	}
	if m.scr == scrMemory && m.mem != nil {
```

Find:

```go
	case memDeletedMsg:
		m.status = fmt.Sprintf("deleted %s and its memory (%s)", msg.name, msg.del)
```

Replace with (the new cases stay inside the `switch msg := msg.(type)`):

```go
	case memDeletedMsg:
		m.status = fmt.Sprintf("deleted %s and its memory (%s)", msg.name, msg.del)
	case profileOpenMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		pm := profile.New(msg.deps)
		pm, _ = pm.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		m.prof, m.scr = &pm, scrProfile
		return m, pm.Init()
	case trustOpenMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.trust, m.scr, m.status = msg.st, scrTrust, ""
		m.trust.pg.fit(len(m.trustLines()), m.bodyHeight())
		return m, nil
	case trustDoneMsg:
		m.scr, m.trust = scrLibrary, nil
		if msg.trusted {
			m.status = "trusted " + msg.name + "; any change to its file revokes it"
		} else {
			m.status = "untrusted " + msg.name
		}
		return m, nil
	case importLoadedMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.imp, m.scr, m.status = msg.st, scrImportPreview, ""
		m.imp.pg.fit(len(m.importLines()), m.bodyHeight())
		return m, nil
	case importedMsg:
		m.scr, m.imp = scrLibrary, nil
		m.status = fmt.Sprintf("imported %s as %s", msg.name, msg.slug)
		if n := msg.res.Added + msg.res.Updated; n > 0 || len(msg.res.Rejected) > 0 {
			m.status += fmt.Sprintf(" · memory: %d imported, %d skipped (they wait for your approval)", n, len(msg.res.Rejected))
		}
		if msg.memErr != nil {
			m.status += " · memory import failed: " + msg.memErr.Error()
		}
		if m.target != "" {
			m.selected[msg.slug] = true
		}
		return m, m.reload()
	case exportedMsg:
		m.scr, m.exp = scrProfile, nil
		if m.prof == nil {
			m.scr = scrLibrary
		}
		m.status = fmt.Sprintf("exported to %s (%d memory entries)", msg.path, msg.entries)
		return m, nil
	case exportSecretMsg:
		if m.exp != nil {
			m.exp.secrets = msg.where
		}
		return m, nil
```

Find:

```go
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case SnapshotMsg:
```

Replace with:

```go
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.trust != nil {
			m.trust.pg.fit(len(m.trustLines()), m.bodyHeight())
		}
		if m.imp != nil {
			m.imp.pg.fit(len(m.importLines()), m.bodyHeight())
		}
	case SnapshotMsg:
```

Find (in the `SnapshotMsg` case):

```go
		m.cursor = max(0, min(m.cursor, len(m.snap.Library)-1))
```

Replace with:

```go
		m.cursor = max(0, min(m.cursor, len(m.library())-1))
```

Find:

```go
func (m Model) current() (view.LibraryDot, bool) {
	if m.cursor >= 0 && m.cursor < len(m.snap.Library) {
		return m.snap.Library[m.cursor], true
	}
```

Replace with:

```go
func (m Model) current() (view.LibraryDot, bool) {
	if lib := m.library(); m.cursor >= 0 && m.cursor < len(lib) {
		return lib[m.cursor], true
	}
```

Find:

```go
		m.cursor = min(m.cursor+1, max(0, len(m.snap.Library)-1))
```

Replace with:

```go
		m.cursor = min(m.cursor+1, max(0, len(m.library())-1))
```

Find (in `apply`):

```go
	for _, d := range m.snap.Library {
		switch {
```

Replace with:

```go
	for _, d := range m.library() {
		switch {
```

The `View` rows are replaced below. Then run `grep -n 'm.snap.Library' internal/ui/manage/model.go`; expected after the `View` edit: no output (`share.go`'s `startExport` reads `m.snap.Library` on purpose, to find a name).

Find (in the `tea.KeyMsg` screen switch; Phase 4 added the providers screen):

```go
		case scrConfirmMemory:
			return m.keysConfirmMemory(msg)
		case scrProviders:
			return m.providersUpdate(msg)
		}
		return m.keysLibrary(msg)
```

Replace with:

```go
		case scrConfirmMemory:
			return m.keysConfirmMemory(msg)
		case scrProviders:
			return m.providersUpdate(msg)
		case scrImportSource:
			return m.keysImportSource(msg)
		case scrImportPreview:
			return m.keysImportPreview(msg)
		case scrImportRename:
			return m.keysImportRename(msg)
		case scrExport:
			return m.keysExport(msg)
		case scrTrust:
			return m.keysTrust(msg)
		}
		return m.keysLibrary(msg)
```

In `keysLibrary`, find (the `space` case's toggle):

```go
		if d, ok := m.current(); ok {
			m.selected[d.Slug] = !m.selected[d.Slug]
		}
	case "enter":
```

Replace with:

```go
		if d, ok := m.current(); ok {
			if d.Origin == dots.OriginProject && !d.Trusted && !m.selected[d.Slug] {
				m.status = "⚠ " + d.Name + " is an untrusted project dot: press T to review it"
				return m, nil
			}
			m.selected[d.Slug] = !m.selected[d.Slug]
		}
	case "enter":
```

Find:

```go
	case "e":
		if d, ok := m.current(); ok {
			full, err := dots.Load(m.d.DotsDir, d.Slug)
```

Replace with:

```go
	case "i":
		if m.d.History == nil {
			m.status = "profiles are not available here"
			return m, nil
		}
		if d, ok := m.current(); ok {
			return m, m.openProfile(d)
		}
	case "I":
		m.scr, m.input, m.status = scrImportSource, newPrompt("~/security.dotpack.toml or https://…", ""), ""
		return m, textinput.Blink
	case "T":
		if d, ok := m.current(); ok {
			if d.Origin != dots.OriginProject {
				m.status = d.Name + " is your own dot; only project dots need trust"
				return m, nil
			}
			if m.target == "" {
				m.status = "focus an agent pane in that repository to trust its dots"
				return m, nil
			}
			return m, m.openTrust(d)
		}
	case "e":
		if d, ok := m.current(); ok {
			if d.Origin == dots.OriginProject {
				m.status = "project dots are edited in their repository: " + d.Path
				return m, nil
			}
			full, err := dots.Load(m.d.DotsDir, d.Slug)
```

Find:

```go
	case "D":
		if d, ok := m.current(); ok {
			m.scr, m.delSlug, m.delName = scrConfirm, d.Slug, d.Name
		}
```

Replace with:

```go
	case "D":
		if d, ok := m.current(); ok {
			if d.Origin == dots.OriginProject {
				m.status = "project dots live in their repository; press T to untrust this one"
				return m, nil
			}
			m.scr, m.delSlug, m.delName = scrConfirm, d.Slug, d.Name
		}
```

In `View`, find:

```go
	case scrMemory:
		if m.mem != nil {
			return m.mem.View()
		}
	case scrProviders:
		return m.providersView()
	}
```

Replace with:

```go
	case scrMemory:
		if m.mem != nil {
			return m.mem.View()
		}
	case scrProviders:
		return m.providersView()
	case scrProfile:
		if m.prof != nil {
			v := m.prof.View()
			if m.status != "" {
				v += "\n" + m.status
			}
			return v
		}
	case scrImportSource, scrImportPreview, scrImportRename:
		return m.importView()
	case scrExport:
		return m.exportView()
	case scrTrust:
		return m.trustView()
	}
```

Find (the library rows):

```go
	if len(m.snap.Library) == 0 {
		b.WriteString(style.Dim.Render("No dots yet. Press t to start from a template or n for a new dot.") + "\n")
	}
	for i, d := range m.snap.Library {
		check := "[ ]"
		if m.selected[d.Slug] {
			check = "[x]"
		}
		line := fmt.Sprintf("%s %s %-14s %-22s %s", check, style.Glyph(d.Glyph, d.Color), d.Name, d.Backend+"/"+d.Model, d.Mode)
```

Replace with:

```go
	lib := m.library()
	if len(lib) == 0 {
		b.WriteString(style.Dim.Render("No dots yet. Press t to start from a template, n for a new dot or I to import one.") + "\n")
	}
	for i, d := range lib {
		check := "[ ]"
		if m.selected[d.Slug] {
			check = "[x]"
		}
		line := fmt.Sprintf("%s %s %-14s %-22s %s%s", check, style.Glyph(d.Glyph, d.Color), d.Name, d.Backend+"/"+d.Model, d.Mode, badge(d))
```

Find:

```go
	help := "space toggle · enter apply · n new · t template · e edit · D delete · K providers · esc close"
```

Replace with:

```go
	help := "space toggle · enter apply · n new · t template · e edit · D delete · i profile · I import · T trust · K providers · esc close"
```

Find:

```go
func (m Model) View() string {
```

Replace with:

```go
// badge marks where a library dot comes from.
func badge(d view.LibraryDot) string {
	switch d.Origin {
	case dots.OriginProject:
		switch {
		case !d.Trusted:
			return "  ⚠ project · untrusted"
		case len(d.Clamped) > 0:
			return "  project · " + strings.Join(d.Clamped, ", ") + " off"
		}
		return "  project"
	case dots.OriginTemplate:
		return "  template-derived"
	case dots.OriginImport:
		return "  imported"
	}
	return ""
}

func (m Model) View() string {
```

- [ ] **Step 5: Wire the popup's new dependencies**

In `cmd/herdr-dots/ui.go`, add `"net/http"` and `"herdr-dots/internal/dotpack"` to the imports. In `runManage`, find (Phase 4 and 5 fields follow these lines; keep them):

```go
	prices := set.Prices()
	prog := tea.NewProgram(manage.New(resolveTarget(p.DotsPanesFile()), manage.Deps{Caller: cl, DotsDir: p.DotsDir(), Check: check, NewDot: newDot,
		Memory: memoryStoreFor(p, cfg, nil), Project: paneProject,
```

Replace with:

```go
	prices := set.Prices()
	client := &http.Client{Timeout: 20 * time.Second}
	home, _ := os.UserHomeDir()
	prog := tea.NewProgram(manage.New(resolveTarget(p.DotsPanesFile()), manage.Deps{Caller: cl, DotsDir: p.DotsDir(), Check: check, NewDot: newDot,
		Memory: memoryStoreFor(p, cfg, nil), Project: paneProject,
		History: historyStoreFor(p), SessionKey: paths.SessionKey(p.SocketPath), ExportDir: home,
		Fetch: func(ctx context.Context, url string) ([]byte, string, error) {
			return dotpack.Fetch(ctx, client, url, dotpack.MaxBytes)
		},
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `GOTOOLCHAIN=local go test -race ./internal/ui/manage ./cmd/herdr-dots`
Expected: `ok` for both; the Phase 1–8 manage tests still pass (their snapshots have no `PaneProjects`, so the library is unchanged for them).

- [ ] **Step 7: Full checks**

Run the common verification block. Expected: as stated there.

- [ ] **Step 8: Live check in a scratch Herdr session (optional but recommended)**

Only with `herdr --session dots-test`; snapshot `~/.claude/settings.json` first (`cp ~/.claude/settings.json "${TMPDIR:-/tmp}/settings.before.json"`). In a scratch git repository with one commit and `.herdr-dots/dots/helper.toml` (the `repoHelperTOML` body from Task 9's test), open a Claude Code pane, open the library (prefix+d): Helper shows `⚠ project · untrusted`; space refuses; `T` shows the file, `y` refuses until the end is on screen; trust without capabilities; Helper shows `project · inject, mcp off`. Edit the file in a shell: within a few seconds the dot pauses and a toast appears. `I` with a pack exported by Task 10's CLI shows the full preview. Never type slash commands into the pane. Afterwards `diff "${TMPDIR:-/tmp}/settings.before.json" ~/.claude/settings.json` prints nothing. Record what you saw in the task report; Task 12 copies it into the notes.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/manage cmd/herdr-dots/ui.go
git commit -m "feat(ui): profile, import and export screens, trust preview and origin badges in the manage popup (Phase 9)"
```

---

### Task 12: README, notes, version 0.9.0

**Depends on:** all previous tasks, and Phase 8's Task 16 (version 0.8.0 and its README edits) merged into the base. Wave 5.

**Files:**
- Modify: `internal/version/version.go`, `herdr-plugin.toml`, `cmd/herdr-dots/memwire_test.go` (the version pin), `README.md`
- Create: `docs/superpowers/notes/2026-10-08-phase-9-verification.md`

**Interfaces:**
- Consumes: Task 1's V1 numbers and Task 11's live-check notes (from their reports); the post-Phase-8 README (Phase 8 Task 16's status block, `### Proactive dots (0.8.0)` section, `## Files` lines, privacy and known-gaps bullets, and roadmap rows `| 8 |` and `| 9–10 |`).
- Produces: version `0.9.0`; user docs for profiles, packs and project dots; V1–V3 recorded. Phase 2 stays **VITAL**; the README stays "scaffold … not done".

- [ ] **Step 1: Version 0.9.0 (test first)**

In `cmd/herdr-dots/memwire_test.go`, find (Phase 8 Task 16's pin):

```go
func TestVersionIsPhaseEight(t *testing.T) {
	if version.Version != "0.8.0" {
		t.Fatalf("version %s, want 0.8.0", version.Version)
	}
}
```

Replace with:

```go
func TestVersionIsPhaseNine(t *testing.T) {
	if version.Version != "0.9.0" {
		t.Fatalf("version %s, want 0.9.0", version.Version)
	}
}
```

Run: `GOTOOLCHAIN=local go test -run 'TestVersionIsPhaseNine' ./cmd/herdr-dots/`
Expected: FAIL with `version 0.8.0, want 0.9.0`.

In `internal/version/version.go`, find:

```go
const Version = "0.8.0"
```

Replace with:

```go
const Version = "0.9.0"
```

In `herdr-plugin.toml`, find (the plugin's own version; `min_herdr_version = "0.8.0"` on the next line is Herdr's version and stays):

```toml
name = "Herdr Dots"
version = "0.8.0"
```

Replace with (Phase 6's manifest test compares `version` with `version.Version` and needs no change):

```toml
name = "Herdr Dots"
version = "0.9.0"
```

Run: `GOTOOLCHAIN=local go test ./cmd/herdr-dots/`
Expected: `ok  	herdr-dots/cmd/herdr-dots`.

- [ ] **Step 2: README**

Six edits against the post-Phase-8 README. Each anchors on a heading or a whole line, not a line number.

(a) The status block. Replace the whole quoted block that starts with `> **Status: 0.8.0, in development (scaffold, not done).**` (it ends at the first blank line) with:

```markdown
> **Status: 0.9.0, in development (scaffold, not done).** It is not finished and not ready for
> general use. New in 0.9.0: per-dot profiles with run history and tuning hints, dot packs to export
> and import dots (risky capabilities start off), and team dots shipped in a repository behind a
> content-hash trust prompt; this is built and unit-tested but has **not been run end to end inside
> Herdr** by the developer yet. Proactive dots (0.8.0) and write-capable dots (0.7.0) have not been
> run end to end inside Herdr yet either, and Windows support (0.6.0) has not been run on Windows
> yet. Phase 2 (dot memory) stays the VITAL part. See the [Roadmap](#roadmap) for what's missing.
```

(b) Add this section at the end of "Using it": after the `### Proactive dots (0.8.0)` section and immediately before `## Files`:

````markdown
### Profiles, history and sharing (0.9.0)

Every dot run (reviews, summons, council members, MCP asks, research, scheduled and commit runs,
drafts and memory passes) and every card outcome (sent, edited, dismissed, applied, discarded,
injected, orphaned) is kept per dot in the plugin state dir under `history/<slug>.jsonl`, across
sessions and projects, capped at 20,000 lines. History holds only 300-character excerpts, with
anything that looks like a key redacted, and never leaves this machine.

Press `i` on a dot in the library for its **profile**: 7- and 30-day stats (`tab` switches) with
runs by kind, how often it flags, how often you accept its advice, suppressed repeats, cost and
median latency; a memory summary; plain tuning hints ("Mostly dismissed", "Rarely speaks", "Your
most expensive dot", "Keeps repeating dismissed advice", "Fails often"); and its recent history
(`enter` shows the full message).

**Share a dot** as one `.dotpack.toml` file: `x` in the profile, or

```sh
herdr-dots dots export security --with-memory dot -o security.dotpack.toml
herdr-dots dots import security.dotpack.toml            # or an https:// URL
```

Packs never contain credentials, history, global memory or preferences, and an export stops if
anything looks like a secret (`--force` overrides). An import shows everything first (purpose,
research topics, schedule task, backend, every memory entry) and never overwrites a dot (`--as
<name>` or `n` to rename). Imported dots start with **inject, write, MCP, research, web, schedules,
commit triggers and custom `base_url`s turned off**; keep one with `--keep inject` (CLI) or its
number in the preview. Absolute `schedule.projects` paths are always removed. Imported memory is
labelled "imported" in prompts, never stops a dot from repeating advice, and every imported entry
waits for your approval (`herdr-dots memory approve`, or `a` in the memory browser).

**Team dots in a repository:** put dot files in `.herdr-dots/dots/*.toml` at the repository root.
Everyone working in that repository sees them in the library as `project` dots, but nobody can
attach one until they **trust** it: `T` in the library (or `herdr-dots dots trust <file>`) shows the
whole file and the capabilities it asks for, each off until you turn it on. Trust covers the exact
file content in this repository and its clones; any change makes it untrusted again and pauses it
where it's attached, and an untrusted or changed project dot never runs, in the background either.
`project_dots_enabled = false` in `config.toml` turns project dots off.
````

(c) Under `## Files`, insert these lines right after the line that starts `` - `STATE_DIR/memory/<project>/schedule.json` `` (Phase 8's last line there):

```markdown
- `STATE_DIR/history/<slug>.jsonl`: per-dot run and card history, shared by every session (`0600`, capped at 20,000 lines).
- Trusted project dot files: `$(herdr plugin config-dir herdr-dots)/trusted.json`
- Team dots: `.herdr-dots/dots/*.toml` in a repository (used only after you trust each file)
```

(d) At the end of `## Privacy and safety` (after its last bullet, "Commit reviews never install git hooks; …", and before `## Known gaps in this scaffold`), add:

```markdown
- Shared dots are someone else's prompts: imported and project dots start with every risky
  capability off, keys this version doesn't understand are refused, and project dot trust is tied
  to the file's exact content.
- Dot packs never carry credentials or history, exported memory is secret-scanned, and history
  stays on this machine with keys redacted.
```

(e) At the end of `## Known gaps in this scaffold` (after its last bullet, Phase 8's "Proactive dots (0.8.0) are unit-tested …", and before `## Roadmap`), add:

```markdown
- Profiles, dot packs and project dots (0.9.0) are unit-tested on macOS only; the manage popup flows
  and a project dot's trust and pause have not been run end to end inside Herdr (checklist in
  `docs/superpowers/notes/2026-10-08-phase-9-verification.md`). A repository can ship a dot with the
  same display name as one of yours: the `proj:` slug and the trust preview tell them apart.
```

(f) In the roadmap table of `README.md`, find Phase 8 Task 16's combined row:

```markdown
| 9–10 | Profiles and sharing, then hardening and release. The project is feature complete only after Phase 10. |
```

Replace with:

```markdown
| 9 | Profiles, history and sharing (0.9.0): per-dot run history and profiles with tuning hints, dot packs (export and import, risky capabilities off), team dots in a repository behind content-hash trust, seven more templates. Built and unit-tested; **not run end to end in Herdr yet** |
| 10 | Hardening and release. The project is feature complete only after Phase 10. |
```

Keep row 2 exactly as it is (**Dot memory — VITAL.**). If Task 11's live check ran and passed, change "has **not been run end to end inside Herdr** by the developer yet" in (a) (for 0.9.0 only), "have not been run end to end inside Herdr" in (e) and "**not run end to end in Herdr yet**" in roadmap row 9 to say it was checked once in an isolated Herdr session on macOS; otherwise leave them.

The roadmap spec (`docs/superpowers/specs/2026-10-08-herdr-dots-roadmap.md`) is not edited: its phase table has no status column, and Phases 2–8 left it unchanged.

- [ ] **Step 3: Write the verification notes**

Create `docs/superpowers/notes/2026-10-08-phase-9-verification.md`, filling the two benchmark numbers from Task 1's report and the live-check lines from Task 11's report (write "not run" if it wasn't):

```markdown
# Phase 9 (profiles, history and sharing) verification notes

- **Date:** 2026-10-08
- **Machine:** macOS (darwin/arm64). Windows and Linux are cross-compiled and unit-tested only.

## V1: history size and profile speed

- `BenchmarkStats30d` (one 20,000-line file read once for both windows and 50 rows): <ns/op from Task 1> (planning run: 35 ms on an Apple M4 Pro).
- `BenchmarkCostShareTenDots` (ten full files): <ns/op from Task 1> (planning run: 370 ms), so the profile renders first and adds the cost hint when the share arrives.
- **Answer:** the 20,000-line cap (trimmed to 15,000) stays.

## V2: project dots outside git repositories

- **Answer:** no. Project identity is the git root commit (decision 11); path identities (no git, no commits, shallow clones) get no project dots.

## V3: committed project memory

- **Answer:** out of scope (decision O4 a). Recorded as a question for the user: should a repository be able to ship read-only project memory (for example `.herdr-dots/memory.toml`)?

## Live check (Task 11 Step 8)

- <what was seen, or "not run">
- `~/.claude/settings.json` unchanged: <yes/no>

## Phase 9 result

- Version 0.9.0. Not done: Phase 10 (hardening and release) remains, and the product is feature complete only after its acceptance checklist.
```

- [ ] **Step 4: Check the wording**

Run:

```bash
grep -n 'VITAL' README.md
grep -n 'not been run end to end\|not run end to end' README.md
grep -nE 'is (done|finished|complete|ready)|feature complete|production[- ]ready' README.md docs/superpowers/notes/2026-10-08-phase-9-verification.md | grep -v 'only when\|until\|not \|only after\|Phase 10\|"feature complete"'
grep -c 'dotpack' README.md
```

Expected: at least one `VITAL` line (Phase 2); at least four "not … run end to end" lines (unless Task 11's live check passed); the third command prints nothing; the last prints a number above 0. (This is Phase 8's wording check: an older `\b(done|ready)\b` grep also flags lines the README already has, such as "Herdr reports `done`" and "Patch ready".)

- [ ] **Step 5: Full checks**

Run the common verification block, then `sh scripts/build.sh && ./bin/herdr-dots version`. Expected: as stated there; the last prints `herdr-dots 0.9.0 (scaffold)`.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/superpowers/notes/2026-10-08-phase-9-verification.md internal/version/version.go herdr-plugin.toml cmd/herdr-dots/memwire_test.go
git commit -m "docs: profiles, history and sharing in the README; version 0.9.0, still not done (Phase 9)"
```

---

## Self-review

**1. Spec coverage.**
- §1 success criteria: 1 (every run kind and resolution recorded, persistent, capped) Tasks 1, 7; 2 (profile screen with 7/30-day stats, memory summary, history, hints) Tasks 8, 11; 3 (export/import round trip, preview, clamps with per-item keeps) Tasks 6, 10, 11; 4 (project dots marked, not attachable until trusted, trust revoked on change) Tasks 2, 5, 9, 11; 5 (no credentials; memory secret-scanned) Task 6 (`Export` scans the dot and memory), Task 4 (`ImportPack` refuses secrets); 6 (at least 7 new templates) Task 3.
- §3 decisions: S1 Task 1 (path, append-only, lock, 20,000/15,000) with Plan decisions 8, 12; S2 Task 1 (`Stats`, Plan decision 10); S3 Task 8 (`Hints`, table tests); S4 Task 6; S5 Task 2 (`ClampDot`, extended by Plan decision 4) used by Tasks 5, 6, 9, 10, 11; S6 Task 6 (`Fetch`); S7 Tasks 2, 5, 9 (slug format changed, Plan decision 1); S8 Task 5 (key changed, Plan decision 2); S9 Task 6 (`SelectMemory`), Tasks 10, 11; S10 Task 4 (amended by Plan decision 26: dot-scope imports are proposals too).
- §4.2 interface table: `dots.Dot.Origin/SourcePath`, `LoadProject`, `Validate` (Task 2); `engine.Deps` (Tasks 7, 9; `LoadProjectDots` replaced by `Trust` + `ResolveRoot`, Plan decision 13); engine history hooks, `ReloadDots` merge, `Attach` refusing untrusted (Tasks 7, 9); `view.LibraryDot` (Tasks 8, 9); `ipc.CmdTrust`/`CmdUntrust` (Task 9, args extended, Plan decision 16); `memory.Store.ImportPack` (Task 4, takes the dot slug); `main.dispatch` `dots` (Task 10); `paths.HistoryDir/TrustFile` (Task 1).
- §5 data model: history lines (Task 1, card key per Plan decision 8), pack (Task 6), trust file (Task 5, entry keyed per Plan decision 2).
- §6 data flow: profile (Tasks 8, 11), hints (Task 8), export/import (Tasks 6, 10, 11), project dots 1–5 (Task 9; scanning a fresh pane via `reload_dots {pane_id}`).
- §7 UI: profile screen (Task 8), library badges, `⚠ untrusted`, `T`, `I`, `x` (Task 11).
- §8.1 errors: corrupt history line skipped and counted (Task 1 test, profile footnote in Task 8); unknown pack format refused (Task 6); URL failures (Task 6); secret in export (Tasks 6, 10, 11); slug collision (Tasks 10, 11). §8.2 safety and §8.3 test table: every row has a test in Tasks 1, 2, 5, 6, 7, 8, 9, 11.
- §9 V1–V3: Task 1 Step 6 and Task 12 notes (Plan decisions 3, 11, 23). §10 out of scope respected. §11 open decisions: answered by rulings 3 and 17 and Plan decision 23.

**2. Placeholder scan.** No "TBD", "TODO" or "similar to Task N". Task 12's notes have `<…>` slots for numbers and observations the executor records from Task 1's benchmark and Task 11's live check; they are recording steps, not missing design. Every Phase 7 and Phase 8 anchor (Task 7 Step 6, Task 9's `background.go` and `ask_council.go` edits) was replayed against the merged code at `c4d3827`; no hedged "if Phase N …" instruction is left.

**3. Type consistency.** `history.Record` fields, `history.CardKey`, `Store.Profile`/`Stats`/`CostShare` are spelled the same in Tasks 1, 7, 8 and 11. `dots.ClampDot(d, keep) (Dot, []Clamp)`, `Requested`, `CheckCapabilities`, `ProjectSlug`, `SplitProjectSlug`, `DisplaySlug`, `DecodeProject(f ProjectFile, projectID string)` match their uses in Tasks 5, 6, 9, 10, 11. `projectdots.ScanProject(root, projectID string, ts *TrustStore) Scan` and `TrustStore.Trust(project, rel, path string, sum [32]byte, caps []string)` match Tasks 9, 10, 11. `engine.trace(paneID string, d dots.Dot, kind, project string)` is the signature `runDot`, `runDraft` and `runBackground` call. `ipc.TrustArgs{PaneID, Slug, SHA256, Capabilities}` is what `Engine.Handle`, `Engine.Trust` and the popup's trust preview use. `memory.Store.ImportPack(p, dot string, entries []memory.Entry)` matches Tasks 6 (tests), 7, 10 and 11.

**4. Review Focus.** Each of the five items names its tests and owning tasks (2, 4, 5, 6, 7, 9, 10, 11). Inputs found while planning got tests too: an inject card resolved before its run line (Task 1), a same-size, same-second edit (Task 5), a symlinked `.herdr-dots` directory (Task 2), a file changed between the trust preview and `y` (Task 9), a pack with a newer capability key (Tasks 2, 6), a malformed config (Task 9), and a popup too short for the preview (Task 11).

**5. Reconcile pass (2026-10-09).** Every task was replayed in wave order on `c4d3827` (Phases 2–8 merged, 0.7.0) with a script that applies each create block and Find/Replace pair verbatim; Task 12 ran on a simulation of Phase 8 Task 16. After each wave: `gofmt -l internal cmd` clean, `go vet ./...` clean, `go test -race ./...` green on darwin, the Linux build, the Windows build and vet, and `GOOS=windows go test -c` for the wave's packages (44 Windows test binaries for `./...` at the end). The Phase 9 engine tests passed with `-race -count=5`; `BenchmarkStats30d` measured 36 ms/op and `BenchmarkCostShareTenDots` 359 ms/op. Plan decisions 24–35 record what changed.

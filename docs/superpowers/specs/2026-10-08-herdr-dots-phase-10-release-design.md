# Herdr Dots Phase 10: Rename, Hardening and Marketplace Release (1.0), Design Spec

**Status: DRAFT, pending user review**

- **Date:** 2026-10-08
- **Phase:** 10 of 10, the **final phase** (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first.
- **Builds on:** everything in Phases 1–9
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-10-release-outline.md`
- **This is the only phase that can make the product feature complete,** and only when every item on the roadmap's feature-complete checklist passes the acceptance run in §6.5. Ships as `1.0.0`.

---

## 1. Intent

### What the user wants

Publish the plugin on the Herdr marketplace under a name that doesn't collide with OpenAI's "Dots", installable by people who don't have Go, upgradeable without losing dots, memory or history, and hardened enough that strangers can rely on it. Versions so far have been `0.x` scaffolds. 1.0 is the first release that claims feature complete.

### Success criteria

1. A new product name and plugin id are chosen by the user (proposal in §3). Existing installs migrate automatically: dots, settings, credentials, memory, history, trust, keybindings and MCP registrations, with nothing lost.
2. `herdr plugin install <owner>/<repo>` works on macOS (arm64, amd64), Linux (amd64, arm64) and Windows (amd64, arm64) **without Go installed**, using checksum-verified prebuilt binaries, with a `go build` fallback.
3. The repo is listed on herdr.dev/plugins (GitHub topic `herdr-plugin`) with a README, demo, license, security policy, changelog and user guide.
4. Every persisted file has a schema version and a migration path. Fuzz tests cover every parser. A written threat model has been reviewed. An 8-hour soak test passes the resource budgets.
5. The feature-complete acceptance run (§6.5) passes on all three OSes, and only then is `1.0.0` tagged and the word "feature complete" used anywhere.

---

## 2. Research summary (looked up 2026-10-08)

- **Herdr marketplace** (herdr.dev/docs/marketplace): an automatic index of public GitHub repos tagged with the topic **`herdr-plugin`**, refreshed every 30 minutes. It isn't a reviewed catalog. Install with `herdr plugin install owner/repo[/subdir]`. The manifest is at the repo root or in a subdirectory.
- **Name collision:** OpenAI announced "Dots" at DevDay 2026 for ChatGPT. Publishing "Herdr Dots" invites confusion and trademark risk, as the v1 spec already noted.
- **Herdr plugin dirs** are keyed by plugin id (`herdr plugin config-dir <id>`), so a rename gives new, empty directories. Migration has to copy from the old id's directories (V1).
- **The v1 `[[build]]` step** runs `go build`, which requires Go ≥ 1.25 on the user's machine. That's fine for developers, a barrier for marketplace users.

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| F1 | Name (proposal; the user decides, O1) | **Herdr Outriders** (`herdr-outriders`): outriders flank a herd, guard it and scout ahead, matching both reviewing (guarding) and proactive research (scouting), and they fit Herdr's herding brand. Individual agents are "outriders". Alternatives in O1 | Keeping "Dots" |
| F2 | Delivery header after the rename | `[◆ Security · outrider]`. `transcript.FromDots` keeps recognizing the legacy `[◆ Security dot]` form forever, so old transcripts don't break the loop guard | Changing the parser to new-only |
| F3 | Migration | On first run, the new binary looks for the old plugin's config and state dirs (V1); if found and the new dirs are empty, it copies (never moves) everything except per-session runtime files, preserves permissions/ACLs, writes `MIGRATED_FROM` markers, and shows a one-time notice. `migrate --from herdr-dots` re-runs it manually | Asking users to re-create dots |
| F4 | Keybindings and MCP | `setup-keys --migrate` rewrites `herdr-dots.*` command references in the user's `config.toml` (backup + confirm). `setup-mcp --migrate` removes the `herdr-dots` MCP server entries and installs `herdr-outriders` | Leaving stale bindings |
| F5 | Binaries | GoReleaser builds 6 targets on tag; `SHA256SUMS` is committed into the repo at the release tag (`release/SHA256SUMS`) and also attached to the GitHub release. The `[[build]]` script downloads the asset for `version + GOOS/GOARCH` and verifies it against the **committed** checksums (trust anchored in the repo the user installed). Fallback `go build` when Go ≥ 1.25 exists; otherwise a clear error | Downloading unverified binaries; requiring Go |
| F6 | Signing | Cosign keyless signatures on release assets (GitHub OIDC), verified optionally (`verify-release`) | Mandatory signature verification in the build step (needs cosign on user machines) |
| F7 | Schema versions | Every state and config file gets `version` (v1's `attachments.json`, `queue.json`, `usage.json`, the feed, `config.toml` and dot TOML gain it, with a missing value meaning 1). `internal/migrate` runs ordered, idempotent migrations at engine start with a backup of each file it rewrites | Ad-hoc field defaults only |
| F8 | Fuzzing | Native Go fuzz tests for `ParseVerdict`/`ParseReply`, `ParseMemoryOps`, `transcript.ParseClaude`, `ParseCodex`, `FromDots`, the memory file loader, `dotpack.Parse`, `schedule.Parse`, IPC request decoding, `keys.Existing`; a 10-minute fuzz run per target in the release CI job | Unit tests only |
| F9 | Resource budgets | Idle engine CPU < 1% averaged over 10 minutes; RSS < 80 MB with 12 dots × 4 panes; snapshot broadcasts throttled to ≤ 10/s and ≤ 256 KB each (feed and history trimmed in snapshots) | Unmeasured |
| F10 | Telemetry | None. Stated in the README and the privacy section | Opt-in analytics |
| F11 | Uninstall | `uninstall-extras` removes MCP entries and the keybinding block (with backups); `--purge` also deletes config and state dirs after typed confirmation | Leaving entries in client configs |
| F12 | Docs | `README.md` (overview, install, quick start, privacy, safety), `docs/guide/` (dots, delivery modes, memory, council, backends, MCP, Windows, write-capable, proactive, profiles and sharing), `SECURITY.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `LICENSE` | README only |

---

## 4. Architecture

New or changed pieces:

- **`internal/migrate`** (new): `Migration{From, To int; File string; Apply func([]byte) ([]byte, error)}` registry; `Run(paths, logf) (Report, error)`; a backup per rewritten file (`<file>.bak-<version>`).
- **`internal/rename`** (new): legacy-dir discovery (`herdr plugin config-dir herdr-dots`, falling back to path rewriting of the current dirs by id), copy with permissions/ACLs, markers.
- **`cmd/<binary>/migrate.go`**, **`uninstall.go`**, **`verifyrelease.go`**.
- **`scripts/build.sh`**, **`scripts/build.ps1`**: download → verify → install, else `go build`.
- **`.goreleaser.yaml`**, **`.github/workflows/release.yml`** (a release candidate tag builds the assets and commits `release/SHA256SUMS`; the final tag is placed on that commit and only uploads, signs and publishes. V3 confirms this ordering works with Herdr's install flow).
- **`docs/security/threat-model.md`** and **`docs/guide/*`**.

### 4.1 Changes to earlier interfaces (exact)

| Symbol | Change |
|---|---|
| Go module path | `herdr-dots` → `herdr-outriders` (or the chosen name); all imports rewritten |
| Binary | `bin/herdr-dots` → `bin/herdr-outriders` (`.exe` on Windows) |
| Manifest `id` | `herdr-dots` → `herdr-outriders`; action ids follow (`herdr-outriders.manage`, …) |
| `transcript.DotPrefix` | Renders `[<glyph> <Name> · outrider]`; `FromDots` parses both forms; `CouncilName` header `[⊕ Council · outrider]` |
| `keys` | `Defaults()` use the new action ids; `Migrate(data []byte, oldID, newID string) ([]byte, int)` |
| `mcpinstall` | Server name `herdr-outriders`; `PlanMigrate(oldName, newName string) (Plan, error)` |
| Every persisted struct | Gains `Version int` with JSON/TOML key `version` (missing = 1) |
| `paths` | `LegacyDirs(oldID string) (config, state string, ok bool)` |
| `version.Version` | `1.0.0` |
| `ui/style` | Honors `NO_COLOR` (no colors; glyphs kept) |

---

## 5. Data model

No new user-facing data. Version fields everywhere (F7). Migration markers: `CONFIG_DIR/MIGRATED_FROM` and `STATE_DIR/MIGRATED_FROM` (`{"from":"herdr-dots","at":"…","files":123}`).

---

## 6. Data flow

### 6.1 Install (marketplace user without Go)

`herdr plugin install owner/herdr-outriders` → Herdr runs `[[build]]` → `scripts/build.sh` reads `internal/version/VERSION` and `release/SHA256SUMS` → downloads `herdr-outriders_<ver>_<os>_<arch>.tar.gz` (or `.zip` on Windows) from the GitHub release → verifies SHA-256 → extracts to `bin/`. Network failure or checksum mismatch → falls back to `go build` if available, else fails with the reason and the manual steps.

### 6.2 Upgrade from `herdr-dots`

First `engine` start under the new id: `rename.FindLegacy("herdr-dots")` → copy (§F3) → `migrate.Run` → one-time toast "Migrated from Herdr Dots: 7 dots, memory for 3 projects. Run `herdr-outriders setup-keys --migrate` to update keybindings." The manage popup shows the same notice until dismissed.

### 6.3 Startup migrations

`migrate.Run` at every engine start: read each file's `version`, apply migrations in order, back up and rewrite atomically, log a report. A file whose version is newer than the binary knows → read-only mode for that file plus a toast "written by a newer version".

### 6.4 Release

Tag `v1.0.0-rc.N` → CI: tests on 3 OSes, a 10-minute fuzz pass per target, GoReleaser build, and a commit of `release/SHA256SUMS` matching those assets. Tag `v1.0.0` on that commit → cosign sign and publish the same assets as the GitHub release. The checksums a user installs are always in the tagged tree (V3 confirms Herdr installs from the tag).

### 6.5 Feature-complete acceptance run

A scripted plus manual checklist, executed on macOS, Linux and Windows (Herdr Windows beta), recorded in `docs/superpowers/notes/1.0-acceptance.md`. It is the roadmap's feature-complete checklist, item by item, each with evidence (command output or screenshot). Any failure blocks the `1.0.0` tag.

---

## 7. UI

- A one-time migration notice (toast plus manage popup banner).
- The `--version` output includes the build source (`release` or `source`) and commit.
- The manage popup footer: "Outriders 1.0.0 · MCP ✓ · keys ✓".

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Behavior |
|---|---|
| Checksum mismatch | Abort the download path; fall back to source build; never run the unverified binary |
| Legacy dirs not found | No migration; normal first run |
| Partial copy failure | Roll back the new dirs (delete what was copied), keep the old dirs untouched, show the error |
| Migration of one file fails | Restore from its backup; engine runs with that file in read-only mode; toast |

### 8.2 Safety

- The threat model covers prompt injection (transcripts, memory, imported packs, project dots, MCP), local privilege boundaries (sockets/pipes, file permissions), supply chain (releases, checksums, signatures, dependencies), secrets (credentials, memory scanner, redaction), write capability (worktree, gates), and cost abuse (budgets, ask limits). Each threat maps to a mitigation and a test.
- A dependency audit: `govulncheck` in CI; pinned module versions; minimal dependencies.
- Run a final security review (the Claude Code `security-review` skill and a human read) on the full code base.

### 8.3 Testing

| Layer | Tests |
|---|---|
| Migration | Fixture dirs from every earlier version (`0.1`–`0.9`) migrate to the current schema; idempotent; newer-version files go read-only |
| Rename | Copy preserves modes (Unix) and DACLs (Windows); rollback on failure; markers; `FromDots` legacy parsing |
| Build script | A fake release server (`httptest`, or a local file URL in CI): verified download, checksum mismatch → fallback, no network + no Go → clear error |
| Fuzz | All F8 targets |
| Soak | 8 h with 12 fake dots (scripted backend) × 4 fake panes; CPU, RSS and snapshot size within F9 |
| Acceptance | §6.5 on three OSes |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | Can a plugin find another plugin id's config and state dirs (`herdr plugin config-dir <old-id>` / `state-dir`), and do they still exist after the old plugin is uninstalled? | F3 |
| V2 | Is the chosen name free: GitHub repo name, Herdr marketplace listing, Homebrew/Scoop names (for future packaging), trademark search? | F1 |
| V3 | Can Herdr's `[[build]]` step access the network during `plugin install`? Can the `SHA256SUMS` file be committed in the same tagged commit (release-candidate flow) instead of after the tag? | F5 |
| V4 | Does `herdr plugin install owner/repo` pick up a manifest whose `[[build]]` has per-platform commands (Phase 6 syntax)? | F5 |

---

## 10. Out of scope

Homebrew, Scoop or apt packaging; a hosted docs website; auto-update inside the plugin (Herdr's own plugin update flow is used); a paid tier.

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Product name | (a) **Herdr Outriders** (`herdr-outriders`); (b) Herdr Kibitz (`herdr-kibitz`: kibitzers watch and comment, which is exactly what dots do; playful; an old `expect` tool shares the name); (c) Herdr Sheepdogs (`herdr-sheepdogs`: herding dogs that watch and alert; friendly, less serious); (d) Herdr Scouts (generic); (e) keep "Herdr Dots" | **(a)** | Distinct from OpenAI's "Dots", fits the Herdr herding theme, and describes both guarding (review) and scouting ahead (proactive research). Video game name overlap ("Outriders", 2021) is in a different trademark class; check in V2 |
| O2 | Rename the per-agent noun | (a) "outrider"; (b) keep "dot" as the generic noun | **(a)** | A half-rename keeps the brand confusion |
| O3 | License | (a) MIT; (b) Apache-2.0 | **(b)** | Explicit patent grant; common for developer tools. MIT is fine if you prefer brevity |
| O4 | Prebuilt binaries | (a) GoReleaser + committed checksums + `go build` fallback; (b) source build only | **(a)** | Marketplace users usually don't have Go 1.25 |
| O5 | Release signing | (a) cosign keyless, optional verification; (b) none; (c) mandatory verification | **(a)** | Supply-chain evidence without forcing a tool on users |

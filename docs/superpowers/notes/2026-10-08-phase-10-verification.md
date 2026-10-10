# Phase 10 (release hardening) verification notes

Phase 10 ships `1.0.0-rc.1`. It builds everything up to a release and stops at
`docs/release/ready-to-publish.md`. Nothing here was pushed, tagged, listed or
published (release-gate amendments).

## V1–V4

| # | Question | Finding | Holds |
|---|---|---|---|
| V1 | Can a plugin find another plugin id's config/state dirs? | Not needed: the name stays "Herdr Dots" (decisions ruling 1); the migration framework (`internal/migrate`) makes a later rename one more migration | n/a |
| V2 | Is the name free (GitHub repo, marketplace, Homebrew/Scoop, trademark)? | Not checked: naming and publishing are the developer's decisions. "Herdr Dots" overlaps OpenAI's "Dots" (spec §2); listed in the ready-to-publish checklist | user gate |
| V3 | Can `[[build]]` reach the network during `herdr plugin install`? Where does Herdr install from? | herdr.dev/docs/plugins (2026-10-08): install clones with `git` (default branch, or `--ref`), shows a preview, runs build steps after confirmation, gives them no runtime env, and documents no network restriction. `plugin link` runs no build. There is no `plugin update`: reinstall | yes |
| V4 | Does install honor per-platform `[[build]]` items? | Yes: item-level `platforms` (Phase 6 decision 2, from Herdr's source) | yes |

## Tools on this machine

Read-only check (`command -v` and `--version` only; nothing installed):

| Tool | Result |
|---|---|
| goreleaser | not installed here |
| cosign | not installed here |
| govulncheck | not installed here |
| actionlint | /opt/homebrew/bin/actionlint |
| shellcheck | /opt/homebrew/bin/shellcheck |
| pwsh | not installed here |
| powershell | not installed here |
| codex | not installed here |
| herdr | ~/.local/bin/herdr (herdr 0.8.2) |
| claude | ~/.local/bin/claude |
| git | git version 2.52.0 |
| go | /opt/homebrew/bin/go (go1.25.7 darwin/arm64) |

GoReleaser is used only as `goreleaser release --snapshot --clean --skip=publish,sign`
when installed; otherwise `scripts/release-local.sh` builds the same archives.

## Baseline at the start of Phase 10

Base commit e9874e1. `GOTOOLCHAIN=local go test -race ./...`: every package `ok`
(packages without tests report `no test files`); `go vet ./...`: `vet-ok`.

```
ok  	herdr-dots/cmd/herdr-dots
ok  	herdr-dots/internal/backend
ok  	herdr-dots/internal/backend/all
ok  	herdr-dots/internal/backend/anthropicapi
ok  	herdr-dots/internal/backend/httpx
ok  	herdr-dots/internal/backend/ollama
ok  	herdr-dots/internal/backend/openaiapi
ok  	herdr-dots/internal/backend/openrouter
ok  	herdr-dots/internal/backend/toolloop
ok  	herdr-dots/internal/config
ok  	herdr-dots/internal/council
ok  	herdr-dots/internal/credentials
ok  	herdr-dots/internal/delivery
ok  	herdr-dots/internal/dotpack
ok  	herdr-dots/internal/dots
ok  	herdr-dots/internal/engine
ok  	herdr-dots/internal/fsutil
ok  	herdr-dots/internal/herdr
ok  	herdr-dots/internal/history
ok  	herdr-dots/internal/ipc
ok  	herdr-dots/internal/keys
ok  	herdr-dots/internal/lockfile
ok  	herdr-dots/internal/mcpinstall
ok  	herdr-dots/internal/mcpserver
ok  	herdr-dots/internal/memory
ok  	herdr-dots/internal/patchstore
ok  	herdr-dots/internal/paths
ok  	herdr-dots/internal/platform
ok  	herdr-dots/internal/pricing
ok  	herdr-dots/internal/projectdots
ok  	herdr-dots/internal/queue
ok  	herdr-dots/internal/schedule
ok  	herdr-dots/internal/secrets
ok  	herdr-dots/internal/state
ok  	herdr-dots/internal/textutil
ok  	herdr-dots/internal/transcript
ok  	herdr-dots/internal/ui/diff
ok  	herdr-dots/internal/ui/manage
ok  	herdr-dots/internal/ui/memui
ok  	herdr-dots/internal/ui/pane
ok  	herdr-dots/internal/ui/profile
ok  	herdr-dots/internal/ui/style
ok  	herdr-dots/internal/ui/summon
ok  	herdr-dots/internal/view
ok  	herdr-dots/internal/worktree
vet-ok
internal/version/version.go:5:const Version = "0.9.0"
herdr-plugin.toml:4:version = "0.9.0"
```

## User gates

Only the developer can do these; Phase 10 prepares each one.

1. Choose a license and add `LICENSE` (ruling 16). No license is chosen.
2. Decide the name for publishing (keep "Herdr Dots" or rename; the migration framework supports a rename).
3. Run the feature-complete acceptance run on Linux and on Windows (Herdr Windows beta) hardware, and on macOS with a real Herdr install.
4. Run the live provider checks: the Claude live tests and the provider part of the macOS smoke, the API backends with your keys, then install Codex and run the Codex live tests (`go test -tags live ./internal/backend/ ./internal/council/`).
5. Create the GitHub repository, push, and let CI run on three OSes.
6. Tag `v1.0.0-rc.N`, merge the checksum PR, set `internal/version/VERSION` to `1.0.0` when the acceptance run passes, tag `v1.0.0`, publish the draft release.
7. Add the `herdr-plugin` topic and check the listing on herdr.dev/plugins.
8. Install from the marketplace without Go on all six targets.

## Phase 10 wrap-up

Phase 10 ends at `1.0.0-rc.1`, a release candidate: not released, not done.
The macOS acceptance evidence is in
[1.0-acceptance.md](1.0-acceptance.md); what the owner still has to do is in
[ready-to-publish.md](../../release/ready-to-publish.md).

- **Soak.** 8-hour soak: running (started 2026-10-09 18:07 on c1245f9);
  result recorded by the controller when it ends. The longest completed soak
  (Task 4, 60 s busy and 30 s idle) passed in 120.12 s: 454 runs, 227 cards,
  117 broadcasts (worst burst 7.1 a second), biggest snapshot 261,793 B
  (budget 262,144 B), peak RSS 33.3 MB, idle CPU 0.443%, goroutines 20 -> 21.
- **Fuzz.** Task 13 run (2026-10-09, c1245f9): `fuzzed 26 targets for 60s
  each`, no crasher (26 targets × 60 s, 27 minutes). Crashers found and fixed in Phase 10 (Task 3), each
  kept as a regression input: `FuzzImportTOML/8a748f204ecc8c6f` (an export
  wrote `ref` for an entry without an id), `FuzzCodexBlock/a763424096dd21b2`,
  `FuzzCodexBlock/816ef7fa033511c9` (a developer's own end-marker line) and
  `FuzzCodexBlock/2d9d465bd56e31b4` (mixed line endings left a blank line
  after `setup-mcp --remove`, a product bug in `internal/mcpinstall/codex.go`).
  Short runs are random: two of those crashers were missed by a 60 s run and
  found by a 1 s or 5 s one; the release workflow's 10 minutes per target is
  the real gate.
- **govulncheck.** Not clean. In the Task 13 run it couldn't reach its
  database (`lookup vuln.go.dev: i/o timeout`, twice). The last result
  (Task 11, go1.25.7): no reachable vulnerability in a third-party module,
  24 reachable ones in the standard library (review R2). A patched Go in the
  workflows and a clean run before any tag are user gates; the release
  workflow's candidate job runs govulncheck and stops on a finding.
- **Security review** (`docs/security/2026-10-08-security-review.md`):
  Critical 0; High 1, fixed (R1: claude-cli runs loaded the repository's
  `.claude/settings.json`; now `--setting-sources user`); Medium 6 (R2 the
  workflows' Go toolchain, a user gate enforced on a tag; R3 partly fixed,
  the rest a follow-up; R5 a follow-up; R4, R6, R7 accepted known issues);
  Low 4 (R8 a follow-up; R9, R10, R11 accepted).
- **Acceptance run (macOS).** `gofmt`, `go vet`, `go test -race -count=1
  ./...` (48 packages `ok`), `make cross`, the local release end to end and
  actionlint/shellcheck passed. Of the 41 checklist rows, 34 have macOS
  evidence (12 of them with a live or Windows part as a user gate), none
  fails, 7 wait on user gates or the running soak. Row 13 (memory export,
  import and purge were CLI only) failed in the first run; Task 14 added
  them to the memory screen, and its live popup check is a user gate.
  Linux and Windows columns: user gates.
- **Live smoke.** `--version`, a fresh engine start without `migrate:`
  warnings, and a `queue.json` a newer version wrote (read-only, unchanged,
  the engine still starts) passed. The toast and the memory approve guard in
  a tiny popup need a running `dots-test` Herdr session, which resumes six
  Claude Code panes from earlier runs on every start, so they are user gates.
- **Parked from Task 7 (notes, not user gates):** word the read-only toast by
  reason when the first real migration lands; deleting a dot from the manage
  popup skips `CheckWritable` (requested by the user); files a migration
  would upgrade are not protected until the first migration exists.

User gates that remain (the Blockers of `docs/release/ready-to-publish.md`):

- License.
- Name and listing.
- The acceptance run on Linux and Windows hardware, and on macOS with a real `herdr plugin install`.
- The 8-hour soak result review.
- A patched Go in every workflow and a clean govulncheck before any tag (no `toolchain` line in `go.mod`).
- GitHub Actions pinned by commit SHA.
- `SECURITY.md` `TODO(publish)` placeholders.
- Live provider checks (Claude, API backends with keys, the `suppressed` repeat).
- The R1 live check (a repository `apiKeyHelper` must not run; does `CLAUDE.md` still reach dots).
- Codex live tests and the P10-R8 dotted MCP name check.
- The Herdr popup and pane checks (Phase 9 popup, memory approve guard, the memory screen's export, import and purge for acceptance row 13, read-only toast, Phase 4 V6).
- PowerShell install tests on a `pwsh` host; the Windows runtime; real cosign.
- Decisions: U22 (`enter` deletes a deleted dot's memory; keep is recommended), the Claude model setting (X13), old trust entries in `~/.claude.json` (X14), the `dots-test` session.

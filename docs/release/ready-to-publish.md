# Ready to publish: the developer's checklist

Phase 10 built Herdr Dots up to a release candidate (`1.0.0-rc.1`) and
stopped here. Everything below needs the owner. Nothing has been pushed,
tagged, listed or published. The macOS evidence is in
[docs/superpowers/notes/1.0-acceptance.md](../superpowers/notes/1.0-acceptance.md).

## Blockers

- [ ] **Listing.** The name stays "Herdr Dots" (`GustavoGomez092/herdr-dots`). The repository is public with the `herdr-plugin` topic (2026-10-10); check that herdr.dev/plugins lists it (the index picks the topic up on its next refresh).
- [ ] **Acceptance run on three OSes.** Run `docs/superpowers/notes/1.0-acceptance.md` on a Linux machine and on Windows with the Herdr Windows beta, and on macOS with a real `herdr plugin install`; fill the Linux and Windows columns.
- [ ] **Soak job cost.** Decide whether CI's soak job should run on every push (it costs runner time); Go 1.26.9, the Actions and GoReleaser are now pinned (keep them current: re-run govulncheck and bump the pins before each release).
- [ ] **Live provider checks.** With Claude Code logged in, run `go test -tags live ./internal/backend/ ./internal/council/` (a small API spend; snapshot `~/.claude/settings.json` before and diff it after: it must not change) and the live smoke in a `herdr --session dots-test` session: attach a Security dot to a Claude Code pane, get a card for an obvious secret, dismiss it, and see `suppressed` in the feed when the same request repeats. Run the API backends' live tests with your keys (`docs/superpowers/notes/2026-10-08-phase-4-verification.md`).
- [ ] **R1 live check (repository settings never load).** In a scratch repository whose `.claude/settings.json` sets an `apiKeyHelper` that writes a marker file, run one dot review: the marker must not appear. In the same run, note whether the repository's `CLAUDE.md` still reaches the dot (the Claude Code bundle suggests it doesn't under `--setting-sources user`; inferred, not run). Re-check `--restricted` on the Claude Code version you have (triage X8).
- [ ] **Codex.** Install Codex, log in, run `go test -tags live ./internal/backend/ ./internal/council/` and the Codex rows of the acceptance run.
- [ ] **P10-R8 live check with real Codex.** A Codex dot runs with every bare-named MCP server from your Codex config disabled; add an MCP server whose name contains a `.` and check that the dot refuses to run with a clear error.
- [ ] **The Herdr popup and pane checks.** In `herdr --session dots-test`: the Phase 9 popup check (profile `i`, import preview `I` read to the end, trust `T`, edit, pause); the memory approve guard (a project proposal, the popup shrunk under 10 lines: `a` says "Too small to show the whole proposal" and `y` doesn't approve); the memory screen's export, import and purge (acceptance row 13: `E` writes a private file, `I` shows a preview and needs `Y` for secret-looking entries, `P` needs the project id and keeps global memory); the read-only toast (put `{"version":2,"cards":[]}` in the session's `queue.json`, start the engine, see the toast naming `queue.json`, and check the file is unchanged); the Phase 4 V6 no-echo key prompt.
- [ ] **PowerShell install on Windows from the release.** `build.ps1` builds the plugin from source on GitHub's windows-latest runner in every CI run, and the four `TestPowerShell*` tests pass there. Still open: a real `herdr plugin install` on Windows, which downloads the release zip.
- [ ] **Windows runtime.** Every Windows behaviour is compile-checked and unit-tested on macOS only: run the acceptance run on Windows (triage W6, W7, W17, X1, X2).

## Decisions for the developer

- [ ] **Your Claude model setting (triage X13).** A v1 test run rewrote the `model` in `~/.claude/settings.json`; it was restored from an older backup, so the true earlier value is unknown. Confirm it is what you want.
- [ ] **Old trust entries in `~/.claude.json` (triage X14).** An earlier soak accepted Claude Code's folder-trust prompt, so `~/.claude.json` holds trust entries for deleted `/private/tmp` test directories (`/private/tmp/dots-test-repo`, `/private/tmp/dots-test-repo-p8`). You may remove them.
- [ ] **The `dots-test` Herdr session.** It still holds six workspaces from earlier test runs, and every start of the session resumes a `claude --resume` in each of their panes. Delete it (`herdr session delete dots-test` while it is stopped) before the next live check, or keep it if you use it.

## Every user gate, cross-checked

Every `USER GATE` and `USER DECISION` row of the triage notes
(`docs/superpowers/notes/2026-10-08-phase-10-triage.md`) and every user gate
line of the phase ledgers, with the checklist line that covers it. Status is
USER GATE for all of them.

| Gate | What | Source | Covered by |
|---|---|---|---|
| X1 | `windows/arm64` built and published, never tested | triage, Phase 10 | Windows runtime; acceptance run |
| X2 | Linux and Windows have only CI and cross-compiled tests | triage, Phases 6 and 10 | Acceptance run on three OSes |
| X3 | Codex paths documented-format, not live-verified (transcripts, MCP, the `-c` override) | triage; v1 T9, P2 T1, P5 T5 | Codex; P10-R8 live check |
| X4 | Live Anthropic, OpenAI and OpenRouter runs; the Phase 4 V6 no-echo popup check | triage; Phase 4 ledger | Live provider checks; popup checks |
| X5 | Visual pane checks; the Phase 9 Task 11 Step 8 popup check | triage; Phase 5, 8, 9 ledgers | Popup and pane checks |
| X6 | Live `suppressed` line on a repeated finding | triage; Phase 2 T16 | Live provider checks |
| X7 | Live Claude draft end to end; Windows runtime of drafts | triage; Phase 7 ledger | Live provider checks; Windows runtime |
| X8 | Re-verify `--restricted` on new Claude Code versions | triage; Phase 7 ledger | R1 live check |
| X9 | Read deny vs Grep/Glob and `--restricted` reads outside the working directory | triage; Phase 8 ledger | Live provider checks |
| X10 | Live `--max-budget-usd` stop behaviour | triage; Phase 8 ledger | Live provider checks |
| X11 | V3/V5/V6 web paths | triage; Phase 8 ledger | Live provider checks |
| X12 | Soak job cost on every push (Go, Actions and GoReleaser now pinned) | triage; Phase 10 Task 5 | Soak job cost |
| X13 | The global Claude model setting after the v1 incident | triage, v1 incident | Decisions: Claude model setting |
| X14 | Trust entries in `~/.claude.json` for deleted test directories | triage, Phase 8 incident | Decisions: old trust entries |
| B14 | OpenAI `reasoning.encrypted_content` sent for non-reasoning models | triage; P4 T11 | Live provider checks (OpenAI key) |
| B26 | History keeps `server_tool_use` blocks after the web tools drop | triage; P8 T9 | Live provider checks (web, X11) |
| W6 | Herdr config precedence (XDG before APPDATA) on Windows | triage; P6 T2 | Windows runtime |
| W7 | OneDrive placeholder files and 8.3 short names | triage; P6 T3, T8 | Windows runtime |
| W16 | CI actions pinned by major tag; CI runs twice on PR branches | triage; P6 T12 | Pin actions |
| W17 | Herdr resolving `./bin/herdr-dots` to `.exe`; first `build.ps1` run; `-race` with gcc on Windows CI | triage; Phase 6 ledger | Windows runtime; PowerShell tests |
| T6-pwsh | PowerShell install tests on a `pwsh` host | Phase 10 ledger (Task 6) | PowerShell install tests |
| R1 | Repository `.claude/settings.json` must not load (live) | Phase 10 ledger (Task 11), security review | R1 live check |
| P10-R8 | Codex dots disable bare-named MCP servers, refuse dotted names (live) | Phase 10 ledger (Task 9) | P10-R8 live check |
| SMOKE | The read-only toast and the memory approve guard in a live Herdr popup | Task 13 smoke (S3, S4) | Popup and pane checks |
| DOTS-TEST | The `dots-test` session resumes six Claude panes on every start | Task 13 smoke | Decisions: the `dots-test` session |
| LICENSE, NAME, PUBLISH | License, name and listing, publishing | spec rulings 1 and 16, release-gate amendments | License; Name and listing; Publishing steps |

## Publishing steps (in order)

`internal/version/VERSION` is the exact version the binaries report
(`1.0.0-rc.1` now). A release candidate is a manual run of the release
workflow on `main`, with no tag; publishing is the tag `v<VERSION>` on the
merge commit of that run's checksum pull request (controller ruling P10-R12,
checked by `scripts/release-kind.sh`). A VERSION ending in `-rc.<digits>`
publishes a prerelease, `X.Y.Z` the final release, and any other suffix is
refused.

1. Create the GitHub repository and push `main`. Check that CI is green on macOS, Linux and Windows, including the PowerShell install tests and the fuzz, vulncheck and soak jobs. **(done 2026-10-10: public repository, CI green on macOS, Linux and Windows)**
2. `release/REPO` holds `GustavoGomez092/herdr-dots` (set 2026-10-10), so installs find the repository even when Herdr's clone has no GitHub `origin`. **(done)**
3. One-time GitHub setup: in Settings → Actions → General → Workflow permissions, enable "Allow GitHub Actions to create and approve pull requests" (the candidate opens the checksum pull request with `GITHUB_TOKEN`). A pull request opened by `GITHUB_TOKEN` doesn't trigger CI: run the `ci` workflow on its branch by hand (Actions → ci → Run workflow, pick the branch), or give the candidate job a personal access token or GitHub App token instead (your choice). **(done 2026-10-10)**
4. **Candidate.** With VERSION at `1.0.0-rc.1`, start the release workflow on `main`: Actions → release → Run workflow (or `gh workflow run release.yml --ref main`). It tests on three OSes, fuzzes every target for 10 minutes, runs govulncheck and the end-to-end install test, builds the six archives with `HERDR_DOTS_VERSION=1.0.0-rc.1` and opens a pull request with `release/SHA256SUMS` and `release/ASSET_ENV`. Its body says: "Merge this, then tag `v<VERSION>` on the merge commit". **(done 2026-10-11: release run 38095520861 (gates green; the first run 38090648131 passed every gate at the 10m fuzz time and failed only in packaging, fixed in 6cb7e36))**
5. Run CI on that pull request's branch (step 3), then merge it. Tag `v1.0.0-rc.1` on the merge commit and push the tag. The workflow checks that the tag is `v<VERSION>` and that the committed `ASSET_ENV` and `SHA256SUMS` are for exactly that VERSION, rebuilds, refuses if the archives differ from the committed checksums, signs them with cosign and creates a draft prerelease. **(done 2026-10-11: PR #1 merged, tag v1.0.0-rc.1 on bd5da4f, publish run 38097269932)**
6. Check the draft (six archives, six `.sigstore.json` bundles, `SHA256SUMS`), then publish it. **(done 2026-10-11: six archives, six bundles and SHA256SUMS; every checksum matches PR #1; cosign v3.1.3 verify-blob passes for all six; published as a pre-release)**
7. Install on a machine without Go: `herdr plugin install GustavoGomez092/herdr-dots`; `./bin/herdr-dots --version` must say `release`; `./bin/herdr-dots verify-release` must pass (with cosign installed, the signature too). Repeat on each of the six targets you can reach. **(done on macOS arm64 2026-10-11: a fresh clone at the tag, with no Go on PATH, installed the verified release binary (`herdr-dots 1.0.0-rc.1 (release, commit 6cb7e36118ce)`); the other five targets wait for those machines)**
8. **Final.** When the acceptance run passes everywhere, set `internal/version/VERSION` and `herdr-plugin.toml` to `1.0.0`, update `TestVersionIsTheReleaseCandidate` and the CHANGELOG heading, commit to `main`, and repeat steps 4–7 for VERSION `1.0.0`: a new candidate run, merge its checksum pull request, tag `v1.0.0` on the merge commit. This draft is the final release, not a prerelease.
9. Add the `herdr-plugin` topic to the repository and check the listing on herdr.dev/plugins (it refreshes every 30 minutes).
10. Only now may the README and CHANGELOG say "released". The roadmap's acceptance checklist is met only when the acceptance run passes on all three OSes.

## What Phase 10 prepared

- `.goreleaser.yaml`, `.github/workflows/release.yml` and `ci.yml` (lint-clean with actionlint; never run).
- `scripts/build.sh`, `build.ps1` (verify, fall back, fail cleanly), `scripts/release-local.sh`, `make snapshot` (archives built and installed end to end locally).
- `herdr-dots verify-release`, `migrate`, `uninstall-extras`.
- `docs/security/threat-model.md`, the security review, `SECURITY.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `docs/guide/`.
- `docs/superpowers/notes/1.0-acceptance.md` (macOS evidence).

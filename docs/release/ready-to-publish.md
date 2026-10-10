# Ready to publish: the developer's checklist

Phase 10 built Herdr Dots up to a release candidate (`1.0.0-rc.1`) and
stopped here. Everything below needs the owner. Nothing has been pushed,
tagged, listed or published. The macOS evidence is in
[docs/superpowers/notes/1.0-acceptance.md](../superpowers/notes/1.0-acceptance.md).

## Blockers

- [ ] **License.** Choose one (the spec suggested Apache-2.0 for its patent grant, or MIT) and add `LICENSE`. No license means no one may use the code.
- [ ] **Name and listing.** Keep "Herdr Dots" or rename. OpenAI announced "Dots" (DevDay 2026); a rename later is supported by `internal/migrate` but touches the plugin id, binary and docs. Check the name on GitHub and herdr.dev/plugins.
- [ ] **Acceptance run on three OSes.** Run `docs/superpowers/notes/1.0-acceptance.md` on a Linux machine and on Windows with the Herdr Windows beta, and on macOS with a real `herdr plugin install`; fill the Linux and Windows columns.
- [ ] **8-hour soak result.** The 8-hour soak is running (started 2026-10-09 18:07 on c1245f9); the controller records the result when it ends. Review it: `--- PASS: TestSoak` and its log line (peak RSS, idle CPU, biggest snapshot) must meet the budgets. If it failed, rerun `make soak SOAK_DURATION=8h` after the fix.
- [ ] **Patched Go in every workflow.** In `.github/workflows/ci.yml` and `release.yml`, replace every `go-version-file: go.mod` with an explicit patched `go-version` (at least the release that fixes the govulncheck findings, e.g. 1.26.9 or later). Then run `make vulncheck` with that Go and get `No vulnerabilities found.` before the first candidate run (on go1.25.7 it found 24 reachable standard-library vulnerabilities, security review R2; the acceptance run's govulncheck couldn't reach its database, so run it with network access). Today every workflow's `setup-go` reads `go-version-file: go.mod`, whose `go 1.25.0` line makes it install exactly go1.25.0. Do not add a `toolchain` line to `go.mod`: under `GOTOOLCHAIN=auto` it would make every source-fallback install download a toolchain, which fails offline and on new hosts.
- [ ] **Pin GitHub Actions by commit SHA** (`actions/checkout`, `actions/setup-go`, `actions/upload-artifact`, `goreleaser/goreleaser-action`, `sigstore/cosign-installer`), and decide whether the soak job should run on every push (it costs runner time).
- [ ] **`SECURITY.md` placeholders.** Replace the `TODO(publish)` lines: the private reporting contact and the repository URL.
- [ ] **Live provider checks.** With Claude Code logged in, run `go test -tags live ./internal/backend/ ./internal/council/` (a small API spend; snapshot `~/.claude/settings.json` before and diff it after: it must not change) and the live smoke in a `herdr --session dots-test` session: attach a Security dot to a Claude Code pane, get a card for an obvious secret, dismiss it, and see `suppressed` in the feed when the same request repeats. Run the API backends' live tests with your keys (`docs/superpowers/notes/2026-10-08-phase-4-verification.md`).
- [ ] **R1 live check (repository settings never load).** In a scratch repository whose `.claude/settings.json` sets an `apiKeyHelper` that writes a marker file, run one dot review: the marker must not appear. In the same run, note whether the repository's `CLAUDE.md` still reaches the dot (the Claude Code bundle suggests it doesn't under `--setting-sources user`; inferred, not run). Re-check `--restricted` on the Claude Code version you have (triage X8).
- [ ] **Codex.** Install Codex, log in, run `go test -tags live ./internal/backend/ ./internal/council/` and the Codex rows of the acceptance run.
- [ ] **P10-R8 live check with real Codex.** A Codex dot runs with every bare-named MCP server from your Codex config disabled; add an MCP server whose name contains a `.` and check that the dot refuses to run with a clear error.
- [ ] **The Herdr popup and pane checks.** In `herdr --session dots-test`: the Phase 9 popup check (profile `i`, import preview `I` read to the end, trust `T`, edit, pause); the memory approve guard (a project proposal, the popup shrunk under 10 lines: `a` says "Too small to show the whole proposal" and `y` doesn't approve); the memory screen's export, import and purge (acceptance row 13: `E` writes a private file, `I` shows a preview and needs `Y` for secret-looking entries, `P` needs the project id and keeps global memory); the read-only toast (put `{"version":2,"cards":[]}` in the session's `queue.json`, start the engine, see the toast naming `queue.json`, and check the file is unchanged); the Phase 4 V6 no-echo key prompt.
- [ ] **PowerShell install tests.** Run `go test ./scripts/` on a host with `pwsh` (the `TestPowerShell*` tests skip without it), and the first real `build.ps1` run on Windows.
- [ ] **Windows runtime.** Every Windows behaviour is compile-checked and unit-tested on macOS only: run the acceptance run on Windows (triage W6, W7, W17, X1, X2).
- [ ] **Real cosign.** Check a signed archive with `cosign verify-blob` (and `herdr-dots verify-release` with cosign installed) after the first signed release.

## Decisions for the developer

- [ ] **U22: what `enter` does when a dot is deleted.** Deleting a dot asks "Also delete its memory in all projects? (Y/n)" and `enter` answers yes (the spec's default: forgetting is real). A reflexive `enter` loses that dot's memory with no undo. Phase 2 memory is VITAL, so the recommendation is to switch the default to keep (`(y/N)`). Your call.
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
| X12 | Pin GitHub Actions by SHA; pin an exact Go toolchain; soak job cost on every push | triage; Phase 10 Task 5 | Pin actions; patched Go |
| X13 | The global Claude model setting after the v1 incident | triage, v1 incident | Decisions: Claude model setting |
| X14 | Trust entries in `~/.claude.json` for deleted test directories | triage, Phase 8 incident | Decisions: old trust entries |
| B14 | OpenAI `reasoning.encrypted_content` sent for non-reasoning models | triage; P4 T11 | Live provider checks (OpenAI key) |
| B26 | History keeps `server_tool_use` blocks after the web tools drop | triage; P8 T9 | Live provider checks (web, X11) |
| W6 | Herdr config precedence (XDG before APPDATA) on Windows | triage; P6 T2 | Windows runtime |
| W7 | OneDrive placeholder files and 8.3 short names | triage; P6 T3, T8 | Windows runtime |
| W16 | CI actions pinned by major tag; CI runs twice on PR branches | triage; P6 T12 | Pin actions |
| W17 | Herdr resolving `./bin/herdr-dots` to `.exe`; first `build.ps1` run; `-race` with gcc on Windows CI | triage; Phase 6 ledger | Windows runtime; PowerShell tests |
| U22 | `enter` deletes a deleted dot's memory by default | triage (USER DECISION); P2 T12 | Decisions: U22 |
| T6-pwsh | PowerShell install tests on a `pwsh` host | Phase 10 ledger (Task 6) | PowerShell install tests |
| T6-cosign | Real `cosign verify-blob` | Phase 10 ledger (Task 6) | Real cosign |
| R1 | Repository `.claude/settings.json` must not load (live) | Phase 10 ledger (Task 11), security review | R1 live check |
| R2 | Patched Go in the workflows; govulncheck clean before the first candidate run | Phase 10 ledger (Task 11), security review | Patched Go |
| SEC | `SECURITY.md` `TODO(publish)` placeholders | Phase 10 ledger (Task 11) | `SECURITY.md` placeholders |
| P10-R8 | Codex dots disable bare-named MCP servers, refuse dotted names (live) | Phase 10 ledger (Task 9) | P10-R8 live check |
| SOAK | 8-hour soak result | Phase 10 ledger, Task 13 | 8-hour soak result |
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

1. Create the GitHub repository and push `main`. Check that CI is green on macOS, Linux and Windows, including the PowerShell install tests and the fuzz, vulncheck and soak jobs.
2. If Herdr's clone won't have a GitHub `origin`, put `owner/herdr-dots` in `release/REPO`.
3. One-time GitHub setup: in Settings → Actions → General → Workflow permissions, enable "Allow GitHub Actions to create and approve pull requests" (the candidate opens the checksum pull request with `GITHUB_TOKEN`). A pull request opened by `GITHUB_TOKEN` doesn't trigger CI: run the `ci` workflow on its branch by hand (Actions → ci → Run workflow, pick the branch), or give the candidate job a personal access token or GitHub App token instead (your choice).
4. **Candidate.** With VERSION at `1.0.0-rc.1`, start the release workflow on `main`: Actions → release → Run workflow (or `gh workflow run release.yml --ref main`). It tests on three OSes, fuzzes every target for 10 minutes, runs govulncheck and the end-to-end install test, builds the six archives with `HERDR_DOTS_VERSION=1.0.0-rc.1` and opens a pull request with `release/SHA256SUMS` and `release/ASSET_ENV`. Its body says: "Merge this, then tag `v<VERSION>` on the merge commit".
5. Run CI on that pull request's branch (step 3), then merge it. Tag `v1.0.0-rc.1` on the merge commit and push the tag. The workflow checks that the tag is `v<VERSION>` and that the committed `ASSET_ENV` and `SHA256SUMS` are for exactly that VERSION, rebuilds, refuses if the archives differ from the committed checksums, signs them with cosign and creates a draft prerelease.
6. Check the draft (six archives, six `.sigstore.json` bundles, `SHA256SUMS`), then publish it.
7. Install on a machine without Go: `herdr plugin install <owner>/herdr-dots`; `./bin/herdr-dots --version` must say `release`; `./bin/herdr-dots verify-release` must pass (with cosign installed, the signature too). Repeat on each of the six targets you can reach.
8. **Final.** When the acceptance run passes everywhere, set `internal/version/VERSION` and `herdr-plugin.toml` to `1.0.0`, update `TestVersionIsTheReleaseCandidate` and the CHANGELOG heading, commit to `main`, and repeat steps 4–7 for VERSION `1.0.0`: a new candidate run, merge its checksum pull request, tag `v1.0.0` on the merge commit. This draft is the final release, not a prerelease.
9. Add the `herdr-plugin` topic to the repository and check the listing on herdr.dev/plugins (it refreshes every 30 minutes).
10. Only now may the README and CHANGELOG say "released". The roadmap's acceptance checklist is met only when the acceptance run passes on all three OSes.

## What Phase 10 prepared

- `.goreleaser.yaml`, `.github/workflows/release.yml` and `ci.yml` (lint-clean with actionlint; never run).
- `scripts/build.sh`, `build.ps1` (verify, fall back, fail cleanly), `scripts/release-local.sh`, `make snapshot` (archives built and installed end to end locally).
- `herdr-dots verify-release`, `migrate`, `uninstall-extras`.
- `docs/security/threat-model.md`, the security review, `SECURITY.md`, `CHANGELOG.md`, `CONTRIBUTING.md`, `docs/guide/`.
- `docs/superpowers/notes/1.0-acceptance.md` (macOS evidence).

# Phase 10 triage of deferred review findings

Source: every deferred line in `.superpowers/sdd/*/progress.md` (v1 and
Phases 2–10: "minor (deferred)", "deferred → Phase 10 triage", "Parked →
Phase 10", "→ final wave" items the final fix waves did not take, "FLAG FOR
FINAL REVIEW", flaky-test notes, Phase 10 carry-overs), the "NOT in scope"
lists of every phase's `final-fix-brief.md`, the Phase 2 completion notes
(P2-R3, P2-R9) and ruling P8-R7. Rule: fold what matters for a release
(security, privacy, a panic, data loss, acting on something unseen); list the
rest as known issues with the reason. A fold that needs a file outside Task 9's
own (`internal/memory/*`, `internal/secrets/*`, these notes) is a known issue
marked "follow-up". Controller ruling P10-R7 (Task 9 fix round 1) then moved
six accepted rows into Task 9: M2, B10, S18's deny-glob half, L20's deny-glob
part (now L21), S15 and S14. Fix round 2 corrected B10 (ruling P10-R8: Codex
doesn't quote `-c` keys), closed M2's path-identity fallback, and added S22
(ruling P10-R9: commit reviews leave denied files out), S23 and S24.

This is release candidate 1.0.0-rc.1, not released.

**Post-rc hardening (after rc.1).** A hardening wave and its fix wave (merged at
`e4e38ea`) took about 90 accepted rows and security findings R3, R5 and R8 of the
security review. The rows they fixed are `FIXED POST-RC`; rows fixed in part are
`PARTLY FIXED POST-RC` and say what remains. The Known issues below list only
what is still open.

**Statuses.** Every item in the full accounting below has exactly one:

- `FIXED T9`: fixed in Phase 10 Task 9, with its test ("Round 1 (P10-R7)"
  and "Round 2" mark what its fix rounds did).
- `FIXED T10`: fixed in Phase 10 Task 10 (cross-reference;
  Task 10 runs in parallel and owns those files). Its tests are not on this
  branch yet; the controller verifies them at merge.
- `FIXED EARLIER`: a deferred line that a later phase already resolved before
  Phase 10; the evidence (a test, or the ledger line of the fix) is given, so
  nothing is dropped silently.
- `FIXED FINAL`: fixed in the Phase 10 final fix wave (after the final
  whole-branch review), with its test.
- `FIXED POST-RC`: fixed in the post-rc hardening wave and its fix wave (after
  1.0.0-rc.1, merged at `e4e38ea`), with its test names in the evidence column.
- `PARTLY FIXED POST-RC`: part of the row is fixed (the evidence says which, with
  its tests) and the rest stays accepted, with its reason.
- `ACCEPTED`: a known issue in 1.0.0-rc.1, with a one-line reason.
- `USER GATE`: needs the developer (hardware, a live provider, a publishing
  step, or their own machine state).
- `USER DECISION`: a product default the developer chooses; it goes on the
  ready-to-publish checklist. None is open: the one row, U22, was applied after
  rc.1 (deleting a dot keeps its memory unless the developer presses `y`) and is
  now `FIXED POST-RC`.

**Counts** (rows of the full accounting): FIXED T9: 10 · FIXED T10: 10 ·
FIXED EARLIER: 62 · FIXED FINAL: 1 · FIXED POST-RC: 71 · PARTLY FIXED POST-RC: 19 ·
ACCEPTED: 142 · USER GATE: 19 · USER DECISION: 0 · total 334. A row may group several sub-items of one ledger line that share
one status and one reason.

**Corrections to the plan's draft of these notes.** Re-reading the ledgers
against the code found five items the draft listed as open that a later phase
had already fixed: the memory CLI's import skip message (Phase 2 final fix 9
prints the entry number only), `councilConventionText` stripping any trailing
`(…)` (Phase 3 final fix 1: `TestFirstPointSkipsHeadersAndStripsAttribution`
keeps "(see RFC 7519)"), turn-review budget checks ignoring in-flight asks
(Phase 8 final fix 3: `TestTurnReviewCountsAnAskInFlight`), `e.Wait` not
covering in-flight IPC handlers (Phase 5 Task 2: `ipc.Serve` waits for every
connection before it returns, and `serveEngine` calls `e.Wait` after it), and
the pane not saying how many cards are hidden (Task 10's carry-over: the
header's QUEUE count is `len(Cards)+HiddenCards`). It also found one new item
(M2 below, fixed in round 1).

## Folded into Phase 10

| Finding (ledger) | Fix | Test |
|---|---|---|
| Zero-width-character secret evasion (Phase 2): `sk-ant-` + U+200B + key passed the scanner | `secrets.Scan` (behind `memory.ScanSecrets`, dot packs, `dots.Encode`, history) also scans the text without format characters and combining marks | `TestInvisibleCharactersDoNotHideASecret`, and `TestInvisibleCharacterStrippingKeepsOrdinaryTextClean` (Devanagari, Hebrew points, Arabic vowels, decomposed accents, Persian ZWNJ, emoji joiners, Hangul and Braille stay unflagged); round 1 also drops `Other_Default_Ignorable_Code_Point` and U+2800 (`TestIgnorableFillersDoNotHideASecret`) |
| Nested-repo `FindProject` prefix match (Phase 2): the first project (by name) whose root prefixes the path won, so a vendored or nested repository could resolve to its parent | the deepest matching root wins (ties keep the old first-by-name order) | `TestFindProjectPrefersTheNestedRepository`, `TestFindProjectKeepsNestedReposAndWorktreesApart` (real repositories: a worktree inside the outer repo, a vendored repo inside it and that repo's own worktree; each path finds its own project and only its own memory) |
| Every git child pinned (Phase 2 final review; Plan decision 29) | already fixed by Phases 6–8; Task 9 pins it: the guard now requires all five settings as string literals (`core.fsmonitor=false`, `core.hooksPath=`, `protocol.allow=never`, `GIT_ALLOW_PROTOCOL=none`, `GIT_NO_LAZY_FETCH=1`), covers `internal/worktree` files that start a child process, fails if any of the three known git call sites stops being found, and (round 1) fails on a `"git"` literal in any other product file; the no-op `core.pager` setup was removed (git starts a pager only on a terminal) | `TestExecGitRunsNothingTheRepositoryConfigures`, `TestEveryGitCallDisablesRepositoryPrograms` |
| memui approve screen clipped in tiny popups (P2-R9): the terminal cut the top lines (who proposed it, kind, paths) while `y` still approved | the approve screen drops the list header; when it still doesn't fit, it says so and `y` refuses until the popup is enlarged | `TestApproveShowsTheFullProposalAndNeedsY` (extended, Task 10) |
| `ExportTOML` wrote `ref = "d:"` for an entry without an id (found by fuzzing) | no `ref` for an unsaved entry | `FuzzImportTOML/8a748f204ecc8c6f` (Task 3) |
| Load/Delete don't sanitize slug (v1 Task 4): `dots.Load/Save/Delete` accepted `../x` | `ErrBadSlug` for a slug that is not a plain file name in the dots dir (hand-named files keep working) | `TestSlugsThatAreNotFileNamesAreRefused` (Task 10) |
| O_CREATE doesn't tighten existing file mode (v1 Task 11): an old feed file kept a loose mode | `OpenFeed` makes it 0600 | `TestOpenFeedMakesAnOldLooseFileOwnerOnly` (Task 10) |
| `Recent(n<0)` panics (v1 Task 11), `TruncateRunes(n<=0)` panics (v1 Task 13) | both return empty | `TestOpenFeedMakesAnOldLooseFileOwnerOnly`, `TestTruncateRunesNeverPanics` (Task 10) |
| Flaky `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies` (Phase 6 note, Phase 8 post-merge): the stub engine's listener was closed asynchronously after cancel, and a Unix listener unlinks its socket path on close, so a late close could delete the socket of the engine the test had just started again | the test's `startStub` stop waits for `ipc.Serve` to return (the old listener is closed and unlinked before the test goes on) | `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies` (`-race -count=50` green, Task 10) |
| Phase 7 draft tests time out under load: the draft rig waits with `eventually`, whose 3 s deadline a loaded machine's git (worktree add, snapshot, diff) can exceed | `eventually` waits up to 15 s (a passing test never waits for it) | the Phase 7 draft tests under `-race -count=6` with parallel load (Task 10) |
| Phase 10 carry-overs to Task 10 (Phase 10 ledger): `trustAt` requeue when the new grant drops inject; `untrustAt` requeue across every rescanned checkout; the pane header's QUEUE count includes `HiddenCards` | Task 10 (engine `projectdots.go`, `ui/pane`) | Task 10's tests |
| Memory CLI in an unattached nested repo filed memory under the outer project (M2, found in Task 9) | round 1: git first; relative `--project` against the CLI's cwd; round 2: for a path identity, a recorded project only at or below the nearest `.git`; purge refuses a missing path | `TestMemoryAddInAnUnattachedNestedRepoUsesTheNestedProject`, `TestMemoryPurgeOfANestedRepoPathLeavesTheOuterProject`, `TestStaleDeeperPathProjectDoesNotBeatTheRepository`, `TestNestedRepoWithAPathIdentityNeverFallsBackToTheOuterProject`, `TestGitFailureInANestedRepoNeverFallsBackToTheOuterProject`, `TestMemoryPurgeRefusesAPathThatDoesNotExist` |
| Codex dot runs loaded the developer's other MCP servers (B10, v1 final) | round 1: every declared server disabled; round 2 (P10-R8): names raw, and a name with '.' or '=' refuses the run | `TestCodexDisablesEveryDeclaredMCPServer`, `TestCodexRefusesAServerItCantDisable` |
| `tool_deny_globs` not in claude-cli research settings (S18, Phase 8 parked) | round 1: `researchSettings(extra)` with the config globs | `TestClaudeResearchDeniesTheConfiguredGlobs` |
| An invalid `tool_deny_globs` entry was dropped silently (L21, Phase 4 Task 4) | round 1: fails closed: API and claude-cli repository tools, drafts, research and commit patches off until fixed (codex dots read through their sandbox); logged and toasted once; roster hint | `TestInvalidDenyGlobFailsClosed`, `TestInvalidDenyGlobTurnsRepoToolsOff`, `TestInvalidDenyGlobRefusesDrafts`, `TestInvalidDenyGlobTurnsResearchOff`, `TestInvalidDenyGlobIsLoggedAndToastedOnce` |
| Same-host redirect allowed `https`→`http` (S15, Phase 4 Task 12) | round 1: a scheme change is refused | `TestSameHostRedirectRefusesASchemeChange` |
| SDK-chain credentials not passed to `Redact` (S14, Phase 4 Task 10) | round 1: `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_API_KEY` redacted by value; round 2 adds `ANTHROPIC_IDENTITY_TOKEN` and custom header values | `TestSDKChainTokensAreRedactedFromErrors`, `TestSDKChainKnownSecretsCoverHeadersAndIdentityToken` |
| Commit reviews sent denied files' content (S22, re-review, P10-R9) | round 2: their hunks are left out, with a count | `TestCommitReviewLeavesDeniedFilesOut` |
| `FuzzCodexBlock`'s first invariant was wrong (found by fuzzing): the developer's own comment may quote the marker text inside a longer line | the invariant compares whole marker lines; no product change | `FuzzCodexBlock/a763424096dd21b2` (Task 3) |

## Already fixed by a later phase (verified at `3adf2cc`)

The full accounting below lists every one (`FIXED EARLIER`); these are the
notable ones.

| Finding (ledger) | Fixed by | Test |
|---|---|---|
| Git config execution in hostile repos (Phase 2 final review): `.git/config` can name `core.fsmonitor`, hooks or transports | Phases 6–8: every git child passes `core.fsmonitor=false`, `core.hooksPath=<null device>`, `protocol.allow=never`, env `GIT_ALLOW_PROTOCOL=none`, `GIT_NO_LAZY_FETCH=1` (`memory.ExecGit`, `toolloop`, `worktree.Git`) | `TestExecGitNeverRunsHooks`, and Task 9's pins `TestExecGitRunsNothingTheRepositoryConfigures`, `TestEveryGitCallDisablesRepositoryPrograms` |
| Unicode bidi controls in model text (an approval-screen spoof) | `textutil.SanitizeModelText` drops U+202A–U+202E, U+2066–U+2069, U+200E/F, U+061C (P8-R10) | `TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners` |
| `DeleteDot` leaves ledger/corrupt/proposals (Phase 2 Task 6; the completion note scheduled ledger excerpts for a later phase) | Phase 7 Task 0 and P7-R2 | `TestDeleteDotScrubsItsLedgerAndOrphanedProposals`, `TestDeleteDotRemovesItsCorruptAsidesOnly` |
| Memory edit temp file in `os.TempDir` holds plaintext memory (Phase 2 Task 14, flagged) | a private 0700 directory removed with everything the editor leaves | `TestMemoryEditRemovesItsTempDirectory` |
| `CleanEnv` returns nil, so a child inherits the full environment (v1 Task 8, flagged) | `CleanEnv` always returns a non-nil slice | `TestCleanEnvNeverReturnsNil` |
| Index-based card selection (v1 Task 18a), malformed config keeps memory on (Phase 2 Task 5), user-saved findings never suppressing (Phase 2 Task 10) | selection by id; every kill switch fails closed; the Phase 2 final fix wave | `TestSelectionFollowsCardIDAcrossSnapshots`, `TestMalformedConfigKeepsLastGoodButDisablesInject`, Phase 2 final-fix tests |
| Turn-review budget checks ignore in-flight asks (Phase 5 final) | P8-R8 and the Phase 8 final fix: every budget check uses `inFlightLocked` | `TestTurnReviewCountsAnAskInFlight`, `TestARunningTurnReviewCountsAsInFlight` |
| Broadcast ordering race (v1 final) | Phase 10 Task 4: a send older than one already handed over is dropped | `TestAnOlderSnapshotNeverOverwritesANewerOne` |
| Flaky `TestFinishedPaidReviewIsNotDoubleCounted` (Phase 9) | the controller's fix (the frontend's catch-up review raced the ask) | 100/100 runs |

## Known issues (not fixed in 1.0.0-rc.1)

Each is minor for a release, with its reason; the row id (S2, M17, …) points
at the full accounting below, which has every item. Every accepted row of
areas 1–3 (secrets and privacy, memory, git) is here, and so is every row
that can lose data.

Deleting a dot keeps its memory unless you press `y` at "Also delete its memory in all projects? (y/N)" [U22].

- **Secrets and privacy:** the scanner errs toward refusing (`Bearer` + prose, `tokenizer=`, any assigned value with a digit), so a false positive refuses or asks, never leaks [S2, S3]; `CleanEnv` strips HERDR_* and the Claude session markers but passes the auth/config `CLAUDE_*` variables [S7]; keys from environment variables aren't shape-checked [S11]; `git push --mirror` or a `refs/*` refspec would push the private snapshot refs [S17]; the feed's learned/error detail and a card's error are sanitized when stored, not again when drawn [S19]; a stray `api_key` line in a local dot file is ignored silently (project and imported dots refuse unknown keys) [S20]; research runs `--restricted`, which drops the developer's own `~/.claude` deny rules (research still denies every built-in secret glob and the config's `tool_deny_globs`, and has no shell and no WebFetch) [S21]; on the Anthropic SDK's own credential chain, tokens the SDK reads from files (an `ant auth` profile) or gets by federation exchange aren't redacted by value (key shapes and auth header values still are) [S23]; **codex dots read the repository through their own sandbox, so `RepoRead`, `tool_deny_globs` and the invalid-glob switch don't limit what they read** [S24]. `claude-cli` runs, drafts included, now deny reads of home secrets (`~/.ssh`, `~/.aws`, `~/.claude` and the like) and of the herdr-dots config directory; this is not live-verified for Grep and Glob [X9]. Reason: none of these sends a key or file content anywhere the developer didn't configure.
- **Memory (Phase 2, VITAL):** `r` doesn't reset `MemoryPasses` [M3]; with `memory_enabled = false`, attaching still runs git (repository programs disabled) and writes `meta.json` (name and roots) [M4]; a suppressed verdict still sets the cooldown [M5]; an edited-then-dismissed card records the edited text [M6]; stats can be stale for a moment after memory is switched off at runtime [M7]; an outcome's session is taken at record time, so suppression can miss across a session change [M8]; `FirstSentence` cuts at abbreviations [M11]; `Similar` is unbounded on long inputs (texts are capped at 500 runes) [M12]; git discovery walks above the cwd, by design [M13]; remember-op caps live in `Apply`, not `cleanOp`, and a few op paths are untested [M14]; ref fields aren't one-lined and the block has no end marker [M16]; a deleted dot's ledger scrub can miss the older cards' lines of a merged entry whose text a later merge rewrote (ledger lines are an audit trail, not memory entries) [M17]; **unparseable ledger lines are dropped when the ledger is rewritten** [M19]; `readScope` doesn't check kind or status (fuzzed) [M20]; some apply tests are weak [M23]; `RecordOutcome` now takes one lock for the ledger line and the entry, but a crash between the two writes can still leave the ledger line without its entry [M25]; `Apply` relies on `checkTarget` for the slug [M26]; an unregistered project id creates an empty directory [M29]; `memory add --force` is the consent, not the secret gate [M31]; a rejected agent proposal can be proposed again (no rejection ledger; pending proposals are capped at 20) [M33]; full-scope proposals are archived soon after [M34]; newer research can lose to older under the Phase 5 gate and isn't deduped against archived entries [M35]; `keysConfirmMemory` drops counts on a partial error [M37]. Reason: false positives refuse rather than leak; archived entries are already out of the prompt; approved memory is never displaced by an unapproved write.
- **Git and repository safety:** a pane in a repository subdirectory walks the tree without `.gitignore` (the jail and deny list still apply) [G3]; the listing's git output is bounded only by the run timeout [G6]; **two engines on one repository drop each other's snapshot refs (by design, X8), which ends that draft** [G7]; `CheckPath` doesn't strip HFS-ignorable code points (`.g‌it`): git's `core.protectHFS` (on by default on macOS) refuses HFS-equivalents of `.git`, and APFS doesn't ignore them [G8]; no per-run write byte cap, absolute worktree paths in errors, a trailing slash refused [G9]; `strayOps` can match "index" inside base85 binary data (fails closed, about 1 in 85⁴) [G10]; the `old mode 120000 → 100644` branch is dead in practice [G11]; a small window between proactive-off and a review starting on new heads [G13]. Reason: every one fails closed or stays inside the developer's own checkout.
- **Engine lifecycle:** a crash-looping engine is respawned about every 6 s [E3]. Reason: rare, recoverable by restarting Herdr; `engine.log` is documented.
- **Upgrading (Linux only):** when the state directory is deep enough that the engine socket falls back to another directory, an old engine and a new client can miss each other until Herdr restarts (security review R8). Reason: it needs a deep state path on Linux and an upgrade while an engine is running; restarting Herdr clears it.
- **Card delivery and pane close:** **when a pane closes inside one delivery call, that card's final status can read orphaned although its text reached the agent, and its sent/orphaned outcome can be missing from memory and history (so that finding may repeat once)** [E40]. Reason: needs the pane to close during the call; one card.
- **Transcripts:** a slash command's `<command-name>` record still counts as a turn [T1]. Reason: at most one extra turn is reviewed; dropping it later would shift turn indexes already counted.
- **Verdicts and backends:** `Hint` matches a bare "401"/"login" [B2]; `codex turn.failed` after an earlier `agent_message` is ignored [B4]; Codex can't disable an MCP server whose name holds '.' or '=' from its command line (it splits `-c` on '=' and '.'), so a codex dot run is refused until that server is renamed [B29]. Reason: a hint, or it fails closed; Codex is not live-verified.
- **Queue, state and feed (v1 Tasks 3, 10, 11):** **in-memory state changes before the save, with no rollback when the write fails** (with Phase 10's read-only gate this is the intended "read-only mode") [Q1, Q3]. Until the first migration of a kind is registered, the command-line gate doesn't keep files that need a migration read-only (none can exist yet: every kind is at version 1) [Q16]; deleting a dot from the manage popup skips the read-only gate, at the developer's request [Q17]. Reason: no corruption on disk; a corrupt file is reported.
- **Council (Phase 3):** `ParseDigest` output is not clipped (the card shows it whole; it is sanitized and fuzzed); a negative `max_members` clamps to 2 while 0 means 8; dot names and preflight reasons are rendered as the developer wrote them; the chair is charged if shutdown lands mid-chair; one council can overrun the pane budget between preflight and member runs. Reason: developer-controlled input, cosmetic, or bounded by one council.
- **API backends, keys and cost (Phase 4):** keys from environment variables are not shape-checked; a prices override without `as_of` keeps the embedded date; an unknown `[providers.x]` table in `config.toml` is logged and toasted, and that provider runs on its defaults and fails visibly [L20]; a pane in a repository subdirectory walks the tree without `.gitignore`; the manage popup reads prices once when opened; `CostServed` falls back to the requested model's price when another model served; zero-token failed runs record a known $0; `chairWarned` is set when merge isn't `chair`; OpenRouter BYOK runs report $0 (call budgets still cap them). Reason: display or accounting edge cases; dollar and call budgets still cap spend.
- **MCP server (Phase 5):** a project-level `.codex/config.toml` server is not detected and a relative `CODEX_HOME` is not resolved; the manage footer reports install records, not live client configs; a consulted turn doesn't decrement the cooldown; `Usage.Add` fsyncs under `e.mu`; `paneClosed` matches an error's text suffix; spend after the last dot is detached isn't shown. Reason: the agent can't approve anything itself, budgets cap spend, and the rest is cosmetic.
- **Windows (Phase 6):** **a residual race between `Purge` and a concurrent writer** [W1]; `EINVAL`/`ACCESS_DENIED` opens during `Purge` aren't retried and a reader can recreate a purged project as an empty directory [W2]; files created with `CreateFile` inherit the private directory's ACL; stale `herdr-dots-run-*` directories aren't swept at start; a Windows user name containing `%` or `!` can't run npm shims (refused, not run). Reason: Windows is beta and not run here (user gate); the private directory protects the files; the lock identity check closes the acknowledged-write loss.
- **Write-capable dots (Phase 7):** the `old mode 120000 → 100644` branch is dead in practice; `FlagAgentConfig` lives in a heavier package than it needs; `x` discards a patch card without a confirm; the `ui/diff` sweep test takes about 30 s. Reason: everything fails closed or is cosmetic.
- **Proactive dots (Phase 8):** no cross-session daily reservation (overshoot of at most one run per session); the daily toast is per engine; `route()` has no re-check after `Add`; `charge()` has no generation check; a huge finite budget is accepted; inbox claims compare-and-delete by token (a suspend longer than the TTL duplicates a card, never loses one); corrupt inbox asides aren't cleaned up; a commit counts as reviewed once one pane's run starts, so a second pane with the same dot gets no card for it [R4]. Reason: bounded spend, no unapproved action, and research can't send file contents anywhere but a search query.
- **Profiles and sharing (Phase 9):** local dot args can still use `claude` short forms (`-c`, `-r`), combined short flags and flags such as `--chrome`, `--cloud`, `--remote-control`, `--teleport`, `--from-pr`, `--file`, `--debug-file`, `--bg`, `--permission-prompts`, and `codex` `--output-schema`/`-i` file reads (only the developer's own dot files can carry them: imported and project dots keep no args but `--model`/`--effort`) [B9]; a scheduled project dot runs the first checkout's copy only; pruning stops scheduled project dots in idle checkouts (fails safe); chair spend is not in the history totals (usage budgets count it); one full-suite flake was seen once and not identified (`TestTwoEnginesRunASlotOnce` also timed out once under heavy parallel load). From Phase 9's final fix wave (after `dffabbf`): the templates' delivery mode isn't pinned by a test. Reason: developer-controlled input, fail-closed behavior, bounded cost or test-only.
- **Snapshots (Phase 10):** a pane with more than 100 visible cards shows the first 100 (most severe, oldest) and the rest wait unseen until earlier ones are resolved; the header's QUEUE count includes them (Task 10). Reason: the F9 snapshot budget; a card is shown whole or not at all.
- **Platforms:** `windows/arm64` is built and published but never tested; Linux and Windows have only CI and cross-compiled tests here (user gate); Codex paths are documented-format, not live-verified (user gate).

## Full accounting, by area

Source tags: `v1 T5` is v1 Task 5; `P4 T7` is Phase 4 Task 7; "final" is that
phase's final review or fix wave; "parked" is a "Parked → Phase 10" line;
"NOT in scope" is a final-fix-brief's deferred list.

### 1. Secrets, credentials and privacy

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| S1 | Zero-width and other invisible characters split a key past the scanner | Phase 2 (plan decision 20) | FIXED T9 | `secrets.Scan` also scans without Cf/Mn/Me; `TestInvisibleCharactersDoNotHideASecret`, `TestInvisibleCharacterStrippingKeepsOrdinaryTextClean` |
| S2 | `secretValue` flags any assigned value with a digit (no false-positive test) | P2 T3 | ACCEPTED | errs toward refusing; `TestScanSecretsIgnoresLookAlikes` pins the look-alikes that pass |
| S3 | `Bearer` + prose and `tokenizer=` false positives | P2 T3 fix round | ACCEPTED | a false positive refuses or asks for confirmation, never leaks |
| S4 | Unicode bidi controls in model text (approval-screen spoof) | plan decision 20 | FIXED EARLIER | P8-R10; `TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners` |
| S5 | `CleanEnv` returns nil, so the child inherits the full environment (FLAG) | v1 T8 | FIXED EARLIER | v1 Task 9 fix round 1; `TestCleanEnvNeverReturnsNil` |
| S6 | `ExecRunner` doesn't apply `CleanEnv` itself | v1 T8 | FIXED POST-RC | `ExecRunner` applies `CleanEnv` itself; `TestExecRunnerCleansTheEnvironmentItself` |
| S7 | `CleanEnv` strips few session markers | v1 final | ACCEPTED | HERDR_*, CLAUDECODE and the Claude session markers go; auth/config `CLAUDE_*` must pass (v1 ruling) |
| S8 | Memory edit temp file in `os.TempDir` holds plaintext (FLAG) | P2 T14 | FIXED EARLIER | Phase 2 final fix 8; `TestMemoryEditRemovesItsTempDirectory` |
| S9 | CLI import skip message prints the entry text (may be a secret) | P2 T14 | FIXED EARLIER | Phase 2 final fix 9: "skipped entry N: err" only (`cmd/herdr-dots/memory.go`) |
| S10 | `credentials.toml` read-modify-write has no file lock | P4 T2 | FIXED POST-RC | `Set` and `Remove` read, change and write under the `credentials.toml` lock; `TestConcurrentSetsKeepEveryKey`, `TestCredentialsMigrationTakesTheKeysSetLock` |
| S11 | Keys from environment variables are not shape-checked | P4 T2 | ACCEPTED | the developer's own environment; a bad key fails at the provider with a redacted error |
| S12 | `Last4` slices bytes | P4 T2 | FIXED POST-RC | `Last4` counts runes; `TestLast4NeverSplitsACharacter` |
| S13 | Other query-string key names are not redacted by name | P4 T2 | FIXED POST-RC | `Redact` removes credentials in `?`/`&` query parameters by name; `TestRedactRemovesQueryStringCredentialsByName` |
| S14 | SDK-chain credentials (`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY`) not passed to `Redact` | P4 T10, P4 NOT in scope | FIXED T9 | Round 1 (P10-R7): on the SDK chain the adapter hands them to `credentials.Redact` by value (never the returned key); round 2 adds `ANTHROPIC_IDENTITY_TOKEN` and the `ANTHROPIC_CUSTOM_HEADERS` values; `TestSDKChainTokensAreRedactedFromErrors`, `TestSDKChainKnownSecretsCoverHeadersAndIdentityToken` |
| S15 | Same-host redirect allowed `https`→`http` | P4 T12, P4 NOT in scope | FIXED T9 | Round 1 (P10-R7): `SameHostRedirect` also refuses a scheme change; `TestSameHostRedirectRefusesASchemeChange` |
| S16 | MCP `read_memory` returns global entries of dots not attached to the pane | P5 NOT in scope | FIXED POST-RC | `read_memory` leaves out global entries of dots not attached to the pane; `TestReadMemoryLeavesOutGlobalEntriesOfUnattachedDots` |
| S17 | `git push --mirror` would push the private snapshot refs | P7 NOT in scope | ACCEPTED | by design: only a mirror push or an explicit `refs/*` refspec sends the snapshot refs (git's own push); keeping the objects alive another way would change Phase 7's ref-based recovery; the README states how long the refs live |
| S18 | `tool_deny_globs` not in the claude-cli research settings | P8 parked | FIXED T9 | Round 1 (P10-R7): `researchSettings` takes the config's deny globs (`Options.Deny` → `ClaudeCLI.Deny`); `TestClaudeResearchDeniesTheConfiguredGlobs` |
| S19 | Feed learned/error detail and `c.Error` rendered raw in the view | P8 T15 | ACCEPTED | sanitized when stored; a second pass at draw time would be defence in depth |
| S20 | Key-like keys (`api_key`, `token`) in a dot file are silently ignored | P4 T5, P4-R10 | ACCEPTED | Phase 9: project and imported dots refuse unknown keys (`DecodeStrict`) and shares refuse secret-looking values (`Encode`); a local file's stray line is never used or sent |
| S21 | Research runs `--restricted`, which ignores the developer's own `~/.claude` deny rules | P8 parked | ACCEPTED | `--restricted` is what keeps repository settings out; research still denies every built-in secret glob and the config's `tool_deny_globs`, has no shell and no WebFetch, and (post-rc) denies reads of home secrets and the config directory, which covers the usual secrets a user-level deny rule would |
| S22 | Commit reviews sent the whole `git show` patch, including committed files the deny set covers (`.env*`, `*.pem`, `tool_deny_globs`) | re-review (P10-R9) | FIXED T9 | Round 2: `commitPatch` leaves out the content of every file `toolloop.Denied` matches (DefaultDeny, DraftDeny, config globs, case-folded) and adds "N denied file(s) left out of this review"; the file list may name them; with more kept files than fit one command line the patch is left out; `TestCommitReviewLeavesDeniedFilesOut` (also from a subdirectory pane) |
| S23 | SDK-chain credentials the adapter can't redact by value: tokens read from files (an `ant auth` profile chosen by `ANTHROPIC_PROFILE`/`ANTHROPIC_CONFIG_DIR`, `ANTHROPIC_IDENTITY_TOKEN_FILE`) and access tokens the SDK gets by federation exchange | re-review | PARTLY FIXED POST-RC | the identity token file's content is now redacted by value (`TestSDKChainKnownSecretsCoverTheIdentityTokenFile`). Still accepted: tokens from an `ant auth` profile and tokens the SDK gets by federation exchange (profile resolution is private to the SDK; exchanged tokens exist only inside it). Used only when no key is in `credentials.toml`; `Redact` still removes every known key shape and `Authorization`/`x-api-key` values |
| S24 | Codex dots read the repository through their own sandbox (`--sandbox read-only`), so `RepoRead`, `tool_deny_globs` and the invalid-glob switch don't limit what they read | re-review | ACCEPTED | `codex exec` always has its shell and file reading; Codex is documented-format and not live-verified here (user gate X3); listed in the README-bound known issues |
| S25 | A commit review leaves out denied files by name, so a denied file renamed in the commit to a non-denied name sends its content under the new name | re-review (P10-R9), Task 9 round 2 | FIXED POST-RC | `commitPatch` lists changes with `git show --name-status -z --find-renames --find-copies`; a rename or copy whose source or destination is denied keeps its content out; `TestCommitReviewLeavesARenamedDeniedFileOut`, `TestParseNameStatus`. A new file copied from an unmodified denied file isn't detected (content-based, outside the name rule) |

### 2. Memory (Phase 2, VITAL) and projects

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| M1 | Nested-repo `FindProject` prefix match: the first project by name won | Phase 2 (plan decision 20) | FIXED T9 | deepest root wins; `TestFindProjectPrefersTheNestedRepository`, `TestFindProjectKeepsNestedReposAndWorktreesApart` |
| M2 | Memory CLI in a nested repo no pane has attached resolved to the outer project | found in Task 9 | FIXED T9 | Round 1 (P10-R7): git first: a g- id is the project; a relative `--project` resolves against the CLI's cwd. Round 2: when git gives only a path identity (no commits, a shallow clone, git failing or timing out), a recorded project counts only if its root is at or below the nearest `.git` on the way up, so an outer project never stands in; `memory purge --project PATH` refuses a path that doesn't exist; `TestMemoryAddInAnUnattachedNestedRepoUsesTheNestedProject`, `TestMemoryPurgeOfANestedRepoPathLeavesTheOuterProject`, `TestStaleDeeperPathProjectDoesNotBeatTheRepository`, `TestPathIdentityStillFindsARecordedProject`, `TestNestedRepoWithAPathIdentityNeverFallsBackToTheOuterProject`, `TestGitFailureInANestedRepoNeverFallsBackToTheOuterProject`, `TestMemoryPurgeRefusesAPathThatDoesNotExist` |
| M3 | `r` doesn't reset `MemoryPasses` | P2-R3 | ACCEPTED | self-correcting on the next session |
| M4 | `memory_enabled = false` still runs git and writes `meta.json` on attach | P2-R3 | ACCEPTED | git runs with repository programs disabled; `meta.json` holds name and roots only |
| M5 | A suppressed verdict still sets the cooldown | P2 T10, P2 NOT in scope | ACCEPTED | one review skipped at most |
| M6 | Edited-then-dismissed records the edited body | P2 T10, P2 NOT in scope | ACCEPTED | the text the developer saw last is what is recorded |
| M7 | Stale stats after memory is switched off at runtime | P2 T10, P2 NOT in scope | ACCEPTED | refreshed on the next snapshot |
| M8 | Outcome `Session` is taken at record time | P2 T10 | ACCEPTED | a card sent after a session change may not suppress across that change |
| M9 | `Remember(AsFinding)` lacks `Session`, so user findings never suppress | P2 T10 | FIXED EARLIER | Phase 2 final fix 1 |
| M10 | `validEntryID` accepts `m+3`/`m007` | P2 T3 | FIXED POST-RC | ids must be canonical; `TestParseRefRefusesNonCanonicalIDs` |
| M11 | `FirstSentence` cuts at abbreviations | P2 T3 | ACCEPTED | display text only; changing `FirstSentence` would change the text of derived entries and dedup matches |
| M12 | `Similar` is unbounded on very long inputs | P2 T3 | ACCEPTED | memory texts are capped at 500 runes |
| M13 | Git discovery walks above cwd | P2 T3 | ACCEPTED | by design: a subdirectory belongs to its repository (`rev-parse --show-toplevel`) |
| M14 | Duplicated remember-slice logic; no Paths/Text caps in `cleanOp`; verdict-flavoured line in the memory pass prompt; Learn+Force and Codex ops untested | P2 T4 | ACCEPTED | caps and clamps are enforced in `Apply` (500 runes, scope/kind, own entries only) |
| M15 | No final tie-break in memory block order | P2 T8 | FIXED POST-RC | ties break by scope, then dot, then id number; `TestSelectBreaksScoreTiesByScopeDotAndID` |
| M16 | Ref fields not one-lined; no end marker on the block; weak random budget test; untested scoring terms | P2 T8 | ACCEPTED | refs are validated ids (canonical id, slug), so they can't hold whitespace; an end marker would change the Phase 2 prompt format (spec); the block sits in the user prompt only |
| M17 | Scrub misses merged-evidence lines and can over-delete project ledger lines across dots | P2 T6 | PARTLY FIXED POST-RC | a card id matches only in the entry's own session (or any session when the entry has none) and, for a project entry, only on the proposing dot's lines; an "Applied patch" finding also scrubs the cards it merged; `TestForgetScrubMatchesACardOnlyInItsSessionAndDot`, `TestForgetScrubRemovesEveryAppliedPatchLineItMerged`, `TestForgetScrubMatchesACardWhenTheEntryHasNoSession`. Still accepted: an entry that merged several cards keeps only its last card, so when a later merge rewrote its text the scrub can't find the older cards' lines by similarity (fixing it needs a list of merged cards on the entry, a schema change); text matches for project entries still cover every dot (errs toward forgetting) |
| M18 | Scrub ignores `.corrupt` files | P2 T6 | FIXED EARLIER | P7-R2; `TestDeleteDotRemovesItsCorruptAsidesOnly` (mixed project asides need `memory purge`, documented) |
| M19 | Unparseable ledger lines dropped on rewrite | P2 T6 | PARTLY FIXED POST-RC | an append after a crash's half-written line starts on a fresh line (`TestLedgerAppendAfterAHalfWrittenLineKeepsTheNewLine`). Unparseable lines are still dropped on rewrite, deliberately: a scrub can't tell whose line it is, so keeping it could keep text the developer asked to forget |
| M20 | `readScope` doesn't check kind/status; `idNum` accepts signs; `prepare` doesn't check `Source.Valid`; store test gaps | P2 T6 | PARTLY FIXED POST-RC | a hand-edited non-canonical or overflowing id gets a fresh id instead of being lost, and `prepare` refuses an unknown source (`TestHandEditedNonCanonicalIDsAreRenumberedNotLost`, `TestAnOverflowingHandEditedIDIsRenumberedNotLost`, `TestAddRefusesAnUnknownSource`). Still accepted: `readScope` doesn't check kind or status (refusing would make a newer version's file count as corrupt and move it aside) |
| M21 | `Purge` removes a held lock file | P2 T6 | FIXED EARLIER | Phase 6 final fix 3 (lock identity check in `withLock`) |
| M22 | `DeleteDot` leaves ledger lines, corrupt asides and orphaned proposals | P2 T6, P2 completion note, P2 NOT in scope | FIXED EARLIER | Phase 7 Task 0; `TestDeleteDotScrubsItsLedgerAndOrphanedProposals` |
| M23 | Weak or vacuous apply tests (auto-approve, other-dot) | P2 T7 | ACCEPTED | test-only; the rules are pinned by later VITAL tests |
| M24 | Prune can delete archived pinned entries with a zero `UpdatedAt` | P2 T7 | FIXED POST-RC | pinned entries are never deleted; age is `UpdatedAt`, else `CreatedAt`, and an entry with neither is kept; `TestPruneNeverDeletesPinnedAndAgesZeroUpdatedAtFromCreatedAt` |
| M25 | `RecordOutcome` writes the ledger and the entry under two locks | P2 T7 | FIXED POST-RC | one hold of the project lock for the ledger line and the entry; `TestRecordOutcomeWritesLedgerAndEntryUnderOneLock`. A crash between the two writes can still leave a ledger line without its entry |
| M26 | `Apply` doesn't `ValidSlug(dot)` up front | P2 T7 | ACCEPTED | every op with an invalid slug is rejected by `checkTarget`/`prepare` before any path is built; an up-front error would change `Apply`'s rule that its error means a store failure |
| M27 | `op.Paths` not sanitized or scanned in `Apply` | P2 T7 | FIXED EARLIER | Phase 2 final fix 4 |
| M28 | Malformed config keeps `MemoryEnabled = true` | P2 T5 | FIXED EARLIER | Phase 2 final fix 6; `TestMalformedConfigKeepsLastGoodButDisablesInject` |
| M29 | An unregistered project id creates its directory | P2 T14 | ACCEPTED | by design: `memory import --project <id>` before a project is attached is legitimate; the result is an empty directory for a valid id |
| M30 | Partial import when the project can't be resolved | P2 T14 | FIXED EARLIER | Phase 2 final fix 10 |
| M31 | `memory add` uses `--force` instead of the secret gate | P2 T14 | ACCEPTED | P2-R7: explicit consent |
| M32 | Forget-all dot prompt lacks the project; interactive confirm untested | P2 T14 | FIXED EARLIER | Phase 2 final fix 11 (names the project, interactive test) |
| M33 | `reject` = Forget, so the agent can re-propose a rejected proposal | P5 T4, P5 NOT in scope | ACCEPTED | proposals never apply without approval; pending agent proposals are capped at 20 (P5-R5) |
| M34 | Full-scope proposals are archived soon after | P5 final | ACCEPTED | they are proposals; approved memory is never displaced (P5-R5) |
| M35 | Newer research loses to older under the Phase 5 gate; research not deduped against archived entries; `ErrDenied` prefix misleading; aside regex not scoped to `dots_seen` | P8 T6 | ACCEPTED | research memory is the first evicted and never suppresses alone |
| M36 | `DeleteDot` can recreate a bare lock directory after a concurrent `Purge` | P7 T0, P7 NOT in scope | FIXED POST-RC | `DeleteDot` removes a project directory left holding only `memory.lock` after a concurrent purge; `TestDeleteDotLeavesNoBareLockDirectoryAfterAConcurrentPurge` |
| M37 | `keysConfirmMemory` drops counts on a partial error; no test for proposed SourceUser survival or ErrLocked partial failure | P7 T0, P7 NOT in scope | ACCEPTED | message and test only |

### 3. Git and repository safety

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| G1 | Git config execution in hostile repos (fsmonitor, hooks, transports) | Phase 2 final review | FIXED EARLIER | Phases 6–8; `TestExecGitNeverRunsHooks`; pinned by Task 9 (row G2) |
| G2 | No guard that every git call carries the repository-program pins | plan decision 21 | FIXED T9 | `TestExecGitRunsNothingTheRepositoryConfigures`, `TestEveryGitCallDisablesRepositoryPrograms` (all five pins as literals; `internal/worktree` covered; known call sites must be found) |
| G3 | A pane in a repository subdirectory walks the tree without `.gitignore` | P4 T7 | ACCEPTED | listing a subdirectory with `.gitignore` means running `ls-files` from the repository top while keeping the pinned `--git-dir=<root>/.git` design (a repository's config must not redirect the listing), a security-sensitive redesign; the jail and deny list still apply |
| G4 | No `-c core.precomposeunicode=false`: tracked NFD names invisible in the listing | P4 T7, P4 NOT in scope | FIXED POST-RC | `list_files` runs git with `-c core.precomposeunicode=false`; `TestListShowsAFileWithADecomposedName` (macOS) |
| G5 | NFC/NFD forms of non-ASCII deny globs differ; per-file root re-resolution cost | P4 T7 | FIXED POST-RC | `toolloop.Denied` tries the name as given, NFC and NFD, for both the name and the glob; `TestDeniedMatchesBothNormalizationForms` |
| G6 | Unbounded git output in the listing | P4 T7 | ACCEPTED | bounded by the run timeout; a byte cap on `ls-files` would also quietly narrow `grep` in very large repositories |
| G7 | Two engines on one repository drop each other's snapshot refs | P7 parked | ACCEPTED | by design (X8); a dropped ref only ends that draft |
| G8 | `CheckPath` doesn't strip HFS-ignorable code points (`.g‌it`) | P7 T6 | ACCEPTED | git refuses HFS-equivalents of `.git` (`core.protectHFS`, on by default on macOS); APFS doesn't ignore them |
| G9 | No per-run write byte cap; errors show the absolute worktree path; a trailing slash is refused; no adapter-level `Select` test | P7 T6, P7 NOT in scope | ACCEPTED | writes are bounded per run at `WriteMaxCalls` (40) × `MaxWriteBytes` (1 MB) for API dots; a `claude-cli` dot writes with Claude Code's own tools, which no toolloop cap reaches, and the patch cap (G12) bounds what is kept; the path is the developer's own temp dir; the trailing-slash refusal fails closed |
| G10 | `strayOps` matches "index" inside base85 binary data | P7 T3, P7 parked | ACCEPTED | fails closed, about 1 in 85⁴; skipping binary sections would loosen a guard against stray header lines |
| G11 | The `old mode 120000 → 100644` branch is dead in practice | P7 T3, P7 NOT in scope | ACCEPTED | harmless |
| G12 | Capture buffers before the size cap | P7 NOT in scope | FIXED POST-RC | `worktree.CaptureMax` keeps at most `max_patch_bytes + 1` bytes of the diff and drains the rest; a cut patch is rejected as too large; `TestCaptureMaxNeverKeepsMoreThanTheCap`, `TestADraftOverMaxPatchBytesIsRejectedFromACappedCapture` |
| G13 | A small window between proactive-off and a review starting on new heads | P8 T13 | ACCEPTED | at most one review; closing the window needs a proactive-switch check between the HEAD check and the spawn in the poll path |
| G14 | Jail root `/System/Volumes/Data` (or a home ancestor under it) got tools and could read `~/.zshrc` | P4 T7 | FIXED EARLIER | Phase 4 final fix 3 (home reachable under the root makes it unavailable) |

### 4. Engine lifecycle, locks and concurrency

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| E1 | `lockfile.Held` probes with the real lock | v1 T17 | FIXED POST-RC | a starting engine waits up to 500 ms for the session lock, so a `Held` probe no longer makes it exit; `TestEngineLockOutwaitsABriefProbe` |
| E2 | toggle-pane read-modify-write not serialized | v1 T17 | FIXED POST-RC | `togglePane` holds a file lock around its read-modify-write of `dotspanes.json`; `TestConcurrentTogglesKeepBothPanes` |
| E3 | A crash-looping engine respawns about every 6 s; "not ready" doesn't point at `engine.log` | v1 T17 | PARTLY FIXED POST-RC | the not-ready error now ends with the `engine.log` path (`TestEnsureEngineNotReadyPointsAtTheLog`). Still accepted: a crash-looping engine respawns about every 6 s (a backoff needs persistent crash state) |
| E4 | `e.Wait` doesn't cover in-flight IPC handlers | v1 T17 | FIXED EARLIER | Phase 5 Task 2: `ipc.Serve` waits for every connection; `serveEngine` calls `e.Wait` after it |
| E5 | togglePane/serveEngine loser path untested | v1 T17 | FIXED POST-RC | `TestServeEngineLoserReturnsQuietly`; the toggle loser path is covered by `TestConcurrentTogglesKeepBothPanes` |
| E6 | `Dismiss`/`Edit` don't take `dmu` (FLAG) | v1 T14 | FIXED EARLIER | v1 final fix 11 |
| E7 | `Attach` Get→Put overwrites concurrent updates (FLAG and its follow-up) | v1 T14 | FIXED EARLIER | v1 final fix 12 (upsert) and Phase 2 Task 0 |
| E8 | Attach upsert with a stale base resurrects a dot detached concurrently | v1 final residual | FIXED EARLIER | Phase 2 Task 0 |
| E9 | `route` adds cards for a pane dropped mid-run; `route()` has no re-check after `Add` | v1 T14 (FLAG), P8 parked | ACCEPTED | a re-check in `route()` after `Add` crosses the lock-order and generation design (Phases 7 and 8), which close the common paths; a card added in the same instant can't be delivered to a gone pane |
| E10 | Stale reader re-cached after unwatch | v1 T14 | ACCEPTED | caching readers only for subscribed panes would change reader lifetime for Summon and Attach; the reader is re-selected on a session change |
| E11 | Per-pane maps (`paneMu` and others) never cleaned on `dropPane` | v1 T14 | ACCEPTED | deleting `paneMu` while a holder is in flight would let a second mutex break pane serialization; a few bytes per closed pane |
| E12 | Pane-budget toast once per dot, not once | v1 T14 | FIXED POST-RC | one per-pane warn key for the pane budget toast, cleared on a new agent session and in `dropPane`; `TestPaneBudgetToastsOncePerPane` |
| E13 | Discarded store errors not logged | v1 T14 | ACCEPTED | logging every discarded store error would spam the log in read-only mode, where every write fails; each store reports corruption on its next read |
| E14 | `SendCards` partial approval | v1 T14 | ACCEPTED | each card's status is saved on its own |
| E15 | `Start` doesn't process restored panes | v1 T14 | ACCEPTED | processing restored panes at `Start` would start paid reviews at engine start, a product behaviour change; the next idle event processes them |
| E16 | Engine test gaps (merge path, 3-failure stop, `InjectEnabled=false`, done/blocked, timeout, `MaxParallelRuns`) | v1 T14 | ACCEPTED | test-only; later phases cover several |
| E17 | A reader swap to one with at least as many turns can skip or re-review | v1 T14 | ACCEPTED | the accepted tradeoff in the ledger |
| E18 | `SetPaused(false)` shows Idle during an in-flight run; `SetMode` copy→save window; Handle routing tests | v1 T16 | PARTLY FIXED POST-RC | resuming a dot whose run is still going shows thinking (`TestResumeDuringARunShowsThinking`). Still accepted: the `SetMode` copy→save window and the Handle routing tests (cosmetic or test-only) |
| E19 | Metadata-only events are not deduped | v1 final | ACCEPTED | `processPane` is a no-op for them |
| E20 | Full transcript re-parse each pass | v1 final | ACCEPTED | performance; the 60 s soak shows 0.58 % idle CPU |
| E21 | Broadcast ordering race | v1 final | FIXED EARLIER | Phase 10 Task 4; `TestAnOlderSnapshotNeverOverwritesANewerOne` |
| E22 | `pausedAll` not persisted | v1 final | ACCEPTED | pause-all lasts for the engine's life |
| E23 | Deferred approvals ignore the focus guard | v1 final | ACCEPTED | the developer approved the card; delivery still waits for idle |
| E24 | `Decide` relies on budget ≥ 1; precedence and no-touched-paths tests; doc comments; `doublestar.Match` error discarded | v1 T12 | ACCEPTED | config clamps budgets to ≥ 1; a bad glob never matches |
| E25 | A consulted turn doesn't decrement the cooldown | P5 T7, P5 NOT in scope | ACCEPTED | one review skipped at most |
| E26 | Feed/result disagreement window | P5 T7, P5 NOT in scope | ACCEPTED | the feed catches up on the next line |
| E27 | Ask concurrent with a council on the same pane (both directions) | P5 T7, P5 T9 | FIXED EARLIER | P5-R2 and Phase 5 final fix 1 (a member being asked is skipped) |
| E28 | `askable`→`trackAsk` window; `ErrPaneClosed` text | P5 T7 | FIXED EARLIER | Phase 5 final fixes 5 and 6 |
| E29 | Post-council feed and `markConsulted` run on the IPC goroutine | P5 T9, P5 NOT in scope | ACCEPTED | no store I/O under engine locks; tracked by `ipc.Serve`'s wait |
| E30 | `Usage.Add` fsyncs under `e.mu`; usage writes under `u.mu` while getters run under `e.mu` | P5 final, P4 T14, P4 NOT in scope | ACCEPTED | a small local file; latency only |
| E31 | `paneClosed` matches an error's text suffix | P5 final | ACCEPTED | a mismatch gives a less clear message, not a wrong action |
| E32 | A user summon is not in `running[k]` | P5 final | ACCEPTED | a developer action, budgeted |
| E33 | Status re-created after pane close by normal runs | P5 final | FIXED POST-RC | `setStatus` skips a pane that is no longer attached; `TestRunEndingAfterPaneCloseLeavesNoStatus` |
| E34 | Turn-review budget checks ignore in-flight asks | P5 final | FIXED EARLIER | P8-R8, Phase 8 final fix 3; `TestTurnReviewCountsAnAskInFlight` |
| E35 | Spend after the last dot is detached isn't shown | P5 final | ACCEPTED | usage still counts it |
| E36 | Council wording "the dot can no longer be asked" | P5 final | ACCEPTED | cosmetic |
| E37 | Detach+re-attach of the same dot during a held draft; `Apply`'s final status write not under `dmu` | P7 T8, P7 NOT in scope | ACCEPTED | the draft's attachment generation refuses a stale result |
| E38 | Detach decides "empty" outside `e.mu` | P7 T8 | FIXED EARLIER | Phase 7 final fix 9 |
| E39 | Detach's empty path lacks `OrphanPane` | P8 parked | FIXED EARLIER | Phase 8 final fix 6; `TestDetachingTheLastDotEndsThePane` |
| E40 | deliver vs `OrphanPane` `SetStatus(Sent)` race; `Pending`+`OrphanPane` two-call race | P8 parked, P9 T7, P9 NOT in scope | ACCEPTED | needs the pane to close inside one delivery call; the effect is on that one card: its final status can read orphaned although its text reached the agent, and its sent/orphaned outcome can be missing from memory and history (so that finding may repeat once). Recorded here as a possible loss of one outcome record |
| E41 | `charge()` has no generation check | P8 parked | ACCEPTED | a charge after a pane id is reused lands on the new pane; bounded by one run |
| E42 | `Claim` doesn't return added ids (claimInbox can orphan others' cards); misleading log when `dropPane` already orphaned; no mid-Add pane-close test | P8 parked, P8 NOT in scope | ACCEPTED | orphaned cards stay visible; never lost |
| E43 | A post-send non-critical part makes a card at once while away; a failed `Add` keeps the old away-period tag; `flushAway` may wait on `dmu` holding `bgSem` | P8 T11 | ACCEPTED | one extra card or a short wait |
| E44 | Shutdown mid-chair still charges the chair; residual check→Add window; chair slot wait bounded only by `run.ctx` | P3 T6, P3 NOT in scope | ACCEPTED | bounded by one council |

### 5. Queue, state, feed and usage

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| Q1 | Queue state changes in memory before the save, with no rollback | v1 T10 | ACCEPTED | with Phase 10's read-only gate this is the intended read-only mode |
| Q2 | `Add` accepts any `Status`; `Open` trusts the persisted `next`; `save()` reorders cards; test gaps | v1 T10 | PARTLY FIXED POST-RC | `Open` raises `next` to the highest numeric card id (`TestOpenNeverReusesACardID`). Still accepted: `Add` accepts any `Status` and `save()` reorders cards (internal callers; display order is computed); test gaps |
| Q3 | Feed `Append` remembers before the write; attachments/usage change memory before `writeJSON` | v1 T11 | ACCEPTED | no data loss on disk |
| Q4 | The feed loader ignores a scanner error and glues onto a truncated last line | v1 T11 | FIXED POST-RC | the loader logs unreadable lines and a scanner error, and the next append starts a new line after a truncated one; `TestFeedLogsATruncatedLastLineAndAppendsAfterIt`, `TestFeedLoadOfAWholeFileLogsNothing` |
| Q5 | `Feed.Recent(n<0)` panics | v1 T11 | FIXED T10 | `TestOpenFeedMakesAnOldLooseFileOwnerOnly` |
| Q6 | Feed rotation rename error dropped | v1 T11 | FIXED POST-RC | a failed feed rotation is logged; `TestFeedRotationFailureIsLogged` |
| Q7 | `O_CREATE` doesn't tighten an existing feed file's mode | v1 T11 | FIXED T10 | `TestOpenFeedMakesAnOldLooseFileOwnerOnly` |
| Q8 | Token sums, `List` deep copy and file modes untested; tests discard errors | v1 T11 | ACCEPTED | test-only; Phase 6 added `platformtest.AssertPrivateFile` checks |
| Q9 | No parent-directory fsync after an atomic write | v1 T3 | FIXED POST-RC | `WriteFileAtomic` syncs the parent directory after the rename (best effort, errors ignored); `TestWriteFileAtomicSyncsTheParentDirAfterTheRename`, `TestSyncDirIsBestEffort` |
| Q10 | Usage dollars are float64; no concurrency test | P4 T6 | ACCEPTED | the daily budget uses int64 microdollars (P8); session sums are display-grade |
| Q11 | Inbox claims compare-and-delete by token; Origin whitelist not tied to `state.Run*`; corrupt inbox asides not cleaned; `Add` uses `time.Now` | P8 T7 | ACCEPTED | a suspend past the TTL duplicates a card, never loses one |
| Q12 | Daily asides: an aside dated after today, a stale side file with today's aside, an unparseable aside | P8 T3 parked | ACCEPTED | conservative: each case blocks or counts as a day already used, except a clock stepped back across a crash (one extra day's budget at most) |
| Q13 | No cross-session daily reservation; the daily toast is per engine | P8 parked, P8 NOT in scope | ACCEPTED | overshoot of at most one run per session |
| Q14 | More than 100 visible cards: the pane didn't say how many were hidden | Phase 10 | FIXED T10 | Task 10 carry-over: QUEUE count = `len(Cards)+HiddenCards` |
| Q15 | Cards past the 100-card snapshot cap wait unseen until earlier ones resolve | Phase 10 (F9) | ACCEPTED | snapshot budget; a card is shown whole or not at all |
| Q16 | The CLI gate (`protectNewerFiles`, a dry run) reports files that need a migration as pending but doesn't keep them read-only, so a writing command could rewrite an older-version file without its migration or backup | P10 T7 parked | ACCEPTED | ledger-only acceptance at Task 7: no kind has a migration yet (every kind is at version 1, and a missing version means 1), so no file can be pending; the rule for the first migration is in `CONTRIBUTING.md` and `internal/migrate/kinds.go` |
| Q17 | Deleting a dot from the manage popup bypasses `CheckWritable` (the read-only gate for files a newer version wrote) | P10 T7 parked | ACCEPTED | ledger-only acceptance at Task 7, the developer's request: deleting is the developer's explicit action on their own dot file |
| Q18 | The read-only toast said "written by a newer herdr-dots" for failed migrations too | P10 T7 parked | FIXED FINAL | it says "written by a newer herdr-dots, or could not be migrated; see engine.log" (`TestReadOnlyNoticeNamesTheFiles`) |
| Q19 | `queue.Store.save` re-marshals the whole queue (indented JSON, every card's full text) on every card change: under the soak's load that allocated about 9.5 GB per 10 minutes. CPU and short-lived memory only, nothing kept | 8-hour soak investigation (post-rc) | ACCEPTED | peak RSS stays within budget (45.3 MB at 20 minutes under soak load); a compact or incremental format is a follow-up |

### 6. IPC and the Herdr client

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| I1 | `Call` and the Herdr client honor a deadline but not cancellation | v1 T5, v1 T15 | FIXED POST-RC | `ipc.Client.Call` and the Herdr client honour cancellation and return the context's error; `TestCallHonorsCancellation` (ipc and herdr) |
| I2 | `Subscribe` returns a wrapped closed-connection error on cancel | v1 T5 | FIXED POST-RC | the subscribe ack wait returns the context's error; `TestSubscribeCancelReturnsTheContextError` |
| I3 | Herdr fake and client test gaps (shared encoder, bogus PaneInfo, list order, hardcoded `w1`, untested methods, ignored `ReportPending` error, unguarded indexes) | v1 T5 | ACCEPTED | test-only |
| I4 | `stream` spins if the Watch channel is closed | v1 T15 | FIXED POST-RC | the stream ends when the watcher channel closes; `TestWatchEndsWhenTheWatcherChannelCloses` |
| I5 | `Serve` doesn't close open connections on cancel | v1 T15 | FIXED EARLIER | Phase 5 Task 2 fix round 1 |
| I6 | No server write deadlines | v1 T15 | FIXED POST-RC | server replies and watch snapshots are written under a 10 s deadline; `TestCommandReplyToANonReadingClientTimesOut`, `TestWatchToANonReadingClientTimesOut` |
| I7 | Test reads `h.args` unlocked; no JSON golden tests; latest-wins untested | v1 T15 | PARTLY FIXED POST-RC | `TestCallRoundTripAndErrors` reads the handler's record under its lock. Still accepted: no JSON golden tests, latest-wins untested (test-only; it can't be pinned deterministically without a client hook); `FuzzIPCDecode` covers decoding |
| I8 | IPC socket authentication | P7 NOT in scope | ACCEPTED | 0600 socket in a 0700 directory, now owner- and symlink-checked in the fallback directory too; Windows pipe owner must be the user (P6-R2) |
| I9 | Shared `mcp.log` rotate race | P5 T10, P5 NOT in scope | ACCEPTED | a log line may land in the rotated file |

### 7. Transcripts and the loop guard

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| T1 | Compact-summary and slash-command records may become turns | v1 T6 | PARTLY FIXED POST-RC | compact summaries and `<local-command-stdout>`/`<local-command-stderr>` echoes are no longer turns (`TestParseClaudeSkipsCompactSummariesAndCommandOutput`), and an attachment from the older parser re-bases `LastSeenTurn` once (`TestAnOldAttachmentAboveTheTurnCountReviewsTheNextRealTurn`, `TestANewAttachmentCarriesTheCurrentParserVersion`). Still accepted: a `<command-name>` record still counts as a turn (only a later record shows whether the command was built-in, and dropping a turn afterwards would shift indexes already counted); at most one extra turn is reviewed |
| T2 | `ResolveClaudePath`'s glob doesn't escape the home path | v1 T6 | FIXED POST-RC | each project folder is listed with `os.ReadDir` instead of globbing the home path; `TestResolveClaudePathUnderAHomeWithGlobCharacters` |
| T3 | Path kind accepts directories; `Turns` ignores ctx | v1 T6 | FIXED POST-RC | `TestResolveClaudePathRefusesADirectory`, `TestClaudeReaderHonoursACancelledContext` |
| T4 | Clip rune-safety test uses ASCII only | v1 T6 | FIXED POST-RC | test-only; `TestFormatClipsNonASCIIOnRuneBoundaries` |
| T5 | Codex payload type mismatch drops the whole record | v1 T7 | FIXED POST-RC | a payload field of another JSON type reads as empty and the rest of the record is kept; `TestParseCodexToleratesUnexpectedFieldTypes` (documented format, Codex not live-verified) |
| T6 | Rollout glob `*` can span dashes | v1 T7 | FIXED POST-RC | the tail after the rollout timestamp must be exactly the id; `TestResolveCodexPathDoesNotMatchAnIDSuffix` (documented format, not live-verified) |
| T7 | `Select` with a nil `Pane` panics | v1 T7 | FIXED POST-RC | a scrollback reader without a pane returns an error; `TestSelectWithoutAPaneReaderDoesNotPanic` |
| T8 | Scrollback drops turns on a read error | v1 T7 | ACCEPTED | the next pass reads again |
| T9 | No `ResolveCodexPath` id-rejection, codex huge-line or `Select` codex/path-kind tests | v1 T7 | FIXED POST-RC | test-only; `TestResolveCodexPathRejectsUnsafeIDs`, `TestParseCodexHandlesHugeLines`, `TestSelectPicksCodexByIDAndByPath` |
| T10 | `TruncateRunes` splits grapheme clusters | v1 final | ACCEPTED | display and inject text only; never a panic since Task 10 |
| T11 | Mixed-prefix loop-guard test | P3 T1 | FIXED EARLIER | Phase 3 final fix 8 |

### 8. Backends, verdicts and parsing

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| B1 | `ParseVerdict`/`ParseReply`/`ParseMemoryOps` take first `{` to last `}` | v1 T8, P8-R7 | FIXED POST-RC | a string-aware balanced scan takes the single object that parses and has the required key, and only when that key appears nowhere else; two candidates stay malformed; the v1 first-`{`-to-last-`}` span is tried first, so no reply accepted before changes; `TestProseBracesAroundTheJSONStillParse`, `TestTwoCandidateObjectsStayMalformed`, fuzzed |
| B2 | `Hint` matches a bare "401"/"login" | v1 T8 | ACCEPTED | a hint, not a decision; the CLIs' real wording is not live-verified, so tightening it could lose the real login hint |
| B3 | `Hint` not applied to an exit-0 `is_error` | v1 T9 | FIXED POST-RC | `is_error` on exit 0 goes through `Hint`; `TestClaudeIsErrorOnExitZeroGetsTheHint` |
| B4 | `codex turn.failed` after an earlier `agent_message` is ignored | v1 T9 | ACCEPTED | Codex not live-verified |
| B5 | `Run` starts the process even if ctx is already cancelled | v1 T8 | FIXED POST-RC | `ExecRunner` checks the context before the lookup and the start; `TestExecRunnerStartsNothingWhenAlreadyCancelled` |
| B6 | `New(kind, nil)` panics on `Run` | v1 T9 | FIXED POST-RC | `ErrNoRunner` instead of a nil dereference; `TestCLIBackendWithoutARunnerFailsInsteadOfPanicking` |
| B7 | Verdict tests for null `relevant`/non-string fields; `Run` paths/`--allowedTools` untested; `live_test` discards the `New` error | v1 T8, v1 T9 | FIXED POST-RC | `TestVerdictWithNullOrWrongTypedFieldsIsMalformed`; the live test checks `New`'s error |
| B8 | Dot `[backend].args` can override the read-only flags | v1 T9 | FIXED EARLIER | v1 final fix 9; P9-R4/P9-R10 deny lists |
| B9 | Local `claude`/`codex` deny-list gaps (`claude` short forms `-c`/`-r`, combined short flags, `--chrome`, `--cloud`, `--remote-control`, `--teleport`, `--from-pr`, `--file`, `--debug-file`, `--bg`, `--permission-prompts`; codex `--output-schema`/`-i` file reads) | P9 parked | ACCEPTED | only the developer's own dot files can carry them; imported and project dots keep only `--model`/`--effort` |
| B10 | Codex dot runs still loaded the developer's other MCP servers | v1 final | FIXED T9 | Round 1 (P10-R7): every server the run's `config.toml` declares gets `-c mcp_servers.<name>.enabled=false`; Round 2: (P10-R8) the name goes in raw, since Codex splits a `-c` override on '=' and the key on '.' with no quoting; `TestCodexDisablesEveryDeclaredMCPServer` |
| B11 | `TestRegistryKeepsV1ConstructorAndAddsKinds` panics under `-count=2` | P4 T8 | FIXED EARLIER | Phase 4 final fix 6 |
| B12 | No `FinishAPI` sanitize test; the unknown-kind error doesn't list kinds | P4 T8 | FIXED POST-RC | `TestUnknownKindListsTheKinds`, `TestFinishAPISanitizesTheReply` |
| B13 | SDK `Retry-After` sleep uncapped and not deadline-pre-checked | P4 T10, P4 T11, P4 NOT in scope | ACCEPTED | the SDK's own retry sleep, which already selects on the context; capping it means replacing the SDK's retries |
| B14 | OpenAI `reasoning.encrypted_content` sent for non-reasoning models | P4 T11, P4 NOT in scope | USER GATE | needs a live OpenAI key |
| B15 | OpenAI `status: failed` / `resp.Error` not surfaced | P4 T11 | FIXED EARLIER | Phase 4 final fix 7 |
| B16 | OpenAI tests: partial usage mid-loop, 400 redaction, strict-schema nullable | P4 T11 | ACCEPTED | test-only; needs new OpenAI fixtures, too large for the hardening wave (400 redaction added by Phase 4 final fix 7) |
| B17 | OpenRouter `schemaRejected` downgrade is broad and permanent | P4 T12, P4 NOT in scope | ACCEPTED | falls back to prompt-only JSON, still parsed and capped |
| B18 | `context.Canceled` reported as a timeout (openrouter, ollama) | P4 T12, P4 NOT in scope | FIXED POST-RC | a cancelled run reads cancelled, not timed out, in openrouter, openai-api and anthropic-api; `TestCancelledRunIsNotReportedAsATimeout` |
| B19 | Refusal text not sanitized before the error | P4 T12 | FIXED EARLIER | Phase 4 final fix 11 |
| B20 | No second-400 test | P4 T12 | FIXED POST-RC | test-only; `TestSecondRejectionAfterTheFallbackFailsTheRun` |
| B21 | `RepoRead` with an empty `Cwd` gets no note | P4 T10 | FIXED EARLIER | Phase 4 final fix 12 |
| B22 | `BaseURL`/`ContextWindow` request fields untested | P4 T14 | ACCEPTED | test-only |
| B23 | `https` required for OpenRouter and non-loopback `base_url`; `u.Hostname()==""` | P4 T5, P4 T12 | FIXED EARLIER | Phase 4 final fix 10 |
| B24 | Research tests: research+Write runner never started, `checkResearchNotWrite` placement, negative tests, a dead loop | P8 T5 | ACCEPTED | test-only; the P8 guards refuse research+write |
| B25 | Web fetch counting test; the pause-cap error text when the budget stops it | P8 T9 | FIXED POST-RC | a web-budget stop says the budget stopped it; `TestWebBudgetStopSaysTheBudgetStoppedIt` |
| B26 | History keeps `server_tool_use` blocks after the web tools drop | P8 T9, P8 NOT in scope | USER GATE | tied to the V5 live web check |
| B27 | No per-run web cap for claude-cli research | P8 T16 | FIXED EARLIER | P8-R14 (no WebFetch) and P8-R15 (`--max-budget-usd` per background run); `TestBackgroundRunsCarryAMaxBudget` (live stop behaviour is a user gate, row X10) |
| B28 | Anthropic adapter test gaps (403/5xx, `max_tokens` stop, partial usage after a tool turn, multi-turn fallback) | P4 T10 | ACCEPTED | test-only; four fixture-heavy gaps; Phase 4 conformance tests pin the VITAL rules |
| B29 | An MCP server whose name holds '.' or '=' can't be disabled from Codex's command line (Codex splits `-c` on '=' and '.', no quoting; P10-R8) | B10 round 2 | ACCEPTED | fails closed: a codex dot run is refused with "rename MCP server '<name>' (Codex can't disable a dotted name from the command line)" and nothing starts; `TestCodexRefusesAServerItCantDisable`; the developer renames that server to use codex dots |

### 9. Pricing, cost and budgets

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| C1 | `CostServed` falls back to the requested model's price when another model served | P4 final | ACCEPTED | overcounts, never undercounts |
| C2 | No pricing test isolates the date-strip step | P4 final | FIXED POST-RC | test-only; `TestCostServedPricesTheUndatedServedModelBeforeTheRequestedOne` |
| C3 | `chairWarned` set when merge isn't `chair` | P4 final | ACCEPTED | one log line |
| C4 | Zero-token failed runs record a known $0 | P4 final | ACCEPTED | P4-R9: no spend happened |
| C5 | A prices override without `as_of` keeps the embedded date | P4 T3 | ACCEPTED | display only |
| C6 | Hard-coded dates in pricing tests | P4 T3 | ACCEPTED | test-only |
| C7 | gpt-6-sol vs gpt-6.1-sol `cache_read` inconsistency | P4 T3 | ACCEPTED | matches the published table (Phase 4 V2) |
| C8 | `costBE.reqs` read without its lock | P4 T14, P4 NOT in scope | ACCEPTED | the race detector stays green over the full suite |
| C9 | No engine-level memory-pass dollar test | P4 T14, P4 NOT in scope | ACCEPTED | test-only |
| C10 | OpenRouter BYOK `usage.cost` is 0 | P4 T12, P4 NOT in scope | ACCEPTED | call budgets still cap BYOK runs |
| C11 | Manage popup reads prices, env names and the Ollama URL once; backend cache keyed on the prices path, not content; duplicated load-and-log helper | P4 T17 | ACCEPTED | a restart picks up a changed prices file |
| C12 | A huge finite budget is accepted; no wrong-type config test | P8 T4 | ACCEPTED | the developer's own setting |
| C13 | Sub-cent budgets render as $0.01 | P8 T16 | ACCEPTED | display only |
| C14 | `tooFrequent`'s 7-day window misses monthly bursts | P8 T8 | ACCEPTED | P8-R12 still caps 96 scheduled runs per dot per day |
| C15 | Chair spend not in the history totals | P9 T7, P9 NOT in scope | ACCEPTED | usage budgets count it |

### 10. Council

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| K1 | `ParseDigest` unclipped, weak shape check | P3 T2, P3 NOT in scope | ACCEPTED | sanitized and fuzzed; shown whole |
| K2 | `Members` null when idle | P3 T2, P3 NOT in scope | FIXED POST-RC | `members: []` in `Plan` and `Select`; `TestPreflightWithNoMembersIsAnEmptyList` |
| K3 | Negative `max_members` clamps to 2 while 0 means 8 | P3 T1, P3 NOT in scope | ACCEPTED | developer's config |
| K4 | Brittle exact-JSON test; "1 dots" grammar; target/name/reasons rendered as written | P3 T4, P3 NOT in scope | ACCEPTED | developer-controlled text |
| K5 | Council card Note line unwrapped | P3 T5 | FIXED EARLIER | Phase 3 final fix 2 |
| K6 | No in-card scroll for a tall expanded card | P3 T5, P3 NOT in scope | ACCEPTED | the digest is always visible |
| K7 | No council-specific reselect test | P3 T5 | ACCEPTED | test-only |
| K8 | Manifest id-uniqueness test; `--council` flag test | P3 T7, P3 NOT in scope | ACCEPTED | test-only |
| K9 | One council can overrun the pane budget between preflight and member runs | P3 T6, P3 NOT in scope | ACCEPTED | bounded by one council |
| K10 | `councilConventionText` strips any trailing `(…)`; numbered bullets not recognized | P3 T6 | FIXED EARLIER | Phase 3 final fix 1; `TestFirstPointSkipsHeadersAndStripsAttribution` |
| K11 | Member runs cancelled by a pane close log feed errors | P3 T8 | FIXED EARLIER | Phase 3 final fix 7 |
| K12 | Council deadline vs slot queueing; digest header inline after the prefix | P3 NOT in scope | ACCEPTED | ordering and layout only |
| K13 | A non-anthropic chair without `chair_model` gets model "" | P4 T14 | FIXED EARLIER | Phase 4 final fix 4 |

### 11. MCP server and installer

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| P1 | A project-level `.codex/config.toml` server is not detected; a relative `CODEX_HOME` is not resolved | P5 T5, P5 NOT in scope | ACCEPTED | `HERDR_PANE_ID` is stripped, so such a server's tools refuse anyway |
| P2 | The manage footer reports install records, not live client configs | P5 T6, P5 NOT in scope | ACCEPTED | display only |
| P3 | `readClaudeEntry` reads EACCES/EIO as "not found"; corrupt `mcp-install.json` message after a successful apply | P5 T6 | FIXED EARLIER | Phase 5 final fixes 7 and 8 |
| P4 | `engineEnv` copies the agent's whole env; silent log-open failure; positional args accepted | P5 T10 | FIXED EARLIER | Phase 5 final fixes 9 and 10 |

### 12. Windows and portability

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| W1 | Residual `Purge` race vs a concurrent writer | P6 T4 | ACCEPTED | lock identity check (Phase 6 final fix 3) closes the acknowledged-write loss |
| W2 | A reader can recreate a purged project as an empty directory; EINVAL/ACCESS_DENIED opens during `Purge` aren't retried | P6 final | ACCEPTED | an empty directory or one failed read |
| W3 | `golang.org/x/sys` marked indirect though imported | P6 T4 | FIXED EARLIER | `go.mod` lists it as a direct requirement |
| W4 | `CreateFile` without explicit security attributes | P6 T4, P6 NOT in scope | ACCEPTED | inherits the private directory's ACL |
| W5 | `StartDetached` drops `SysProcAttr.CmdLine`; `ErrUnsafeBatchArg` echoes the argument | P6 T2 | FIXED EARLIER | Phase 6 final fixes 4 and 5 |
| W6 | Herdr config precedence (XDG before APPDATA) on Windows | P6 T2 | USER GATE | needs Herdr on Windows |
| W7 | OneDrive placeholder files may be refused (ModeIrregular); 8.3 short names rely on the exact-name check | P6 T3, P6 T8 | USER GATE | needs real Windows. Phase 10 final fix 7: the engine lock file now refuses only name-surrogate reparse points (symlinks, junctions; still opened as themselves with `FILE_FLAG_OPEN_REPARSE_POINT`) and accepts other tags such as OneDrive cloud files (`TestLockFileAcceptsCloudFilesAndRefusesNameSurrogates`, Windows-only, compile-checked on macOS). Cloud files elsewhere and 8.3 names still need a real Windows run |
| W8 | No startup sweep of stale `herdr-dots-run-*` directories; comment on inline `--append-system-prompt` | P6 T9, P6 NOT in scope | ACCEPTED | private temp dirs; comment only |
| W9 | `CodexCLI.Run` lacks `defer Close` | P6 T9 | FIXED EARLIER | Phase 6 final fix 9 |
| W10 | `TestCallFailsFastWithoutServer` uses a non-pipe path on Windows | P6 T6 | FIXED EARLIER | Phase 6 final fix 7 |
| W11 | Windows credentials Lstat→Open gap; drive-root and junction tests | P6 T8 | FIXED EARLIER | Phase 6 final fix 2 |
| W12 | Inner trailing-space test, `gitEnv` doc comment, trivial short-name cases | P6 T8, P6 NOT in scope | ACCEPTED | test or comment only |
| W13 | An aliased `os/exec` import would pass the portability guard | P6 final | FIXED POST-RC | the guard reads the file's import name for `os/exec` and flags a dot import; `TestChildGuardRules` (cases "aliased os/exec", "aliased exec.Cmd literal", "dot import of os/exec") |
| W14 | Pipe integrity labels | P6 NOT in scope | ACCEPTED | the pipe owner check (P6-R2) applies |
| W15 | Windows user names containing `%` or `!` | P6 NOT in scope | ACCEPTED | batch quoting refuses them: a clear error, nothing runs |
| W16 | CI actions pinned by major tag; CI runs twice on PR branches | P6 T12, P6 NOT in scope | USER GATE | the ready-to-publish checklist (SHA pins) |
| W17 | Herdr resolving `./bin/herdr-dots` to `.exe`; first `build.ps1` run; `-race` with gcc on Windows CI | P6 user gates | USER GATE | needs Windows and a CI push |

### 13. Write-capable dots (drafts, patches, diff viewer)

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| D1 | Patch `Diff` doesn't check stat size == `m.Bytes` before `ReadFile` | P7 T4, P7 NOT in scope | FIXED POST-RC | `patchstore.Diff` checks with `Lstat` that the file is regular and exactly `Meta.Bytes` long before reading it; `TestDiffChecksTheFileBeforeReadingIt` (Unix), `TestDiffOfAFileOfAnotherSizeIsChanged` |
| D2 | Patches directory not created with `platform.SecureDir` | P7 T4 | FIXED EARLIER | Phase 7 final fix 15b |
| D3 | Orphan `.patch` without `.json` not swept | P7 T4, P7 NOT in scope | FIXED POST-RC | `patchstore.SweepOrphanPatches` removes `.patch` files without a record, at start and daily; `TestSweepOrphanPatchesRemovesOnlyPatchesWithoutARecord`, `TestSweepOrphanPatchesOfAMissingDir`, `TestTheDraftSweepRemovesAPatchFileWithoutARecord` |
| D4 | Patchstore tests: concurrent Update, missing patch, empty SHA, invalid id | P7 T4, P7 NOT in scope | FIXED POST-RC | test-only; `TestDiffOfAMissingPatchAnEmptyHashAndAnInvalidID`, `TestConcurrentUpdatesAreSerialized` |
| D5 | P7-R2: corrupt asides of a deleted dot's file | P7 T0 | FIXED EARLIER | Phase 7 final fix 6; `TestDeleteDotRemovesItsCorruptAsidesOnly` |
| D6 | `resolveForWrite` duplicates the jail entry code | P7 T6 | FIXED EARLIER | Phase 7 final fix 15a |
| D7 | Remember on a patch card stores the redacted body | P7 T8 | FIXED EARLIER | Phase 7 final fix 8 (the engine refuses Remember on patch cards) |
| D8 | An orphaned card whose patch was applied can't be dismissed after a failed card write | P7 parked | FIXED POST-RC | discarding a patch whose record says applied is still refused, but a pending or orphaned card is now marked applied instead of staying on screen; `TestDiscardRefusesAPatchAlreadyApplied` |
| D9 | Stale comment at `patchstore.go:57`; `FlagAgentConfig` in a heavier package | P7 parked | PARTLY FIXED POST-RC | the stale comment at `patchstore.go:57` now says what `Get` does with `summary_raw`. Still accepted: `FlagAgentConfig` lives in a heavier package than it needs (cosmetic; moving it touches toolloop, engine and `ui/diff`) |
| D10 | `x` discards a patch card without a confirm | P7 NOT in scope | ACCEPTED | the draft can be run again; nothing is applied |

### 14. Proactive dots (schedules, research, commits)

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| R1 | The manage editor's View mutates state | P8 T14 | ACCEPTED | no edit acts on undrawn focus (Task 14 hardening) |
| R2 | Scheduler log noise (cap skips per slot, future-claim line per slot, not-caught-up on every start); `maxScheduledPerDay` comment | P8 T12 | PARTLY FIXED POST-RC | the gate-skip line is logged once a day per dot, project and reason, and the future-claim line once a day (`TestACappedScheduleLogsItsSkipOnceADay`). Still accepted: the "not caught up" line on every engine start (suppressing it needs persisted state or memory I/O under `e.mu`) |
| R3 | A state directory shared across machines would double-run schedules | P8 T12 | ACCEPTED | not a supported setup |
| R4 | One dot on two panes reviews a commit once per pane | P8 T13, P8 NOT in scope | FIXED POST-RC | a commit counts as reviewed once one pane's run starts (`headSeen.reviewed`), so the review goes to the first pane that sees the commit; `TestOneDotOnTwoPanesReviewsACommitOnce` |
| R5 | `e` edit-then-send on an unexpanded digest bypassed P8-R9 | P8 T15 | FIXED EARLIER | Phase 8 final fix 10a |
| R6 | Inject-mode commit loop | P8 T13 | FIXED EARLIER | Phase 8 final fix 10b; `TestInjectCommitReviewAfterADotTurnIsQueued` |
| R7 | Research repeats a finding while it is still pending | P8 T16 | ACCEPTED | by design |

### 15. Profiles, sharing, project dots and history

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| H1 | History quarantine falls back to `os.Remove` when the rename fails | P9 T1, P9 NOT in scope | FIXED POST-RC | the rename error is returned and the file kept; `TestQuarantineKeepsTheFileWhenTheMoveFails` |
| H2 | `clean()` collapses whitespace in project and pane paths | P9 T1 | ACCEPTED | display only |
| H3 | Template safety test should pin `Capabilities.MCP` | P9 T3 | FIXED EARLIER | P9-R16: the test pins every capability default |
| H4 | Template name/glyph control characters | P9 T3 | FIXED EARLIER | P9-R5: `Validate` rejects control/bidi in every display string |
| H5 | `history_test` sleep; `resolutionFor` literals; passed ask severity "info" | P9 T7, P9 NOT in scope | ACCEPTED | test or wording |
| H6 | Dropped-on-pane-close runs leave no history record | P8 carry to P9 | FIXED EARLIER | Phase 9 Task 7; `TestDroppedBackgroundRunIsRecordedWithItsCost` |
| H7 | `dots untrust FILE` revokes every checkout | P9 parked | FIXED POST-RC | revokes the file's current bytes, else the entry at this path, else every entry of the file; another checkout's trust of different bytes stays; `TestUntrustFileLeavesAnotherCheckoutsTrust`, `TestUntrustFileFallsBackToEveryEntryWhenNoneIsAtThisPath` |
| H8 | A scheduled project dot runs the first checkout's copy only | P9 parked | ACCEPTED | the copy is trusted; the fix needs `schedule.go` (the tick iterates one copy per slug) and per-checkout claim keys, and a slot is claimed once per project id (P8-R12) anyway |
| H9 | Pruning stops scheduled project dots in idle checkouts | P9 parked | ACCEPTED | fails safe; keeping idle checkouts scanned is a design change (pruning) |
| H10 | `scanSeq` is never cleaned | P9 parked | FIXED POST-RC | scans are numbered by one engine-wide clock, so nothing is kept per checkout; `TestAPrunedCheckoutLeavesNoScanCounter`, `TestAScanFromBeforeAPruneNeverOverwritesALaterOne` |
| H11 | In-limit hostile project files cost about 60 ms per 100 MB to pre-scan | P9 parked (final) | FIXED POST-RC | a project file with more than 2,048 `=` and `[` characters outside strings and comments is refused before decoding; `TestAFileWithTooManyKeysOrTablesIsRefusedBeforeDecoding`, `TestTrustRefusesAFileOverTheKeyAndTableCap`, `TestCodeInAPromptStringIsNotCountedAsKeysOrTables`, `TestCountKeysAndTablesSkipsStringsAndComments`. User dots and dot packs have no count cap (bounded by their size limits) |
| H12 | Templates' delivery mode isn't pinned by a test | P9 parked (final) | ACCEPTED | test-only |
| H13 | `trustAt` doesn't requeue when the new grant drops inject; `untrustAt` doesn't requeue across every rescanned checkout | P10 carry to Task 10 | FIXED T10 | Task 10 (engine `projectdots.go`) |

### 16. UI: pane, manage, summon, memory screen

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| U1 | Pane card selection by index (FLAG) | v1 T18a | FIXED EARLIER | v1 final fix 3; `TestSelectionFollowsCardIDAcrossSnapshots` |
| U2 | Suggest-card test's "copy" assertion satisfied by the static hint; blink messages not forwarded; no ctrl+c while editing; missing tests (d/r/t/f/?/esc/errMsg/multi-pane) | v1 T18a | ACCEPTED | cosmetic or test-only |
| U3 | Pane header and footer taller than the pane hide the header | v1 final residual | FIXED EARLIER | Phase 2 Task 0 |
| U4 | `fitRoster` n≤1 hides the selected dot; "+N" line not cut to width | P2 T0 | FIXED EARLIER | Phase 2 final fix 5 |
| U5 | Pane footer and card header lines not cut to width | P4 T16, P4 NOT in scope | FIXED POST-RC | card header lines and section rules are cut to the pane width; `TestNarrowPaneCutsCardHeadersAndFooter` |
| U6 | Pane and memory screen draw `─` and `▸` without the ASCII fallback; `SetASCII` is an unlocked package variable | P9 parked | FIXED POST-RC | rules, markers and glyphs go through `style.Sym` (ASCII fallback) and the flag is an `atomic.Bool`; `TestSetASCIIIsSafeForConcurrentUse`, `TestASCIIModeDrawsRulesAndMarkerInASCII`, `TestASCIIModeDrawsRulesMarkerAndCutsInASCII`, `TestASCIIModeDrawsMarkerAndCouncilInASCII`, `TestASCIIModeTemplateMarkerIsASCII` |
| U7 | Summon popup: double `enter` summons twice | v1 T20a | FIXED POST-RC | a second `enter` can't summon twice; `TestDoubleEnterSummonsOnce` |
| U8 | Summon popup: esc keeps stale input; stale status not cleared; ctrl+c in asking mode goes to the input; static help text in the empty state | v1 T20a | PARTLY FIXED POST-RC | esc clears the question and a stale status, and ctrl+c while asking quits (`TestEscClearsTheQuestionAndAStaleStatus`, `TestCtrlCWhileAskingQuits`). Still accepted: the static help text in the empty state (cosmetic) |
| U9 | Manage `savedMsg` selects edited dots, so enter attaches them (FLAG) | v1 T19a | FIXED EARLIER | v1 final fix 13 |
| U10 | Renaming a dot to another dot's name isn't refused | v1 T19a, v1 final | FIXED POST-RC | renaming a dot to another library dot's name (any case) is refused; `TestRenamingADotToAnotherDotsNameIsRefused` |
| U11 | Unknown color silently rewritten to red; dot colors not validated | v1 T19a, v1 T4 | PARTLY FIXED POST-RC | an unknown file color is kept as an extra choice (`TestEditKeepsAnUnknownColor`). Still accepted: dot colors are not validated in `dots.Validate` (it would make existing dot files fail to load) |
| U12 | Fixed 64-column editor layout | v1 T19a | ACCEPTED | cosmetic |
| U13 | Manage checkboxes seeded once: a dot attached elsewhere after the popup opened can be detached by apply | v1 T19a | FIXED POST-RC | boxes the developer hasn't toggled follow every snapshot; `TestCheckboxesFollowAttachmentsMadeElsewhere`, `TestATickedBoxSurvivesASnapshot` |
| U14 | Manage edit/Check/Validate/esc paths untested; nil vs empty globs; stale save messages can cross into a newly opened editor; a Logf-only assertion | v1 T19a | ACCEPTED | test-only or cosmetic |
| U15 | memui approve screen clipped in tiny popups | P2-R9 | FIXED T10 | `TestApproveShowsTheFullProposalAndNeedsY` (extended) |
| U16 | memui forget-all target empty when the project name cleans to nothing | P2 T11 | FIXED EARLIER | Phase 2 final fix 12 |
| U17 | memui long status lines overflow the viewport; edit-mode status persists; `q` quits | P2 T11 | PARTLY FIXED POST-RC | a long status wraps to the popup and esc from the editor clears its status (`TestLongStatusWrapsToTheViewport`, `TestEscFromTheEditorClearsItsStatus`). Still accepted: `q` goes back (matches the pane's `q`) |
| U18 | memui out-of-order loads | P2 T11 | ACCEPTED | approve checks the text the developer saw |
| U19 | `M` on an empty library is silent; the memory question is asked when the dot has no memory; missing Y/enter/esc/error tests | P2 T12 | PARTLY FIXED POST-RC | `M` on an empty library says "no dot selected · P project memory" (`TestMemoryKeyOnAnEmptyLibrarySaysWhy`). Still accepted: the memory question is asked when the dot has no memory (knowing that needs a memory-store query; with the keep default the question is harmless) |
| U20 | Remember chooser: esc test doesn't assert it closes; it can send while disconnected; empty and untruncated `suppressed()` detail; a long header truncates the budget | P2 T13 | PARTLY FIXED POST-RC | the remember chooser, rule reason, card editor and summon question stay open with the offline hint while disconnected (`TestOpenPromptsDontSendWhileDisconnected`). Still accepted: the esc-test assertion, the `suppressed()` detail and the header/budget truncation (cosmetic) |
| U21 | A summon whose verdict is not relevant gives no card (the spec says always a card) | v1 final | ACCEPTED | the feed shows the pass line |
| U22 | Deleting a dot asks "Also delete its memory in all projects? (Y/n)" and `enter` answers yes | P2 T12 | FIXED POST-RC | deleting a dot keeps its memory unless the developer presses `y` at "Also delete its memory in all projects? (y/N)" (the developer said to fix what can be fixed; the recommendation was applied); `TestDeleteDotMemoryQuestionDefaultsToKeep` |

### 17. CLI, setup-keys, config, paths and the dots store

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| L1 | `go.mod` marks direct dependencies `// indirect` | v1 T2 | FIXED EARLIER | `go.mod` lists them as direct requirements |
| L2 | `[[startup]] engine --ensure` exits 1 until Task 17 | v1 T2 | FIXED EARLIER | v1 Task 17 |
| L3 | Dispatch tests don't assert all routes; version test prints to stdout | v1 T2 | ACCEPTED | test-only |
| L4 | Config values only lower-bound clamped; unknown keys silent | v1 T3 | FIXED POST-RC | unknown `config.toml` keys and `[providers.<unknown>]` tables are recorded, logged by name (never by value) and toasted once per set, from engine start and on reload; `version` is never flagged; `TestLoadRecordsUnknownKeysAndProviders`, `TestLoadNeverFlagsVersion`, `TestUnknownConfigKeysAreLoggedAndToastedOncePerSet`. Upper clamps on config values stay as they are (a product decision) |
| L5 | No `WriteFileAtomic` failure-path test, 100-byte boundary test or `Ensure` chmod test; config tests ignore `WriteFile` errors | v1 T3 | ACCEPTED | test-only |
| L6 | `FromEnv` doesn't check absolute paths | v1 T3 | FIXED POST-RC | `FromEnv` refuses a relative plugin config or state directory; `TestFromEnvRefusesRelativeDirs` |
| L7 | `keys.Append` doesn't validate the resulting TOML and isn't atomic | v1 T21 | FIXED EARLIER | v1 final fix 14 |
| L8 | Backup names have 1 s resolution; empty backup when the config is missing | v1 T21 | FIXED POST-RC | backups are created with `O_EXCL` and get `-2`, `-3`, … on a clash; none is written when there was no config; `TestAppendBackupsInTheSameSecondAreUnique`, `TestAppendWithoutAConfigWritesNoBackup`, `TestSetupKeysAppendedLine` |
| L9 | `Plan` misses a plugin action already bound to another key | v1 T21 | FIXED POST-RC | an action bound to another key counts as already bound; `TestPlanSeesAnActionAlreadyBoundToAnotherKey` |
| L10 | `runSetupKeys` untested; `-h` surfaces `ErrHelp` | v1 T21 | FIXED POST-RC | `setup-keys -h` prints usage without an error; `TestSetupKeysHelpIsNotAnError` (`runSetupKeys` as a whole stays untested: it reads stdin and runs herdr) |
| L11 | `dots.Load`/`Delete` don't sanitize the slug | v1 T4 | FIXED T10 | `TestSlugsThatAreNotFileNamesAreRefused` |
| L12 | `Save(create=true)` Stat-then-write TOCTOU | v1 T4 | ACCEPTED | local, one user |
| L13 | `Templates()` silently drops a broken template; dots test gaps | v1 T4 | ACCEPTED | templates are embedded and tested |
| L14 | `TruncateRunes(n<=0)` panics | v1 T13 | FIXED T10 | `TestTruncateRunesNeverPanics` |
| L15 | `Send` returns Busy with a PaneGet error; empty-cards path untested | v1 T13 | PARTLY FIXED POST-RC | `TestSendWithNoCardsTouchesNothing` covers the empty-cards path. Still accepted: `Send` returns Busy with a PaneGet error (the card stays queued; callers read the error) |
| L16 | Batch truncation can cut later dots' text | v1 T13 | ACCEPTED | inject sends less than approved, never more |
| L17 | `memory --help` needs the state dir; no `memory_enabled=false` engine wiring test | P2 T15 | FIXED POST-RC | `memory --help` and `memory <sub> -h` work without the plugin dirs; `TestMemoryHelpWorksWithoutThePluginDirs` |
| L18 | Discover wait test bound loose; CRLF pointer untested | P2 T2 | FIXED POST-RC | test-only; `TestDiscoverReadsACRLFStatePointer` |
| L19 | README: the live "suppressed" line wasn't observed; clarify the copywriter's proposed scope | P2 T16 | ACCEPTED | docs wording; Task 12 owns the README (the live check is row X6) |
| L20 | Unknown providers in `config.toml` dropped without a warning; `ExtraHeaders`/`ToolDenyGlobs` slices aliased | P4 T4 | PARTLY FIXED POST-RC | an unknown `[providers.x]` table is now logged and toasted (`TestLoadRecordsUnknownKeysAndProviders`, `TestUnknownConfigKeysAreLoggedAndToastedOncePerSet`), and the provider still runs on its defaults and fails visibly. Still accepted: the `ExtraHeaders`/`ToolDenyGlobs` slices are aliased (only read after `Load`; a convention, not enforced) |
| L21 | An invalid `tool_deny_globs` entry was dropped silently (the rest of the list still applied) | P4 T4 | FIXED T9 | Round 1 (P10-R7): it fails closed: `config.Load` records it (`RepoToolsOff`), and until it is fixed API and claude-cli repository tools are off, drafts are refused, research is off and commit reviews send no patch (codex dots still read the repository through their sandbox, row S24); one log line and one toast, and research dots show "research off · fix tool_deny_globs" in the roster; `TestInvalidDenyGlobFailsClosed`, `TestInvalidDenyGlobTurnsRepoToolsOff`, `TestInvalidDenyGlobRefusesDrafts`, `TestInvalidDenyGlobTurnsResearchOff`, `TestInvalidDenyGlobIsLoggedAndToastedOnce`, `TestCommitReviewWithAnInvalidDenyGlobSendsNoPatch` |

### 18. Tests, flakes and CI

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| F1 | Flaky `TestIPCEngineStartsTheEngineLazilyAndAgainWhenItDies` | P6 note, P8 post-merge, P6 NOT in scope | FIXED T10 | `startStub` stop waits for `ipc.Serve` |
| F2 | Phase 7 draft tests time out under load | Phase 7 | FIXED T10 | `eventually` waits up to 15 s |
| F3 | Flaky `TestFinishedPaidReviewIsNotDoubleCounted` | P9 | FIXED EARLIER | controller fix; 100/100 runs |
| F4 | `TestAskCouncilCountsAsOneAsk` flaked once under load | P9 parked (final) | FIXED T10 | `processPaneIfIdle` before the first ask (the cause named in the known issues); 400 loaded `-race` runs green in the hardening wave |
| F5 | One unidentified full-suite flake | P9 parked | ACCEPTED | seen once; not reproduced (the hardening wave found and fixed the flakes F7–F9 below, which may be what it was; a later fix-wave run saw `TestTwoEnginesRunASlotOnce` time out once under heavy parallel load: a fixed 60 ms sleep with two engines; not changed, not shown to be the same flake) |
| F6 | The `ui/diff` sweep test takes about 30 s | P7 parked | ACCEPTED | test time only |
| F7 | `TestBudgetResetsOnNewAgentSession` asserted usage right after the backend was called, but a run is charged after it returns (about 1 in 120 under `-race -cpu=1,2`) | hardening wave (post-rc) | FIXED POST-RC | the test waits for the charge; 0 failures in 200 loaded runs; `TestBudgetResetsOnNewAgentSession` |
| F8 | `TestDismissEditAndInjectResolutions` indexed the run's history record before it could exist and panicked in a full-suite run | hardening wave (post-rc) | FIXED POST-RC | the index is guarded; 0 failures in 200 loaded runs; `TestDismissEditAndInjectResolutions` |
| F9 | `TestCouncilStartingDuringAnAskLeavesTheDotOut`: the armed fake reader gated any transcript read, so attach's catch-up pass could take the council's gate (4 of 400 failures); the same hazard silently disarmed `TestAttachDoesNotLoseConcurrentAttachmentUpdates` | hardening wave (post-rc) | FIXED POST-RC | the gate matches only the council's own read (`armFor`); 0 of 400 and 0 of 600 runs; `TestCouncilStartingDuringAnAskLeavesTheDotOut`, `TestAttachDoesNotLoseConcurrentAttachmentUpdates` |
| F10 | `TestAskWithoutTurnsSendsNone`: its dot reviewed turns, so a catch-up review could race the ask | hardening wave (controller, `e4e38ea`) | FIXED POST-RC | the test's dot doesn't review turns, so no catch-up review races the ask; `TestAskWithoutTurnsSendsNone` |

### 19. Platforms, live verification and incidents

| # | Finding | Source | Status | Reason or evidence |
|---|---|---|---|---|
| X1 | `windows/arm64` built and published, never tested | Phase 10 | USER GATE | no hardware here |
| X2 | Linux and Windows have only CI and cross-compiled tests | Phases 6, 10 | USER GATE | 3-OS acceptance needs hardware |
| X3 | Codex paths are documented-format, not live-verified (transcripts, MCP, `-c` override) | v1 T9, P2 T1, P5 T5 | USER GATE | Codex isn't installed here |
| X4 | Live Anthropic, OpenAI and OpenRouter runs; Phase 4 V6 no-echo popup check | P4 user gates | USER GATE | no keys here |
| X5 | Visual pane checks; Phase 9 Task 11 Step 8 popup live check | P5, P8, P9 user gates | USER GATE | needs a Claude pane (its folder-trust prompt writes `~/.claude.json`) |
| X6 | Live "suppressed" line on a repeated finding | P2 T16 | USER GATE | needs a live model |
| X7 | Live Claude draft end to end; Windows runtime of drafts | P7 user gates | USER GATE | live model; Windows |
| X8 | Re-verify `--restricted` on new Claude Code versions | P7 user gates | USER GATE | an older CLI fails closed |
| X9 | Read deny vs Grep/Glob and `--restricted` outside-cwd reads | P8 user gates | USER GATE | live Claude Code |
| X10 | Live `--max-budget-usd` stop behaviour | P8 user gates | USER GATE | live spend |
| X11 | V3/V5/V6 web paths | P8 user gates | USER GATE | live web search |
| X12 | Pin GitHub Actions by SHA; pin an exact Go toolchain; soak job cost on every push | P10 carry to Task 13 | PARTLY FIXED POST-RC | Go 1.26.9 (govulncheck clean), the five Actions by commit SHA and GoReleaser v2.18.3 are pinned in both workflows; the soak job's cost on every push stays a decision on the ready-to-publish checklist |
| X13 | v1 incident: an E2E agent's `/model haiku` rewrote the global `~/.claude/settings.json` model; restored from an old backup, the true prior value is unknown | v1 incident | USER GATE | the developer confirms their model setting |
| X14 | Phase 8 incident: accepting Claude's folder-trust prompt added trust entries for `/private/tmp/dots-test-repo` and `/private/tmp/dots-test-repo-p8` to `~/.claude.json` | P8 incident | USER GATE | the developer may remove them; the directories are gone |

Carry-overs to Phase 10 Tasks 7 and 13 that are not findings (migration
wiring, docs) are tracked by those tasks; the Task 10 ones are in the folded
table, and the Task 13 user gates are row X12.

# Herdr Dots threat model (1.0.0-rc.1)

Status: release candidate 1.0.0-rc.1, not released. This document says what
the plugin defends against, how, which test pins each defence, and what is
left over. It is not a claim that the plugin is free of security bugs; the
companion review is
[2026-10-08-security-review.md](2026-10-08-security-review.md), and the
accepted items come from the Phase 10 triage notes
(`docs/superpowers/notes/2026-10-08-phase-10-triage.md`, row ids such as S16
or I8 below).

Scope: the plugin binary (`herdr-dots`), its engine, panes and popups, the
MCP server, the install scripts and the release pipeline. Out of scope: Herdr
itself, Claude Code, Codex, the model providers, and the developer's own
machine account (anything running as the developer can already read every
file the plugin can).

## Assets

- The developer's source code and the coding agent's conversation (transcripts).
- API keys (`credentials.toml`, environment variables).
- Dot memory: project conventions, findings, dismissed findings, preferences.
- The developer's working tree, index, branches and git history.
- Herdr, Claude Code and Codex configuration files (keybindings, MCP entries).
- Money: provider spend under budgets.

## Trust boundaries

1. **Model output → plugin.** Every model reply is untrusted text: verdicts,
   memory ops, chair digests, council stances, patches, research digests.
2. **Repository → plugin.** A cloned repository is untrusted: its files, its
   `.git/config`, its project dots (`.herdr-dots/dots/`), its transcripts,
   and its agent settings (`.claude/settings*.json`, `.codex/`, `.mcp.json`).
3. **Shared files → plugin.** Imported dot packs and project dots come from
   other people.
4. **Local processes → engine.** The engine socket or pipe, the state dir.
5. **Network → install.** The release download and the GitHub release.
6. **Plugin → the developer's agent.** Card text typed into the agent pane
   (approved, or unapproved in inject mode), and MCP answers the agent reads.

The eleven areas in "Boundaries in detail" below split these six by feature.

## Threats, mitigations and tests

| # | Threat | Mitigation | Pinned by |
|---|---|---|---|
| T1 | Prompt injection in a transcript or a repository file makes a dot run tools that change things | Dots are read-only: claude `--tools Read,Grep,Glob` with hooks disabled, codex `--sandbox read-only`, API dots get a jailed read-only tool loop; denied backend args are dropped; write-capable dots (Phase 7) need two explicit approvals and never get a shell | `TestClaudeBuildCmdIsReadOnlyAndHookless`, `TestClaudeBuildCmdNeverEmitsDeniedArgs`, `TestCodexBuildCmdNeverEmitsDeniedArgs`, `TestValidateRejectsUnsafeBackendArgs`, `TestSafeArgsDropsDeniedFlagsAndTheirValues`, `TestResolveInRootRefusesEscapes`, `TestNoExistenceOracleOutsideRoot`, `TestDefaultDenyCoversCommonCredentialFiles`, `TestClaudeDraftGetsFileToolsAndNoShell`, `TestWriteToolsNeedBothWriteAndPassWrite`, `TestApplyPatchRefusesAPatchThatIsNotTheOneReviewed` |
| T2 | Model output with terminal escapes or bidi controls rewrites the screen, the clipboard, or makes approved text read differently | `SanitizeModelText` drops C0/C1 controls and bidi controls before display, storage and delivery | `TestSanitizeModelTextDropsControlsKeepsNewlineTab`, `TestSanitizeModelTextRemovesBidiKeepsEmojiJoiners`, `FuzzDiffViewParse`, `FuzzParseCLIOutput`, `TestFeedSanitizesDetail`, `TestNamesAreSanitizedOnScreen`, `FuzzParseVerdict`, `FuzzParseDigest` |
| T3 | Injected text becomes shared project memory ("always disable CSRF") | Model-written project memory waits as a proposal until the developer approves it seeing the full text; models can't write dismissed, preference or global entries, nor edit user or other dots' entries; memory goes only in the user prompt, framed as reference data | `TestApplyAddsDotActiveAndProjectProposed`, `TestApplyRefusesWhatModelsMayNotWrite`, `TestModelNeverRewritesDeveloperWording`, `TestUserPromptPutsMemoryBeforeConversation`, `TestApproveShowsTheFullProposalAndNeedsY` |
| T4 | A model suppresses future findings by writing its own "dismissed" entries | Only outcome and user findings suppress repeats | `TestOutcomeMergePromotesModelFinding` and the Phase 2 final-fix suppression tests (`TestModelWrittenFindingNeverSuppresses`, `TestSuppressedByIgnoresModelAndImportFindings`, `TestSentFindingSuppressesRepeat`, `TestUserSavedFindingSuppressesRepeat`) |
| T5 | Approving something the developer didn't see (stale selection, clipped popup) | Selection by id; approve/pin refuse text that changed on disk; the approve screen refuses when the popup can't show it whole | `TestSelectionFollowsCardIDAcrossSnapshots`, `TestApproveAndPinRefuseTextThatChangedOnDisk`, `TestApproveShowsTheFullProposalAndNeedsY` |
| T6 | Secrets end up in memory, the ledger, history, exports or packs | The secret scanner (`internal/secrets`) refuses model writes and asks before user writes; ledger lines and history excerpts that look like secrets are redacted; invisible characters can't split a key; packs never include credentials and are scanned (Phase 9) | `TestScanSecretsFindsCredentials`, `TestRecordOutcomeNeverStoresSecrets`, `TestInvisibleCharactersDoNotHideASecret`, `TestSecretNeedsASecondSave`, `FuzzScanSecrets`, `TestExportRefusesSecretsUnlessForced`, `TestExportRefusesDotSecretsEvenWhenForced` |
| T7 | API keys leak to logs, the feed, snapshots, IPC, argv or errors | `credentials.toml` 0600 (DACL on Windows) or env indirection; `credentials.Redact` on every error path; keys never on argv | `TestPermissiveFileIsRefusedWithoutReadingIt`, `TestMalformedFileErrorNeverQuotesTheKey`, `TestEntryNeverFormatsItsKey`, `TestRedactMasksWholeAuthorizationValues`, `TestBrokenCredentialsAreReportedWithoutTheirText`, `FuzzCredentialsFile` |
| T8 | A hostile repository runs code through git (`core.fsmonitor`, hooks, transports, lazy fetches) | Every git call passes `core.hooksPath` to the null device, `core.fsmonitor=false` and `protocol.allow=never`, with `GIT_ALLOW_PROTOCOL=none` and `GIT_NO_LAZY_FETCH=1`, and filters `GIT_*` overrides; drafts also turn off filters and submodule recursion | `TestExecGitRunsNothingTheRepositoryConfigures`, `TestEveryGitCallDisablesRepositoryPrograms`, `TestExecGitNeverRunsHooks`, `TestResolveProjectIgnoresInheritedGitEnv` |
| T9 | Repository project dots or imported packs smuggle risky capabilities | Project dots need a content-hashed trust; a changed file revokes trust; risky capabilities are clamped on import | `TestAnyEditRevokesTrustAndChangesTheStamp`, `TestUntrustedFileIsClampedAndListsWhatItAsks`, `TestCorruptTrustFileFailsClosedAndIsNeverOverwritten`, `TestClampDotTurnsEveryRiskyCapabilityOff`, `FuzzDotpackParse`, `FuzzDecodeStrict` |
| T10 | A dot name or slug from the popup, the CLI, IPC or MCP names a file outside the dots dir | `dots.Load/Save/Delete` refuse slugs `Slugify` couldn't make; memory validates slugs and project ids before any path | `TestSlugsThatAreNotFileNamesAreRefused`, `TestUnsafeIDsAndSlugsAreRefused` |
| T11 | Another local user talks to the engine or reads state | Socket in a 0700 directory with a 0600 socket (Unix); a per-user DACL'd named pipe with an owner check (Windows); state files 0600, dirs 0700; the feed is tightened to 0600 | `TestEnsureCreatesPrivateDirs`, `TestFilesArePrivate`, `TestOpenFeedMakesAnOldLooseFileOwnerOnly`, `TestPipeOwnerError`, `TestCheckPrivateEntries` |
| T12 | Malformed IPC input crashes or hangs the engine | Each connection decodes JSON into typed structs; unknown commands fail | `FuzzIPCDecode` |
| T13 | A dot's delivered message re-triggers the same dot (loops, runaway spend) | Loop guard on the delivery header; inject never follows a dot-started turn; councils and consulted dots skip the next turn | `TestLoopGuardSkipsTurnStartedByThisDot`, `TestInjectForcedToQueueForDotStartedTurns`, `TestConsultedDotSkipsTheNextTurnOnly`, `FuzzFromDots` |
| T14 | Cost abuse: a chatty model, a recursion through MCP, background research | Call and dollar budgets per dot and per session, ask limits per turn, a global daily budget for background work, MCP tools refuse inside dot runs | `TestBudgetStopsDotAndWarnsOnce`, `TestPlanRefusesWhenThePaneBudgetIsShort`, `TestDollarBudgetStopsTheDot`, `TestAskLimiterPerMinuteWindow`, `TestAskChargesDollarsAndRespectsTheDollarBudget`, `TestDailyBudgetStopsBackgroundRunsUntilMidnight`, `TestDecideBackgroundNeverUnlimited`, `TestEveryToolRefusesOutsideHerdr` |
| T15 | Inject types into the agent while the developer is typing | Inject only when the agent is idle and the pane isn't focused; a runtime kill switch that fails closed | `TestInjectWaitsWhileFocusedThenFlushes`, `TestInjectKillSwitchAppliesAtRuntime`, `TestMalformedConfigKeepsLastGoodButDisablesInject`, `FuzzConfigLoad` |
| T16 | A write-capable dot's patch escapes the worktree or touches `.git/` | Patch validation (no `.git/`, no escapes, size caps), apply only when the agent is idle, never stage or commit | `TestEscapesRefusesNTFSAliasesOfDotGit`, `TestValidateRefusesARenameOrCopyThatHidesASourcePath`, `TestACommittedSymlinkCannotBeRetargetedByAForgedDiff`, `TestAnApplyWithoutTheReviewedHashIsRefused`, `FuzzDiffHeaders` |
| T17 | A tampered or substituted release binary | `build.sh`/`build.ps1` verify the archive against `release/SHA256SUMS` from the cloned tree and never run an unverified binary; the publish job refuses archives that differ from the committed checksums; cosign keyless signatures, checked by `verify-release` | `TestChecksumMismatchFallsBackToASourceBuild`, `TestArchiveWithoutTheBinaryIsRejected`, `TestVerifyReleaseRefusesATamperedArchive`, `TestVerifyReleaseRefusesAReplacedBinary`, `TestVerifyReleaseRunsCosignWithTheWorkflowIdentity` |
| T18 | Vulnerable dependencies | Few, pinned modules; `govulncheck` in CI and before a release | the `vulncheck` CI job; Task 11 Step 3 result: no reachable vulnerability in a third-party module; 24 reachable ones in the Go 1.25.7 standard library this machine builds with; the workflows most likely install go1.25.0 (review R2: an explicit patched `go-version` in every workflow is a user gate; the release candidate job runs govulncheck) |
| T19 | Files from a newer version are overwritten by an older binary (data loss after a downgrade) | `migrate.Run` marks them read-only; `WriteFileAtomic` refuses them; migrations back up first and run under the file's lock | `TestNewerFileIsReadOnlyAndNeverRewritten`, `TestFutureQueueIsReadButNeverRewritten`, `TestWritingCommandsProtectNewerFiles`, `TestLockedFileWaitsThenFailsWithoutChange` |
| T20 | Uninstall deletes the developer's data or config by mistake | Backups before every config edit; only herdr-dots bindings removed; `--purge` needs a typed phrase, refuses while an engine runs, and refuses home, root and short paths | `TestRemoveBlockKeepsEverythingElseByteForByte`, `TestPurgeNeedsTheTypedPhraseEvenWithYes`, `TestPurgeRefusesWhileAnEngineRuns`, `TestSafeToPurgeRefusesHomeRootAndRelativePaths` |
| T21 | Telemetry or unexpected network traffic | None: the plugin talks only to the backends the developer configures, Herdr's local socket, and GitHub during install | code review (Task 11 Step 3): the only HTTP clients are the provider adapters (`internal/backend/*`, also behind the manage popup's save check and model list) and pack import from an https URL (`internal/dotpack/fetch.go`, from `dots import` or the manage popup), which the developer starts |
| T22 | A hostile repository's `.claude/settings.json` (or `settings.local.json`) runs commands or redirects a claude-cli dot: `claude -p` skips the workspace trust dialog, so credential helpers (`apiKeyHelper`, `awsAuthRefresh`, `awsCredentialExport`) and `env` (`ANTHROPIC_BASE_URL`) from the repository would apply to every review | Every claude-cli run loads only the developer's user settings (`--setting-sources user`); drafts and research run `--restricted`, which loads no settings file; a dot's args can't pass either flag (in either form) or `--managed-settings`; `--strict-mcp-config` keeps the repository's `.mcp.json` out | `TestClaudeRunsNeverLoadRepositorySettings`, `TestDotArgsCannotReloadRepositorySettings`, `TestDotArgsCannotReloadRepositorySettingsInline`, `TestValidateRejectsUnsafeBackendArgs`, `TestClaudeDraftGetsFileToolsAndNoShell`, `TestClaudeResearchDeniesSecretsAndFetch` (review R1, fixed in this task; argv-pinned; live check pending) |

## Boundaries in detail

Each area lists the assets at stake, the threats, the mitigations with the
code and the tests that enforce them, and the residual risk with the accepted
triage rows (the full reasons are in the triage notes).

### B1. Untrusted transcript and repository content reaching the models (prompt injection)

- **Assets:** the developer's code, the conversation, memory, money.
- **Threats:** text in a transcript, a repository file, a commit, a web page
  or a project dot's purpose steers a dot: it runs tools, reads secrets,
  writes memory, or tells the agent to do something harmful.
- **Mitigations:**
  - Read-only runs (T1): `internal/backend/claude.go` (`BuildCmd`: `--tools
    Read,Grep,Glob`, `--strict-mcp-config`, `--settings` with
    `disableAllHooks` and, when the read tools are on, Read deny rules for
    secret files (`researchSettings`, review R3,
    `TestClaudeRepoReadRunsDenySecretFiles`), `--setting-sources user`),
    `internal/backend/codex.go` (`--sandbox read-only`, never drafts:
    `ErrCodexCannotDraft`), `internal/backend/toolloop` (jail: `ResolveInRoot`,
    `openVerified`, `Unavailable` for `/`, `$HOME` and its ancestors,
    `DefaultDeny`). Dot args that would widen this are dropped and refused
    (`internal/dots/args.go`: `SafeArgs`, `DeniedArgs`; P9-R4, P9-R10).
  - Prompt framing: memory and the conversation go only in the user prompt,
    tool results are labelled untrusted data (`toolloop.SystemNote`), research
    web text likewise (`TestResearchSystemPrompt`).
  - Commit reviews quote the commit as data and leave out denied files
    (`internal/engine/commits.go`: `commitPatch`; `TestCommitReviewLeavesDeniedFilesOut`, P10-R9).
  - Repository agent settings never load (T22; argv-pinned; live check
    pending). A side effect, inferred from the Claude Code bundle and not
    run: with `--setting-sources user` the repository's `CLAUDE.md` probably
    no longer reaches claude-cli dots either (project instructions load with
    the project source). Confirming it is part of the R1 live gate.
- **Residual risk:** a dot can still *say* something wrong; it can't act on
  it, and memory written from it waits for approval (Known gaps). claude-cli
  review, ask and memory runs now get the secret-file Read deny rules, but
  they are not confined to the repository: they can read other files outside
  it, and whether Read deny rules also bind Grep and Glob is unverified
  (review R3, triage X9). Codex dots read
  through Codex's own sandbox, so `tool_deny_globs` doesn't limit them
  [triage S24], and Codex may load repository instructions (review R6). Local dot
  args can still use some claude/codex flags the denylist doesn't name [triage B9].
  Research runs `--restricted` and so drop the developer's own `~/.claude`
  deny rules [triage S21]. The JSON extractor fails closed on prose with braces
  [triage B1].

### B2. Model output reaching the terminal and the agent (sanitizing, delivery, inject)

- **Assets:** the developer's screen and clipboard, the agent's next turn.
- **Threats:** escape sequences or bidi controls in model text; a forged dot
  header that defeats the loop guard; text typed into the agent while the
  developer types; unapproved text reaching the agent.
- **Mitigations:**
  - `internal/textutil/sanitize.go` (`SanitizeModelText`) before storage,
    display and delivery (T2; P8-R10); display strings of dots refuse control,
    bidi and invisible characters (`dots.UnsafeText`,
    `TestUnsafeTextRefusesControlBidiAndHiddenCharacters`, P9-R5).
  - `internal/delivery/delivery.go`: `CanDeliver` (idle agent; inject also
    needs the pane unfocused), `Format` neutralises header look-alikes
    (`TestFormatNeutralisesForgedHeadersInBodies`), inject is capped at 2000
    characters and never carries a patch (`TestPatchCardsAreNeverInjected`).
  - `internal/engine/deliver.go` (`flushAuto`): a project dot whose trust
    changed never injects (`TestAWaitingInjectCardIsNotInjectedAfterUntrust`,
    `TestNarrowingTheInjectGrantRequeuesWaitingCards`); `inject_enabled` is a
    runtime kill switch that a malformed config turns off (T15).
  - Approval is by card id, digests need the part count seen (T5; P8-R9).
- **Residual risk:** inject mode is, by design, unapproved text in the
  agent's prompt; it is off unless the developer turns it on for a dot, and
  clamped for shared and project dots. Stored text is sanitized once, not
  again at draw time [triage S19]. A pane closing inside a delivery can lose one
  outcome record [triage E40].

### B3. Write-capable drafts, the two approval gates and the patch apply path

- **Assets:** the working tree, the index, refs, the agent's configuration.
- **Threats:** a draft edits outside its worktree, touches `.git`,
  `.claude/` or secrets, plants a symlink, or the developer applies a patch
  different from the one reviewed.
- **Mitigations:**
  - Gate 1 (start a draft) and gate 2 (apply) are UI confirmations; the
    instruction over the cap is refused, not cut (P7-R9).
  - claude drafts: `--restricted`, file tools only, `acceptEdits`, no Bash or
    web, `writeSettings` denies `.git` and `.claude/**`
    (`TestClaudeDraftGetsFileToolsAndNoShell`, P7-R3, live-verified on Claude
    Code 2.1.295); API drafts: `toolloop/write.go`
    (`TestWriteToolsRefuseGitSecretsAndEscapes`); codex never drafts.
  - `internal/worktree/validate.go` (`Validate`) and `apply.go` (`Apply`):
    paths from `git apply --numstat` on the reviewed bytes, `CheckPath`
    (`.git` in every spelling, reserved names), deny list incl. `DraftDeny`,
    no symlink creation or retarget (P7-R6), no submodules, size caps, a
    private index (nothing staged or committed), top-level only
    (T16; `TestADraftTouchingASecretIsRejected`,
    `TestAnAPIWriteDotCantProposeAgentConfig`).
  - The apply request carries the SHA-256 the viewer showed (P7-R7,
    `TestAnApplyWithoutTheReviewedHashIsRefused`); the viewer shows long lines
    whole (`TestALongLinesHiddenPayloadIsOnScreen`).
  - Agent and CI configuration changes are allowed but flagged
    (`toolloop.AgentConfigGlobs`).
- **Residual risk:** gate 2 lists unviewed files rather than blocking
  (P7-R8). Apply uses the developer's own filters and LFS (P7-R4), which come
  from the local `.git/config`, not from a clone. The private snapshot refs
  leave with `git push --mirror` [triage S17]; two engines on one repository end each
  other's drafts [triage G7]. `x` discards a patch without a confirm [triage D10].

### B4. Proactive and background runs: the daily budget, web research, git config execution

- **Assets:** money, the code (no one is watching), the developer's machine.
- **Threats:** unattended runs overspend; research exfiltrates repository
  data over the web; a hostile repository runs programs through git during a
  commit review or a snapshot.
- **Mitigations:**
  - `internal/state/daily.go` (`Daily`): a global daily budget in int64
    microdollars that fails closed on a corrupt file until local midnight
    (P8-R3, `TestCorruptDailyFileStopsBackgroundRuns`,
    `TestDailyBudgetStopsBackgroundRunsUntilMidnight`); schedules at most every
    15 minutes, 96 scheduled runs and 24 commit reviews per dot per day
    (P8-R5, P8-R12); background claude-cli runs carry `--max-budget-usd`
    (P8-R15, `TestBackgroundRunsCarryAMaxBudget`).
  - Web: claude-cli research gets WebSearch only, never WebFetch, runs
    `--restricted` and denies Read of secret files (P8-R14,
    `TestClaudeResearchDeniesSecretsAndFetch`,
    `TestClaudeResearchDeniesTheConfiguredGlobs`); at most 5 paid web uses per
    run (P8-R6, `TestWebUsesAreCappedAcrossPauses`,
    `TestWebUsesAreCappedAcrossToolTurns`, `TestWebFetchCarriesContentCap`);
    an invalid `tool_deny_globs` entry turns research off
    (`TestInvalidDenyGlobTurnsResearchOff`).
  - Git: T8; `worktree.Git` adds `diff.submodule=short`, filters off in
    drafts (P8-R13); commit reviews pass `--no-ext-diff --no-textconv`
    (`TestHooksNeverRunDuringSnapshotCheckoutAndRemove`,
    `TestNoLazyFetchWhereGitIgnoresNoLazyFetch`,
    `TestGitEnvDropsRedirectsAndPinsSafety`).
- **Residual risk:** a search query can carry repository text to the search
  provider (accepted, P8-R14). anthropic-api research pairs repository reading
  with server-side web fetch; Anthropic only fetches URLs already in the
  context, and 5 uses cap a link-following leak, but this is provider
  behaviour, not live-verified here (review R5). No cross-session daily
  reservation (at most one extra run per session) [triage Q13]; `--max-budget-usd`
  stop behaviour is a user gate [triage X10].

### B5. Shared dot packs and project dots (trust, the clamp, hidden characters, TOML bombs)

- **Assets:** everything a dot can reach; memory.
- **Threats:** someone else's dot turns on inject, write, MCP, research,
  web, schedules, commits or a `base_url`; passes CLI flags; hides text with
  invisible characters; a TOML bomb hangs the engine; a changed file keeps
  an old trust.
- **Mitigations:**
  - `internal/dots/clamp.go` (`ClampDot`): every keepable capability off
    unless kept one by one, args reduced to `--model`/`--effort` with plain
    values, budgets capped (`TestClampDotTurnsEveryRiskyCapabilityOff`,
    `TestClampDotHandlesEveryDotField`, `TestClampKeepsOnlyModelAndEffortArgs`).
  - `internal/projectdots` (`ScanProject`, `Item.Runnable`, `TrustStore`):
    trust binds `(project id, path, SHA-256 of the bytes shown)`; any edit
    revokes it; a corrupt store fails closed and is never overwritten
    (P9-R12; `TestRunnableIsNilUnlessThisContentIsTrusted`,
    `TestTrustRefusesAFileChangedSinceReview`,
    `TestTrustPreviewShowsHiddenCharactersAndHashesTheBytesRead`).
  - `internal/dots/tomlscan.go` (`PreScanTOML`) and `DecodeStrict` (unknown
    keys refused, origin never trusted, P9-R7): `TestDecodeProjectRefusesTOMLBombsFast`,
    `TestImportTOMLRefusesBombsFast`, `TestParseRefusesTOMLBombsFast`,
    `TestLoadAllRefusesAUserDotBombFast`; project files are regular files only,
    capped in size and count (`dots.ReadProjectFiles`).
  - `internal/dotpack`: https only, same-host redirects, no credentials,
    size cap (`TestFetchHTTPSOnlyCappedAndReportsTheFinalURL`,
    `TestFetchSendsNoCredentialsAndStaysOnTheHost`); pack memory lands as
    proposals that never suppress (`TestImportPackStaysProposedEvenWithAutoApprove`,
    `TestImportedDismissedMemoryNeverSuppresses`, P9-R8, P9-R11); invisible
    carriers refused (`TestImportPackRefusesInvisibleCarriers`).
- **Residual risk:** trust is keyed by the git root commit, so a fork or
  clone sharing it trusts byte-identical files (by design,
  `TestTrustIsSharedByClonesOfTheSameProjectOnly`). An in-limit hostile file
  still costs about 60 ms per 100 MB to pre-scan [triage H11]; `dots untrust FILE`
  revokes every checkout [triage H7]. A trusted project dot's purpose is a prompt
  like any other (B1).

### B6. Phase 2 memory (VITAL): provenance, suppression, secret scanning

- **Assets:** the project's memory, which steers every future review.
- **Threats:** injected text becomes a convention; a model silences its own
  future findings; a secret is stored; the developer's words are rewritten.
- **Mitigations:**
  - `internal/memory/apply.go` (`Apply`): models add dot-scope entries
    (active) and project-scope entries (proposed); never dismissed,
    preference or global; update/forget only their own model-written entries
    and their own still-proposed project entries; text and paths are
    sanitized, then scanned (T3; `TestApplyRefusesWhatModelsMayNotWrite`,
    `TestResearchKindsOnlyInResearchPasses`).
  - `internal/memory/propose.go` (`Propose`): agent proposals via MCP stay
    proposals, at most 20 waiting (P5-R5, `TestTwentyFirstPendingAgentProposalIsRefused`,
    `TestProposeNeverRewordsASimilarEntry`).
  - `internal/engine/memory.go` (`suppressedBy`): only outcome and user
    findings in the same session window, and dismissed entries (which models
    can't write), suppress; imports never do (T4; `TestSuppressedBySkipsImportedEntriesInEveryScope`).
  - `internal/secrets` (`Scan`, behind `memory.ScanSecrets`): known key
    shapes, assignments, high-entropy tokens, and the same text without
    invisible characters (T6; `TestIgnorableFillersDoNotHideASecret`).
  - Approve shows the whole proposal or refuses (T5; P2-R9 closed by
    Task 10).
- **Residual risk:** homoglyph and space-split secrets pass the scanner
  (Known gaps). Ledger audit lines can be lost or over-scrubbed [triage M17, M19,
  M25]; a rejected agent proposal can be proposed again [triage M33]. `enter`
  deletes a deleted dot's memory by default [triage U22, a user decision]. Project
  memory follows the git root commit, so a fork shares it (by design).

### B7. MCP: the main agent asking dots; Codex and Claude MCP isolation

- **Assets:** money, memory, the queue's contents.
- **Threats:** the agent approves its own suggestions, writes memory
  directly, recurses through dots, or a dot run starts the developer's other
  MCP servers.
- **Mitigations:**
  - `internal/mcpserver/server.go`: six tools, queries only (list, ask,
    council, list queue, read memory, propose memory); `propose_memory` is a
    proposal; every tool refuses without `HERDR_PANE_ID`, which `CleanEnv`
    strips from dot runs (T14; `TestServeMCPOutsideHerdrRefusesEveryTool`,
    `TestProposeMemoryLandsAsAProposalAndNotifies`); answers are redacted and
    forged headers neutralised (`TestMCPAnswersAreRedactedAndNeutraliseForgedDotHeaders`);
    clamped and untrusted project dots can't be asked
    (`TestMCPRefusesUntrustedAndClampedProjectDots`); a broken config offers
    only `list_dots` (`TestServeMCPFailsClosedOnABrokenConfig`).
  - Isolation of dot runs: claude `--strict-mcp-config`; codex gets `-c
    mcp_servers.<name>.enabled=false` for every declared server and refuses
    to run when a name can't be addressed (P10-R7, P10-R8;
    `TestCodexDisablesEveryDeclaredMCPServer`,
    `TestCodexRefusesAServerItCantDisable`).
- **Residual risk:** the MCP restriction is not a security boundary against
  the agent itself: the agent has a shell and runs as the developer, so it can
  talk to the engine socket directly (B9). An `ask_dot` to a claude-cli dot
  runs it with the read tools in the pane's directory, and those reads are
  not confined to it: the dot can read files outside the repository (secret
  files are denied by name, review R3), and its answer goes straight to the
  agent without the approval queue. `read_memory` returns global
  entries of dots not attached to the pane [triage S16]. A project-level
  `.codex/config.toml` server isn't detected [triage P1]. Codex paths are not
  live-verified [triage X3].

### B8. Credentials and keys (redaction everywhere)

- **Assets:** API keys, OAuth tokens the SDK reads.
- **Threats:** a key reaches a log, the feed, a snapshot, IPC, argv, an
  error, memory or history; another user reads `credentials.toml`; a
  redirect forwards the key.
- **Mitigations:** `internal/credentials` (owner-only file checked on the
  open descriptor, `O_NOFOLLOW`, DACL on Windows; `Redact` with exact known
  keys first, then key shapes and whole `Authorization` values; `entry`
  never formats its key) (T7); `internal/backend/httpx` (same-host
  redirects, no scheme change, P10-R7 S15; `TestKeyIsNotForwardedOnCrossHostRedirect`,
  `TestSameHostRedirectRefusesASchemeChange`); SDK-chain tokens redacted by
  value (`TestSDKChainTokensAreRedactedFromErrors`); history and memory run
  `credentials.Redact` and the scanner (`TestDefaultRedactionNeverStoresKeys`).
  Keys travel in headers, never argv; CLI dots use the CLIs' own login.
- **Residual risk:** environment keys aren't shape-checked [triage S11]; tokens
  the Anthropic SDK reads from files or federation aren't redacted by value
  [triage S23]; query-string key names aren't redacted by name [triage S13];
  `credentials.toml` read-modify-write has no lock [triage S10]. A repository could
  have redirected a claude-cli dot's API endpoint through its settings until
  this task (T22, fixed).

### B9. IPC: a same-user, unauthenticated socket

- **Assets:** every engine command: send, dismiss, edit, trust, set mode
  (including inject), draft, apply a patch, attach, pause.
- **Threats:** another local user, or any program running as the developer,
  drives the engine.
- **Mitigations:** Unix: the socket lives in the 0700 per-session state
  directory (or a 0700 per-uid directory in the temp dir when that path is
  too long) and is made 0600 (`internal/ipc/endpoint_unix.go`,
  `TestListenReplacesAStaleSocketAndIsPrivate`); Windows: a named pipe whose
  DACL grants only the user, and a client check that the pipe's owner is the
  user (P6-R2; `TestPipeSDDLGrantsOnlyTheUser`, `TestPipeOwnerError`). Each
  request is size-capped and decoded into typed structs (T12). Apply needs
  the reviewed hash; trust needs the shown hash.
- **Residual risk, stated plainly:** the endpoint is **unauthenticated**.
  Any process running as the developer, the coding agent included through
  its shell, can connect and send every command, including approving and
  sending cards, trusting a project dot, turning on inject and applying a
  patch (it can read the hash from a snapshot). The approval gates protect
  against the plugin's own models and against acting on unseen text in the
  UI; they are not a boundary against other programs of the same user, who
  could edit the files directly anyway [triage I8]. On Linux, a local user who
  pre-creates the temp-dir fallback directory stops the engine from starting
  (denial of service, review R8). There are no server write deadlines [triage I6].

### B10. Install and update (checksums, cosign optional, source fallback)

- **Assets:** the binary Herdr runs at every start.
- **Threats:** a tampered download, a substituted archive, a half-installed
  binary, a malicious archive member, a vulnerable toolchain.
- **Mitigations:** `scripts/build.sh` and `build.ps1`: the checksum comes
  from `release/SHA256SUMS` in the cloned tree, never the server; HTTPS-only
  redirects with curl; a mismatch, a missing binary or a link member falls
  back to a source build; the binary is placed before the record that vouches
  for it (T17; `TestVerifiedReleaseIsInstalled`,
  `TestUnsafeArchiveMembersFallBackToSource`,
  `TestFailedReleaseInstallStepFailsTheScript`). `herdr-dots verify-release`
  re-checks the kept archive, the installed binary (the last member, like
  tar) and, with cosign installed, the keyless signature from the
  repository's `release.yml` (`TestVerifyReleaseHashesTheLastMemberLikeTar`).
  The publish job refuses archives that differ from the committed checksums.
- **Residual risk:** cosign is optional; checksums alone vouch only for the
  tree the developer cloned. wget can't restrict redirect protocols (the
  checksum still decides). A source build uses whatever Go the developer has.
  Release builds use `go-version-file: go.mod`, which says `go 1.25.0`, so
  setup-go most likely installs go1.25.0; already go1.25.7 has 24 reachable
  standard-library vulnerabilities. Setting an explicit patched `go-version`
  in every workflow is a user gate [triage X12, review R2]; the release
  workflow's candidate job runs govulncheck, so a tag with a vulnerable
  toolchain stops before any checksums are proposed. Actions are
  pinned by tag, not SHA [triage W16]. `release/REPO` and the checksums are empty
  until publishing, so installs build from source today.

### B11. Windows (compiled and unit-tested only)

- **Assets:** the same as on Unix.
- **Threats:** cmd.exe shim injection, junctions and reparse points that
  leave the jail, ACL inheritance that exposes files, pipe squatting,
  repository-local executables found before PATH.
- **Mitigations:** `internal/platform` (batch-argument quoting that refuses
  unsafe characters, `TestCommandRefusesUnsafeShimArguments`,
  `TestExecRunnerRefusesUnsafeShimArgsBeforeStarting`; owner-only DACLs,
  `TestPrivateWritesGetAnOwnerOnlyDACLEvenInAnOpenDir`; `OpenNoFollow` and
  `VerifyOpenedPath` against junctions, `TestOpenNoFollowRefusesAJunction`,
  `TestAJunctionOutOfTheRootIsRefused`, `TestCredentialsThroughAJunctionAreRefused`;
  `ChildEnv` with `NoDefaultCurrentDirectoryInExePath`; reserved device
  names, `TestWindowsReservedDeviceNamesIncludeZeroAndSuperscripts`); the
  portability guard keeps every child process on `platform.Command`
  (`TestProductChildrenGoThroughPlatform`).
- **Residual risk:** none of this has run on Windows here: it is compiled
  and unit-tested (cross-compiled, plus CI when pushed), a user gate [triage X1,
  X2, W6, W7, W17] (P6-R1). Known: a residual `Purge` race [triage W1, W2],
  inherited ACLs on `CreateFile` [triage W4], an aliased `os/exec` import would pass
  the guard [triage W13], user names with `%` or `!` can't run npm shims [triage W15].

## Known gaps

- Homoglyph secrets (Cyrillic look-alikes) and secrets split by spaces pass the scanner.
- The memory block and dot purposes are prompts: a determined injection can still make a dot *say* something wrong. Dots can't act on it (read-only), and memory written from it waits for approval.
- `lockfile.Held` probes with the real lock (see the triage notes).
- Research runs don't apply the developer's own `~/.claude` deny rules (`--restricted`); the plugin's secret-file deny list and the config's `tool_deny_globs` still apply, and research has no shell and no WebFetch.
- A dot file the developer writes can still pass some `claude`/`codex` flags the denylist doesn't name (short forms, combined flags; see the triage notes). Imported and project dots keep no args but `--model`/`--effort`.
- Linux and Windows privacy (modes, DACLs, pipes) is tested in CI only; not run on those systems here.
- The engine endpoint is unauthenticated for processes of the same user, the coding agent included (B9).
- claude-cli review, ask, memory and council runs deny secret files by name but can still read other files outside the repository; whether Read deny rules also bind Grep and Glob is unverified (review R3, triage X9).
- Codex dots may read repository instructions and project config that the plugin doesn't control (review R6, [triage S24]).
- `--setting-sources user` (T22) is pinned by argv tests and the flag is present in the installed Claude Code 2.1.295, but a live run that shows a repository `apiKeyHelper` is ignored is a user gate, like the `--restricted` re-check [triage X8].
- Release binaries must be built with a patched Go toolchain: set an explicit `go-version` in every workflow (review R2, [triage X12]).

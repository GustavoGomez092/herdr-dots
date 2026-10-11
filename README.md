# Herdr Dots

> **1.0.0-rc.1, release candidate.** Claude Code dots are live-verified; the API backends and Codex are not yet; see [Known issues](#known-issues).

Herdr Dots is a [Herdr](https://herdr.dev) plugin that adds **dots**:
specialist agents (security, frontend, backend, copywriter, ...) that join a
Claude Code or Codex session running in a Herdr pane. After every turn of the
main agent, each attached dot checks the turn against its purpose and speaks
up only when it has something worth saying. Dots remember your project, can
convene as a council, run on Claude Code, Codex or API models, answer your
agent over MCP, draft fixes in a sandbox when you allow it, research while
you're away, and can be shared with your team.

## Status

Every roadmap phase (1–10) is built. The test suite passes in CI on macOS,
Linux and Windows, and the macOS acceptance run has evidence for every item
it can show ([notes](docs/superpowers/notes/1.0-acceptance.md)). The core loop also
passes end to end on Linux and Windows with Herdr. Open before 1.0.0: the
popup screens in an interactive Herdr session on Linux and Windows, and live
checks with the API backends and Codex ([checklist](docs/release/ready-to-publish.md)).

## Requirements

- Herdr 0.8.0 or newer on macOS or Linux; 0.9.1 or newer on Windows (beta)
- `claude` (Claude Code) logged in for `claude-cli` dots, `codex` for `codex-cli` dots, or an API key for API dots
- Go 1.25 or newer only when no prebuilt binary fits (the install falls back to a source build)

## Install

    herdr plugin install GustavoGomez092/herdr-dots

Details, checks and upgrades: [docs/guide/install-and-upgrade.md](docs/guide/install-and-upgrade.md).

## Quick start

1. Open Dots (`prefix+d`, or 'Dots: manage' in Herdr's action menu). The first time, it offers to install the MCP server and the keybindings.
2. Create a dot from a template (Security is a good first one).
3. Focus your Claude Code or Codex pane and attach the dot; press `prefix+a` to open the Dots pane.
4. Work as usual. Cards wait in the queue: `enter` sends one, `x` dismisses it, and dismissing teaches the dot not to raise it again.

## Guide

[docs/guide/](docs/guide/README.md): dots, delivery modes, memory, council,
backends and cost, MCP, Windows, write-capable dots, proactive dots,
profiles and sharing, install and upgrade.

## Privacy and safety

- **No telemetry.** The plugin talks only to the backends you configure, Herdr's local socket, and GitHub when it installs.
- Dots are read-only unless you opt a dot into write mode (then two approvals, a separate worktree, no shell).
- Memory and history stay on your machine, in the plugin's state directory; every entry can be listed, edited, forgotten or purged.
- Keys stay in `credentials.toml` (owner-only) or environment variables.
- `NO_COLOR=1` turns colors off (bold, dim and reverse stay).
- Every rule in detail: [docs/guide/privacy-and-safety.md](docs/guide/privacy-and-safety.md). Security policy: [SECURITY.md](SECURITY.md); threat model: [docs/security/threat-model.md](docs/security/threat-model.md).

## Uninstall

    ./bin/herdr-dots uninstall-extras            # MCP entries and keybindings, with backups
    ./bin/herdr-dots uninstall-extras --purge    # also every dot, setting, credential and all memory
    herdr plugin uninstall herdr-dots

`uninstall-extras` removes only the lines herdr-dots added, shows exactly
which lines go and saves a backup first. `--purge` asks you to type a phrase
and refuses links, running engines and folders that aren't the plugin's own.
Details: [docs/guide/install-and-upgrade.md](docs/guide/install-and-upgrade.md#uninstall).

## Known issues

Detail for every item (the ids in brackets), with the reason each is not
fixed in 1.0.0-rc.1:
[docs/superpowers/notes/2026-10-08-phase-10-triage.md](docs/superpowers/notes/2026-10-08-phase-10-triage.md).

Deleting a dot keeps its memory unless you press `y` at "Also delete its memory in all projects? (y/N)" [U22].

- **Secrets and privacy:** the scanner errs toward refusing (`Bearer` + prose, `tokenizer=`, any assigned value with a digit), so a false positive refuses or asks, never leaks [S2, S3]; `CleanEnv` strips HERDR_* and the Claude session markers but passes the auth/config `CLAUDE_*` variables [S7]; keys from environment variables aren't shape-checked [S11]; `git push --mirror` or a `refs/*` refspec would push the private snapshot refs [S17]; the feed's learned/error detail and a card's error are sanitized when stored, not again when drawn [S19]; a stray `api_key` line in a local dot file is ignored silently (project and imported dots refuse unknown keys) [S20]; research runs `--restricted`, which drops the developer's own `~/.claude` deny rules (research still denies every built-in secret glob and the config's `tool_deny_globs`, and has no shell and no WebFetch) [S21]; on the Anthropic SDK's own credential chain, tokens the SDK reads from files (an `ant auth` profile) or gets by federation exchange aren't redacted by value (key shapes and auth header values still are) [S23]; **codex dots read the repository through their own sandbox, so `RepoRead`, `tool_deny_globs` and the invalid-glob switch don't limit what they read** [S24]. `claude-cli` runs, drafts included, now deny reads of home secrets (`~/.ssh`, `~/.aws`, `~/.claude` and the like) and of the herdr-dots config directory; this is not live-verified for Grep and Glob [X9]. Reason: none of these sends a key or file content anywhere the developer didn't configure.
- **Memory (VITAL):** `r` doesn't reset `MemoryPasses` [M3]; with `memory_enabled = false`, attaching still runs git (repository programs disabled) and writes `meta.json` (name and roots) [M4]; a suppressed verdict still sets the cooldown [M5]; an edited-then-dismissed card records the edited text [M6]; stats can be stale for a moment after memory is switched off at runtime [M7]; an outcome's session is taken at record time, so suppression can miss across a session change [M8]; `FirstSentence` cuts at abbreviations [M11]; `Similar` is unbounded on long inputs (texts are capped at 500 runes) [M12]; git discovery walks above the cwd, by design [M13]; remember-op caps live in `Apply`, not `cleanOp`, and a few op paths are untested [M14]; ref fields aren't one-lined and the block has no end marker [M16]; a deleted dot's ledger scrub can miss the older cards' lines of a merged entry whose text a later merge rewrote (ledger lines are an audit trail, not memory entries) [M17]; **unparseable ledger lines are dropped when the ledger is rewritten** [M19]; `readScope` doesn't check kind or status (fuzzed) [M20]; some apply tests are weak [M23]; `RecordOutcome` now takes one lock for the ledger line and the entry, but a crash between the two writes can still leave the ledger line without its entry [M25]; `Apply` relies on `checkTarget` for the slug [M26]; an unregistered project id creates an empty directory [M29]; `memory add --force` is the consent, not the secret gate [M31]; a rejected agent proposal can be proposed again (no rejection ledger; pending proposals are capped at 20) [M33]; full-scope proposals are archived soon after [M34]; newer research can lose to older under the rule that approved memory is never displaced by an unapproved write and isn't deduped against archived entries [M35]; `keysConfirmMemory` drops counts on a partial error [M37]. Reason: false positives refuse rather than leak; archived entries are already out of the prompt; approved memory is never displaced by an unapproved write.
- **Git and repository safety:** a pane in a repository subdirectory walks the tree without `.gitignore` (the jail and deny list still apply) [G3]; the listing's git output is bounded only by the run timeout [G6]; **two engines on one repository drop each other's snapshot refs (by design, X8), which ends that draft** [G7]; `CheckPath` doesn't strip HFS-ignorable code points (`.g‌it`): git's `core.protectHFS` (on by default on macOS) refuses HFS-equivalents of `.git`, and APFS doesn't ignore them [G8]; no per-run write byte cap, absolute worktree paths in errors, a trailing slash refused [G9]; `strayOps` can match "index" inside base85 binary data (fails closed, about 1 in 85⁴) [G10]; the `old mode 120000 → 100644` branch is dead in practice [G11]; a small window between proactive-off and a review starting on new heads [G13]. Reason: every one fails closed or stays inside the developer's own checkout.
- **Engine lifecycle:** a crash-looping engine is respawned about every 6 s [E3]. Reason: rare, recoverable by restarting Herdr; `engine.log` is documented.
- **Upgrading (Linux only):** when the state directory is deep enough that the engine socket falls back to another directory, an old engine and a new client can miss each other until Herdr restarts (security review R8). Reason: it needs a deep state path on Linux and an upgrade while an engine is running; restarting Herdr clears it.
- **Card delivery and pane close:** **when a pane closes inside one delivery call, that card's final status can read orphaned although its text reached the agent, and its sent/orphaned outcome can be missing from memory and history (so that finding may repeat once)** [E40]. Reason: needs the pane to close during the call; one card.
- **Transcripts:** a slash command's `<command-name>` record still counts as a turn [T1]. Reason: at most one extra turn is reviewed; dropping it later would shift turn indexes already counted.
- **Verdicts and backends:** `Hint` matches a bare "401"/"login" [B2]; `codex turn.failed` after an earlier `agent_message` is ignored [B4]; Codex can't disable an MCP server whose name holds '.' or '=' from its command line (it splits `-c` on '=' and '.'), so a codex dot run is refused until that server is renamed [B29]. Reason: a hint, or it fails closed; Codex is not live-verified.
- **Queue, state and feed:** every card change rewrites the whole queue file (indented JSON), which costs CPU under heavy load but keeps nothing in memory [Q19]; **in-memory state changes before the save, with no rollback when the write fails** (with the read-only gate for files a newer version wrote, this is the intended "read-only mode") [Q1, Q3]. Until the first migration of a kind is registered, the command-line gate doesn't keep files that need a migration read-only (none can exist yet: every kind is at version 1) [Q16]; deleting a dot from the manage popup skips the read-only gate, at the developer's request [Q17]. Reason: no corruption on disk; a corrupt file is reported.
- **Council:** `ParseDigest` output is not clipped (the card shows it whole; it is sanitized and fuzzed); a negative `max_members` clamps to 2 while 0 means 8; dot names and preflight reasons are rendered as the developer wrote them; the chair is charged if shutdown lands mid-chair; one council can overrun the pane budget between preflight and member runs. Reason: developer-controlled input, cosmetic, or bounded by one council.
- **API backends, keys and cost:** keys from environment variables are not shape-checked; a prices override without `as_of` keeps the embedded date; an unknown `[providers.x]` table in `config.toml` is logged and toasted, and that provider runs on its defaults and fails visibly [L20]; a pane in a repository subdirectory walks the tree without `.gitignore`; the manage popup reads prices once when opened; `CostServed` falls back to the requested model's price when another model served; zero-token failed runs record a known $0; `chairWarned` is set when merge isn't `chair`; OpenRouter BYOK runs report $0 (call budgets still cap them). Reason: display or accounting edge cases; dollar and call budgets still cap spend.
- **MCP server:** a project-level `.codex/config.toml` server is not detected and a relative `CODEX_HOME` is not resolved; the manage footer reports install records, not live client configs; a consulted turn doesn't decrement the cooldown; `Usage.Add` fsyncs under `e.mu`; `paneClosed` matches an error's text suffix; spend after the last dot is detached isn't shown. Reason: the agent can't approve anything itself, budgets cap spend, and the rest is cosmetic.
- **Windows:** **a residual race between `Purge` and a concurrent writer** [W1]; `EINVAL`/`ACCESS_DENIED` opens during `Purge` aren't retried and a reader can recreate a purged project as an empty directory [W2]; files created with `CreateFile` inherit the private directory's ACL; stale `herdr-dots-run-*` directories aren't swept at start; a Windows user name containing `%` or `!` can't run npm shims (refused, not run). Reason: Windows is beta and not run here (user gate); the private directory protects the files; the lock identity check closes the acknowledged-write loss.
- **Write-capable dots:** the `old mode 120000 → 100644` branch is dead in practice; `FlagAgentConfig` lives in a heavier package than it needs; `x` discards a patch card without a confirm; the `ui/diff` sweep test takes about 30 s. Reason: everything fails closed or is cosmetic.
- **Proactive dots:** no cross-session daily reservation (overshoot of at most one run per session); the daily toast is per engine; `route()` has no re-check after `Add`; `charge()` has no generation check; a huge finite budget is accepted; inbox claims compare-and-delete by token (a suspend longer than the TTL duplicates a card, never loses one); corrupt inbox asides aren't cleaned up; a commit counts as reviewed once one pane's run starts, so a second pane with the same dot gets no card for it [R4]. Reason: bounded spend, no unapproved action, and research can't send file contents anywhere but a search query.
- **Profiles and sharing:** local dot args can still use `claude` short forms (`-c`, `-r`), combined short flags and flags such as `--chrome`, `--cloud`, `--remote-control`, `--teleport`, `--from-pr`, `--file`, `--debug-file`, `--bg`, `--permission-prompts`, and `codex` `--output-schema`/`-i` file reads (only the developer's own dot files can carry them: imported and project dots keep no args but `--model`/`--effort`) [B9]; a scheduled project dot runs the first checkout's copy only; pruning stops scheduled project dots in idle checkouts (fails safe); chair spend is not in the history totals (usage budgets count it); an unidentified test flake was seen once (`TestTwoEnginesRunASlotOnce` also timed out once under heavy parallel load). Also: the templates' delivery mode isn't pinned by a test. Reason: developer-controlled input, fail-closed behavior, bounded cost or test-only.
- **Snapshots:** a pane with more than 100 visible cards shows the first 100 (most severe, oldest) and the rest wait unseen until earlier ones are resolved; the header's QUEUE count includes them. Reason: the snapshot size limit (100 cards, 50 feed entries, 256 KB); a card is shown whole or not at all.
- **Platforms:** `windows/arm64` is built and published but never tested; Linux and Windows have only CI and cross-compiled tests here (user gate); Codex paths are documented-format, not live-verified (user gate).

Things that rest on the developer's own machines or accounts (user gates), and
what has not been run live:

- Codex transcript reading and the codex backend are built from Codex's documented formats and
  have **not been verified live**.
- Verified live: Herdr reports `done` (not `idle`) after an agent turn; the engine handles both.
- The first `agent start` in a new directory may stop at Claude's folder-trust prompt.
- The Dots pane opens at Herdr's default split width.
- Memory retrieval is lexical (paths, keywords, recency), not semantic.
- The Codex backend's `remember` support follows Codex's documented output format and has not been
  verified live.
- Windows support has not been run on Windows by the developer; it is cross-compiled and
  unit-tested only. The three-OS CI workflow is written but not yet pushed anywhere.
- The MCP server is verified live with Claude Code; Codex support follows Codex's documented config
  format and has **not been verified live** (see `docs/superpowers/notes/2026-10-08-phase-5-verification.md`).
  That includes the `-c mcp_servers.herdr-dots.enabled=false` override codex dot runs get.
- No MCP resources or prompts, no HTTP transport, no access for agents outside Herdr.
- API backends are tested against recorded-style fakes; live runs need your keys (see
  `docs/superpowers/notes/2026-10-08-phase-4-verification.md`). Only Ollama has been run live; the
  Anthropic, OpenAI and OpenRouter paths are not live-verified. OpenAI model ids and prices change
  often: check `prices.toml` against the pricing page.
- No streaming for API dots; no OS keychain; no Bedrock/Vertex/Foundry providers (Gemini and others
  are reachable through OpenRouter).
- A council in progress doesn't survive an engine restart.
- Long council deliveries were verified live with Claude Code only; Codex is untested.
- Write-capable dots are unit-tested, with real git, on macOS only; a real draft inside
  Herdr, Ubuntu's git and Git for Windows are still to be tried (checklist in
  `docs/superpowers/notes/2026-10-08-phase-7-verification.md`, "User gates"). A live Claude draft
  has not been run on this machine, and on Windows drafts are compile-checked and unit-tested only.
- `codex-cli` dots can't draft (Codex has no verified way to run without a shell).
- Drafts can't run tests or builds: write-capable dots get no shell.
- Proactive dots are unit-tested on macOS only, with a fake clock and real git, and were
  soak-tested once in an isolated Herdr session on macOS with `claude-cli` haiku dots (results in
  `docs/superpowers/notes/2026-10-08-phase-8-verification.md`). The soak drove the engine with a
  test client: the Dots pane was not used by hand, the daily budget was checked by lowering it, and
  a non-critical "away, then one card when you come back" digest was not seen live (every research
  result in the soak was critical). Web research was not part of the soak; Codex and OpenAI web
  search are not live-verified. Schedules run only while Herdr runs.
- Profiles, dotpacks and project dots are unit-tested on macOS only. The manage popup's
  live check was not run on this machine, and a project dot's trust and pause have not been run end
  to end inside Herdr (checklist in `docs/superpowers/notes/2026-10-08-phase-9-verification.md`).
  A repository can ship a dot with the same display name as one of yours: the `proj:` slug and the
  trust preview tell them apart.

- The PowerShell install (`scripts/build.ps1`) has not been tested here.
- `cosign` is optional: `herdr-dots verify-release` checks checksums without
  it and says the signature was not checked.
- Claude, Anthropic, OpenAI and OpenRouter live checks, Codex end to end, the
  three-OS acceptance run on real Linux and Windows machines, and the manage
  popup's live check are user gates and have not been run.

## Roadmap history

| Phase | Version | Increment |
|---|---|---|
| 1 | 0.1.0 | Core loop |
| **2** | 0.2.0 | **Dot memory (VITAL)** |
| 3 | 0.3.0 | Council mode |
| 4 | 0.4.0 | API and Ollama backends, keys, cost |
| 5 | 0.5.0 | MCP server |
| 6 | 0.6.0 | Windows support |
| 7 | 0.7.0 | Write-capable dots in sandboxed worktrees |
| 8 | 0.8.0 | Proactive dots |
| 9 | 0.9.0 | Profiles, history and sharing |
| 10 | 1.0.0-rc.1 | Release hardening: migrations, fuzzing, soak, verified installs, threat model |

## License

Apache License 2.0: see [LICENSE](LICENSE).

# Changelog

All notable changes. Versions before 1.0.0-rc.1 were development
milestones, never released.

## 1.0.0-rc.3 (release candidate, published 2026-10-11)

Published as a GitHub pre-release with six signed, checksum-verified archives (cosign verified); installs checked on macOS and on Windows x86_64 and arm64.

Fixes found by the interactive check on Windows (the real Herdr client in a ConPTY on GitHub's Windows runner; popups and keybindings all pass).

### Fixes

- Windows: the MCP setup records the plugin's path without the `\\?\` long-path prefix Herdr can start plugins with, and treats an entry recorded with it as the same server, so the Setup screen and `setup-mcp` agree on whether the MCP server is installed.
- A prompt too small to show its field (the memory export, import and purge prompts) ignores typing until it fits, so nothing is typed into a hidden field.

## 1.0.0-rc.2 (release candidate, published 2026-10-11)

Published as a GitHub pre-release with six signed, checksum-verified archives (cosign verified); installs checked on Linux, macOS and Windows.

Fixes found by the first real installs on Windows and the Linux end-to-end and interactive checks.

### Fixes

- Windows install: `build.ps1` hashes and unzips with .NET, so `herdr plugin install` works when Windows PowerShell is started from PowerShell 7 (found by the first real install on Windows; `herdr plugin install` takes the fix from `main`).
- a turn review that repeats a finding whose card from the same dot is still waiting on the pane (pending, or auto not yet delivered) adds no second card; the feed says `suppressed (duplicate of card <id>)` and the run is charged as usual. Resolved cards never block a new one (memory handles those), and summoned answers and council cards are never deduped. The roster now reads `suppressed` or `duplicate` after such a turn, not `passed`.
- keys typed fast are no longer dropped. Bubble Tea merges printable keys that arrive in one read into one key message; every Dots TUI (pane, manage, memory, summon, council, diff viewer, profile, Setup screen) now takes such a burst one key per rune while browsing. Text fields get it as typed, so pasted text never becomes commands. A burst of keys never answers a confirm opened by the same burst, and confirms cancel on any other key (the draft confirm and the remember chooser now do too). A burst also ends at a key whose screen opens a moment later (a preview, the purge screen), so the rest never runs on the screen underneath. The Setup screen and the trust and dot-import previews scroll on a burst but take `y` only as its own key.
- wrapped card text, delivery errors, the draft instruction and the council note and member answers keep their indent on every line.
- the selected roster row, and the selected library row with its badge, are highlighted whole, not only up to the dot's glyph.
- the manage popup's footers and the Setup screen's command line (the long `claude mcp add`) wrap between words; only a word longer than the line is broken. The trust preview's file and the Setup screen's plan text wrap byte for byte, every space kept, and each wrapped row of the file starts with `↪` so it never passes for a line of its own.
- the Setup screen's "Too small" message fits the popup's height, so its title and "Too small" are never pushed off the top; the same holds for the memory screen's "Too small" screens.
- "1 dot" in the council estimate and the council card header; the chair's Markdown headings show as plain labels ("Must fix:") on council cards; the memory screen hides its browse key line while a prompt (export, import, filter, a confirm) is open; the Setup screen's scroll hint shows only when the screen is longer than one page.
- Known issue (B2, Herdr 0.9.3): a newly toggled Dots pane draws at its pre-split width until it is focused; focus it once, or resize, to fix it (README, Known issues).

## 1.0.0-rc.1 (release candidate, published 2026-10-11)

Published as a GitHub pre-release with six signed, checksum-verified archives. Everything below shipped in it.

Hardening after the first rc.1 build: about 90 accepted known issues and security findings R2, R3, R5 and R8 are fixed (see the triage notes).

### Changes

- Licensed under the Apache License 2.0 (`LICENSE`).
- Security reports go through GitHub's private vulnerability reporting (`SECURITY.md`).
- First-run Setup screen: the first time you open the manage popup, it offers to install the MCP server (Claude Code, Codex) and the keybindings, shows the exact change in a scrollable screen, and applies it only on `y` after every line has been on screen (`n` asks again in 7 days, `x` never). The Dots pane only shows a one-line hint while setup is pending. `S` in the manage popup reopens it; `herdr-dots setup` does the same from a shell. Answers are kept in `onboarding.json` (a new versioned config file). The setup commands start `claude`, `codex` and `herdr` without any inherited `CLAUDE*` variable except `CLAUDE_CONFIG_DIR` and `CLAUDE_CODE_GIT_BASH_PATH`.
- `claude-cli` dots no longer receive Claude Code session variables (every `CLAUDE_CODE_*` except the provider, auth and Git Bash path settings), even from an engine started straight from a Claude Code session.

### Security

- Release builds use Go 1.26.9: `govulncheck` reports no vulnerabilities (Go 1.25 had 24 reachable standard-library ones). The workflows also pin their GitHub Actions by commit and GoReleaser at v2.18.3.
- `claude-cli` runs, drafts included, deny reads of home secrets (`~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.claude` and the like) and of the herdr-dots config directory. Not live-verified for Grep and Glob.
- `anthropic-api` research searches the web only; it no longer fetches pages.
- The MCP `read_memory` tool returns global entries only of dots attached to the pane.
- The engine socket's fallback directory (Linux) prefers `/run/user/<uid>` and refuses a symlink, a directory another user owns or one with loose permissions, with an error that names it.
- Renamed or copied denied files are left out of commit reviews.
- Deny globs match names in both Unicode forms (NFC and NFD); a file name stored decomposed on macOS is listed.
- `ExecRunner` cleans the child's environment itself; key redaction also covers query-string credentials and the content of `ANTHROPIC_IDENTITY_TOKEN_FILE`.

### Memory

- Deleting a dot keeps its memory unless you press `y`.
- Forgetting a finding scrubs only its own session's ledger lines; pruning never deletes pinned entries; a project's outcome and ledger line are written under one lock.
- Hand-edited or overflowing memory ids are renumbered instead of lost; ties in memory selection are ordered.

### Fixes

- Unknown keys in `config.toml` (and unknown `[providers.x]` tables) are logged by name and toasted once.
- Cancelled runs read as cancelled, not timed out; a Claude error on exit 0 gets the login hint; a verdict with prose braces around the JSON parses.
- A pane's call budget toast appears once; resuming a dot mid-run shows it thinking; `memory --help` works without the plugin directories.
- Concurrent `keys set` and pane toggles no longer lose a write; a starting engine outwaits a lock probe.
- The summon popup can't summon twice on a double `enter`; narrow panes cut card headers; the manage popup keeps your attachments made elsewhere.
- A commit counts as reviewed once one pane's run starts.
- The IPC server and Herdr client honour cancellation and write deadlines.

### Changes

- Upgrade note: attachments made by the older turn parser re-base once, so the first review after upgrading may look at the latest turn again; slash-command output and compact summaries no longer count as turns.
- ASCII mode now covers the pane, the memory screen, the summon popup and the template list.
- Upgrade note (Linux only, deep state paths only): an old engine and a new client can miss each other until Herdr restarts.

### Phase 10 release work

- Schema versions for every persisted file and a migration framework: older files are upgraded with a backup, files from a newer version stay read-only (`herdr-dots migrate`).
- Prebuilt, checksum-verified binaries once published (user gate) for macOS, Linux and Windows (amd64, arm64), with a source-build fallback; `herdr-dots verify-release`; `--version` shows the build source and commit.
- `herdr-dots uninstall-extras [--purge]`.
- Snapshot broadcasts capped at 10 a second and 256 KB; an 8-hour soak test.
- Fuzz tests for every parser; `govulncheck` in CI.
- Hardening: secrets split by invisible characters are caught; the memory approve screen refuses when the popup can't show the whole proposal; nested repositories are their own projects; dot names can't escape the dots directory; an old feed file becomes owner-only; two panics are gone; a memory export of an unsaved entry imports again; every git call's repository-program guard is pinned by a test.
- Codex dot runs disable every MCP server declared in your Codex config and are refused while a server name contains `.` or `=`; commit reviews leave out the content of denied files; an invalid `tool_deny_globs` entry turns repository tools off instead of being dropped; `herdr-dots memory` in an unattached nested repository uses the nested project.
- `claude-cli` review, ask, memory, council and chair runs ignore the repository's `.claude/settings*.json` (`--setting-sources user`), so a repository can't run a credential helper or redirect a dot (live-verified 2026-10-11 with Claude Code 2.1.296: in a repository whose `.claude/settings.json` names an `apiKeyHelper`, plain `claude -p` ran it 11 times and a dot review ran it 0 times); the repository's `CLAUDE.md` doesn't reach them as instructions either (verified the same day: the dot read it only as a repository file, flagged it and didn't follow it).
- The memory screen exports (`E`, a private file in your home directory, with a warning when the folder you pick is in a git repository), imports (`I`, a preview first, `Y` for secret-looking entries) and purges a project's memory (`P`, type the project id; global memory is kept).
- Threat model, security review, user guide, `SECURITY.md`, `CONTRIBUTING.md`.

## 0.9.0

Dot profiles with 7- and 30-day stats, run history, dot packs (export and import with clamps and secret scanning), project dots with content-hashed trust, more templates.

## 0.8.0

Proactive dots: idle-time research with a "while you were away" digest, scheduled dots with catch-up, commit-triggered reviews, a project inbox, a daily dollar budget.

## 0.7.0

Write-capable dots: drafts in a sandboxed worktree from a side-effect-free snapshot, two approval gates, validated patches, a diff viewer.

## 0.6.0

Windows support (Herdr Windows beta): named pipes, `LockFileEx`, Job Objects, DACL privacy, `.cmd` shim handling; system prompts moved off argv; a three-OS CI matrix.

## 0.5.0

The Dots MCP server: the main agent can list and ask dots, convene a council, read the queue and memory, and propose memory.

## 0.4.0

Anthropic, OpenAI, OpenRouter and Ollama backends with a jailed read-only tool loop, `credentials.toml`, costs in dollars and dollar budgets.

## 0.3.0

Council mode: every attached dot answers at once, one merged digest card, an optional dot-to-dot second round.

## 0.2.0

Dot memory (VITAL): project, dot and global memory, learned from outcomes and memory passes, dismissed findings never raised again, proposals that wait for approval, full TUI and CLI control.

## 0.1.0

The core loop: dots attached to Claude Code and Codex panes review each turn and deliver through observe, suggest, queue or inject.

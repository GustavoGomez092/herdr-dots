# Changelog

All notable changes. Versions before 1.0.0-rc.1 were development
milestones, never released.

## Unreleased (after 1.0.0-rc.1)

Post-rc hardening: about 90 accepted known issues and security findings R3, R5 and R8 are fixed (see the triage notes).

### Security

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

## 1.0.0-rc.1 (release candidate, not released)

- Schema versions for every persisted file and a migration framework: older files are upgraded with a backup, files from a newer version stay read-only (`herdr-dots migrate`).
- Prebuilt, checksum-verified binaries once published (user gate) for macOS, Linux and Windows (amd64, arm64), with a source-build fallback; `herdr-dots verify-release`; `--version` shows the build source and commit.
- `herdr-dots uninstall-extras [--purge]`.
- Snapshot broadcasts capped at 10 a second and 256 KB; an 8-hour soak test.
- Fuzz tests for every parser; `govulncheck` in CI.
- Hardening: secrets split by invisible characters are caught; the memory approve screen refuses when the popup can't show the whole proposal; nested repositories are their own projects; dot names can't escape the dots directory; an old feed file becomes owner-only; two panics are gone; a memory export of an unsaved entry imports again; every git call's repository-program guard is pinned by a test.
- Codex dot runs disable every MCP server declared in your Codex config and are refused while a server name contains `.` or `=`; commit reviews leave out the content of denied files; an invalid `tool_deny_globs` entry turns repository tools off instead of being dropped; `herdr-dots memory` in an unattached nested repository uses the nested project.
- `claude-cli` review, ask, memory, council and chair runs ignore the repository's `.claude/settings*.json` (`--setting-sources user`), so a repository can't run a credential helper or redirect a dot; the repository's `CLAUDE.md` probably no longer reaches them either (live check pending).
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

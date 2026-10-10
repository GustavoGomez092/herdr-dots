# Changelog

All notable changes. Versions before 1.0.0-rc.1 were development
milestones, never released.

## 1.0.0-rc.1 (release candidate, not released)

- Schema versions for every persisted file and a migration framework: older files are upgraded with a backup, files from a newer version stay read-only (`herdr-dots migrate`).
- Prebuilt, checksum-verified binaries once published (user gate) for macOS, Linux and Windows (amd64, arm64), with a source-build fallback; `herdr-dots verify-release`; `--version` shows the build source and commit.
- `herdr-dots uninstall-extras [--purge]`.
- Snapshot broadcasts capped at 10 a second and 256 KB; an 8-hour soak test.
- Fuzz tests for every parser; `govulncheck` in CI.
- Hardening: secrets split by invisible characters are caught; the memory approve screen refuses when the popup can't show the whole proposal; nested repositories are their own projects; dot names can't escape the dots directory; an old feed file becomes owner-only; two panics are gone; a memory export of an unsaved entry imports again; every git call's repository-program guard is pinned by a test.
- Codex dot runs disable every MCP server declared in your Codex config and are refused while a server name contains `.` or `=`; commit reviews leave out the content of denied files; an invalid `tool_deny_globs` entry turns repository tools off instead of being dropped; `herdr-dots memory` in an unattached nested repository uses the nested project.
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

# Security policy

This policy covers Herdr Dots 1.0.0 and later.

## Reporting a vulnerability

Please report security problems privately, not in public issues.

- Use GitHub's private vulnerability reporting: open
  https://github.com/GustavoGomez092/herdr-dots/security/advisories/new (the repository's Security
  tab, "Report a vulnerability").

Include the version (`herdr-dots --version`), your OS, and steps to
reproduce. You can expect an acknowledgement within a week.

## Supported versions

Only the newest release gets fixes.

## The safety model in brief

- **Dots are read-only.** Claude Code dots run with read-only tools and hooks
  disabled, Codex dots in its read-only sandbox, API dots with a jailed
  read-only tool loop. Write-capable dots (off by default) work in a separate
  worktree and need two approvals; they never get a shell.
- **Nothing reaches your agent without you,** unless you choose inject mode,
  which only types when the agent is idle and you're not in the pane.
- **Model output is untrusted:** control and bidi characters are stripped
  before anything is shown, stored or sent; model-written project memory waits
  for your approval with the full text on screen.
- **Secrets:** keys live in `credentials.toml` (owner-only) or environment
  variables and never appear in logs, the feed, snapshots or errors; memory
  refuses text that looks like a secret.
- **Repositories are untrusted:** git runs with hooks, fsmonitor and lazy
  fetches disabled; Claude Code dots never load a repository's
  `.claude/settings*.json` (live-verified 2026-10-11); project dots
  need your trust, and a changed file
  loses it.
- **Installs are checked:** prebuilt binaries are checked against the
  checksums committed in the repository; otherwise the plugin builds from
  source. `herdr-dots verify-release` re-checks an install (and its cosign
  signature when cosign is installed). Checksums vouch only for the tree you
  cloned; the cosign signature ties the archive to the repository's release
  workflow.
- **No telemetry.**

What it does not protect against: the engine's local socket (or pipe) is
private to your user account but not authenticated, so any program running
as you, your coding agent included, can send it commands, approvals among
them. Treat the approval prompts as protection against the dots' models and
against acting on text you haven't seen, not against other programs of your
own account.

The full threat model is in [docs/security/threat-model.md](docs/security/threat-model.md),
and the latest review in [docs/security/2026-10-08-security-review.md](docs/security/2026-10-08-security-review.md).

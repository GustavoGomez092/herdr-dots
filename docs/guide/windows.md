# Windows (beta)

Herdr Dots is built for the Herdr Windows beta; this page lists how it behaves there and what has not been run.

Herdr Dots is built to run on the Herdr Windows beta with the same features as on macOS and
Linux, but it has not been run on Windows yet.

- **Not run-verified yet.** Every Windows code path is compiled and unit-tested on macOS, and
  `.github/workflows/ci.yml` runs the full test suite on `windows-latest`, but the developer has
  not run Herdr Dots on a Windows machine, and the CI workflow has not been pushed yet. The
  checklist in `docs/superpowers/notes/2026-10-08-phase-6-verification.md` ("User gates") lists
  what to try first: whether Herdr resolves `./bin/herdr-dots` to the `.exe`, the first
  `build.ps1` run, named-pipe owner checks (including an elevated/UAC prompt), process-tree kills,
  file locking, owner-only ACLs, the repo-tool jail on NTFS, and `.cmd` shims.
- **Engine IPC** is a named pipe, `\\.\pipe\herdr-dots-<session key>`, that only your Windows user
  can open. The engine checks that the pipe really belongs to you before trusting it.
- **Privacy:** `credentials.toml`, memory, settings and session state get an owner-only ACL (you,
  SYSTEM and Administrators). A credentials file that other users can read is refused with an
  `icacls` command that fixes it.
- **Dot processes** run in a Job Object, so a timeout or an engine crash ends the CLI and
  everything it started (node, git).
- **`claude.cmd` / `codex.cmd`** (npm installs) work: their command lines are quoted for cmd.exe
  and the system prompt goes through a private temp file. A native `claude.exe` is preferred when
  both are on `PATH`. A path containing `%`, `"` or `!` can't be passed to a `.cmd` program; such a
  run fails with a message saying so.
- **Long system prompts** go through a private temp file (`--append-system-prompt-file`) on every
  OS, not argv. This was verified live with Claude Code on macOS only.
- **Repo tools for API dots** are confined to the project folder, including NTFS tricks (8.3 short
  names, alternate data streams, junctions). Cloud placeholder files (for example OneDrive
  files that are not downloaded) may be refused as irregular files.
- **Keybindings:** `setup-keys` edits `%APPDATA%\herdr\config.toml` (or `HERDR_CONFIG_PATH`).
- **Glyphs:** if the dot symbols show as boxes, set `HERDR_DOTS_ASCII=1` for ASCII stand-ins
  (dot symbols, section rules, the selection marker and the `…` of cut text).
- Herdr's own Windows limits apply (no direct terminal attach, no live server handoff).
- Windows was compiled and unit-tested on macOS only; nothing in this guide has
  been run on a Windows machine. The PowerShell install (`scripts/build.ps1`)
  has not been tested here, and `windows/arm64` binaries are built but never
  tested.

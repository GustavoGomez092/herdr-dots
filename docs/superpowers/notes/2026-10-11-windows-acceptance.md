# Windows acceptance run (v1.0.0-rc.1)

**Date:** 2026-10-11.

**Environment:** GitHub `windows-latest` runner (Windows Server 2025, x86_64), Herdr 0.9.3 (`herdr-windows-x86_64.zip`) headless (`herdr --session acc server`), PowerShell 7 as the pane shell. Go 1.26.9 (setup-go) built only the test tools, not the plugin. The plugin was installed the real way:

```
herdr plugin install GustavoGomez092/herdr-dots --yes
```

That installed the verified release binary `herdr-dots 1.0.0-rc.1 (release, commit 6cb7e36118ce)`. `verify-release` printed `checksum ok` and `binary ok`.

**Last run:** https://github.com/GustavoGomez092/herdr-dots/actions/runs/38106303816 (`workflow_dispatch`, all 7 steps PASS, 1m45s).

**How the run was driven** (`.github/workflows/windows-acceptance.yml`, `scripts/acceptance/windows/`):
- **The agent:** a stand-in `claude.exe` (`fakeclaude`, agent mode), typed into the pane's PowerShell. ConPTY has no foreground process group. Herdr 0.9.3 on Windows instead scans the pane shell's child processes for an agent name, so the pane shows `agent: claude`. The stand-in draws what the driver asks for: a `✻ Cogitating… (1s · esc to interrupt)` line for working, then 14 plain lines for idle (the claude manifest looks at the bottom 12 non-empty lines). It also logs what is typed into it.
- **The dot's CLI:** `C:\acc\bin\claude.cmd`, an npm-style shim first on PATH. It runs `fakeclaude` in review mode, which logs argv (and the start of any settings or prompt file it is given) and prints a fixed verdict in the claude-cli JSON format. The format was checked with the product's own `backend.ParseClaudeOutput`.
- **The transcript:** `%USERPROFILE%\.claude\projects\C--work-app\<session>.jsonl`. That is Claude Code's encoding of `C:\work\app`, and the product finds a session by id in any project folder. The agent session was reported the way Herdr's Claude hook (`herdr-agent-state.ps1`, SessionStart) reports it: `pane report-agent-session --source herdr:claude --seq <ms> --agent-session-id <id> --agent-session-path <transcript> --session-start-source startup`. The attachment then read `source: claude`, `session_kind: id`.
- **Engine actions:** `acctool`, a client of the engine's named pipe (`\\.\pipe\herdr-dots-<session key>`). It does the same as the manage popup's attach, send and dismiss. It also subscribed to `pane.agent_status_changed` on Herdr's own pipe and saved the events to `sub.log`.

| # | Step | Result |
|---|---|---|
| 1 | Install from GitHub; `--version` says `release`; `verify-release` gives checksum ok and binary ok | PASS |
| 2 | The engine starts with the session (plugin startup `engine --ensure`, exit 0; `engine.log`: endpoint `\\.\pipe\herdr-dots-e3b1d4411f16`, which answers a snapshot) | PASS |
| 3 | Attach the Security dot to the Claude pane `w1:p1` over IPC; the catch-up review runs (turn 1 `flagged`) | PASS |
| 4 | `plugin action invoke toggle-pane` opens the Dots pane `w1:p2`; `pane read` shows the roster (`◆ Security queue flagged`) and the turn 1 card | PASS |
| 5 | A turn ends (working → idle in `sub.log`), the dot runs through `claude.cmd` (argv: `--strict-mcp-config`, `--settings <file>` holding the hookless settings and the Read denies, `--append-system-prompt-file <file>`, `--tools Read,Grep,Glob`, `--setting-sources user`), and card 2 appears in the pane and `queue.json` | PASS |
| 6 | Sending card 2 types `[◆ Security dot] …` into the agent pane (on screen and in claude.exe's stdin); the card is `sent` and remembered (`mem` entry `m1 finding`) | PASS |
| 7 | Turn 3, the sent finding again: `suppressed` (`similar to d:m1`). Turn 4 raises a new finding (card 3). After card 3 is dismissed (`m2 dismissed`), turn 5's same finding is `suppressed` (`similar to d:m2`) | PASS |

**Observations:**
- **The Windows shim path works.** The CLI resolved to `claude.cmd`, so the product moved the settings JSON and the system prompt into private temp files (`C:\Users\RUNNER~1\AppData\Local\Temp\herdr-dots-run-*\settings.json`, `system.txt`). cmd.exe passed every argument through unchanged.
- **The engine runs inside the runner's job.** `engine.log` says: `the caller's job forbids breakaway; the engine starts inside it and ends if that job is closed with kill-on-close`. That is the GitHub runner's job object, and it didn't affect the run.
- **The scrollback fallback.** In one run the session report wasn't applied (see below), so the engine used the scrollback fallback (`reading scrollback: transcript unavailable`). Steps 1–7 still passed. Turn 3 got no review there, because its screen delta held the delivered dot text and the scrollback reader marks such a turn `FromDots`, as designed.
- **Duplicate pending cards.** As on Linux, the catch-up card (turn 1) and card 2 hold the same finding and are both pending. Card 1 is still pending at the end. The fix is on main after rc.1.
- **Roster wording.** As on Linux, the roster shows `passed` after a turn whose finding was suppressed. The fix is on main after rc.1.

**Product bugs found:** none in the product code.

**Repository issue seen (not product code):** `ci` fails on `main` and on this branch in every OS job at `TestWindowsBuildScriptBuildsTheExe`: `build.ps1 lacks "Get-FileHash -Algorithm SHA256"`. Commit 0b5b966 changed `scripts/build.ps1` to hash with .NET, because `Get-FileHash` can't load when Windows PowerShell is started from PowerShell 7. The test in `cmd/herdr-dots/platform_manifest_test.go` still pins the old cmdlet. The pin needs to follow the fix (for example, pin the .NET SHA256 call).

**Herdr 0.9.3 Windows behaviour seen (not Dots issues):**
- **Agent detection is a process scan.** A `claude.exe` anywhere under the pane shell is detected (`identify_agent` strips `.exe`). The screen manifest then drives working and idle, the same as on Linux.
- **The session report.** `pane report-agent-session` prints nothing on success. A plain report (`--source acc --agent claude --agent-session-id <id>`, no seq), sent right after detection, was not applied (run 38105874693). The hook's form (`--source herdr:claude`, seq, path, `--session-start-source startup`) was applied immediately. The cause wasn't pinned down.
- **The headless server makes no workspace.** It starts with none; `workspace create --cwd` made `w1:p1`.
- **The API socket** is a marker file at `%APPDATA%\herdr\sessions\acc\herdr.sock`, served as the pipe `\\.\pipe\<that path>`, as `platform.PipeName` expects.
- **Plugin directories:** config at `%APPDATA%\herdr\plugins\config\herdr-dots`, state (engine session dir, memory) at `%LOCALAPPDATA%\herdr\plugins\herdr-dots`.
- **Open files.** The engine keeps its files open, so a reader must open them with `FileShare.ReadWrite`; `[IO.File]::ReadAllText` fails.

**Running it:** `gh workflow run windows-acceptance.yml --ref <branch>`. GitHub only dispatches a workflow that is on the default branch or has run before, so while iterating, the branch briefly had a `push` trigger (removed). The evidence (engine log, feed, queue, attachments, memory, transcript, argv log, pane reads, `sub.log`) is uploaded as the `windows-acceptance-evidence` artifact.

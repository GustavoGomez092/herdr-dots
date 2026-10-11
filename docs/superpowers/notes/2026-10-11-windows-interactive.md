# Windows interactive check: popups and keybindings (v1.0.0-rc.2)

**Date:** 2026-10-11.

**Environment:** GitHub `windows-latest` runner (Windows Server 2025, x86_64), the real interactive Herdr 0.9.3 client (`herdr --session acc`, which started its own server) in a 200×50 ConPTY. PowerShell 7 was the pane shell. The prefix is `ctrl+b`, Herdr's default (`herdr --default-config`: `# prefix = "ctrl+b"`).

**Plugin:** installed with `herdr plugin install GustavoGomez092/herdr-dots --yes`, which installed the release binary `herdr-dots 1.0.0-rc.2 (release, commit 8033b076ec88)`. The keybindings were added with `setup-keys --yes` before the client started (prefix+d, prefix+a, prefix+shift+s, prefix+shift+c in `%APPDATA%\herdr\config.toml`).

**Last run:** https://github.com/GustavoGomez092/herdr-dots/actions/runs/38124108448 (`workflow_dispatch`, commit 97e401c, 2m15s). Every screen is dumped in the job log, and the `windows-interactive-evidence` artifact holds the dumps (`screens/`), the engine files, the memory, the export, the argv log and the harness log.

**How the run was driven** (`.github/workflows/windows-interactive.yml`, `scripts/acceptance/windows/run-interactive.ps1`):
- **conptyh in place of tmux** (`scripts/acceptance/windows/conpty/`, its own Go module, so the product's `go.mod` gains nothing). It starts the Herdr client in a ConPTY (`github.com/charmbracelet/x/conpty`) and feeds the output into a VT emulator (`github.com/charmbracelet/x/vt`) that keeps the screen. It takes one command per line, on stdin or from `conptyh cmd` over localhost:
  - `keys`: one write per key, 120 ms apart; `ctrl+b` is 0x02;
  - `type`: one write per rune;
  - `burst`: one write for the whole text;
  - `screen`, `wait-for <secs> <regex>`, `wait-gone`, `resize <cols> <rows>`.
- **The agent and the dot's CLI** are the stand-ins of the headless run (`fakeclaude`):
  - `claude.exe` (agent mode), typed into the pane through the client. Herdr's process scan shows the pane as `agent: claude`.
  - The session was reported the way Herdr's Claude hook reports it.
  - `claude.cmd`, an npm-style shim, runs the dot's reviews.
- **The user PATH.** Herdr 0.9.3 on Windows starts every pane (the shell, the plugin popups, the Dots pane) with the user's environment from the registry, not the client's. The pane shell's PATH began `C:\Program Files\PowerShell\7;…` and lacked the directories set in the driver's process. So `claude.cmd`'s directory went on the user PATH (`HKCU\Environment`), where an npm install of Claude Code puts it. Before that change, manage said `claude CLI not found on PATH` and offered no MCP server. See runs 38121960201 and 38122763912.

| # | Step | Result |
|---|---|---|
| 1 | `prefix+d` opens manage. The first-run Setup pager offers the MCP server (the keys were already bound); `n` snoozes it; the library shows | PASS |
| 2 | `t`, Security template, `ctrl+s` saves (the new dot comes back `[x] ◆ Security`), `enter` closes manage and attaches it to the agent pane (`attachments.json`; sidebar `claude ◆1`) | PASS |
| 3 | `prefix+a` opens the Dots pane `w1:p2`; the roster `◆ Security queue flagged` and the turn 1 card show | PASS |
| 4 | A turn ends and a `WARN Security · turn 2` card appears; `tab` to the roster, then `m` cycles queue → inject → observe → suggest → queue | PASS |
| 5 | `prefix+shift+s` summons Security with "Is the login route safe against brute force?" (the question reached the dot's stdin); the answer arrives as an `INFO` card | PASS |
| 6 | `prefix+shift+c` convenes a council (one member, `estimate: 1 call (1 dot × 1 round) · 117 left`); the council card `⊕ INFO Council · turn 2 · 1 dot` arrives | PASS |
| 7 | After sending card 1 and dismissing card 2 (`mem 2`), `M` shows the finding and the dismissed entry. `E` defaults to `C:\Users\runneradmin\herdr-dots-memory-app-20261011.toml`. The export, written to `D:\a\_temp\herdr-dots-mem-export.toml`, has 2 entries, and its ACL is the owner, SYSTEM and Administrators only, with no inherited entries | PASS |
| 8 | At 60×15 with the export screen open, it says "Too small … (needs 10 lines, has 8)". `enter` writes nothing ("nothing exported: the popup is too small to show the whole path"; no file in the home dir). At 200×50 the memory screen comes back whole, and so does an export screen that was shrunk while open | PASS |
| 8b | Typing at the "Too small" export screen leaves the path alone | FAIL: W2 |
| B1 | A fast key burst: `jjj` in one write in the template list moves `▸` from API Design to Copywriter (3 rows) | PASS |

**Found:**
- **W1 (Windows): the Setup screen plans the MCP server with a `\\?\` path.**
  - **Seen:** in the popup, the first-run pager plans:
    ```
    Will run: claude mcp add --scope user … --transport stdio herdr-dots --
    '\\?\C:\Users\runneradmin\AppData\Roaming\herdr\plugins\github\herdr-dots-2b4238f3b918\bin\herdr-dots.exe' mcp --config-dir …
    ```
    The same plan from `herdr-dots setup` in a plain shell (end of the job log) says `'C:\Users\runneradmin\…\bin\herdr-dots.exe'`.
  - **Cause:** Herdr 0.9.3 starts plugin commands by a verbatim path. `newSetupIO` (`cmd/herdr-dots/setupmcp.go`) takes `os.Executable()` plus `EvalSymlinks`, and keeps the prefix that came with the process image.
  - **Effect (inferred; `y` was not pressed, because the stand-in can't run `claude mcp add`):**
    - `y` in the popup would register a `\\?\…` command in `~/.claude.json` (Codex: in `config.toml`).
    - `claudeEntryMatches` compares `Command` exactly, so the popup and the CLI would each see the other's entry as different. A server installed with `setup-mcp` would look not installed to the popup, which would offer it again. One installed from the popup would make `setup-mcp` plan a reinstall.
  - **Fix:** normalize the binary path. `internal/platform/finalpath.go` already strips `\\?\` (`normalizeFinalPath`).
- **W2 (all platforms, low): a "Too small" path screen still takes typed keys.**
  - **Seen:** with the export screen shrunk to 60×15, `z` and `q` changed nothing on screen (screen 40). Grown back, the path read `C:\Users\runneradmin\herdr-dots-memory-app-20261011.tomlzq` (screen 41).
  - **Cause:** `memui.keysPath` (`internal/ui/memui/model.go`) checks `shownWhole()` only on `enter`; every other key goes to `m.path.Update`. `keysConfirmPurge` has the same pattern for the purge confirmation.
  - **Why it's low:** `enter` stays refused while the screen doesn't fit, and the edited path is visible once the screen fits again.
  - **Fix:** while `!shownWhole()`, drop every key except `esc`/`ctrl+c` (and `enter`'s refusal).
- **B2 on Windows (Herdr):** the Dots pane's first frame after `prefix+a` was drawn at the pre-split width: the queue rule wrapped and the card text lost its indent (screen 14). The next frame was right, with two stale footer lines left under it (screen 15), and by the next turn the pane was clean (screen 16). On Linux the wrong width lasted until the pane was focused; here it corrected itself within about a second, without focus.
- **Fixed since rc.1, seen right here:**
  - fast key bursts (B1);
  - wrapped card lines keep their indent (B3);
  - "1 dot";
  - the council card reads `Consider: - …` rather than raw `##`;
  - the memory key line is gone under the export prompt.
- **Herdr 0.9.3 Windows behaviour seen (not Dots issues):**
  - Panes get the registry environment (see above). A user whose `claude` is on a PATH set only in a shell profile gets no MCP offer in manage, and can't save a claude-cli dot there ("claude CLI not found on PATH").
  - The client shows the same welcome card as on Linux (`↵ continue`).
  - ConPTY asks the host for win32-input-mode (`ESC[?9001h`). Plain VT input (C0 bytes for `ctrl+…`, `ESC`, `ESC[Z`) is translated fine; `S` and `C` arrive as shift+s and shift+c.

**Running it:** `gh workflow run windows-interactive.yml -R GustavoGomez092/herdr-dots --ref win-interactive`. While iterating, the workflow briefly had a `push` trigger limited to `win-interactive` (removed before the last run). It took 8 runs.

**Follow-up:** W1 (the `\\?\` prefix in the MCP setup path) and W2 (typing at a too-small prompt) are fixed in fc98e50 (`TestStripLongPathPrefix`, `TestClaudeEntryWithALongPathPrefixStillMatches`, `TestTooSmallPromptsIgnoreTyping`) and ship in 1.0.0-rc.3.

# Live verification findings (V1-V9), 2026-10-08

Environment: herdr 0.8.2 (protocol 20), isolated `--session dots-test`, socket
`~/.config/herdr/sessions/dots-test/herdr.sock`. Claude Code 2.1.295 in the pane (default model Opus 5.5, auto mode); dot-style run used `haiku`.
Scratch repo `/tmp/dots-test-repo` (resolves to `/private/tmp/dots-test-repo`).

## Confirmed by schema (not re-tested)
- `pane.get` -> `{"type":"pane_info","pane":{...}}`; `pane.agent_session` = `{"source","agent","kind":"id"|"path","value"}` or null.
- `plugin.pane.open` params: plugin_id, entrypoint, placement, direction, target_pane_id, width, height, env, focus, cwd.
- Subscription kinds: only `pane.agent_status_changed`, `pane.output_matched`, `pane.scroll_changed` (no pane-closed event; engine polls).
- `claude -p --output-format json` result shape; `--bare` unusable (API-key auth); `--settings '{"disableAllHooks":true}'` works with subscription auth.
- Codex not installed.

## Setup caveats discovered (affect the plan)
1. **Env inheritance.** The herdr server (and so every pane it spawns) inherits the environment of whoever started it. Started from inside a Claude Code session, Claude in the pane showed "Transcript saving is off - inherited CLAUDE_CODE_CHILD_SESSION marker" and wrote no transcript. Fixed by restarting the server with all `CLAUDE*` env vars unset. Relevant to the "strip child env" constraint: also strip `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_CODE_SESSION_ATTENDED`, `CLAUDE_CODE_MESSAGING_*`, `CLAUDE_CODE_EXECPATH`, `CLAUDE_PID`, `CLAUDE_EFFORT` (not only `CLAUDECODE`/`CLAUDE_CODE_ENTRYPOINT`) for any dot subprocess, or `--no-session-persistence` aside, behaviour may differ.
2. First `agent start --kind claude` in a new dir blocks on the folder-trust prompt (`agent_not_ready`). Needed `pane send-keys <pane> Down Enter`.
3. Claude project dir uses the resolved path: `~/.claude/projects/-private-tmp-dots-test-repo/` (not `-tmp-...`) on macOS. Transcript path derivation must use the realpath of `cwd` (herdr already reports `/private/tmp/...`).

## V1 - session identity
Cmd: `herdr agent prompt w2:p1 "Read auth.ts ..." --wait --until idle`, then raw `pane.get`.
Evidence:
```
"agent":"claude","agent_status":"done","cwd":"/private/tmp/dots-test-repo","foreground_cwd":"/private/tmp/dots-test-repo",
"agent_session":{"source":"herdr:claude","agent":"claude","kind":"id","value":"81ef4218-b6bb-4f49-aba5-9156edd7320e"}
```
`ls ~/.claude/projects/*/81ef4218-....jsonl` -> exists (`-private-tmp-dots-test-repo/`). `agent_session` is populated as soon as the agent starts (already present in `agent start` response).
**Finding:** kind=`id`, value = transcript file stem. Resolve by globbing `~/.claude/projects/*/<value>.jsonl`.
**Gotcha:** `--until idle` TIMED OUT. After a turn in an unfocused/unattended pane the status is `done`, not `idle` (idle only for the freshly-started pane). Use `--wait` with no `--until` (default idle|done|blocked) or `--until done`.
Assumption holds: **yes** (with the idle/done caveat).

## V2 - status transitions in a tool-using turn
Subscribed via `events.subscribe`. Envelope of one event:
```
{"data":{"agent":"claude","agent_status":"working","pane_id":"w2:p1","workspace_id":"w2"},"event":"pane.agent_status_changed"}
```
Subscribe ack: `{"id":"s","result":{"type":"subscription_started"}}`.
Turn 2 (Grep + Read + explain, tools used): `19:16:22.483 working` -> `19:16:40.721 done`; turn 3 (tool use + Write): `19:16:49.533 working` -> `19:17:12.563 done`. **No intra-turn idle/done gap at all** (one continuous `working` period per turn, 18-23 s). Longest intra-turn idle gap: 0 ms. Only two samples; 1500 ms debounce is more than sufficient on this evidence.
Extra finding: `pane.agent_status_changed` also fires when only metadata changes (same status, extra `display_agent` field in `data`, e.g. at 19:18:02 with `"agent_status":"done","display_agent":"claude ◆2"`). The engine must dedupe on status change, not on every event. Final status for an unattended pane is `done`, so treat `done` and `idle` both as "turn finished".
Assumption holds: **yes** (recommended `turn_debounce_ms` stays 1500; treat `done` as terminal state).

## V6 - prompt delivery into the Claude TUI
Cmd: `herdr agent prompt w2:p1 $'[◆ Security dot] JWT in localStorage is XSS-stealable.\n\nUse an httpOnly cookie.' --wait`.
Transcript user record: `"content":"[◆ Security dot] JWT in localStorage is XSS-stealable.\n\nUse an httpOnly cookie."` - one user record, prefix at the very start, blank line preserved, `origin.kind":"human"`, `promptSource":"typed"`.
`agent prompt` accepted pane id `w2:p1` as `<TARGET>` (also accepts agent name).
Assumption holds: **yes**.

## V3 - split beside a specific pane
Response of `plugin.pane.open` (target_pane_id w2:p1, direction right, focus false):
```
{"id":"1","result":{"type":"plugin_pane_opened","plugin_pane":{"plugin_id":"dots-probe","entrypoint":"side","pane":{"pane_id":"w2:p2","terminal_id":"term_...","workspace_id":"w2","tab_id":"w2:t1","focused":false,"cwd":"/private/tmp/dots-probe","label":"Probe",...}}}}
```
- New pane id at `result.plugin_pane.pane.pane_id`: yes. Opened to the right of w2:p1, unfocused (focused_pane_id stayed w2:p1).
- `PROBE_TARGET=w2:p1` present in pane env. Also present: `HERDR_PLUGIN_ID`, `HERDR_PLUGIN_ENTRYPOINT_ID`, `HERDR_SESSION=dots-test`, `HERDR_SOCKET_PATH`, `HERDR_ENV=1`, `HERDR_TAB_ID`, `HERDR_PLUGIN_ROOT`, `HERDR_PLUGIN_STATE_DIR`, `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_CONTEXT_JSON` (workspace_id, workspace_cwd, tab_id, focused_pane_id, ...). `HERDR_PANE_ID` was not observed in the probe pane's env; not needed: the plan passes the target pane via the `plugin.pane.open` env (`PROBE_TARGET` confirmed).
- cwd defaults to plugin root, not the target pane's cwd (pass `cwd` if needed).
- Default split ratio: **0.5** (divider; 47/47 columns of a 94-column area).
- **Resize direction differs from the brief.** `pane.resize {"pane_id":"w2:p2","direction":"left","amount":0.15}` moved the divider LEFT, making the new right pane WIDER (ratio 0.5 -> 0.35, pane widths 33/61). `direction":"right"` moves the divider right and NARROWS the new right pane (sequence: left 0.15 -> ratio 0.35; right 0.15 -> 0.50; right 0.15 -> 0.65 with widths 61/33). `amount` is a ratio delta. Response includes the full layout (`splits[].ratio`, `panes[].rect`).
- `plugin.pane.close {"pane_id":"w2:p2"}` -> `{"type":"plugin_pane_closed","pane_id":"w2:p2"}`; pane gone from `pane list`. `plugin unlink` ok.
- `plugin link` warns if manifest lacks `platforms`.
Assumption holds: **yes** for open, env and close. Resize direction is inverted relative to the brief (`right` narrows a right-hand split).
Plan impact: none, because v1 never resizes. A future width fix must use `direction:"right"` to narrow a right-hand split.

## V7 - dot CLI runs don't touch Herdr state
Ran the stripped-env `claude -p ... --no-session-persistence ...` inside a split shell pane (w2:p3) that had 7 `HERDR_*` vars. Result: `{"type":"result","is_error":false,"result":"...","total_cost_usd":0.0008}` (haiku-5-5; model replied conversationally, not with the requested JSON - prompt design must be firmer).
- No new file in `~/.claude/projects/-private-tmp-dots-test-repo/` (session persistence off works).
- Main pane w2:p1 stayed `done`, no events for it.
- BUT `agent list` showed a new entry `("w2:p3","claude","unknown")` while the dot ran: herdr detects `claude` as the foreground process of the pane it runs in (by process; the dot command ran with `HERDR_PANE_ID`, `HERDR_SOCKET_PATH` and `HERDR_ENV` unset via `env -u`, so detection did not depend on them). It had no agent_session, status `unknown`, and vanished when the process exited. It does not affect other panes. Consequence: a dot run from a visible plugin pane's shell appears as a transient claude agent in that pane; run it from a non-pane daemon/process or accept the transient entry.
Assumption holds: **yes** for the main pane and transcripts. A transient agent entry appears for the pane hosting a dot run.
Plan impact: the controller ruled no code change, because the engine runs setsid-detached outside any pane; this is re-checked in the Task 22 E2E.

## V8 - key bindings
`grep -n 'prefix+d\|prefix+a\|prefix+shift+s' ~/.config/herdr/config.toml` matches only line 13 `key = "prefix+down"` (an existing user plugin binding; a prefix-substring false positive). Exact-match `grep -E '"prefix\+(d|a|shift\+s)"'` on config.toml and on `herdr --default-config`: no matches. Defaults mention `prefix+s` (settings) and `prefix+shift+d` (close_workspace) as built-ins, which the plan avoids. The user has `prefix+down` bound by an existing user plugin binding.
Assumption holds: **yes**.

## V9 - sidebar metadata
`pane.report_metadata {pane_id, source:"herdr-dots", display_agent:"claude ◆2"}` -> `{"type":"ok"}`; `pane.get` then shows `"display_agent":"claude ◆2"` alongside the unchanged `agent:"claude"` and `agent_status`. `clear_display_agent:true` -> ok, `display_agent` field disappears (restored). Claude status detection kept working afterwards (next prompt: working -> done events). Sidebar rendering itself was not viewed (no attached TUI); verified via `pane.get`/`agent list` (`display_agent` also in agent list).
Assumption holds: **yes** (data level; visual not checked).

## V5 - Claude transcript fixture
`internal/transcript/testdata/claude_real.jsonl` (47 lines, ~40 KB), from session 81ef4218-... Redaction: tool_result content -> `<redacted>`, `toolUseResult` removed, **and all `attachment` records dropped** - the plan's jq alone is not enough: attachment records embed the user's global CLAUDE.md, email address, memory path and installed skill listings. Verified with grep: no `gustavo`, `/Users/`, `gmail`. Remaining paths are `/private/tmp/dots-test-repo` only.
Record types present: user (8), assistant (11), system (4, subtype `turn_duration`), ai-title (3), last-prompt (4), mode (4), permission-mode (4), atis-latch (4), file-history-snapshot (4), file-history-delta (1). Tool names used in assistant tool_use blocks: Read, Bash (grep), Write. `user.message.content` is a string for typed prompts and an array of `tool_result` blocks for tool replies; assistant content blocks: thinking, tool_use, text. Typed-prompt user records have `origin.kind:"human"`. Reader must tolerate non-message record types (no `message` key).
Note: the redacted fixture leaves `thinking.signature` blobs (harmless).

## V4/V5/V6 for Codex
`which codex` -> not found. **BLOCKED - codex not installed; codex reader/backend are built from the documented formats in Tasks 7 and 9 and must be live-verified before any codex dot is relied on.** Decision for the user: install now (`npm i -g @openai/codex`) or defer. Nothing was installed.

## Cleanup
`herdr --session dots-test server stop` run; status "not running". User's real session untouched (no `~/.config/herdr/herdr.sock` exists; config.toml only read).

## Plan corrections and controller rulings
- Post-turn status is `done`, not `idle`. Use `agent prompt --wait` without `--until idle`, or `--until done`. The v1 engine and delivery already accept idle or done.
- Resize direction: `direction:"right"` narrows a right-hand split; `"left"` widens it. v1 never resizes.
- V7 ruling: no code change; the engine runs setsid-detached outside any pane, so the transient agent entry should not occur. Re-checked in the Task 22 E2E.
- CleanEnv also drops `CLAUDE_CODE_CHILD_SESSION`.
- The first `agent start` in a new directory stops at the Claude folder-trust prompt (`agent_not_ready`).
- Status events also fire on metadata-only changes (same status, extra `display_agent`); harmless.
- V2 evidence is thin (2 turns). Re-check the debounce during Task 22.

## End-to-end smoke test (Task 22b)

Automated replacement for the human checklist, run against herdr 0.8.2 in the isolated `--session dots-test` (headless server started with all `CLAUDE*` env unset), scratch repo `/tmp/dots-test-repo`, Claude panes on haiku, Security dot with `model = "haiku"`. Driven with `herdr --session dots-test ...` plus a throwaway Go client (ipc attach/send/set_mode/detach/watch; deleted afterwards, not committed).

**Plugin approach: manual engine, NOT `plugin link`.** Herdr plugins are global to the user: `herdr --session dots-test plugin list` shows the same user-level plugin registry (`cloudmanic.herdr-plus`, stored in `~/.config/herdr/plugins.json`) as the real session. Linking would have run the startup hook and added actions in the user's real sessions, so nothing was linked. Instead the binary was run with `HERDR_SOCKET_PATH=<dots-test socket>`, `HERDR_PLUGIN_ID=herdr-dots`, `HERDR_PLUGIN_CONFIG_DIR`/`HERDR_PLUGIN_STATE_DIR` under `/tmp/dots-e2e` (`herdr-dots engine --ensure`). Dot TOML = `security.toml` with model haiku, written to `<config>/dots/`.

| # | Item | Result |
|---|---|---|
| 1 | Engine starts, `engine.log` says "engine started" | PASS: `engine started: session dir /tmp/dots-e2e/state/865d905ee558, socket .../dots.sock` |
| 2 | Attach works, snapshot shows the dot | PASS: snapshot pane `w4:p1`, dot `security` `◆ Security` mode `queue`, library lists it with model haiku |
| 3 | Turn "store the login token in localStorage in auth.ts" -> Security card, `display_agent` "claude ◆N" | PASS: card severity `warn` ("any XSS ... can read localStorage ..."), `pane.get` -> `"display_agent":"claude ◆2"` |
| 4a | Send card -> `[◆ Security dot] ...` arrives in Claude and Claude responds | PASS: pane read shows `❯ [◆ Security dot] Before committing: ...`; transcript user record starts with the prefix; Claude edited auth.ts |
| 4b | Security dot does not review its own dot-started turn | PASS with transcript source (calls stayed 4 for 35 s after a delivered card and the dot-started turn finished). FAIL when attached before the first prompt: see bug 1 (calls rose 2 -> 3, new card created 9 s after the dot turn ended) |
| 5 | `set_mode inject`; focused pane holds, unfocused delivers | PASS: with `herdr workspace focus w4` the new finding was card status `auto` (held, no `[◆` user record); after `workspace focus w1` the next poll delivered it (`[◆ Security dot] The token-logging line ...` user record at 03:05:03Z) |
| 6 | Close the Claude pane -> cards orphaned | FAIL: 60+ s after `pane close`, engine still lists pane `w4:p1`, cards stay `pending`. See bug 2 |
| 7 | Dots pane via `plugin action invoke herdr-dots.toggle-pane` | NOT-AUTOMATABLE without linking (global). `HERDR_PANE_ID=w4:p1 herdr-dots action toggle-pane` fails `herdr plugin_not_found: plugin not found` as expected for an unregistered plugin; ensureEngine and the action path up to the pane open were exercised. Toggle UI left for the human run |
| 8 | V7: no phantom agent during dot runs | PASS: `agent list` polled every 2 s for 4 min across 3 dot runs: always exactly the 3 main panes, never an extra pane/agent (engine runs setsid-detached) |
| 9 | V2: debounce | PASS: 3 multi-tool turns (Read/Write/Edit/Bash, 5-13 s) each produced exactly one dot call; observed `working` -> `done` with no intermediate flaps at 2 s polling. Still only a 2 s-granularity check; the earlier event-level V2 data (0 ms gaps) stands |

### Bugs found

1. **Attach on a fresh Claude session locks the pane to the scrollback reader (breaks the loop guard and causes a spurious review).** Claude creates its `.jsonl` lazily at the first prompt. If the dot is attached before that, `transcript.Select` (`internal/transcript/select.go:17-21`) cannot resolve the path and returns the scrollback reader; `Attach` persists `Source: scrollback` (`internal/engine/engine.go:~186-188`) and the reader is cached forever in `e.readers` (`engine.go:247-256`), never upgraded once the file exists. Consequences observed: (a) attach alone triggered a dot call on an empty conversation (card for turn 1, created before any prompt was sent); (b) the delivered `[◆ Security dot]` turn was reviewed by Security (scrollback turns carry no user prefix; `attachments.json` showed `"source":"scrollback"`, `last_seen_turn":3` with only 2 real turns, and card turn labels were off by one). Detach + re-attach after the transcript existed gave `"source":"claude"` and the guard worked. Suggested fix: re-resolve the reader when the source is scrollback and the session transcript now exists (e.g. in `checkSession`/`onIdle`), and do not run on attach when there are no turns.
2. **Closed panes are never detected, so cards are never orphaned.** Herdr returns error code `pane_not_found` (`{"error":{"code":"pane_not_found","message":"pane w4:p1 not found"}}`), but `herdr.IsNotFound` (`internal/herdr/types.go:74-77`) only matches `"not_found"`. `poll()` (`internal/engine/deliver.go:122-130`) therefore treats it as a transient error and `continue`s forever. Same check in `Start` (`engine.go:~160`). Suggested fix: match `pane_not_found` (and probably any `*_not_found`).

### Side effects and cleanup
- Sending `/model haiku` into the pane made Claude write `"model": "haiku"` into the user's `~/.claude/settings.json`; it was restored to the previous `"opus"` (from `settings.json.bak` / prior notes) at the end. Future runs should pass `--model` via agent args instead of using `/model`.
- Engine killed, test server stopped (`herdr --session dots-test server stop`), no plugin linked or unlinked, user config.toml untouched, `e2e-scratch/` deleted.
- Setup note: `env -i` for the server breaks Claude auth ("Not logged in"); unset only the `CLAUDE*` variables.
- Server restore note: a restarted `dots-test` server restores earlier workspaces (w1-w3) from `session.json`.

# Phase 5 verification findings (V1–V6), 2026-10-08

Environment (execution machine, probes run 2026-10-09): go1.25.7 darwin/arm64; Claude Code 2.1.295;
codex not installed; macOS 26.5.2 (`sw_vers -productVersion`); go-sdk v1.8.0.

Every section records the command, the evidence and "Assumption holds: yes/no/not live-verified".
Probes run in `mktemp -d` directories with `--mcp-config <temp> --strict-mcp-config` or a temporary
`CODEX_HOME`; the real `~/.claude.json`, `~/.claude/` and `~/.codex/config.toml` are never modified.

## Confirmed while planning (2026-10-08, Claude Code 2.1.295, go-sdk v1.8.0, Go 1.25.7)

- V1: a probe stdio server started by `claude -p --mcp-config <temp> --strict-mcp-config` received Claude
  Code's whole environment (61 variables including HERDR_PANE_ID and HERDR_SOCKET_PATH); `${HERDR_PANE_ID:-}`
  in the server's `env` expanded to the value, `${UNSET:-}` to "". Assumption holds: yes.
- V4: Claude Code docs: MCP_TOOL_TIMEOUT defaults to about 28 hours; MCP_TIMEOUT is the startup timeout.
  Assumption holds: yes (docs).
- V5: Claude Code sent `server/discover` with protocolVersion "2026-07-28" and called the tool. Assumption holds: yes.
- V6: with text "TEXT-WORD: walrus" and structured content "STRUCTURED-WORD: pelican", Claude repeated only
  "pelican": the model sees the structured result. (Informational; Plan decision 15.)
- V2/V3: Codex docs only (Codex not installed): servers get `env` plus `env_vars`; `-c key=value` overrides
  config keys. Not live-verified.

## Execution-machine results (2026-10-09)

Probe: the brief's throwaway stdio server (go-sdk v1.8.0) built in a `mktemp -d` directory, driven by
`claude -p "<call mcp__probe__echo once, repeat every code word>" --mcp-config <temp>/mcp.json
--strict-mcp-config --allowedTools mcp__probe__echo --model haiku --no-session-persistence
--settings '{"disableAllHooks":true}'` with `HERDR_PANE_ID=w9:p9 HERDR_SOCKET_PATH=/tmp/fake.sock` exported and
the `CLAUDE*` child-session variables unset.

Setup note: in the empty scratch module, `go get go-sdk@v1.8.0` alone left go.sum incomplete and the build
failed with "missing go.sum entry"; a `go mod tidy` in the temp module fixed it. Not a repo concern.

- V1 (environment reaches the stdio server): `plain.log` lists `ENV HERDR_PANE_ID=w9:p9`,
  `ENV HERDR_SOCKET_PATH=/tmp/fake.sock`, `ENVCOUNT 62`. `expand.log` lists `ENV PROBE_PANE=w9:p9` and
  `ENV PROBE_MISSING=` (empty), so `${VAR:-}` expansion works for a set and an unset variable.
  Assumption holds: yes.
- V4 (tool timeout): documentation only, not probed (MCP_TOOL_TIMEOUT default about 28 hours;
  MCP_TIMEOUT is startup). Assumption holds: yes (docs).
- V5 (protocol): the first line the server received was
  `{"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",
  "io.modelcontextprotocol/clientInfo":{"name":"claude-code",...,"version":"2.1.295"}...}}}`.
  Methods seen: `server/discover` (2), `subscriptions/listen` (1), `tools/list` (1), `tools/call` (1); no
  `initialize` line. The tool call succeeded against go-sdk v1.8.0. Assumption holds: yes.
- V6 (what the model sees): the model's whole reply was `STRUCTURED-WORD: pelican`; `walrus` (the text
  content) did not appear. Claude Code gives the model the structured result. Informational; Plan decision 15
  holds.
- V2 (Codex `env_vars` forwarding): not live-verified (codex not installed).
- V3 (Codex `-c mcp_servers.<name>.enabled=false` stops the server): not live-verified (codex not installed).
  The design does not depend on it (Plan decision 17).

## Smoke test: `herdr-dots mcp` over real stdio (Task 10, 2026-10-09)

Built binary, temp config/state dirs, `HERDR_PANE_ID`/`HERDR_SOCKET_PATH` unset. Stdout, exactly two lines:

```
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"logging":{},"tools":{"listChanged":true}},"protocolVersion":"2025-11-25","serverInfo":{"name":"herdr-dots","version":"0.4.0"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"herdr-dots: not running inside a Herdr pane; dots are unavailable here."}],"isError":true}}
```

`mcp.log`: `started outside a Herdr pane (...) every tool refuses`, `list_dots refused: not inside a Herdr pane`, `stopped: <nil>`.

## End-to-end (Task 11, 2026-10-09)

Version `herdr-dots 0.5.0 (scaffold)`. Run by the implementer agent (the user was away) against the built binary, herdr 0.8.2 in
the isolated `--session dots-test` (server started with every `CLAUDE*` variable unset), scratch repo `/tmp/dots-test-repo`,
Claude Code 2.1.295 on haiku. No `herdr plugin link`, no Claude slash commands. The engine was started by hand with
`HERDR_PLUGIN_CONFIG_DIR`/`HERDR_PLUGIN_STATE_DIR` under a temp dir (as in the Phase 2 and 3 smoke tests); a throwaway IPC client
attached the dots and read snapshots (deleted, not committed). Dots: `security` (queue) and `copywriter` (observe), both haiku.
Claude was started in the pane with `claude --model haiku --mcp-config <temp mcp.json> --allowedTools 'mcp__herdr-dots__*'`
(`--allowedTools` only so tool permission prompts do not block the unattended run). The temp `mcp.json` pointed `--config-dir`
and `--state-dir` at the temp plugin dirs. Real configs were snapshotted first and compared afterwards.

| # | Check | Result |
|---|---|---|
| 1 | Installer against throwaway `CLAUDE_CONFIG_DIR`/`CODEX_HOME` | PASS. Preview showed the `claude mcp add --scope user --env 'HERDR_PANE_ID=${HERDR_PANE_ID:-}' --env 'HERDR_SOCKET_PATH=${HERDR_SOCKET_PATH:-}' --transport stdio herdr-dots -- …` command, the expected `mcpServers` entry and the Codex block as a `+` diff; after `y`, `claude mcp get herdr-dots` showed `Status: Connected` with both env values; the Codex file held exactly the managed block; `--remove --yes` left the Codex file at 0 bytes, removed the Claude entry (`claude mcp get` exit 1), and each touched file got a `0600` `.bak-herdr-dots-<time>` backup. "codex is not on PATH: not verified" was printed. |
| 2a | `list_dots` and `ask_dot` from Claude Code | PASS. Prompt "list the dots, then ask the security dot whether storing the JWT in localStorage is OK": Claude made 2 herdr-dots calls and reported Security's answer (use HttpOnly/Secure cookies). No card was created (`cards: []`). Feed: `security 2 asked warn · cost=$0.0010 tokens=7138/359`; roster status `consulted`. |
| 2b | Consulted dot skips that turn, reviews the next | PASS. No `pass`/`flagged` line for turn 2; turn 3 ("ok") logged `security 3 pass`. |
| 2c | Per-turn ask limit | PASS. Five `ask_dot` calls in one turn: the first three answered (feed shows three `asked` lines for turn 4, security calls 3 to 6), the 4th and 5th returned `ask limit reached; continue without the dot (3 per turn)` and ran nothing. The next turn asked again normally. |
| 2d | `ask_council` and `propose_memory` | PASS. The council digest came back to the agent (Security "Should fix", Copywriter "Consider") with no queue card; feed `council 5 asked warn · 2 of 2 members answered` (the two member runs also logged their usual `flagged`/`learned` lines, with no card, as in Phase 3). `propose_memory` returned `p:m2`; `herdr-dots memory list --all` against the temp state dir showed `p:m2  convention  proposed  Auth errors must be generic…` and the JSON shows `"source": "agent"`, no dot, `"status": "proposed"`. The two other proposals (`p:m1`, `p:m3`) are the dots' own learning. The next turn was reviewed by Security (`pass`) as usual. |
| 3 | No recursion (`mcp.log`) | PASS. One `serving pane w5:p1 through …/dots.sock` line for the agent start; the only other lines were the two `ask_dot: ask limit reached` refusals. About 10 claude dot runs (asks, reviews, council members) added no `serving pane` line and no refusal line. The codex half (a codex dot run starting no server or logging `refused: not inside a Herdr pane`) was not run: codex is not installed. |
| 4 | Real configs unchanged | PASS. `~/.claude/settings.json` and `~/.codex/config.toml` byte-identical to the snapshot. `~/.claude.json` differs (Claude Code's own usage data) but its `mcpServers` key is unchanged (compared by hash, contents not printed). |

Deviation from the brief: outside a linked plugin, `setup-mcp` refuses with "the plugin state dir is unknown", so check 1 set
`HERDR_PLUGIN_ID`, `HERDR_PLUGIN_CONFIG_DIR` and `HERDR_PLUGIN_STATE_DIR` to temp dirs (the brief assumes a shell inside a linked
plugin's session). Cleanup: engine and `dots-test` server stopped, only processes started here were killed, temp dirs left in the
system temp folder.

### Not automated or not verified (user gates)

- Codex MCP is **not live-verified** (codex is not installed here): the managed block, `env_vars` forwarding, `tool_timeout_sec`,
  and `codex mcp list` follow Codex's documented format only.
- The codex dot-run override `-c mcp_servers.herdr-dots.enabled=false` is **not live-verified**. The server also refuses without
  `HERDR_PANE_ID` as the second layer.
- NOT-AUTOMATABLE (needs a human eye or keypress): the `◆ asked by agent` line rendering in the Dots pane, the roster `consulted`
  label, the manage popup's MCP footer and the memory screen's `P` proposals view. This run verified the underlying snapshot JSON
  (outcome `asked`, status `consulted`), feed data and memory CLI output instead.
- Installing into the real Claude Code and Codex configs was not done (left to the developer: `./bin/herdr-dots setup-mcp`).

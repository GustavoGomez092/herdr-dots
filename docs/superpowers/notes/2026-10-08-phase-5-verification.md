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

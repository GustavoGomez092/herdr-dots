# MCP server

The Dots MCP server lets the main agent ask the dots, convene a council and read the queue and memory itself.

Claude Code or Codex can consult your dots themselves while they work ("ask the security dot before you
write the token handling").

The first time you open the manage popup, a Setup screen offers to install the server for each agent
CLI it finds (Claude Code: `claude` on your PATH; Codex: `codex` on your PATH or an existing
`~/.codex/config.toml`). It shows the exact change before anything happens: the
`claude mcp add --scope user …` command line and the file Claude Code writes, and the marked block for
Codex's config. Scroll through it (`j` or `space`); `y` works once every line has been on screen. `y`
installs it (the same steps as `setup-mcp --yes`, backups first), `n` or `esc` asks again in 7 days,
`x` stops asking, and `ctrl+c` closes the popup without answering. Nothing is written without `y`. The
Dots pane never shows the screen; while setup is pending it shows one line pointing to the manage
popup. Press `S` in the manage popup to open the screen again at any
time; it shows what is installed and what it would change. `herdr-dots setup` does the same from a
shell (`--yes`, `--no-mcp`, `--no-keys`). Your answers are kept in `onboarding.json` in the plugin's
config directory. Or install the server yourself:

```bash
./bin/herdr-dots setup-mcp                 # Claude Code and/or Codex, whichever is installed
./bin/herdr-dots setup-mcp --client claude # or --client codex
./bin/herdr-dots setup-mcp --remove        # take it out again
```

`setup-mcp` prints the exact commands and the config change, asks before applying (`--yes` skips the
question) and backs up every file it touches (`<file>.bak-herdr-dots-<time>`). For Claude Code it runs
`claude mcp add --scope user …` (Claude Code edits its own `~/.claude.json`). For Codex it adds one
marked block to `~/.codex/config.toml` and leaves every other line alone:

```toml
# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)
[mcp_servers.herdr-dots]
command = "/path/to/herdr-dots/bin/herdr-dots"
args = ["mcp", "--config-dir", "…", "--state-dir", "…"]
env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]
startup_timeout_sec = 20
tool_timeout_sec = 180
# <<< herdr-dots MCP server
```

Restart the agent in its Herdr pane afterwards. If you move or rebuild the plugin somewhere else, run
`setup-mcp` again. The manage popup's footer shows where it is installed.

The agent sees these tools (as `mcp__herdr-dots__<tool>`):

| Tool | What it does |
|---|---|
| `list_dots` | The dots attached to the agent's pane: purpose, mode, status, budget |
| `ask_dot` | Ask one attached dot a question and wait for its answer (up to the run timeout) |
| `ask_council` | Ask every attached dot at once and get the merged council digest |
| `list_queue` | Read the dots' cards waiting for you in this pane |
| `read_memory` | Read the project's approved dot memory; a dot's global (all-project) memory only for the dots attached to the pane |
| `propose_memory` | Propose a convention, decision or note; it waits for your approval in the memory screen |

> **Warning: MCP answers bypass the approval queue.** Whatever `ask_dot`, `ask_council` and `list_queue`
> return goes straight to the main agent, without you approving it first (the queue still gets nothing
> from these calls). The calls show up in the agent's transcript and as `◆ asked by agent` lines in the
> Dots pane feed. If you don't want that, remove the tools you don't want from `[mcp] tools`.

What keeps it in check:

- Only dots **attached** to the agent's own pane can be asked; a server only ever serves the pane it was
  started in. Outside Herdr (or inside a dot's own run) every tool refuses.
- At most 3 asks per turn and 6 per minute per pane (a council counts as one), on top of the normal call
  and dollar budgets. Over the limit the agent is told to continue without the dot.
- A dot that was asked skips its own review of that turn (no double advice); the roster shows `consulted`.
- Memory the agent writes is always a project proposal you approve, and secret-looking text is refused.
- Results are capped at about 24,000 characters.

```toml
[mcp]
tools = ["list_dots", "ask_dot", "ask_council", "list_queue", "read_memory", "propose_memory"]
max_asks_per_turn   = 3
max_asks_per_minute = 6
```

`ask_dot` waits up to `run_timeout_seconds` + 10 s (130 s by default). Codex stops waiting after
`tool_timeout_sec` (180 s as installed); a council can take longer with the default 120 s run timeout,
so raise `tool_timeout_sec` or lower `run_timeout_seconds` if Codex gives up on councils. Like any
council, an `ask_council` in progress ends (with an error for the agent, and no card) if the pane closes
or the engine restarts. If `config.toml` can't be parsed when the agent starts the server, only
`list_dots` is offered until you fix it and restart the agent (the same fail-closed rule that turns
inject and memory off). The server logs to `mcp.log` in the plugin's state folder; it never prints
anything but protocol messages.

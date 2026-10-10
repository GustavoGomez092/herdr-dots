# Backends, keys and cost

Each dot runs on a backend (Claude Code, Codex, an API model or Ollama); this page covers choosing one, storing keys and capping cost.

Each dot picks its backend in the editor (`prefix+d`, then `e` or `n`) or in its TOML file:

| Kind | Talks to | Key | Cost shown |
|---|---|---|---|
| `claude-cli` | your `claude` login | none | from Claude Code (`total_cost_usd`) |
| `codex-cli` | your `codex` login | none | `?` (tokens only) |
| `anthropic-api` | Anthropic Messages API | `ANTHROPIC_API_KEY` or `keys set anthropic` | price table |
| `openai-api` | OpenAI Responses API | `OPENAI_API_KEY` or `keys set openai` | price table |
| `openrouter` | OpenRouter | `OPENROUTER_API_KEY` or `keys set openrouter` | reported by OpenRouter |
| `ollama` | a local Ollama server | none | $0 |

```toml
[backend]
kind       = "anthropic-api"
model      = "claude-opus-5-5"   # the default; set effort = "low" or pick a cheaper model to spend less
effort     = "low"               # low | medium | high | xhigh | max (anthropic-api, openai-api)
max_tokens = 4096
fallbacks  = "default"           # default | off: on a safety refusal Anthropic retries on another model

[budget]
max_calls_per_session = 40
max_usd_per_session   = 0.50     # 0 = off
```

`openai-api`, `openrouter` and `ollama` need a model (there is no OpenAI default; ids and prices change
often). Ollama dots take `base_url` and `context_window` (default 16384, always sent as `num_ctx`;
long prompts are shrunk to fit and the engine log says so).

**Keys.** `herdr-dots keys set <anthropic|openai|openrouter>` reads the key from stdin without echo
and stores it in `$(herdr plugin config-dir herdr-dots)/credentials.toml` (mode 0600, refused if group-
or world-readable). Environment variables win over the file; `[providers.<name>] api_key_env` in
`config.toml` renames the variable. `keys list` shows where each key comes from and its last 4
characters; `keys test` checks them; `keys remove` deletes one. The manage popup's `K` screen does the
same, with a masked entry field. Keys never go in dot files or `config.toml`, never appear on the
command line, and are scrubbed from errors, logs and the feed. Keys sit in a file on disk: keep it out
of dotfile repos. OS keychain storage is planned.

**Repo access.** With "Read repo" on, API dots can call `read_file`, `grep` and `list_files`, jailed
to the pane's working directory (symlinks can't escape it), honoring `.gitignore`, at most 8 calls per
run. These tools are read-only and are switched off when the pane's directory is your home directory
or `/` (the dot is told it has no repo tools). File names must match the names on disk exactly: a
case variant of a file name is "no such file". Secret files are always denied (`.env*`, `*.pem`,
`*.key`, `id_rsa*`, `id_ed25519*`, `*.p12`, `.git/**`, `**/secrets/**`, `.netrc`, `*.kdbx`);
`tool_deny_globs` in `config.toml` adds more.

**Cost.** Every run records tokens (including cached input) and a dollar cost: Anthropic and OpenAI
from an embedded price table dated 2026-10-08 (override entries in `prices.toml` next to
`config.toml`), OpenRouter from its own `usage.cost`, Ollama a known $0. OpenRouter keys that bring
your own provider key (BYOK) may report a cost of 0, so treat those figures as a lower bound. Models
without a price show `?` and never count toward dollar budgets (the editor warns). The Dots pane
header shows `$spent/budget`, the roster shows each dot's session cost, and the feed shows each run's
cost. When Anthropic's refusal fallback answers, the run is priced at the fallback model's rates and
the feed shows `model=…`; a declined first attempt isn't priced, so such a run's cost can be
slightly low.

**Councils.** Members and the chair can be API dots; their runs are priced like any other (the chair
counts against the pane). With `session_budget_usd` set, the council popup shows `$spent of $budget`,
members past their `max_usd_per_session` sit out, and a pane past its dollar budget can't start a
council. An API `chair_backend` without `chair_model` uses that kind's default (`claude-opus-5-5` for
`anthropic-api`); `openai-api`, `openrouter` and `ollama` chairs need `chair_model`.

## Codex dots and your other MCP servers

A codex dot run switches off every MCP server declared in your Codex
`config.toml` (`$CODEX_HOME/config.toml`, else `~/.codex/config.toml`), so a
dot never starts them. Codex splits a `-c` override on `.` and `=`, so it can't
switch off a server whose name contains either character from the command
line. If any server in that file has such a name, codex dot runs are refused
with a message that starts "rename MCP server" until you rename it. This
affects `codex-cli` dots only. This path follows Codex's documented format and
has not been verified live (Codex is not installed on the developer's machine).

## An invalid `tool_deny_globs` entry turns repository tools off

If `tool_deny_globs` in `config.toml` holds an entry that is not a valid glob,
the engine fails closed instead of dropping it silently. Until you fix it:
repository tools for API and `claude-cli` dots, drafts, research and commit
reviews that read patches are all off. The engine logs it and shows one toast,
and the roster shows a hint (for example `research off · fix tool_deny_globs`).
`codex-cli` dots read the repository through Codex's own sandbox, so
`tool_deny_globs` and this switch do not limit what they read.

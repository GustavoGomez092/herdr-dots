# Phase 8 verification notes (proactive dots)

Date: 2026-10-08 (live checks run 2026-10-09). Machine: macOS (darwin), Herdr 0.8.2, Claude Code 2.1.295, git on PATH. Codex is not installed.

| # | Question | Finding | Evidence | Assumption holds |
|---|---|---|---|---|
| V1 | Does Herdr expose a user-activity signal? | No input timestamps. `PaneInfo.focused` and a `pane.focused` event exist (also `pane_focused`, `focused`, `focused_pane_*`, `focus_changed`, `tab.focused`, `workspace.focused`). Nothing like `last_input`, `input_at` or `idle_since`; the only `idle` hits are agent statuses. `pane.send_input` / `pane.input.set` are write calls, not signals. | `herdr api schema --json` grep (Step 1) | Yes. Design uses activity = new turn, agent working, focus gained |
| V2 | claude print mode with `WebSearch,WebFetch` | `is_error` false, `total_cost_usd` 0.0456, exit 0. The result was a JSON object naming Go 1.27 (released 2026-08-19) with URL https://go.dev/blog/go1.27. `~/.claude/settings.json` unchanged (9807e438…), `~/.codex/config.toml` unchanged (5ee4b989…), `grep -c herdr-dots ~/.claude.json` still 0. The model's reply began with a sentence saying a search result carried an instruction (add hyperlinks) and that it ignored it; the JSON was still present after that sentence, so the adapter must extract the JSON object from the text rather than require it to be the whole reply (`ParseVerdict` already tolerates this; Task 9 should keep a test for it). | Live run, prompt on stdin, scratch dir under `$TMPDIR` | Yes. `KindClaudeCLI` stays in `backend.WebSupported` (STOP rule not triggered) |
| V3 | codex exec web search under `--sandbox read-only` | Not verifiable: Codex not installed | — | not live-verified; codex researches without the web |
| V4 | Cost of the 15 s HEAD poll | 100 runs: 1 s (planning: 0.42 s per 100), so about 10 ms per call; four panes every 15 s is under 0.1 % of one core | Timed loop (`git rev-parse --verify --quiet HEAD` with `core.hooksPath=/dev/null`, `core.fsmonitor=false`) | Yes |
| V5 | Anthropic server web tools | No key: `ANTHROPIC_API_KEY` is unset on this machine. Not live-verified (user gate). | — | not live-verified, design unaffected |
| V6 | OpenAI hosted web search, stateless | No key: `OPENAI_API_KEY` is unset on this machine. Not live-verified (user gate). | — | not live-verified, design unaffected |

## Live soak (Task 16)

<filled by Task 16>

## Remaining user gates

- Codex web search (V3) and OpenAI web search (V6) are documented-format, not live-verified.
- Anthropic server web search (V5) needs an `ANTHROPIC_API_KEY`; run the brief's Step 4 curl once a key is available.
- A real Windows run of proactive dots is part of Phase 6's Windows gate.

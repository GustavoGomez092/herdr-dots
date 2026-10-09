# Phase 4 verification findings (V1–V6), 2026-10-08

Environment: go1.25.7 darwin/arm64 (`GOTOOLCHAIN=local`); macOS 26.5.2; ollama 0.22.1 (server already running on 127.0.0.1:11434, models qwen2.5:7b-instruct and mistral:latest present, nothing pulled). ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY: all unset (GEMINI_API_KEY unset too). No key value was read, printed or sent.

Every section records the command, the evidence and "Assumption holds: yes/no/not live-verified".

## Confirmed while planning (2026-10-08)

- Dependencies resolve and compile with Go 1.25.7 and `GOTOOLCHAIN=local`: anthropic-sdk-go v1.79.1 (go 1.24), openai-go/v3 v3.74.0 (go 1.25.0), x/term v0.37.0 (go 1.24.0). x/term v0.46.0 needs go 1.26, so it is not used. x/sys rises to v0.47.0. The go.mod directive becomes `go 1.25.0`.
- V1 (anthropic-sdk-go names, v1.79.1): `client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{...})`; `Betas: []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}`; `Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}`; `OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort("low"), Format: anthropic.BetaJSONOutputFormatParam{Schema: map[string]any{...}}}`; `ToolChoice: anthropic.BetaToolChoiceUnionParam{OfNone: &anthropic.BetaToolChoiceNoneParam{}}`; `resp.StopReason == anthropic.BetaStopReasonRefusal`, `resp.StopDetails.Category`; `anthropic.NewBetaToolResultBlock(id, content, isError)`; `client.Models.Get / List / ListAutoPaging`; `option.WithoutEnvironmentDefaults()`. Against an httptest server the body carries `"fallbacks":"default"` and no thinking/temperature keys, the header is `anthropic-beta: server-side-fallback-2026-07-01`, the path is `/v1/messages?beta=true`, and a `fallback` content block decodes as `anthropic.BetaFallbackBlock`. A 401's error string includes the response body (hence redaction).
- V2 (field): the Responses API reports cached input as `usage.input_tokens_details.cached_tokens` (openai-go `ResponseUsageInputTokensDetails.CachedTokens`), inside `input_tokens`.
- V2 (prices): developers.openai.com/api/docs/pricing, standard tier, read 2026-10-08: gpt-6-astra $10.00 / cached $1.00 / out $50.00; gpt-6.1-sol $2.00 / $0.10 / $10.00; gpt-6-sol $2.00 / $0.20 / $10.00; gpt-6-luna $0.10 / $0.01 / $0.50; gpt-5.6-sol $4.00 / $0.40 / $20.00; gpt-5.6-terra $2.00 / $0.20 / $12.00; gpt-5.6-luna $0.20 / $0.02 / $1.20 (per 1M tokens).
- OpenRouter usage accounting (openrouter.ai/docs/use-cases/usage-accounting): `usage.cost` is always returned; `usage: {include: true}` is deprecated and has no effect; `prompt_tokens_details.cached_tokens` and `cache_write_tokens`.
- V4 (docs): Ollama's default context length depends on VRAM (under 24 GB: 4,000 tokens; 24–48 GB: 32,000; 48 GB or more: 256,000), so `num_ctx` is always sent. `/api/chat` takes `tools`, `format` (`"json"` or a JSON schema), `options`, `stream`; tool results go back as `{"role":"tool","tool_name":…,"content":…}`.

## V1 — Anthropic SDK names and request shape

Re-check of the SDK surface (no key needed). In a scratch module outside the repo: `go get github.com/anthropics/anthropic-sdk-go@v1.79.1`, then a `main.go` using every symbol listed under V1 above (`Beta.Messages.New`, `AnthropicBetaServerSideFallback2026_07_01`, `BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}`, `BetaOutputConfigParam{Effort, Format}`, `BetaToolChoiceNoneParam`, `BetaStopReasonRefusal`, `StopDetails.Category`, `NewBetaToolResultBlock`, `Models.Get`, `Models.ListAutoPaging`, `BetaFallbackBlock`, `option.WithoutEnvironmentDefaults`). `go build ./...` succeeded with no output.

Live request (Step 2): not run, ANTHROPIC_API_KEY is unset.

Unauthenticated probe: `curl -i https://api.anthropic.com/v1/messages` with `x-api-key: invalid`, the beta header and `"fallbacks":"default"` returned `HTTP/2 401`, body `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"},"request_id":null}`. The error shape matches what Tasks 9 and 10 parse. This does not show whether the server accepts `fallbacks` or `output_config`, because auth is checked first.

Assumption holds: yes for the SDK names (compiled against v1.79.1). Live acceptance of `fallbacks` + `output_config`: not live-verified, design unaffected (Task 10's httptest suite pins the request shape; the first run with a key will confirm).

## V5 — structured output and tools together (Anthropic)

Needs the same live call as V1 Step 2. Assumption holds: not live-verified, design unaffected (no key on this machine; a 400 would surface as a clean provider error).

## V2 — OpenAI ids and prices

Fetched https://developers.openai.com/api/docs/pricing on 2026-10-08 and read the page's embedded table data for the `standard` tier (the page also embeds batch and flex tiers at half price; those are not used). The data rows have four numbers: input, cached input, a third column, output. Standard tier values (per 1M tokens), identical to the planning list:

| model | input | cached input | 3rd column | output |
|---|---|---|---|---|
| gpt-6-astra | 10 | 1 | 12.5 | 50 |
| gpt-6.1-sol | 2 | 0.1 | 2.5 | 10 |
| gpt-6-sol | 2 | 0.2 | 2.5 | 10 |
| gpt-6-luna | 0.1 | 0.01 | 0.125 | 0.5 |
| gpt-5.6-sol | 4 | 0.4 | 5 | 20 |
| gpt-5.6-terra | 2 | 0.2 | 2.5 | 12 |
| gpt-5.6-luna | 0.2 | 0.02 | 0.25 | 1.2 |

Difference from the plan: none in input, cached or output, so `prices.toml` needs no data change. Observation: the third column is always 1.25x input; its header was not extracted, so it is probably cache write. The OpenAI usage fields in the plan do not report cache writes, so Task 3 can leave it out. The live `/v1/models` listing (needs a key) was not run. An unauthenticated `GET https://api.openai.com/v1/models` with a bogus bearer returned `HTTP/2 401`, confirming the endpoint.

Assumption holds: yes for prices (re-read, no differences). Model ids exist on the live API: not live-verified, design unaffected (no model id is hard-coded as a default; prices are data).

## V3 — OpenRouter `response_format` and `usage.cost`

Live call (Step 4): not run, OPENROUTER_API_KEY is unset. An unauthenticated `POST https://openrouter.ai/api/v1/chat/completions` with a bogus bearer returned `HTTP/2 401` (endpoint reachable).

Docs re-read (openrouter.ai/docs/use-cases/usage-accounting, fetched 2026-10-08): `usage: { include: true }` and `stream_options: { include_usage: true }` are deprecated and have no effect; full usage details are always included in every response; the page documents `cached_tokens` and `cache_write_tokens`. This agrees with the planning notes.

Assumption holds: not live-verified, design unaffected. The docs support `usage.cost` always being present. Whether `response_format` json_schema is accepted depends on the model, and Task 12's retry-once-without-it fallback (Plan decision 14) covers a 400.

## V4 — Ollama `num_ctx`, `format` and `tools`

Live, against the running Ollama 0.22.1 with `qwen2.5:7b-instruct`, `stream:false`, `options.num_ctx=16384`, `num_predict=200`, one `list_files` tool, user message "Call list_files, then answer.":

1. `format` (JSON schema `{ok:boolean}`) and `tools` together: HTTP 200, `message.content` = `{"ok": true}`, no `tool_calls`, `done_reason":"stop"`. The grammar constraint won and the model could not call the tool.
2. `tools` only (no `format`): HTTP 200, `message.content` = `""`, `message.tool_calls` = `[{"id":"call_…","function":{"index":0,"name":"list_files","arguments":{}}}]`.
3. `num_ctx` in `options` is accepted (both calls succeeded with 16384; `prompt_eval_count` 139).

So a combined request is accepted, but `format` suppresses tool calls. This supports Plan decision 12: Task 13 must never combine them, sends tool turns without `format`, then re-asks once with `format` and no tools. Note that `tool_calls[].function.arguments` is a JSON object (not a string) and a call carries an `id`.

Assumption holds: yes, design unaffected (the evidence supports never combining them).

## V6 — password input in a Herdr popup

Partly run after Task 17 (binary built from this tree, temp `HERDR_PLUGIN_CONFIG_DIR` and `HERDR_PLUGIN_STATE_DIR`, a dummy key, no plugin link, no Herdr session):

- `echo <dummy> | herdr-dots keys set anthropic` stored the key and printed `stored · not verified: anthropic-api timed out: context deadline exceeded` (the dummy key never reached a working provider; the message carries no key text).
- `herdr-dots keys list` showed `anthropic  file  …ZZZZ` (last four only); `credentials.toml` is mode `-rw-------`.
- `herdr-dots keys` with no arguments prints the usage line before touching any plugin state.

Not run: the no-echo read inside a Herdr popup pane. That needs an interactive terminal in a `herdr --session dots-test` popup, which a non-interactive run cannot provide. User gate: in a test session popup, run `herdr-dots keys set openai` and confirm typed characters are not echoed. The Providers screen uses a masked Bubble Tea input and does not depend on this.

Assumption holds: piped path verified; popup no-echo not live-verified (user gate), design unaffected.

## Summary

| Check | Result |
|---|---|
| V1 | SDK names verified by compiling against v1.79.1; live request not verified (no key) |
| V2 | prices re-read, no differences; live model ids not verified (no key) |
| V3 | not live-verified (no key); docs agree; the fallback covers it |
| V4 | verified live: `format` + `tools` together suppresses tool calls; the design never combines them |
| V5 | not live-verified (no key) |
| V6 | piped `keys set`/`list` verified with a dummy key in a temp dir; popup no-echo is a user gate |

No assumption was contradicted, so there are no GATE lines.

## Results

Final state of the assumptions (no key existed on the verification machine; every live provider run is a user gate).

| Check | Assumption holds | Live evidence |
|---|---|---|
| V1 Anthropic SDK names and request shape | yes for SDK names (compiled against anthropic-sdk-go v1.79.1); live acceptance of `fallbacks` + `output_config` not live-verified | not live-verified: no key |
| V2 OpenAI ids and prices | yes for prices (re-read 2026-10-08, no differences); live model ids not live-verified | not live-verified: no key |
| V3 OpenRouter `response_format` and `usage.cost` | not live-verified, design unaffected (docs agree; retry without `response_format` covers a 400). BYOK keys may report `usage.cost` 0 (documented in the README) | not live-verified: no key |
| V4 Ollama `num_ctx`, `format` and `tools` | yes: `format` together with `tools` suppresses tool calls, so the design never combines them; `num_ctx` accepted | verified live: Ollama 0.22.1, `qwen2.5:7b-instruct` |
| V5 structured output and tools together (Anthropic) | not live-verified, design unaffected | not live-verified: no key |
| V6 key entry | piped `keys set`/`list` verified with a dummy key (stored in a 0600 file, listed as its last 4 characters only); no-echo terminal read and Herdr popup check not verified | user gate: run `herdr-dots keys set openai` in a `--session dots-test` popup and confirm nothing is echoed |

User gates before relying on the API backends: one live run each for `anthropic-api`, `openai-api` and `openrouter` (needs your key), and the V6 popup check.

SDK names the compiler made us change: none. The names listed under V1 compiled as planned.

No assumption was contradicted, so there are no GATE lines.

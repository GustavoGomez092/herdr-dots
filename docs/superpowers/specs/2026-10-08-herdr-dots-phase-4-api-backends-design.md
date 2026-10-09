# Herdr Dots Phase 4: API and Ollama Backends, Design Spec

**Status: DRAFT, pending user review**

- **Date:** 2026-10-08
- **Phase:** 4 of 10 (see the [roadmap](2026-10-08-herdr-dots-roadmap.md)). Phase 2, **dot memory (VITAL)**, comes first.
- **Builds on:** v1 `backend` (Task 8–9 interfaces), `dots`, `config`, `state.Usage`, `engine.Decide`; Phase 2 `backend.Request.Memory`; Phase 3 chair runs (they work through any backend)
- **Plan outline:** `docs/superpowers/plans/drafts/2026-10-08-herdr-dots-phase-4-api-backends-outline.md`
- **The product is not done after this phase.** Ships as `0.4.0`.

---

## 1. Intent

### What the user wants

v1 dots run only through the `claude` and `codex` CLIs. That's the cheapest path for subscribers, but it ties dots to two vendors and to slow CLI start-ups. The spec's promise is "provider-agnostic: each dot picks its own backend and model." Phase 4 delivers that with four direct backends behind the same `backend.Backend` interface:

- **`anthropic-api`**: Anthropic Messages API
- **`openai-api`**: OpenAI Responses API
- **`openrouter`**: any model on OpenRouter (OpenAI-compatible Chat Completions)
- **`ollama`**: local models, no key, no cost, nothing leaves the machine

Along with them: API keys stored safely in the plugin config dir, honest **cost reporting** in dollars per dot and per session, and dollar budgets next to v1's call budgets.

### Success criteria

1. Creating a dot with any of the four kinds works from the manage editor. Saving runs `Backend.Check`, which validates the key and the model, or that the Ollama model is pulled.
2. API dots can read the repo like CLI dots (`repo_read = true`) through a built-in **read-only tool loop** (`read_file`, `grep`, `list_files`) jailed to the project root. Secret files are denied by default.
3. Verdicts use each provider's **structured output** feature where available, so malformed verdicts become rare. `ParseVerdict` still guards every reply.
4. Every run records input, output and cache tokens and a **USD cost** (provider-reported for OpenRouter; computed from an embedded, dated, overridable price table for Anthropic and OpenAI; $0 for Ollama). The pane header shows session cost, and the feed shows per-run cost.
5. `max_usd_per_session` per dot and `session_budget_usd` per pane stop runs like call budgets do.
6. Keys never appear in logs, errors, snapshots, IPC or memory. The credentials file is `0600` and refused if it's group- or world-readable.
7. v1 CLI dots behave exactly as before.

---

## 2. Research summary (looked up 2026-10-08)

| Provider | Endpoint and SDK | Structured output | Usage and cost | Notes for us |
|---|---|---|---|---|
| Anthropic | Messages API, `POST /v1/messages`; official Go SDK `github.com/anthropics/anthropic-sdk-go` (`client.Messages.New`) | `output_config.format` (JSON schema); `strict: true` on tool definitions | `usage.input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`. Prices (per 1M, in/out): Opus 5.5 $4/$20, Sonnet 5.5 $2/$10, Haiku 4.5 $1/$5; cache reads $0.20 on Opus 5.5 / Sonnet 5.5 | Current default model is `claude-opus-5-5`. On Opus 5.5 thinking can't be disabled (omit `thinking`); depth is set with `output_config.effort` (default `medium` on Opus 5.5). Sampling params (`temperature`) return 400 on current models. Assistant prefill returns 400. Forced `tool_choice` (`any`/`tool`) returns 400 on Opus 5.5 / Sonnet 5.5: use `auto`. `stop_reason: "refusal"` with `stop_details.category` must be checked. Server-side refusal fallbacks: beta `server-side-fallback-2026-07-01` with `fallbacks: "default"`. SDK credential chain: `ANTHROPIC_API_KEY` → `ANTHROPIC_AUTH_TOKEN` → `ant auth login` profile. Prompt caching via `cache_control` (minimum prefix 512–4,096 tokens depending on model). Models API `GET /v1/models/{id}` validates the key and model |
| OpenAI | Responses API, `POST /v1/responses`; official Go SDK `github.com/openai/openai-go/v3` (`client.Responses.New`; v3.45+ needs Go 1.25) | `text.format = {type: "json_schema", name, strict: true, schema}`; refusals arrive as a `refusal` content item | `usage.input_tokens`, `output_tokens`, cached input tokens, `output_tokens_details.reasoning_tokens` | Model ids and prices change often (the 2026-10 pricing page lists a `gpt-6` family; the third-party listings I found conflicted), so **no default model is hard-coded** and the price table is data. `GET /v1/models/{id}` for `Check` |
| OpenRouter | `POST https://openrouter.ai/api/v1/chat/completions`, OpenAI-compatible, Bearer key | `response_format: {type: "json_schema", json_schema: {name, strict, schema}}` (model-dependent) | **`usage.cost` is always returned** (credits, USD), plus token counts; no extra flag needed | Model ids like `anthropic/claude-opus-5-5`. Optional attribution headers `HTTP-Referer`, `X-Title` |
| Ollama | `POST {base}/api/chat` (default `http://127.0.0.1:11434`, honors `OLLAMA_HOST`); no SDK needed | `format: <JSON schema>` (Ollama ≥ 0.5.0); `stream: false` | `prompt_eval_count`, `eval_count`, `total_duration`; cost $0 | `GET /api/tags` lists pulled models. **The default context window can be small**: set `options.num_ctx` explicitly, or long prompts are truncated silently |

Sources: Anthropic facts come from the bundled `claude-api` skill (cached 2026-09-25). OpenAI: developers.openai.com structured-outputs guide and the openai-go README. OpenRouter: the usage-accounting docs. Ollama: the structured-outputs blog post and API docs.

---

## 3. Decisions

| # | Decision | Choice | Rejected |
|---|---|---|---|
| A1 | Client libraries | Official SDKs for Anthropic (`anthropic-sdk-go`) and OpenAI (`openai-go/v3`). Hand-rolled `net/http` + `encoding/json` for OpenRouter and Ollama (small, stable JSON APIs) | The OpenAI SDK pointed at OpenRouter (loses the `usage.cost` field typing); the Ollama Go module (pulls in the whole Ollama codebase) |
| A2 | Repo access for API dots | A provider-neutral **read-only tool loop** in `internal/backend/toolloop`: `read_file`, `grep`, `list_files`, jailed to the project root, deny-list for secret files, ≤ 8 tool calls per run, ≤ 64 KB per tool result | No repo access for API dots (breaks parity with CLI dots); a full agent harness (YAGNI) |
| A3 | Verdict shape | Structured output with the verdict JSON schema (`relevant`, `severity`, `message`, optional `remember`) when the provider or model supports it; prompt-only otherwise. `ParseReply` always runs | Trusting structured output blindly |
| A4 | Streaming | **Not used** in Phase 4. Verdicts are short (`max_tokens` default 4,096), well under SDK timeouts | Streaming for live pane progress (no UI need yet) |
| A5 | Key storage | `CONFIG_DIR/credentials.toml` (`0600`, owner check, refused if group- or world-readable), or per-provider `api_key_env` naming an environment variable. Lookup order: `api_key_env` → credentials file → (Anthropic only) the SDK's default chain | OS keychain (platform-specific; deferred to the open decisions); plain `config.toml` (often shared in dotfiles) |
| A6 | Key entry | Manage popup "Providers" screen (masked input) and `herdr-dots keys set <provider>` (reads stdin with no echo) | Typing keys into dot TOML |
| A7 | Cost | `internal/pricing` with an embedded `prices.toml` (dated), overridable by `CONFIG_DIR/prices.toml`. OpenRouter uses `usage.cost`. Ollama $0. CLI backends keep v1 behavior (claude's `total_cost_usd`; codex tokens only). Unknown model → cost shown as `?`, `CostEstimated=false`, and dollar budgets don't count it (a warning in the editor) | Fetching prices at runtime (network dependency, no stable endpoint) |
| A8 | Anthropic defaults | Model `claude-opus-5-5`; `thinking` omitted (adaptive); `effort` from the dot (`[backend] effort`, empty = provider default); `max_tokens` 4,096; the system prompt block marked `cache_control` ephemeral; **server-side refusal fallbacks on by default** (`fallbacks = "default"`, beta `server-side-fallback-2026-07-01`) for the models that support them, with a per-dot `fallbacks = "off"` | Cheaper default model (that's the user's call, not ours) |
| A9 | Refusals | Anthropic `stop_reason == "refusal"` and OpenAI `refusal` items become a run error `refused (<category>)`. They're logged to the feed and count toward the failure streak | Treating a refusal as `relevant:false` (hides a real problem) |
| A10 | Retries | SDK defaults (2 retries on 408/409/429/5xx and connection errors) for Anthropic and OpenAI. The same policy is implemented for OpenRouter and Ollama (2 retries, exponential backoff with `Retry-After` respected). All within `run_timeout_seconds` | Unbounded retries |
| A11 | Ollama context | Dot setting `[backend] context_window` (default 16,384) → `options.num_ctx`. Before sending, estimate prompt tokens (characters / 3.5); if over 90%, shrink the turns first, then memory, and log it | Leaving Ollama's small default |
| A12 | Dollar budgets | `[budget] max_usd_per_session` per dot, `session_budget_usd` per pane in `config.toml` (0 = off). Checked in `Decide` | Calls-only budgets (meaningless across providers) |

---

## 4. Architecture

### 4.1 Packages

```
internal/backend/backend.go        Backend interface (unchanged), Request/Result additions, kinds
internal/backend/registry.go       NewWith(kind, Options); New(kind, r) kept as a wrapper
internal/backend/schema.go         VerdictSchema() (JSON schema shared by all API backends)
internal/backend/toolloop/         Tool, RepoTools(root, Limits), Jail, Driver
internal/backend/anthropicapi/     Messages API adapter (SDK)
internal/backend/openaiapi/        Responses API adapter (SDK)
internal/backend/openrouter/       Chat Completions adapter (net/http)
internal/backend/ollama/           /api/chat adapter (net/http)
internal/backend/httpx/            shared JSON POST + retry/backoff + redaction for hand-rolled clients
internal/credentials/              credentials.toml load/save/lookup, permission check, Redact
internal/pricing/                  embedded prices.toml, override, Cost()
```

Each adapter implements `backend.Backend` (`Kind`, `Check`, `Run`) plus the optional `backend.ModelLister` (`Models(ctx) ([]string, error)`) used by the editor's model picker.

### 4.2 The tool loop (shared)

```
Run(req):
  msgs := [user: MemoryPrompt + UserPrompt]
  for step := 0; ; step++ {
     reply := provider.call(system, msgs, tools if req.RepoRead, schema)
     usage += reply.usage
     if reply.toolCalls == 0 or step == MaxToolCalls: break
     for each call: result := toolloop.Execute(call) (jailed, capped); append to msgs
  }
  if the last reply was a tool call (cap hit): one final call with tools disabled and
     "Tool budget exhausted; answer now."
  return ParseReply(reply.text) + usage + cost
```

`toolloop.Driver` holds the step loop, caps and the final-answer turn. Each adapter supplies a `Turn` function that maps provider wire formats to `toolloop.Call` and `toolloop.Reply`. The tools:

| Tool | Args | Behavior |
|---|---|---|
| `read_file` | `path`, `offset` (line), `limit` (≤ 400 lines) | Path resolved with `filepath.EvalSymlinks` and required to stay under the root; deny-list applied; binary files refused |
| `grep` | `pattern` (RE2), `glob` (optional) | Walks the root respecting `.gitignore` (via `git ls-files -co --exclude-standard` when it's a git repo), ≤ 200 matches |
| `list_files` | `glob` | `doublestar` match over the same file list, ≤ 500 paths |

Default deny-list: `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`, `id_ed25519*`, `*.p12`, `.git/**`, `**/secrets/**`, `.netrc`, `*.kdbx`. Configurable in `config.toml` as `tool_deny_globs` (additive only, so it can never be emptied).

### 4.3 Changes to v1 and Phase 2 interfaces (exact)

| Symbol | Change |
|---|---|
| `backend.Request` | Add `Effort string`, `MaxTokens int`, `BaseURL string`, `ContextWindow int`, `Fallbacks string` |
| `backend.Result` | Add `CacheReadTokens, CacheWriteTokens int`, `CostKnown bool`, `Model string` (the model that actually answered, which matters with fallbacks), `ToolCalls int`, `Duration time.Duration` |
| `backend` | Add consts `KindClaudeCLI = "claude-cli"`, `KindCodexCLI = "codex-cli"`, `KindAnthropicAPI = "anthropic-api"`, `KindOpenAIAPI = "openai-api"`, `KindOpenRouter = "openrouter"`, `KindOllama = "ollama"`; `type ModelLister interface{ Models(ctx context.Context) ([]string, error) }`; `type Options struct{ Runner Runner; Creds credentials.Source; HTTP *http.Client; Prices pricing.Table; Provider config.ProviderConfig }`; `NewWith(kind string, o Options) (Backend, error)`. `New(kind string, r Runner) (Backend, error)` stays, calling `NewWith(kind, Options{Runner: r})` (API kinds then fail `Check` with "no credentials") |
| `backend.ClaudeCLI` | `Run` sets `Result.CostKnown = true` when `total_cost_usd` is present (v1 already parses it) |
| `dots.BackendKinds` | Append the four API kinds |
| `dots.BackendSpec` | Add `Effort string` (`""`, `low`, `medium`, `high`, `xhigh`, `max`; valid for `anthropic-api` and `openai-api`), `MaxTokens int` (0 = 4,096; ≤ 64,000), `BaseURL string` (`ollama` and `openrouter` only), `ContextWindow int` (`ollama` only; 2,048–262,144), `Fallbacks string` (`anthropic-api` only: `default` \| `off`). Validation: `Model` required for API kinds except `anthropic-api` (default `claude-opus-5-5`) |
| `dots.BudgetSpec` | Add `MaxUSDPerSession float64` (0 = off) |
| `config.Config` | Add `SessionBudgetUSD float64`, `Providers map[string]ProviderConfig` (TOML `[providers.<name>]`: `api_key_env string`, `base_url string`, `extra_headers map[string]string` for OpenRouter attribution), `ToolDenyGlobs []string`, `PricesFile string` (default `CONFIG_DIR/prices.toml`) |
| `paths.Paths` | Add `CredentialsFile() string` (`ConfigDir/credentials.toml`), `PricesFile() string` |
| `state.Usage` | Add `type Delta struct{ Calls, In, Out, CacheRead, CacheWrite int; CostUSD float64; CostKnown bool }`; `Add(paneID, slug string, d Delta) error`; `Cost(paneID, slug string) float64`; `PaneCost(paneID string) float64`. `Record(paneID, slug, in, out)` stays as a wrapper (`Add` with `Calls:1`). File format gains fields; old files load with 0 cost |
| `engine.FilterInput` | Add `DotCostUSD, PaneCostUSD, DotBudgetUSD, PaneBudgetUSD float64`. `Decide` returns `ReasonBudget` / `ReasonSessionBudget` when a dollar budget is reached (same reasons as calls) |
| `engine.runDot` | Fills the new `Request` fields from `dots.BackendSpec`; records `state.Delta` from `Result` |
| `engine.Deps.Backend` | Unchanged signature `func(kind string) (backend.Backend, error)`; `buildEngine` closes over `backend.Options` |
| `view.DotState` | Add `CostUSD float64`, `CostKnown bool` |
| `view.Pane` | Add `CostUSD, BudgetUSD float64` |
| `state.FeedEntry` | `Detail` for flagged/pass runs gains a `cost=$0.0123 tokens=1234/210` suffix (format-only change) |
| `ui/manage` form | Backend kind cycles six kinds; kind-specific fields appear (effort, base URL, context window, fallbacks); a model picker from `ModelLister` when available |
| `ui/manage` | New "Providers" screen (`K` from the library) |
| `main.dispatch` | Route `keys` → `runKeys(args []string) error` |

---

## 5. Data model

### 5.1 Dot TOML examples

```toml
[backend]
kind      = "anthropic-api"
model     = "claude-opus-5-5"
effort    = "low"          # optional; empty = provider default
max_tokens = 4096
fallbacks = "default"      # default | off

[budget]
max_calls_per_session = 40
max_usd_per_session   = 0.50
```

```toml
[backend]
kind           = "ollama"
model          = "qwen3-coder:30b"
base_url       = "http://127.0.0.1:11434"
context_window = 32768
```

### 5.2 `credentials.toml` (`0600`)

```toml
# Managed by herdr-dots. Do not commit.
[anthropic]
api_key = "sk-ant-…"
[openai]
api_key = "sk-…"
[openrouter]
api_key = "sk-or-…"
```

### 5.3 `prices.toml` (embedded; override file has the same shape)

```toml
as_of = "2026-10-08"
[anthropic."claude-opus-5-5"]
input = 4.00
output = 20.00
cache_read = 0.20
cache_write = 5.00     # 5-minute cache writes at 1.25× input
[anthropic."claude-sonnet-5-5"]
input = 2.00
output = 10.00
cache_read = 0.20
cache_write = 2.50
[anthropic."claude-haiku-4-5"]
input = 1.00
output = 5.00
cache_read = 0.10
cache_write = 1.25
# openai entries are filled from the official pricing page when the phase starts (V2)
```

### 5.4 `config.toml` additions

```toml
session_budget_usd = 2.00
tool_deny_globs = ["**/fixtures/private/**"]

[providers.anthropic]
api_key_env = "ANTHROPIC_API_KEY"
[providers.ollama]
base_url = "http://127.0.0.1:11434"
[providers.openrouter.extra_headers]
"X-Title" = "Herdr Dots"
```

---

## 6. Data flow

### 6.1 Run (API backend)

1. `engine.runDot` → `Deps.Backend(kind)` → the adapter (cached per kind).
2. The adapter resolves the key: `credentials.Lookup(provider, providerConfig)` → `(key, source)`. Missing → `ErrNoCredentials` with the hint "run `herdr-dots keys set anthropic` or set ANTHROPIC_API_KEY". For Anthropic with no key, the SDK's default chain is tried before failing.
3. Build the request: system = `SystemPrompt(r)`; user = `MemoryPrompt(r)` + `UserPrompt(r)` (Anthropic: the memory block is its own content block with `cache_control`, so it caches across runs while turns change); tools = `toolloop.RepoTools` when `RepoRead`; structured output = `VerdictSchema()`.
4. `toolloop.Driver.Run` → final text → `ParseReply` → `Result` with tokens, `Model`, `ToolCalls`, `Duration`.
5. Cost: OpenRouter `usage.cost`; others `pricing.Table.Cost(provider, model, usage)`; Ollama 0. `CostKnown` reflects whether a price was found.
6. `engine` records `state.Usage.Add(pane, slug, Delta{…})`. The feed detail includes cost.

### 6.2 Check (editor save, and a `r` reset)

| Kind | Check |
|---|---|
| `anthropic-api` | `client.Models.Get(model)` → 401: "key rejected"; 404: "model not available to this key" |
| `openai-api` | `GET /v1/models/{model}` |
| `openrouter` | `GET /api/v1/models`, confirm the id exists; key validated with `GET /api/v1/key` |
| `ollama` | `GET /api/version` (unreachable → "Ollama isn't running: start it with `ollama serve`"), then `GET /api/tags` (model missing → "run `ollama pull <model>`") |

### 6.3 Key management

- `herdr-dots keys set <provider>`: prompt without echo (`golang.org/x/term`), write atomically with `0600`, then `Check` with a cheap call (`Models` list). Prints `stored · verified` or the redacted error.
- `keys list`: shows providers with the key source (`env:ANTHROPIC_API_KEY` / `file` / `sdk-default` / `missing`) and the last 4 characters only.
- `keys remove <provider>`; `keys test [<provider>]`.

---

## 7. UI

- **Editor:** a `kind` field cycles `claude-cli → codex-cli → anthropic-api → openai-api → openrouter → ollama`. Kind-specific fields show and hide. The model field offers `ctrl+space` completion from `ModelLister`. Below the backend fields: "est. $0.004/run at 3 turns" when the price is known, or "price unknown: dollar budgets won't count this dot".
- **Providers screen (`K`):**

```
┌ PROVIDERS ────────────────────────────────────────────────┐
│ anthropic   file  …a9F2   ✓ verified 2m ago              │
│ openai      env:OPENAI_API_KEY   ✓                        │
│ openrouter  missing                                       │
│ ollama      http://127.0.0.1:11434  ✓ 6 models            │
└ s set key  t test  x remove  esc back ───────────────────┘
```

- **Dots pane:** the header gains `$0.42/2.00`; the roster shows each dot's session cost; the feed shows the per-run cost.

---

## 8. Errors, safety, testing

### 8.1 Errors

| Failure | Behavior |
|---|---|
| Missing or invalid key | `error` status with an actionable hint; counts toward the 3-failure stop |
| 429 / 529 / 5xx | Retried (A10), then error `rate limited` / `provider overloaded` |
| Refusal | `refused (<category>)` error; feed entry |
| Tool loop cap reached | Final no-tools answer turn; feed detail `tools: 8 (cap)` |
| Jail escape attempt (`../`, symlink out, absolute path) | Tool result error `path outside project`; logged; the run continues |
| Ollama down / model missing | Hint as in §6.2 |
| Prompt over Ollama context | Shrink turns, then memory; log `trimmed to fit num_ctx` |
| Unknown price | Cost `?`; dollar budgets ignore it; editor warning |
| `credentials.toml` too permissive | Refuse to read; error "credentials.toml is readable by others: run chmod 600" |

### 8.2 Safety

- Keys stay inside the backend adapters and `internal/credentials`. `credentials.Redact(s)` scrubs anything key-shaped from errors before they reach the feed, logs, toasts or snapshots. A test asserts no key substring ever reaches `engine.log`, the feed, or a snapshot.
- API dots are read-only by construction: the only tools are the three read tools, jailed and deny-listed.
- Prompt injection: the same posture as v1 (queue default, read-only tools). Tool results are wrapped as data.
- Privacy: the README states which provider receives transcript excerpts, memory and file contents for each kind, and that Ollama stays local.

### 8.3 Testing

| Layer | Tests |
|---|---|
| Adapters | `httptest.Server` fakes per provider: request shape (model, schema, tools, effort, `cache_control`, fallbacks header), a tool-call round trip, refusal handling, 429-then-200 retry, 401 hint, usage parsing, cost computation |
| Tool loop | Jail (`../`, absolute, symlink-out), deny-list, binary refusal, caps, the final-answer turn after the cap |
| Credentials | Permission refusal, env precedence, atomic write `0600`, redaction table |
| Pricing | Embedded table parses, override merges, unknown model → `known=false` |
| Engine | Dollar-budget stop via `Decide`; `Usage.Add` persistence; old `usage.json` loads |
| Live (`-tags live`) | One real call per provider, skipped when no key or Ollama isn't running |
| Always | `go test -race ./...`, `go vet ./...` |

---

## 9. Verification questions

| # | Question | Affects |
|---|---|---|
| V1 | Exact `anthropic-sdk-go` field names for `OutputConfig.Format` (JSON schema), `Effort`, the beta `Fallbacks` parameter on `client.Beta.Messages.New`, and refusal `StopDetails` in the version pinned at phase start (write code first, fix against the compiler, per the claude-api skill's guidance) | Anthropic adapter |
| V2 | Current OpenAI model ids and prices from the official pricing page; whether the cached-token field is `usage.input_tokens_details.cached_tokens` in Responses API output | OpenAI adapter, `prices.toml` |
| V3 | Does OpenRouter's `response_format: json_schema` work across the user's chosen models, or should OpenRouter fall back to prompt-only when a model rejects it (detect 400, retry without)? | OpenRouter adapter |
| V4 | Ollama's default `num_ctx` in the installed version, and whether `format` + `tools` can be combined in one `/api/chat` call | Ollama adapter, A11 |
| V5 | Does structured output + tool use in one Anthropic request behave (tools first, then a schema-conforming final message)? | A2/A3 |
| V6 | Can `golang.org/x/term` read a password inside a Herdr popup pane (TTY present)? | Providers screen, `keys set` |

---

## 10. Out of scope

Streaming; OS keychain storage; Bedrock, Vertex and Foundry providers; Gemini or other native APIs (reachable through OpenRouter); write tools for API dots (Phase 7); web search tools (Phase 8); automatic price updates.

---

## 11. Open decisions for the user

| # | Decision | Options | Recommendation | Why |
|---|---|---|---|---|
| O1 | Key storage | (a) `credentials.toml` 0600 + env indirection; (b) OS keychain (macOS `security`, Linux `secret-tool`); (c) env vars only | **(a)** now, (b) as a later enhancement | Meets "key storage in the plugin config dir" and works everywhere, including headless Linux |
| O2 | Default Anthropic model for new API dots | (a) `claude-opus-5-5`; (b) `claude-sonnet-5-5`; (c) `claude-haiku-4-5` | **(a)** | The current default model. Cost is your call: set `effort = "low"` or pick Sonnet per dot |
| O3 | Anthropic refusal fallbacks | (a) on by default (`fallbacks: "default"`); (b) off | **(a)** | A safety-classifier false positive on a code review shouldn't silently drop a review. The fallback model and cost are reported in `Result.Model` |
| O4 | Repo tools for API dots | (a) built-in read-only tool loop; (b) no repo access | **(a)** | Parity with CLI dots |
| O5 | Streaming | (a) none; (b) stream for pane progress | **(a)** | Short outputs; no UI need yet |
| O6 | Unknown-price dots and dollar budgets | (a) ignore cost (warn); (b) refuse to run under a dollar budget | **(a)** | Doesn't block local or new models |

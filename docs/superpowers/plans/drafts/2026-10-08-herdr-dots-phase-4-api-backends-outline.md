# Herdr Dots Phase 4: API and Ollama Backends, Implementation Plan Outline

**Status: DRAFT, pending user review**

> **This is an outline, not an executable plan.** Earlier phases' code doesn't exist yet. When Phase 4 starts, write the full step-by-step plan with superpowers:writing-plans against the real code (load the `claude-api` skill before writing the Anthropic adapter, and re-check OpenAI, OpenRouter and Ollama docs on the web), then execute it with superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Add `anthropic-api`, `openai-api`, `openrouter` and `ollama` backends behind v1's `backend.Backend` interface, with a shared read-only repo tool loop, safe key storage, and USD cost reporting and budgets.

**Architecture:** One adapter package per provider implements `Backend` (+ optional `ModelLister`). A provider-neutral `toolloop` drives tool calls with jailed read-only tools. `credentials` stores keys in `CONFIG_DIR/credentials.toml` (0600) or reads env vars; `pricing` computes cost from an embedded, overridable table. `state.Usage` and `engine.Decide` learn dollars.

**Tech Stack:** Go 1.25; new dependencies `github.com/anthropics/anthropic-sdk-go`, `github.com/openai/openai-go/v3` (≥ v3.45 for Go 1.25), `golang.org/x/term`; existing `doublestar/v4`, `BurntSushi/toml`.

**Spec:** `docs/superpowers/specs/2026-10-08-herdr-dots-phase-4-api-backends-design.md`

## Global Constraints

- The product is **not** done after this phase. Version `0.4.0`. The README roadmap keeps Phase 2 **VITAL**.
- Go module `herdr-dots`, `go 1.25`; macOS and Linux only.
- Anthropic code: model id strings exactly as published (default `claude-opus-5-5`, never date-suffixed); never send `temperature`, `top_p`, `top_k`, `budget_tokens`, `thinking: disabled`, assistant prefill, or forced `tool_choice` (`any`/`tool`); always check `stop_reason == "refusal"`; server-side fallbacks on by default (`fallbacks: "default"`, beta `server-side-fallback-2026-07-01`) and documented.
- API dots are read-only: tools are `read_file`, `grep`, `list_files` only, jailed to the project root, with a deny-list that config can extend but never shrink.
- Keys never reach logs, feed, snapshots, IPC, memory or error strings (`credentials.Redact`). `credentials.toml` is `0600` and refused if more permissive.
- Default `max_tokens` 4,096; ≤ 8 tool calls per run; ≤ 64 KB per tool result; all within `run_timeout_seconds`.
- v1 CLI backends behave exactly as before. `backend.New(kind, r)` stays.
- Files `0600`, directories `0700`, atomic writes. `go test -race ./...` and `go vet ./...` before every commit; live tests only behind `-tags live`.

## Review Focus

1. **Symlink or `../` escape from the tool jail.** A model asks `read_file("../../.ssh/id_rsa")` or a symlink inside the repo points outside. Expect a tool error, no bytes leaked, and a log line. (Task 3)
2. **A key in an error message.** The provider echoes the key in a 401 body, or the URL contains it. Expect redaction before the feed, toast or log. (Task 2, Task 4)
3. **Ollama silently truncating a long prompt.** With 20 turns + memory over `num_ctx`, expect the prompt shrunk to fit with a log line, not a garbage verdict. (Task 7)
4. **Rate limiting mid-council** (Phase 3 fan-out). Expect 429 retried with backoff within the run timeout, then a clean `rate limited` error, never a hung run. (Task 4)
5. **A model without a known price under a dollar budget.** Expect the cost shown as `?`, the run allowed, an editor warning, and budgets never computing NaN. (Task 2, Task 8)

---

## File Structure

```
internal/credentials/credentials.go     Source interface, File store, Lookup, Redact, perms check
internal/pricing/pricing.go             Table, Default, Load, Cost
internal/pricing/prices.toml            embedded dated price table
internal/backend/backend.go             + kinds, Request/Result fields, ModelLister, Options
internal/backend/registry.go            + NewWith
internal/backend/schema.go              VerdictSchema
internal/backend/httpx/httpx.go         PostJSON with retry/backoff/redaction
internal/backend/toolloop/tools.go      Tool, RepoTools, Jail, deny-list
internal/backend/toolloop/driver.go     Call, Reply, Driver
internal/backend/anthropicapi/anthropic.go
internal/backend/openaiapi/openai.go
internal/backend/openrouter/openrouter.go
internal/backend/ollama/ollama.go
internal/dots/dot.go                    + BackendSpec fields, kinds, BudgetSpec.MaxUSDPerSession, validation
internal/config/config.go               + SessionBudgetUSD, Providers, ToolDenyGlobs, PricesFile
internal/paths/paths.go                 + CredentialsFile, PricesFile
internal/state/usage.go                 + Delta, Add, Cost, PaneCost
internal/engine/filter.go               dollar budgets
internal/engine/engine.go               request fields, usage deltas
internal/view/view.go                   + cost fields
internal/ui/manage/form.go              kind-specific fields, model picker
internal/ui/manage/providers.go         Providers screen
internal/ui/pane/model.go               cost display
cmd/herdr-dots/keys.go                  keys CLI
cmd/herdr-dots/engine.go                buildEngine backend.Options
README.md                               backends, keys, privacy per provider, cost
```

---

## Tasks

### Task 1: Verification spike (V1–V6)

**Files:** Create `docs/superpowers/notes/phase-4-verification.md`.
**Interfaces:** Produces findings only: exact SDK field names (Anthropic V1, OpenAI V2), OpenAI model ids and prices for `prices.toml`, OpenRouter schema support (V3), Ollama `num_ctx` default and `format`+`tools` combination (V4), Anthropic structured output + tools (V5), password input in a Herdr popup (V6).
**Tests:** None. Each finding records the command, the evidence, and "assumption holds: yes/no". STOP and report to the user on any "no".

### Task 2: Credentials and pricing

**Files:** Create `internal/credentials/credentials.go`, `internal/pricing/pricing.go`, `internal/pricing/prices.toml`. Modify `internal/paths/paths.go`. Test: `internal/credentials/credentials_test.go`, `internal/pricing/pricing_test.go`.
**Interfaces:**
- Consumes: `fsutil.WriteFileAtomic`, `paths.Paths`.
- Produces: `type Source interface{ Lookup(provider string) (key, from string, err error) }`; `type File struct{ Path string; Env func(string) string; EnvNames map[string]string }`; `(File) Lookup(provider string) (string, string, error)`; `(File) Set(provider, key string) error`; `(File) Remove(provider string) error`; `(File) List() ([]Status, error)`; `type Status struct{ Provider, From, Last4 string }`; `var ErrNoCredentials, ErrInsecurePerms`; `Redact(s string) string`. Pricing: `type Usage struct{ In, Out, CacheRead, CacheWrite int }`; `type Price struct{ Input, Output, CacheRead, CacheWrite float64 }`; `type Table struct{ AsOf string; Prices map[string]map[string]Price }`; `Default() Table`; `Load(path string) (Table, error)` (merges over `Default`); `(Table) Cost(provider, model string, u Usage) (usd float64, known bool)`; `(paths.Paths) CredentialsFile() string`, `PricesFile() string`.
**Tests:** env precedence over file; a `0644` file → `ErrInsecurePerms`; `Set` writes `0600` atomically; `List` shows only the last 4 characters; `Redact` table (sk-ant-, sk-or-, sk-, Bearer headers, `api_key=`); embedded table parses with `as_of`; override merges one model; unknown model → `known=false`; cost arithmetic for Opus 5.5 with cache read/write.

### Task 3: Tool loop

**Files:** Create `internal/backend/toolloop/tools.go`, `driver.go`. Test: `internal/backend/toolloop/tools_test.go`, `driver_test.go`.
**Interfaces:**
- Consumes: `doublestar/v4`; `git ls-files` via `os/exec` with `-c core.hooksPath=/dev/null`.
- Produces: `type Tool struct{ Name, Description string; Schema map[string]any; Run func(ctx context.Context, args json.RawMessage) (string, error) }`; `type Limits struct{ MaxCalls, MaxResultBytes, MaxReadLines, MaxGrepMatches, MaxListPaths int; Deny []string }`; `DefaultLimits() Limits`; `RepoTools(root string, lim Limits) []Tool`; `ResolveInRoot(root, p string) (string, error)`; `var ErrOutsideRoot, ErrDenied`; `type Call struct{ ID, Name string; Args json.RawMessage }`; `type Result struct{ CallID, Content string; IsError bool }`; `type Reply struct{ Text string; Calls []Call; Usage pricing.Usage }`; `type TurnFunc func(ctx context.Context, results []Result, final bool) (Reply, error)`; `type Driver struct{ Tools []Tool; Limits Limits }`; `(Driver) Run(ctx context.Context, turn TurnFunc) (text string, usage pricing.Usage, calls int, err error)`.
**Tests:** `../` and absolute paths → `ErrOutsideRoot`; a symlink pointing outside → `ErrOutsideRoot`; `.env` and `id_rsa` → `ErrDenied`; a binary file refused; `read_file` offset/limit; `grep` respects `.gitignore`; caps truncate with a marker; the driver stops at `MaxCalls` and issues one `final=true` turn; tool errors are returned to the model as `IsError` results, not run failures.

### Task 4: Backend core changes and the shared HTTP helper

**Files:** Modify `internal/backend/backend.go`, `registry.go`; create `internal/backend/schema.go`, `internal/backend/httpx/httpx.go`. Test: `internal/backend/registry_test.go`, `schema_test.go`, `httpx/httpx_test.go`.
**Interfaces:**
- Consumes: v1 `Backend`, `Request`, `Result`, `New`; Tasks 2–3.
- Produces: kind consts (`KindClaudeCLI` … `KindOllama`); `Request.Effort, MaxTokens, BaseURL, ContextWindow, Fallbacks`; `Result.CacheReadTokens, CacheWriteTokens int; CostKnown bool; Model string; ToolCalls int; Duration time.Duration`; `type ModelLister interface{ Models(ctx context.Context) ([]string, error) }`; `type Options struct{ Runner Runner; Creds credentials.Source; HTTP *http.Client; Prices pricing.Table; Provider config.ProviderConfig; Deny []string }`; `NewWith(kind string, o Options) (Backend, error)`; `VerdictSchema() map[string]any`; `httpx.PostJSON(ctx context.Context, c *http.Client, url string, headers map[string]string, body, out any, retries int) (status int, err error)` and `httpx.GetJSON(...)` (retry on 408/409/429/5xx with backoff and `Retry-After`; errors passed through `credentials.Redact`).
**Tests:** `New("claude-cli", r)` still works; `NewWith` for each kind returns the right type; an unknown kind errors; the schema validates a sample verdict with and without `remember`; httpx retries 429 then succeeds, honors `Retry-After`, stops at the context deadline, and redacts a key echoed in an error body.

### Task 5: Anthropic adapter

**Files:** Create `internal/backend/anthropicapi/anthropic.go`. Test: `internal/backend/anthropicapi/anthropic_test.go`, `anthropic_live_test.go` (`//go:build live`).
**Interfaces:**
- Consumes: `anthropic-sdk-go` (`anthropic.NewClient`, `option.WithAPIKey`, `option.WithBaseURL` for tests, `option.WithHTTPClient`, `client.Messages.New`, `client.Beta.Messages.New` for fallbacks, `client.Models.Get` (exact names per V1)); `toolloop.Driver`; `pricing`; `credentials`.
- Produces: `type Backend struct{ …unexported… }`; `New(o backend.Options) *Backend`; methods `Kind() string` (`"anthropic-api"`), `Check(ctx) error`, `Run(ctx, backend.Request) (backend.Result, error)`, `Models(ctx) ([]string, error)`; `const DefaultModel = "claude-opus-5-5"`.
**Tests (against `httptest.Server` via `option.WithBaseURL`):** the request has no `temperature`/`thinking`/prefill; `output_config.effort` only when set; the JSON-schema output format is present; the system block has `cache_control`; the memory block is a separate cached user block; the fallbacks beta header and `fallbacks: "default"` are present unless `Fallbacks == "off"`; a tool-use round trip through the driver; `stop_reason: "refusal"` → error containing the category; usage → cost via the price table; 401 → "key rejected"; 404 on `Check` → "model not available".

### Task 6: OpenAI adapter

**Files:** Create `internal/backend/openaiapi/openai.go`. Test: `openai_test.go`, `openai_live_test.go` (`live`).
**Interfaces:**
- Consumes: `github.com/openai/openai-go/v3` (`openai.NewClient`, `option.WithAPIKey`, `option.WithBaseURL`, `client.Responses.New`); driver, pricing, credentials.
- Produces: `New(o backend.Options) *Backend`; `Kind()` = `"openai-api"`; `Check`, `Run`, `Models`.
**Tests:** `text.format` json_schema strict; `reasoning.effort` mapped from `Effort`; a function-tool round trip; a refusal content item → error; usage parsing including cached tokens (field per V2); a missing model → validation error at the dot level, not at runtime.

### Task 7: OpenRouter and Ollama adapters

**Files:** Create `internal/backend/openrouter/openrouter.go`, `internal/backend/ollama/ollama.go`. Test: `openrouter_test.go`, `ollama_test.go`, and live variants.
**Interfaces:**
- Consumes: `httpx`, driver, credentials (OpenRouter), pricing (unused for OpenRouter because `usage.cost` is authoritative).
- Produces: `openrouter.New(o backend.Options) *Backend` (`Kind()` = `"openrouter"`); `ollama.New(o backend.Options) *Backend` (`Kind()` = `"ollama"`); both implement `Check`, `Run`, `Models`; `ollama.EstimateTokens(s string) int`; `ollama.FitPrompt(r backend.Request, window int) (backend.Request, bool)`.
**Tests:** OpenRouter: `response_format` json_schema; a 400 "schema unsupported" → one retry without `response_format` (per V3); `usage.cost` → `CostUSD` with `CostKnown=true`; extra headers sent. Ollama: `stream:false`, `format` schema, `options.num_ctx`; `OLLAMA_HOST` honored; connection refused → "Ollama isn't running" hint; model missing in `/api/tags` → `ollama pull` hint; `FitPrompt` drops the oldest turns, then memory, and reports trimming; cost 0 with `CostKnown=true`.

### Task 8: Dots, config, usage and budget changes

**Files:** Modify `internal/dots/dot.go`, `internal/config/config.go`, `internal/state/usage.go`, `internal/engine/filter.go`, `internal/engine/engine.go`, `internal/view/view.go`. Test: extend the corresponding `_test.go` files.
**Interfaces:**
- Consumes: Task 4 kinds and fields.
- Produces: `dots.BackendSpec.Effort, MaxTokens, BaseURL, ContextWindow, Fallbacks`; `dots.BudgetSpec.MaxUSDPerSession`; `config.Config.SessionBudgetUSD`, `Providers map[string]ProviderConfig`, `ToolDenyGlobs []string`, `PricesFile string`; `config.ProviderConfig{APIKeyEnv, BaseURL string; ExtraHeaders map[string]string}`; `state.Delta{Calls, In, Out, CacheRead, CacheWrite int; CostUSD float64; CostKnown bool}`; `(*Usage) Add(paneID, slug string, d Delta) error`, `Cost(paneID, slug string) float64`, `PaneCost(paneID string) float64`; `engine.FilterInput.DotCostUSD, PaneCostUSD, DotBudgetUSD, PaneBudgetUSD`; `view.DotState.CostUSD, CostKnown`; `view.Pane.CostUSD, BudgetUSD`.
**Tests:** validation per kind (effort on ollama → error; base_url on anthropic → error; model empty on openai → error; anthropic defaults the model); old dot TOML and old `usage.json` load; `Decide` with a dollar budget hit → `ReasonBudget` / `ReasonSessionBudget`; unknown-cost runs (`CostKnown=false`) don't count toward dollar budgets; `runDot` passes the spec fields into `Request` and records a `Delta`.

### Task 9: Editor, Providers screen, pane cost, keys CLI

**Files:** Modify `internal/ui/manage/form.go`, `internal/ui/manage/model.go`, `internal/ui/pane/model.go`; create `internal/ui/manage/providers.go`, `cmd/herdr-dots/keys.go`; modify `cmd/herdr-dots/main.go`, `cmd/herdr-dots/engine.go`, `cmd/herdr-dots/ui.go`. Test: extend the UI tests; `cmd/herdr-dots/keys_test.go`.
**Interfaces:**
- Consumes: `credentials.File`, `backend.NewWith`, `backend.ModelLister`, `pricing.Table`.
- Produces: `manage.Deps.Creds credentials.Source`, `manage.Deps.Lister func(kind string) backend.ModelLister`, `manage.Deps.Prices pricing.Table`; `manage.NewProviders(d Deps) ProvidersModel`; `runKeys(args []string) error` (`set|remove|list|test`); `buildEngine` builds `backend.Options` once and caches adapters per kind.
**Tests:** the kind cycle shows and hides fields; the model picker lists fake models; the estimate line shows "price unknown" for an unpriced model; Providers screen `s` stores via a fake `Source` and never renders the full key; `keys set` reads stdin with no echo (stdin pipe in tests), writes `0600`, verifies via a fake backend; the pane header shows `$x.xx/y.yy`; `go test -race ./... && go vet ./...`; live tests with real keys run manually and are recorded in `docs/superpowers/notes/phase-4-verification.md`; README updated (providers, privacy per provider, cost caveats; Phase 2 still **VITAL**; no "done/ready").

// Package anthropicapi is the anthropic-api backend: the Anthropic Messages
// API through the official Go SDK, with structured verdict output, the
// read-only repo tool loop, prompt caching and server-side refusal fallbacks.
package anthropicapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/backend/httpx"
	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/platform"
	"herdr-dots/internal/pricing"
	"herdr-dots/internal/textutil"
)

// DefaultModel is used when a dot sets no model (decision 7).
const DefaultModel = "claude-opus-5-5"

// Provider is the credentials, pricing and config name.
const Provider = "anthropic"

func init() {
	backend.Register(backend.KindAnthropicAPI, func(o backend.Options) (backend.Backend, error) { return New(o), nil })
}

// Backend implements backend.Backend, backend.ModelLister and
// backend.ModelChecker.
type Backend struct{ o backend.Options }

func New(o backend.Options) *Backend { return &Backend{o: o} }

func (b *Backend) Kind() string { return backend.KindAnthropicAPI }

// client builds an SDK client. With a key from credentials the SDK ignores
// the environment (WithoutEnvironmentDefaults); with none and SDKCreds set it
// uses the SDK's chain (ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, `ant auth`
// profile). The returned key is "" in the SDK-chain case.
func (b *Backend) client() (anthropic.Client, string, error) {
	var key string
	var err error
	if b.o.Creds != nil {
		key, _, err = b.o.Creds.Lookup(Provider)
	} else {
		err = fmt.Errorf("%w for anthropic: run `herdr-dots keys set anthropic`", credentials.ErrNoCredentials)
	}
	var opts []option.RequestOption
	switch {
	case err == nil:
		opts = append(opts, option.WithoutEnvironmentDefaults(), option.WithAPIKey(key))
	case errors.Is(err, credentials.ErrNoCredentials) && b.o.SDKCreds:
		// fall through to the SDK's own credential chain
	default:
		return anthropic.Client{}, "", err
	}
	opts = append(opts, option.WithMaxRetries(2))
	hc := b.o.HTTP
	if hc == nil {
		hc = httpx.SameHostClient() // never forward the key to another host on a redirect
	}
	opts = append(opts, option.WithHTTPClient(hc))
	if u := b.o.Provider.BaseURL; u != "" {
		opts = append(opts, option.WithBaseURL(u))
	}
	for k, v := range b.o.Provider.ExtraHeaders {
		opts = append(opts, option.WithHeader(k, v))
	}
	return anthropic.NewClient(opts...), key, nil
}

// Check validates the key with the cheapest call: list one model.
func (b *Backend) Check(ctx context.Context) error {
	c, key, err := b.client()
	if err != nil {
		return err
	}
	_, err = c.Models.List(ctx, anthropic.ModelListParams{Limit: anthropic.Int(1)})
	return mapErr(err, key, "")
}

// CheckModel confirms r.Model (or the default) is available to this key.
func (b *Backend) CheckModel(ctx context.Context, r backend.Request) error {
	model := r.Model
	if model == "" {
		model = DefaultModel
	}
	c, key, err := b.client()
	if err != nil {
		return err
	}
	_, err = c.Models.Get(ctx, model, anthropic.ModelGetParams{})
	return mapErr(err, key, model)
}

// Models lists the model ids this key can use.
func (b *Backend) Models(ctx context.Context, _ backend.Request) ([]string, error) {
	c, key, err := b.client()
	if err != nil {
		return nil, err
	}
	var ids []string
	pager := c.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for pager.Next() {
		ids = append(ids, pager.Current().ID)
	}
	return ids, mapErr(pager.Err(), key, "")
}

// Run reviews one turn. Request shape: model, max_tokens, the system prompt
// as one cached block, the memory block as its own cached user block, the
// verdict schema as output_config.format, effort only when the dot sets it,
// no thinking/temperature/top_p/top_k/prefill/forced tool_choice, and
// fallbacks "default" unless the dot turns them off.
func (b *Backend) Run(ctx context.Context, r backend.Request) (backend.Result, error) {
	started := time.Now()
	c, key, err := b.client()
	if err != nil {
		return backend.Result{}, err
	}
	model := r.Model
	if model == "" {
		model = DefaultModel
	}
	// A draft (Phase 7) also gets write_file and edit_file, jailed to the
	// worktree, and a larger tool budget; reviews and memory passes don't.
	tools, lim, note := toolloop.Select(r.Cwd, r.RepoRead && r.Pass != backend.PassMemory, backend.WriteMode(r), toolloop.WithDeny(b.o.Deny))
	system := backend.SystemPrompt(r)
	if note != "" {
		system += "\n" + note + "\n"
	}
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(backend.MaxTokensFor(r)),
		System:    []anthropic.BetaTextBlockParam{{Text: system, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(userBlocks(r)...)},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Format: anthropic.BetaJSONOutputFormatParam{Schema: backend.VerdictSchema(r)},
		},
	}
	if r.Effort != "" {
		params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(r.Effort)
	}
	if r.Fallbacks != "off" {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
	}
	for _, t := range tools {
		tp := anthropic.BetaToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: t.Schema["properties"], Required: requiredOf(t.Schema)},
		}
		params.Tools = append(params.Tools, anthropic.BetaToolUnionParam{OfTool: &tp})
	}
	webOn := r.Web && r.Pass == backend.PassResearch && !r.Write // never mixed with write tools
	repoTools := params.Tools
	// webUsed counts the paid web uses (searches + fetches) over the whole
	// run: every tool-loop turn and every pause_turn resume (ruling P8-R6).
	webUsed := 0
	// declareTools gives each call only what is left of the budget; at zero
	// the web tools are dropped and the repo tools stay.
	declareTools := func() {
		params.Tools = repoTools
		if left := webMaxUses - webUsed; webOn && left > 0 {
			params.Tools = append(append([]anthropic.BetaToolUnionParam(nil), repoTools...),
				anthropic.BetaToolUnionParam{OfWebSearchTool20260209: &anthropic.BetaWebSearchTool20260209Param{MaxUses: anthropic.Int(int64(left))}},
				anthropic.BetaToolUnionParam{OfWebFetchTool20260209: &anthropic.BetaWebFetchTool20260209Param{
					MaxUses: anthropic.Int(int64(left)), MaxContentTokens: anthropic.Int(webFetchMaxTokens)}},
			)
		}
	}
	if webOn {
		// Search results come back with citations, which structured outputs
		// reject: the JSON contract in the system prompt and ParseReply cover
		// the reply instead. The dynamic-filtering web tools run their own
		// code execution, so code_execution is never declared.
		params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{}
	}
	prices := b.o.PriceTable()
	// send makes one call and resumes a turn the server paused while running
	// web tools (stop_reason "pause_turn"): the paused assistant turn goes
	// back as is, with no extra user message. Usage and searches are summed,
	// and returned with an error too, so paid calls are never lost.
	send := func(ctx context.Context) (*anthropic.BetaMessage, pricing.Usage, int, error) {
		var u pricing.Usage
		searches := 0
		for n := 0; ; n++ {
			declareTools()
			resp, err := c.Beta.Messages.New(ctx, params)
			if err != nil {
				return nil, u, searches, err
			}
			params.Messages = append(params.Messages, resp.ToParam())
			u = u.Add(pricing.Usage{In: int(resp.Usage.InputTokens), Out: int(resp.Usage.OutputTokens),
				CacheRead: int(resp.Usage.CacheReadInputTokens), CacheWrite: int(resp.Usage.CacheCreationInputTokens)})
			s, f := webUses(resp)
			searches += s
			webUsed += s + f
			if resp.StopReason != anthropic.BetaStopReasonPauseTurn {
				return resp, u, searches, nil
			}
			if n == maxPauses || webUsed >= webMaxUses {
				return resp, u, searches, fmt.Errorf("web research paused %d times without finishing", n+1)
			}
		}
	}
	turn := func(ctx context.Context, results []toolloop.Result, final bool) (toolloop.Reply, error) {
		if len(results) > 0 {
			var blocks []anthropic.BetaContentBlockParamUnion
			for _, res := range results {
				blocks = append(blocks, anthropic.NewBetaToolResultBlock(res.CallID, res.Content, res.IsError))
			}
			if final {
				blocks = append(blocks, anthropic.NewBetaTextBlock(toolloop.FinalNudge))
			}
			params.Messages = append(params.Messages, anthropic.NewBetaUserMessage(blocks...))
		}
		if final && len(tools) > 0 {
			// Repo tools stay declared (the history holds tool_use blocks);
			// "none" is allowed on every model, unlike forced any/tool. With
			// only web tools there is no tool budget to end, so a web-only
			// first (final) turn keeps tool_choice unset.
			params.ToolChoice = anthropic.BetaToolChoiceUnionParam{OfNone: &anthropic.BetaToolChoiceNoneParam{}}
		}
		resp, u, searches, err := send(ctx)
		served := model
		if resp != nil && resp.Model != "" {
			served = string(resp.Model)
		}
		usd, known := prices.CostServed(Provider, served, model, u)
		if known {
			usd += float64(searches) * WebSearchUSD
		}
		if err != nil {
			if u == (pricing.Usage{}) {
				return toolloop.Reply{}, mapErr(err, key, model)
			}
			// Calls already paid for (earlier pauses) keep their spend.
			return toolloop.Reply{Usage: u, CostUSD: usd, CostKnown: known, Model: served}, mapErr(err, key, model)
		}
		reply := toolloop.Reply{Usage: u, CostUSD: usd, CostKnown: known, Model: served}
		if resp.StopReason == anthropic.BetaStopReasonRefusal {
			return reply, backend.Refused(string(resp.StopDetails.Category))
		}
		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.BetaTextBlock:
				reply.Text += v.Text
			case anthropic.BetaToolUseBlock:
				reply.Calls = append(reply.Calls, toolloop.Call{ID: v.ID, Name: v.Name, Args: json.RawMessage(v.JSON.Input.Raw())})
			}
		}
		if resp.StopReason == anthropic.BetaStopReasonMaxTokens {
			return reply, fmt.Errorf("reply cut off at max_tokens (%d): raise [backend] max_tokens", backend.MaxTokensFor(r))
		}
		return reply, nil
	}
	d := toolloop.Driver{Tools: tools, Limits: lim, Logf: b.o.Logf}
	out, err := d.Run(ctx, turn)
	run := backend.APIRun{Text: out.Text, Usage: out.Usage, CostUSD: out.CostUSD, CostKnown: out.CostKnown,
		Model: out.Model, ToolCalls: out.Calls, Capped: out.Capped, Started: started}
	if err != nil {
		return backend.PartialAPI(run), err
	}
	if out.Model != "" && out.Model != model {
		b.o.Log("anthropic-api: %s answered for %s (refusal fallback)", textutil.SanitizeModelText(out.Model), textutil.SanitizeModelText(model))
	}
	return backend.FinishAPI(r, run)
}

// userBlocks is the memory block (cached on its own, so it is reused while
// the conversation changes) followed by the rest of the user prompt.
func userBlocks(r backend.Request) []anthropic.BetaContentBlockParamUnion {
	var blocks []anthropic.BetaContentBlockParamUnion
	if m := backend.MemoryPrompt(r); m != "" {
		blocks = append(blocks, anthropic.BetaContentBlockParamUnion{OfText: &anthropic.BetaTextBlockParam{
			Text: m, CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}})
	}
	rest := r
	rest.Memory = ""
	return append(blocks, anthropic.NewBetaTextBlock(backend.UserPrompt(rest)))
}

func requiredOf(schema map[string]any) []string {
	req, _ := schema["required"].([]string)
	return req
}

// mapErr turns SDK errors into actionable, redacted run errors.
func mapErr(err error, key, model string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("anthropic-api timed out: %w", err)
	}
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch code := apierr.StatusCode; {
		case code == 401 || code == 403:
			return fmt.Errorf("anthropic key rejected (%d): run `herdr-dots keys set anthropic`", code)
		case code == 404 && model != "":
			return fmt.Errorf("model %q not available to this key", model)
		case code == 429:
			return errors.New("rate limited by anthropic (after retries)")
		case code == 529 || code >= 500:
			return fmt.Errorf("provider overloaded (anthropic %d, after retries)", code)
		default:
			return fmt.Errorf("anthropic rejected the request (%d): %s", code, clip(credentials.Redact(apierr.Error(), key), 300))
		}
	}
	msg := errors.New(clip(credentials.Redact(err.Error(), key), 300))
	if platform.ConnRefused(err) {
		return backend.MarkUnreachable(msg)
	}
	return msg
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

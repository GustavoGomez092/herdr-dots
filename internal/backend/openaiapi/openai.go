// Package openaiapi is the openai-api backend: the OpenAI Responses API
// through the official Go SDK (openai-go/v3). Requests are stateless
// (store=false) and carry encrypted reasoning items between tool turns.
// No model is a default: OpenAI ids and prices change often.
package openaiapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/pricing"
)

const (
	Provider       = "openai"
	DefaultBaseURL = "https://api.openai.com/v1"
)

func init() {
	backend.Register(backend.KindOpenAIAPI, func(o backend.Options) (backend.Backend, error) { return New(o), nil })
}

type Backend struct{ o backend.Options }

func New(o backend.Options) *Backend { return &Backend{o: o} }

func (b *Backend) Kind() string { return backend.KindOpenAIAPI }

func (b *Backend) client() (openai.Client, string, error) {
	if b.o.Creds == nil {
		return openai.Client{}, "", fmt.Errorf("%w for openai: run `herdr-dots keys set openai`", credentials.ErrNoCredentials)
	}
	key, _, err := b.o.Creds.Lookup(Provider)
	if err != nil {
		return openai.Client{}, "", err
	}
	base := b.o.Provider.BaseURL
	if base == "" {
		base = DefaultBaseURL // explicit, so OPENAI_BASE_URL can't redirect a dot
	}
	opts := []option.RequestOption{option.WithAPIKey(key), option.WithBaseURL(base), option.WithMaxRetries(2)}
	if b.o.HTTP != nil {
		opts = append(opts, option.WithHTTPClient(b.o.HTTP))
	}
	for k, v := range b.o.Provider.ExtraHeaders {
		opts = append(opts, option.WithHeader(k, v))
	}
	return openai.NewClient(opts...), key, nil
}

func (b *Backend) Check(ctx context.Context) error {
	c, key, err := b.client()
	if err != nil {
		return err
	}
	pager := c.Models.ListAutoPaging(ctx)
	pager.Next()
	return mapErr(pager.Err(), key, "")
}

func (b *Backend) CheckModel(ctx context.Context, model string) error {
	if model == "" {
		return errors.New("openai-api needs a model: set [backend] model")
	}
	c, key, err := b.client()
	if err != nil {
		return err
	}
	_, err = c.Models.Get(ctx, model)
	return mapErr(err, key, model)
}

func (b *Backend) Models(ctx context.Context) ([]string, error) {
	c, key, err := b.client()
	if err != nil {
		return nil, err
	}
	var ids []string
	pager := c.Models.ListAutoPaging(ctx)
	for pager.Next() {
		ids = append(ids, pager.Current().ID)
	}
	return ids, mapErr(pager.Err(), key, "")
}

func (b *Backend) Run(ctx context.Context, r backend.Request) (backend.Result, error) {
	started := time.Now()
	if r.Model == "" {
		return backend.Result{}, errors.New("openai-api needs a model: set [backend] model")
	}
	c, key, err := b.client()
	if err != nil {
		return backend.Result{}, err
	}
	var tools []toolloop.Tool
	lim := toolloop.WithDeny(b.o.Deny)
	if r.RepoRead && r.Cwd != "" && r.Pass != backend.PassMemory {
		tools = toolloop.RepoTools(r.Cwd, lim)
	}
	system := backend.SystemPrompt(r)
	switch {
	case len(tools) > 0:
		system += "\n" + toolloop.SystemNote + "\n"
	case r.RepoRead && r.Pass != backend.PassMemory && toolloop.Unavailable(r.Cwd):
		system += "\n" + toolloop.NoToolsNote + "\n"
	}
	input := responses.ResponseInputParam{
		responses.ResponseInputItemParamOfMessage(backend.UserPrompt(r), responses.EasyInputMessageRoleUser),
	}
	params := responses.ResponseNewParams{
		Model:           shared.ResponsesModel(r.Model),
		Instructions:    openai.String(system),
		MaxOutputTokens: openai.Int(int64(backend.MaxTokensFor(r))),
		Store:           openai.Bool(false),
		Include:         []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent},
		Text: responses.ResponseTextConfigParam{Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{Name: "dot_reply", Schema: backend.VerdictSchema(r), Strict: openai.Bool(true)},
		}},
	}
	if r.Effort != "" {
		params.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(r.Effort)}
	}
	for _, t := range tools {
		params.Tools = append(params.Tools, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{
			Name: t.Name, Description: openai.String(t.Description), Parameters: strictSchema(t.Schema), Strict: openai.Bool(true),
		}})
	}
	prices := b.o.PriceTable()
	turn := func(ctx context.Context, results []toolloop.Result, final bool) (toolloop.Reply, error) {
		for _, res := range results {
			out := res.Content
			if res.IsError {
				out = "ERROR: " + out
			}
			item := responses.ResponseInputItemParamOfFunctionCallOutput(out)
			item.OfFunctionCallOutput.CallID = openai.String(res.CallID)
			input = append(input, item)
		}
		if final && len(results) > 0 {
			input = append(input, responses.ResponseInputItemParamOfMessage(toolloop.FinalNudge, responses.EasyInputMessageRoleUser))
		}
		if final && len(params.Tools) > 0 {
			params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptionsNone)}
		}
		params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: input}
		resp, err := c.Responses.New(ctx, params)
		if err != nil {
			return toolloop.Reply{}, mapErr(err, key, r.Model)
		}
		cached := int(resp.Usage.InputTokensDetails.CachedTokens)
		u := pricing.Usage{In: int(resp.Usage.InputTokens) - cached, Out: int(resp.Usage.OutputTokens), CacheRead: cached}
		served := resp.Model
		if served == "" {
			served = r.Model
		}
		usd, known := prices.CostServed(Provider, served, r.Model, u)
		reply := toolloop.Reply{Usage: u, CostUSD: usd, CostKnown: known, Model: served}
		var refusal string
		for _, it := range resp.Output {
			switch v := it.AsAny().(type) {
			case responses.ResponseReasoningItem:
				p := v.ToParam()
				input = append(input, responses.ResponseInputItemUnionParam{OfReasoning: &p})
			case responses.ResponseFunctionToolCall:
				p := v.ToParam()
				input = append(input, responses.ResponseInputItemUnionParam{OfFunctionCall: &p})
				reply.Calls = append(reply.Calls, toolloop.Call{ID: v.CallID, Name: v.Name, Args: json.RawMessage(v.Arguments)})
			case responses.ResponseOutputMessage:
				p := v.ToParam()
				input = append(input, responses.ResponseInputItemUnionParam{OfOutputMessage: &p})
				for _, ct := range v.Content {
					switch x := ct.AsAny().(type) {
					case responses.ResponseOutputText:
						reply.Text += x.Text
					case responses.ResponseOutputRefusal:
						refusal = x.Refusal
					}
				}
			}
		}
		if refusal != "" {
			return reply, backend.Refused(clip(refusal, 80))
		}
		if resp.Status == responses.ResponseStatusIncomplete && len(reply.Calls) == 0 {
			return reply, fmt.Errorf("reply incomplete (%s): raise [backend] max_tokens", resp.IncompleteDetails.Reason)
		}
		return reply, nil
	}
	out, err := toolloop.Driver{Tools: tools, Limits: lim, Logf: b.o.Logf}.Run(ctx, turn)
	run := backend.APIRun{Text: out.Text, Usage: out.Usage, CostUSD: out.CostUSD, CostKnown: out.CostKnown,
		Model: out.Model, ToolCalls: out.Calls, Capped: out.Capped, Started: started}
	if err != nil {
		return backend.PartialAPI(run), err
	}
	return backend.FinishAPI(r, run)
}

// strictSchema makes a tool schema valid for strict mode: every property
// required, optional ones nullable.
func strictSchema(s map[string]any) map[string]any {
	props, _ := s["properties"].(map[string]any)
	req := map[string]bool{}
	if rs, ok := s["required"].([]string); ok {
		for _, r := range rs {
			req[r] = true
		}
	}
	np := map[string]any{}
	var all []string
	for k, v := range props {
		p, _ := v.(map[string]any)
		cp := map[string]any{}
		for kk, vv := range p {
			cp[kk] = vv
		}
		if !req[k] {
			cp["type"] = []any{cp["type"], "null"}
		}
		np[k] = cp
		all = append(all, k)
	}
	sort.Strings(all)
	return map[string]any{"type": "object", "properties": np, "required": all, "additionalProperties": false}
}

func mapErr(err error, key, model string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("openai-api timed out: %w", err)
	}
	var apierr *openai.Error
	if errors.As(err, &apierr) {
		switch code := apierr.StatusCode; {
		case code == 401 || code == 403:
			return fmt.Errorf("openai key rejected (%d): run `herdr-dots keys set openai`", code)
		case code == 404 && model != "":
			return fmt.Errorf("model %q not available to this key", model)
		case code == 429:
			return errors.New("rate limited by openai (after retries)")
		case code >= 500:
			return fmt.Errorf("provider overloaded (openai %d, after retries)", code)
		default:
			return fmt.Errorf("openai rejected the request (%d): %s", code, clip(credentials.Redact(apierr.Error(), key), 300))
		}
	}
	return errors.New(clip(credentials.Redact(err.Error(), key), 300))
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

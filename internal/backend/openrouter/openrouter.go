// Package openrouter is the openrouter backend: OpenRouter's OpenAI-compatible
// Chat Completions API over plain net/http. OpenRouter reports each call's
// cost in usage.cost, which is used as-is.
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/backend/httpx"
	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/pricing"
)

const (
	Provider       = "openrouter"
	DefaultBaseURL = "https://openrouter.ai/api/v1"
)

func init() {
	backend.Register(backend.KindOpenRouter, func(o backend.Options) (backend.Backend, error) { return New(o), nil })
}

type Backend struct {
	o        backend.Options
	noSchema sync.Map // model → true once it rejected response_format
}

func New(o backend.Options) *Backend { return &Backend{o: o} }

func (b *Backend) Kind() string { return backend.KindOpenRouter }

func (b *Backend) base(r backend.Request) string {
	for _, u := range []string{r.BaseURL, b.o.Provider.BaseURL} {
		if u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return DefaultBaseURL
}

func (b *Backend) client() (httpx.Client, error) {
	if b.o.Creds == nil {
		return httpx.Client{}, fmt.Errorf("%w for openrouter: run `herdr-dots keys set openrouter`", credentials.ErrNoCredentials)
	}
	key, _, err := b.o.Creds.Lookup(Provider)
	if err != nil {
		return httpx.Client{}, err
	}
	h := map[string]string{"Authorization": "Bearer " + key, "X-Title": "Herdr Dots"}
	for k, v := range b.o.Provider.ExtraHeaders {
		h[k] = v
	}
	return httpx.Client{HTTP: b.o.HTTP, Headers: h, Retries: 2, Secret: key}, nil
}

// Wire types (OpenAI Chat Completions shape).
type (
	message struct {
		Role       string     `json:"role"`
		Content    string     `json:"content"`
		ToolCalls  []toolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
		Refusal    string     `json:"refusal,omitempty"`
	}
	toolCall struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	toolDef struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	}
	chatRequest struct {
		Model          string         `json:"model"`
		Messages       []message      `json:"messages"`
		MaxTokens      int            `json:"max_tokens"`
		ResponseFormat map[string]any `json:"response_format,omitempty"`
		Tools          []toolDef      `json:"tools,omitempty"`
		ToolChoice     string         `json:"tool_choice,omitempty"`
	}
	chatResponse struct {
		Model   string `json:"model"`
		Choices []struct {
			Message      message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int      `json:"prompt_tokens"`
			CompletionTokens    int      `json:"completion_tokens"`
			Cost                *float64 `json:"cost"`
			PromptTokensDetails struct {
				CachedTokens     int `json:"cached_tokens"`
				CacheWriteTokens int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
)

func (b *Backend) Check(ctx context.Context) error { return b.check(ctx, backend.Request{}) }

// check validates the key at r's endpoint.
func (b *Backend) check(ctx context.Context, r backend.Request) error {
	c, err := b.client()
	if err != nil {
		return err
	}
	_, err = c.Do(ctx, "GET", b.base(r)+"/key", nil, nil)
	return mapErr(err, "")
}

func (b *Backend) Models(ctx context.Context, r backend.Request) ([]string, error) {
	c, err := b.client()
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if _, err := c.Do(ctx, "GET", b.base(r)+"/models", nil, &out); err != nil {
		return nil, mapErr(err, "")
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func (b *Backend) CheckModel(ctx context.Context, r backend.Request) error {
	model := r.Model
	if model == "" {
		return errors.New("openrouter needs a model, e.g. anthropic/claude-opus-5-5")
	}
	if err := b.check(ctx, r); err != nil {
		return err
	}
	ids, err := b.Models(ctx, r)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == model {
			return nil
		}
	}
	return fmt.Errorf("model %q is not listed by OpenRouter", model)
}

func (b *Backend) Run(ctx context.Context, r backend.Request) (backend.Result, error) {
	started := time.Now()
	if r.Model == "" {
		return backend.Result{}, errors.New("openrouter needs a model, e.g. anthropic/claude-opus-5-5")
	}
	c, err := b.client()
	if err != nil {
		return backend.Result{}, err
	}
	// A draft (Phase 7) also gets write_file and edit_file, jailed to the
	// worktree, and a larger tool budget; reviews and memory passes don't.
	tools, lim, note := toolloop.Select(r.Cwd, r.RepoRead && r.Pass != backend.PassMemory, backend.WriteMode(r), toolloop.WithDeny(b.o.Deny))
	system := backend.SystemPrompt(r)
	if note != "" {
		system += "\n" + note + "\n"
	}
	cr := chatRequest{
		Model:     r.Model,
		Messages:  []message{{Role: "system", Content: system}, {Role: "user", Content: backend.UserPrompt(r)}},
		MaxTokens: backend.MaxTokensFor(r),
	}
	schema := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "dot_reply", "strict": true, "schema": backend.VerdictSchema(r)}}
	for _, t := range tools {
		var d toolDef
		d.Type = "function"
		d.Function.Name, d.Function.Description, d.Function.Parameters = t.Name, t.Description, t.Schema
		cr.Tools = append(cr.Tools, d)
	}
	url := b.base(r) + "/chat/completions"
	prices := b.o.PriceTable()
	turn := func(ctx context.Context, results []toolloop.Result, final bool) (toolloop.Reply, error) {
		for _, res := range results {
			content := res.Content
			if res.IsError {
				content = "ERROR: " + content
			}
			cr.Messages = append(cr.Messages, message{Role: "tool", ToolCallID: res.CallID, Content: content})
		}
		if final && len(results) > 0 {
			cr.Messages = append(cr.Messages, message{Role: "user", Content: toolloop.FinalNudge})
		}
		if final && len(cr.Tools) > 0 {
			cr.ToolChoice = "none"
		}
		cr.ResponseFormat = nil
		if _, off := b.noSchema.Load(r.Model); !off {
			cr.ResponseFormat = schema
		}
		var resp chatResponse
		status, err := c.Do(ctx, "POST", url, cr, &resp)
		if err != nil && status == 400 && cr.ResponseFormat != nil && schemaRejected(err) {
			// V3: this model (or its provider) doesn't take json_schema;
			// remember that and fall back to prompt-only output.
			b.noSchema.Store(r.Model, true)
			b.o.Log("openrouter: %s rejected response_format; using prompt-only verdicts", r.Model)
			cr.ResponseFormat = nil
			_, err = c.Do(ctx, "POST", url, cr, &resp)
		}
		if err != nil {
			return toolloop.Reply{}, mapErr(err, r.Model)
		}
		cached := resp.Usage.PromptTokensDetails.CachedTokens
		write := resp.Usage.PromptTokensDetails.CacheWriteTokens
		reply := toolloop.Reply{
			Usage: pricing.Usage{In: max(0, resp.Usage.PromptTokens-cached-write), Out: resp.Usage.CompletionTokens, CacheRead: cached, CacheWrite: write},
			Model: resp.Model,
		}
		if resp.Usage.Cost != nil {
			reply.CostUSD, reply.CostKnown = *resp.Usage.Cost, true
		} else {
			// No usage.cost: a [openrouter."<model>"] entry in prices.toml
			// still prices the run (served id, undated, then requested).
			reply.CostUSD, reply.CostKnown = prices.CostServed(Provider, resp.Model, r.Model, reply.Usage)
		}
		if len(resp.Choices) == 0 {
			return reply, errors.New("openrouter returned no choices")
		}
		m := resp.Choices[0].Message
		if m.Refusal != "" {
			return reply, backend.Refused(clip(m.Refusal, 80))
		}
		reply.Text = m.Content
		for _, tc := range m.ToolCalls {
			reply.Calls = append(reply.Calls, toolloop.Call{ID: tc.ID, Name: tc.Function.Name, Args: json.RawMessage(tc.Function.Arguments)})
		}
		cr.Messages = append(cr.Messages, message{Role: "assistant", Content: m.Content, ToolCalls: m.ToolCalls})
		if resp.Choices[0].FinishReason == "length" && len(reply.Calls) == 0 {
			return reply, fmt.Errorf("reply cut off at max_tokens (%d): raise [backend] max_tokens", cr.MaxTokens)
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

func schemaRejected(err error) bool {
	low := strings.ToLower(err.Error())
	for _, s := range []string{"response_format", "json_schema", "structured output", "structured_output"} {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

func mapErr(err error, model string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("openrouter timed out: %w", err)
	}
	var se *httpx.StatusError
	if errors.As(err, &se) {
		switch code := se.Status; {
		case code == 401 || code == 403:
			return fmt.Errorf("openrouter key rejected (%d): run `herdr-dots keys set openrouter`", code)
		case code == 402:
			return errors.New("openrouter credits exhausted (402): add credits at openrouter.ai")
		case code == 404 && model != "":
			return fmt.Errorf("model %q not available on OpenRouter", model)
		case code == 429:
			return errors.New("rate limited by openrouter (after retries)")
		case code >= 500:
			return fmt.Errorf("provider overloaded (openrouter %d, after retries)", code)
		}
		return fmt.Errorf("openrouter rejected the request: %v", se)
	}
	return err // httpx already redacted it
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

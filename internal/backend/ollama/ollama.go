// Package ollama is the ollama backend: a local Ollama server's /api/chat
// over plain net/http. No key, no cost; nothing leaves the machine unless
// base_url points elsewhere. num_ctx is always set, and prompts are shrunk
// to fit it instead of being truncated silently by Ollama.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/backend/httpx"
	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/pricing"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/transcript"
)

const (
	Provider       = "ollama"
	DefaultBaseURL = "http://127.0.0.1:11434"
)

func init() {
	backend.Register(backend.KindOllama, func(o backend.Options) (backend.Backend, error) { return New(o), nil })
}

type Backend struct {
	o      backend.Options
	getenv func(string) string // OLLAMA_HOST; tests replace it
}

func New(o backend.Options) *Backend { return &Backend{o: o, getenv: os.Getenv} }

func (b *Backend) Kind() string { return backend.KindOllama }

// BaseURL picks the server: the dot's base_url, then [providers.ollama]
// base_url, then OLLAMA_HOST, then 127.0.0.1:11434.
func (b *Backend) BaseURL(r backend.Request) string {
	for _, u := range []string{r.BaseURL, b.o.Provider.BaseURL} {
		if u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	if h := strings.TrimSpace(b.getenv("OLLAMA_HOST")); h != "" {
		return normalizeHost(h)
	}
	return DefaultBaseURL
}

// normalizeHost turns OLLAMA_HOST forms (host, host:port, 0.0.0.0:port,
// scheme://host:port) into a client URL.
func normalizeHost(h string) string {
	if !strings.Contains(h, "://") {
		h = "http://" + h
	}
	u, err := url.Parse(h)
	if err != nil || u.Host == "" {
		return DefaultBaseURL
	}
	host, port := u.Hostname(), u.Port()
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "11434"
	}
	return u.Scheme + "://" + net.JoinHostPort(host, port)
}

func (b *Backend) client() httpx.Client {
	h := map[string]string{}
	for k, v := range b.o.Provider.ExtraHeaders {
		h[k] = v
	}
	return httpx.Client{HTTP: b.o.HTTP, Headers: h, Retries: 2}
}

type (
	message struct {
		Role      string     `json:"role"`
		Content   string     `json:"content"`
		ToolCalls []toolCall `json:"tool_calls,omitempty"`
		ToolName  string     `json:"tool_name,omitempty"`
	}
	toolCall struct {
		Function struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
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
		Model    string         `json:"model"`
		Messages []message      `json:"messages"`
		Stream   bool           `json:"stream"`
		Format   map[string]any `json:"format,omitempty"`
		Tools    []toolDef      `json:"tools,omitempty"`
		Options  map[string]any `json:"options"`
	}
	chatResponse struct {
		Model           string  `json:"model"`
		Message         message `json:"message"`
		DoneReason      string  `json:"done_reason"`
		PromptEvalCount int     `json:"prompt_eval_count"`
		EvalCount       int     `json:"eval_count"`
	}
)

// Check confirms the server answers.
func (b *Backend) Check(ctx context.Context) error {
	base := b.BaseURL(backend.Request{})
	_, err := b.client().Do(ctx, "GET", base+"/api/version", nil, nil)
	return mapErr(err, base, "")
}

// Models lists pulled models (GET /api/tags).
func (b *Backend) Models(ctx context.Context) ([]string, error) {
	return b.models(ctx, b.BaseURL(backend.Request{}))
}

func (b *Backend) models(ctx context.Context, base string) ([]string, error) {
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if _, err := b.client().Do(ctx, "GET", base+"/api/tags", nil, &out); err != nil {
		return nil, mapErr(err, base, "")
	}
	ids := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		ids = append(ids, m.Name)
	}
	return ids, nil
}

// CheckModel confirms the server runs and model is pulled ("qwen3" matches
// "qwen3:latest").
func (b *Backend) CheckModel(ctx context.Context, model string) error {
	if model == "" {
		return errors.New("ollama needs a model, e.g. qwen3-coder:30b")
	}
	if err := b.Check(ctx); err != nil {
		return err
	}
	ids, err := b.models(ctx, b.BaseURL(backend.Request{}))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == model || id == model+":latest" {
			return nil
		}
	}
	return fmt.Errorf("model %q is not pulled: run `ollama pull %s`", model, model)
}

// EstimateTokens is the spec's rough count: characters / 3.5.
func EstimateTokens(s string) int { return int(float64(len([]rune(s)))/3.5 + 0.5) }

// toolSchemaFallback is used when the tool definitions can't be built.
const toolSchemaFallback = 600

// minToolRoom is the fewest tokens a tool result may be clipped to; below it
// the window counts as full.
const minToolRoom = 64

// errContextFull ends a run whose tool results no longer fit num_ctx.
var errContextFull = errors.New("ollama: context window full (raise context_window)")

// noteFor is the repo-tools note the system prompt carries (toolloop.Note).
func noteFor(r backend.Request, tools bool) string {
	return toolloop.Note(r.RepoRead && r.Pass != backend.PassMemory, r.Cwd, tools)
}

func toolDefs(tools []toolloop.Tool) []toolDef {
	var defs []toolDef
	for _, t := range tools {
		var d toolDef
		d.Type = "function"
		d.Function.Name, d.Function.Description, d.Function.Parameters = t.Name, t.Description, t.Schema
		defs = append(defs, d)
	}
	return defs
}

func defsTokens(defs []toolDef) int {
	if len(defs) == 0 {
		return 0
	}
	raw, err := json.Marshal(defs)
	if err != nil {
		return toolSchemaFallback
	}
	return EstimateTokens(string(raw))
}

// promptBudget is what system + conversation may use: 90% of the window minus
// room for the reply.
func promptBudget(r backend.Request, window int) int {
	return window*9/10 - min(backend.MaxTokensFor(r), window/4)
}

// messageTokens estimates the conversation so far, tool-call arguments included.
func messageTokens(msgs []message) int {
	n := 0
	for _, m := range msgs {
		n += EstimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			n += EstimateTokens(tc.Function.Name) + EstimateTokens(string(tc.Function.Arguments))
		}
	}
	return n
}

// clipTokens keeps a prefix of s that fits in room tokens and marks the cut.
func clipTokens(s string, room int) string {
	const marker = "\n[clipped to fit num_ctx]"
	keep := int(float64(room-EstimateTokens(marker)-2) * 3.5)
	rs := []rune(s)
	if keep <= 0 || keep >= len(rs) {
		return s
	}
	return string(rs[:keep]) + marker
}

// FitPrompt shrinks r until system + user prompt (+ tools) fit in 90% of the
// window minus room for the reply: oldest turns first (keeping at least one),
// then the memory block, then the start of the last turn's text. trimmed
// reports whether anything was cut.
func FitPrompt(r backend.Request, window int, tools bool) (backend.Request, bool) {
	if window <= 0 {
		window = backend.DefaultContextWindow
	}
	budget := promptBudget(r, window)
	extra := EstimateTokens(noteFor(r, tools))
	if tools {
		d := defsTokens(toolDefs(toolloop.RepoTools(r.Cwd, toolloop.DefaultLimits())))
		if d == 0 {
			d = toolSchemaFallback
		}
		extra += d
	}
	size := func(x backend.Request) int {
		return EstimateTokens(backend.SystemPrompt(x)) + EstimateTokens(backend.UserPrompt(x)) + extra
	}
	trimmed := false
	r.Turns = append([]transcript.Turn(nil), r.Turns...)
	for size(r) > budget && len(r.Turns) > 1 {
		r.Turns, trimmed = r.Turns[1:], true
	}
	if size(r) > budget && r.Memory != "" {
		r.Memory, trimmed = "", true
	}
	if over := size(r) - budget; over > 0 && len(r.Turns) == 1 {
		t := r.Turns[0]
		cut := int(float64(over)*3.5) + 16
		t.Assistant = keepTail(t.Assistant, &cut)
		t.User = keepTail(t.User, &cut)
		r.Turns[0], trimmed = t, true
	}
	return r, trimmed
}

// keepTail drops up to *cut runes from the start of s, marking the cut.
func keepTail(s string, cut *int) string {
	rs := []rune(s)
	if *cut <= 0 || len(rs) == 0 {
		return s
	}
	n := min(*cut, len(rs))
	*cut -= n
	return "[…trimmed to fit num_ctx] " + string(rs[n:])
}

func (b *Backend) Run(ctx context.Context, r backend.Request) (backend.Result, error) {
	started := time.Now()
	if r.Model == "" {
		return backend.Result{}, errors.New("ollama needs a model, e.g. qwen3-coder:30b")
	}
	window := r.ContextWindow
	if window <= 0 {
		window = backend.DefaultContextWindow
	}
	var tools []toolloop.Tool
	lim := toolloop.WithDeny(b.o.Deny)
	if r.RepoRead && r.Cwd != "" && r.Pass != backend.PassMemory {
		tools = toolloop.RepoTools(r.Cwd, lim)
	}
	r, trimmed := FitPrompt(r, window, len(tools) > 0)
	if trimmed {
		b.logf("ollama: %s prompt trimmed to fit num_ctx %d", textutil.SanitizeModelText(r.DotName), window)
	}
	system := backend.SystemPrompt(r)
	if note := noteFor(r, len(tools) > 0); note != "" {
		system += "\n" + note + "\n"
	}
	base := b.BaseURL(r)
	cr := chatRequest{
		Model:    r.Model,
		Messages: []message{{Role: "system", Content: system}, {Role: "user", Content: backend.UserPrompt(r)}},
		Options:  map[string]any{"num_ctx": window, "num_predict": backend.MaxTokensFor(r)},
	}
	defs := toolDefs(tools)
	budget, defTokens := promptBudget(r, window), defsTokens(defs)
	schema := backend.VerdictSchema(r)
	c := b.client()
	call := func(ctx context.Context, withTools bool) (toolloop.Reply, error) {
		cr.Tools, cr.Format = nil, schema
		if withTools {
			// A grammar-constrained reply can't emit tool calls, so turns that
			// offer tools go without format; ParseReply checks the text and
			// Run re-asks once with format if it doesn't parse.
			cr.Tools, cr.Format = defs, nil
		}
		var resp chatResponse
		if _, err := c.Do(ctx, "POST", base+"/api/chat", cr, &resp); err != nil {
			return toolloop.Reply{}, mapErr(err, base, r.Model)
		}
		reply := toolloop.Reply{
			Text:  resp.Message.Content,
			Usage: pricing.Usage{In: resp.PromptEvalCount, Out: resp.EvalCount}, CostKnown: true, Model: r.Model,
		}
		if resp.DoneReason == "length" && len(resp.Message.ToolCalls) == 0 {
			// The reply was cut at num_predict; asking again would cut it again.
			return reply, errors.New("ollama: output truncated (raise max_tokens)")
		}
		for i, tc := range resp.Message.ToolCalls {
			reply.Calls = append(reply.Calls, toolloop.Call{ID: fmt.Sprintf("call_%d_%d", len(cr.Messages), i), Name: tc.Function.Name, Args: tc.Function.Arguments})
		}
		cr.Messages = append(cr.Messages, message{Role: "assistant", Content: resp.Message.Content, ToolCalls: resp.Message.ToolCalls})
		return reply, nil
	}
	turn := func(ctx context.Context, results []toolloop.Result, final bool) (toolloop.Reply, error) {
		used := messageTokens(cr.Messages) + defTokens
		for _, res := range results {
			content := res.Content
			if res.IsError {
				content = "ERROR: " + content
			}
			// Tool results grow the conversation; clip each to what is left of
			// num_ctx so Ollama never truncates the prompt silently.
			room := budget - used
			if room < minToolRoom {
				b.logf("ollama: %s context window %d full after %d tool results", textutil.SanitizeModelText(r.DotName), window, len(cr.Messages)-2)
				return toolloop.Reply{}, errContextFull
			}
			if EstimateTokens(content) > room {
				content = clipTokens(content, room)
			}
			used += EstimateTokens(content)
			cr.Messages = append(cr.Messages, message{Role: "tool", ToolName: res.Name, Content: content})
		}
		if final && len(results) > 0 {
			cr.Messages = append(cr.Messages, message{Role: "user", Content: toolloop.FinalNudge})
		}
		return call(ctx, !final && len(defs) > 0)
	}
	out, err := toolloop.Driver{Tools: tools, Limits: lim, Logf: b.o.Logf}.Run(ctx, turn)
	if err == nil && len(defs) > 0 && !backend.ValidReply(r, out.Text) {
		cr.Messages = append(cr.Messages, message{Role: "user", Content: "Reply now with only the JSON object."})
		var extra toolloop.Reply
		extra, err = call(ctx, false)
		out.Usage, out.Turns = out.Usage.Add(extra.Usage), out.Turns+1
		if err == nil {
			out.Text = extra.Text
		}
	}
	// A local run costs a known $0.
	run := backend.APIRun{Text: out.Text, Usage: out.Usage, CostUSD: 0, CostKnown: true,
		Model: r.Model, ToolCalls: out.Calls, Capped: out.Capped, Started: started}
	if err != nil {
		res := backend.PartialAPI(run)
		res.Raw = out.Text
		return res, err
	}
	return backend.FinishAPI(r, run)
}

func (b *Backend) logf(format string, args ...any) {
	if b.o.Logf != nil {
		b.o.Logf(format, args...)
	}
}

func mapErr(err error, base, model string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("ollama timed out (is the model loaded? first runs can be slow): %w", err)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("ollama cancelled: %w", err)
	}
	var se *httpx.StatusError
	if errors.As(err, &se) {
		if se.Status == 404 && model != "" {
			return fmt.Errorf("model %q is not pulled: run `ollama pull %s`", model, model)
		}
		return fmt.Errorf("ollama error: %s", textutil.SanitizeModelText(se.Error()))
	}
	if errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "connection refused") {
		return fmt.Errorf("Ollama isn't running at %s: start it with `ollama serve`", base)
	}
	return fmt.Errorf("ollama at %s: %s", base, textutil.SanitizeModelText(err.Error()))
}

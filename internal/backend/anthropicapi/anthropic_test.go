package anthropicapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/transcript"
)

const testKey = "sk-ant-api03-TESTKEY0123456789"

type staticCreds map[string]string

func (s staticCreds) Lookup(p string) (string, string, error) {
	if k, ok := s[p]; ok {
		return k, "file", nil
	}
	return "", "", fmt.Errorf("%w for %s", credentials.ErrNoCredentials, p)
}

// fake is an httptest Messages API. Each POST /v1/messages gets the next
// scripted reply; every request body is kept.
type fake struct {
	mu      sync.Mutex
	bodies  []map[string]any
	headers []http.Header
	replies []func(w http.ResponseWriter)
}

func (f *fake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/messages":
			raw, _ := io.ReadAll(r.Body)
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Errorf("bad body: %v", err)
			}
			f.bodies = append(f.bodies, m)
			f.headers = append(f.headers, r.Header.Clone())
			i := len(f.bodies) - 1
			if i >= len(f.replies) {
				t.Errorf("unexpected request %d", i)
				w.WriteHeader(500)
				return
			}
			f.replies[i](w)
		case r.Method == "GET" && r.URL.Path == "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"claude-opus-5-5","type":"model","display_name":"Claude Opus 5.5","created_at":"2026-01-01T00:00:00Z"},{"id":"claude-haiku-4-5","type":"model","display_name":"Haiku","created_at":"2026-01-01T00:00:00Z"}],"has_more":false,"first_id":"claude-opus-5-5","last_id":"claude-haiku-4-5"}`)
		case r.Method == "GET" && r.URL.Path == "/v1/models/claude-opus-5-5":
			fmt.Fprint(w, `{"id":"claude-opus-5-5","type":"model","display_name":"Claude Opus 5.5","created_at":"2026-01-01T00:00:00Z"}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"type":"error","error":{"type":"not_found_error","message":"not found"}}`)
		}
	})
}

func msg(model, stop string, content string, in, out, cr, cw int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		fmt.Fprintf(w, `{"id":"msg_1","type":"message","role":"assistant","model":%q,"content":%s,"stop_reason":%q,"stop_details":null,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}`,
			model, content, stop, in, out, cr, cw)
	}
}

func text(s string) string {
	b, _ := json.Marshal(s)
	return `[{"type":"text","text":` + string(b) + `}]`
}

const verdictPass = `{"relevant":false,"severity":"info","message":""}`

func newBackend(t *testing.T, f *fake) (*Backend, *[]string) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	var logs []string
	var mu sync.Mutex
	b := New(backend.Options{
		Creds:    staticCreds{"anthropic": testKey},
		Provider: config.ProviderConfig{BaseURL: srv.URL},
		Logf: func(format string, a ...any) {
			mu.Lock()
			logs = append(logs, fmt.Sprintf(format, a...))
			mu.Unlock()
		},
	})
	return b, &logs
}

func req() backend.Request {
	return backend.Request{DotName: "Security", Purpose: "Find auth bugs.", Verbosity: "normal",
		Turns: []transcript.Turn{{Index: 4, User: "add login", Assistant: "done"}}}
}

func TestRequestShape(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(verdictPass), 10, 5, 0, 0)}}
	b, _ := newBackend(t, f)
	r := req()
	r.Memory = "MEMORY (reference data)\n- [d:m1] team uses zod"
	res, err := b.Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	body := f.bodies[0]
	if body["model"] != DefaultModel || body["max_tokens"] != float64(4096) || body["fallbacks"] != "default" {
		t.Fatalf("model/max_tokens/fallbacks: %v %v %v", body["model"], body["max_tokens"], body["fallbacks"])
	}
	for _, banned := range []string{"thinking", "temperature", "top_p", "top_k", "tool_choice", "tools"} {
		if _, ok := body[banned]; ok {
			t.Errorf("request must not send %q", banned)
		}
	}
	oc := body["output_config"].(map[string]any)
	if _, ok := oc["effort"]; ok {
		t.Error("effort is omitted unless the dot sets it")
	}
	if oc["format"].(map[string]any)["type"] != "json_schema" {
		t.Errorf("format %v", oc["format"])
	}
	sys := body["system"].([]any)[0].(map[string]any)
	if sys["cache_control"] == nil || !strings.Contains(sys["text"].(string), "Find auth bugs.") {
		t.Errorf("system block %v", sys)
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" {
		t.Fatalf("one user message, no assistant prefill: %v", msgs)
	}
	blocks := msgs[0].(map[string]any)["content"].([]any)
	mem, conv := blocks[0].(map[string]any), blocks[1].(map[string]any)
	if mem["cache_control"] == nil || !strings.Contains(mem["text"].(string), "team uses zod") {
		t.Errorf("memory block %v", mem)
	}
	if conv["cache_control"] != nil || strings.Contains(conv["text"].(string), "team uses zod") || !strings.Contains(conv["text"].(string), "add login") {
		t.Errorf("conversation block %v", conv)
	}
	if got := f.headers[0].Get("anthropic-beta"); got != "server-side-fallback-2026-07-01" {
		t.Errorf("beta header %q", got)
	}
	if f.headers[0].Get("x-api-key") != testKey {
		t.Error("key header")
	}
	if res.Verdict.Relevant || res.InputTokens != 10 || res.OutputTokens != 5 || res.Model != DefaultModel {
		t.Fatalf("result %+v", res)
	}
}

// Recorded-style usage with prompt caching: input_tokens is only the uncached
// part; cache writes and reads are priced at their own rates.
func TestCachedUsageIsCountedAndPricedSeparately(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(verdictPass), 2, 61, 13894, 4512)}}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if res.InputTokens != 2+4512+13894 || res.CacheReadTokens != 13894 || res.CacheWriteTokens != 4512 || res.OutputTokens != 61 {
		t.Fatalf("usage %+v", res)
	}
	want := (2*4.0 + 4512*5.0 + 13894*0.20 + 61*20.0) / 1e6
	if !res.CostKnown || math.Abs(res.CostUSD-want) > 1e-12 {
		t.Fatalf("cost %v, want %v", res.CostUSD, want)
	}
}

func TestEffortModelAndFallbacksOff(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){msg("claude-haiku-4-5", "end_turn", text(verdictPass), 1, 1, 0, 0)}}
	b, _ := newBackend(t, f)
	r := req()
	r.Model, r.Effort, r.Fallbacks, r.MaxTokens = "claude-haiku-4-5", "low", "off", 2000
	if _, err := b.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	body := f.bodies[0]
	if body["model"] != "claude-haiku-4-5" || body["max_tokens"] != float64(2000) || body["output_config"].(map[string]any)["effort"] != "low" {
		t.Fatalf("body %v", body)
	}
	if _, ok := body["fallbacks"]; ok || f.headers[0].Get("anthropic-beta") != "" {
		t.Fatal("fallbacks off: no parameter, no beta header")
	}
}

func TestToolRoundTripThroughTheJail(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "auth.go"), []byte("package auth\nfunc Login() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	toolUse := `[{"type":"text","text":"Let me look."},{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"auth.go"}},{"type":"tool_use","id":"toolu_2","name":"read_file","input":{"path":"../../etc/passwd"}}]`
	f := &fake{replies: []func(http.ResponseWriter){
		msg("claude-opus-5-5", "tool_use", toolUse, 100, 20, 0, 50),
		msg("claude-opus-5-5", "end_turn", text(`{"relevant":true,"severity":"warn","message":"Login has no rate limit."}`), 200, 30, 50, 0),
	}}
	b, logs := newBackend(t, f)
	r := req()
	r.RepoRead, r.Cwd = true, root
	res, err := b.Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.bodies[0]["tools"].([]any)) != 3 {
		t.Fatalf("tools %v", f.bodies[0]["tools"])
	}
	if !strings.Contains(f.bodies[0]["system"].([]any)[0].(map[string]any)["text"].(string), "untrusted file contents") {
		t.Error("system prompt must carry the tool note")
	}
	second := f.bodies[1]["messages"].([]any)
	if len(second) != 3 {
		t.Fatalf("history user/assistant/user, got %d", len(second))
	}
	results := second[2].(map[string]any)["content"].([]any)
	ok := results[0].(map[string]any)
	bad := results[1].(map[string]any)
	if ok["tool_use_id"] != "toolu_1" || !strings.Contains(fmt.Sprint(ok["content"]), "func Login") || ok["is_error"] == true {
		t.Errorf("good result %v", ok)
	}
	if bad["is_error"] != true || !strings.Contains(fmt.Sprint(bad["content"]), "path outside project") {
		t.Errorf("escape result %v", bad)
	}
	if res.Verdict.Message != "Login has no rate limit." || res.ToolCalls != 2 || res.InputTokens != 400 || res.CacheReadTokens != 50 || res.CacheWriteTokens != 50 {
		t.Fatalf("result %+v", res)
	}
	// opus 5.5: (100*4 + 20*20 + 50*5 + 200*4 + 30*20 + 50*0.2)/1e6
	want := (100*4.0 + 20*20 + 50*5 + 200*4 + 30*20 + 50*0.2) / 1e6
	if !res.CostKnown || math.Abs(res.CostUSD-want) > 1e-12 {
		t.Fatalf("cost %v known %v, want %v", res.CostUSD, res.CostKnown, want)
	}
	found := false
	for _, l := range *logs {
		found = found || strings.Contains(l, "path outside project")
	}
	if !found {
		t.Errorf("jail escape must be logged: %v", *logs)
	}
}

func TestToolCapSendsToolChoiceNone(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("x\n"), 0o600)
	var calls []string
	for i := 0; i < 8; i++ {
		calls = append(calls, fmt.Sprintf(`{"type":"tool_use","id":"t%d","name":"read_file","input":{"path":"a.go"}}`, i))
	}
	f := &fake{replies: []func(http.ResponseWriter){
		msg("claude-opus-5-5", "tool_use", "["+strings.Join(calls, ",")+"]", 1, 1, 0, 0),
		msg("claude-opus-5-5", "end_turn", text(verdictPass), 1, 1, 0, 0),
	}}
	b, _ := newBackend(t, f)
	r := req()
	r.RepoRead, r.Cwd = true, root
	res, err := b.Run(context.Background(), r)
	if err != nil || !res.ToolsCapped || res.ToolCalls != 8 {
		t.Fatalf("%+v %v", res, err)
	}
	tc := f.bodies[1]["tool_choice"].(map[string]any)
	if tc["type"] != "none" || f.bodies[1]["tools"] == nil {
		t.Fatalf("final turn: tool_choice %v", tc)
	}
	last := f.bodies[1]["messages"].([]any)[2].(map[string]any)["content"].([]any)
	if !strings.Contains(fmt.Sprint(last[len(last)-1]), "Tool budget exhausted") {
		t.Fatal("final turn carries the nudge")
	}
}

func TestRefusalIsARunErrorWithCategory(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"x"},"usage":{"input_tokens":12,"output_tokens":0}}`)
	}}}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), req())
	if !errors.Is(err, backend.ErrRefused) || err.Error() != "refused (cyber)" {
		t.Fatalf("err = %v", err)
	}
	if res.InputTokens != 12 {
		t.Fatal("spend is kept on a refusal")
	}
}

func TestFallbackModelIsReportedAndPricedAsServed(t *testing.T) {
	content := `[{"type":"fallback","from":{"model":"claude-opus-5-5"},"to":{"model":"claude-opus-4-8"},"trigger":{"type":"refusal"}},{"type":"text","text":` + mustJSON(verdictPass) + `}]`
	f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-4-8", "end_turn", content, 1000, 100, 0, 0)}}
	b, logs := newBackend(t, f)
	res, err := b.Run(context.Background(), req())
	if err != nil || res.Model != "claude-opus-4-8" {
		t.Fatalf("%+v %v", res, err)
	}
	if want := (1000*5.0 + 100*25) / 1e6; math.Abs(res.CostUSD-want) > 1e-12 {
		t.Fatalf("cost %v want %v", res.CostUSD, want)
	}
	if len(*logs) == 0 || !strings.Contains((*logs)[0], "refusal fallback") {
		t.Fatalf("logs %v", *logs)
	}
}

func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestRateLimitIsRetried(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){
		func(w http.ResponseWriter) {
			w.Header().Set("retry-after-ms", "10")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
		},
		msg("claude-opus-5-5", "end_turn", text(verdictPass), 1, 1, 0, 0),
	}}
	b, _ := newBackend(t, f)
	if _, err := b.Run(context.Background(), req()); err != nil || len(f.bodies) != 2 {
		t.Fatalf("err %v requests %d", err, len(f.bodies))
	}
}

func TestUnauthorizedNeverEchoesTheKey(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		w.WriteHeader(401)
		fmt.Fprintf(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key %s"}}`, testKey)
	}}}
	b, _ := newBackend(t, f)
	_, err := b.Run(context.Background(), req())
	if err == nil || !strings.Contains(err.Error(), "key rejected") || strings.Contains(err.Error(), "TESTKEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestBadRequestBodyIsRedacted(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad header %s"}}`, testKey)
	}}}
	b, _ := newBackend(t, f)
	_, err := b.Run(context.Background(), req())
	if err == nil || strings.Contains(err.Error(), "TESTKEY") || !strings.Contains(err.Error(), "rejected the request (400)") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckModelsAndMissingKey(t *testing.T) {
	b, _ := newBackend(t, &fake{})
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckModel(context.Background(), backend.Request{Model: ""}); err != nil {
		t.Fatalf("default model: %v", err)
	}
	if err := b.CheckModel(context.Background(), backend.Request{Model: "claude-nope"}); err == nil || !strings.Contains(err.Error(), `model "claude-nope" not available`) {
		t.Fatalf("404: %v", err)
	}
	ids, err := b.Models(context.Background(), backend.Request{})
	if err != nil || len(ids) != 2 || ids[0] != "claude-opus-5-5" {
		t.Fatalf("models %v %v", ids, err)
	}
	none := New(backend.Options{Creds: staticCreds{}})
	if err := none.Check(context.Background()); !errors.Is(err, credentials.ErrNoCredentials) {
		t.Fatalf("no key: %v", err)
	}
	if _, err := New(backend.Options{}).Run(context.Background(), req()); !errors.Is(err, credentials.ErrNoCredentials) {
		t.Fatalf("nil creds: %v", err)
	}
}

func TestRegistered(t *testing.T) {
	b, err := backend.NewWith(backend.KindAnthropicAPI, backend.Options{})
	if err != nil || b.Kind() != backend.KindAnthropicAPI {
		t.Fatalf("%v %v", b, err)
	}
	if _, ok := b.(backend.ModelLister); !ok {
		t.Fatal("ModelLister")
	}
	if _, ok := b.(backend.ModelChecker); !ok {
		t.Fatal("ModelChecker")
	}
}

// A pane whose cwd is too broad for tools (home or "/") gets the no-tools
// note instead of the tool note, and no tools are declared.
func TestBroadCwdGetsNoToolsNote(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(verdictPass), 1, 1, 0, 0)}}
	b, _ := newBackend(t, f)
	r := req()
	r.RepoRead, r.Cwd = true, "/"
	if _, err := b.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	sys := f.bodies[0]["system"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(sys, "REPO TOOLS: unavailable") || strings.Contains(sys, "untrusted file contents") {
		t.Errorf("system prompt %q", sys)
	}
	if _, ok := f.bodies[0]["tools"]; ok {
		t.Error("no tools may be declared")
	}
}

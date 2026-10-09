package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const testKey = "sk-or-v1-TESTKEY0123456789abcdef"

type staticCreds map[string]string

func (s staticCreds) Lookup(p string) (string, string, error) {
	if k, ok := s[p]; ok {
		return k, "file", nil
	}
	return "", "", fmt.Errorf("%w for %s", credentials.ErrNoCredentials, p)
}

type fake struct {
	mu      sync.Mutex
	bodies  []map[string]any
	headers []http.Header
	replies []func(w http.ResponseWriter)
}

func (f *fake) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/chat/completions":
			raw, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			f.bodies = append(f.bodies, m)
			f.headers = append(f.headers, r.Header.Clone())
			i := len(f.bodies) - 1
			if i >= len(f.replies) {
				t.Errorf("unexpected request %d", i)
				w.WriteHeader(500)
				return
			}
			f.replies[i](w)
		case "/api/v1/key":
			if r.Header.Get("Authorization") != "Bearer "+testKey {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"data":{"label":"x"}}`)
		case "/api/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"anthropic/claude-opus-5-5"},{"id":"qwen/qwen3-coder"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chat(model, content string, toolCalls string, cost string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		c, _ := json.Marshal(content)
		if toolCalls == "" {
			toolCalls = "null"
		}
		usage := `"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":200}`
		if cost != "" {
			usage += `,"cost":` + cost
		}
		fmt.Fprintf(w, `{"id":"gen-1","model":%q,"choices":[{"message":{"role":"assistant","content":%s,"tool_calls":%s},"finish_reason":"stop"}],"usage":{%s}}`, model, c, toolCalls, usage)
	}
}

func newBackend(t *testing.T, f *fake, headers map[string]string) *Backend {
	srv := f.server(t)
	return New(backend.Options{Creds: staticCreds{"openrouter": testKey},
		Provider: config.ProviderConfig{BaseURL: srv.URL + "/api/v1", ExtraHeaders: headers}})
}

func req() backend.Request {
	return backend.Request{DotName: "Sec", Purpose: "p", Verbosity: "terse", Model: "anthropic/claude-opus-5-5",
		Turns: []transcript.Turn{{Index: 1, User: "u", Assistant: "a"}}}
}

func TestRequestShapeCostAndHeaders(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){chat("anthropic/claude-opus-5-5", `{"relevant":false,"severity":"info","message":""}`, "", "0.0123")}}
	res, err := newBackend(t, f, map[string]string{"HTTP-Referer": "https://example.test"}).Run(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	body := f.bodies[0]
	rf := body["response_format"].(map[string]any)
	if rf["type"] != "json_schema" || rf["json_schema"].(map[string]any)["strict"] != true {
		t.Fatalf("response_format %v", rf)
	}
	msgs := body["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["role"] != "user" || body["max_tokens"] != float64(4096) {
		t.Fatalf("messages %v", msgs)
	}
	h := f.headers[0]
	if h.Get("Authorization") != "Bearer "+testKey || h.Get("X-Title") != "Herdr Dots" || h.Get("HTTP-Referer") != "https://example.test" {
		t.Fatalf("headers %v", h)
	}
	if !res.CostKnown || res.CostUSD != 0.0123 || res.InputTokens != 1000 || res.CacheReadTokens != 200 {
		t.Fatalf("result %+v", res)
	}
}

func TestMissingCostIsUnknown(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){chat("m", `{"relevant":false,"severity":"info","message":""}`, "", "")}}
	res, err := newBackend(t, f, nil).Run(context.Background(), req())
	if err != nil || res.CostKnown {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSchemaRejectionFallsBackToPromptOnlyOnce(t *testing.T) {
	reject := func(w http.ResponseWriter) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"Provider does not support response_format json_schema"}}`)
	}
	ok := chat("m", `{"relevant":false,"severity":"info","message":""}`, "", "0.001")
	f := &fake{replies: []func(http.ResponseWriter){reject, ok, ok}}
	b := newBackend(t, f, nil)
	if _, err := b.Run(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if _, has := f.bodies[1]["response_format"]; has {
		t.Fatal("retry must drop response_format")
	}
	if _, err := b.Run(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if _, has := f.bodies[2]["response_format"]; has || len(f.bodies) != 3 {
		t.Fatal("the model is remembered as schema-less")
	}
}

func TestToolRoundTripAndFinalChoiceNone(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o600)
	var calls []string
	for i := 0; i < 8; i++ {
		calls = append(calls, fmt.Sprintf(`{"id":"call_%d","type":"function","function":{"name":"list_files","arguments":"{}"}}`, i))
	}
	f := &fake{replies: []func(http.ResponseWriter){
		chat("m", "", "["+strings.Join(calls, ",")+"]", "0.001"),
		chat("m", `{"relevant":false,"severity":"info","message":""}`, "", "0.002"),
	}}
	r := req()
	r.RepoRead, r.Cwd = true, root
	res, err := newBackend(t, f, nil).Run(context.Background(), r)
	if err != nil || res.ToolCalls != 8 || !res.ToolsCapped || res.CostUSD != 0.003 {
		t.Fatalf("%+v %v", res, err)
	}
	second := f.bodies[1]
	if second["tool_choice"] != "none" {
		t.Fatalf("tool_choice %v", second["tool_choice"])
	}
	msgs := second["messages"].([]any)
	if msgs[2].(map[string]any)["role"] != "assistant" || msgs[3].(map[string]any)["role"] != "tool" || msgs[3].(map[string]any)["tool_call_id"] != "call_0" {
		t.Fatalf("history %v", msgs)
	}
	if !strings.Contains(fmt.Sprint(msgs[3].(map[string]any)["content"]), "a.go") {
		t.Fatalf("tool output %v", msgs[3])
	}
}

func TestErrorsAreActionableAndRedacted(t *testing.T) {
	for code, want := range map[int]string{401: "key rejected", 402: "credits exhausted", 404: "not available"} {
		code := code
		f := &fake{replies: []func(http.ResponseWriter){func(w http.ResponseWriter) {
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"error":{"message":"bad key %s"}}`, testKey)
		}}}
		_, err := newBackend(t, f, nil).Run(context.Background(), req())
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "TESTKEY") {
			t.Errorf("%d: %v", code, err)
		}
	}
}

func TestCheckAndModels(t *testing.T) {
	b := newBackend(t, &fake{}, nil)
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckModel(context.Background(), backend.Request{Model: "qwen/qwen3-coder"}); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckModel(context.Background(), backend.Request{Model: "nope/x"}); err == nil {
		t.Fatal("unknown model")
	}
	bad := New(backend.Options{Creds: staticCreds{"openrouter": "sk-or-v1-wrongwrongwrong"}, Provider: b.o.Provider})
	if err := bad.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "key rejected") {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := New(backend.Options{}).Run(context.Background(), req()); !errors.Is(err, credentials.ErrNoCredentials) {
		t.Fatal("no creds")
	}
}

// Final review item 2: CheckModel and Models use the dot's base_url.
func TestCheckModelAndModelsUseTheDotsBaseURL(t *testing.T) {
	srv := (&fake{}).server(t)
	b := New(backend.Options{Creds: staticCreds{"openrouter": testKey}, Provider: config.ProviderConfig{BaseURL: "http://127.0.0.1:1/api/v1"}})
	r := backend.Request{Model: "qwen/qwen3-coder", BaseURL: srv.URL + "/api/v1"}
	if err := b.CheckModel(context.Background(), r); err != nil {
		t.Fatalf("dot base_url: %v", err)
	}
	if ids, err := b.Models(context.Background(), r); err != nil || len(ids) != 2 {
		t.Fatalf("models %v %v", ids, err)
	}
}

// Carry-in from Task 7: a pane cwd that is too broad for repo tools gets the
// NoToolsNote, offers no tools, and never claims it can read the repo.
func TestTooBroadCwdGetsNoToolsNote(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){chat("m", `{"relevant":false,"severity":"info","message":""}`, "", "0.001")}}
	r := req()
	r.RepoRead, r.Cwd = true, "/"
	if _, err := newBackend(t, f, nil).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	body := f.bodies[0]
	if _, has := body["tools"]; has {
		t.Fatalf("tools offered: %v", body["tools"])
	}
	sys := body["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "REPO TOOLS: unavailable") || strings.Contains(sys, "you may call read_file") {
		t.Fatalf("system prompt %q", sys)
	}
}

func TestMemoryStaysInTheUserMessage(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){chat("m", `{"relevant":false,"severity":"info","message":""}`, "", "0.001")}}
	r := req()
	r.Memory = "SECRET-MEMORY-MARKER"
	if _, err := newBackend(t, f, nil).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	msgs := f.bodies[0]["messages"].([]any)
	if strings.Contains(msgs[0].(map[string]any)["content"].(string), "SECRET-MEMORY-MARKER") ||
		!strings.Contains(msgs[1].(map[string]any)["content"].(string), "SECRET-MEMORY-MARKER") {
		t.Fatalf("memory placement %v", msgs)
	}
}

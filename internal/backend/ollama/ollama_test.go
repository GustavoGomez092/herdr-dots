package ollama

import (
	"context"
	"encoding/json"
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
	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/config"
	"herdr-dots/internal/transcript"
)

type fake struct {
	mu      sync.Mutex
	bodies  []map[string]any
	replies []string
}

func (f *fake) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/api/chat":
			raw, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			f.bodies = append(f.bodies, m)
			i := len(f.bodies) - 1
			if i >= len(f.replies) {
				t.Errorf("unexpected request %d", i)
				w.WriteHeader(500)
				return
			}
			fmt.Fprint(w, f.replies[i])
		case "/api/version":
			fmt.Fprint(w, `{"version":"0.12.0"}`)
		case "/api/tags":
			fmt.Fprint(w, `{"models":[{"name":"qwen3-coder:30b"},{"name":"llama3:latest"}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chat(content, toolCalls string) string {
	c, _ := json.Marshal(content)
	if toolCalls == "" {
		toolCalls = "[]"
	}
	return fmt.Sprintf(`{"model":"qwen3-coder:30b","message":{"role":"assistant","content":%s,"tool_calls":%s},"done":true,"done_reason":"stop","prompt_eval_count":321,"eval_count":45}`, c, toolCalls)
}

const pass = `{"relevant":false,"severity":"info","message":""}`

func newBackend(t *testing.T, f *fake) (*Backend, *[]string) {
	srv := f.server(t)
	var logs []string
	b := New(backend.Options{Provider: config.ProviderConfig{BaseURL: srv.URL}, Logf: func(s string, a ...any) { logs = append(logs, fmt.Sprintf(s, a...)) }})
	b.getenv = func(string) string { return "" }
	return b, &logs
}

func req() backend.Request {
	return backend.Request{DotName: "Local", Purpose: "p", Verbosity: "normal", Model: "qwen3-coder:30b",
		Turns: []transcript.Turn{{Index: 1, User: "u", Assistant: "a"}}}
}

func TestRequestShapeNoCost(t *testing.T) {
	f := &fake{replies: []string{chat(pass, "")}}
	b, _ := newBackend(t, f)
	r := req()
	r.ContextWindow = 32768
	res, err := b.Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	body := f.bodies[0]
	if body["stream"] != false || body["format"] == nil || body["options"].(map[string]any)["num_ctx"] != float64(32768) {
		t.Fatalf("body %v", body)
	}
	if _, has := body["tools"]; has {
		t.Fatal("no tools without repo_read")
	}
	if res.CostUSD != 0 || res.CostKnown || res.InputTokens != 321 || res.OutputTokens != 45 {
		t.Fatalf("ollama has no price: cost must be unknown, never $0 known: %+v", res)
	}
}

func TestDefaultContextWindowIsAlwaysSent(t *testing.T) {
	f := &fake{replies: []string{chat(pass, "")}}
	b, _ := newBackend(t, f)
	if _, err := b.Run(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if f.bodies[0]["options"].(map[string]any)["num_ctx"] != float64(backend.DefaultContextWindow) {
		t.Fatal("num_ctx must be explicit")
	}
}

func TestToolTurnsOmitFormatAndInvalidTextIsReAsked(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o600)
	f := &fake{replies: []string{
		chat("", `[{"function":{"name":"read_file","arguments":{"path":"a.go"}}}]`),
		chat("Looks fine to me.", ""), // prose: not a verdict
		chat(pass, ""),
	}}
	b, _ := newBackend(t, f)
	r := req()
	r.RepoRead, r.Cwd = true, root
	res, err := b.Run(context.Background(), r)
	if err != nil || res.Verdict.Relevant || res.ToolCalls != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if f.bodies[0]["format"] != nil || f.bodies[0]["tools"] == nil {
		t.Fatal("tool turns: tools, no format")
	}
	msgs := f.bodies[1]["messages"].([]any)
	tool := msgs[3].(map[string]any)
	if tool["role"] != "tool" || tool["tool_name"] != "read_file" || !strings.Contains(tool["content"].(string), "package a") {
		t.Fatalf("tool message %v", tool)
	}
	if f.bodies[2]["format"] == nil || f.bodies[2]["tools"] != nil {
		t.Fatal("the re-ask uses format and no tools")
	}
	if res.InputTokens != 3*321 {
		t.Fatalf("usage sums every call: %d", res.InputTokens)
	}
}

func TestOllamaDownAndModelMissingHints(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	down := srv.URL
	srv.Close()
	b := New(backend.Options{Provider: config.ProviderConfig{BaseURL: down}})
	if err := b.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "ollama serve") {
		t.Fatalf("down: %v", err)
	}
	up, _ := newBackend(t, &fake{})
	if err := up.CheckModel(context.Background(), "mistral"); err == nil || !strings.Contains(err.Error(), "ollama pull mistral") {
		t.Fatalf("missing: %v", err)
	}
	if err := up.CheckModel(context.Background(), "llama3"); err != nil {
		t.Fatalf("llama3 matches llama3:latest: %v", err)
	}
	if ids, err := up.Models(context.Background()); err != nil || len(ids) != 2 {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestBaseURLPrecedenceAndOllamaHost(t *testing.T) {
	b := New(backend.Options{})
	b.getenv = func(string) string { return "0.0.0.0:11500" }
	if got := b.BaseURL(backend.Request{}); got != "http://127.0.0.1:11500" {
		t.Fatalf("OLLAMA_HOST: %s", got)
	}
	b.getenv = func(string) string { return "" }
	if got := b.BaseURL(backend.Request{}); got != DefaultBaseURL {
		t.Fatalf("default: %s", got)
	}
	b.o.Provider.BaseURL = "http://gpu-box:11434/"
	if got := b.BaseURL(backend.Request{}); got != "http://gpu-box:11434" {
		t.Fatalf("provider: %s", got)
	}
	if got := b.BaseURL(backend.Request{BaseURL: "http://dot:1"}); got != "http://dot:1" {
		t.Fatalf("dot: %s", got)
	}
	for in, want := range map[string]string{"example.com": "http://example.com:11434", "https://h:9": "https://h:9", "[::]:7": "http://127.0.0.1:7"} {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFitPromptDropsOldestTurnsThenMemory(t *testing.T) {
	big := strings.Repeat("x", 7000) // ~2000 tokens each
	var turns []transcript.Turn
	for i := 1; i <= 20; i++ {
		turns = append(turns, transcript.Turn{Index: i, User: big, Assistant: "ok"})
	}
	r := backend.Request{DotName: "d", Purpose: "p", Model: "m", Turns: turns, Memory: strings.Repeat("m", 3500)}
	got, trimmed := FitPrompt(r, 16384, false)
	if !trimmed || len(got.Turns) >= 20 || got.Turns[len(got.Turns)-1].Index != 20 || got.Memory == "" {
		t.Fatalf("turns kept %d, last %d, memory kept %v", len(got.Turns), got.Turns[len(got.Turns)-1].Index, got.Memory != "")
	}
	budget := 16384*9/10 - 4096
	if n := EstimateTokens(backend.SystemPrompt(got)) + EstimateTokens(backend.UserPrompt(got)); n > budget {
		t.Fatalf("still over: %d > %d", n, budget)
	}
	small, _ := FitPrompt(r, 2048, false)
	if small.Memory != "" || len(small.Turns) != 1 || !strings.Contains(small.Turns[0].User, "trimmed to fit") {
		t.Fatalf("tiny window: memory %q turns %d", small.Memory[:min(10, len(small.Memory))], len(small.Turns))
	}
	if len(r.Turns) != 20 {
		t.Fatal("FitPrompt must not modify the caller's slice")
	}
	if _, trimmed := FitPrompt(req(), 16384, true); trimmed {
		t.Fatal("a small prompt is untouched")
	}
}

func TestTrimIsLogged(t *testing.T) {
	f := &fake{replies: []string{chat(pass, "")}}
	b, logs := newBackend(t, f)
	r := req()
	r.ContextWindow = 2048
	r.Turns[0].User = strings.Repeat("y", 20000)
	if _, err := b.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(*logs) == 0 || !strings.Contains((*logs)[0], "trimmed to fit num_ctx 2048") {
		t.Fatalf("logs %v", *logs)
	}
}

func systemOf(f *fake, i int) string {
	return f.bodies[i]["messages"].([]any)[0].(map[string]any)["content"].(string)
}

func TestBroadCwdGetsNoToolsNote(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	for name, cwd := range map[string]string{"home": home, "root": "/"} {
		f := &fake{replies: []string{chat(pass, "")}}
		b, _ := newBackend(t, f)
		r := req()
		r.RepoRead, r.Cwd = true, cwd
		if _, err := b.Run(context.Background(), r); err != nil {
			t.Fatal(name, err)
		}
		if _, has := f.bodies[0]["tools"]; has {
			t.Errorf("%s: no tools for a broad cwd", name)
		}
		sys := systemOf(f, 0)
		if !strings.Contains(sys, toolloop.NoToolsNote) || strings.Contains(sys, toolloop.SystemNote) {
			t.Errorf("%s: system prompt must carry NoToolsNote only", name)
		}
		if f.bodies[0]["format"] == nil {
			t.Errorf("%s: toolless run uses format", name)
		}
	}
}

func TestToolsNoteOnlyWhenToolsOffered(t *testing.T) {
	f := &fake{replies: []string{chat(pass, "")}}
	b, _ := newBackend(t, f)
	r := req()
	r.RepoRead, r.Cwd = true, t.TempDir()
	if _, err := b.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if sys := systemOf(f, 0); !strings.Contains(sys, toolloop.SystemNote) || strings.Contains(sys, toolloop.NoToolsNote) {
		t.Fatal("system prompt must carry SystemNote only")
	}
	f2 := &fake{replies: []string{chat(pass, "")}}
	b2, _ := newBackend(t, f2)
	if _, err := b2.Run(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if sys := systemOf(f2, 0); strings.Contains(sys, "REPO TOOLS") {
		t.Fatal("no repo note without repo_read")
	}
}

func TestMemoryIsOnlyInTheUserMessage(t *testing.T) {
	f := &fake{replies: []string{chat(pass, "")}}
	b, _ := newBackend(t, f)
	r := req()
	r.Memory = "MEMORY-MARKER-42"
	if _, err := b.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	msgs := f.bodies[0]["messages"].([]any)
	if strings.Contains(msgs[0].(map[string]any)["content"].(string), "MEMORY-MARKER-42") {
		t.Fatal("memory leaked into the system prompt")
	}
	if !strings.Contains(msgs[1].(map[string]any)["content"].(string), "MEMORY-MARKER-42") {
		t.Fatal("memory missing from the user message")
	}
}

func TestServerErrorTextIsSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, "bad \x1b]52;c;ZXZpbA==\x07request\x1b[31m")
	}))
	t.Cleanup(srv.Close)
	b := New(backend.Options{Provider: config.ProviderConfig{BaseURL: srv.URL}})
	_, err := b.Run(context.Background(), req())
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) || strings.ContainsRune(err.Error(), 7) {
		t.Fatalf("error must be sanitized: %q", err)
	}
}

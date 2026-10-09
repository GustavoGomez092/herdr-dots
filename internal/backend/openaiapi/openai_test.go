package openaiapi

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

const testKey = "sk-proj-TESTKEY0123456789abcd"

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
	replies []string // raw JSON bodies, or "STATUS:<code>:<body>"
}

func (f *fake) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/responses":
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
			rep := f.replies[i]
			if strings.HasPrefix(rep, "STATUS:") {
				var code int
				var body string
				parts := strings.SplitN(rep, ":", 3)
				fmt.Sscan(parts[1], &code)
				body = parts[2]
				w.WriteHeader(code)
				fmt.Fprint(w, body)
				return
			}
			fmt.Fprint(w, rep)
		case r.URL.Path == "/v1/models":
			fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-6-sol","object":"model","created":1,"owned_by":"openai"}]}`)
		case r.URL.Path == "/v1/models/gpt-6-sol":
			fmt.Fprint(w, `{"id":"gpt-6-sol","object":"model","created":1,"owned_by":"openai"}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"error":{"message":"not found","type":"invalid_request_error"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func response(model, outputJSON string, in, cached, out int) string {
	return fmt.Sprintf(`{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":%q,"output":%s,
	"usage":{"input_tokens":%d,"input_tokens_details":{"cached_tokens":%d,"cache_write_tokens":0},"output_tokens":%d,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":0},
	"parallel_tool_calls":true,"tool_choice":"auto","tools":[],"temperature":null,"top_p":null,"error":null,"incomplete_details":null,"instructions":null,"metadata":{}}`,
		model, outputJSON, in, cached, out)
}

func textOut(s string) string {
	b, _ := json.Marshal(s)
	return `[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":` + string(b) + `,"annotations":[]}]}]`
}

func newBackend(t *testing.T, f *fake) *Backend {
	srv := f.server(t)
	return New(backend.Options{Creds: staticCreds{"openai": testKey}, Provider: config.ProviderConfig{BaseURL: srv.URL + "/v1"}})
}

func req() backend.Request {
	return backend.Request{DotName: "Perf", Purpose: "Find slow code.", Verbosity: "normal", Model: "gpt-6-sol",
		Turns: []transcript.Turn{{Index: 2, User: "add cache", Assistant: "ok"}}}
}

func TestRequestShapeAndCachedTokenPricing(t *testing.T) {
	f := &fake{replies: []string{response("gpt-6-sol", textOut(`{"relevant":false,"severity":"info","message":""}`), 1000, 400, 50)}}
	b := newBackend(t, f)
	r := req()
	r.Effort = "high"
	res, err := b.Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	body := f.bodies[0]
	if body["model"] != "gpt-6-sol" || body["store"] != false || body["max_output_tokens"] != float64(4096) {
		t.Fatalf("body %v", body)
	}
	if body["reasoning"].(map[string]any)["effort"] != "high" {
		t.Errorf("reasoning %v", body["reasoning"])
	}
	format := body["text"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["strict"] != true || format["schema"] == nil {
		t.Errorf("format %v", format)
	}
	if !strings.Contains(body["instructions"].(string), "Find slow code.") {
		t.Error("system prompt goes in instructions")
	}
	for _, banned := range []string{"temperature", "top_p", "previous_response_id", "tools"} {
		if _, ok := body[banned]; ok {
			t.Errorf("must not send %s", banned)
		}
	}
	// 600 uncached * 2 + 400 cached * 0.2 + 50 * 10
	want := (600*2.0 + 400*0.2 + 50*10) / 1e6
	if res.InputTokens != 1000 || res.CacheReadTokens != 400 || !res.CostKnown || math.Abs(res.CostUSD-want) > 1e-12 {
		t.Fatalf("result %+v want cost %v", res, want)
	}
}

func TestFunctionToolRoundTripKeepsReasoningItems(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "db.go"), []byte("package db\n// N+1 query here\n"), 0o600)
	first := `[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"ENC"},
	 {"type":"function_call","id":"fc_1","call_id":"call_1","name":"grep","arguments":"{\"pattern\":\"N\\\\+1\",\"glob\":null}","status":"completed"}]`
	f := &fake{replies: []string{
		response("gpt-6-sol", first, 100, 0, 10),
		response("gpt-6-sol", textOut(`{"relevant":true,"severity":"warn","message":"N+1 query in db.go."}`), 200, 100, 20),
	}}
	b := newBackend(t, f)
	r := req()
	r.RepoRead, r.Cwd = true, root
	res, err := b.Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	tools := f.bodies[0]["tools"].([]any)
	if len(tools) != 3 || tools[0].(map[string]any)["strict"] != true {
		t.Fatalf("tools %v", tools)
	}
	in := f.bodies[1]["input"].([]any)
	types := []string{}
	for _, it := range in {
		m := it.(map[string]any)
		ty, _ := m["type"].(string)
		if ty == "" {
			ty = "message"
		}
		types = append(types, ty)
	}
	if strings.Join(types, ",") != "message,reasoning,function_call,function_call_output" {
		t.Fatalf("second request input types %v", types)
	}
	outItem := in[3].(map[string]any)
	if outItem["call_id"] != "call_1" || !strings.Contains(fmt.Sprint(outItem["output"]), "db.go:2:") {
		t.Fatalf("function output %v", outItem)
	}
	if in[1].(map[string]any)["encrypted_content"] != "ENC" {
		t.Fatal("encrypted reasoning must be sent back")
	}
	if f.bodies[0]["include"].([]any)[0] != "reasoning.encrypted_content" {
		t.Fatal("include encrypted reasoning")
	}
	if res.Verdict.Message != "N+1 query in db.go." || res.ToolCalls != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestRefusalItemIsARunError(t *testing.T) {
	out := `[{"type":"message","id":"m","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"I can't help with that."}]}]`
	f := &fake{replies: []string{response("gpt-6-sol", out, 10, 0, 3)}}
	_, err := newBackend(t, f).Run(context.Background(), req())
	if !errors.Is(err, backend.ErrRefused) || !strings.Contains(err.Error(), "can't help") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownModelPriceIsUnknownNotZero(t *testing.T) {
	f := &fake{replies: []string{response("gpt-unlisted", textOut(`{"relevant":false,"severity":"info","message":""}`), 10, 0, 3)}}
	r := req()
	r.Model = "gpt-unlisted"
	res, err := newBackend(t, f).Run(context.Background(), r)
	if err != nil || res.CostKnown || res.CostUSD != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestMissingModelAndBadKey(t *testing.T) {
	r := req()
	r.Model = ""
	if _, err := newBackend(t, &fake{}).Run(context.Background(), r); err == nil || !strings.Contains(err.Error(), "needs a model") {
		t.Fatalf("err = %v", err)
	}
	f := &fake{replies: []string{`STATUS:401:{"error":{"message":"Incorrect API key provided: ` + testKey + `","type":"invalid_request_error"}}`}}
	_, err := newBackend(t, f).Run(context.Background(), req())
	if err == nil || !strings.Contains(err.Error(), "key rejected") || strings.Contains(err.Error(), "TESTKEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckAndModels(t *testing.T) {
	b := newBackend(t, &fake{})
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckModel(context.Background(), backend.Request{Model: "gpt-6-sol"}); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckModel(context.Background(), backend.Request{Model: "gpt-nope"}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("err = %v", err)
	}
	if ids, err := b.Models(context.Background(), backend.Request{}); err != nil || len(ids) != 1 {
		t.Fatalf("%v %v", ids, err)
	}
	if _, err := backend.NewWith(backend.KindOpenAIAPI, backend.Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestBroadCwdGetsNoToolsNote(t *testing.T) {
	f := &fake{replies: []string{response("gpt-6-sol", textOut(`{"relevant":false,"severity":"info","message":""}`), 10, 0, 3)}}
	r := req()
	r.RepoRead, r.Cwd = true, "/"
	if _, err := newBackend(t, f).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	ins := f.bodies[0]["instructions"].(string)
	if !strings.Contains(ins, "unavailable for this pane") || strings.Contains(ins, "you may call read_file") {
		t.Errorf("instructions %q", ins)
	}
	if _, ok := f.bodies[0]["tools"]; ok {
		t.Error("no tools expected")
	}
}

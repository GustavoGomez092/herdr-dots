package anthropicapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// searchMsg is a Messages API reply whose usage reports server web searches.
func searchMsg(stop, content string, searches int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		fmt.Fprintf(w, `{"id":"msg_w","type":"message","role":"assistant","model":"claude-opus-5-5","content":%s,"stop_reason":%q,"stop_details":null,"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"server_tool_use":{"web_search_requests":%d,"web_fetch_requests":0}}}`,
			content, stop, searches)
	}
}

// pausedContent is a turn the server paused mid web search.
const pausedContent = `[{"type":"server_tool_use","id":"srvtoolu_01","name":"web_search","input":{"query":"lodash 4.17.20 advisory"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_01","content":[{"type":"web_search_result","url":"https://example.com/advisory","title":"Advisory","encrypted_content":"e30=","page_age":null}]}]`

func webReq() backend.Request {
	r := req()
	r.Pass, r.Web = backend.PassResearch, true
	return r
}

func toolTypes(body map[string]any) []string {
	var out []string
	tools, _ := body["tools"].([]any)
	for _, tl := range tools {
		out = append(out, fmt.Sprint(tl.(map[string]any)["type"]))
	}
	return out
}

func TestWebResearchDeclaresServerToolsWithoutStructuredOutput(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){searchMsg("end_turn", text(verdictPass), 1)}}
	b, _ := newBackend(t, f)
	if _, err := b.Run(context.Background(), webReq()); err != nil {
		t.Fatal(err)
	}
	body := f.bodies[0]
	types := strings.Join(toolTypes(body), ",")
	if !strings.Contains(types, "web_search_20260209") || !strings.Contains(types, "web_fetch_20260209") || strings.Contains(types, "code_execution") {
		t.Fatalf("tools %s", types)
	}
	if oc, ok := body["output_config"].(map[string]any); ok {
		if _, has := oc["format"]; has {
			t.Fatal("web results carry citations: no structured output on a web request")
		}
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatal("a web-only first turn must keep its tools (no tool_choice)")
	}
	if !strings.Contains(body["system"].([]any)[0].(map[string]any)["text"].(string), "untrusted reference data") {
		t.Fatal("the research prompt warns about web content")
	}
}

func TestOnlyWebResearchGetsWebTools(t *testing.T) {
	with := func(f func(*backend.Request)) backend.Request { x := req(); x.Web = true; f(&x); return x }
	for name, r := range map[string]backend.Request{
		"review with Web set":    with(func(x *backend.Request) {}),
		"research without web":   func() backend.Request { x := req(); x.Pass = backend.PassResearch; return x }(),
		"memory with Web set":    with(func(x *backend.Request) { x.Pass = backend.PassMemory }),
		"write pass with Web":    with(func(x *backend.Request) { x.Pass, x.Write = backend.PassWrite, true }),
		"Write flag with Web":    with(func(x *backend.Request) { x.Write = true }),
		"ask pass with Web":      with(func(x *backend.Request) { x.Pass = "ask" }),
		"draft pass with Web":    with(func(x *backend.Request) { x.Pass = "draft" }),
		"council pass with Web":  with(func(x *backend.Request) { x.Pass = "council" }),
		"research + Write + Web": with(func(x *backend.Request) { x.Pass, x.Write = backend.PassResearch, true }),
	} {
		f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(verdictPass), 10, 5, 0, 0)}}
		b, _ := newBackend(t, f)
		_, _ = b.Run(context.Background(), r) // a refused request is fine: nothing may be sent with web tools
		for _, body := range f.bodies {
			for _, ty := range toolTypes(body) {
				if strings.Contains(ty, "web_") {
					t.Errorf("%s: web tool %s declared", name, ty)
				}
			}
		}
		if name == "review with Web set" || name == "research without web" {
			if len(f.bodies) == 0 {
				t.Fatalf("%s: no request", name)
			}
			if oc := f.bodies[0]["output_config"].(map[string]any); oc["format"] == nil {
				t.Errorf("%s: structured output stays on", name)
			}
		}
	}
}

func TestPauseTurnIsResumedAndSearchesArePriced(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){
		searchMsg("pause_turn", pausedContent, 2),
		searchMsg("end_turn", text(`{"relevant":true,"severity":"warn","message":"lodash 4.17.20 has a prototype pollution advisory."}`), 1),
	}}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), webReq())
	if err != nil || !res.Verdict.Relevant || res.Verdict.Severity != "warn" {
		t.Fatalf("%+v %v", res, err)
	}
	if len(f.bodies) != 2 {
		t.Fatalf("%d requests, want 2 (one resume)", len(f.bodies))
	}
	msgs := f.bodies[1]["messages"].([]any)
	if len(msgs) != 2 || msgs[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("the resume re-sends the paused assistant turn and adds no user message: %v", msgs)
	}
	if res.InputTokens != 200 || res.OutputTokens != 40 {
		t.Fatalf("usage is summed over both calls: in %d out %d", res.InputTokens, res.OutputTokens)
	}
	if res.CostKnown && res.CostUSD < 3*WebSearchUSD {
		t.Fatalf("cost %v does not include 3 searches at $%v", res.CostUSD, WebSearchUSD)
	}
}

// The model may wrap the research JSON in prose; the verdict still parses.
func TestWebReplyWithProseAroundTheJSON(t *testing.T) {
	reply := `Here is what I found: {"relevant":true,"severity":"info","message":"ok"} Hope that helps.`
	f := &fake{replies: []func(http.ResponseWriter){searchMsg("end_turn", text(reply), 0)}}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), webReq())
	if err != nil || !res.Verdict.Relevant {
		t.Fatalf("%+v %v", res, err)
	}
}

// pause_turn is resumed at most maxPauses times.
func TestPauseTurnResumesAreCapped(t *testing.T) {
	var replies []func(http.ResponseWriter)
	for i := 0; i < maxPauses+3; i++ {
		replies = append(replies, searchMsg("pause_turn", pausedContent, 1))
	}
	f := &fake{replies: replies}
	b, _ := newBackend(t, f)
	_, _ = b.Run(context.Background(), webReq())
	if len(f.bodies) != maxPauses+1 {
		t.Fatalf("%d requests, want %d", len(f.bodies), maxPauses+1)
	}
}

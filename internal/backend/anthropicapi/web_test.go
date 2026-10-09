package anthropicapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

// searchMsg is a Messages API reply whose usage reports server web searches.
func searchMsg(stop, content string, searches int) func(http.ResponseWriter) {
	return searchMsgModel("claude-opus-5-5", stop, content, searches)
}

func searchMsgModel(model, stop, content string, searches int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		fmt.Fprintf(w, `{"id":"msg_w","type":"message","role":"assistant","model":%q,"content":%s,"stop_reason":%q,"stop_details":null,"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"server_tool_use":{"web_search_requests":%d,"web_fetch_requests":0}}}`,
			model, content, stop, searches)
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

// baselineCost is the priced cost of a web research run with this usage and no searches.
func baselineCost(t *testing.T, in, out int) float64 {
	t.Helper()
	f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(verdictPass), in, out, 0, 0)}}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), webReq())
	if err != nil || !res.CostKnown {
		t.Fatalf("baseline %+v %v", res, err)
	}
	return res.CostUSD
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
	for _, c := range []struct {
		name    string
		r       backend.Request
		mustRun bool // a plain request that has to succeed; write requests may be refused
		format  bool // structured output stays on
	}{
		{"review with Web set", with(func(x *backend.Request) {}), true, true},
		{"research without web", func() backend.Request { x := req(); x.Pass = backend.PassResearch; return x }(), true, true},
		{"memory with Web set", with(func(x *backend.Request) { x.Pass = backend.PassMemory }), true, true},
		{"ask pass with Web", with(func(x *backend.Request) { x.Pass = "ask" }), true, true},
		{"draft pass with Web", with(func(x *backend.Request) { x.Pass = "draft" }), true, true},
		{"council pass with Web", with(func(x *backend.Request) { x.Pass = "council" }), true, true},
		{"write pass with Web", with(func(x *backend.Request) { x.Pass, x.Write = backend.PassWrite, true }), false, false},
		{"Write flag with Web", with(func(x *backend.Request) { x.Write = true }), true, true},
		{"research + Write + Web", with(func(x *backend.Request) { x.Pass, x.Write = backend.PassResearch, true }), true, false},
	} {
		reply := verdictPass
		if c.r.Pass == backend.PassMemory {
			reply = `{"remember":[]}`
		}
		f := &fake{replies: []func(http.ResponseWriter){msg("claude-opus-5-5", "end_turn", text(reply), 10, 5, 0, 0)}}
		b, _ := newBackend(t, f)
		_, err := b.Run(context.Background(), c.r)
		if err != nil && c.mustRun {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, body := range f.bodies {
			for _, ty := range toolTypes(body) {
				if strings.Contains(ty, "web_") {
					t.Errorf("%s: web tool %s declared", c.name, ty)
				}
			}
		}
		if c.format && len(f.bodies) > 0 {
			if oc, _ := f.bodies[0]["output_config"].(map[string]any); oc == nil || oc["format"] == nil {
				t.Errorf("%s: structured output stays on", c.name)
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
	base := baselineCost(t, 200, 40)
	if !res.CostKnown || math.Abs(res.CostUSD-base-3*WebSearchUSD) > 1e-9 {
		t.Fatalf("cost %v (known %v), want %v + 3 searches at $%v", res.CostUSD, res.CostKnown, base, WebSearchUSD)
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

// pause_turn is resumed at most maxPauses times, then it is an explicit error
// that still carries the spend.
func TestPauseTurnResumesAreCapped(t *testing.T) {
	var replies []func(http.ResponseWriter)
	for i := 0; i < maxPauses+3; i++ {
		// no paid uses in these pauses, so only the pause cap can stop the run
		replies = append(replies, searchMsg("pause_turn", `[{"type":"text","text":"still working"}]`, 0))
	}
	f := &fake{replies: replies}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), webReq())
	if len(f.bodies) != maxPauses+1 {
		t.Fatalf("%d requests, want %d", len(f.bodies), maxPauses+1)
	}
	want := fmt.Sprintf("web research paused %d times without finishing", maxPauses+1)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err %v, want %q", err, want)
	}
	if res.InputTokens != 100*(maxPauses+1) || res.OutputTokens != 20*(maxPauses+1) {
		t.Fatalf("spend lost: in %d out %d", res.InputTokens, res.OutputTokens)
	}
}

func maxUsesOf(body map[string]any, prefix string) (float64, bool) {
	tools, _ := body["tools"].([]any)
	for _, tl := range tools {
		m := tl.(map[string]any)
		if strings.HasPrefix(fmt.Sprint(m["type"]), prefix) {
			v, ok := m["max_uses"].(float64)
			return v, ok
		}
	}
	return 0, false
}

// The cap of 5 paid web uses covers pause_turn resumes: each call gets the
// remaining budget, and once it is spent the run stops resuming.
func TestWebUsesAreCappedAcrossPauses(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){
		searchMsg("pause_turn", pausedContent, 2),
		searchMsg("pause_turn", pausedContent, 3),
		searchMsg("end_turn", text(verdictPass), 0), // must never be requested
	}}
	b, _ := newBackend(t, f)
	_, err := b.Run(context.Background(), webReq())
	if len(f.bodies) != 2 {
		t.Fatalf("%d requests, want 2 (budget spent after the second)", len(f.bodies))
	}
	if v, _ := maxUsesOf(f.bodies[0], "web_search"); v != 5 {
		t.Fatalf("first call max_uses %v", v)
	}
	if v, ok := maxUsesOf(f.bodies[1], "web_search"); !ok || v != 3 {
		t.Fatalf("second call max_uses %v (ok %v), want the remaining 3", v, ok)
	}
	if v, _ := maxUsesOf(f.bodies[1], "web_fetch"); v != 3 {
		t.Fatalf("fetch max_uses on the second call %v, want 3", v)
	}
	if err == nil || !strings.Contains(err.Error(), "without finishing") {
		t.Fatalf("err %v", err)
	}
}

// Across tool-loop turns the budget carries over, and at zero the web tools
// are dropped from later calls while the repo tools stay.
func TestWebUsesAreCappedAcrossToolTurns(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{}`), 0o600)
	srv := `{"type":"server_tool_use","id":"srvtoolu_01","name":"web_search","input":{"query":"q"}}`
	use := `{"type":"tool_use","id":"toolu_1","name":"list_files","input":{"glob":"*"}}`
	f := &fake{replies: []func(http.ResponseWriter){
		searchMsg("tool_use", `[`+srv+`,`+use+`]`, 2),
		searchMsg("tool_use", `[`+srv+`,`+use+`]`, 3),
		searchMsg("end_turn", text(verdictPass), 0),
	}}
	b, _ := newBackend(t, f)
	r := webReq()
	r.RepoRead, r.Cwd = true, root
	if _, err := b.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(f.bodies) != 3 {
		t.Fatalf("%d requests", len(f.bodies))
	}
	if v, _ := maxUsesOf(f.bodies[0], "web_search"); v != 5 {
		t.Fatalf("turn 1 max_uses %v", v)
	}
	if v, _ := maxUsesOf(f.bodies[1], "web_search"); v != 3 {
		t.Fatalf("turn 2 max_uses %v, want 3", v)
	}
	third := toolTypes(f.bodies[2])
	if len(third) == 0 || strings.Contains(strings.Join(third, ","), "web_") {
		t.Fatalf("turn 3 tools %v: web dropped, repo tools kept", third)
	}
}

func TestWebFetchCarriesContentCap(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){searchMsg("end_turn", text(verdictPass), 0)}}
	b, _ := newBackend(t, f)
	if _, err := b.Run(context.Background(), webReq()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range f.bodies[0]["tools"].([]any) {
		m := tl.(map[string]any)
		if strings.HasPrefix(fmt.Sprint(m["type"]), "web_fetch") {
			found = true
			if m["max_content_tokens"] != float64(8000) {
				t.Fatalf("fetch tool %v", m)
			}
		}
	}
	if !found {
		t.Fatal("no fetch tool")
	}
}

func serverError(w http.ResponseWriter) {
	w.WriteHeader(500)
	fmt.Fprint(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
}

// A failure after paid pauses keeps the spend: usage and priced searches.
func TestFailureMidPauseKeepsTheSpend(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){
		searchMsg("pause_turn", pausedContent, 1),
		searchMsg("pause_turn", pausedContent, 1),
		serverError, serverError, serverError, // the SDK retries a 500 twice
	}}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), webReq())
	if err == nil {
		t.Fatal("want the 500 as an error")
	}
	if res.InputTokens != 200 || res.OutputTokens != 40 {
		t.Fatalf("spend lost: in %d out %d", res.InputTokens, res.OutputTokens)
	}
	base := baselineCost(t, 200, 40)
	if !res.CostKnown || math.Abs(res.CostUSD-base-2*WebSearchUSD) > 1e-9 {
		t.Fatalf("cost %v known %v, want %v + 2 searches", res.CostUSD, res.CostKnown, base)
	}
}

// A gateway that omits usage.server_tool_use must not zero the cost: the
// server_tool_use blocks in the content are counted too.
func TestSearchesAreCountedFromContentBlocks(t *testing.T) {
	block := `{"type":"server_tool_use","id":"srvtoolu_01","name":"web_search","input":{"query":"q"}}`
	f := &fake{replies: []func(http.ResponseWriter){
		msg("claude-opus-5-5", "end_turn", `[`+block+`,`+strings.TrimPrefix(text(verdictPass), "["), 100, 20, 0, 0),
	}}
	b, _ := newBackend(t, f)
	res, err := b.Run(context.Background(), webReq())
	if err != nil {
		t.Fatal(err)
	}
	base := baselineCost(t, 100, 20)
	if !res.CostKnown || math.Abs(res.CostUSD-base-WebSearchUSD) > 1e-9 {
		t.Fatalf("cost %v known %v, want %v + 1 search", res.CostUSD, res.CostKnown, base)
	}
}

// An unpriced model stays unknown: searches do not make it a known price.
func TestUnpricedModelWithSearchesIsUnknownCost(t *testing.T) {
	f := &fake{replies: []func(http.ResponseWriter){searchMsgModel("mystery-model", "end_turn", text(verdictPass), 2)}}
	b, _ := newBackend(t, f)
	r := webReq()
	r.Model = "mystery-model"
	res, err := b.Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if res.CostKnown || res.CostUSD != 0 {
		t.Fatalf("cost %v known %v", res.CostUSD, res.CostKnown)
	}
}

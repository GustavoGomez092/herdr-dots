package openaiapi

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
)

const webVerdict = `{"relevant":true,"severity":"warn","message":"lodash 4.17.20 has an advisory."}`

// webSearchOut is a reply that searched the web once, then answered.
func webSearchOut() string {
	return `[{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"lodash advisory"}},` +
		strings.TrimPrefix(textOut(webVerdict), "[")
}

func webResearch() backend.Request {
	r := req()
	r.Pass, r.Web = backend.PassResearch, true
	return r
}

func TestWebResearchAddsHostedSearchAndPricesCalls(t *testing.T) {
	f := &fake{replies: []string{response("gpt-6-sol", webSearchOut(), 1000, 0, 50)}}
	res, err := newBackend(t, f).Run(context.Background(), webResearch())
	if err != nil || !res.Verdict.Relevant {
		t.Fatalf("%+v %v", res, err)
	}
	tools, _ := f.bodies[0]["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search" {
		t.Fatalf("tools %v", tools)
	}
	if _, ok := f.bodies[0]["tool_choice"]; ok {
		t.Fatal("a web-only first turn keeps tool_choice unset")
	}
	plain := &fake{replies: []string{response("gpt-6-sol", textOut(webVerdict), 1000, 0, 50)}}
	base, _ := newBackend(t, plain).Run(context.Background(), webResearch())
	if !res.CostKnown || !base.CostKnown || math.Abs(res.CostUSD-base.CostUSD-WebSearchCallUSD) > 1e-9 {
		t.Fatalf("one search call adds $%v: %v (known %v) vs %v (known %v)", WebSearchCallUSD, res.CostUSD, res.CostKnown, base.CostUSD, base.CostKnown)
	}
	review := &fake{replies: []string{response("gpt-6-sol", textOut(webVerdict), 1000, 0, 50)}}
	r := req()
	r.Web = true // a review never gets the web
	if _, err := newBackend(t, review).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, ok := review.bodies[0]["tools"]; ok {
		t.Fatalf("review tools %v", review.bodies[0]["tools"])
	}
}

func TestOnlyWebResearchGetsHostedSearch(t *testing.T) {
	with := func(f func(*backend.Request)) backend.Request { x := req(); x.Web = true; f(&x); return x }
	for _, c := range []struct {
		name    string
		r       backend.Request
		mustRun bool // a plain request that has to succeed; write requests may be refused
	}{
		{"memory with Web", with(func(x *backend.Request) { x.Pass = backend.PassMemory }), true},
		{"write pass with Web", with(func(x *backend.Request) { x.Pass, x.Write = backend.PassWrite, true }), false},
		{"Write flag with Web", with(func(x *backend.Request) { x.Write = true }), true},
		{"ask pass with Web", with(func(x *backend.Request) { x.Pass = "ask" }), true},
		{"draft pass with Web", with(func(x *backend.Request) { x.Pass = "draft" }), true},
		{"council pass with Web", with(func(x *backend.Request) { x.Pass = "council" }), true},
		{"research + Write + Web", with(func(x *backend.Request) { x.Pass, x.Write = backend.PassResearch, true }), true},
		{"research without Web", func() backend.Request { x := req(); x.Pass = backend.PassResearch; return x }(), true},
	} {
		reply := webVerdict
		if c.r.Pass == backend.PassMemory {
			reply = `{"remember":[]}`
		}
		f := &fake{replies: []string{response("gpt-6-sol", textOut(reply), 10, 0, 5)}}
		if _, err := newBackend(t, f).Run(context.Background(), c.r); err != nil && c.mustRun {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, body := range f.bodies {
			if strings.Contains(fmt.Sprint(body["tools"]), "web_search") {
				t.Errorf("%s: hosted web search declared: %v", c.name, body["tools"])
			}
		}
	}
}

// An unpriced model stays unknown: search calls do not make it a known price.
func TestUnpricedModelWithSearchesIsUnknownCost(t *testing.T) {
	f := &fake{replies: []string{response("mystery-model", webSearchOut(), 1000, 0, 50)}}
	r := webResearch()
	r.Model = "mystery-model"
	res, err := newBackend(t, f).Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if res.CostKnown || res.CostUSD != 0 {
		t.Fatalf("cost %v known %v", res.CostUSD, res.CostKnown)
	}
}

func searchItems(n int) string {
	var items []string
	for i := 0; i < n; i++ {
		items = append(items, fmt.Sprintf(`{"type":"web_search_call","id":"ws_%d","status":"completed","action":{"type":"search","query":"q"}}`, i))
	}
	return strings.Join(items, ",")
}

// Paid web uses are capped at 5 per run over all turns: each turn carries the
// remaining budget as max_tool_calls, and at zero the web tool is dropped
// while the repo tools stay.
func TestWebCallsAreCappedAcrossTurns(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{}`), 0o600)
	call := `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"list_files","arguments":"{\"glob\":\"*\"}","status":"completed"}`
	call2 := strings.ReplaceAll(strings.ReplaceAll(call, "fc_1", "fc_2"), "call_1", "call_2")
	f := &fake{replies: []string{
		response("gpt-6-sol", `[`+searchItems(2)+`,`+call+`]`, 100, 0, 10),
		response("gpt-6-sol", `[`+searchItems(3)+`,`+call2+`]`, 100, 0, 10),
		response("gpt-6-sol", textOut(webVerdict), 100, 0, 10),
	}}
	r := webResearch()
	r.RepoRead, r.Cwd = true, root
	res, err := newBackend(t, f).Run(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.bodies) != 3 {
		t.Fatalf("%d requests", len(f.bodies))
	}
	for i, want := range []float64{5, 3} {
		if got, _ := f.bodies[i]["max_tool_calls"].(float64); got != want {
			t.Fatalf("turn %d max_tool_calls %v, want %v", i+1, f.bodies[i]["max_tool_calls"], want)
		}
	}
	if _, ok := f.bodies[2]["max_tool_calls"]; ok {
		t.Fatalf("turn 3 has no web budget left, max_tool_calls %v", f.bodies[2]["max_tool_calls"])
	}
	third := fmt.Sprint(f.bodies[2]["tools"])
	if strings.Contains(third, "web_search") || !strings.Contains(third, "list_files") {
		t.Fatalf("turn 3 tools %s: web dropped, repo tools kept", third)
	}
	plain := &fake{replies: []string{
		response("gpt-6-sol", `[`+call+`]`, 100, 0, 10), response("gpt-6-sol", `[`+call2+`]`, 100, 0, 10),
		response("gpt-6-sol", textOut(webVerdict), 100, 0, 10)}}
	base, err := newBackend(t, plain).Run(context.Background(), r)
	if err != nil || !base.CostKnown || !res.CostKnown {
		t.Fatalf("%v %+v", err, base)
	}
	if math.Abs(res.CostUSD-base.CostUSD-5*WebSearchCallUSD) > 1e-9 {
		t.Fatalf("cost %v vs baseline %v + 5 calls", res.CostUSD, base.CostUSD)
	}
}

// The model may wrap the research JSON in prose; the verdict still parses.
func TestWebReplyWithProseAroundTheJSON(t *testing.T) {
	prose := `[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Found it: ` +
		strings.ReplaceAll(webVerdict, `"`, `\"`) + ` Done.","annotations":[]}]}]`
	f := &fake{replies: []string{response("gpt-6-sol", prose, 10, 0, 5)}}
	res, err := newBackend(t, f).Run(context.Background(), webResearch())
	if err != nil || !res.Verdict.Relevant {
		t.Fatalf("%+v %v", res, err)
	}
}

// A web search followed by a repo tool call: the next request carries the
// web_search_call item back (stateless store:false).
func TestWebSearchCallIsEchoedOnTheNextToolTurn(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"lodash":"4.17.20"}}`), 0o600)
	call := `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"list_files","arguments":"{\"glob\":\"*\"}","status":"completed"}`
	first := `[{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"lodash advisory"}},` + call + `]`
	f := &fake{replies: []string{response("gpt-6-sol", first, 100, 0, 10), response("gpt-6-sol", textOut(webVerdict), 100, 0, 10)}}
	r := webResearch()
	r.RepoRead, r.Cwd = true, root
	if _, err := newBackend(t, f).Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if len(f.bodies) != 2 {
		t.Fatalf("%d requests", len(f.bodies))
	}
	if in := fmt.Sprint(f.bodies[1]["input"]); !strings.Contains(in, "web_search_call") || !strings.Contains(in, "function_call_output") {
		t.Fatalf("second input %s", in)
	}
}

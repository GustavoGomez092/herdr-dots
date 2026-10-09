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
	if res.CostKnown && math.Abs(res.CostUSD-base.CostUSD-WebSearchCallUSD) > 1e-9 {
		t.Fatalf("one search call adds $%v: %v vs %v", WebSearchCallUSD, res.CostUSD, base.CostUSD)
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
	for name, r := range map[string]backend.Request{
		"memory with Web":        with(func(x *backend.Request) { x.Pass = backend.PassMemory }),
		"write pass with Web":    with(func(x *backend.Request) { x.Pass, x.Write = backend.PassWrite, true }),
		"Write flag with Web":    with(func(x *backend.Request) { x.Write = true }),
		"ask pass with Web":      with(func(x *backend.Request) { x.Pass = "ask" }),
		"draft pass with Web":    with(func(x *backend.Request) { x.Pass = "draft" }),
		"council pass with Web":  with(func(x *backend.Request) { x.Pass = "council" }),
		"research + Write + Web": with(func(x *backend.Request) { x.Pass, x.Write = backend.PassResearch, true }),
		"research without Web":   func() backend.Request { x := req(); x.Pass = backend.PassResearch; return x }(),
	} {
		f := &fake{replies: []string{response("gpt-6-sol", textOut(webVerdict), 10, 0, 5)}}
		_, _ = newBackend(t, f).Run(context.Background(), r)
		for _, body := range f.bodies {
			if strings.Contains(fmt.Sprint(body["tools"]), "web_search") {
				t.Errorf("%s: hosted web search declared: %v", name, body["tools"])
			}
		}
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

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/pricing"
)

type fakeAPI struct{ o Options }

func (fakeAPI) Kind() string                                 { return "fake-api" }
func (fakeAPI) Check(context.Context) error                  { return nil }
func (fakeAPI) Run(context.Context, Request) (Result, error) { return Result{}, nil }

func TestRegistryKeepsV1ConstructorAndAddsKinds(t *testing.T) {
	b, err := New("claude-cli", &fakeRunner{})
	if err != nil || b.Kind() != KindClaudeCLI {
		t.Fatalf("New(claude-cli) = %v %v", b, err)
	}
	if b, _ := New("codex-cli", nil); b.Kind() != KindCodexCLI {
		t.Fatal("codex-cli")
	}
	if _, err := NewWith("nope", Options{}); err == nil || !strings.Contains(err.Error(), `unknown backend "nope"`) {
		t.Fatalf("unknown kind: %v", err)
	}
	Register("fake-api", func(o Options) (Backend, error) { return fakeAPI{o: o}, nil })
	got, err := NewWith("fake-api", Options{Deny: []string{"x"}})
	if err != nil || got.(fakeAPI).o.Deny[0] != "x" {
		t.Fatalf("NewWith passes options: %+v %v", got, err)
	}
	if !slices.Contains(Kinds(), "fake-api") || !slices.Contains(Kinds(), KindClaudeCLI) {
		t.Fatalf("Kinds() = %v", Kinds())
	}
	defer func() {
		if recover() == nil {
			t.Fatal("registering a kind twice must panic")
		}
	}()
	Register("fake-api", func(o Options) (Backend, error) { return fakeAPI{}, nil })
}

func TestProviderFor(t *testing.T) {
	for kind, want := range map[string]string{KindAnthropicAPI: "anthropic", KindOpenAIAPI: "openai", KindOpenRouter: "openrouter", KindOllama: "ollama", KindClaudeCLI: ""} {
		if got := ProviderFor(kind); got != want {
			t.Errorf("ProviderFor(%s) = %q", kind, got)
		}
	}
}

func TestVerdictSchemaShapes(t *testing.T) {
	s := VerdictSchema(Request{})
	props := s["properties"].(map[string]any)
	if len(props) != 3 || s["additionalProperties"] != false || len(s["required"].([]any)) != 3 {
		t.Fatalf("review schema %v", s)
	}
	if _, ok := VerdictSchema(Request{Learn: true})["properties"].(map[string]any)["remember"]; !ok {
		t.Fatal("learning schema carries remember")
	}
	mp := VerdictSchema(Request{Pass: PassMemory})["properties"].(map[string]any)
	if _, ok := mp["relevant"]; ok || mp["remember"] == nil {
		t.Fatalf("memory-pass schema %v", mp)
	}
	if _, err := json.Marshal(VerdictSchema(Request{Learn: true})); err != nil {
		t.Fatal(err)
	}
}

// A reply that satisfies the schema must parse, with and without remember.
func TestSchemaConformingRepliesParse(t *testing.T) {
	for _, reply := range []string{
		`{"message":"","relevant":false,"severity":"info"}`,
		`{"message":"Use httpOnly","relevant":true,"remember":[{"id":"","kind":"convention","op":"add","paths":[],"scope":"project","text":"Cookies are httpOnly."}],"severity":"warn"}`,
	} {
		if !ValidReply(Request{Learn: true}, reply) {
			t.Errorf("reply should parse: %s", reply)
		}
	}
	if !ValidReply(Request{Pass: PassMemory}, `{"remember":[]}`) || ValidReply(Request{}, "not json") {
		t.Fatal("ValidReply")
	}
}

func TestFinishAPIFillsUsageCostAndVerdict(t *testing.T) {
	res, err := FinishAPI(Request{}, APIRun{
		Text:  `{"relevant":true,"severity":"warn","message":"m"}`,
		Usage: pricing.Usage{In: 100, Out: 20, CacheRead: 300, CacheWrite: 50}, CostUSD: 0.01, CostKnown: true,
		Model: "claude-opus-4-8", ToolCalls: 3, Capped: true, Started: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.InputTokens != 450 || res.OutputTokens != 20 || res.CacheReadTokens != 300 || res.CacheWriteTokens != 50 ||
		res.CostUSD != 0.01 || !res.CostKnown || res.Model != "claude-opus-4-8" || res.ToolCalls != 3 || !res.ToolsCapped ||
		res.Duration < time.Second || res.Verdict.Message != "m" {
		t.Fatalf("%+v", res)
	}
	if _, err := FinishAPI(Request{}, APIRun{Text: "prose"}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed: %v", err)
	}
}

func TestRefusedError(t *testing.T) {
	if err := Refused("cyber"); !errors.Is(err, ErrRefused) || err.Error() != "refused (cyber)" {
		t.Fatalf("%v", err)
	}
	if Refused("").Error() != "refused (unspecified)" {
		t.Fatal("empty category")
	}
}

// Recorded-style `claude -p --output-format json` usage: with prompt caching
// input_tokens is only the uncached part (2 here).
func TestClaudeOutputCountsCachedInput(t *testing.T) {
	out := `{"type":"result","is_error":false,"result":"{\"relevant\":false,\"severity\":\"info\",\"message\":\"\"}","total_cost_usd":0.0291,
	"usage":{"input_tokens":2,"cache_creation_input_tokens":4512,"cache_read_input_tokens":13894,"output_tokens":61,"server_tool_use":{"web_search_requests":0},"service_tier":"standard"}}`
	res, err := ParseClaudeOutput([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if res.InputTokens != 2+4512+13894 || res.CacheWriteTokens != 4512 || res.CacheReadTokens != 13894 || res.OutputTokens != 61 {
		t.Fatalf("usage %+v", res)
	}
	if !res.CostKnown || res.CostUSD != 0.0291 {
		t.Fatalf("cost %+v", res)
	}
}

func TestClaudeCostKnownOnlyWhenReported(t *testing.T) {
	res, _ := ParseClaudeOutput([]byte(`{"is_error":false,"result":"{\"relevant\":false}","total_cost_usd":0.0042,"usage":{}}`))
	if !res.CostKnown || res.CostUSD != 0.0042 {
		t.Fatalf("%+v", res)
	}
	res, _ = ParseClaudeOutput([]byte(`{"is_error":false,"result":"{\"relevant\":false}","usage":{}}`))
	if res.CostKnown {
		t.Fatal("no total_cost_usd → unknown")
	}
}

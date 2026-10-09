package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/config"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
)

const testPID = "g-0123456789abcdef"

// fakeEngine records calls and answers from its fields.
type fakeEngine struct {
	mu       sync.Mutex
	dots     ipc.DotsResult
	ask      ipc.AskResult
	council  ipc.CouncilResult
	queue    ipc.QueueResult
	err      error
	asks     []ipc.AskArgs
	deadline time.Duration // time left on the last Ask's context
	notified int
	statuses []string
}

func (f *fakeEngine) ListDots(context.Context) (ipc.DotsResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dots, f.err
}

func (f *fakeEngine) Ask(ctx context.Context, slug, q string, include bool) (ipc.AskResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asks = append(f.asks, ipc.AskArgs{Slug: slug, Question: q, IncludeTurns: include})
	if d, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(d)
	}
	return f.ask, f.err
}

func (f *fakeEngine) AskCouncil(context.Context, string) (ipc.CouncilResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.council, f.err
}

func (f *fakeEngine) ListQueue(_ context.Context, status string) (ipc.QueueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, status)
	return f.queue, f.err
}

func (f *fakeEngine) MemoryChanged(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notified++
	return nil
}

// connect serves New(d) over in-memory transports and returns a client session.
func connect(t *testing.T, d Deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := New(d).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call returns the tool's text, its structured result as JSON, and whether it
// was an error (a protocol error counts as one, with its message as text).
func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, []byte, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return err.Error(), nil, true
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	js, _ := json.Marshal(res.StructuredContent)
	return text, js, res.IsError
}

func inPane(e EngineAPI) Deps { return Deps{Engine: e, PaneID: "w1:p1"} }

func memStore(t *testing.T) *memory.Store {
	t.Helper()
	return memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
}

func TestToolListFollowsConfig(t *testing.T) {
	names := func(cs *mcp.ClientSession) []string {
		res, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tl := range res.Tools {
			out = append(out, tl.Name)
		}
		slices.Sort(out)
		return out
	}
	all := slices.Sorted(slices.Values(config.MCPTools))
	if got := names(connect(t, inPane(&fakeEngine{}))); !slices.Equal(got, all) {
		t.Fatalf("default tools %v", got)
	}
	d := inPane(&fakeEngine{})
	d.Tools = []string{"ask_dot", "list_queue", "bogus"}
	if got := names(connect(t, d)); !slices.Equal(got, []string{"ask_dot", "list_queue"}) {
		t.Fatalf("configured tools %v", got)
	}
}

// Review Focus 1: outside Herdr (or in a dot's own CLI run) every tool
// refuses, but the tool list stays the same.
func TestEveryToolRefusesOutsideHerdr(t *testing.T) {
	cs := connect(t, Deps{Memory: memStore(t)})
	args := map[string]map[string]any{
		"list_dots":      {},
		"ask_dot":        {"slug": "security", "question": "is this safe?"},
		"ask_council":    {"question": "ready to merge?"},
		"list_queue":     {},
		"read_memory":    {},
		"propose_memory": {"kind": "note", "text": "x"},
	}
	for _, tool := range config.MCPTools {
		text, _, isErr := call(t, cs, tool, args[tool])
		if !isErr || text != NotInHerdr {
			t.Errorf("%s: isError=%v text=%q", tool, isErr, text)
		}
	}
}

func TestAskDotReturnsTheAnswer(t *testing.T) {
	fe := &fakeEngine{ask: ipc.AskResult{Slug: "security", Name: "Security", Relevant: true, Severity: "warn",
		Message: "Keep the JWT in an httpOnly cookie.", CostUSD: 0.0123, CostKnown: true, Model: "sonnet"}}
	cs := connect(t, inPane(fe))
	text, js, isErr := call(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "  Is localStorage OK for the JWT?  "})
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if !strings.HasPrefix(text, "[◆ Security] (warn) Keep the JWT in an httpOnly cookie.") || !strings.Contains(text, "cost $0.0123") {
		t.Fatalf("text %q", text)
	}
	var out AskDotOut
	if err := json.Unmarshal(js, &out); err != nil || out.Severity != "warn" || out.Model != "sonnet" || !strings.Contains(out.Note, "approval queue") {
		t.Fatalf("structured %s (%v)", js, err)
	}
	if len(fe.asks) != 1 || fe.asks[0] != (ipc.AskArgs{Slug: "security", Question: "Is localStorage OK for the JWT?", IncludeTurns: true}) {
		t.Fatalf("engine got %+v", fe.asks)
	}
	call(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "q", "include_turns": false})
	if fe.asks[1].IncludeTurns {
		t.Fatal("include_turns=false must reach the engine")
	}
}

func TestAskDotNothingToAdd(t *testing.T) {
	cs := connect(t, inPane(&fakeEngine{ask: ipc.AskResult{Slug: "copy", Name: "Copy", Severity: "info"}}))
	if text, _, _ := call(t, cs, "ask_dot", map[string]any{"slug": "copy", "question": "q"}); !strings.HasPrefix(text, "[◆ Copy] nothing to add") {
		t.Fatalf("text %q", text)
	}
}

func TestAskDotValidatesBeforeAsking(t *testing.T) {
	fe := &fakeEngine{}
	cs := connect(t, inPane(fe))
	for name, args := range map[string]map[string]any{
		"long question":  {"slug": "security", "question": strings.Repeat("a", MaxQuestionChars+1)},
		"empty question": {"slug": "security", "question": "   "},
		"bad slug":       {"slug": "../x", "question": "q"},
		"missing slug":   {"question": "q"},
	} {
		if _, _, isErr := call(t, cs, "ask_dot", args); !isErr {
			t.Errorf("%s accepted", name)
		}
	}
	if text, _, _ := call(t, cs, "ask_dot", map[string]any{"slug": "security", "question": strings.Repeat("é", MaxQuestionChars+1)}); !strings.Contains(text, "the limit is 4000") {
		t.Fatalf("text %q", text)
	}
	if len(fe.asks) != 0 {
		t.Fatalf("the engine must not be asked: %+v", fe.asks)
	}
}

// Review Focus 4: the ask carries a deadline the engine can work within.
func TestAskDotPassesADeadline(t *testing.T) {
	fe := &fakeEngine{ask: ipc.AskResult{Name: "Security"}}
	d := inPane(fe)
	d.AskTimeout = 130 * time.Second
	call(t, connect(t, d), "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if fe.deadline < 120*time.Second || fe.deadline > 130*time.Second {
		t.Fatalf("deadline %v", fe.deadline)
	}
}

func TestEngineErrorsBecomeToolErrors(t *testing.T) {
	fe := &fakeEngine{err: errors.New("ask limit reached; continue without the dot (3 per turn)")}
	text, _, isErr := call(t, connect(t, inPane(fe)), "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if !isErr || text != "ask limit reached; continue without the dot (3 per turn)" {
		t.Fatalf("isError=%v text=%q", isErr, text)
	}
	fe.err = context.DeadlineExceeded
	if text, _, _ := call(t, connect(t, inPane(fe)), "ask_dot", map[string]any{"slug": "security", "question": "q"}); !strings.Contains(text, "no answer in time") {
		t.Fatalf("text %q", text)
	}
	// Keys never reach the agent, whatever the engine sent (reconciled).
	fe.err = errors.New("Security failed: 401 Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123")
	if text, _, isErr := call(t, connect(t, inPane(fe)), "ask_dot", map[string]any{"slug": "security", "question": "q"}); !isErr || strings.Contains(text, "abcdefghijklmnop") {
		t.Fatalf("isError=%v text=%q", isErr, text)
	}
}

func TestResultsStayWithinMaxResultChars(t *testing.T) {
	fe := &fakeEngine{ask: ipc.AskResult{Name: "Security", Relevant: true, Severity: "warn", Message: strings.Repeat("long advice ", 2000)}}
	d := inPane(fe)
	d.MaxResultChars = 3000
	text, js, _ := call(t, connect(t, d), "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if n := utf8.RuneCountInString(text) + utf8.RuneCount(js); n > 3000 || !strings.Contains(text, "…") {
		t.Fatalf("ask result is %d characters", n)
	}

	var cards []ipc.CardInfo
	for i := range 20 {
		cards = append(cards, ipc.CardInfo{ID: string(rune('a' + i)), Dot: "security", Severity: "warn", Body: strings.Repeat("b", 2000)})
	}
	fe.queue = ipc.QueueResult{Cards: cards, Total: 35}
	text, js, _ = call(t, connect(t, inPane(fe)), "list_queue", map[string]any{})
	if n := utf8.RuneCountInString(text) + utf8.RuneCount(js); n > DefaultMaxResultChars {
		t.Fatalf("queue result is %d characters", n)
	}
	var out ListQueueOut
	_ = json.Unmarshal(js, &out)
	if len(out.Cards) >= 20 || out.Total != 35 || !strings.Contains(text, "more not shown") {
		t.Fatalf("cards %d total %d text tail %q", len(out.Cards), out.Total, text[max(0, len(text)-200):])
	}
}

func TestListDotsQueueAndCouncil(t *testing.T) {
	fe := &fakeEngine{
		dots:  ipc.DotsResult{ProjectID: testPID, ProjectName: "shop", Dots: []ipc.DotInfo{{Slug: "security", Name: "Security", PurposeSummary: "Find auth bugs.", Mode: "queue", Status: "idle", Calls: 2, Budget: 40}}},
		queue: ipc.QueueResult{Cards: []ipc.CardInfo{{ID: "7", Dot: "security", Severity: "critical", Turn: 4, Status: "pending", Body: "JWT in localStorage"}}, Total: 1},
		council: ipc.CouncilResult{Digest: "## Must fix\n- JWT in localStorage (Security)", CostUSD: 0.05,
			Members: []ipc.CouncilMember{{Slug: "security", Severity: "critical"}, {Slug: "copy", Failed: true}}},
	}
	cs := connect(t, inPane(fe))
	if text, _, isErr := call(t, cs, "list_dots", map[string]any{}); isErr || !strings.Contains(text, "security: Security (queue, idle, 2/40 calls) Find auth bugs.") {
		t.Fatalf("list_dots %q", text)
	}
	if text, _, isErr := call(t, cs, "list_queue", map[string]any{"status": "all"}); isErr || !strings.Contains(text, "#7 security (critical, turn 4, pending): JWT in localStorage") {
		t.Fatalf("list_queue %q", text)
	}
	if fe.statuses[0] != ipc.QueueAll {
		t.Fatalf("status %v", fe.statuses)
	}
	if _, _, isErr := call(t, cs, "list_queue", map[string]any{"status": "dismissed"}); !isErr {
		t.Fatal("unknown status accepted")
	}
	text, js, isErr := call(t, cs, "ask_council", map[string]any{"question": "ready to merge?"})
	if isErr || !strings.Contains(text, "## Must fix") {
		t.Fatalf("ask_council %q", text)
	}
	var out AskCouncilOut
	if err := json.Unmarshal(js, &out); err != nil || len(out.Members) != 2 || !out.Members[1].Failed {
		t.Fatalf("council %s", js)
	}
	empty := connect(t, inPane(&fakeEngine{}))
	if text, _, _ := call(t, empty, "list_dots", map[string]any{}); !strings.Contains(text, "No dots are attached") {
		t.Fatalf("empty list_dots %q", text)
	}
}

func TestReadMemoryReturnsActiveEntriesOnly(t *testing.T) {
	st := memStore(t)
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindConvention, Text: "Use pnpm, never npm.", Pinned: true},
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Adopt zod for validation.", Status: memory.StatusProposed, Source: memory.SourceModel, Dot: "security"},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindFinding, Text: "Tokens belong in httpOnly cookies.", Source: memory.SourceOutcome},
	} {
		if _, err := st.Add(testPID, e); err != nil {
			t.Fatal(err)
		}
	}
	d := inPane(&fakeEngine{dots: ipc.DotsResult{ProjectID: testPID}})
	d.Memory = st
	cs := connect(t, d)
	text, js, isErr := call(t, cs, "read_memory", map[string]any{})
	if isErr {
		t.Fatal(text)
	}
	var out ReadMemoryOut
	_ = json.Unmarshal(js, &out)
	if out.Total != 2 || len(out.Entries) != 2 || out.Entries[0].ID != "p:m1" || out.Entries[1].Dot != "security" || strings.Contains(text, "zod") {
		t.Fatalf("read_memory %s\n%s", js, text)
	}
	_, js, _ = call(t, cs, "read_memory", map[string]any{"scope": "dot", "query": "cookies"})
	_ = json.Unmarshal(js, &out)
	if len(out.Entries) != 1 || out.Entries[0].Kind != "finding" {
		t.Fatalf("filtered %s", js)
	}
	_, js, _ = call(t, cs, "read_memory", map[string]any{"limit": 1})
	_ = json.Unmarshal(js, &out)
	if len(out.Entries) != 1 || out.Total != 2 {
		t.Fatalf("limited %s", js)
	}
	if _, _, isErr := call(t, cs, "read_memory", map[string]any{"scope": "global-ish"}); !isErr {
		t.Fatal("unknown scope accepted")
	}
}

func TestProposeMemoryLandsAsAProposalAndNotifies(t *testing.T) {
	st := memStore(t)
	fe := &fakeEngine{dots: ipc.DotsResult{ProjectID: testPID}}
	d := inPane(fe)
	d.Memory = st
	cs := connect(t, d)
	text, js, isErr := call(t, cs, "propose_memory", map[string]any{"kind": "convention", "text": "API handlers return problem+json errors.", "paths": []string{"api/**"}})
	if isErr {
		t.Fatal(text)
	}
	var out ProposeMemoryOut
	if err := json.Unmarshal(js, &out); err != nil || out.Status != "proposed" || out.ID != "p:m1" {
		t.Fatalf("propose %s", js)
	}
	es, _ := st.List(testPID, memory.Filter{Scope: memory.ScopeProject, Statuses: []memory.Status{memory.StatusProposed}})
	if len(es) != 1 || es[0].Source != memory.SourceAgent || es[0].Evidence.Pane != "w1:p1" {
		t.Fatalf("stored %+v", es)
	}
	if fe.notified != 1 {
		t.Fatalf("engine notified %d times", fe.notified)
	}
	text, _, isErr = call(t, cs, "propose_memory", map[string]any{"kind": "note", "text": "deploy with sk-ant-api03-abcdefghijklmnopqrstuvwxyz012345"})
	if !isErr || !strings.Contains(text, "looks like a secret") || strings.Contains(text, "sk-ant") {
		t.Fatalf("secret: isError=%v %q", isErr, text)
	}
	for _, args := range []map[string]any{
		{"kind": "finding", "text": "x"},
		{"kind": "note", "text": strings.Repeat("a", MaxProposalChars+1)},
	} {
		if _, _, isErr := call(t, cs, "propose_memory", args); !isErr {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestMemoryToolsNeedMemoryAndAProject(t *testing.T) {
	cs := connect(t, inPane(&fakeEngine{dots: ipc.DotsResult{ProjectID: testPID}}))
	if text, _, _ := call(t, cs, "read_memory", map[string]any{}); !strings.Contains(text, "memory_enabled = false") {
		t.Fatalf("memory off: %q", text)
	}
	d := inPane(&fakeEngine{})
	d.Memory = memStore(t)
	if text, _, _ := call(t, connect(t, d), "propose_memory", map[string]any{"kind": "note", "text": "x"}); !strings.Contains(text, "project is unknown") {
		t.Fatalf("no project: %q", text)
	}
}

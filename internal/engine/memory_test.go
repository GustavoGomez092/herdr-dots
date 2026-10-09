package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// memBackend returns scripted Results (verdict + remember ops).
type memBackend struct {
	mu      sync.Mutex
	results []backend.Result // consumed in order; the last one repeats
	calls   []backend.Request
}

func (b *memBackend) Kind() string                { return "claude-cli" }
func (b *memBackend) Check(context.Context) error { return nil }
func (b *memBackend) Run(_ context.Context, r backend.Request) (backend.Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, r)
	res := backend.Result{Verdict: backend.Verdict{Severity: "info"}}
	if len(b.results) > 0 {
		res = b.results[0]
		if len(b.results) > 1 {
			b.results = b.results[1:]
		}
	}
	res.InputTokens, res.OutputTokens = 10, 5
	return res, nil
}
func (b *memBackend) set(rs ...backend.Result) { b.mu.Lock(); b.results = rs; b.mu.Unlock() }
func (b *memBackend) n() int                   { b.mu.Lock(); defer b.mu.Unlock(); return len(b.calls) }
func (b *memBackend) last() backend.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls[len(b.calls)-1]
}
func (b *memBackend) passes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, c := range b.calls {
		if c.Pass == backend.PassMemory {
			n++
		}
	}
	return n
}

const testProject = "g-0123456789abcdef"

type memOpts struct {
	cfg         func(*config.Config)
	lockTimeout time.Duration
	configPath  string                                   // config.toml re-read every 10ms; "" = never
	reader      func(state.Attachment) transcript.Reader // nil = r.readers by session
	resolve     func(string) (memory.Project, error)     // nil = testProject
}

type memRig struct {
	mu      sync.Mutex // guards readers
	srv     *herdrtest.Server
	e       *Engine
	be      *memBackend
	readers map[string]*fakeReader // by session value
	q       *queue.Store
	atts    *state.Attachments
	feed    *state.Feed
	mem     *memory.Store
}

func newMemRig(t *testing.T, o memOpts, ds ...dots.Dot) *memRig {
	t.Helper()
	srv := herdrtest.New(t)
	srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	dir := t.TempDir()
	q, _ := queue.Open(filepath.Join(dir, "queue.json"))
	atts, _ := state.OpenAttachments(filepath.Join(dir, "attachments.json"))
	usage, _ := state.OpenUsage(filepath.Join(dir, "usage.json"))
	feed, _ := state.OpenFeed(filepath.Join(dir, "feed.jsonl"))
	lim := memory.DefaultLimits()
	lim.LockTimeout = 2 * time.Second
	if o.lockTimeout > 0 {
		lim.LockTimeout = o.lockTimeout
	}
	mem := memory.Open(filepath.Join(dir, "memory"), lim)
	r := &memRig{srv: srv, be: &memBackend{}, readers: map[string]*fakeReader{"s1": {}}, q: q, atts: atts, feed: feed, mem: mem}
	cfg := config.Default()
	cfg.TurnDebounceMs = 0
	if o.cfg != nil {
		o.cfg(&cfg)
	}
	reader := func(a state.Attachment) transcript.Reader {
		r.mu.Lock()
		defer r.mu.Unlock()
		if rd, ok := r.readers[a.SessionValue]; ok {
			return rd
		}
		return &fakeReader{}
	}
	if o.reader != nil {
		reader = o.reader
	}
	resolve := o.resolve
	if resolve == nil {
		resolve = func(string) (memory.Project, error) {
			return memory.Project{ID: testProject, Name: "repo", Root: "/repo"}, nil
		}
	}
	r.e = New(Deps{
		Herdr:          &herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"},
		Backend:        func(string) (backend.Backend, error) { return r.be, nil },
		Reader:         reader,
		ConfigPath:     o.configPath,
		ConfigCheck:    10 * time.Millisecond,
		LoadDots:       func() ([]dots.Dot, []dots.LoadError) { return ds, nil },
		SaveDot:        func(dots.Dot) error { return nil },
		Config:         cfg,
		Queue:          q,
		Attachments:    atts,
		Usage:          usage,
		Feed:           feed,
		PollInterval:   20 * time.Millisecond,
		Memory:         mem,
		ResolveProject: resolve,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(r.e.Wait)
	t.Cleanup(cancel)
	r.e.Start(ctx)
	return r
}

func (r *memRig) reader(session string) *fakeReader {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readers[session]
}

func (r *memRig) setReader(session string, rd *fakeReader) {
	r.mu.Lock()
	r.readers[session] = rd
	r.mu.Unlock()
}

func (r *memRig) attach(t *testing.T, slugs ...string) {
	t.Helper()
	if err := r.e.Attach(context.Background(), pane, slugs); err != nil {
		t.Fatal(err)
	}
	if !r.srv.WaitSubscribed(pane, time.Second) {
		t.Fatal("engine did not subscribe to the pane")
	}
}

func (r *memRig) turnEnds(session string, tr transcript.Turn) {
	r.reader(session).add(tr)
	r.srv.Push(pane, herdr.StatusWorking)
	r.srv.Push(pane, herdr.StatusIdle)
}

func (r *memRig) entries(f memory.Filter) []memory.Entry {
	es, _ := r.mem.List(testProject, f)
	return es
}

func (r *memRig) hasFeed(outcome, detail string) bool {
	for _, e := range r.feed.Recent(pane, 50) {
		if e.Outcome == outcome && strings.Contains(e.Detail, detail) {
			return true
		}
	}
	return false
}

func (r *memRig) dotAtt(slug string) state.DotAttachment {
	a, _ := r.atts.Get(pane)
	if da := a.Find(slug); da != nil {
		return *da
	}
	return state.DotAttachment{}
}

var (
	jwtFirst = backend.Verdict{Relevant: true, Severity: "critical", Message: "JWT stored in localStorage is readable by any XSS payload; move it to an httpOnly cookie."}
	jwtAgain = backend.Verdict{Relevant: true, Severity: "critical", Message: "The JWT is stored in localStorage, which any XSS payload can read; move it to an httpOnly cookie."}
)

func TestAttachResolvesAndRecordsProject(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	if a, _ := r.atts.Get(pane); a.ProjectID != testProject || a.ProjectName != "repo" {
		t.Fatalf("attachment %+v", a)
	}
	eventually(t, "meta.json records the project", func() bool {
		ps, _ := r.mem.Projects()
		return len(ps) == 1 && ps[0].Name == "repo" && slices.Contains(ps[0].Roots, "/repo")
	})
	eventually(t, "snapshot shows the project", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.ProjectID == testProject && p.ProjectName == "repo"
	})
}

func TestReviewCarriesMemoryBlockAndTouchesIt(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	if _, err := r.mem.Add(testProject, memory.Entry{Scope: memory.ScopeProject, Kind: memory.KindDecision,
		Text: "Auth tokens live in httpOnly cookies, never localStorage.", Source: memory.SourceUser, Pinned: true}); err != nil {
		t.Fatal(err)
	}
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "dot ran", func() bool { return r.be.n() == 1 })
	req := r.be.last()
	if !strings.HasPrefix(req.Memory, memory.RenderHeader) || !strings.Contains(req.Memory, "[p:m1] decision (developer): Auth tokens live in httpOnly cookies") {
		t.Fatalf("memory block:\n%s", req.Memory)
	}
	if !req.Learn || req.Pass != backend.PassReview {
		t.Fatalf("request %+v", req)
	}
	eventually(t, "entry touched", func() bool {
		es := r.entries(memory.Filter{Scope: memory.ScopeProject})
		return len(es) == 1 && es[0].Uses == 1 && !es[0].LastUsedAt.IsZero()
	})
}

func TestRememberOpsBecomeEntriesAndLearnedFeed(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: backend.Verdict{Severity: "info"}, Remember: []backend.MemoryOp{
		{Op: "add", Scope: "dot", Kind: "note", Text: "The team prefers zod over hand-written validators."},
		{Op: "add", Scope: "project", Kind: "convention", Text: "Components live in src/ui/<area>/<Name>.tsx."},
	}})
	r.reader("s1").add(transcript.Turn{User: "add a form"})
	r.attach(t, "security")
	eventually(t, "learned feed entry", func() bool { return r.hasFeed(state.OutcomeLearned, "learned 2") })
	d := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"})
	p := r.entries(memory.Filter{Scope: memory.ScopeProject})
	if len(d) != 1 || d[0].Status != memory.StatusActive || d[0].Source != memory.SourceModel || d[0].Evidence.Session != "s1" {
		t.Fatalf("dot memory %+v", d)
	}
	if len(p) != 1 || p[0].Status != memory.StatusProposed || p[0].Dot != "security" {
		t.Fatalf("project memory %+v", p)
	}
	eventually(t, "snapshot counts", func() bool {
		pv, _ := r.e.Snapshot().Pane(pane)
		return pv.MemoryEntries == 2 && pv.MemoryProposals == 1 && pv.Dots[0].MemoryEntries == 1
	})
}

func TestDismissedAdviceIsSuppressedNextTime(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: jwtFirst}, backend.Result{Verdict: jwtAgain})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "first card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if err := r.e.Dismiss(r.q.Pending(pane)[0].ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "dismissed entry", func() bool {
		return len(r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security", Kinds: []memory.Kind{memory.KindDismissed}})) == 1
	})
	r.turnEnds("s1", transcript.Turn{User: "refactor auth"})
	eventually(t, "suppressed feed entry", func() bool { return r.hasFeed(state.OutcomeSuppressed, "similar to d:m1") })
	if n := len(r.q.Pending(pane)); n != 0 {
		t.Fatalf("a suppressed verdict must not make a card, got %d", n)
	}
	led, _ := r.mem.Ledger(testProject)
	if len(led) != 1 || led[0].Kind != memory.OutcomeDismissed || led[0].Session != "s1" {
		t.Fatalf("ledger %+v", led)
	}
}

// Review Focus 4: a long re-raise whose first sentence repeats dismissed advice.
func TestLongReRaiseOfDismissedAdviceIsSuppressed(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: "Rate limiting is missing on /login."}},
		backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: "Rate limiting is missing on /login. An attacker can brute-force passwords at full speed; add a token bucket per IP and per account, return 429 with Retry-After, and log lockouts so support can unlock real users."}})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "first card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.e.Dismiss(r.q.Pending(pane)[0].ID)
	eventually(t, "dismissed entry", func() bool { return len(r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"})) == 1 })
	r.turnEnds("s1", transcript.Turn{User: "tweak login"})
	eventually(t, "suppressed", func() bool { return r.hasFeed(state.OutcomeSuppressed, "d:m1") })
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("the re-raise made a card")
	}
}

func TestSummonIsNeverSuppressed(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: jwtFirst}, backend.Result{Verdict: jwtAgain})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "first card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.e.Dismiss(r.q.Pending(pane)[0].ID)
	eventually(t, "dismissed entry", func() bool { return len(r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"})) == 1 })
	if err := r.e.Summon(context.Background(), pane, "security", "is the token safe?"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "summoned card", func() bool { return len(r.q.Pending(pane)) == 1 })
}

func TestEditedSendRemembersEditedText(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: critical})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	id := r.q.Pending(pane)[0].ID
	if err := r.e.Edit(id, "Store the session token in an httpOnly cookie. Never in localStorage."); err != nil {
		t.Fatal(err)
	}
	if err := r.e.SendCards([]string{id}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "finding from the edited text", func() bool {
		es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security", Kinds: []memory.Kind{memory.KindFinding}})
		return len(es) == 1 && es[0].Text == "Store the session token in an httpOnly cookie."
	})
	led, _ := r.mem.Ledger(testProject)
	if len(led) != 1 || led[0].Kind != memory.OutcomeEdited || led[0].Body != critical.Message || !strings.HasPrefix(led[0].Edited, "Store the session") {
		t.Fatalf("ledger %+v", led)
	}
}

func TestObserveDotRunsOneMemoryPassAndNoCard(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	d.Memory.EveryTurns = 2
	r := newMemRig(t, memOpts{}, d)
	r.be.set(backend.Result{Remember: []backend.MemoryOp{{Op: "add", Kind: "note", Text: "Headlines use sentence case."}}})
	r.attach(t, "copy")
	r.turnEnds("s1", transcript.Turn{User: "write the hero"})
	eventually(t, "turn 1 consumed", func() bool { return r.dotAtt("copy").LastSeenTurn == 1 })
	time.Sleep(50 * time.Millisecond)
	if r.be.n() != 0 {
		t.Fatal("no memory pass after one turn")
	}
	r.turnEnds("s1", transcript.Turn{User: "write the footer"})
	eventually(t, "one memory pass", func() bool { return r.be.n() == 1 })
	eventually(t, "pass bookkeeping", func() bool {
		da := r.dotAtt("copy")
		return da.MemoryPasses == 1 && da.MemorySeenTurn == 2
	})
	req := r.be.last()
	if req.Pass != backend.PassMemory || req.RepoRead || req.Force || len(req.Turns) != 2 {
		t.Fatalf("memory pass request %+v", req)
	}
	eventually(t, "learned", func() bool { return r.hasFeed(state.OutcomeLearned, "learned 1") })
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("a memory pass never makes a card")
	}
	if es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "copy"}); len(es) != 1 {
		t.Fatalf("entries %+v", es)
	}
	time.Sleep(60 * time.Millisecond)
	if r.be.n() != 1 {
		t.Fatalf("passes %d, want exactly 1", r.be.n())
	}
}

func TestMaxPassesPerSessionStopsPasses(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	d.Memory.EveryTurns, d.Memory.MaxPassesPerSession = 1, 1
	r := newMemRig(t, memOpts{}, d)
	r.attach(t, "copy")
	for _, u := range []string{"one", "two", "three"} {
		r.turnEnds("s1", transcript.Turn{User: u})
	}
	eventually(t, "three turns consumed", func() bool { return r.dotAtt("copy").LastSeenTurn == 3 })
	eventually(t, "first pass", func() bool { return r.be.passes() == 1 })
	time.Sleep(80 * time.Millisecond)
	if r.be.passes() != 1 {
		t.Fatalf("passes %d, want 1", r.be.passes())
	}
}

func TestSessionBudgetStopsPasses(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	d.Memory.EveryTurns = 1
	r := newMemRig(t, memOpts{cfg: func(c *config.Config) { c.SessionBudgetCalls = 1 }}, d)
	r.attach(t, "copy")
	for _, u := range []string{"one", "two", "three"} {
		r.turnEnds("s1", transcript.Turn{User: u})
	}
	eventually(t, "three turns consumed", func() bool { return r.dotAtt("copy").LastSeenTurn == 3 })
	eventually(t, "first pass", func() bool { return r.be.n() == 1 })
	time.Sleep(80 * time.Millisecond)
	if r.be.n() != 1 {
		t.Fatalf("calls %d, want 1 (session budget)", r.be.n())
	}
}

// Review Focus 3: after /clear the turn count restarts; learning must resume.
func TestObserveLearningResumesAfterNewSession(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	d.Memory.EveryTurns = 2
	r := newMemRig(t, memOpts{}, d)
	r.attach(t, "copy")
	r.turnEnds("s1", transcript.Turn{User: "one"})
	r.turnEnds("s1", transcript.Turn{User: "two"})
	eventually(t, "first pass", func() bool { return r.dotAtt("copy").MemorySeenTurn == 2 })
	fresh := &fakeReader{}
	r.setReader("s2", fresh)
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.AgentSession = &herdr.AgentSession{Kind: "id", Value: "s2"}
	r.srv.SetPane(p)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "session followed", func() bool { a, _ := r.atts.Get(pane); return a.SessionValue == "s2" })
	r.turnEnds("s2", transcript.Turn{User: "fresh one"})
	r.turnEnds("s2", transcript.Turn{User: "fresh two"})
	eventually(t, "second pass in the new session", func() bool { return r.be.passes() == 2 })
	eventually(t, "counter tracks the new session", func() bool { return r.dotAtt("copy").MemorySeenTurn == 2 })
}

// Ruling 12's "per session": the pass budget restarts with a new agent
// session, like the call budgets (reset on a new session since the v1 fix
// wave).
func TestMemoryPassBudgetResetsOnNewSession(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	d.Memory.EveryTurns, d.Memory.MaxPassesPerSession = 1, 1
	r := newMemRig(t, memOpts{}, d)
	r.attach(t, "copy")
	r.turnEnds("s1", transcript.Turn{User: "one"})
	eventually(t, "first pass", func() bool { return r.dotAtt("copy").MemoryPasses == 1 })
	r.turnEnds("s1", transcript.Turn{User: "two"})
	eventually(t, "turn two consumed", func() bool { return r.dotAtt("copy").LastSeenTurn == 2 })
	time.Sleep(50 * time.Millisecond)
	if r.be.passes() != 1 {
		t.Fatalf("passes %d, want 1 (max_passes_per_session = 1)", r.be.passes())
	}
	r.setReader("s2", &fakeReader{})
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.AgentSession = &herdr.AgentSession{Kind: "id", Value: "s2"}
	r.srv.SetPane(p)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "session followed, pass budget reset", func() bool {
		a, _ := r.atts.Get(pane)
		return a.SessionValue == "s2" && r.dotAtt("copy").MemoryPasses == 0
	})
	r.turnEnds("s2", transcript.Turn{User: "fresh"})
	eventually(t, "a pass in the new session", func() bool { return r.be.passes() == 2 })
}

// config.toml reloads at runtime (v1 fix wave): memory_enabled = false takes
// memory out of the next run without an engine restart.
func TestMemoryKillSwitchAppliesAtRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	r := newMemRig(t, memOpts{configPath: path}, dot("Security", dots.ModeQueue))
	if _, err := r.mem.Add(testProject, memory.Entry{Scope: memory.ScopeProject, Kind: memory.KindDecision,
		Text: "Auth tokens live in httpOnly cookies.", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "first review", func() bool { return r.be.n() == 1 })
	if req := r.be.last(); req.Memory == "" || !req.Learn {
		t.Fatalf("memory on: %+v", req)
	}
	if err := os.WriteFile(path, []byte("turn_debounce_ms = 0\nmemory_enabled = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	eventually(t, "config reloaded", func() bool { return !r.e.config().MemoryEnabled })
	r.turnEnds("s1", transcript.Turn{User: "add logout"})
	eventually(t, "second review", func() bool { return r.be.n() == 2 })
	if req := r.be.last(); req.Memory != "" || req.Learn {
		t.Fatalf("memory_enabled = false must take memory out of the run: %+v", req)
	}
	if err := r.e.Remember("any", memory.AsFinding, ""); err == nil || !strings.Contains(err.Error(), "memory_enabled") {
		t.Fatalf("Remember with memory off: %v", err)
	}
}

// A scrollback fallback upgraded to the transcript moves MemorySeenTurn with
// LastSeenTurn: scrollback turn numbers don't carry over to the transcript.
func TestScrollbackUpgradeMovesMemorySeenTurn(t *testing.T) {
	var ready atomic.Bool
	claude := &fakeReader{}
	for _, u := range []string{"one", "two", "three", "four"} {
		claude.add(transcript.Turn{User: u})
	}
	d := dot("Copy", dots.ModeObserve)
	d.Memory.EveryTurns = 10 // no memory pass in this test
	r := newMemRig(t, memOpts{reader: func(state.Attachment) transcript.Reader {
		if ready.Load() {
			return claude
		}
		return &fakeReader{source: transcript.SourceScrollback}
	}}, d)
	r.attach(t, "copy")
	ready.Store(true) // the transcript now exists
	r.srv.Push(pane, herdr.StatusWorking)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "upgraded and caught up", func() bool {
		a, _ := r.atts.Get(pane)
		return a.Source == transcript.SourceClaude && r.dotAtt("copy").LastSeenTurn == 4
	})
	if da := r.dotAtt("copy"); da.MemorySeenTurn != 3 {
		t.Fatalf("MemorySeenTurn %d, want 3 (every transcript turn but the last counts as seen)", da.MemorySeenTurn)
	}
}

func TestRememberCommandEachKind(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: critical})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	id := r.q.Pending(pane)[0].ID
	for _, c := range []struct{ as, reason string }{{"finding", ""}, {"rule", "internal tool"}, {"convention", ""}, {"global", ""}} {
		raw, _ := json.Marshal(ipc.RememberArgs{CardID: id, As: c.as, Reason: c.reason})
		if err := r.e.Handle(context.Background(), ipc.CmdRemember, raw); err != nil {
			t.Fatalf("%s: %v", c.as, err)
		}
	}
	d := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"})
	if len(d) != 2 || d[0].Kind != memory.KindFinding || d[1].Kind != memory.KindDismissed || d[1].Text != "JWT in localStorage"+memory.ReasonSep+"internal tool)" {
		t.Fatalf("dot memory %+v", d)
	}
	if p := r.entries(memory.Filter{Scope: memory.ScopeProject}); len(p) != 1 || p[0].Kind != memory.KindConvention || p[0].Status != memory.StatusActive {
		t.Fatalf("project memory %+v", p)
	}
	if g, _ := r.mem.List("", memory.Filter{Scope: memory.ScopeGlobal, Dot: "security"}); len(g) != 1 || g[0].Kind != memory.KindPreference {
		t.Fatalf("global memory %+v", g)
	}
	raw, _ := json.Marshal(ipc.RememberArgs{CardID: id, As: "forever"})
	if err := r.e.Handle(context.Background(), ipc.CmdRemember, raw); err == nil {
		t.Fatal("an unknown kind must fail")
	}
}

func TestMemoryChangedRefreshesCounts(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	for _, text := range []string{"first developer note", "second developer note"} {
		r.mem.Add(testProject, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: text})
	}
	if err := r.e.Handle(context.Background(), ipc.CmdMemoryChanged, nil); err != nil {
		t.Fatal(err)
	}
	pv, _ := r.e.Snapshot().Pane(pane)
	if pv.MemoryEntries != 2 || pv.Dots[0].MemoryEntries != 2 {
		t.Fatalf("snapshot %+v", pv)
	}
}

func TestLockedMemoryNeverBlocksAReview(t *testing.T) {
	r := newMemRig(t, memOpts{lockTimeout: 100 * time.Millisecond}, dot("Security", dots.ModeQueue))
	dir := filepath.Join(r.mem.Dir, testProject)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.TryLock(filepath.Join(dir, "memory.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	r.be.set(backend.Result{Verdict: critical})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "card despite the lock", func() bool { return len(r.q.Pending(pane)) == 1 })
	if req := r.be.last(); req.Memory != "" {
		t.Fatalf("a locked store means a run without memory, got:\n%s", req.Memory)
	}
}

func TestAttachmentWithoutProjectResolvesLazily(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.reader("s1").add(transcript.Turn{User: "first"})
	r.attach(t, "security")
	eventually(t, "first run", func() bool { return r.be.n() == 1 })
	_ = r.atts.Update(pane, func(a *state.Attachment) { a.ProjectID, a.ProjectName = "", "" }) // as saved by v1
	r.turnEnds("s1", transcript.Turn{User: "next"})
	eventually(t, "project resolved on the next run", func() bool { a, _ := r.atts.Get(pane); return a.ProjectID == testProject })
}

func TestNilMemoryKeepsV1Requests(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "dot ran", func() bool { return r.be.n() == 1 })
	if req := r.be.last(); req.Memory != "" || req.Learn || req.Pass != backend.PassReview {
		t.Fatalf("v1 request changed: %+v", req)
	}
	if err := r.e.Remember("1", memory.AsFinding, ""); err == nil {
		t.Fatal("Remember must fail when memory is off")
	}
}

func TestDecideMemory(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	two := []transcript.Turn{{Index: 1}, {Index: 2}}
	base := MemoryInput{Dot: d, NewTurns: append(two, transcript.Turn{Index: 3}, transcript.Turn{Index: 4}, transcript.Turn{Index: 5}), MemoryEnabled: true, PaneBudget: 120}
	if !DecideMemory(base) {
		t.Fatal("5 new turns, defaults: run")
	}
	cases := map[string]func(*MemoryInput){
		"global off":    func(in *MemoryInput) { in.MemoryEnabled = false },
		"dot off":       func(in *MemoryInput) { in.Dot.Memory.Enabled = false },
		"learn off":     func(in *MemoryInput) { in.Dot.Memory.Learn = false },
		"paused all":    func(in *MemoryInput) { in.PausedAll = true },
		"paused":        func(in *MemoryInput) { in.Att.Paused = true },
		"too few turns": func(in *MemoryInput) { in.NewTurns = two },
		"passes used":   func(in *MemoryInput) { in.Att.MemoryPasses = 10 },
		"dot budget":    func(in *MemoryInput) { in.DotCalls = 40 },
		"pane budget":   func(in *MemoryInput) { in.PaneCalls = 120 },
		"all its own": func(in *MemoryInput) {
			for i := range in.NewTurns {
				in.NewTurns[i].FromDots = []string{"Copy"}
			}
		},
	}
	for name, mod := range cases {
		in := base
		in.NewTurns = slices.Clone(base.NewTurns)
		mod(&in)
		if DecideMemory(in) {
			t.Errorf("%s: want no memory pass", name)
		}
	}
}

// The 3-ops-per-review cap is enforced by backend.ParseReply; the engine
// enforces it again, so a backend that returns more (an API backend that
// skipped ParseReply, a fake) can never apply more than 3 from one review.
func TestReviewAppliesAtMostThreeOps(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	var ops []backend.MemoryOp
	for _, text := range []string{"First distinct note about zod schemas.", "Second note on the router layout.",
		"Third note about cookie flags.", "Fourth note regarding CSP headers.", "Fifth note on rate limiting buckets."} {
		ops = append(ops, backend.MemoryOp{Op: "add", Scope: "dot", Kind: "note", Text: text})
	}
	r.be.set(backend.Result{Verdict: backend.Verdict{Severity: "info"}, Remember: ops})
	r.reader("s1").add(transcript.Turn{User: "add a form"})
	r.attach(t, "security")
	eventually(t, "learned feed entry", func() bool { return r.hasFeed(state.OutcomeLearned, "learned") })
	if !r.hasFeed(state.OutcomeLearned, "learned 3") {
		t.Fatalf("feed %+v", r.feed.Recent(pane, 10))
	}
	if es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"}); len(es) != backend.MaxReviewOps {
		t.Fatalf("a review applied %d ops, want at most %d: %+v", len(es), backend.MaxReviewOps, es)
	}
}

// A memory pass keeps the larger per-pass cap (5), not the review cap.
func TestMemoryPassAppliesAtMostFiveOps(t *testing.T) {
	d := dot("Copy", dots.ModeObserve)
	d.Memory.EveryTurns = 1
	r := newMemRig(t, memOpts{}, d)
	var ops []backend.MemoryOp
	for _, text := range []string{"Alpha headline rule for hero.", "Bravo footer copy tone.", "Charlie button labels verbs.",
		"Delta error message voice.", "Echo empty state wording.", "Foxtrot tooltip length limit.", "Golf legal footnote style."} {
		ops = append(ops, backend.MemoryOp{Op: "add", Kind: "note", Text: text})
	}
	r.be.set(backend.Result{Remember: ops})
	r.attach(t, "copy")
	r.turnEnds("s1", transcript.Turn{User: "write the hero"})
	eventually(t, "learned", func() bool { return r.hasFeed(state.OutcomeLearned, "learned") })
	if es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "copy"}); len(es) != backend.MaxPassOps {
		t.Fatalf("a memory pass applied %d ops, want %d", len(es), backend.MaxPassOps)
	}
}

// memory_enabled = false: Attach runs no git (ResolveProject) and records no
// project; no meta.json is written.
func TestAttachWithMemoryDisabledResolvesNoProject(t *testing.T) {
	var calls atomic.Int32
	r := newMemRig(t, memOpts{
		cfg: func(c *config.Config) { c.MemoryEnabled = false },
		resolve: func(string) (memory.Project, error) {
			calls.Add(1)
			return memory.Project{ID: testProject, Name: "repo", Root: "/repo"}, nil
		},
	}, dot("Security", dots.ModeQueue))
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "dot ran", func() bool { return r.be.n() == 1 })
	r.turnEnds("s1", transcript.Turn{User: "next"})
	eventually(t, "second run", func() bool { return r.be.n() == 2 })
	if req := r.be.last(); req.Memory != "" || req.Learn {
		t.Fatalf("memory off: %+v", req)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("ResolveProject ran %d times with memory disabled", n)
	}
	if a, _ := r.atts.Get(pane); a.ProjectID != "" {
		t.Fatalf("attachment %+v", a)
	}
	if ps, _ := r.mem.Projects(); len(ps) != 0 {
		t.Fatalf("projects %+v", ps)
	}
}

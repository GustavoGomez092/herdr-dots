package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// proBackend answers per dot (by DotName) and can hold runs of one pass.
type proBackend struct {
	mu       sync.Mutex
	def      backend.Result            // for dots without their own result
	byDot    map[string]backend.Result // by DotName
	calls    []backend.Request
	fails    int
	gate     chan struct{}
	gatePass string // with gate: runs of this pass block until the gate closes
	gateAll  bool   // with gate: every run blocks
}

func (b *proBackend) Kind() string                { return "claude-cli" }
func (b *proBackend) Check(context.Context) error { return nil }
func (b *proBackend) Run(_ context.Context, r backend.Request) (backend.Result, error) {
	b.mu.Lock()
	b.calls = append(b.calls, r)
	gate := b.gate
	hold := gate != nil && (b.gateAll || r.Pass == b.gatePass)
	b.mu.Unlock()
	if hold {
		<-gate
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fails > 0 {
		b.fails--
		return backend.Result{}, errors.New("backend unavailable")
	}
	res, ok := b.byDot[r.DotName]
	if !ok {
		res = b.def
	}
	if res.Verdict.Severity == "" {
		res.Verdict.Severity = "info"
	}
	res.InputTokens, res.OutputTokens = 10, 5
	return res, nil
}

func (b *proBackend) set(res backend.Result) { b.mu.Lock(); b.def = res; b.mu.Unlock() }
func (b *proBackend) setFor(name string, res backend.Result) {
	b.mu.Lock()
	if b.byDot == nil {
		b.byDot = map[string]backend.Result{}
	}
	b.byDot[name] = res
	b.mu.Unlock()
}
func (b *proBackend) failNext(n int) { b.mu.Lock(); b.fails = n; b.mu.Unlock() }

// hold blocks runs of pass ("" = every run) until the returned gate closes.
func (b *proBackend) hold(pass string, all bool) chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gate, b.gatePass, b.gateAll = make(chan struct{}), pass, all
	return b.gate
}
func (b *proBackend) n() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.calls) }
func (b *proBackend) last() backend.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls[len(b.calls)-1]
}

// runs returns the recorded requests of one pass.
func (b *proBackend) runs(pass string) []backend.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []backend.Request
	for _, c := range b.calls {
		if c.Pass == pass {
			out = append(out, c)
		}
	}
	return out
}

// proClock is a settable clock for proactive timers.
type proClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *proClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *proClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type proOpts struct {
	cfg        func(*config.Config)
	clock      *proClock     // nil = a clock at proStart
	configPath string        // config.toml re-read every 10ms; "" = never
	noDaily    bool          // Deps.Daily = nil
	deps       func(d *Deps) // last-minute Deps changes
	cwd        string        // the pane's cwd; "" = "/repo"
	shared     string        // memory, daily.json and inbox.json under this dir (two engines); "" = private
	prepare    func(*memory.Store)
}

// proStart is a Wednesday morning, local time.
var proStart = time.Date(2026, 10, 21, 10, 0, 0, 0, time.Local)

// proRig is a memory-enabled engine with a daily store, an inbox and a fake
// clock, on one pane ("w1:p1", project testProject).
type proRig struct {
	mu      sync.Mutex
	srv     *herdrtest.Server
	e       *Engine
	be      *proBackend
	readers map[string]*fakeReader
	q       *queue.Store
	atts    *state.Attachments
	usage   *state.Usage
	feed    *state.Feed
	mem     *memory.Store
	daily   *state.Daily
	inbox   *queue.Inbox
	clock   *proClock
	dir     string
	dots    []dots.Dot // what LoadDots returns; setDots + ReloadDots changes it
}

func (r *proRig) setDots(ds ...dots.Dot) {
	r.mu.Lock()
	r.dots = ds
	r.mu.Unlock()
	r.e.ReloadDots()
}

func newProRig(t *testing.T, o proOpts, ds ...dots.Dot) *proRig {
	t.Helper()
	srv := herdrtest.New(t)
	cwd := o.cwd
	if cwd == "" {
		cwd = "/repo"
	}
	srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: cwd,
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	dir := t.TempDir()
	q, _ := queue.Open(filepath.Join(dir, "session", "queue.json"))
	atts, _ := state.OpenAttachments(filepath.Join(dir, "session", "attachments.json"))
	usage, _ := state.OpenUsage(filepath.Join(dir, "session", "usage.json"))
	feed, _ := state.OpenFeed(filepath.Join(dir, "session", "feed.jsonl"))
	shared := dir
	if o.shared != "" {
		shared = o.shared
	}
	mem := memory.Open(filepath.Join(shared, "memory"), memory.DefaultLimits())
	daily, _ := state.OpenDaily(filepath.Join(shared, "daily.json"))
	inbox := queue.OpenInbox(filepath.Join(shared, "inbox.json"))
	if o.prepare != nil {
		o.prepare(mem)
	}
	clk := o.clock
	if clk == nil {
		clk = &proClock{t: proStart}
	}
	r := &proRig{srv: srv, be: &proBackend{}, readers: map[string]*fakeReader{"s1": {}}, q: q, atts: atts, usage: usage,
		feed: feed, mem: mem, daily: daily, inbox: inbox, clock: clk, dir: dir, dots: ds}
	cfg := config.Default()
	cfg.TurnDebounceMs = 0
	if o.cfg != nil {
		o.cfg(&cfg)
	}
	deps := Deps{
		Herdr:   &herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"},
		Backend: func(string) (backend.Backend, error) { return r.be, nil },
		Reader: func(a state.Attachment) transcript.Reader {
			r.mu.Lock()
			defer r.mu.Unlock()
			if rd, ok := r.readers[a.SessionValue]; ok {
				return rd
			}
			return &fakeReader{}
		},
		ConfigPath:  o.configPath,
		ConfigCheck: 10 * time.Millisecond,
		LoadDots: func() ([]dots.Dot, []dots.LoadError) {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.dots, nil
		},
		SaveDot:      func(dots.Dot) error { return nil },
		Config:       cfg,
		Queue:        q,
		Attachments:  atts,
		Usage:        usage,
		Feed:         feed,
		PollInterval: 20 * time.Millisecond,
		Memory:       mem,
		ResolveProject: func(string) (memory.Project, error) {
			return memory.Project{ID: testProject, Name: "repo", Root: cwd}, nil
		},
		Clock: clk.Now,
		Daily: daily,
		Inbox: inbox,
	}
	if o.noDaily {
		deps.Daily = nil
	}
	if o.deps != nil {
		o.deps(&deps)
	}
	r.e = New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(r.e.Wait)
	t.Cleanup(cancel)
	r.e.Start(ctx)
	return r
}

func (r *proRig) attach(t *testing.T, slugs ...string) {
	t.Helper()
	if err := r.e.Attach(context.Background(), pane, slugs); err != nil {
		t.Fatal(err)
	}
	if !r.srv.WaitSubscribed(pane, time.Second) {
		t.Fatal("engine did not subscribe to the pane")
	}
}

func (r *proRig) reader() *fakeReader {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readers["s1"]
}

func (r *proRig) turnEnds(tr transcript.Turn) {
	r.reader().add(tr)
	r.srv.Push(pane, herdr.StatusWorking)
	r.srv.Push(pane, herdr.StatusIdle)
}

func (r *proRig) hasFeed(paneID, outcome, detail string) bool {
	for _, e := range r.feed.Recent(paneID, 50) {
		if e.Outcome == outcome && strings.Contains(e.Detail, detail) {
			return true
		}
	}
	return false
}

func (r *proRig) toasts() []string {
	var out []string
	for _, c := range r.srv.Calls("notification.show") {
		out = append(out, c.Params["body"].(string))
	}
	return out
}

func (r *proRig) today(t *testing.T) state.Day {
	t.Helper()
	d, err := r.daily.Today(r.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (r *proRig) dotState(slug string) (s struct{ Next, Background, Status string }) {
	p, _ := r.e.Snapshot().Pane(pane)
	for _, d := range p.Dots {
		if d.Slug == slug {
			s.Next, s.Background, s.Status = d.Next, d.Background, d.Status
		}
	}
	return s
}

// proDot is a queue-mode dot with every proactive feature off.
func proDot(name string) dots.Dot { return dot(name, dots.ModeQueue) }

func proResult(sev, msg string) backend.Result {
	return backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: sev, Message: msg}}
}

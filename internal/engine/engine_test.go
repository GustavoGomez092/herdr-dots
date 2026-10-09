package engine

import (
	"context"
	"errors"
	"os"
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
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

type fakeBackend struct {
	mu       sync.Mutex
	verdicts []backend.Verdict // consumed in order; the last one repeats
	err      error
	calls    []backend.Request
	gate     chan struct{} // optional: runs block (after being recorded) until closed
}

func (f *fakeBackend) Kind() string                { return "claude-cli" }
func (f *fakeBackend) Check(context.Context) error { return nil }
func (f *fakeBackend) n() int                      { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }
func (f *fakeBackend) last() backend.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}
func (f *fakeBackend) set(vs ...backend.Verdict) { f.mu.Lock(); f.verdicts = vs; f.mu.Unlock() }
func (f *fakeBackend) fail(err error)            { f.mu.Lock(); f.err = err; f.mu.Unlock() }
func (f *fakeBackend) Run(_ context.Context, r backend.Request) (backend.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, r)
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate // the run is in flight until the test closes gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return backend.Result{}, f.err
	}
	v := backend.Verdict{Relevant: false, Severity: "info"}
	if len(f.verdicts) > 0 {
		v = f.verdicts[0]
		if len(f.verdicts) > 1 {
			f.verdicts = f.verdicts[1:]
		}
	}
	return backend.Result{Verdict: v, InputTokens: 10, OutputTokens: 5}, nil
}

type fakeReader struct {
	mu    sync.Mutex
	turns []transcript.Turn

	// Optional gate: after arm(), the next Turns call takes its snapshot, signals
	// entered and blocks until release is closed (a stale in-flight read).
	armed   bool
	entered chan struct{}
	release chan struct{}
}

func (r *fakeReader) arm() {
	r.mu.Lock()
	r.armed, r.entered, r.release = true, make(chan struct{}), make(chan struct{})
	r.mu.Unlock()
}

func (r *fakeReader) Source() string { return transcript.SourceClaude }
func (r *fakeReader) Turns(context.Context) ([]transcript.Turn, error) {
	r.mu.Lock()
	ts := append([]transcript.Turn(nil), r.turns...)
	armed, entered, release := r.armed, r.entered, r.release
	r.armed = false
	r.mu.Unlock()
	if armed {
		close(entered)
		<-release
	}
	return ts, nil
}
func (r *fakeReader) add(t transcript.Turn) {
	r.mu.Lock()
	t.Index = len(r.turns) + 1
	r.turns = append(r.turns, t)
	r.mu.Unlock()
}

type rig struct {
	mu      sync.Mutex // guards readers
	srv     *herdrtest.Server
	e       *Engine
	be      *fakeBackend
	readers map[string]*fakeReader // by session value
	q       *queue.Store
	atts    *state.Attachments
	usage   *state.Usage
	saved   []dots.Dot
}

const pane = "w1:p1"

func dot(name string, mode dots.Mode) dots.Dot {
	d := dots.New(name)
	d.Purpose = "purpose of " + name
	d.Delivery.Mode = mode
	return d
}

func newRig(t *testing.T, ds ...dots.Dot) *rig {
	t.Helper()
	return newRigWith(t, nil, ds...)
}

// newRigWith lets a test adjust the engine's Deps before it starts.
func newRigWith(t *testing.T, opt func(*Deps), ds ...dots.Dot) *rig {
	t.Helper()
	srv := herdrtest.New(t)
	srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	dir := t.TempDir()
	q, _ := queue.Open(filepath.Join(dir, "queue.json"))
	atts, _ := state.OpenAttachments(filepath.Join(dir, "attachments.json"))
	usage, _ := state.OpenUsage(filepath.Join(dir, "usage.json"))
	feed, _ := state.OpenFeed(filepath.Join(dir, "feed.jsonl"))
	r := &rig{srv: srv, be: &fakeBackend{}, readers: map[string]*fakeReader{"s1": {}}, q: q, atts: atts, usage: usage}
	cfg := config.Default()
	cfg.TurnDebounceMs = 0
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
		LoadDots:     func() ([]dots.Dot, []dots.LoadError) { return ds, nil },
		SaveDot:      func(d dots.Dot) error { r.saved = append(r.saved, d); return nil },
		Config:       cfg,
		Queue:        q,
		Attachments:  atts,
		Usage:        usage,
		Feed:         feed,
		PollInterval: 20 * time.Millisecond,
	}
	if opt != nil {
		opt(&deps)
	}
	r.e = New(deps)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(r.e.Wait) // runs after cancel: no engine goroutine may outlive the test's temp dir
	t.Cleanup(cancel)
	r.e.Start(ctx)
	return r
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func (r *rig) attach(t *testing.T, slugs ...string) {
	t.Helper()
	if err := r.e.Attach(context.Background(), pane, slugs); err != nil {
		t.Fatal(err)
	}
	if !r.srv.WaitSubscribed(pane, time.Second) {
		t.Fatal("engine did not subscribe to the pane")
	}
}

func (r *rig) setReader(session string, rd *fakeReader) {
	r.mu.Lock()
	r.readers[session] = rd
	r.mu.Unlock()
}

func (r *rig) turnEnds(t *testing.T, tr transcript.Turn) {
	r.readers["s1"].add(tr)
	r.srv.Push(pane, herdr.StatusWorking)
	r.srv.Push(pane, herdr.StatusIdle)
}

func lastSeen(r *rig, slug string) int {
	a, _ := r.atts.Get(pane)
	if da := a.Find(slug); da != nil {
		return da.LastSeenTurn
	}
	return -1
}

var critical = backend.Verdict{Relevant: true, Severity: "critical", Message: "JWT in localStorage"}

func TestCatchUpCreatesQueueCardAndReportsPending(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "a pending card", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if c.Body != "JWT in localStorage" || c.Mode != "queue" || c.Status != queue.StatusPending || c.Glyph != "●" {
		t.Fatalf("card %+v", c)
	}
	eventually(t, "pending reported to herdr", func() bool {
		calls := r.srv.Calls("pane.report_metadata")
		return len(calls) > 0 && calls[len(calls)-1].Params["display_agent"] == "claude ◆1"
	})
	if req := r.be.last(); req.Cwd != "/repo" || req.Turns[0].User != "add login" {
		t.Fatalf("request %+v", req)
	}
}

func TestTurnEndPassMakesNoCard(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "rename a var"})
	eventually(t, "dot ran", func() bool { return r.be.n() == 1 })
	eventually(t, "status passed", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Dots) == 1 && p.Dots[0].Status == view.StatusPassed
	})
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("a pass must not create a card")
	}
}

func TestObserveDotNeverRunsOnTurns(t *testing.T) {
	r := newRig(t, dot("Copy", dots.ModeObserve))
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "copy")
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "copy") == 2 })
	if r.be.n() != 0 {
		t.Fatalf("observe dot ran %d times", r.be.n())
	}
}

func TestLoopGuardSkipsTurnStartedByThisDot(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "[● Security dot] use cookies", FromDots: []string{"Security"}})
	eventually(t, "turn consumed", func() bool { return lastSeen(r, "security") == 1 })
	time.Sleep(50 * time.Millisecond)
	if r.be.n() != 0 {
		t.Fatal("dot must not review a turn it started")
	}
}

func TestBudgetStopsDotAndWarnsOnce(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Budget.MaxCallsPerSession = 1
	r := newRig(t, d)
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "security")
	eventually(t, "first run", func() bool { return r.be.n() == 1 })
	eventually(t, "first run finished", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Dots[0].Status == view.StatusPassed
	})
	r.turnEnds(t, transcript.Turn{User: "two"})
	r.turnEnds(t, transcript.Turn{User: "three"})
	eventually(t, "budget status", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Dots[0].Status == view.StatusBudget
	})
	eventually(t, "one budget toast", func() bool { return len(r.srv.Calls("notification.show")) == 1 })
	time.Sleep(50 * time.Millisecond)
	if r.be.n() != 1 || len(r.srv.Calls("notification.show")) != 1 {
		t.Fatalf("calls=%d toasts=%d", r.be.n(), len(r.srv.Calls("notification.show")))
	}
}

func TestInjectDeliversWhenIdleAndUnfocused(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeInject))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "prompt sent", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
	text := r.srv.Calls("agent.prompt")[0].Params["text"].(string)
	if !strings.HasPrefix(text, "[● Security dot] JWT in localStorage") {
		t.Fatalf("prompt %q", text)
	}
	eventually(t, "card sent", func() bool { return len(r.q.Pending(pane)) == 0 })
}

func TestInjectWaitsWhileFocusedThenFlushes(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeInject))
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.Focused = true
	r.srv.SetPane(p)
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "auto card", func() bool {
		cs := r.q.Pending(pane)
		return len(cs) == 1 && cs[0].Status == queue.StatusAuto
	})
	time.Sleep(60 * time.Millisecond)
	if len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatal("must not type into the focused pane")
	}
	p.Focused = false
	r.srv.SetPane(p)
	eventually(t, "flushed after focus left", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
}

func TestInjectForcedToQueueForDotStartedTurns(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeInject), dot("Frontend", dots.ModeQueue))
	r.attach(t, "security", "frontend")
	r.be.set(critical)
	r.turnEnds(t, transcript.Turn{User: "[● Frontend dot] add skeleton", FromDots: []string{"Frontend"}})
	eventually(t, "security card", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if c.Mode != "queue" || c.Status != queue.StatusPending {
		t.Fatalf("inject from a dot-started turn must be queued, got %+v", c)
	}
	if len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatal("no auto inject on dot-started turns")
	}
}

func TestSendWhileBusyWaitsForIdle(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	id := r.q.Pending(pane)[0].ID
	r.srv.Push(pane, herdr.StatusWorking)
	if err := r.e.SendCards([]string{id}); err != nil {
		t.Fatal(err)
	}
	c, _ := r.q.Get(id)
	if c.Status != queue.StatusAuto || !c.Approved || len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatalf("busy send should wait: %+v", c)
	}
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "delivered on idle", func() bool { c, _ := r.q.Get(id); return c.Status == queue.StatusSent })
}

func TestSendRejectsSuggestCards(t *testing.T) {
	r := newRig(t, dot("Frontend", dots.ModeSuggest))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(backend.Verdict{Relevant: true, Severity: "warn", Message: "skeleton"})
	r.attach(t, "frontend")
	eventually(t, "suggest card", func() bool { return len(r.q.Pending(pane)) == 1 })
	eventually(t, "warn toast", func() bool { return len(r.srv.Calls("notification.show")) == 1 })
	if err := r.e.SendCards([]string{r.q.Pending(pane)[0].ID}); err == nil {
		t.Fatal("suggest cards cannot be sent")
	}
}

func TestDeliveryFailureReturnsCardToPending(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.srv.SetPromptError("internal")
	id := r.q.Pending(pane)[0].ID
	_ = r.e.SendCards([]string{id})
	c, _ := r.q.Get(id)
	if c.Status != queue.StatusPending || c.Error == "" {
		t.Fatalf("failed delivery: %+v", c)
	}
}

func TestClosedPaneOrphansCards(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.srv.RemovePane(pane)
	eventually(t, "attachment dropped", func() bool { _, ok := r.atts.Get(pane); return !ok })
	vis := r.q.Visible(pane)
	if len(vis) != 1 || vis[0].Status != queue.StatusOrphaned {
		t.Fatalf("visible %+v", vis)
	}
	if p, ok := r.e.Snapshot().Pane(pane); !ok || len(p.Cards) != 1 {
		t.Fatal("orphaned cards stay in the snapshot")
	}
}

// Review Focus 5: /clear gives the pane a new session id; follow it.
func TestNewAgentSessionIsFollowed(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "old conversation"})
	r.attach(t, "security")
	eventually(t, "catch-up run", func() bool { return r.be.n() == 1 })
	s2 := &fakeReader{}
	s2.add(transcript.Turn{User: "fresh conversation"})
	r.setReader("s2", s2)
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.AgentSession = &herdr.AgentSession{Kind: "id", Value: "s2"}
	r.srv.SetPane(p)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "dot reviews the new session", func() bool {
		return r.be.n() >= 2 && r.be.last().Turns[len(r.be.last().Turns)-1].User == "fresh conversation"
	})
	a, _ := r.atts.Get(pane)
	if a.SessionValue != "s2" {
		t.Fatalf("attachment still on %q", a.SessionValue)
	}
}

func TestAttachUnknownDotFails(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	if err := r.e.Attach(context.Background(), pane, []string{"nope"}); err == nil {
		t.Fatal("unknown dot must fail")
	}
	if err := r.e.Attach(context.Background(), "w9:p9", []string{"security"}); err == nil {
		t.Fatal("unknown pane must fail")
	}
}

// Attaching another dot must not overwrite updates made to the attachment
// while Attach was reading the transcript (pause, failures, cooldown, detach).
func TestAttachDoesNotLoseConcurrentAttachmentUpdates(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue))
	r.attach(t, "security")
	rd := r.readers["s1"]
	rd.arm()
	done := make(chan error, 1)
	go func() { done <- r.e.Attach(context.Background(), pane, []string{"frontend"}) }()
	select {
	case <-rd.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Attach never read the transcript")
	}
	if err := r.e.SetPaused(pane, "security", true); err != nil { // e.g. runFailed stopping the dot
		t.Fatal(err)
	}
	close(rd.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	a, _ := r.atts.Get(pane)
	if da := a.Find("security"); da == nil || !da.Paused {
		t.Fatalf("pause made during Attach was lost: %+v", a.Dots)
	}
	if a.Find("frontend") == nil {
		t.Fatalf("frontend not attached: %+v", a.Dots)
	}
}

// Dots attach only to agent panes (not a shell, not the Dots pane itself).
func TestAttachToPaneWithoutAgentFails(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.srv.SetPane(herdr.PaneInfo{PaneID: "w1:p7", AgentStatus: herdr.StatusUnknown, Cwd: "/repo"})
	err := r.e.Attach(context.Background(), "w1:p7", []string{"security"})
	if err == nil || !strings.Contains(err.Error(), "agent") {
		t.Fatalf("attach to a pane with no agent: %v", err)
	}
	if _, ok := r.atts.Get("w1:p7"); ok {
		t.Fatal("no attachment may be stored")
	}
}

// A session change racing a processPane that already read the old session must
// not make the engine skip the new session's turns or re-review the old ones.
func TestSessionChangeDuringInFlightProcessPane(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	old := r.readers["s1"]
	old.arm()
	old.add(transcript.Turn{User: "old"})
	r.srv.Push(pane, herdr.StatusWorking)
	r.srv.Push(pane, herdr.StatusIdle)
	select {
	case <-old.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("processPane never read the old session")
	}
	fresh := &fakeReader{}
	fresh.add(transcript.Turn{User: "fresh"})
	r.setReader("s2", fresh)
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.AgentSession = &herdr.AgentSession{Kind: "id", Value: "s2"}
	r.srv.SetPane(p)
	r.srv.Push(pane, herdr.StatusIdle)
	time.Sleep(30 * time.Millisecond) // let the second pass queue behind the pane lock
	close(old.release)
	eventually(t, "dot reviews the new session", func() bool {
		return r.be.n() >= 2 && r.be.last().Turns[len(r.be.last().Turns)-1].User == "fresh"
	})
	time.Sleep(60 * time.Millisecond)
	r.be.mu.Lock()
	var users []string
	for _, c := range r.be.calls {
		users = append(users, c.Turns[len(c.Turns)-1].User)
	}
	r.be.mu.Unlock()
	if strings.Join(users, ",") != "old,fresh" {
		t.Fatalf("reviews = %v, want [old fresh] (no re-review, no skipped turn)", users)
	}
}

// failingGet makes PaneGet fail with a transient (non not_found) error.
type failingGet struct{ *herdr.SocketClient }

func (failingGet) PaneGet(context.Context, string) (herdr.PaneInfo, error) {
	return herdr.PaneInfo{}, errors.New("socket timeout")
}

func TestStartKeepsAttachmentOnTransientPaneGetError(t *testing.T) {
	srv := herdrtest.New(t)
	dir := t.TempDir()
	q, _ := queue.Open(filepath.Join(dir, "queue.json"))
	atts, _ := state.OpenAttachments(filepath.Join(dir, "attachments.json"))
	usage, _ := state.OpenUsage(filepath.Join(dir, "usage.json"))
	feed, _ := state.OpenFeed(filepath.Join(dir, "feed.jsonl"))
	if err := atts.Put(state.Attachment{PaneID: pane, Agent: "claude", SessionValue: "s1",
		Dots: []state.DotAttachment{{Slug: "security"}}}); err != nil {
		t.Fatal(err)
	}
	e := New(Deps{
		Herdr:   failingGet{&herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"}},
		Backend: func(string) (backend.Backend, error) { return &fakeBackend{}, nil },
		Reader:  func(state.Attachment) transcript.Reader { return &fakeReader{} },
		LoadDots: func() ([]dots.Dot, []dots.LoadError) {
			return []dots.Dot{dot("Security", dots.ModeQueue)}, nil
		},
		SaveDot: func(dots.Dot) error { return nil },
		Config:  config.Default(), Queue: q, Attachments: atts, Usage: usage, Feed: feed,
		PollInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(e.Wait)
	t.Cleanup(cancel)
	e.Start(ctx)
	if _, ok := atts.Get(pane); !ok {
		t.Fatal("a transient PaneGet error at Start must not drop the attachment")
	}
}

// When the reader is replaced by one with fewer turns (scrollback fallback,
// restart, path no longer resolving), the counter follows it down so new turns
// are reviewed instead of skipped.
func TestReaderReplacedWithFewerTurnsStillReviewsNewTurns(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	for i := 0; i < 5; i++ {
		r.readers["s1"].add(transcript.Turn{User: "old"})
	}
	r.attach(t, "security")
	eventually(t, "five turns consumed", func() bool { return lastSeen(r, "security") == 5 })
	eventually(t, "catch-up run finished", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Dots) == 1 && p.Dots[0].Status == view.StatusPassed
	})
	before := r.be.n()

	small := &fakeReader{}
	small.add(transcript.Turn{User: "after swap"})
	r.setReader("s1", small)
	r.e.dropReader(pane)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "counter follows the smaller reader", func() bool { return lastSeen(r, "security") == 1 })

	small.add(transcript.Turn{User: "brand new"})
	r.srv.Push(pane, herdr.StatusWorking)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "new turn reviewed", func() bool {
		return r.be.n() > before && r.be.last().Turns[len(r.be.last().Turns)-1].User == "brand new"
	})
}

// Dismiss and Edit serialize with deliveries (e.dmu), so a card can't be
// dismissed or rewritten while a flush is sending it.
func TestDismissAndEditWaitForInFlightDelivery(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	id := r.q.Pending(pane)[0].ID
	for _, step := range []struct {
		name string
		op   func() error
	}{
		{"edit", func() error { return r.e.Edit(id, "rewritten") }},
		{"dismiss", func() error { return r.e.Dismiss(id) }}, // last: a dismissed card can't be edited
	} {
		name, op := step.name, step.op
		r.e.dmu.Lock() // a delivery is in flight
		done := make(chan error, 1)
		go func() { done <- op() }()
		select {
		case err := <-done:
			r.e.dmu.Unlock()
			t.Fatalf("%s ran during an in-flight delivery (err %v)", name, err)
		case <-time.After(50 * time.Millisecond):
		}
		if c, _ := r.q.Get(id); c.Status != queue.StatusPending || (name == "edit" && c.Body != critical.Message) {
			t.Fatalf("%s changed the card mid-delivery: %+v", name, c)
		}
		r.e.dmu.Unlock()
		if err := <-done; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "edit" {
			if c, _ := r.q.Get(id); c.Body != "rewritten" {
				t.Fatalf("edit after delivery: %+v", c)
			}
		}
	}
}

// A run that ends while the agent is already working on the next prompt must
// not review the half-written turn; the next idle event picks it up whole.
func TestRerunWaitsForIdleInsteadOfReadingAHalfFinishedTurn(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "one"})
	gate := make(chan struct{})
	r.be.mu.Lock()
	r.be.gate = gate
	r.be.mu.Unlock()
	r.attach(t, "security")
	eventually(t, "catch-up run in flight", func() bool { return r.be.n() == 1 })

	// Turn two ends during the run: the dot is busy, so it is marked for a rerun.
	r.turnEnds(t, transcript.Turn{User: "two"})
	k := key{pane, "security"}
	eventually(t, "rerun requested", func() bool { r.e.mu.Lock(); defer r.e.mu.Unlock(); return r.e.rerun[k] })
	// The agent starts on the next prompt; its turn is half written.
	r.srv.Push(pane, herdr.StatusWorking)
	r.readers["s1"].add(transcript.Turn{User: "three", Assistant: "partial…"})
	close(gate)
	eventually(t, "run finished", func() bool { r.e.mu.Lock(); defer r.e.mu.Unlock(); return !r.e.running[k] })
	time.Sleep(100 * time.Millisecond)
	if n := r.be.n(); n != 1 {
		t.Fatalf("a rerun while the agent works read a half-finished turn (%d calls)", n)
	}
	if ls := lastSeen(r, "security"); ls != 1 {
		t.Fatalf("LastSeenTurn moved to %d while the agent worked", ls)
	}
	r.srv.Push(pane, herdr.StatusDone)
	eventually(t, "review after the turn finished", func() bool {
		return r.be.n() == 2 && r.be.last().Turns[len(r.be.last().Turns)-1].User == "three"
	})
}

// Attaching while the agent works defers the catch-up to the next idle event.
func TestAttachCatchUpWaitsForIdle(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.srv.Push(pane, herdr.StatusWorking)
	r.attach(t, "security")
	time.Sleep(100 * time.Millisecond)
	if r.be.n() != 0 {
		t.Fatal("catch-up must not run while the agent is working")
	}
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "catch-up on idle", func() bool { return r.be.n() == 1 })
}

// configRig watches a config.toml in a temp dir; check sets ConfigCheck.
func configRig(t *testing.T, check time.Duration, ds ...dots.Dot) (*rig, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	r := newRigWith(t, func(d *Deps) { d.ConfigPath, d.ConfigCheck = path, check }, ds...)
	return r, path
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("turn_debounce_ms = 0\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The inject kill switch applies at runtime, within the config check interval.
func TestInjectKillSwitchAppliesAtRuntime(t *testing.T) {
	r, path := configRig(t, 10*time.Millisecond, dot("Security", dots.ModeInject))
	r.attach(t, "security")
	if !r.e.Snapshot().InjectEnabled {
		t.Fatal("inject starts enabled")
	}
	writeConfig(t, path, "inject_enabled = false\n")
	eventually(t, "config reloaded", func() bool { return !r.e.Snapshot().InjectEnabled })
	r.be.set(critical)
	r.turnEnds(t, transcript.Turn{User: "add login"})
	eventually(t, "a card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if c := r.q.Pending(pane)[0]; c.Mode != "queue" || c.Status != queue.StatusPending {
		t.Fatalf("with inject disabled the card must be queued, got %+v", c)
	}
	time.Sleep(50 * time.Millisecond)
	if len(r.srv.Calls("agent.prompt")) != 0 {
		t.Fatal("nothing may be typed into the agent")
	}
}

func TestReloadDotsAlsoReloadsConfig(t *testing.T) {
	r, path := configRig(t, time.Hour, dot("Security", dots.ModeInject)) // no mtime checks
	writeConfig(t, path, "inject_enabled = false\n")
	if err := r.e.Handle(context.Background(), ipc.CmdReloadDots, nil); err != nil {
		t.Fatal(err)
	}
	if r.e.Snapshot().InjectEnabled {
		t.Fatal("reload_dots must re-read config.toml")
	}
}

// A malformed config keeps the last good settings but forces inject off.
func TestMalformedConfigKeepsLastGoodButDisablesInject(t *testing.T) {
	r, path := configRig(t, 10*time.Millisecond, dot("Security", dots.ModeInject))
	r.attach(t, "security")
	writeConfig(t, path, "session_budget_calls = 7\n")
	eventually(t, "good config applied", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Budget == 7 })
	writeConfig(t, path, "session_budget_calls = 9\ninject_enabled = = true\n")
	eventually(t, "inject forced off", func() bool { return !r.e.Snapshot().InjectEnabled })
	if p, _ := r.e.Snapshot().Pane(pane); p.Budget != 7 {
		t.Fatalf("last good settings must stay, budget %d", p.Budget)
	}
}

// budgetRig runs one dot with a 1-call budget until it is out of budget.
func budgetRig(t *testing.T) *rig {
	t.Helper()
	d := dot("Security", dots.ModeQueue)
	d.Budget.MaxCallsPerSession = 1
	r := newRig(t, d)
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "security")
	eventually(t, "first run finished", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return r.be.n() == 1 && p.Dots[0].Status == view.StatusPassed
	})
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "budget status", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Dots[0].Status == view.StatusBudget
	})
	return r
}

func TestBudgetToastSaysHowItResets(t *testing.T) {
	r := budgetRig(t)
	eventually(t, "budget toast", func() bool { return len(r.srv.Calls("notification.show")) == 1 })
	body, _ := r.srv.Calls("notification.show")[0].Params["body"].(string)
	if body != "budget reached; resets on a new agent session or press r" {
		t.Fatalf("toast %q", body)
	}
}

// Budgets are per agent session: /clear (a new session) starts from zero.
func TestBudgetResetsOnNewAgentSession(t *testing.T) {
	r := budgetRig(t)
	s2 := &fakeReader{}
	s2.add(transcript.Turn{User: "fresh"})
	r.setReader("s2", s2)
	p, _ := (&herdr.SocketClient{Path: r.srv.Path}).PaneGet(context.Background(), pane)
	p.AgentSession = &herdr.AgentSession{Kind: "id", Value: "s2"}
	r.srv.SetPane(p)
	r.srv.Push(pane, herdr.StatusIdle)
	eventually(t, "new session reviewed despite the old budget", func() bool {
		return r.be.n() == 2 && r.be.last().Turns[len(r.be.last().Turns)-1].User == "fresh"
	})
	if c := r.usage.Calls(pane, "security"); c != 1 {
		t.Fatalf("usage after reset + one run = %d, want 1", c)
	}
}

// r (Reset) clears the dot's usage counter, so it runs again.
func TestResetClearsDotUsage(t *testing.T) {
	r := budgetRig(t)
	if err := r.e.Reset(pane, "security"); err != nil {
		t.Fatal(err)
	}
	if c := r.usage.Calls(pane, "security"); c != 0 {
		t.Fatalf("usage after reset = %d", c)
	}
	r.turnEnds(t, transcript.Turn{User: "three"})
	eventually(t, "dot runs again after reset", func() bool {
		return r.be.n() == 2 && r.be.last().Turns[len(r.be.last().Turns)-1].User == "three"
	})
}

package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

func TestDecideBackground(t *testing.T) {
	base := BackgroundInput{Proactive: true, DailyCount: 2, MaxPerDay: 6, CostToday: 0.40, DailyBudget: 1.00}
	cases := []struct {
		name string
		mod  func(*BackgroundInput)
		want Reason
	}{
		{"runs", func(*BackgroundInput) {}, ReasonRun},
		{"kill switch", func(in *BackgroundInput) { in.Proactive = false }, ReasonProactiveOff},
		{"pause all", func(in *BackgroundInput) { in.PausedAll = true }, ReasonPausedAll},
		{"paused dot", func(in *BackgroundInput) { in.Paused = true }, ReasonPaused},
		{"daily runs", func(in *BackgroundInput) { in.DailyCount = 6 }, ReasonDailyRuns},
		{"no per-dot cap", func(in *BackgroundInput) { in.MaxPerDay, in.DailyCount = 0, 99 }, ReasonRun},
		{"daily budget", func(in *BackgroundInput) { in.CostToday = 1.00 }, ReasonDailyBudget},
	}
	for _, c := range cases {
		in := base
		c.mod(&in)
		if got := DecideBackground(in); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func scheduledJob(r *proRig, d dots.Dot) bgJob {
	return bgJob{kind: state.RunScheduled, project: testProject, projectName: "repo", cwd: r.dir, dot: d,
		question: "Review commits since the last run.", label: "scheduled Wed 09:00", deliver: r.e.deliverScheduled}
}

// A scheduled result for a project with no attached pane waits in the shared
// inbox (with a toast) and becomes a pending card when a pane attaches.
func TestScheduledResultWaitsInInboxUntilAttach(t *testing.T) {
	d := dot("Security", dots.ModeInject)
	r := newProRig(t, proOpts{}, d)
	r.be.set(backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: "Commit 3f2a skips the CSRF check."}, CostUSD: 0.12, CostKnown: true})
	r.e.runBackground(scheduledJob(r, d))
	cs, _ := r.inbox.List(testProject)
	if len(cs) != 1 || cs[0].Origin != state.RunScheduled || cs[0].Label != "scheduled Wed 09:00" || cs[0].Mode != "queue" {
		t.Fatalf("inbox %+v", cs)
	}
	if toasts := r.toasts(); len(toasts) != 1 || !strings.Contains(toasts[0], "Security (scheduled) has a finding for repo") {
		t.Fatalf("toasts %v", toasts)
	}
	if day := r.today(t); day.Count("security", state.RunScheduled) != 1 || day.CostUSD != 0.12 {
		t.Fatalf("daily %+v", day)
	}
	req := r.be.last()
	if req.Trigger != backend.TriggerScheduled || req.Question != "Review commits since the last run." || !req.RepoRead || req.Cwd != r.dir || req.Web || req.Pass != backend.PassReview {
		t.Fatalf("request %+v", req)
	}
	if req.Write || backend.WriteMode(req) {
		t.Fatal("a background run never asks for write tools")
	}
	r.attach(t, "security")
	eventually(t, "the inbox card is claimed", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if c.Status != queue.StatusPending || c.Approved || c.Origin != state.RunScheduled {
		t.Fatalf("an inbox card is never auto-delivered, even for an inject dot: %+v", c)
	}
	if left, _ := r.inbox.List(testProject); len(left) != 0 {
		t.Fatalf("inbox not emptied: %+v", left)
	}
}

// The daily budget stops every background run, toasts once a day, shows in
// the roster, and resets after local midnight.
func TestDailyBudgetStopsBackgroundRunsUntilMidnight(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Trigger.OnCommit = true
	r := newProRig(t, proOpts{}, d)
	r.attach(t, "security")
	_ = r.daily.Add("frontend", state.RunResearch, 1.00, r.clock.Now()) // another session spent it
	r.e.runBackground(scheduledJob(r, d))
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 0 {
		t.Fatalf("%d backend calls over the daily budget", r.be.n())
	}
	if toasts := r.toasts(); len(toasts) != 1 || !strings.Contains(toasts[0], "daily budget $1.00 reached") {
		t.Fatalf("one toast per day: %v", toasts)
	}
	if s := r.dotState("security"); s.Next != "paused · daily budget" {
		t.Fatalf("roster hint %q", s.Next)
	}
	if snap := r.e.Snapshot(); snap.TodayUSD != 1.00 || snap.TodayRuns != 1 || snap.DailyBudgetUSD != 1.00 {
		t.Fatalf("snapshot today %v/%v runs %d", snap.TodayUSD, snap.DailyBudgetUSD, snap.TodayRuns)
	}
	r.clock.Add(14 * time.Hour) // past local midnight
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 1 {
		t.Fatal("background work resumes on a new day")
	}
}

func TestProactiveKillSwitchStopsBackgroundRuns(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{cfg: func(c *config.Config) { c.ProactiveEnabled = false }}, d)
	r.e.runBackground(scheduledJob(r, d))
	r2 := newProRig(t, proOpts{noDaily: true}, d)
	r2.e.runBackground(scheduledJob(r2, d))
	if r.be.n() != 0 || r2.be.n() != 0 {
		t.Fatal("proactive_enabled = false (or no daily store) stops every background run")
	}
}

// proactive_enabled is a safety switch: a malformed config turns it off.
func TestMalformedConfigTurnsProactiveOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	r := newProRig(t, proOpts{configPath: path}, dot("Security", dots.ModeQueue))
	if !r.e.config().ProactiveEnabled {
		t.Fatal("on by default")
	}
	os.WriteFile(path, []byte("proactive_enabled = = true\n"), 0o600)
	eventually(t, "proactive forced off", func() bool { return !r.e.config().ProactiveEnabled })
}

// A run on an attached pane counts toward the pane's session usage, the
// daily store and the feed, and a failure is fed (redacted) without a card.
func TestBackgroundRunAccountsUsageAndFeed(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{}, d)
	r.attach(t, "security")
	var got atomic.Int32
	j := scheduledJob(r, d)
	j.paneID, j.session = pane, "s1"
	j.deliver = func(bgJob, backend.Verdict) { got.Add(1) }
	r.be.set(backend.Result{Verdict: backend.Verdict{Severity: "info"}, CostUSD: 0.02, CostKnown: true})
	r.e.runBackground(j)
	if r.usage.Calls(pane, "security") != 1 || got.Load() != 0 {
		t.Fatalf("calls %d delivered %d", r.usage.Calls(pane, "security"), got.Load())
	}
	if !r.hasFeed(pane, state.OutcomeScheduled, "scheduled Wed 09:00: nothing new") {
		t.Fatalf("feed %+v", r.feed.Recent(pane, 10))
	}
	r.be.set(proResult("warn", "The new endpoint has no rate limit."))
	r.e.runBackground(j)
	if got.Load() != 1 || !r.hasFeed(pane, state.OutcomeScheduled, "The new endpoint has no rate limit.") {
		t.Fatal("a relevant verdict is delivered and fed")
	}
	r.be.failNext(1)
	r.e.runBackground(j)
	if got.Load() != 1 || !r.hasFeed(pane, state.OutcomeError, "scheduled Wed 09:00: backend unavailable") {
		t.Fatal("a failed run is fed as an error and delivers nothing")
	}
	if day := r.today(t); day.Count("security", state.RunScheduled) != 3 {
		t.Fatalf("every run counts toward the day, failed or not: %+v", day.Runs)
	}
}

// A pane that closes while a background run is out (its id then reused by a
// new attachment, Phase 7's generations): a commit review charges and cards
// nothing on the new attachment; a scheduled result goes to the inbox.
func TestBackgroundRunOutlivingItsPane(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{}, d)
	r.attach(t, "security")
	r.be.set(proResult("warn", "The new endpoint has no rate limit."))
	gate := r.be.hold("", true)
	done := make(chan struct{})
	j := scheduledJob(r, d)
	j.kind, j.label, j.paneID, j.session = state.RunCommit, "commit a1b2c3d", pane, "s1"
	j.deliver = func(bgJob, backend.Verdict) { t.Error("a review for a closed pane was delivered") }
	j.done = func() { close(done) }
	r.e.spawn(func() { r.e.runBackground(j) })
	eventually(t, "the commit review is out", func() bool { return r.be.n() == 1 })
	r.e.dropPane(pane)
	r.attach(t, "security") // the same pane id: a new attachment
	close(gate)
	<-done
	if n := r.usage.Calls(pane, "security"); n != 0 {
		t.Fatalf("the new attachment was charged %d calls", n)
	}
	if day := r.today(t); day.Count("security", state.RunCommit) != 1 {
		t.Fatalf("the daily store still counts the run: %+v", day.Runs)
	}

	gate = r.be.hold("", true)
	done = make(chan struct{})
	s := scheduledJob(r, d)
	r.e.mu.Lock()
	s.paneID, s.session, s.gen = pane, "s1", r.e.gens[pane]
	r.e.mu.Unlock()
	s.done = func() { close(done) }
	r.e.spawn(func() { r.e.runBackground(s) })
	eventually(t, "the scheduled run is out", func() bool { return r.be.n() == 2 })
	r.e.dropPane(pane)
	close(gate)
	<-done
	if cs, _ := r.inbox.List(testProject); len(cs) != 1 || cs[0].PaneID != "" {
		t.Fatalf("a scheduled result whose pane closed waits in the inbox: %+v", cs)
	}
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("no card on the closed pane")
	}
}

// One background run at a time per engine: research, schedules and commit
// reviews never crowd out turn reviews.
func TestOneBackgroundRunAtATime(t *testing.T) {
	d1, d2 := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
	r := newProRig(t, proOpts{}, d1, d2)
	gate := r.be.hold("", true)
	r.e.spawn(func() { r.e.runBackground(scheduledJob(r, d1)) }) // tracked: Wait drains it
	r.e.spawn(func() { r.e.runBackground(scheduledJob(r, d2)) })
	eventually(t, "one run started", func() bool { return r.be.n() == 1 })
	time.Sleep(50 * time.Millisecond)
	if r.be.n() != 1 {
		t.Fatal("a second background run started while one was in flight")
	}
	close(gate)
	eventually(t, "the second run", func() bool { return r.be.n() == 2 })
}

// The card re-checks the pane under e.mu: a pane that closes after the run
// ended but before its card lands gets no card; the result waits in the inbox.
func TestScheduledCardRechecksThePane(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{}, d)
	r.attach(t, "security")
	r.be.set(proResult("warn", "Commit 3f2a skips the CSRF check."))
	j := paneJob(r, d)
	j.deliver = func(j bgJob, v backend.Verdict) {
		r.e.dropPane(pane)
		r.e.deliverScheduled(j, v)
	}
	r.e.runBackground(j)
	if cs, _ := r.inbox.List(testProject); len(cs) != 1 {
		t.Fatalf("inbox %+v", cs)
	}
	for _, c := range r.q.All() {
		if c.PaneID == pane && c.Status == queue.StatusPending {
			t.Fatalf("a card landed on the closed pane: %+v", c)
		}
	}
}

// paneJob is a scheduled job built for the rig's pane, with its generation.
func paneJob(r *proRig, d dots.Dot) bgJob {
	j := scheduledJob(r, d)
	r.e.mu.Lock()
	j.paneID, j.session, j.gen = pane, "s1", r.e.gens[pane]
	r.e.mu.Unlock()
	return j
}

func proAsk(r *proRig, slug string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.e.Ask(ctx, pane, slug, "q", false)
	return err
}

// Ruling P8-R2: a background run in flight on a pane counts as a call already
// made when an MCP ask or a draft checks the pane's call budget.
func TestAsksAndDraftsCountABackgroundRunInFlight(t *testing.T) {
	sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
	fe.Capabilities.Write = true
	r := newProRig(t, proOpts{
		cfg: func(c *config.Config) { c.SessionBudgetCalls, c.WriteEnabled = 1, true },
		deps: func(d *Deps) {
			d.Patches, d.WorktreesDir = patchstore.Open(filepath.Join(t.TempDir(), "patches")), t.TempDir()
		},
	}, sec, fe)
	r.attach(t, "security", "frontend")
	gate := r.be.hold("", true)
	done := make(chan struct{})
	j := paneJob(r, sec)
	j.done = func() { close(done) }
	r.e.spawn(func() { r.e.runBackground(j) })
	eventually(t, "the background run is out", func() bool { return r.be.n() == 1 })
	// The draft first: an ask that got through would itself be in flight.
	if _, err := r.e.Draft(context.Background(), pane, "frontend", "fix it", ""); !errors.Is(err, ErrBudget) {
		t.Errorf("draft while a background run is in flight: %v, want ErrBudget", err)
	}
	if err := proAsk(r, "frontend"); !errors.Is(err, ErrBudget) {
		t.Errorf("ask while a background run is in flight: %v, want ErrBudget", err)
	}
	close(gate)
	<-done
	if n := r.be.n(); n != 1 {
		t.Fatalf("%d backend calls, want only the background run", n)
	}
}

// Ruling P8-R2, the other way: asks and live drafts in flight on the pane
// count as calls already made when a background run checks the budget.
func TestBackgroundRunsCountAsksAndDraftsInFlight(t *testing.T) {
	sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
	budget1 := proOpts{cfg: func(c *config.Config) { c.SessionBudgetCalls = 1 }}
	r := newProRig(t, budget1, sec, fe)
	r.attach(t, "security", "frontend")
	gate := r.be.hold("", true)
	asked := make(chan error, 1)
	go func() { asked <- proAsk(r, "frontend") }()
	eventually(t, "the ask is out", func() bool { return r.be.n() == 1 })
	done := make(chan struct{})
	j := paneJob(r, sec)
	j.done = func() { close(done) }
	r.e.spawn(func() { r.e.runBackground(j) })
	select {
	case <-done: // skipped without calling the backend
	case <-time.After(3 * time.Second): // it is held at the backend's gate
		close(gate)
		t.Fatal("a background run started next to an ask in flight over a 1-call budget")
	}
	if n := r.be.n(); n != 1 {
		t.Errorf("%d backend calls, want only the ask", n)
	}
	close(gate)
	if err := <-asked; err != nil {
		t.Fatal(err)
	}

	r2 := newProRig(t, budget1, sec, fe)
	r2.attach(t, "security", "frontend")
	r2.e.mu.Lock()
	r2.e.drafts["d1"] = &draftRun{pane: pane, slug: "frontend", cancel: func() {}}
	r2.e.mu.Unlock()
	r2.e.runBackground(paneJob(r2, sec))
	if n := r2.be.n(); n != 0 {
		t.Fatalf("a background run started next to a live draft over a 1-call budget (%d calls)", n)
	}
	r2.e.mu.Lock()
	r2.e.drafts["d1"].abandoned = true // winding down: it charges nothing more
	r2.e.mu.Unlock()
	r2.e.runBackground(paneJob(r2, sec))
	if n := r2.be.n(); n != 1 {
		t.Fatalf("an abandoned draft still blocked the background run (%d calls)", n)
	}
}

// errBackend fails every run with err.
type errBackend struct{ err error }

func (b errBackend) Kind() string                { return "claude-cli" }
func (b errBackend) Check(context.Context) error { return nil }
func (b errBackend) Run(context.Context, backend.Request) (backend.Result, error) {
	return backend.Result{}, b.err
}

// A failed background run's error is redacted and sanitized before it
// reaches the log or the feed.
func TestBackgroundFailureIsRedactedAndSanitized(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	var mu sync.Mutex
	var logs []string
	secret := "sk-ant-api03-SECRETSECRETSECRET"
	r := newProRig(t, proOpts{deps: func(dp *Deps) {
		dp.Backend = func(string) (backend.Backend, error) {
			return errBackend{errors.New("401 for key " + secret + "\x1b[2J\x07 rejected")}, nil
		}
		dp.Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	}}, d)
	r.attach(t, "security")
	r.e.runBackground(paneJob(r, d))
	var detail string
	for _, e := range r.feed.Recent(pane, 10) {
		if e.Outcome == state.OutcomeError {
			detail = e.Detail
		}
	}
	if !strings.Contains(detail, "scheduled Wed 09:00: 401 for key sk-ant-[redacted]") {
		t.Fatalf("feed detail %q", detail)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, s := range append(logs, detail) {
		if strings.Contains(s, "SECRET") || strings.ContainsAny(s, "\x1b\x07") {
			t.Fatalf("unredacted or unsanitized: %q", s)
		}
	}
}

// Task 3's contract: a Blocked day (daily.json was unreadable and moved
// aside) is "budget reached" for the rest of the local day.
func TestCorruptDailyFileStopsBackgroundRuns(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Trigger.OnCommit = true
	r := newProRig(t, proOpts{}, d)
	r.attach(t, "security")
	if err := os.WriteFile(filepath.Join(r.dir, "daily.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 0 {
		t.Fatal("a background run started on a blocked day")
	}
	if s := r.dotState("security"); s.Next != "paused · daily budget" {
		t.Fatalf("roster hint %q", s.Next)
	}
	r.clock.Add(14 * time.Hour) // past local midnight
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 1 {
		t.Fatal("background work resumes on a new day")
	}
}

// Any error from Daily.Today or Daily.Add counts as "budget reached": a
// locked daily.json stops the run, and a run whose cost could not be recorded
// stops background work in this engine for the rest of the local day.
func TestDailyStoreErrorsFailClosed(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	r := newProRig(t, proOpts{}, d)
	r.daily.Timeout = 50 * time.Millisecond
	lockPath := filepath.Join(r.dir, "daily.json.lock")
	l, err := lockfile.Acquire(lockPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r.e.runBackground(scheduledJob(r, d))
	l.Release()
	if r.be.n() != 0 {
		t.Fatal("a background run started while daily.json was unreadable")
	}

	gate := r.be.hold("", true)
	done := make(chan struct{})
	j := scheduledJob(r, d)
	j.done = func() { close(done) }
	r.e.spawn(func() { r.e.runBackground(j) })
	eventually(t, "the run is out", func() bool { return r.be.n() == 1 })
	if l, err = lockfile.Acquire(lockPath, time.Second); err != nil {
		t.Fatal(err)
	}
	close(gate) // its Daily.Add times out on the lock
	<-done
	l.Release()
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 1 {
		t.Fatal("a run whose cost was never recorded let more background work start today")
	}
	r.clock.Add(14 * time.Hour) // past local midnight
	r.e.runBackground(scheduledJob(r, d))
	if r.be.n() != 2 {
		t.Fatal("background work resumes on a new day")
	}
}

// Research is a read-only pass even for a write-capable dot: PassResearch,
// never Write; web tools only when the dot asks and the backend has them;
// and only a research pass may write research and question entries.
func TestResearchRunIsReadOnlyAndAloneWritesResearchMemory(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Capabilities.Write = true
	d.Research.Web = true
	r := newProRig(t, proOpts{cfg: func(c *config.Config) { c.WriteEnabled = true }}, d)
	r.attach(t, "security")
	ops := []backend.MemoryOp{{Op: "add", Scope: "dot", Kind: "research", Text: "OWASP ASVS 5.0 moved session rules to V7."}}
	r.be.set(backend.Result{Verdict: backend.Verdict{Severity: "info"}, Remember: ops})
	j := paneJob(r, d)
	j.kind, j.label, j.deliver = state.RunResearch, "research", nil
	r.e.runBackground(j)
	req := r.be.last()
	if req.Pass != backend.PassResearch || req.Write || backend.WriteMode(req) || !req.Web || req.Trigger != backend.TriggerTurn {
		t.Fatalf("research request %+v", req)
	}
	research := memory.Filter{Kinds: []memory.Kind{memory.KindResearch}}
	if es, _ := r.mem.List(testProject, research); len(es) != 1 {
		t.Fatalf("research entries after a research pass: %+v", es)
	}
	ops[0].Text = "A scheduled review must not write research."
	r.be.set(backend.Result{Verdict: backend.Verdict{Severity: "info"}, Remember: ops})
	r.e.runBackground(paneJob(r, d))
	if req := r.be.last(); req.Pass != backend.PassReview || req.Web || req.Write {
		t.Fatalf("scheduled request %+v", req)
	}
	if es, _ := r.mem.List(testProject, research); len(es) != 1 {
		t.Fatalf("a scheduled review wrote research memory: %+v", es)
	}
}

// The inbox's Origin whitelist and the engine's run kinds stay in step: a
// renamed state.Run* constant fails here instead of dropping paid results.
func TestInboxAcceptsEveryBackgroundRunKind(t *testing.T) {
	in := queue.OpenInbox(filepath.Join(t.TempDir(), "inbox.json"))
	for _, kind := range []string{state.RunResearch, state.RunScheduled, state.RunCommit} {
		if err := in.Put(queue.Card{Dot: "security", Project: testProject, Body: "x", Origin: kind}); err != nil {
			t.Errorf("origin %q: %v", kind, err)
		}
		if outcomeFor(kind) != kind {
			t.Errorf("feed outcome for %q is %q", kind, outcomeFor(kind))
		}
	}
}

// daily_budget_usd is never "unlimited": a zero, negative, NaN or infinite
// budget (only reachable without config.Load's checks) falls back to $1.00.
func TestDecideBackgroundNeverUnlimited(t *testing.T) {
	for _, b := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		in := BackgroundInput{Proactive: true, CostToday: config.DefaultDailyBudgetUSD, DailyBudget: b}
		if got := DecideBackground(in); got != ReasonDailyBudget {
			t.Errorf("budget %v: %s, want %s", b, got, ReasonDailyBudget)
		}
	}
}

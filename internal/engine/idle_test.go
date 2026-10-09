package engine

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

func TestDecideResearch(t *testing.T) {
	d := researchDot("Security")
	base := ResearchInput{Dot: d, AgentIdle: true, IdleFor: 11 * time.Minute, PaneBudget: 120,
		Background: BackgroundInput{Proactive: true, DailyBudget: 1.00}}
	cases := []struct {
		name string
		mod  func(*ResearchInput)
		want bool
	}{
		{"due", func(*ResearchInput) {}, true},
		{"research off", func(in *ResearchInput) { in.Dot.Research.Enabled = false }, false},
		{"agent working", func(in *ResearchInput) { in.AgentIdle = false }, false},
		{"not idle long enough", func(in *ResearchInput) { in.IdleFor = 9 * time.Minute }, false},
		{"already ran this period", func(in *ResearchInput) { in.RanThisPeriod = true }, false},
		{"in flight", func(in *ResearchInput) { in.Busy = true }, false},
		{"paused dot", func(in *ResearchInput) { in.Att.Paused = true }, false},
		{"dot call budget", func(in *ResearchInput) { in.DotCalls = 40 }, false},
		{"pane call budget", func(in *ResearchInput) { in.PaneCalls = 120 }, false},
		{"pane dollar budget", func(in *ResearchInput) { in.PaneCostUSD, in.PaneBudgetUSD = 2, 2 }, false},
		{"daily runs", func(in *ResearchInput) { in.Background.DailyCount = 6 }, false},
		{"daily budget", func(in *ResearchInput) { in.Background.CostToday = 1.00 }, false},
		{"kill switch", func(in *ResearchInput) { in.Background.Proactive = false }, false},
	}
	for _, c := range cases {
		in := base
		c.mod(&in)
		if got := DecideResearch(in); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// researchDot researches after 10 idle minutes and never reviews turns, so
// every backend call in these tests is a research pass.
func researchDot(name string) dots.Dot {
	d := dot(name, dots.ModeQueue)
	d.Trigger.OnTurn = false
	d.Research.Enabled = true
	d.Research.Topics = []string{"dependency advisories for packages in package.json"}
	return d
}

// idleRig attaches the dots to a pane with two finished turns.
func idleRig(t *testing.T, ds ...dots.Dot) *proRig {
	t.Helper()
	r := newProRig(t, proOpts{}, ds...)
	r.reader().add(transcript.Turn{User: "add login", Assistant: "edited internal/auth/session.go"})
	r.reader().add(transcript.Turn{User: "add logout"})
	slugs := make([]string, len(ds))
	for i, d := range ds {
		slugs[i] = d.Slug
	}
	r.attach(t, slugs...)
	r.idleNow(t, ds[0].Slug)
	return r
}

// idleNow waits until poll has seen every turn and the idle agent, so the
// idle period has started and a clock jump counts as idle time.
func (r *proRig) idleNow(t *testing.T, slug string) {
	t.Helper()
	r.idlePeriod(t)
	eventually(t, "the roster countdown", func() bool { return r.dotState(slug).Next == "research in 10m" })
}

// idlePeriod waits until poll has seen every turn and the idle agent.
func (r *proRig) idlePeriod(t *testing.T) {
	t.Helper()
	rd := r.reader()
	rd.mu.Lock()
	want := len(rd.turns)
	rd.mu.Unlock()
	eventually(t, "a fresh idle period", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		st := r.e.idle[pane]
		return st != nil && st.turns == want && st.agentIdle
	})
}

func TestIdleResearchRunsOncePerIdlePeriod(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	r.clock.Add(4 * time.Minute)
	eventually(t, "roster countdown", func() bool { return r.dotState("security").Next == "research in 6m" })
	r.clock.Add(5 * time.Minute)
	time.Sleep(80 * time.Millisecond)
	if r.be.n() != 0 {
		t.Fatal("no research before idle_minutes")
	}
	r.clock.Add(2 * time.Minute)
	eventually(t, "a research pass", func() bool { return len(r.be.runs(backend.PassResearch)) == 1 })
	req := r.be.runs(backend.PassResearch)[0]
	if !req.RepoRead || req.Cwd != "/repo" || req.Web || !req.Learn || len(req.Turns) != 2 {
		t.Fatalf("request %+v", req)
	}
	for _, want := range []string{"Topics you watch:\n- dependency advisories for packages in package.json"} {
		if !strings.Contains(req.Question, want) {
			t.Fatalf("brief missing %q:\n%s", want, req.Question)
		}
	}
	time.Sleep(80 * time.Millisecond)
	if n := len(r.be.runs(backend.PassResearch)); n != 1 {
		t.Fatalf("%d research passes in one idle period", n)
	}
	if day := r.today(t); day.Count("security", state.RunResearch) != 1 {
		t.Fatalf("daily count %+v", day.Runs)
	}
	r.turnEnds(transcript.Turn{User: "next"}) // the developer is back: a new period
	r.idleNow(t, "security")
	r.clock.Add(11 * time.Minute)
	eventually(t, "a research pass in the new period", func() bool { return len(r.be.runs(backend.PassResearch)) == 2 })
}

// All relevant results of an idle period become one digest card when the
// developer comes back; nothing is carded before.
func TestDigestAppearsOnTheNextTurnWithEveryPart(t *testing.T) {
	r := idleRig(t, researchDot("Security"), researchDot("Frontend"))
	r.be.setFor("Security", proResult("warn", "lodash 4.17.20 has a prototype pollution advisory."))
	r.be.setFor("Frontend", proResult("info", "The login form lacks autocomplete attributes."))
	r.clock.Add(42 * time.Minute)
	eventually(t, "two research passes", func() bool { return len(r.be.runs(backend.PassResearch)) == 2 })
	eventually(t, "two results collected", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Away != nil && p.Away.Pending == 2
	})
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("no card while the developer is away")
	}
	r.turnEnds(transcript.Turn{User: "back"})
	eventually(t, "a digest card", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if c.Kind != queue.KindDigest || c.Glyph != "☾" || c.Severity != "warn" || c.Mode != "queue" || c.Status != queue.StatusPending || len(c.Parts) != 2 || c.Label != "2 dots · 42 min" {
		t.Fatalf("digest %+v", c)
	}
	for _, want := range []string{"## Should fix", "(Security)", "## Consider", "(Frontend)"} {
		if !strings.Contains(c.Body, want) {
			t.Errorf("body missing %q:\n%s", want, c.Body)
		}
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.Away != nil {
		t.Fatal("the collector is empty after the digest")
	}
	time.Sleep(80 * time.Millisecond)
	if len(r.q.Pending(pane)) != 1 {
		t.Fatal("exactly one digest")
	}
}

func TestCriticalResearchCardsImmediately(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	r.be.set(proResult("critical", "The session secret is committed in config/dev.env."))
	r.clock.Add(11 * time.Minute)
	eventually(t, "a digest without waiting for the developer", func() bool { return len(r.q.Pending(pane)) == 1 })
	if c := r.q.Pending(pane)[0]; c.Kind != queue.KindDigest || c.Severity != "critical" {
		t.Fatalf("%+v", c)
	}
}

// Focusing the agent pane is "back" too.
func TestFocusingThePaneFlushesTheDigest(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	r.be.set(proResult("warn", "No test covers the logout path."))
	r.clock.Add(11 * time.Minute)
	eventually(t, "a result collected", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Away != nil })
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo", Focused: true,
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	eventually(t, "a digest on focus", func() bool { return len(r.q.Pending(pane)) == 1 })
}

// Review Focus 3: the developer comes back while research runs. The turn
// review is not held up, the late result joins one digest, and there is no
// second digest.
func TestReturnMidResearchFoldsIntoOneDigest(t *testing.T) {
	reviewer := dot("Reviewer", dots.ModeQueue)
	r := idleRig(t, researchDot("Security"), researchDot("Frontend"), reviewer)
	r.be.setFor("Security", proResult("warn", "lodash advisory."))
	r.be.setFor("Frontend", proResult("info", "autocomplete attributes."))
	gate := r.be.hold(backend.PassResearch, false)
	r.clock.Add(11 * time.Minute)
	eventually(t, "a research pass in flight", func() bool { return len(r.be.runs(backend.PassResearch)) == 1 })
	before := len(r.be.runs(backend.PassReview))
	r.turnEnds(transcript.Turn{User: "back"})
	eventually(t, "the turn review runs while research is held", func() bool { return len(r.be.runs(backend.PassReview)) > before })
	close(gate)
	eventually(t, "one digest with both parts", func() bool {
		for _, c := range r.q.Pending(pane) {
			if c.Kind == queue.KindDigest && len(c.Parts) == 2 {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)
	n := 0
	for _, c := range r.q.Pending(pane) {
		if c.Kind == queue.KindDigest {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d digests, want 1", n)
	}
}

// A pane that closes while its research runs, its id then reused by a new
// attachment (Phase 7's generations): the late result, even a critical one,
// makes no digest there and charges the new attachment nothing.
func TestResearchForAClosedPaneMakesNoDigest(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	r.be.set(proResult("critical", "The session secret is committed in config/dev.env."))
	gate := r.be.hold(backend.PassResearch, false)
	r.clock.Add(11 * time.Minute)
	eventually(t, "a research pass in flight", func() bool { return len(r.be.runs(backend.PassResearch)) == 1 })
	r.e.dropPane(pane)
	r.attach(t, "security") // the same pane id: a new attachment
	close(gate)
	eventually(t, "the run ended", func() bool { return r.dotState("security").Background == "" })
	time.Sleep(100 * time.Millisecond)
	if len(r.q.Pending(pane)) != 0 {
		t.Fatalf("a closed pane's research reached the new attachment: %+v", r.q.Pending(pane))
	}
	if n := r.usage.Calls(pane, "security"); n != 0 {
		t.Fatalf("the new attachment was charged %d calls", n)
	}
	for _, f := range r.feed.Recent(pane, 50) {
		if f.Outcome == state.OutcomeResearch || f.Dot == digestSlug {
			t.Fatalf("a closed pane's research fed the new attachment: %+v", f)
		}
	}
}

// Research reads the dot's open questions from memory and may write
// research and question entries; observe dots learn without a card.
func TestResearchUsesOpenQuestionsAndWritesResearchKinds(t *testing.T) {
	d := researchDot("Security")
	d.Delivery.Mode = dots.ModeObserve
	r := newProRig(t, proOpts{}, d)
	if _, err := r.mem.Add(testProject, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindQuestion,
		Text: "Is the session cookie rotated on privilege change?", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	r.reader().add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	r.idleNow(t, "security")
	res := proResult("warn", "The cookie is not rotated on role change.")
	res.Remember = []backend.MemoryOp{
		{Op: "add", Kind: "research", Text: "Role changes in internal/auth keep the old session id."},
		{Op: "add", Kind: "question", Text: "Does logout clear the refresh token?"},
	}
	r.be.set(res)
	r.clock.Add(11 * time.Minute)
	eventually(t, "a research pass", func() bool { return len(r.be.runs(backend.PassResearch)) == 1 })
	if q := r.be.runs(backend.PassResearch)[0].Question; !strings.Contains(q, "- [d:m1] Is the session cookie rotated on privilege change?") {
		t.Fatalf("brief:\n%s", q)
	}
	eventually(t, "research entries", func() bool {
		es, _ := r.mem.List(testProject, memory.Filter{Scope: memory.ScopeDot, Dot: "security", Kinds: []memory.Kind{memory.KindResearch, memory.KindQuestion}})
		return len(es) == 3
	})
	time.Sleep(80 * time.Millisecond)
	r.turnEnds(transcript.Turn{User: "back"})
	time.Sleep(100 * time.Millisecond)
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("an observe dot's research never makes a card")
	}
}

func TestResearchDailyCapAndWeb(t *testing.T) {
	d := researchDot("Security")
	d.Research.MaxRunsPerDay, d.Research.Web = 1, true
	r := idleRig(t, d)
	r.clock.Add(11 * time.Minute)
	eventually(t, "the first pass", func() bool { return len(r.be.runs(backend.PassResearch)) == 1 })
	if !r.be.runs(backend.PassResearch)[0].Web {
		t.Fatal("web research on a claude-cli dot gets web tools")
	}
	r.turnEnds(transcript.Turn{User: "again"})
	r.idlePeriod(t) // no countdown: the dot is at its daily cap (fix round 1, item 4)
	r.clock.Add(11 * time.Minute)
	time.Sleep(150 * time.Millisecond)
	if n := len(r.be.runs(backend.PassResearch)); n != 1 {
		t.Fatalf("%d passes; max_runs_per_day = 1", n)
	}
	codex := researchDot("Perf")
	codex.Backend.Kind, codex.Backend.Model, codex.Research.Web = "codex-cli", "", true
	r2 := idleRig(t, codex)
	r2.clock.Add(11 * time.Minute)
	eventually(t, "a codex pass", func() bool { return len(r2.be.runs(backend.PassResearch)) == 1 })
	if r2.be.runs(backend.PassResearch)[0].Web {
		t.Fatal("backends without web tools research without the web")
	}
}

// Sending a digest records each part's outcome under its own dot (Phase 3's
// per-part recording), never under "away"; a digest can only be remembered
// as a project convention.
func TestDigestOutcomesAreRecordedPerDot(t *testing.T) {
	r := idleRig(t, researchDot("Security"), researchDot("Frontend"))
	r.be.setFor("Security", proResult("warn", "lodash 4.17.20 has a prototype pollution advisory."))
	r.be.setFor("Frontend", proResult("warn", "The login form lacks autocomplete attributes."))
	r.clock.Add(11 * time.Minute)
	eventually(t, "two results collected", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Away != nil && p.Away.Pending == 2
	})
	r.turnEnds(transcript.Turn{User: "back"})
	eventually(t, "a digest", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if err := r.e.Remember(c.ID, memory.AsFinding, ""); err == nil || !strings.Contains(err.Error(), "digest card can only be remembered as a project convention") {
		t.Fatalf("remember: %v", err)
	}
	if err := r.e.SendCardsSeen([]string{c.ID}, map[string]int{c.ID: len(c.Parts)}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "one outcome per part", func() bool {
		led, _ := r.mem.Ledger(testProject)
		seen := map[string]bool{}
		for _, o := range led {
			seen[o.Dot] = true
		}
		return len(led) == 2 && seen["security"] && seen["frontend"] && !seen["away"]
	})
}

// Binding (Task 10 review): dedupe uses bgQueued, reserved in the trigger. A
// research job still waiting for the background slot (queued, not running)
// is not reserved a second time when the next idle period comes due.
func TestQueuedResearchIsNotReservedTwice(t *testing.T) {
	r := idleRig(t, researchDot("Security"), researchDot("Frontend"))
	gate := r.be.hold(backend.PassResearch, false)
	var closeOnce sync.Once
	release := func() { closeOnce.Do(func() { close(gate) }) }
	defer release()
	r.clock.Add(11 * time.Minute)
	eventually(t, "one pass running, one queued", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.be.runs(backend.PassResearch)) == 1 && len(r.e.bgQueued) == 2
	})
	r.turnEnds(transcript.Turn{User: "back"})
	r.idleNow(t, "security")
	r.clock.Add(11 * time.Minute)
	time.Sleep(150 * time.Millisecond) // several polls in the due new period
	r.e.mu.Lock()
	sec, front := r.e.bgQueued[key{pane, "security"}], r.e.bgQueued[key{pane, "frontend"}]
	r.e.mu.Unlock()
	// Each job counts 2 (the trigger's reservation plus runBackground's
	// own); a second reservation for the same key would make it 3 or more.
	if sec > 2 || front > 2 {
		t.Fatalf("bgQueued security=%d frontend=%d: a queued research job was reserved twice", sec, front)
	}
	release()
	eventually(t, "every reservation released", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.e.bgQueued) == 0
	})
}

// The digest card is added with the pane check in the same e.mu section: a
// pane closed between collecting the parts and adding the card gets none,
// pending or orphaned.
func TestPaneClosedDuringDigestFlushGetsNoCard(t *testing.T) {
	var rp atomic.Pointer[proRig]
	var once sync.Once
	r := newProRig(t, proOpts{hook: func(point string) {
		if point == "away-before-lock" {
			once.Do(func() { rp.Load().e.dropPane(pane) })
		}
	}}, researchDot("Security"))
	rp.Store(r)
	r.reader().add(transcript.Turn{User: "add login", Assistant: "edited internal/auth/session.go"})
	r.attach(t, "security")
	r.idleNow(t, "security")
	r.be.set(proResult("critical", "The session secret is committed in config/dev.env."))
	r.clock.Add(11 * time.Minute)
	eventually(t, "the pane dropped mid-flush", func() bool { _, ok := r.atts.Get(pane); return !ok })
	time.Sleep(100 * time.Millisecond)
	for _, c := range r.q.All() {
		if c.Kind == queue.KindDigest {
			t.Fatalf("a closed pane got a digest card: %+v", c)
		}
	}
}

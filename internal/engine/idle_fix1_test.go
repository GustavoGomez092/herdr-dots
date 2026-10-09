package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// setPane is what Herdr reports for the agent pane on the next poll.
func (r *proRig) setPane(status herdr.AgentStatus, focused bool) {
	r.srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: status, Cwd: "/repo", Focused: focused,
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
}

// idleOf reads the pane's idle state under e.mu.
func (r *proRig) idleOf(f func(st *idleState) bool) bool {
	r.e.mu.Lock()
	defer r.e.mu.Unlock()
	st := r.e.idle[pane]
	return st != nil && f(st)
}

func (r *proRig) digests() []queue.Card {
	var out []queue.Card
	for _, c := range r.q.All() {
		if c.Kind == queue.KindDigest {
			out = append(out, c)
		}
	}
	return out
}

// firstRunAndOther waits for the first research pass (held by the gate) and
// names its dot and the other one.
func firstRunAndOther(t *testing.T, r *proRig) (first, other string) {
	t.Helper()
	eventually(t, "a research pass in flight", func() bool { return len(r.be.runs(backend.PassResearch)) == 1 })
	first, other = r.be.runs(backend.PassResearch)[0].DotName, "Frontend"
	if first == "Frontend" {
		other = "Security"
	}
	return first, other
}

// P8-R11 (b): once the developer is back, a critical part waits for the runs
// in flight like any other: a return mid-research yields exactly one card,
// whichever job wins the background slot.
func TestReturnMidResearchWithACriticalPartMakesOneCard(t *testing.T) {
	for _, criticalFirst := range []bool{true, false} {
		name := "critical runs second"
		if criticalFirst {
			name = "critical runs first"
		}
		t.Run(name, func(t *testing.T) {
			r := idleRig(t, researchDot("Security"), researchDot("Frontend"))
			gate := r.be.hold(backend.PassResearch, false)
			r.clock.Add(11 * time.Minute)
			first, second := firstRunAndOther(t, r)
			crit, plain := first, second
			if !criticalFirst {
				crit, plain = second, first
			}
			r.be.setFor(crit, proResult("critical", "The session secret is committed."))
			r.be.setFor(plain, proResult("info", "autocomplete attributes."))
			r.turnEnds(transcript.Turn{User: "back"})
			eventually(t, "the return seen", func() bool { return r.idleOf(func(st *idleState) bool { return st.turns == 3 }) })
			close(gate)
			eventually(t, "one card with both parts", func() bool {
				ds := r.digests()
				return len(ds) == 1 && len(ds[0].Parts) == 2
			})
			time.Sleep(100 * time.Millisecond)
			if ds := r.digests(); len(ds) != 1 || ds[0].Severity != "critical" {
				t.Fatalf("digests %+v", ds)
			}
		})
	}
}

// P8-R11 (a): while the developer is away, a critical part cards the
// period's digest at once; a later part of the same period joins that card.
func TestCriticalWhileAwayThenALaterPartJoinsTheCard(t *testing.T) {
	r := idleRig(t, researchDot("Security"), researchDot("Frontend"))
	gate1 := r.be.hold(backend.PassResearch, false)
	r.clock.Add(11 * time.Minute)
	first, second := firstRunAndOther(t, r)
	r.be.setFor(first, proResult("critical", "The session secret is committed."))
	r.be.setFor(second, proResult("warn", "No test covers logout."))
	gate2 := r.be.hold(backend.PassResearch, false) // the second pass waits here
	close(gate1)
	eventually(t, "the critical card at once", func() bool { ds := r.digests(); return len(ds) == 1 && len(ds[0].Parts) == 1 })
	id := r.digests()[0].ID
	close(gate2)
	eventually(t, "the later part joins it", func() bool { ds := r.digests(); return len(ds) == 1 && len(ds[0].Parts) == 2 })
	c := r.digests()[0]
	if c.ID != id || c.Status != queue.StatusPending || c.Severity != "critical" || c.Label != "2 dots · 11 min" ||
		!strings.Contains(c.Body, "("+first+")") || !strings.Contains(c.Body, "("+second+")") {
		t.Fatalf("%+v", c)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.Away != nil {
		t.Fatal("nothing waits in the collector")
	}
	time.Sleep(80 * time.Millisecond)
	if len(r.digests()) != 1 {
		t.Fatal("exactly one digest")
	}
}

// P8-R11 (a): a critical card already sent takes no more parts: a later
// part of the period forms a new card.
func TestCriticalCardSentThenALaterPartMakesANewCard(t *testing.T) {
	r := idleRig(t, researchDot("Security"), researchDot("Frontend"))
	gate1 := r.be.hold(backend.PassResearch, false)
	r.clock.Add(11 * time.Minute)
	first, second := firstRunAndOther(t, r)
	r.be.setFor(first, proResult("critical", "The session secret is committed."))
	r.be.setFor(second, proResult("critical", "The admin route has no auth check."))
	gate2 := r.be.hold(backend.PassResearch, false)
	close(gate1)
	eventually(t, "the critical card", func() bool { return len(r.digests()) == 1 })
	sent := r.digests()[0]
	if err := r.e.SendCardsSeen([]string{sent.ID}, map[string]int{sent.ID: len(sent.Parts)}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the card sent", func() bool { c, _ := r.q.Get(sent.ID); return c.Status == queue.StatusSent })
	close(gate2)
	eventually(t, "a new card for the later part", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	if c.ID == sent.ID || c.Kind != queue.KindDigest || len(c.Parts) != 1 || c.Parts[0].DotName != second {
		t.Fatalf("%+v", c)
	}
	if old, _ := r.q.Get(sent.ID); len(old.Parts) != 1 {
		t.Fatalf("the sent card changed: %+v", old)
	}
}

// Only the agent working, a new turn, a new session or the pane gaining focus
// is activity: a blocked, unknown or empty status flapping starts no period
// and flushes nothing.
func TestUnknownStatusFlappingStartsNoPeriod(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	r.be.set(proResult("warn", "No test covers logout."))
	r.clock.Add(11 * time.Minute)
	eventually(t, "a result collected", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Away != nil })
	var period int
	r.idleOf(func(st *idleState) bool { period = st.period; return true })
	for _, s := range []herdr.AgentStatus{herdr.StatusUnknown, herdr.StatusBlocked, "", herdr.StatusUnknown} {
		r.setPane(s, false)
		eventually(t, "the flap seen", func() bool { return r.idleOf(func(st *idleState) bool { return !st.agentIdle }) })
		r.setPane(herdr.StatusIdle, false)
		eventually(t, "idle again", func() bool { return r.idleOf(func(st *idleState) bool { return st.agentIdle }) })
	}
	if !r.idleOf(func(st *idleState) bool { return st.period == period }) {
		t.Fatal("a status flap started a new idle period")
	}
	if len(r.q.Pending(pane)) != 0 {
		t.Fatal("a status flap flushed the digest")
	}
	r.setPane(herdr.StatusWorking, false) // the agent working is activity
	eventually(t, "a digest once the agent works", func() bool { return len(r.q.Pending(pane)) == 1 })
}

// A failed digest add keeps the parts: the next return cards them.
func TestDigestAddFailureKeepsTheParts(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	r.be.set(proResult("warn", "No test covers logout."))
	r.clock.Add(11 * time.Minute)
	eventually(t, "a result collected", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Away != nil })
	qpath := filepath.Join(r.dir, "session", "queue.json")
	if err := os.Mkdir(qpath, 0o700); err != nil { // queue.json can't be written: Add fails
		t.Fatal(err)
	}
	var period int
	r.idleOf(func(st *idleState) bool { period = st.period; return true })
	r.setPane(herdr.StatusIdle, true) // focus: the developer is back
	eventually(t, "the return seen", func() bool { return r.idleOf(func(st *idleState) bool { return st.period > period }) })
	time.Sleep(80 * time.Millisecond)
	if p, _ := r.e.Snapshot().Pane(pane); p.Away == nil || p.Away.Pending != 1 {
		t.Fatalf("the parts were dropped after a failed add: %+v", p.Away)
	}
	if err := os.Remove(qpath); err != nil {
		t.Fatal(err)
	}
	r.setPane(herdr.StatusIdle, false)
	eventually(t, "focus lost seen", func() bool { return r.idleOf(func(st *idleState) bool { return !st.focused }) })
	r.setPane(herdr.StatusIdle, true)
	eventually(t, "the digest on the next return", func() bool {
		ps := r.q.Pending(pane)
		return len(ps) == 1 && len(ps[0].Parts) == 1
	})
}

// The roster hint shows only a pass that could run: not for a paused dot, a
// dot at its daily cap, or a pane over its session budget.
func TestResearchHintHidesWhenThePassCannotRun(t *testing.T) {
	t.Run("paused", func(t *testing.T) {
		r := idleRig(t, researchDot("Security"))
		if err := r.e.SetPaused(pane, "security", true); err != nil {
			t.Fatal(err)
		}
		if n := r.dotState("security").Next; n != "" {
			t.Fatalf("hint %q for a paused dot", n)
		}
	})
	t.Run("daily cap", func(t *testing.T) {
		d := researchDot("Security")
		d.Research.MaxRunsPerDay = 1
		r := idleRig(t, d)
		if err := r.daily.Add("security", state.RunResearch, 0, r.clock.Now()); err != nil {
			t.Fatal(err)
		}
		r.e.refreshToday()
		if n := r.dotState("security").Next; n != "" {
			t.Fatalf("hint %q at the daily cap", n)
		}
	})
	t.Run("pane budget", func(t *testing.T) {
		r := idleRig(t, researchDot("Security"))
		if err := r.usage.Add(pane, "other", state.Delta{Calls: 120}); err != nil {
			t.Fatal(err)
		}
		if n := r.dotState("security").Next; n != "" {
			t.Fatalf("hint %q over the pane budget", n)
		}
	})
}

// A pane idle for two days, polled all along, gets one pass in its period.
func TestLongIdleMakesOnePassPerPeriod(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	for range 48 {
		r.clock.Add(time.Hour)
		time.Sleep(25 * time.Millisecond) // a poll or so per hour
	}
	time.Sleep(80 * time.Millisecond)
	if n := len(r.be.runs(backend.PassResearch)); n != 1 {
		t.Fatalf("%d passes in one 48h idle period", n)
	}
}

// Focus on/off cycles start new periods, but max_runs_per_day still bounds
// the passes.
func TestFocusCyclesAreBoundedByTheDailyCap(t *testing.T) {
	d := researchDot("Security")
	d.Research.MaxRunsPerDay = 2
	r := idleRig(t, d)
	for i := range 4 {
		r.setPane(herdr.StatusIdle, true)
		eventually(t, "focus seen", func() bool { return r.idleOf(func(st *idleState) bool { return st.focused }) })
		r.setPane(herdr.StatusIdle, false)
		eventually(t, "focus lost seen", func() bool { return r.idleOf(func(st *idleState) bool { return !st.focused }) })
		r.clock.Add(11 * time.Minute)
		if i < 2 {
			eventually(t, "a pass", func() bool { return len(r.be.runs(backend.PassResearch)) == i+1 })
		}
	}
	time.Sleep(150 * time.Millisecond)
	if n := len(r.be.runs(backend.PassResearch)); n != 2 {
		t.Fatalf("%d passes; max_runs_per_day = 2", n)
	}
}

// The last Detach clears the pane's idle state in the section that bumps
// its generation: a later attach collects from scratch.
func TestLastDetachClearsIdleState(t *testing.T) {
	r := idleRig(t, researchDot("Security"))
	r.be.set(proResult("warn", "No test covers logout."))
	r.clock.Add(11 * time.Minute)
	eventually(t, "a result collected", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Away != nil })
	if err := r.e.Detach(pane, "security"); err != nil {
		t.Fatal(err)
	}
	r.e.mu.Lock()
	_, ok := r.e.idle[pane]
	r.e.mu.Unlock()
	if ok {
		t.Fatal("idle state survived the last Detach")
	}
	r.attach(t, "security")
	time.Sleep(80 * time.Millisecond)
	if p, _ := r.e.Snapshot().Pane(pane); p.Away != nil {
		t.Fatalf("a new attachment inherited research: %+v", p.Away)
	}
}

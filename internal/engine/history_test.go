package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

// histRig is a rig whose engine keeps history in a temp dir.
func histRig(t *testing.T, ds ...dots.Dot) (*rig, *history.Store) {
	t.Helper()
	hs := history.Open(filepath.Join(t.TempDir(), "history"))
	r := newRigWith(t, func(d *Deps) { d.History, d.SessionKey = hs, "sess1" }, ds...)
	return r, hs
}

func histRuns(hs *history.Store, slug string) []history.Record {
	rs, _ := hs.Query(slug, time.Time{}, 0)
	return rs
}

func TestReviewRunAndSendAreRecorded(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "a card", func() bool { return len(r.q.Pending(pane)) == 1 })
	c := r.q.Pending(pane)[0]
	eventually(t, "run recorded", func() bool { return len(histRuns(hs, "security")) == 1 })
	run := histRuns(hs, "security")[0]
	if run.Kind != history.KindReview || run.Outcome != state.OutcomeFlagged || run.Severity != "critical" ||
		run.Card != history.CardKey("sess1", c.ID) || run.Pane != pane || run.In != 10 || run.Out != 5 ||
		run.Excerpt != "JWT in localStorage" || !run.CostUnknown {
		t.Fatalf("run %+v", run)
	}
	if err := r.e.SendCards([]string{c.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "sent recorded", func() bool { return histRuns(hs, "security")[0].Resolution == history.ResSent })
}

func TestPassSummonAndErrorRunsAreRecorded(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security")
	r.turnEnds(t, transcript.Turn{User: "rename a var"})
	eventually(t, "pass recorded", func() bool { rs := histRuns(hs, "security"); return len(rs) == 1 && rs[0].Outcome == state.OutcomePass })
	r.be.set(critical)
	if err := r.e.Summon(context.Background(), pane, "security", "anything?"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "summon recorded", func() bool { rs := histRuns(hs, "security"); return len(rs) == 2 && rs[0].Kind == history.KindSummon })
	r.be.fail(errors.New("claude exited 1"))
	r.turnEnds(t, transcript.Turn{User: "next"})
	eventually(t, "error recorded", func() bool {
		rs := histRuns(hs, "security")
		return len(rs) == 3 && rs[0].Outcome == state.OutcomeError && strings.Contains(rs[0].Excerpt, "exited 1")
	})
}

func TestDismissEditAndInjectResolutions(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue), dot("Fast", dots.ModeInject))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security", "fast")
	eventually(t, "security card", func() bool { return len(r.q.Pending(pane)) == 1 && r.q.Pending(pane)[0].Dot == "security" })
	eventually(t, "inject resolution", func() bool {
		rs := histRuns(hs, "fast")
		return len(rs) == 1 && rs[0].Resolution == history.ResInjected
	})
	id := r.q.Pending(pane)[0].ID
	if err := r.e.Dismiss(id); err != nil {
		t.Fatal(err)
	}
	// The card can be pending before its run's history record is written.
	eventually(t, "dismissed", func() bool {
		rs := histRuns(hs, "security")
		return len(rs) > 0 && rs[0].Resolution == history.ResDismissed
	})

	r.turnEnds(t, transcript.Turn{User: "more"})
	eventually(t, "second security card", func() bool {
		for _, c := range r.q.Pending(pane) {
			if c.Dot == "security" {
				id = c.ID
				return true
			}
		}
		return false
	})
	if err := r.e.Edit(id, "edited text"); err != nil {
		t.Fatal(err)
	}
	if err := r.e.SendCards([]string{id}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "edited", func() bool { return histRuns(hs, "security")[0].Resolution == history.ResEdited })
	st, _ := hs.Stats("security", time.Now().Add(time.Minute), time.Hour)
	if st.Resolved != 2 || st.Accepted != 1 || st.Dismissed != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestClosedPaneRecordsOrphanedCards(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.srv.RemovePane(pane)
	eventually(t, "orphaned", func() bool {
		rs := histRuns(hs, "security")
		return len(rs) == 1 && rs[0].Resolution == history.ResOrphaned
	})
}

func TestNoHistoryStoreKeepsEngineBehavior(t *testing.T) {
	r := newRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "a card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if err := r.e.Dismiss(r.q.Pending(pane)[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryKindForEveryRunFlavor(t *testing.T) {
	for want, o := range map[string]runOpts{
		history.KindReview:  {},
		history.KindSummon:  {summon: true},
		history.KindMemory:  {memoryPass: true},
		history.KindCouncil: {summon: true, noForce: true},
		history.KindAsk:     {summon: true, ask: true},
	} {
		if got := o.historyKind(); got != want {
			t.Errorf("%+v: got %s, want %s", o, got, want)
		}
	}
}

// Phase 9 provenance rule: an imported dismissed entry, even once the
// developer approved it, guides the prompt but never silences the dot; only
// outcome and user entries suppress repeats.
func TestImportedDismissedMemoryNeverSuppresses(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	if _, err := r.mem.ImportPack(testProject, "security", []memory.Entry{
		{Scope: memory.ScopeDot, Kind: memory.KindDismissed, Text: jwtFirst.Message},
	}); err != nil {
		t.Fatal(err)
	}
	es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"})
	if len(es) != 1 || es[0].Status != memory.StatusProposed {
		t.Fatalf("an imported entry waits for approval: %+v", es)
	}
	if _, err := r.mem.Update(testProject, es[0].Ref(), func(x *memory.Entry) error { x.Status = memory.StatusActive; return nil }); err != nil {
		t.Fatal(err)
	}
	r.be.set(backend.Result{Verdict: jwtAgain})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "a card despite the imported rule", func() bool { return len(r.q.Pending(pane)) == 1 })
	if !strings.Contains(r.be.last().Memory, "(imported)") {
		t.Fatalf("the imported rule is in the prompt, labelled:\n%s", r.be.last().Memory)
	}
}

// Phase 8's background run kinds are history kinds verbatim.
func TestBackgroundKindsAreHistoryKinds(t *testing.T) {
	if state.RunResearch != history.KindResearch || state.RunScheduled != history.KindScheduled || state.RunCommit != history.KindCommit {
		t.Fatal("state.Run* and history.Kind* must match")
	}
}

// Phase 7: a draft is one "draft" run linked to its patch card; discarding
// the patch and sending another are its resolutions.
func TestDraftRunAndPatchOutcomesAreRecorded(t *testing.T) {
	hs := history.Open(filepath.Join(t.TempDir(), "history"))
	r := newDraftRig(t, draftOpts{deps: func(d *Deps) { d.History, d.SessionKey = hs, "sess1" }}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	var cards []string
	for i := 0; i < 2; i++ {
		if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
			t.Fatal(err)
		}
		c := r.patchCard(t)
		r.draftsDone(t)
		cards = append(cards, c.ID)
		if i == 0 {
			if err := r.e.DiscardPatch(c.ID); err != nil {
				t.Fatal(err)
			}
		} else if err := r.e.SendCards([]string{c.ID}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "two draft runs with their resolutions", func() bool {
		var drafts []history.Record
		for _, rec := range histRuns(hs, "security") {
			if rec.Kind == history.KindDraft {
				drafts = append(drafts, rec)
			}
		}
		return len(drafts) == 2 && drafts[1].Card == history.CardKey("sess1", cards[0]) && drafts[1].Resolution == history.ResDiscarded &&
			drafts[0].Card == history.CardKey("sess1", cards[1]) && drafts[0].Resolution == history.ResSent &&
			drafts[0].Outcome == state.OutcomeDrafted && !strings.Contains(drafts[0].Excerpt, "diff --git")
	})
}

// Phase 8: a scheduled run is a "scheduled" run in the history.
func TestScheduledRunIsRecorded(t *testing.T) {
	hs := history.Open(filepath.Join(t.TempDir(), "history"))
	r := newProRig(t, proOpts{cwd: t.TempDir(), deps: func(d *Deps) { d.History, d.SessionKey = hs, "sess1" }},
		scheduledDot("Security", "0 11 * * *"))
	r.attach(t, "security")
	eventually(t, "the next slot is planned", func() bool { return r.dotState("security").Next == "scheduled Wed 11:00" })
	r.be.set(proResult("warn", "Commit 3f2a skips the CSRF check."))
	r.clock.Add(time.Hour + 20*time.Second)
	eventually(t, "the scheduled run recorded", func() bool {
		for _, rec := range histRuns(hs, "security") {
			if rec.Kind == history.KindScheduled && rec.Outcome == state.OutcomeFlagged && rec.Severity == "warn" && rec.Project == testProject {
				return true
			}
		}
		return false
	})
}

// A council card or an away digest speaks for several dots: each dot that
// contributed a part gets the resolution.
func TestMultiDotCardsResolveForEachContributor(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.e.spawnResolve(queue.Card{ID: "9", Kind: queue.KindDigest, Dot: "away", Parts: []queue.CardPart{
		{Dot: "security", Severity: "warn"}, {Dot: "frontend", Severity: "info"}, {Dot: "docs", Failed: true}, {Dot: "perf"},
	}}, history.ResSent)
	eventually(t, "two resolutions", func() bool {
		slugs, _ := hs.Slugs()
		return len(slugs) == 2
	})
	for _, slug := range []string{"security", "frontend"} {
		if st, _ := hs.Stats(slug, time.Now().Add(time.Minute), time.Hour); st.Accepted != 1 {
			t.Fatalf("%s: %+v", slug, st)
		}
	}
}

func TestResolutionForEveryOutcome(t *testing.T) {
	for _, c := range []struct {
		kind   string
		edited bool
		want   string
	}{
		{memory.OutcomeSent, false, history.ResSent},
		{memory.OutcomeSent, true, history.ResEdited},
		{memory.OutcomeEdited, false, history.ResEdited},
		{memory.OutcomeDismissed, false, history.ResDismissed},
		{"applied", false, history.ResApplied},
		{"discarded", false, history.ResDiscarded},
		{"something else", false, ""},
	} {
		if got := resolutionFor(c.kind, c.edited); got != c.want {
			t.Errorf("%s/%v: got %q, want %q", c.kind, c.edited, got, c.want)
		}
	}
}

// Ruling P9-R8: an imported dismissed entry never suppresses a repeat,
// approved or not, in dot scope or in project scope; the same text from an
// outcome or the developer still does.
func TestSuppressedBySkipsImportedEntriesInEveryScope(t *testing.T) {
	msg := jwtAgain.Message
	for _, sc := range []memory.Scope{memory.ScopeDot, memory.ScopeProject} {
		for _, st := range []memory.Status{memory.StatusActive, memory.StatusProposed} {
			imp := memory.Entry{Scope: sc, Kind: memory.KindDismissed, Dot: "security", Text: jwtFirst.Message,
				Status: st, Source: memory.SourceImport, From: "security"}
			if ref, ok := suppressedBy(memRun{cands: []memory.Entry{imp}}, msg, 3, "s1"); ok {
				t.Errorf("%s/%s: an imported entry suppressed the verdict (%s)", sc, st, ref)
			}
		}
	}
	for _, src := range []memory.Source{memory.SourceOutcome, memory.SourceUser} {
		own := memory.Entry{Scope: memory.ScopeDot, Kind: memory.KindDismissed, Dot: "security", Text: jwtFirst.Message,
			Status: memory.StatusActive, Source: src}
		if _, ok := suppressedBy(memRun{cands: []memory.Entry{own}}, msg, 3, "s1"); !ok {
			t.Errorf("a %s dismissed entry no longer suppresses", src)
		}
	}
}

// The engine path for a project-scope import: approved, it still never
// silences the dot.
func TestImportedProjectDismissedMemoryNeverSuppresses(t *testing.T) {
	d := dot("Security", dots.ModeQueue)
	d.Memory.ReadProject = true
	r := newMemRig(t, memOpts{}, d)
	if _, err := r.mem.ImportPack(testProject, "security", []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindDismissed, Text: jwtFirst.Message},
	}); err != nil {
		t.Fatal(err)
	}
	es := r.entries(memory.Filter{Scope: memory.ScopeProject})
	if len(es) != 1 || es[0].Status != memory.StatusProposed {
		t.Fatalf("an imported entry waits for approval: %+v", es)
	}
	if _, err := r.mem.Update(testProject, es[0].Ref(), func(x *memory.Entry) error { x.Status = memory.StatusActive; return nil }); err != nil {
		t.Fatal(err)
	}
	r.be.set(backend.Result{Verdict: jwtAgain})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "a card despite the imported project rule", func() bool { return len(r.q.Pending(pane)) == 1 })
}

// Carry-over: a research or commit run whose dot is detached (or whose pane
// closes) mid-run is dropped with no feed line and no card, but its spend is
// in the daily total, so history records it as "dropped" with its cost.
func TestDroppedBackgroundRunIsRecordedWithItsCost(t *testing.T) {
	for _, kind := range []string{state.RunResearch, state.RunCommit} {
		t.Run(kind, func(t *testing.T) {
			hs := history.Open(filepath.Join(t.TempDir(), "history"))
			sec, fe := dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue)
			r := newProRig(t, proOpts{deps: func(d *Deps) { d.History, d.SessionKey = hs, "sess1" }}, sec, fe)
			r.attach(t, "security", "frontend")
			res := proResult("warn", "Session cookie lacks HttpOnly.")
			res.CostUSD, res.CostKnown = 0.02, true
			r.be.set(res)
			open := holdAll(t, r)
			done := make(chan struct{})
			j := paneJob(r, fe)
			j.kind, j.label = kind, kind
			j.done = func() { close(done) }
			r.e.spawn(func() { r.e.runBackground(j) })
			eventually(t, "the run is out", func() bool { return r.be.n() == 1 })
			if err := r.e.Detach(pane, "frontend"); err != nil {
				t.Fatal(err)
			}
			open()
			<-done
			eventually(t, "a dropped record", func() bool {
				rs := histRuns(hs, "frontend")
				return len(rs) == 1 && rs[0].Kind == kind && rs[0].Outcome == "dropped" && rs[0].CostUSD == 0.02 &&
					!rs[0].CostUnknown && rs[0].In == 10
			})
		})
	}
}

// Carry-over: an MCP ask cancelled because its pane closed may still have
// been charged by its provider; it is recorded as "dropped".
func TestCancelledAskIsRecordedDropped(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.attach(t, "security") // no turns yet: no review
	gate := make(chan struct{})
	r.be.mu.Lock()
	r.be.gate = gate
	r.be.mu.Unlock()
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(open)
	asked := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := r.e.Ask(ctx, pane, "security", "q", false)
		asked <- err
	}()
	eventually(t, "the ask is in flight", func() bool { return r.be.n() == 1 })
	r.e.dropPane(pane)
	open()
	if err := <-asked; !errors.Is(err, ErrPaneClosed) {
		t.Fatalf("ask: %v, want ErrPaneClosed", err)
	}
	eventually(t, "the ask recorded dropped", func() bool {
		rs := histRuns(hs, "security")
		return len(rs) == 1 && rs[0].Kind == history.KindAsk && rs[0].Outcome == "dropped" && rs[0].In == 10
	})
}

// Detaching a pane's last dot orphans its cards as closing it does: each gets
// an "orphaned" resolution.
func TestLastDetachRecordsOrphanedCards(t *testing.T) {
	r, hs := histRig(t, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "x"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if err := r.e.Detach(pane, "security"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "orphaned", func() bool {
		rs := histRuns(hs, "security")
		return len(rs) == 1 && rs[0].Resolution == history.ResOrphaned
	})
}

// A history store that can't be written is logged and never fails or blocks
// the run: the card still arrives and can be dismissed.
func TestHistoryWriteFailureNeverBlocksARun(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var logs []string
	r := newRigWith(t, func(d *Deps) {
		d.History, d.SessionKey = history.Open(filepath.Join(notDir, "history")), "sess1"
		d.Logf = func(f string, a ...any) { mu.Lock(); logs = append(logs, fmt.Sprintf(f, a...)); mu.Unlock() }
	}, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "add login"})
	r.be.set(critical)
	r.attach(t, "security")
	eventually(t, "a card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if err := r.e.Dismiss(r.q.Pending(pane)[0].ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the failed writes are logged", func() bool {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, l := range logs {
			if strings.HasPrefix(l, "history security:") {
				n++
			}
		}
		return n == 2 // the run and its resolution
	})
}

// An inbox card claimed for a pane that closed meanwhile is orphaned by the
// claim's re-check, and gets an "orphaned" resolution like any other.
func TestInboxClaimForAClosedPaneRecordsOrphaned(t *testing.T) {
	hs := history.Open(filepath.Join(t.TempDir(), "history"))
	var rp atomic.Pointer[proRig]
	var armed atomic.Bool
	r := newProRig(t, proOpts{
		deps: func(d *Deps) { d.History, d.SessionKey = hs, "sess1" },
		hook: func(point string) {
			if point == "inbox-before-claim" && armed.CompareAndSwap(true, false) {
				rp.Load().e.dropPane(pane)
			}
		},
	}, dot("Security", dots.ModeQueue))
	rp.Store(r)
	r.attach(t, "security")
	eventually(t, "the first inbox sweep", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return !r.e.inboxAt.IsZero()
	})
	time.Sleep(100 * time.Millisecond) // the attach's and the sweep's claims of the empty inbox end
	if err := r.inbox.Put(queue.Card{Dot: "security", Project: testProject, Body: "x", Mode: "queue", Origin: state.RunScheduled}); err != nil {
		t.Fatal(err)
	}
	armed.Store(true)
	r.e.claimInbox(pane, testProject)
	eventually(t, "an orphaned resolution", func() bool {
		b, _ := os.ReadFile(filepath.Join(hs.Dir, "security.jsonl"))
		return strings.Count(string(b), `"resolution":"orphaned"`) == 1
	})
}

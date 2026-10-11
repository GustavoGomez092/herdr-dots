package engine

import (
	"context"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// Post-rc (Linux acceptance): a finding the dot raises again while its card
// is still waiting adds no second card.

var sqlConcat = backend.Verdict{Relevant: true, Severity: "warn", Message: "The /search handler builds its SQL query by string concatenation; use bound parameters."}

func (r *memRig) dotCalls(slug string) int {
	pv, _ := r.e.Snapshot().Pane(pane)
	for _, d := range pv.Dots {
		if d.Slug == slug {
			return d.Calls
		}
	}
	return -1
}

func TestRepeatWhileCardPendingAddsNoSecondCard(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: jwtFirst}, backend.Result{Verdict: jwtAgain})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "first card", func() bool { return len(r.q.Pending(pane)) == 1 })
	first := r.q.Pending(pane)[0].ID
	r.turnEnds("s1", transcript.Turn{User: "refactor auth"})
	eventually(t, "duplicate feed line", func() bool { return r.hasFeed(state.OutcomeSuppressed, "duplicate of card "+first) })
	if p := r.q.Pending(pane); len(p) != 1 || p[0].ID != first {
		t.Fatalf("pending %+v, want only card %s", p, first)
	}
	if n := r.dotCalls("security"); n != 2 {
		t.Fatalf("calls %d, want the deduped run charged too (2)", n)
	}
	eventually(t, "roster says duplicate", func() bool { return r.dotStatus("security") == view.StatusDuplicate })
	// The dedupe writes nothing to memory: memory records only outcomes.
	if es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security"}); len(es) != 0 {
		t.Fatalf("memory entries %+v, want none", es)
	}
}

func TestDismissedCardStillSuppressedByMemoryNotDeduped(t *testing.T) {
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
	eventually(t, "suppressed by memory", func() bool { return r.hasFeed(state.OutcomeSuppressed, "similar to d:m1") })
	if r.hasFeed(state.OutcomeSuppressed, "duplicate of card") {
		t.Fatal("a dismissed card must never dedupe; memory handles it")
	}
	if n := len(r.q.Pending(pane)); n != 0 {
		t.Fatalf("pending %d, want 0", n)
	}
	eventually(t, "roster says suppressed", func() bool { return r.dotStatus("security") == view.StatusSuppressed })
}

func TestDifferentFindingWhileCardPendingAddsCard(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: jwtFirst}, backend.Result{Verdict: sqlConcat})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "first card", func() bool { return len(r.q.Pending(pane)) == 1 })
	r.turnEnds("s1", transcript.Turn{User: "add search"})
	eventually(t, "second card", func() bool { return len(r.q.Pending(pane)) == 2 })
	if r.hasFeed(state.OutcomeSuppressed, "duplicate of card") {
		t.Fatal("a different finding was deduped")
	}
	eventually(t, "roster says flagged", func() bool { return r.dotStatus("security") == view.StatusFlagged })
}

func TestOtherDotSameTextAddsCard(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue), dot("Privacy", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: jwtFirst}) // every run, of either dot, says the same
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "security's card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if err := r.e.Attach(context.Background(), pane, []string{"privacy"}); err != nil {
		t.Fatal(err)
	}
	r.turnEnds("s1", transcript.Turn{User: "refactor auth"})
	eventually(t, "privacy's card", func() bool {
		for _, c := range r.q.Pending(pane) {
			if c.Dot == "privacy" {
				return true
			}
		}
		return false
	})
	eventually(t, "security's repeat deduped", func() bool { return r.hasFeed(state.OutcomeSuppressed, "duplicate of card") })
	n := map[string]int{}
	for _, c := range r.q.Pending(pane) {
		n[c.Dot]++
	}
	if n["security"] != 1 || n["privacy"] != 1 {
		t.Fatalf("pending per dot %v, want one each", n)
	}
}

func TestSummonIsNeverDeduped(t *testing.T) {
	r := newMemRig(t, memOpts{}, dot("Security", dots.ModeQueue))
	r.be.set(backend.Result{Verdict: jwtFirst}, backend.Result{Verdict: jwtAgain})
	r.reader("s1").add(transcript.Turn{User: "add login"})
	r.attach(t, "security")
	eventually(t, "first card", func() bool { return len(r.q.Pending(pane)) == 1 })
	if err := r.e.Summon(context.Background(), pane, "security", "is the token safe?"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "summoned card", func() bool { return len(r.q.Pending(pane)) == 2 })
	if r.hasFeed(state.OutcomeSuppressed, "duplicate of card") {
		t.Fatal("a summon was deduped")
	}
}

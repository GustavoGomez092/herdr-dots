package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/transcript"
)

// councilCardFor queues a council card with two contributing members, one
// member with nothing to add and one that failed.
func councilCardFor(t *testing.T, r *memRig) queue.Card {
	t.Helper()
	c, err := r.q.Add(queue.Card{Kind: queue.KindCouncil, Dot: "council", DotName: transcript.CouncilName, Glyph: transcript.CouncilGlyph,
		PaneID: pane, Turn: 1, Severity: "critical", Mode: "queue", Project: testProject,
		Body: "## Must fix\n- JWT stored in localStorage (Security)\n## Should fix\n- No loading state on login (Frontend)",
		Parts: []queue.CardPart{
			{Dot: "security", DotName: "Security", Glyph: "●", Severity: "critical", Body: "JWT stored in localStorage is readable by any XSS payload."},
			{Dot: "frontend", DotName: "Frontend", Glyph: "●", Severity: "warn", Body: "The login form has no loading state."},
			{Dot: "copy", DotName: "Copy", Glyph: "●"},
			{Dot: "infra", DotName: "Infra", Glyph: "●", Body: "timed out", Failed: true},
		}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ledgerBy(r *memRig, kind string) map[string]memory.Outcome {
	led, _ := r.mem.Ledger(testProject)
	out := map[string]memory.Outcome{}
	for _, o := range led {
		if o.Kind == kind {
			out[o.Dot] = o
		}
	}
	return out
}

func memCalls(b *memBackend, name string) []backend.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []backend.Request
	for _, c := range b.calls {
		if c.DotName == name {
			out = append(out, c)
		}
	}
	return out
}

func TestSentCouncilCardRecordsOneOutcomePerContributingMember(t *testing.T) {
	r := newMemRig(t, memOpts{}, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	r.attach(t, "security", "frontend", "copy")
	c := councilCardFor(t, r)
	if err := r.e.SendCards([]string{c.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "two sent outcomes", func() bool { return len(ledgerBy(r, memory.OutcomeSent)) == 2 })
	sent := ledgerBy(r, memory.OutcomeSent)
	if sent["security"].Body != c.Parts[0].Body || sent["frontend"].Severity != "warn" || sent["security"].Card != c.ID {
		t.Fatalf("sent outcomes %+v", sent)
	}
	if led, _ := r.mem.Ledger(testProject); len(led) != 2 {
		t.Fatalf("silent and failed members record nothing: %+v", led)
	}
	eventually(t, "each member remembers its own finding", func() bool {
		s := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security", Kinds: []memory.Kind{memory.KindFinding}})
		f := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "frontend", Kinds: []memory.Kind{memory.KindFinding}})
		return len(s) == 1 && len(f) == 1 && strings.HasPrefix(s[0].Text, "JWT stored in localStorage")
	})
	// Requirement (d): council outcomes use Source outcome, so Phase 2's
	// anti-repeat filter treats them like any sent card.
	for _, dot := range []string{"security", "frontend"} {
		for _, e := range r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: dot, Kinds: []memory.Kind{memory.KindFinding}}) {
			if e.Source != memory.SourceOutcome {
				t.Fatalf("%s: council finding has source %q, want outcome", dot, e.Source)
			}
		}
	}
	if es := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "council"}); len(es) != 0 {
		t.Fatalf("the council itself remembers nothing: %+v", es)
	}
}

func TestDismissedAndEditedCouncilCards(t *testing.T) {
	r := newMemRig(t, memOpts{}, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	r.attach(t, "security", "frontend")
	c1 := councilCardFor(t, r)
	if err := r.e.Dismiss(c1.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "two dismissed outcomes", func() bool { return len(ledgerBy(r, memory.OutcomeDismissed)) == 2 })
	eventually(t, "dismissed entries per member", func() bool {
		s := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "security", Kinds: []memory.Kind{memory.KindDismissed}})
		f := r.entries(memory.Filter{Scope: memory.ScopeDot, Dot: "frontend", Kinds: []memory.Kind{memory.KindDismissed}})
		return len(s) == 1 && len(f) == 1
	})
	c2 := councilCardFor(t, r)
	const edited = "Move the session token to an httpOnly cookie. Add a spinner to the login form."
	if err := r.e.Edit(c2.ID, edited); err != nil {
		t.Fatal(err)
	}
	if err := r.e.SendCards([]string{c2.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "two edited outcomes", func() bool { return len(ledgerBy(r, memory.OutcomeEdited)) == 2 })
	for dot, o := range ledgerBy(r, memory.OutcomeEdited) {
		if o.Edited != edited || o.Body == "" || o.Body == edited {
			t.Fatalf("%s: edited outcome %+v", dot, o)
		}
	}
}

func TestChairReadsProjectConventionsOnly(t *testing.T) {
	r := newMemRig(t, memOpts{}, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	for _, e := range []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Auth tokens live in httpOnly cookies, never localStorage.", Source: memory.SourceUser},
		{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote, Text: "A private note only the security dot reads.", Source: memory.SourceUser},
	} {
		if _, err := r.mem.Add(testProject, e); err != nil {
			t.Fatal(err)
		}
	}
	r.be.set(backend.Result{Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: "Tokens belong in cookies."}})
	r.attach(t, "security", "frontend")
	r.reader("s1").add(transcript.Turn{User: "store the token"})
	if _, err := r.e.Council(context.Background(), pane, "", nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the chair ran", func() bool { return len(memCalls(r.be, council.ChairName)) == 1 })
	chair := memCalls(r.be, council.ChairName)[0]
	if !strings.Contains(chair.Memory, "Auth tokens live in httpOnly cookies") || strings.Contains(chair.Memory, "private note") ||
		chair.Learn || chair.RepoRead {
		t.Fatalf("chair request: learn=%v repo=%v memory:\n%s", chair.Learn, chair.RepoRead, chair.Memory)
	}
	if sec := memCalls(r.be, "Security"); len(sec) != 1 || !strings.Contains(sec[0].Memory, "private note") || sec[0].Force {
		t.Fatalf("members keep their usual memory block: %+v", sec)
	}
}

func TestCouncilCardRemembersOnlyAsAConvention(t *testing.T) {
	r := newMemRig(t, memOpts{}, member("Security", dots.ModeQueue))
	r.attach(t, "security")
	c := councilCardFor(t, r)
	for _, as := range []string{"finding", "rule", "global"} {
		raw, _ := json.Marshal(ipc.RememberArgs{CardID: c.ID, As: as})
		if err := r.e.Handle(context.Background(), ipc.CmdRemember, raw); err == nil || !strings.Contains(err.Error(), "convention") {
			t.Fatalf("remember a council card as %s: %v", as, err)
		}
	}
	raw, _ := json.Marshal(ipc.RememberArgs{CardID: c.ID, As: "convention"})
	if err := r.e.Handle(context.Background(), ipc.CmdRemember, raw); err != nil {
		t.Fatal(err)
	}
	if p := r.entries(memory.Filter{Scope: memory.ScopeProject}); len(p) != 1 || p[0].Kind != memory.KindConvention {
		t.Fatalf("project memory %+v", p)
	}
}

// Global constraint (reconciled): a turn a council delivery started never
// triggers a Phase 2 memory pass on its own, for any observe dot, because
// DecideMemory skips turns that IsFromDot reports as the dot's own. The
// second turn is the form Claude Code writes for long prompts (V3).
func TestCouncilTurnNeverTriggersAMemoryPass(t *testing.T) {
	d := member("Copy", dots.ModeObserve)
	d.Memory.EveryTurns = 1
	for name, text := range map[string]string{
		"plain":   transcript.DotPrefix(transcript.CouncilGlyph, transcript.CouncilName) + " ## Must fix\n- JWT in localStorage (Security)",
		"wrapped": claudeWrap(transcript.DotPrefix(transcript.CouncilGlyph, transcript.CouncilName) + " ## Must fix\n- JWT in localStorage (Security)"),
	} {
		t.Run(name, func(t *testing.T) {
			councilTurn := transcript.Turn{Index: 1, User: text, FromDots: transcript.FromDots(text)}
			in := MemoryInput{Dot: d, NewTurns: []transcript.Turn{councilTurn}, MemoryEnabled: true, PaneBudget: 100}
			if DecideMemory(in) {
				t.Fatal("a council-started turn alone must not start a memory pass")
			}
			in.NewTurns = append(in.NewTurns, transcript.Turn{Index: 2, User: "now add a logout button"})
			if !DecideMemory(in) {
				t.Fatal("a developer turn after it still counts")
			}
		})
	}
}

// Fix round: Remember stores the card's first point, not the "## " header.
func TestRememberCouncilCardStoresTheFirstPoint(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"chair digest": {"## Must fix\n- JWT stored in localStorage (Security, Frontend)\n## Should fix\n- No loading state (Frontend)", "JWT stored in localStorage"},
		"plain merge":  {"## Must fix\n- JWT in localStorage. (Security)\n\n## Should fix\n- No loading state. (Frontend)", "JWT in localStorage."},
	} {
		t.Run(name, func(t *testing.T) {
			r := newMemRig(t, memOpts{}, member("Security", dots.ModeQueue))
			r.attach(t, "security")
			c := councilCardFor(t, r)
			if err := r.q.Edit(c.ID, tc.body); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(ipc.RememberArgs{CardID: c.ID, As: "convention"})
			if err := r.e.Handle(context.Background(), ipc.CmdRemember, raw); err != nil {
				t.Fatal(err)
			}
			p := r.entries(memory.Filter{Scope: memory.ScopeProject})
			if len(p) != 1 || p[0].Text != tc.want {
				t.Fatalf("project memory %+v, want text %q", p, tc.want)
			}
		})
	}
}

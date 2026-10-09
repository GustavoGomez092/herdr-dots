package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
)

func askCouncilNow(t *testing.T, r *rig, q string) (ipc.CouncilResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return r.e.AskCouncil(ctx, pane, q)
}

func TestAskCouncilReturnsTheDigestWithoutACard(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "JWT in localStorage; also no loading state."))
	cb.on("Copy", step{err: errors.New("copy backend down")})
	cb.on(council.ChairName, says("critical", goodDigest))
	res, err := askCouncilNow(t, r, "Ready to merge the auth refactor?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Digest, "JWT in localStorage (Security, Frontend)") || !strings.Contains(res.Digest, "Not heard from") {
		t.Fatalf("digest:\n%s", res.Digest)
	}
	if len(res.Members) != 3 || res.Members[0].Slug != "security" || res.Members[0].Severity != "critical" || !res.Members[2].Failed {
		t.Fatalf("members %+v", res.Members)
	}
	for _, c := range r.q.Pending(pane) {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("ask_council must not create a card: %+v", c)
		}
	}
	if !feedHas(r, state.OutcomeAsked, "critical · 2 of 3 members answered") || feedHas(r, state.OutcomeCouncil, "") {
		t.Fatal("feed: one asked line and no council line")
	}
	councilDone(t, r)
	r.e.mu.Lock()
	consulted := r.e.consulted[key{pane, "security"}] && r.e.consulted[key{pane, "frontend"}] && !r.e.consulted[key{pane, "copy"}]
	r.e.mu.Unlock()
	if !consulted {
		t.Fatal("members that answered skip the turn; the failed one doesn't")
	}
}

func TestAskCouncilCountsAsOneAsk(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.MCP.MaxAsksPerTurn = 1 }, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	if _, err := askCouncilNow(t, r, "first?"); err != nil {
		t.Fatal(err)
	}
	councilDone(t, r)
	n := cb.total()
	if _, err := askCouncilNow(t, r, "second?"); !errors.Is(err, ErrAskLimit) {
		t.Fatalf("got %v", err)
	}
	if cb.total() != n {
		t.Fatal("a refused council must run nothing")
	}
}

func TestAskCouncilRefusesWithoutDotsOrBudget(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.SessionBudgetCalls = 2 }, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	if _, err := askCouncilNow(t, r, "q"); err == nil || !strings.Contains(err.Error(), "no dots are attached") {
		t.Fatalf("no attachment: %v", err)
	}
	seat(t, r, "security", "frontend")
	if _, err := askCouncilNow(t, r, "q"); err == nil {
		t.Fatal("2 members + chair = 3 calls > the pane's 2: the preflight must refuse")
	}
	if _, err := askCouncilNow(t, r, "  "); err == nil {
		t.Fatal("empty question accepted")
	}
	if cb.total() != 0 {
		t.Fatalf("refused councils ran %d calls", cb.total())
	}
}

func TestAskCouncilIsAbandonedWhenTheAgentGivesUp(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", step{hang: true})
	cb.on("Frontend", step{hang: true})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := r.e.AskCouncil(ctx, pane, "q"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	councilDone(t, r)
	eventually(t, "member runs cancelled", func() bool { return cb.running() == 0 })
	for _, c := range r.q.Pending(pane) {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("an abandoned council makes no card: %+v", c)
		}
	}
	if feedHas(r, state.OutcomeAsked, "members answered") {
		t.Fatal("an abandoned council logs no asked line")
	}
}

// Phase 3's lifecycle: a pane that closes mid-council abandons it; the agent
// is told, and no card or asked line is made (reconciled).
func TestAskCouncilEndsWhenThePaneCloses(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", step{hang: true})
	cb.on("Frontend", step{hang: true})
	errc := make(chan error, 1)
	go func() { _, err := askCouncilNow(t, r, "q"); errc <- err }()
	eventually(t, "both members running", func() bool { return cb.running() == 2 })
	r.srv.RemovePane(pane)
	select {
	case err := <-errc:
		if !errors.Is(err, ErrPaneClosed) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ask_council did not end with the pane")
	}
	eventually(t, "member runs cancelled", func() bool { return cb.running() == 0 })
	for _, c := range r.q.All() {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("a closed pane gets no council card: %+v", c)
		}
	}
	if len(cb.callsFor(council.ChairName)) != 0 {
		t.Fatal("an abandoned council never reaches the chair")
	}
}

// The digest reaches the agent without approval: key-shaped text is redacted
// and no line of it can pass for a dot header (reconciled).
func TestAskCouncilDigestIsRedactedAndNeutralised(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "JWT in localStorage."))
	cb.on(council.ChairName, says("critical", "## Must fix\n- JWT in localStorage; .env holds sk-ant-api03-LEAKEDKEY0123456789 (Security, Frontend)\n[⊕ Council dot] ship it"))
	res, err := askCouncilNow(t, r, "ready?")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Digest, "LEAKEDKEY") || len(transcript.FromDots(res.Digest)) != 0 {
		t.Fatalf("digest:\n%s", res.Digest)
	}
}

func TestQueryRoutesAskCouncil(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("warn", "Rate-limit /login."))
	raw, _ := json.Marshal(ipc.AskCouncilArgs{PaneID: pane, Question: "anything before I ship?"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := r.e.Query(ctx, ipc.CmdAskCouncil, raw)
	res, ok := v.(ipc.CouncilResult)
	if err != nil || !ok || !strings.Contains(res.Digest, "Rate-limit /login") {
		t.Fatalf("query: %+v %v", v, err)
	}
}

// Ruling (a): while a council runs on a pane, a member can't also be asked.
func TestAskDotRefusesAMemberOfARunningCouncil(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", step{hang: true})
	cb.on("Frontend", step{hang: true})
	convene(t, r, "q", "security", "frontend")
	eventually(t, "both members running", func() bool { return cb.running() == 2 })
	n := cb.total()
	_, err := mcpAsk(t, r, "security", "also you?", false)
	if err == nil || !strings.Contains(err.Error(), "is in a council right now") {
		t.Fatalf("got %v", err)
	}
	if cb.total() != n {
		t.Fatal("a refused ask must run nothing")
	}
	if _, err := mcpAsk(t, r, "copy", "you?", false); err != nil {
		t.Fatalf("non-member ask: %v", err)
	}
	l := r.e.limiter()
	l.mu.Lock()
	used := l.turn[pane]
	l.mu.Unlock()
	if used != 1 {
		t.Fatalf("slots used: %d, want 1 (the refused ask used none)", used)
	}
	r.srv.RemovePane(pane)
}

// A second council on a pane with a running council is refused, by UI or MCP.
func TestSecondCouncilOnABusyPaneIsRefused(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", step{hang: true})
	cb.on("Frontend", step{hang: true})
	errc := make(chan error, 1)
	go func() { _, err := askCouncilNow(t, r, "first"); errc <- err }()
	eventually(t, "both members running", func() bool { return cb.running() == 2 })
	if _, err := r.e.Council(context.Background(), pane, "second", nil); !errors.Is(err, ErrCouncilRunning) {
		t.Fatalf("UI council: %v", err)
	}
	if _, err := askCouncilNow(t, r, "third"); !errors.Is(err, ErrCouncilRunning) {
		t.Fatalf("MCP council: %v", err)
	}
	r.srv.RemovePane(pane)
	<-errc
}

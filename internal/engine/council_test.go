package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/council"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// step is one scripted answer of councilBackend.
type step struct {
	v    backend.Verdict
	err  error
	hang bool          // block until the run's context ends
	wait chan struct{} // block until closed (or the context ends)
}

func says(sev, msg string) step {
	return step{v: backend.Verdict{Relevant: true, Severity: sev, Message: msg}}
}

var quiet = step{v: backend.Verdict{Severity: "info"}} // "nothing from my side"

// councilBackend answers by Request.DotName, so every member and the chair
// can be scripted on their own. A name's last step repeats; unscripted names
// pass.
type councilBackend struct {
	mu       sync.Mutex
	script   map[string][]step
	calls    []backend.Request
	inflight int
}

func (b *councilBackend) Kind() string                { return "claude-cli" }
func (b *councilBackend) Check(context.Context) error { return nil }
func (b *councilBackend) on(name string, steps ...step) {
	b.mu.Lock()
	b.script[name] = steps
	b.mu.Unlock()
}

func (b *councilBackend) Run(ctx context.Context, r backend.Request) (backend.Result, error) {
	b.mu.Lock()
	b.calls = append(b.calls, r)
	st := quiet
	if s := b.script[r.DotName]; len(s) > 0 {
		st = s[0]
		if len(s) > 1 {
			b.script[r.DotName] = s[1:]
		}
	}
	b.inflight++
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.inflight--; b.mu.Unlock() }()
	switch {
	case st.hang:
		<-ctx.Done()
		return backend.Result{}, ctx.Err()
	case st.wait != nil:
		select {
		case <-st.wait:
		case <-ctx.Done():
			return backend.Result{}, ctx.Err()
		}
	}
	if st.err != nil {
		return backend.Result{}, st.err
	}
	return backend.Result{Verdict: st.v, InputTokens: 10, OutputTokens: 5}, nil
}

func (b *councilBackend) callsFor(name string) []backend.Request {
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

func (b *councilBackend) total() int   { b.mu.Lock(); defer b.mu.Unlock(); return len(b.calls) }
func (b *councilBackend) running() int { b.mu.Lock(); defer b.mu.Unlock(); return b.inflight }

// member is a dot that never runs on turns by itself, so only the council
// calls the backend (no catch-up races in these tests).
func member(name string, mode dots.Mode) dots.Dot {
	d := dot(name, mode)
	d.Trigger.OnTurn = false
	return d
}

func councilRig(t *testing.T, cfg func(*config.Config), ds ...dots.Dot) (*rig, *councilBackend) {
	t.Helper()
	cb := &councilBackend{script: map[string][]step{}}
	r := newRigWith(t, func(d *Deps) {
		d.Backend = func(string) (backend.Backend, error) { return cb, nil }
		if cfg != nil {
			cfg(&d.Config)
		}
	}, ds...)
	return r, cb
}

// seat attaches the dots and adds one finished turn for the council to read.
func seat(t *testing.T, r *rig, slugs ...string) {
	t.Helper()
	r.attach(t, slugs...)
	r.readers["s1"].add(transcript.Turn{User: "refactor auth to JWT"})
}

func convene(t *testing.T, r *rig, q string, slugs ...string) {
	t.Helper()
	if _, err := r.e.Council(context.Background(), pane, q, slugs); err != nil {
		t.Fatal(err)
	}
}

func councilCard(t *testing.T, r *rig) queue.Card {
	t.Helper()
	var card queue.Card
	eventually(t, "a council card", func() bool {
		for _, c := range r.q.Pending(pane) {
			if c.Kind == queue.KindCouncil {
				card = c
				return true
			}
		}
		return false
	})
	return card
}

func councilDone(t *testing.T, r *rig) {
	t.Helper()
	eventually(t, "council finished", func() bool { p, _ := r.e.Snapshot().Pane(pane); return p.Council == nil })
}

func feedHas(r *rig, outcome, detail string) bool {
	p, _ := r.e.Snapshot().Pane(pane)
	for _, e := range p.Feed {
		if e.Outcome == outcome && strings.Contains(e.Detail, detail) {
			return true
		}
	}
	return false
}

const goodDigest = "## Must fix\n- JWT in localStorage (Security, Frontend)\n## Consider\n- Shorter button copy (Copy)"

func TestCouncilMergesMembersThroughTheChair(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeSuggest), member("Copy", dots.ModeObserve))
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "JWT in localStorage; also no loading state."))
	cb.on("Copy", says("info", "Shorter button copy."))
	cb.on(council.ChairName, says("critical", goodDigest))
	convene(t, r, "Ready to merge the auth refactor?")
	c := councilCard(t, r)
	councilDone(t, r)
	if c.Dot != "council" || c.DotName != transcript.CouncilName || c.Glyph != transcript.CouncilGlyph || c.Mode != "queue" ||
		c.Status != queue.StatusPending || c.Severity != "critical" || c.Body != goodDigest || c.Note != "" ||
		c.Question != "Ready to merge the auth refactor?" || c.Turn != 1 {
		t.Fatalf("card %+v", c)
	}
	if len(c.Parts) != 3 || c.Parts[0].Dot != "security" || c.Parts[1].Dot != "frontend" || c.Parts[2].DotName != "Copy" ||
		c.Parts[0].Severity != "critical" || c.Parts[2].Body != "Shorter button copy." {
		t.Fatalf("parts %+v", c.Parts)
	}
	if n := len(r.q.Pending(pane)); n != 1 {
		t.Fatalf("members (even suggest and observe ones) make no cards of their own: %d pending", n)
	}
	for _, name := range []string{"Security", "Frontend", "Copy"} {
		reqs := cb.callsFor(name)
		if len(reqs) != 1 || reqs[0].Force || reqs[0].Question != "Ready to merge the auth refactor?" || len(reqs[0].Turns) != 1 {
			t.Fatalf("%s requests %+v", name, reqs)
		}
	}
	ch := cb.callsFor(council.ChairName)
	if len(ch) != 1 || !ch[0].Force || ch[0].RepoRead || ch[0].Purpose != council.ChairPurpose || ch[0].Model != "sonnet" || len(ch[0].Args) != 0 {
		t.Fatalf("chair requests %+v", ch)
	}
	for _, want := range []string{"Ready to merge", "JWT in localStorage.", "no loading state", "Shorter button copy."} {
		if !strings.Contains(ch[0].Question, want) {
			t.Errorf("chair question lacks %q:\n%s", want, ch[0].Question)
		}
	}
	if r.usage.PaneCalls(pane) != 4 || r.usage.Calls(pane, "security") != 1 || r.usage.Calls(pane, "council") != 1 {
		t.Fatalf("usage: pane %d, security %d, chair %d", r.usage.PaneCalls(pane), r.usage.Calls(pane, "security"), r.usage.Calls(pane, "council"))
	}
	if !feedHas(r, state.OutcomeCouncil, "3 of 3 members answered") {
		t.Fatal("feed has no council entry")
	}
}

func TestCouncilMembersShareTheParallelismCap(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.MaxParallelRuns = 1 },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	gate := make(chan struct{})
	for _, n := range []string{"Security", "Frontend", "Copy"} {
		cb.on(n, step{v: backend.Verdict{Relevant: true, Severity: "warn", Message: n + " point"}, wait: gate})
	}
	convene(t, r, "")
	eventually(t, "one member running", func() bool { return cb.running() == 1 })
	time.Sleep(40 * time.Millisecond)
	if cb.running() != 1 || cb.total() != 1 {
		t.Fatalf("max_parallel_runs = 1, but %d running and %d started", cb.running(), cb.total())
	}
	close(gate)
	if c := councilCard(t, r); len(c.Parts) != 3 {
		t.Fatalf("parts %+v", c.Parts)
	}
}

// Review Focus: a member hangs until the deadline while the others finish.
func TestCouncilMemberTimeoutBecomesAFailedPart(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	r.e.councilWait = 150 * time.Millisecond
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on("Copy", step{hang: true})
	cb.on(council.ChairName, says("critical", "## Must fix\n- JWT in localStorage (Security)\n## Should fix\n- No loading state (Frontend)"))
	convene(t, r, "")
	c := councilCard(t, r)
	if !c.Parts[2].Failed || c.Parts[2].Body != "timed out" || c.Parts[2].Severity != "" {
		t.Fatalf("copy part %+v", c.Parts[2])
	}
	if !strings.HasSuffix(c.Body, "## Not heard from\n- Copy (timed out)") {
		t.Fatalf("the digest must list who timed out:\n%s", c.Body)
	}
	eventually(t, "the hung run is cancelled", func() bool { return cb.running() == 0 })
	eventually(t, "the timeout counts toward the failure streak", func() bool {
		a, _ := r.atts.Get(pane)
		return a.Find("copy").Failures == 1
	})
	councilDone(t, r)
}

func TestCouncilWithNoAnswersMakesNoCard(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", step{err: errors.New("claude: not logged in")})
	cb.on("Frontend", step{err: errors.New("claude: not logged in")})
	convene(t, r, "")
	councilDone(t, r)
	eventually(t, "a failure toast", func() bool {
		for _, c := range r.srv.Calls("notification.show") {
			if strings.Contains(fmt.Sprint(c.Params["body"]), "council failed: no member answered") {
				return true
			}
		}
		return false
	})
	if !feedHas(r, state.OutcomeError, "council failed: no member answered") {
		t.Fatal("feed has no council error")
	}
	for _, c := range r.q.All() {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("no card when nobody answered: %+v", c)
		}
	}
	if len(cb.callsFor(council.ChairName)) != 0 {
		t.Fatal("nothing to merge: no chair call")
	}
}

// Review Focus: the chair returns prose instead of the JSON verdict.
func TestCouncilFallsBackToPlainMergeWhenTheChairFails(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on(council.ChairName, step{err: fmt.Errorf("%w: no JSON object in %q", backend.ErrMalformed, "Looks fine to me")})
	convene(t, r, "")
	c := councilCard(t, r)
	if want := "## Must fix\n- JWT in localStorage. (Security)\n\n## Should fix\n- No loading state. (Frontend)"; c.Body != want {
		t.Fatalf("plain merge:\n%s", c.Body)
	}
	if !strings.HasPrefix(c.Note, "merged without chair (chair error: malformed verdict") {
		t.Fatalf("note %q", c.Note)
	}
}

// Requirement (a): a chair that answers "not relevant" or with an empty
// message is no digest; it must not reach the card as an empty body.
func TestEmptyChairDigestFallsBackToPlainMerge(t *testing.T) {
	for name, st := range map[string]step{
		"pass":  {v: backend.Verdict{Severity: "info"}},
		"empty": {v: backend.Verdict{Relevant: true, Severity: "warn", Message: "  \n"}},
	} {
		t.Run(name, func(t *testing.T) {
			r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
			seat(t, r, "security", "frontend")
			cb.on("Security", says("critical", "JWT in localStorage."))
			cb.on("Frontend", says("warn", "No loading state."))
			cb.on(council.ChairName, st)
			convene(t, r, "")
			c := councilCard(t, r)
			if !strings.HasPrefix(c.Body, "## Must fix\n- JWT in localStorage. (Security)") || !strings.HasPrefix(c.Note, "merged without chair (chair error:") {
				t.Fatalf("body %q note %q", c.Body, c.Note)
			}
		})
	}
}

// Review Focus: the chair drops attribution.
func TestUnattributedChairDigestIsMarkedUnverified(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on(council.ChairName, says("critical", "## Must fix\n- JWT in localStorage\n- No loading state"))
	convene(t, r, "")
	c := councilCard(t, r)
	if c.Body != "## Must fix\n- JWT in localStorage\n- No loading state" || !strings.HasPrefix(c.Note, "unverified digest") {
		t.Fatalf("body %q note %q", c.Body, c.Note)
	}
}

func TestCouncilWithNothingToAdd(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	convene(t, r, "anything?")
	c := councilCard(t, r)
	if c.Body != council.NothingToAdd || c.Severity != "info" || len(c.Parts) != 2 || c.Parts[0].Body != "" || c.Parts[0].Failed {
		t.Fatalf("card %+v", c)
	}
	if len(cb.callsFor(council.ChairName)) != 0 {
		t.Fatal("nothing to merge: no chair call")
	}
}

func TestSingleAnswerSkipsTheChair(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	convene(t, r, "")
	c := councilCard(t, r)
	if c.Body != "## Must fix\n- JWT in localStorage. (Security)" || c.Severity != "critical" || c.Note != "" {
		t.Fatalf("card %+v", c)
	}
	if len(cb.callsFor(council.ChairName)) != 0 {
		t.Fatal("one answer: no chair call")
	}
}

func TestMergePlainNeverCallsTheChair(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.Council.Merge = config.MergePlain },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	pf, _ := r.e.CouncilPreflight(pane, nil)
	if pf.EstimatedCalls != 2 {
		t.Fatalf("plain merge costs no chair call: %+v", pf)
	}
	convene(t, r, "")
	if c := councilCard(t, r); !strings.HasPrefix(c.Body, "## Must fix\n- JWT in localStorage. (Security)") || c.Note != "" {
		t.Fatalf("card %+v", c)
	}
	if len(cb.callsFor(council.ChairName)) != 0 {
		t.Fatal("merge = plain: no chair call")
	}
}

func TestRoundTwoCrossReview(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.Council.Rounds = 2 },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", says("critical", "JWT in localStorage."), says("warn", "OBJECT: cookies need CSRF protection first."))
	cb.on("Frontend", says("warn", "No loading state."), says("info", "AGREE: the token point stands."))
	cb.on("Copy", quiet, quiet)
	cb.on(council.ChairName, says("critical", goodDigest))
	convene(t, r, "Ready to merge?")
	c := councilCard(t, r)
	for _, name := range []string{"Security", "Frontend", "Copy"} {
		if n := len(cb.callsFor(name)); n != 2 {
			t.Fatalf("%s called %d times, want 2", name, n)
		}
	}
	r2 := cb.callsFor("Security")[1]
	if r2.Force || !strings.HasPrefix(r2.Question, "Ready to merge?\n\n"+council.OthersHeader) ||
		!strings.Contains(r2.Question, "No loading state.") || strings.Contains(r2.Question, "JWT in localStorage.") ||
		!strings.Contains(r2.Question, "AGREE:") {
		t.Fatalf("round-2 question:\n%s", r2.Question)
	}
	if c.Parts[0].Stance != council.StanceObject || !strings.HasSuffix(c.Parts[0].Body, "↳ OBJECT: cookies need CSRF protection first.") ||
		c.Parts[1].Stance != council.StanceAgree || c.Parts[2].Stance != "" {
		t.Fatalf("parts %+v", c.Parts)
	}
	if q := cb.callsFor(council.ChairName)[0].Question; !strings.Contains(q, "round 2, OBJECT (warn)") {
		t.Fatalf("the chair sees round 2:\n%s", q)
	}
	if r.usage.PaneCalls(pane) != 7 {
		t.Fatalf("3 members × 2 rounds + chair = 7, got %d", r.usage.PaneCalls(pane))
	}
}

func TestSecondCouncilIsRejectedWhileOneRuns(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue))
	seat(t, r, "security")
	cb.on("Security", step{hang: true})
	convene(t, r, "first")
	eventually(t, "council running", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return p.Council != nil && p.Council.Phase == view.CouncilMembers && p.Council.Total == 1
	})
	if _, err := r.e.Council(context.Background(), pane, "second", nil); !errors.Is(err, ErrCouncilRunning) {
		t.Fatalf("want ErrCouncilRunning, got %v", err)
	}
	raw, _ := json.Marshal(ipc.CouncilArgs{PaneID: pane})
	if err := r.e.Handle(context.Background(), ipc.CmdCouncil, raw); err == nil || err.Error() != "council already running" {
		t.Fatalf("over IPC: %v", err)
	}
}

// Review Focus: a tight budget refuses before any call is spent.
func TestCouncilPreflightRefusesWhenThePaneBudgetIsShort(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.SessionBudgetCalls = 2 },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Backend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "backend", "copy")
	pf, err := r.e.CouncilPreflight(pane, nil)
	if err != nil {
		t.Fatal(err)
	}
	const why = "pane budget too low: this council needs 5 calls, 2 left"
	if pf.Allowed || pf.Refusal != why || len(pf.Members) != 4 {
		t.Fatalf("preflight %+v", pf)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.CouncilPreflight == nil || p.CouncilPreflight.Refusal != why {
		t.Fatalf("the snapshot carries the preflight: %+v", p.CouncilPreflight)
	}
	if _, err := r.e.Council(context.Background(), pane, "", nil); err == nil || err.Error() != why {
		t.Fatalf("council: %v", err)
	}
	if cb.total() != 0 {
		t.Fatalf("a refused council spends nothing, got %d calls", cb.total())
	}
}

// Final review item 6: every preflight carries a growing sequence number and
// the popup's request id, so a reopened popup can tell its own (or a newer)
// preflight from an old one.
func TestCouncilPreflightIsStampedWithSeqAndRequest(t *testing.T) {
	r, _ := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	first, _ := r.e.CouncilPreflight(pane, nil)
	raw, _ := json.Marshal(ipc.CouncilArgs{PaneID: pane, Request: "popup-2"})
	if err := r.e.Handle(context.Background(), ipc.CmdCouncilPreflight, raw); err != nil {
		t.Fatal(err)
	}
	p, _ := r.e.Snapshot().Pane(pane)
	if first.Seq == 0 || p.CouncilPreflight == nil || p.CouncilPreflight.Seq <= first.Seq || p.CouncilPreflight.Request != "popup-2" {
		t.Fatalf("first %+v, published %+v", first, p.CouncilPreflight)
	}
	if b, _ := json.Marshal(council.Preflight{}); strings.Contains(string(b), "seq") || strings.Contains(string(b), "request") {
		t.Fatalf("unstamped preflights keep the old JSON: %s", b)
	}
}

func TestPreflightSkipsPausedAndOverBudgetMembers(t *testing.T) {
	fe := member("Frontend", dots.ModeQueue)
	fe.Budget.MaxCallsPerSession = 1
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), fe, member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	_ = r.usage.Record(pane, "frontend", 1, 1)
	if err := r.e.SetPaused(pane, "copy", true); err != nil {
		t.Fatal(err)
	}
	pf, _ := r.e.CouncilPreflight(pane, nil)
	if strings.Join(pf.Members, ",") != "security" || strings.Join(pf.Skipped, ",") != "frontend,copy" ||
		pf.Reasons["frontend"] != council.ReasonBudget || pf.Reasons["copy"] != council.ReasonPaused || !pf.Allowed {
		t.Fatalf("preflight %+v", pf)
	}
	cb.on("Security", says("warn", "No CSRF token."))
	convene(t, r, "")
	c := councilCard(t, r)
	if len(c.Parts) != 3 || !c.Parts[1].Failed || c.Parts[1].Body != "skipped: budget" || c.Parts[2].Body != "skipped: paused" {
		t.Fatalf("parts %+v", c.Parts)
	}
	if !strings.HasSuffix(c.Body, "## Not heard from\n- Frontend (skipped: budget)\n- Copy (skipped: paused)") {
		t.Fatalf("body:\n%s", c.Body)
	}
	if len(cb.callsFor("Frontend"))+len(cb.callsFor("Copy")) != 0 {
		t.Fatal("skipped members are never called")
	}
}

// claudeWrap is how Claude Code stores a long pasted prompt in its transcript
// (seen live in V3): the dot prefix sits on its own line inside a wrapper.
func claudeWrap(text string) string {
	return "\n\n<pasted_content id=\"1\">\n" + text + "\n</pasted_content id=\"1\">\n"
}

// Review Focus: a council delivery must not trigger every dot again. The
// second case is the transcript form Claude Code writes for long prompts.
func TestCouncilDeliveryIsSkippedByEveryDot(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapped=%v", wrapped), func(t *testing.T) {
			r, cb := councilRig(t, nil, dot("Security", dots.ModeQueue), dot("Frontend", dots.ModeQueue))
			r.attach(t, "security", "frontend")
			r.turnEnds(t, transcript.Turn{User: "add login"})
			eventually(t, "both dots reviewed turn 1", func() bool {
				p, _ := r.e.Snapshot().Pane(pane)
				return cb.total() == 2 && lastSeen(r, "security") == 1 && lastSeen(r, "frontend") == 1 &&
					p.Dots[0].Status == view.StatusPassed && p.Dots[1].Status == view.StatusPassed
			})
			cb.on("Security", says("critical", "JWT in localStorage."))
			cb.on("Frontend", says("warn", "No loading state."))
			cb.on(council.ChairName, says("critical", goodDigest))
			convene(t, r, "")
			c := councilCard(t, r)
			councilDone(t, r)
			if err := r.e.SendCards([]string{c.ID}); err != nil {
				t.Fatal(err)
			}
			eventually(t, "council delivered", func() bool { return len(r.srv.Calls("agent.prompt")) == 1 })
			text := r.srv.Calls("agent.prompt")[0].Params["text"].(string)
			if text != "[⊕ Council dot] "+goodDigest {
				t.Fatalf("delivered %q", text)
			}
			seen := text
			if wrapped {
				seen = claudeWrap(text)
			}
			from := transcript.FromDots(seen)
			if len(from) != 1 || from[0] != transcript.CouncilName {
				t.Fatalf("FromDots(%q) = %v", seen, from)
			}
			before := cb.total()
			r.turnEnds(t, transcript.Turn{User: seen, FromDots: from})
			eventually(t, "the council turn is consumed", func() bool { return lastSeen(r, "security") == 2 && lastSeen(r, "frontend") == 2 })
			time.Sleep(60 * time.Millisecond)
			if cb.total() != before {
				t.Fatalf("no dot may review the turn a council started: %d new calls", cb.total()-before)
			}
		})
	}
}

// Review Focus: the pane closes mid-council.
func TestPaneClosedMidCouncilAbandonsIt(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", step{hang: true})
	cb.on("Frontend", step{hang: true})
	convene(t, r, "")
	eventually(t, "both members running", func() bool { return cb.running() == 2 })
	r.srv.RemovePane(pane)
	eventually(t, "attachment dropped", func() bool { _, ok := r.atts.Get(pane); return !ok })
	eventually(t, "in-flight member runs cancelled", func() bool { return cb.running() == 0 })
	eventually(t, "council forgotten", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.e.councils) == 0
	})
	time.Sleep(50 * time.Millisecond)
	for _, c := range r.q.All() {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("a closed pane gets no council card: %+v", c)
		}
	}
	if len(cb.callsFor(council.ChairName)) != 0 {
		t.Fatal("an abandoned council never reaches the chair")
	}
}

func TestHandleRoutesCouncilCommands(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	r.e.councilWait = 200 * time.Millisecond
	seat(t, r, "security", "frontend")
	cb.on("Security", step{hang: true})
	ctx := context.Background()
	raw, _ := json.Marshal(ipc.CouncilArgs{PaneID: pane})
	if err := r.e.Handle(ctx, ipc.CmdCouncilPreflight, raw); err != nil {
		t.Fatal(err)
	}
	if p, _ := r.e.Snapshot().Pane(pane); p.CouncilPreflight == nil || strings.Join(p.CouncilPreflight.Members, ",") != "security,frontend" {
		t.Fatalf("preflight in snapshot: %+v", p.CouncilPreflight)
	}
	raw, _ = json.Marshal(ipc.CouncilArgs{PaneID: pane, Question: "ship it?", Slugs: []string{"security"}})
	if err := r.e.Handle(ctx, ipc.CmdCouncil, raw); err != nil {
		t.Fatal(err)
	}
	p, _ := r.e.Snapshot().Pane(pane)
	if p.Council == nil || p.Council.Phase != view.CouncilMembers || p.Council.Total != 1 || p.CouncilPreflight != nil {
		t.Fatalf("snapshot while running: %+v / %+v", p.Council, p.CouncilPreflight)
	}
	councilDone(t, r)
	if len(cb.callsFor("Frontend")) != 0 {
		t.Fatal("only the selected member runs")
	}
	if err := r.e.Handle(ctx, ipc.CmdCouncil, nil); err == nil {
		t.Fatal("missing args must error")
	}
	raw, _ = json.Marshal(ipc.CouncilArgs{PaneID: "w9:p9"})
	if err := r.e.Handle(ctx, ipc.CmdCouncilPreflight, raw); !errors.Is(err, state.ErrNoAttachment) && (err == nil || !strings.Contains(err.Error(), "no dots attached")) {
		t.Fatalf("unknown pane: %v", err)
	}
}

// Requirement (c): nil slugs mean "all eligible"; an explicitly empty
// selection is refused cleanly instead of crashing or silently meaning "all".
func TestCouncilSlugSelectionNilVersusEmpty(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	pf, err := r.e.CouncilPreflight(pane, nil)
	if err != nil || len(pf.Members) != 2 || !pf.Allowed {
		t.Fatalf("nil selects every eligible dot: %+v %v", pf, err)
	}
	pf, err = r.e.CouncilPreflight(pane, []string{})
	if err != nil || len(pf.Members) != 0 || pf.Allowed || pf.Refusal == "" {
		t.Fatalf("an empty selection is refused: %+v %v", pf, err)
	}
	if _, err := r.e.Council(context.Background(), pane, "", []string{}); err == nil {
		t.Fatal("an empty selection must not convene a council")
	}
	if cb.total() != 0 {
		t.Fatalf("nothing may run: %d calls", cb.total())
	}
}

// Requirement (e): everything the engine builds into the card is sanitized,
// because the pane renders it raw.
func TestCouncilCardTextIsSanitized(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT\x1b[31m in localStorage.\x07"))
	cb.on("Frontend", step{err: errors.New("boom\x1b]0;pwn\x07 failed")})
	convene(t, r, "ready\x1b[2J?")
	c := councilCard(t, r)
	all := []string{c.Question, c.Body, c.Note}
	for _, p := range c.Parts {
		all = append(all, p.DotName, p.Body)
	}
	for _, s := range all {
		if strings.ContainsAny(s, "\x1b\x07") {
			t.Fatalf("raw control character in %q (card %+v)", s, c)
		}
	}
	if c.Question != "ready[2J?" || c.Parts[0].Body != "JWT[31m in localStorage." {
		t.Fatalf("card %+v", c)
	}
}

// A chair digest is model text too.
func TestChairDigestIsSanitized(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on(council.ChairName, says("critical", "## Must fix\n- JWT\x1b[31m in localStorage (Security, Frontend)"))
	convene(t, r, "")
	c := councilCard(t, r)
	if strings.ContainsAny(c.Body+c.Note, "\x1b\x07") {
		t.Fatalf("raw control character in %q / %q", c.Body, c.Note)
	}
}

// councilRigCancel is councilRig with the engine context's cancel function,
// for tests that shut the engine down mid-council.
func councilRigCancel(t *testing.T, ds ...dots.Dot) (*rig, *councilBackend, context.CancelFunc) {
	t.Helper()
	cb := &councilBackend{script: map[string][]step{}}
	srv := herdrtest.New(t)
	srv.SetPane(herdr.PaneInfo{PaneID: pane, Agent: "claude", AgentStatus: herdr.StatusIdle, Cwd: "/repo",
		AgentSession: &herdr.AgentSession{Kind: "id", Value: "s1"}})
	dir := t.TempDir()
	q, _ := queue.Open(filepath.Join(dir, "queue.json"))
	atts, _ := state.OpenAttachments(filepath.Join(dir, "attachments.json"))
	usage, _ := state.OpenUsage(filepath.Join(dir, "usage.json"))
	feed, _ := state.OpenFeed(filepath.Join(dir, "feed.jsonl"))
	r := &rig{srv: srv, readers: map[string]*fakeReader{"s1": {}}, q: q, atts: atts, usage: usage}
	cfg := config.Default()
	cfg.TurnDebounceMs = 0
	r.e = New(Deps{
		Herdr:    &herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"},
		Backend:  func(string) (backend.Backend, error) { return cb, nil },
		Reader:   func(state.Attachment) transcript.Reader { return r.readers["s1"] },
		LoadDots: func() ([]dots.Dot, []dots.LoadError) { return ds, nil },
		SaveDot:  func(dots.Dot) error { return nil },
		Config:   cfg, Queue: q, Attachments: atts, Usage: usage, Feed: feed,
		PollInterval: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(r.e.Wait)
	t.Cleanup(cancel)
	r.e.Start(ctx)
	return r, cb, cancel
}

// Fix round: shutting the engine down mid-council loses the council (spec):
// no card, no chair call, no charge.
func TestEngineShutdownMidCouncilLeavesNoCard(t *testing.T) {
	r, cb, cancel := councilRigCancel(t, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue), member("Copy", dots.ModeQueue))
	seat(t, r, "security", "frontend", "copy")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Copy", says("info", "Shorter button copy."))
	cb.on("Frontend", step{hang: true})
	cb.on(council.ChairName, says("critical", goodDigest))
	convene(t, r, "")
	eventually(t, "two members answered, one hangs", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return cb.total() == 3 && cb.running() == 1 && p.Council != nil && p.Council.Done == 2
	})
	cancel()
	r.e.Wait()
	for _, c := range r.q.All() {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("a shut-down engine leaves no council card: %+v", c)
		}
	}
	if len(cb.callsFor(council.ChairName)) != 0 || r.usage.Calls(pane, "council") != 0 {
		t.Fatal("no chair call and no charge after shutdown")
	}
}

func feedErrors(r *rig, slug string) []state.FeedEntry {
	var out []state.FeedEntry
	for _, f := range r.e.d.Feed.Recent(pane, 100) {
		if f.Dot == slug && f.Outcome == state.OutcomeError {
			out = append(out, f)
		}
	}
	return out
}

// Final review item 7: a member run cancelled because its pane closed or the
// engine stopped writes no feed error and bumps no failure streak; a real
// timeout still does.
func TestCancelledMemberRunsAreNotFailures(t *testing.T) {
	t.Run("pane closed", func(t *testing.T) {
		r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
		seat(t, r, "security", "frontend")
		cb.on("Security", step{hang: true})
		cb.on("Frontend", step{hang: true})
		convene(t, r, "")
		eventually(t, "both members running", func() bool { return cb.running() == 2 })
		r.srv.RemovePane(pane)
		eventually(t, "in-flight member runs cancelled", func() bool { return cb.running() == 0 })
		time.Sleep(50 * time.Millisecond)
		for _, slug := range []string{"security", "frontend"} {
			if errs := feedErrors(r, slug); len(errs) != 0 {
				t.Fatalf("%s: feed errors for a closed pane: %+v", slug, errs)
			}
		}
	})
	t.Run("engine stopped", func(t *testing.T) {
		r, cb, cancel := councilRigCancel(t, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
		seat(t, r, "security", "frontend")
		cb.on("Security", says("critical", "JWT in localStorage."))
		cb.on("Frontend", step{hang: true})
		convene(t, r, "")
		eventually(t, "one member hangs", func() bool { return cb.total() == 2 && cb.running() == 1 })
		cancel()
		r.e.Wait()
		if errs := feedErrors(r, "frontend"); len(errs) != 0 {
			t.Fatalf("feed errors on shutdown: %+v", errs)
		}
		a, _ := r.atts.Get(pane)
		if da := a.Find("frontend"); da == nil || da.Failures != 0 {
			t.Fatalf("shutdown bumps no failure streak: %+v", da)
		}
	})
	t.Run("real timeout", func(t *testing.T) {
		r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
		r.e.councilWait = 100 * time.Millisecond
		seat(t, r, "security", "frontend")
		cb.on("Security", says("critical", "JWT in localStorage."))
		cb.on("Frontend", step{hang: true})
		convene(t, r, "")
		eventually(t, "the timeout is a feed error", func() bool { return len(feedErrors(r, "frontend")) == 1 })
	})
}

// Fix round: the pane closing while the chair runs gives no card.
func TestPaneClosedDuringChairAbandonsIt(t *testing.T) {
	r, cb := councilRig(t, nil, member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	cb.on(council.ChairName, step{hang: true})
	convene(t, r, "")
	eventually(t, "chair running", func() bool { return len(cb.callsFor(council.ChairName)) == 1 && cb.running() == 1 })
	r.srv.RemovePane(pane)
	eventually(t, "chair cancelled", func() bool { return cb.running() == 0 })
	eventually(t, "council forgotten", func() bool {
		r.e.mu.Lock()
		defer r.e.mu.Unlock()
		return len(r.e.councils) == 0
	})
	for _, c := range r.q.All() {
		if c.Kind == queue.KindCouncil {
			t.Fatalf("a closed pane gets no council card: %+v", c)
		}
	}
}

// Fix round: the chair run shares max_parallel_runs with the members.
func TestChairHoldsAParallelismSlot(t *testing.T) {
	r, cb := councilRig(t, func(c *config.Config) { c.MaxParallelRuns = 1 },
		member("Security", dots.ModeQueue), member("Frontend", dots.ModeQueue))
	seat(t, r, "security", "frontend")
	cb.on("Security", says("critical", "JWT in localStorage."))
	cb.on("Frontend", says("warn", "No loading state."))
	gate := make(chan struct{})
	cb.on(council.ChairName, step{v: backend.Verdict{Relevant: true, Severity: "critical", Message: goodDigest}, wait: gate})
	convene(t, r, "")
	eventually(t, "chair running", func() bool { return cb.running() == 1 && len(cb.callsFor(council.ChairName)) == 1 })
	if len(r.e.sem) != 1 {
		t.Fatalf("the chair must hold a slot of the semaphore, held %d", len(r.e.sem))
	}
	close(gate)
	councilCard(t, r)
}

// Fix round: card text is sanitized where the card is built, whatever the
// callers did upstream.
func TestCardBuildSanitizesUnsanitizedParts(t *testing.T) {
	r, _ := councilRig(t, nil, member("Security", dots.ModeQueue))
	seat(t, r, "security")
	parts := []council.Part{
		{Slug: "security", Name: "Sec\x1b[31m", Glyph: "●", Verdict: backend.Verdict{Relevant: true, Severity: "warn", Message: "bad\x1b[2J text"},
			Stance: council.StanceObject, Reply: "re\x07ply", ReplySeverity: "warn"},
		{Slug: "frontend", Name: "Front", Glyph: "●", Err: "error: x\x1b]0;t\x07"},
	}
	run := &councilRun{id: "council-9", paneID: pane, question: "q\x1b[1m?", turn: 1}
	r.e.addCouncilCard(run, parts, "body\x1b[0m", "note\x07")
	c := councilCard(t, r)
	all := []string{c.Body, c.Question, c.Note}
	for _, p := range c.Parts {
		all = append(all, p.DotName, p.Body)
	}
	for _, s := range all {
		if strings.ContainsAny(s, "\x1b\x07") {
			t.Fatalf("raw control character in %q (card %+v)", s, c)
		}
	}
}

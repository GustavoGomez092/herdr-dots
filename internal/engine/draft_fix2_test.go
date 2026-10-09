package engine

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-dots/internal/queue"
)

// holdDraftInTranscriptRead starts a Draft that blocks inside its (unlocked)
// transcript read and returns once it is there.
func holdDraftInTranscriptRead(t *testing.T, r *draftRig) (rd *fakeReader, result chan error) {
	t.Helper()
	rd = r.readers["s1"]
	rd.arm()
	result = make(chan error, 1)
	go func() {
		_, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
		result <- err
	}()
	select {
	case <-rd.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Draft never read the transcript")
	}
	return rd, result
}

func (r *draftRig) noPatchCards(t *testing.T) {
	t.Helper()
	for _, c := range r.q.All() {
		if c.Kind == queue.KindPatch && c.Status != queue.StatusDismissed {
			t.Fatalf("a card for a closed pane: %+v", c)
		}
	}
	r.noDraftsInFlight(t)
	if ents, _ := os.ReadDir(r.wts); len(ents) != 0 || r.refs(t) != "" {
		t.Fatalf("worktree or ref left: %v %q", ents, r.refs(t))
	}
	if ms, _ := r.patches.List(); len(ms) != 0 {
		t.Fatalf("records left: %+v", ms)
	}
}

// Fix 2.1: the pane closes while Draft is between reading the attachment and
// registering. Nothing was flagged then, so the registration itself must see it.
func TestAPaneThatClosesBeforeTheDraftRegistersGetsNoDraft(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	rd, result := holdDraftInTranscriptRead(t, r)
	r.e.dropPane(pane)
	close(rd.release)
	if err := <-result; !errors.Is(err, ErrPaneClosed) {
		t.Fatalf("Draft = %v, want ErrPaneClosed", err)
	}
	if len(r.dr.writes()) != 0 {
		t.Fatal("a draft ran for a closed pane")
	}
	r.noPatchCards(t)
}

// The same, with the pane id re-attached during the hold: the new attachment
// is not the one the draft was asked for.
func TestAReusedPaneIdIsNotTheAttachmentADraftWasStartedFor(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	rd, result := holdDraftInTranscriptRead(t, r)
	r.e.dropPane(pane)
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	close(rd.release)
	if err := <-result; !errors.Is(err, ErrPaneClosed) {
		t.Fatalf("Draft = %v, want ErrPaneClosed", err)
	}
	if len(r.dr.writes()) != 0 {
		t.Fatal("a draft ran for the old attachment")
	}
	r.noPatchCards(t)
}

// A draft that is already running when its pane closes and is re-attached
// (generation changes) ends without a card or a charge even if nothing
// flagged it.
func TestARunningDraftEndsWhenItsAttachmentGenerationChanged(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	gate := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(gate) }) })
	r.dr.mu.Lock()
	r.dr.gate, r.dr.edit, r.dr.deaf = gate, fixJWT, true
	r.dr.mu.Unlock()
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the draft runs", func() bool { return len(r.dr.writes()) == 1 })
	r.e.dropPane(pane)
	r.e.mu.Lock() // the flag alone must not be what stops it
	for _, dr := range r.e.drafts {
		dr.abandoned = false
	}
	r.e.mu.Unlock()
	r.attach(t, "security")
	r.reviewsDone(t, 1)
	calls := r.usage.Calls(pane, "security")
	once.Do(func() { close(gate) })
	r.noDraftsInFlight(t)
	r.noPatchCards(t)
	if got := r.usage.Calls(pane, "security"); got != calls {
		t.Fatalf("the new attachment paid for the old draft: %d calls, was %d", got, calls)
	}
}

// Fix 2.2: Meta.Summary stays redacted for every display reader; the raw text
// lives in SummaryRaw, which only the memory outcome paths read.
func TestMetaSummaryIsRedactedAndSummaryRawIsNot(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"))
	key := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"
	r.dr.mu.Lock()
	r.dr.msg = "Rotated " + key + " out of the config."
	r.dr.mu.Unlock()
	r.dr.set(fixJWT, nil)
	r.attach(t, "security")
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	m, err := r.patches.Get(c.PatchID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Summary, key) || m.Summary != c.Body {
		t.Fatalf("Meta.Summary must be the redacted text: %q (card %q)", m.Summary, c.Body)
	}
	if !strings.Contains(m.SummaryRaw, key) {
		t.Fatalf("SummaryRaw = %q, want the text as written", m.SummaryRaw)
	}
}

// Fix 2.3: detaching one dot mid-draft still charges the pane for the call.
func TestDetachingOneDotMidDraftStillChargesTheBackendCall(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, writeDot("Security"), dot("Style", "queue"))
	gate := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})
	r.dr.mu.Lock()
	r.dr.gate, r.dr.edit = gate, fixJWT
	r.dr.mu.Unlock()
	r.attach(t, "security", "style")
	r.reviewsDone(t, 2)
	before := r.usage.PaneCalls(pane)
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the draft runs", func() bool { return len(r.dr.writes()) == 1 })
	if err := r.e.Detach(pane, "security"); err != nil {
		t.Fatal(err)
	}
	r.noDraftsInFlight(t)
	if got := r.usage.PaneCalls(pane); got != before+1 {
		t.Fatalf("pane calls %d, want %d: the backend call happened", got, before+1)
	}
	for _, c := range r.q.All() {
		if c.Kind == queue.KindPatch && c.Status != queue.StatusDismissed {
			t.Fatalf("a card for a detached dot: %+v", c)
		}
	}
}

// Fix 2.5: an apply holds its card against a concurrent send, in both orders.
func TestSendCardsCannotDeliverACardBeingApplied(t *testing.T) {
	setup := func(t *testing.T) (*draftRig, queue.Card) {
		r := newDraftRig(t, draftOpts{}, writeDot("Security"))
		r.dr.set(fixJWT, nil)
		r.attach(t, "security")
		if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
			t.Fatal(err)
		}
		c := r.patchCard(t)
		r.noDraftsInFlight(t)
		return r, c
	}
	t.Run("send during an apply", func(t *testing.T) {
		r, c := setup(t)
		var sendErr error
		r.e.testHook = func(point string) {
			if point == "apply-reserved" {
				sendErr = r.e.SendCards([]string{c.ID})
			}
		}
		if err := r.applyReviewed(c.ID); err != nil {
			t.Fatal(err)
		}
		if sendErr == nil {
			t.Fatal("SendCards delivered a card that is being applied")
		}
		if n := len(r.srv.Calls("agent.prompt")); n != 0 {
			t.Fatalf("%d prompts sent", n)
		}
		if got, _ := r.q.Get(c.ID); got.Status != queue.StatusApplied {
			t.Fatalf("card %+v", got)
		}
	})
	t.Run("apply after a send", func(t *testing.T) {
		r, c := setup(t)
		if err := r.e.SendCards([]string{c.ID}); err != nil {
			t.Fatal(err)
		}
		eventually(t, "sent", func() bool { got, _ := r.q.Get(c.ID); return got.Status == queue.StatusSent })
		if err := r.applyReviewed(c.ID); err == nil {
			t.Fatal("applied a card that was sent")
		}
		if got, _ := os.ReadFile(r.repo + "/auth.js"); strings.Contains(string(got), "setCookie") {
			t.Fatal("the patch was applied")
		}
	})
}

package engine

import (
	"strings"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/queue"
)

const digestChanged = "the digest changed since you read it: expand it again"

func addDigest(t *testing.T, r *proRig, parts int) queue.Card {
	t.Helper()
	ps := make([]queue.CardPart, parts)
	for i := range ps {
		ps[i] = queue.CardPart{Dot: "a", Body: "x"}
	}
	c, err := r.q.Add(queue.Card{PaneID: pane, Dot: "away", Kind: queue.KindDigest, Body: "d", Mode: string(dots.ModeQueue), Parts: ps})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSendCardsRefusesADigestWhoseCountDiffers(t *testing.T) {
	r := idleRig(t, researchDot("Security"), dot("Reviewer", dots.ModeQueue))
	c := addDigest(t, r, 2)
	for name, parts := range map[string]map[string]int{"stale": {c.ID: 1}, "missing": nil, "other id": {"zzz": 2}} {
		err := r.e.SendCardsSeen([]string{c.ID}, parts)
		if err == nil || !strings.Contains(err.Error(), digestChanged) {
			t.Fatalf("%s: err %v", name, err)
		}
		if cur, _ := r.q.Get(c.ID); cur.Approved || cur.Status != queue.StatusPending {
			t.Fatalf("%s: a refused digest was approved: %+v", name, cur)
		}
	}
	if err := r.e.SendCards([]string{c.ID}); err == nil || !strings.Contains(err.Error(), digestChanged) {
		t.Fatalf("plain SendCards must refuse a digest: %v", err)
	}
}

func TestSendCardsSendsADigestWhoseCountMatches(t *testing.T) {
	r := idleRig(t, researchDot("Security"), dot("Reviewer", dots.ModeQueue))
	c := addDigest(t, r, 2)
	if err := r.e.SendCardsSeen([]string{c.ID}, map[string]int{c.ID: 2}); err != nil {
		t.Fatalf("matching count: %v", err)
	}
	if cur, _ := r.q.Get(c.ID); !cur.Approved {
		t.Fatalf("matching count did not approve: %+v", cur)
	}
}

func TestSendCardsStaleDigestApprovesNothingInTheBatch(t *testing.T) {
	r := idleRig(t, researchDot("Security"), dot("Reviewer", dots.ModeQueue))
	plain, err := r.q.Add(queue.Card{PaneID: pane, Dot: "reviewer", Body: "plain", Mode: string(dots.ModeQueue)})
	if err != nil {
		t.Fatal(err)
	}
	dg := addDigest(t, r, 1)
	if err := r.e.SendCardsSeen([]string{plain.ID, dg.ID}, map[string]int{dg.ID: 0}); err == nil {
		t.Fatal("stale digest accepted")
	}
	if cur, _ := r.q.Get(plain.ID); cur.Approved {
		t.Fatal("the plain card was approved in a refused batch")
	}
	if err := r.e.SendCardsSeen([]string{plain.ID}, nil); err != nil {
		t.Fatalf("a non-digest send must not need counts: %v", err)
	}
}

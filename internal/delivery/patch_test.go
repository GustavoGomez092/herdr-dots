package delivery

import (
	"context"
	"errors"
	"testing"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/queue"
)

func TestPatchCardsAreNeverInjected(t *testing.T) {
	ft := &fakeTarget{pane: herdr.PaneInfo{AgentStatus: herdr.StatusIdle}}
	cards := []queue.Card{
		{Dot: "frontend", DotName: "Frontend", Glyph: "▲", Body: "Add a skeleton."},
		{Kind: queue.KindPatch, Dot: "security", DotName: "Security", Glyph: "◆", Body: "Patch ready."},
	}
	if _, err := Send(context.Background(), ft, "w1:p1", cards, true); !errors.Is(err, ErrPatchInject) {
		t.Fatalf("err %v, want ErrPatchInject", err)
	}
	if len(ft.sent) != 0 {
		t.Fatalf("nothing may be typed: %v", ft.sent)
	}
	if g, err := Send(context.Background(), ft, "w1:p1", cards, false); err != nil || g != Now || len(ft.sent) != 1 {
		t.Fatalf("an approved send still works: %v %v %v", g, err, ft.sent)
	}
}

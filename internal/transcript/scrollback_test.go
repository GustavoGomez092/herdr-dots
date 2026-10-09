package transcript

import (
	"context"
	"testing"
)

type fakePane struct{ text string }

func (f *fakePane) PaneRead(context.Context, string, string, int) (string, error) { return f.text, nil }

func TestScrollbackEmitsDeltaTurns(t *testing.T) {
	p := &fakePane{text: "$ claude\n> hello\nHi there"}
	r := NewScrollback(p, "w1:p1", 400)
	ctx := context.Background()

	turns, _ := r.Turns(ctx)
	if len(turns) != 1 || turns[0].Assistant != "$ claude\n> hello\nHi there" || r.Source() != SourceScrollback {
		t.Fatalf("first read %+v", turns)
	}
	turns, _ = r.Turns(ctx)
	if len(turns) != 1 {
		t.Fatal("unchanged screen must not add a turn")
	}
	p.text = "$ claude\n> hello\nHi there\n> [◆ Security dot] check auth\nDone."
	turns, _ = r.Turns(ctx)
	if len(turns) != 2 || turns[1].Assistant != "> [◆ Security dot] check auth\nDone." || turns[1].Index != 2 {
		t.Fatalf("delta %+v", turns)
	}
}

package pane

import (
	"fmt"
	"strings"
	"testing"
)

// Cards the snapshot trimmed away (view.Trim) are counted, never silently
// missing: the queue says how many more there are, on one line.
func TestQueueSaysHowManyCardsAreNotShown(t *testing.T) {
	s := snap()
	s.Panes[0].HiddenCards = 37
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	v := m.View()
	if n := strings.Count(v, "+37 not shown"); n != 1 {
		t.Fatalf("want one \"+37 not shown\" line, got %d:\n%s", n, v)
	}
	if v := ready(&fakeCaller{}).View(); strings.Contains(v, "not shown") {
		t.Fatalf("nothing hidden, yet:\n%s", v)
	}
}

// The QUEUE header counts every queued card, the trimmed ones too.
func TestQueueHeaderCountsHiddenCards(t *testing.T) {
	s := snap()
	shown := len(s.Panes[0].Cards)
	s.Panes[0].HiddenCards = 37
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	if want := fmt.Sprintf("QUEUE (%d)", shown+37); !strings.Contains(m.View(), want) {
		t.Fatalf("want %q:\n%s", want, m.View())
	}
}

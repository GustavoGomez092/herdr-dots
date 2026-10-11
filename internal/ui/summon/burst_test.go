package summon

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

// burst is several printable keys arriving in one read: Bubble Tea merges
// them into one KeyMsg (B1).
func burst(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func fiveDotsSnap() SnapshotMsg {
	var ds []view.DotState
	for i := 1; i <= 5; i++ {
		ds = append(ds, view.DotState{Slug: fmt.Sprintf("dot%d", i), Name: fmt.Sprintf("Dot%d", i), Glyph: "◆"})
	}
	return SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: ds}}})
}

func TestABurstOfJMovesOneSummonRowPerRune(t *testing.T) {
	m, _ := New("w1:p1", &fakeCaller{}).Update(fiveDotsSnap())
	m, _ = m.Update(burst("jjj"))
	if got := m.(Model).cursor; got != 3 {
		t.Fatalf("cursor %d after \"jjj\", want 3", got)
	}
}

func TestABurstIsTypedIntoTheSummonQuestion(t *testing.T) {
	fc := &fakeCaller{}
	m, _ := New("w1:p1", fc).Update(fiveDotsSnap())
	m, _ = m.Update(key("enter"))
	m, _ = m.Update(burst("hello"))
	if got := m.(Model).input.Value(); got != "hello" {
		t.Fatalf("question %q, want hello", got)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls %v, want none", fc.calls)
	}
}

func TestABurstOfJMovesOneCouncilRowPerRune(t *testing.T) {
	m, _ := NewCouncil("w1:p1", &fakeCaller{}).Update(fiveDotsSnap())
	m, _ = m.Update(ckey("tab")) // focus the member list
	m, _ = m.Update(burst("jjj"))
	if got := m.(Model).council.cursor; got != 3 {
		t.Fatalf("council cursor %d after \"jjj\", want 3", got)
	}
}

func TestABurstIsTypedIntoTheCouncilQuestion(t *testing.T) {
	m, _ := NewCouncil("w1:p1", &fakeCaller{}).Update(fiveDotsSnap())
	m, _ = m.Update(burst("hello"))
	if got := m.(Model).council.question.Value(); got != "hello" {
		t.Fatalf("council question %q, want hello", got)
	}
}

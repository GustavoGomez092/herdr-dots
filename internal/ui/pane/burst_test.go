package pane

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/queue"
)

// burst is several printable keys arriving in one read: Bubble Tea merges
// them into one KeyMsg (B1).
func burst(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// update sends msg and runs the command it returns, like the runtime.
func update(m tea.Model, msg tea.Msg) tea.Model {
	m, cmd := m.Update(msg)
	runCmd(&m, cmd)
	return m
}

func runCmd(m *tea.Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			runCmd(m, c)
		}
	default:
		*m, _ = (*m).Update(msg)
	}
}

func fiveCards() []queue.Card {
	var cs []queue.Card
	for i := 1; i <= 5; i++ {
		cs = append(cs, queue.Card{ID: fmt.Sprint(i), Dot: "security", DotName: "Security", Glyph: "◆", Severity: "warn",
			Body: fmt.Sprintf("card %d", i), Mode: "queue", Status: queue.StatusPending, Turn: 4})
	}
	return cs
}

func TestABurstOfJMovesOneCardPerRune(t *testing.T) {
	s := snap()
	s.Panes[0].Cards = fiveCards()
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	m = update(m, burst("jjj"))
	if got := m.(Model).card; got != 3 {
		t.Fatalf("card cursor %d after \"jjj\", want 3", got)
	}
}

func TestABurstOfJMovesOneRosterRowPerRune(t *testing.T) {
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(sixDots()))
	m = press(m, "tab")
	m = update(m, burst("jjj"))
	if got := m.(Model).roster; got != 3 {
		t.Fatalf("roster cursor %d after \"jjj\", want 3", got)
	}
}

// A pasted question is typed into the summon field, never run as keys.
func TestABurstIsTypedIntoTheSummonQuestion(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), "tab", "s")
	m = update(m, burst("hello"))
	if got := m.(Model).question.Value(); got != "hello" {
		t.Fatalf("question %q, want hello", got)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls %+v, want none", fc.calls)
	}
}

// "Mf" in one read opens the remember chooser; f, which would answer it,
// must come as its own key.
func TestABurstNeverAnswersTheRememberChooser(t *testing.T) {
	fc := &fakeCaller{}
	m := update(ready(fc), burst("Mf"))
	if !m.(Model).remembering {
		t.Fatal("M did not open the chooser")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls %+v: the burst answered the chooser", fc.calls)
	}
	m = press(m, "f")
	if len(fc.calls) != 1 {
		t.Fatalf("a separate f must answer it, calls %+v", fc.calls)
	}
}

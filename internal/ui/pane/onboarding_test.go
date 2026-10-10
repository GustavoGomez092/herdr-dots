package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Ruling P10-R14: the Dots pane never shows the Setup screen. When setup is
// pending it says so in one dim line, records nothing and stays usable.
func TestThePaneShowsASetupHintAndStaysUsable(t *testing.T) {
	calls := 0
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc).WithSetupPending(func() bool { calls++; return true })
	cmd := m.Init()
	if cmd == nil || calls != 0 {
		t.Fatalf("detection runs in Init's command, not before: cmd=%v calls=%d", cmd != nil, calls)
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m, _ = m.Update(SnapshotMsg(snap()))
	m, _ = m.Update(cmd())
	if calls != 1 {
		t.Fatalf("detection ran %d times", calls)
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(strings.Join(strings.Fields(v), " "), setupHint) {
		t.Fatalf("the hint is missing:\n%s", v)
	}
	if !strings.Contains(v, "DOTS") || strings.Contains(v, "Let your agent consult") {
		t.Fatalf("the pane itself is shown, never the Setup screen:\n%s", v)
	}
	for _, l := range strings.Split(m.View(), "\n") {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line %q is wider than the 40-column pane", l)
		}
	}
	m = press(m, "tab", "j", "x") // roster focus; x on the roster asks to tab back
	if !strings.Contains(m.View(), "tab to the queue") {
		t.Fatalf("the pane's keys work:\n%s", m.View())
	}
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q quits")
	}
}

func TestNoHintWhenNothingIsPending(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{}).WithSetupPending(func() bool { return false })
	m, _ = m.Update(SnapshotMsg(snap()))
	m, _ = m.Update(m.Init()())
	if strings.Contains(m.View(), "Setup pending") {
		t.Fatalf("view:\n%s", m.View())
	}
	if New("w1:p1", &fakeCaller{}).Init() != nil {
		t.Fatal("without WithSetupPending, Init does nothing")
	}
}

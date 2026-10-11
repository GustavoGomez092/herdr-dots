package memui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/memory"
)

// burst is several printable keys arriving in one read: Bubble Tea merges
// them into one KeyMsg (B1).
func burst(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func moreFindings(t *testing.T, st *memory.Store) {
	t.Helper()
	for _, text := range []string{"SQL built by string concatenation in reports.go", "Session cookie lacks the Secure flag", "Uploads accept any MIME type"} {
		e := memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindFinding, Text: text, Source: memory.SourceOutcome}
		if _, err := st.Add(pid, e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestABurstOfJMovesOneMemoryRowPerRune(t *testing.T) {
	st := seed(t)
	moreFindings(t, st)
	n := 0
	m, _ := open(st, "security", &n).Update(burst("jjj"))
	if got := m.(Model).cursor; got != 3 {
		t.Fatalf("cursor %d after \"jjj\", want 3", got)
	}
}

// "xy" in one read acts as x then y: x opens the forget confirm, and the y
// in the same burst never answers it.
func TestABurstNeverConfirmsAForget(t *testing.T) {
	st := seed(t)
	n := 0
	m, cmd := open(st, "security", &n).Update(burst("xy"))
	m = run(m, cmd)
	if got := m.(Model).mode; got != modeConfirmForget {
		t.Fatalf("mode %v after \"xy\", want the forget confirm still open", got)
	}
	if es := entries(t, st, memory.ScopeDot, "security"); len(es) != 2 {
		t.Fatalf("%d entries left, want 2: the burst forgot one", len(es))
	}
	m = press(m, "y")
	if es := entries(t, st, memory.ScopeDot, "security"); len(es) != 1 {
		t.Fatalf("%d entries left, want 1: a separate y must forget", len(es))
	}
}

func TestABurstIsTypedIntoTheFilter(t *testing.T) {
	n := 0
	m := press(open(seed(t), "security", &n), "/")
	m, _ = m.Update(burst("hello"))
	if got := m.(Model).input.Value(); got != "hello" {
		t.Fatalf("filter %q, want hello", got)
	}
}

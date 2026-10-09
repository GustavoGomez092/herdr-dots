package profile

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

func TestHostileTextIsSanitizedAndFitted(t *testing.T) {
	hs, st := fixture(t)
	evil := "Ev\x1b[31mil‮" + strings.Repeat("N", 200)
	m := New(Deps{History: hs, Memory: st, Now: func() time.Time { return pNow }, Purpose: "p\x1b]0;x\x07 " + strings.Repeat("w ", 100),
		Dot: view.LibraryDot{Slug: "security", Name: evil, Backend: "b\x1b[2J", Model: "m‮", Mode: "q", Origin: "o\x07"}})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m, _ = pdrive(t, m, m.Init())
	v := m.View()
	for _, bad := range []string{"\x1b[31m", "\x1b[2J", "\x1b]0", "‮", "\x07"} {
		if strings.Contains(v, bad) {
			t.Fatalf("unsanitized %q in:\n%s", bad, v)
		}
	}
}

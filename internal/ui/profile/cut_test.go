package profile

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/view"
)

// Ruling P10-R3: a purpose the snapshot cut is never shown as the whole
// trusted copy.
func TestProfileMarksACutPurpose(t *testing.T) {
	for _, cut := range []bool{true, false} {
		hs, st := fixture(t)
		m := New(Deps{History: hs, Memory: st, Now: func() time.Time { return pNow }, Purpose: "Be helpful.…", PurposeCut: cut,
			Dot: view.LibraryDot{Slug: "proj:helper", Name: "Helper", Glyph: "◆", Origin: "project", Trusted: true, PurposeCut: cut}})
		m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		m, _ = pdrive(t, m, m.Init())
		v := m.View()
		if got := strings.Contains(v, "Purpose (cut): Be helpful."); got != cut {
			t.Fatalf("cut=%v, marked=%v:\n%s", cut, got, v)
		}
		if !cut && !strings.Contains(v, "Purpose: Be helpful.") {
			t.Fatalf("uncut purpose:\n%s", v)
		}
	}
}

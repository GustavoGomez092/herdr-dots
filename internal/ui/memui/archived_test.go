package memui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/memory"
)

// Phase 9 residual (P9-R15): approving or unpinning an import in a full scope
// archives it at once; the browser must say so instead of reporting success.

const archivedNote = "this dot's memory is full and imports give way to your own entries"

func fullStore(t *testing.T, limit int) *memory.Store {
	t.Helper()
	lim := memory.DefaultLimits()
	lim.MaxDot = limit
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), lim)
	for i := range limit {
		if _, err := st.Add(pid, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote,
			Text: fmt.Sprintf("word%dx kilo%dy lima%dz", i, i, i), Source: memory.SourceUser}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// selectRow moves the cursor down until the selected row shows text.
func selectRow(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	for range 12 {
		for _, line := range strings.Split(m.View(), "\n") {
			if strings.Contains(line, "▸ ") && strings.Contains(line, text) {
				return m
			}
		}
		m = press(m, "j")
	}
	t.Fatalf("no row %q:\n%s", text, m.View())
	return m
}

func pendingImport(t *testing.T, st *memory.Store, text string) {
	t.Helper()
	if _, err := st.ImportPack(pid, "security", []memory.Entry{{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: text}}); err != nil {
		t.Fatal(err)
	}
}

func wantArchived(t *testing.T, m tea.Model, st *memory.Store, verb string) {
	t.Helper()
	ref := memory.Ref{Scope: memory.ScopeDot, Dot: "security", ID: "m3"}
	want := verb + " " + ref.String() + ", then archived: " + archivedNote
	if v := m.View(); !strings.Contains(flat(v), want) { // the status wraps to the popup
		t.Fatalf("want %q in:\n%s", want, v)
	}
	if e, _ := st.Get(pid, ref); e.Status != memory.StatusArchived {
		t.Fatalf("not archived: %+v", e)
	}
}

func TestApproveSaysWhenTheEntryWasArchivedAtOnce(t *testing.T) {
	st, n := fullStore(t, 2), 0
	pendingImport(t, st, "Imported advice about caching")
	m := selectRow(t, open(st, "security", &n), "Imported advice")
	wantArchived(t, press(m, "a", "y"), st, "approved")
}

func TestUnchangedSaveSaysWhenTheEntryWasArchivedAtOnce(t *testing.T) {
	st, n := fullStore(t, 2), 0
	pendingImport(t, st, "Imported advice about caching")
	m := selectRow(t, open(st, "security", &n), "Imported advice")
	wantArchived(t, press(m, "e", "ctrl+s"), st, "approved (still imported)")
}

func TestUnpinSaysWhenTheEntryWasArchivedAtOnce(t *testing.T) {
	st, n := fullStore(t, 2), 0
	// A pinned approved import beside two own entries, then a lower limit.
	st.Limits.MaxDot = 3
	if _, err := st.Add(pid, memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindNote,
		Text: "Imported pinned advice", Source: memory.SourceImport, Status: memory.StatusActive, Pinned: true}); err != nil {
		t.Fatal(err)
	}
	st.Limits.MaxDot = 2
	m := selectRow(t, open(st, "security", &n), "Imported pinned")
	wantArchived(t, press(m, "p"), st, "unpinned")
}

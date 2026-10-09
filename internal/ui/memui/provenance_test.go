package memui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/memory"
)

// Phase 9: an imported pack's memory is never shown as the developer's.
func TestImportedMemoryIsMarkedInRowsAndOnApproval(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	if _, err := st.ImportPack(pid, "security", []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis."},
	}); err != nil {
		t.Fatal(err)
	}
	n := 0
	m, _ := open(st, "", &n).Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	if v := m.View(); !strings.Contains(v, "(imported)") || strings.Contains(v, "(you)") {
		t.Fatalf("row not marked imported:\n%s", v)
	}
	v := press(m, "a").View()
	if !strings.Contains(v, "proposed by: security (imported from a dot pack)") {
		t.Fatalf("approval screen hides the provenance:\n%s", v)
	}
}

// The row also says which dot's pack an entry came from (project rows carry
// no dot otherwise), cleaned like every other shown string.
func TestImportedRowNamesItsPack(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	if _, err := st.ImportPack(pid, "security", []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis.\x1b[2J"},
	}); err != nil {
		t.Fatal(err)
	}
	n := 0
	m, _ := open(st, "", &n).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	v := m.View()
	if !strings.Contains(v, "(imported) from security: Sessions live in Redis.") || strings.Contains(v, "\x1b[2J") {
		t.Fatalf("row doesn't name the pack:\n%q", v)
	}
}

// Editing an imported dot-scope proposal makes it the developer's own words,
// and the developer's dot memory is active (dot scope has no user proposals).
func TestEditingAnImportedDotProposalMakesItYours(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	if _, err := st.ImportPack(pid, "security", []memory.Entry{
		{Scope: memory.ScopeDot, Kind: memory.KindNote, Text: "Prefer zod."},
	}); err != nil {
		t.Fatal(err)
	}
	n := 0
	m := open(st, "security", &n)
	m = press(m, "e", "ctrl+u", "Prefer zod at API boundaries.", "ctrl+s")
	es := entries(t, st, memory.ScopeDot, "security")
	if len(es) != 1 || es[0].Text != "Prefer zod at API boundaries." || es[0].Source != memory.SourceUser || es[0].Status != memory.StatusActive {
		t.Fatalf("entries %+v\n%s", es, m.View())
	}
	if v := m.View(); !strings.Contains(v, "saved and approved d:m1") {
		t.Fatalf("the edit approved the entry without saying so:\n%s", v)
	}
}

// "(imported)" is the widest source label; every row's text starts in the
// same column.
func TestSourceColumnFitsImported(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	if _, err := st.ImportPack(pid, "security", []memory.Entry{
		{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis."},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add(pid, memory.Entry{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Queues live in SQS.", Source: memory.SourceUser, Status: memory.StatusProposed}); err != nil {
		t.Fatal(err)
	}
	n := 0
	m, _ := open(st, "", &n).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	col := func(needle string) int {
		for _, l := range strings.Split(m.View(), "\n") {
			if i := strings.Index(l, needle); i >= 0 {
				return len([]rune(l[:i]))
			}
		}
		t.Fatalf("no %q in:\n%s", needle, m.View())
		return -1
	}
	if a, b := col("from security:"), col("Queues live in SQS."); a != b {
		t.Fatalf("text columns %d and %d differ:\n%s", a, b, m.View())
	}
}

// Phase 2's `memory import` restores your own export with Source import but
// no pack: its approval screen says so instead of naming a dot pack.
func TestRestoredProposalIsNotLabelledAPack(t *testing.T) {
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	if _, err := st.Add(pid, memory.Entry{Scope: memory.ScopeProject, Dot: "frontend", Kind: memory.KindDecision, Text: "Sessions live in Redis.",
		Status: memory.StatusProposed, Source: memory.SourceImport}); err != nil {
		t.Fatal(err)
	}
	n := 0
	m, _ := open(st, "", &n).Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	v := press(m, "a").View()
	if !strings.Contains(v, "proposed by: frontend (restored from backup)") || strings.Contains(v, "dot pack") {
		t.Fatalf("restore labelled as a pack:\n%s", v)
	}
}

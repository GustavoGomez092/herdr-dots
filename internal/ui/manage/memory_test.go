package manage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
)

const memPID = "g-0123456789abcdef"

func memSetup(t *testing.T) (tea.Model, *fakeCaller, string, *memory.Store) {
	t.Helper()
	dir := t.TempDir()
	st := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits())
	fc := &fakeCaller{}
	project := func(target string) (memory.Project, error) {
		if target != "w1:p1" {
			t.Errorf("resolved project for %q", target)
		}
		return memory.Project{ID: memPID, Name: "herdr-dots"}, nil
	}
	m, _ := New("w1:p1", Deps{Caller: fc, DotsDir: dir, Check: func(context.Context, string) error { return nil },
		Memory: st, Project: project}).Update(SnapshotMsg(lib()))
	return m, fc, dir, st
}

func addMem(t *testing.T, st *memory.Store, p string, e memory.Entry) {
	t.Helper()
	if _, err := st.Add(p, e); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryKeyOpensTheSelectedDotsMemory(t *testing.T) {
	m, _, _, st := memSetup(t)
	addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeDot, Dot: "backend", Kind: memory.KindNote, Text: "Handlers return problem+json."})
	m = press(m, "M") // the cursor is on Backend
	v := m.View()
	if !strings.Contains(v, "MEMORY → Backend · project herdr-dots") || !strings.Contains(v, "Handlers return problem+json.") {
		t.Fatalf("memory screen:\n%s", v)
	}
	m = press(m, "esc")
	if !strings.Contains(m.View(), "DOTS LIBRARY") {
		t.Fatalf("esc returns to the library:\n%s", m.View())
	}
}

func TestProjectMemoryKey(t *testing.T) {
	m, _, _, st := memSetup(t)
	addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Postgres is the only datastore."})
	v := press(m, "P").View()
	if !strings.Contains(v, "MEMORY → project") || !strings.Contains(v, "Postgres is the only datastore.") {
		t.Fatalf("project memory:\n%s", v)
	}
}

func TestMemoryEditsNotifyTheEngine(t *testing.T) {
	m, fc, _, st := memSetup(t)
	addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeDot, Dot: "backend", Kind: memory.KindNote, Text: "Handlers return problem+json."})
	press(m, "M", "x", "y")
	if es, _ := st.List(memPID, memory.Filter{Scope: memory.ScopeDot, Dot: "backend"}); len(es) != 0 {
		t.Fatalf("entry not forgotten: %+v", es)
	}
	if last := fc.calls[len(fc.calls)-1]; !strings.HasPrefix(last, ipc.CmdMemoryChanged) {
		t.Fatalf("want memory_changed after an edit, got %v", fc.calls)
	}
}

func TestMemoryKeysWithoutAStore(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	if v := press(m, "M").View(); !strings.Contains(v, "memory is turned off") {
		t.Fatalf("M without memory:\n%s", v)
	}
	if strings.Contains(m.View(), "M memory") {
		t.Fatal("no memory keys in the footer when memory is off")
	}
}

func TestDeleteDotAsksAboutMemoryAndNKeepsIt(t *testing.T) {
	m, _, dir, st := memSetup(t)
	d, _ := dots.FromTemplate("backend")
	_ = dots.Save(dir, d, true)
	addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeDot, Dot: "backend", Kind: memory.KindNote, Text: "Handlers return problem+json."})
	addMem(t, st, "", memory.Entry{Scope: memory.ScopeGlobal, Dot: "backend", Kind: memory.KindPreference, Text: "Prefer Go."})
	m = press(m, "D", "y")
	if _, err := os.Stat(filepath.Join(dir, "backend.toml")); !os.IsNotExist(err) {
		t.Fatal("the dot file is deleted on y")
	}
	if !strings.Contains(m.View(), "Also delete its memory in all projects? (Y/n)") {
		t.Fatalf("memory question:\n%s", m.View())
	}
	m = press(m, "n")
	if es, _ := st.List(memPID, memory.Filter{Dot: "backend"}); len(es) != 2 {
		t.Fatalf("n must keep memory: %+v", es)
	}
	if !strings.Contains(m.View(), "memory kept") {
		t.Fatalf("status:\n%s", m.View())
	}
}

func TestDeleteDotWithMemoryRemovesItEverywhere(t *testing.T) {
	m, fc, dir, st := memSetup(t)
	d, _ := dots.FromTemplate("backend")
	_ = dots.Save(dir, d, true)
	addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeDot, Dot: "backend", Kind: memory.KindNote, Text: "Handlers return problem+json."})
	addMem(t, st, "p-00000000000000ff", memory.Entry{Scope: memory.ScopeDot, Dot: "backend", Kind: memory.KindNote, Text: "Other project."})
	addMem(t, st, "", memory.Entry{Scope: memory.ScopeGlobal, Dot: "backend", Kind: memory.KindPreference, Text: "Prefer Go."})
	m = press(m, "D", "y", "y")
	if es, _ := st.List(memPID, memory.Filter{Dot: "backend"}); len(es) != 0 {
		t.Fatalf("memory left: %+v", es)
	}
	if es, _ := st.List("p-00000000000000ff", memory.Filter{Dot: "backend"}); len(es) != 0 {
		t.Fatalf("memory left in another project: %+v", es)
	}
	if !strings.Contains(m.View(), "deleted Backend and its memory (3 files)") {
		t.Fatalf("status:\n%s", m.View())
	}
	if last := fc.calls[len(fc.calls)-1]; !strings.HasPrefix(last, ipc.CmdMemoryChanged) {
		t.Fatalf("calls %v", fc.calls)
	}
}

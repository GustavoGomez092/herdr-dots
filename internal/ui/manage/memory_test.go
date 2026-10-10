package manage

import (
	"context"
	"errors"
	"fmt"
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
	if !strings.Contains(m.View(), "Also delete its memory in all projects? (y/N)") {
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

func TestDeleteDotWithMemoryAlsoForgetsItsLedgerAndProposals(t *testing.T) {
	m, _, dir, st := memSetup(t)
	d, _ := dots.FromTemplate("backend")
	_ = dots.Save(dir, d, true)
	if err := st.RecordOutcome(memPID, memory.Outcome{Dot: "backend", Card: "7", Kind: memory.OutcomeDismissed, Body: "Use a connection pool."}); err != nil {
		t.Fatal(err)
	}
	addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeProject, Dot: "backend", Kind: memory.KindConvention, Text: "Handlers return problem+json.", Status: memory.StatusProposed, Source: memory.SourceModel})
	m = press(m, "D", "y", "y")
	if !strings.Contains(m.View(), "deleted Backend and its memory (1 file, 1 ledger line, 1 proposal)") {
		t.Fatalf("status:\n%s", m.View())
	}
	if l, _ := st.Ledger(memPID); len(l) != 0 {
		t.Fatalf("ledger left: %+v", l)
	}
	if es, _ := st.List(memPID, memory.Filter{Scope: memory.ScopeProject}); len(es) != 0 {
		t.Fatalf("proposal left: %+v", es)
	}
}

func memSetupWith(t *testing.T, project func(string) (memory.Project, error)) (tea.Model, *memory.Store, string) {
	t.Helper()
	root := t.TempDir()
	st := memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits())
	m, _ := New("w1:p1", Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil },
		Memory: st, Project: project}).Update(SnapshotMsg(lib()))
	return m, st, root
}

func TestMemoryOpensDegradedWhenProjectLookupFails(t *testing.T) {
	m, st, _ := memSetupWith(t, func(string) (memory.Project, error) {
		return memory.Project{}, errors.New("herdr pane.get failed")
	})
	addMem(t, st, "", memory.Entry{Scope: memory.ScopeGlobal, Dot: "backend", Kind: memory.KindPreference, Text: "Prefer Go."})
	v := press(m, "M").View()
	if !strings.Contains(v, "MEMORY → Backend") || !strings.Contains(v, "Prefer Go.") {
		t.Fatalf("degraded browser:\n%s", v)
	}
	if !strings.Contains(v, "project memory unavailable: herdr pane.get failed") {
		t.Fatalf("error not surfaced:\n%s", v)
	}
}

func TestMemoryWithoutAProjectIsGlobalOnlyAndRefusesProjectScope(t *testing.T) {
	m, st, root := memSetupWith(t, func(string) (memory.Project, error) { return memory.Project{}, nil })
	addMem(t, st, "", memory.Entry{Scope: memory.ScopeGlobal, Dot: "backend", Kind: memory.KindPreference, Text: "Prefer Go."})
	opened := press(m, "M")
	if v := opened.View(); !strings.Contains(v, "no project found; showing this dot's global memory") {
		t.Fatalf("status:\n%s", v)
	}
	// Only the global view exists: tab must not reach a project scope.
	if after := press(opened, "tab").View(); !strings.Contains(after, "scope: global") {
		t.Fatalf("tab must not reach a project scope:\n%s", after)
	}
	// P (project memory) is refused with a clear message.
	if pv := press(m, "P").View(); !strings.Contains(pv, "DOTS LIBRARY") || !strings.Contains(pv, "no project") {
		t.Fatalf("P without a project:\n%s", pv)
	}
	// Nothing was written under a project path.
	entries, _ := os.ReadDir(filepath.Join(root, "memory"))
	for _, e := range entries {
		if e.IsDir() && e.Name() != "_global" {
			t.Fatalf("unexpected directory written: %s", e.Name())
		}
	}
}

// Final review Important 3: the memory screen opened from manage scrolls.
func TestMemoryScreenFromManageScrolls(t *testing.T) {
	m, _, _, st := memSetup(t)
	for i := 0; i < 30; i++ {
		addMem(t, st, memPID, memory.Entry{Scope: memory.ScopeProject, Kind: memory.KindNote, Text: fmt.Sprintf("note number %03d", i), Source: memory.SourceUser})
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = press(m, "P")
	for i := 0; i < 25; i++ {
		m = press(m, "j")
	}
	v := m.View()
	if !strings.Contains(v, "MEMORY → project") || !strings.Contains(v, "▸ p:m26") || strings.Count(v, "\n") >= 10 {
		t.Fatalf("header and selected p:m26 must fit in 10 lines:\n%s", v)
	}
	// Later size changes reach the memory screen too.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 7})
	if v := m.View(); !strings.Contains(v, "MEMORY → project") || !strings.Contains(v, "▸ p:m26") || strings.Count(v, "\n") >= 7 {
		t.Fatalf("after a resize to 7 lines:\n%s", v)
	}
}

// Ruling P10-R13 (replaces Task 14 fix round 1's pane checkout): the memory
// screen's export defaults to the home directory, never into the target
// pane's checkout, where `git add -A` would publish global and
// secret-confirmed memory (nor into the popup's cwd, the install directory).
func TestMemoryScreenExportsIntoHomeNotThePanesCheckout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m, _, _, _ := memSetup(t)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := lib()
	s.PaneRoots = map[string]string{"w1:p1": repo}
	m, _ = m.Update(SnapshotMsg(s))
	v := press(m, "P", "E").View()
	flat := strings.Join(strings.Fields(v), "")
	if want := strings.Join(strings.Fields(filepath.Join(home, "herdr-dots-memory-herdr-dots-")), ""); !strings.Contains(flat, want) {
		t.Fatalf("the export default must be in the home directory %s:\n%s", home, v)
	}
	if strings.Contains(flat, strings.Join(strings.Fields(repo), "")) || strings.Contains(v, "git repository") {
		t.Fatalf("the export default must not be in the pane's checkout %s:\n%s", repo, v)
	}
}

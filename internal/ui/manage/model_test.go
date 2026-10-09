package manage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

type fakeCaller struct{ calls []string }

func (f *fakeCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, cmd+" "+string(b))
	return nil
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press runs returned commands (and the messages they produce) like the runtime.
func press(m tea.Model, keys ...string) tea.Model {
	for _, s := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(s))
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				break
			}
			if _, quit := msg.(tea.QuitMsg); quit {
				break
			}
			m, cmd = m.Update(msg)
		}
	}
	return m
}

// pickTemplate opens the template list and moves the cursor to name, so a
// test never depends on how many templates sort before it.
func pickTemplate(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	m = press(m, "t")
	for i := 0; i <= len(dots.Templates()); i++ {
		for _, line := range strings.Split(m.View(), "\n") {
			if strings.HasPrefix(line, "▸ ") && strings.HasSuffix(line, " "+name) {
				return m
			}
		}
		m = press(m, "j")
	}
	t.Fatalf("template %s not in the list:\n%s", name, m.View())
	return m
}

func lib() view.Snapshot {
	return view.Snapshot{
		Library: []view.LibraryDot{
			{Slug: "backend", Name: "Backend", Glyph: "▼", Backend: "claude-cli", Model: "sonnet", Mode: "queue"},
			{Slug: "security", Name: "Security", Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Mode: "queue"},
		},
		Panes:   []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{{Slug: "security"}}}},
		Invalid: []view.Invalid{{File: "broken.toml", Line: 2, Error: "bad"}},
	}
}

func setup(t *testing.T, target string) (tea.Model, *fakeCaller, string) {
	dir := t.TempDir()
	fc := &fakeCaller{}
	m, _ := New(target, Deps{Caller: fc, DotsDir: dir, Check: func(context.Context, string) error { return nil }}).Update(SnapshotMsg(lib()))
	return m, fc, dir
}

func TestLibraryShowsAttachmentAndInvalid(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	v := m.View()
	for _, want := range []string{"target: w1:p1", "[ ] ▼ Backend", "[x] ◆ Security", "broken.toml:2"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q\n%s", want, v)
		}
	}
}

func TestApplyAttachesAndDetaches(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	press(m, "space", "j", "space", "enter") // check Backend, uncheck Security
	want := []string{
		ipc.CmdAttach + ` {"pane_id":"w1:p1","slugs":["backend"]}`,
		ipc.CmdDetach + ` {"pane_id":"w1:p1","slug":"security"}`,
	}
	if strings.Join(fc.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(fc.calls, "\n"))
	}
}

func TestNoTargetIsLibraryOnly(t *testing.T) {
	m, fc, _ := setup(t, "")
	m = press(m, "space")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "focus an agent pane") {
		t.Fatal("space without a target must explain, not attach")
	}
}

func TestCreateFromTemplateSavesAndReloads(t *testing.T) {
	m, fc, dir := setup(t, "w1:p1")
	m = press(pickTemplate(t, m, "Copywriter"), "enter", "ctrl+s")
	if _, err := os.Stat(filepath.Join(dir, "copywriter.toml")); err != nil {
		t.Fatalf("dot file not written: %v\n%s", err, m.View())
	}
	if last := fc.calls[len(fc.calls)-1]; !strings.HasPrefix(last, ipc.CmdReloadDots) {
		t.Fatalf("want reload after save, got %v", fc.calls)
	}
	if !strings.Contains(m.View(), "saved copywriter") {
		t.Fatalf("status:\n%s", m.View())
	}
}

// Editing an unattached dot must not silently check it for attaching; only a
// newly created dot is pre-selected.
func TestEditingUnattachedDotDoesNotSelectIt(t *testing.T) {
	m, fc, dir := setup(t, "w1:p1")
	d, _ := dots.FromTemplate("backend")
	_ = dots.Save(dir, d, true)
	m = press(m, "e", "ctrl+s") // Backend is under the cursor and unattached
	if !strings.Contains(m.View(), "saved backend") {
		t.Fatalf("status:\n%s", m.View())
	}
	if strings.Contains(m.View(), "[x] ▼ Backend") {
		t.Fatalf("edited dot must stay unchecked:\n%s", m.View())
	}
	fc.calls = nil
	press(m, "enter")
	for _, c := range fc.calls {
		if strings.HasPrefix(c, ipc.CmdAttach) {
			t.Fatalf("enter after an edit attached a dot: %v", fc.calls)
		}
	}
}

func TestNewDotIsSelectedAfterSave(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	m = press(pickTemplate(t, m, "Copywriter"), "enter", "ctrl+s") // create Copywriter
	s := lib()
	s.Library = append(s.Library, view.LibraryDot{Slug: "copywriter", Name: "Copywriter", Glyph: "✎", Backend: "claude-cli", Model: "sonnet", Mode: "suggest"})
	m, _ = m.Update(SnapshotMsg(s))
	fc.calls = nil
	press(m, "enter")
	if len(fc.calls) == 0 || fc.calls[0] != ipc.CmdAttach+` {"pane_id":"w1:p1","slugs":["copywriter"]}` {
		t.Fatalf("a new dot is attached on enter: %v", fc.calls)
	}
}

func TestDuplicateNameShowsError(t *testing.T) {
	m, _, dir := setup(t, "w1:p1")
	d, _ := dots.FromTemplate("security")
	_ = dots.Save(dir, d, true)
	m = press(m, "t")
	for i := 0; i < len(dots.Templates()); i++ {
		if strings.Contains(m.View(), "▸ ◆ Security") {
			break
		}
		m = press(m, "j")
	}
	m = press(m, "enter", "ctrl+s")
	if !strings.Contains(m.View(), "already exists") {
		t.Fatalf("want duplicate error:\n%s", m.View())
	}
}

func TestNewDotUsesConfigDefaults(t *testing.T) {
	fc := &fakeCaller{}
	newDot := func() dots.Dot { d := dots.New(""); d.Backend.Kind, d.Backend.Model = "codex-cli", "gpt-5"; return d }
	var m tea.Model = New("w1:p1", Deps{Caller: fc, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil }, NewDot: newDot})
	m, _ = m.Update(SnapshotMsg(lib()))
	m = press(m, "n")
	if v := m.View(); !strings.Contains(v, "codex-cli") || !strings.Contains(v, "gpt-5") {
		t.Fatalf("editor should start from config defaults:\n%s", v)
	}
}

func TestDeleteNeedsConfirmation(t *testing.T) {
	m, fc, dir := setup(t, "w1:p1")
	d, _ := dots.FromTemplate("backend")
	_ = dots.Save(dir, d, true)
	m = press(m, "D")
	if !strings.Contains(m.View(), "Delete Backend?") {
		t.Fatalf("confirm:\n%s", m.View())
	}
	press(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "backend.toml")); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
	if !strings.HasPrefix(fc.calls[len(fc.calls)-1], ipc.CmdReloadDots) {
		t.Fatal("reload after delete")
	}
}

func TestSaveSnapshotsFormBeforeCmdRuns(t *testing.T) {
	m, _, dir := setup(t, "w1:p1")
	m = press(pickTemplate(t, m, "Copywriter"), "enter") // Copywriter in the editor
	m, cmd := m.Update(key("ctrl+s"))
	if cmd == nil || !strings.Contains(m.View(), "saving") {
		t.Fatalf("want a save cmd and a saving marker:\n%s", m.View())
	}
	// The user keeps typing before the cmd runs: the form must not change.
	before := m.View()
	m, _ = m.Update(key("X"))
	m, _ = m.Update(key("tab"))
	if m.View() != before {
		t.Fatal("form must ignore edits while saving")
	}
	msg := cmd()
	if _, ok := msg.(savedMsg); !ok {
		t.Fatalf("want savedMsg, got %#v", msg)
	}
	d, err := dots.Load(dir, "copywriter")
	if err != nil || strings.Contains(d.Name, "X") {
		t.Fatalf("saved value must be the one at ctrl+s: %+v %v", d, err)
	}
	m, _ = m.Update(msg)
	if strings.Contains(m.View(), "saving") {
		t.Fatal("saving marker should clear")
	}
}

func TestSaveErrorReenablesEditing(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	m = press(m, "n", "ctrl+s") // blank name fails validation in the cmd
	if !strings.Contains(m.View(), "name") && !strings.Contains(strings.ToLower(m.View()), "invalid") {
		t.Logf("view:\n%s", m.View())
	}
	if strings.Contains(m.View(), "saving") {
		t.Fatalf("saving must clear after an error:\n%s", m.View())
	}
	m = press(m, "Z")
	if !strings.Contains(m.View(), "Z") {
		t.Fatal("typing should work again after an error")
	}
}

func TestDeleteTargetsTheDotShownInTheConfirm(t *testing.T) {
	m, _, dir := setup(t, "w1:p1")
	for _, n := range []string{"backend", "security"} {
		d, _ := dots.FromTemplate(n)
		_ = dots.Save(dir, d, true)
	}
	m = press(m, "D") // confirm for Backend
	// a fresh snapshot reorders the library under the open confirm
	s := lib()
	s.Library[0], s.Library[1] = s.Library[1], s.Library[0]
	m, _ = m.Update(SnapshotMsg(s))
	press(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "backend.toml")); !os.IsNotExist(err) {
		t.Fatal("Backend (the dot shown in the prompt) should be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "security.toml")); err != nil {
		t.Fatal("Security must survive")
	}
}

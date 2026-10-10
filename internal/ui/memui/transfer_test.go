package memui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/memory"
	"herdr-dots/internal/platform/platformtest"
)

var everyStatus = []memory.Status{memory.StatusActive, memory.StatusProposed, memory.StatusArchived}

// openAt opens the memory screen with cwd as its working directory. A zero
// project opens a dot's global memory only.
func openAt(st *memory.Store, pr memory.Project, dot, cwd string) tea.Model {
	m := New(Deps{Store: st, Project: pr, Dot: dot, DotName: "Security", Cwd: cwd})
	return run(m, m.Init())
}

var herdrDots = memory.Project{ID: pid, Name: "herdr-dots"}

// exports lists the export files E wrote into dir.
func exports(t *testing.T, dir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(dir, "herdr-dots-memory-*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// typePath replaces the path input's text with p and presses enter.
func typePath(m tea.Model, p string) tea.Model {
	return press(m, "ctrl+u", p, "enter")
}

func status(m tea.Model) string { return m.(Model).status }

func sameEntries(t *testing.T, got, want []memory.Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d:\n%+v\n%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Ref() != w.Ref() || g.Text != w.Text || g.Kind != w.Kind || g.Status != w.Status || g.Dot != w.Dot || g.Pinned != w.Pinned {
			t.Fatalf("entry %d: got %+v, want %+v", i, g, w)
		}
	}
}

func TestExportWritesThePrivateFileOfTheView(t *testing.T) {
	st, cwd := seed(t), t.TempDir()
	m := press(openAt(st, herdrDots, "", cwd), "E")
	if !strings.Contains(m.View(), "herdr-dots-memory-herdr-dots-") {
		t.Fatalf("the path is prefilled with the project's name:\n%s", m.View())
	}
	m = press(m, "enter")
	files := exports(t, cwd)
	if len(files) != 1 {
		t.Fatalf("want one export in the working directory, got %v (status %q)", files, status(m))
	}
	platformtest.AssertPrivateFile(t, files[0])
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	back, err := memory.ImportTOML(data)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := st.List(pid, memory.Filter{Scope: memory.ScopeProject, Statuses: everyStatus})
	sameEntries(t, back, want)
	if s := status(m); !strings.Contains(s, "exported 2 entries to ") || !strings.Contains(s, filepath.Base(files[0])) {
		t.Fatalf("status %q", s)
	}

	// The same name again is refused, and the file is untouched.
	if _, err := st.Add(pid, memory.Entry{Scope: memory.ScopeProject, Kind: memory.KindNote, Text: "A third, later entry", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	m = press(m, "E", "enter")
	if s := status(m); !strings.Contains(s, "exists; pick another name") {
		t.Fatalf("an existing file must not be overwritten: %q", s)
	}
	if again, _ := os.ReadFile(files[0]); string(again) != string(data) {
		t.Fatal("the existing export was rewritten")
	}
}

// The all-scopes view exports every scope it lists: global plus this
// project's. A relative path resolves against the working directory.
func TestExportOfTheAllViewTakesEveryListedScope(t *testing.T) {
	st, cwd := seed(t), t.TempDir()
	if _, err := st.Add("", memory.Entry{Scope: memory.ScopeGlobal, Dot: "security", Kind: memory.KindPreference, Text: "Prefer Go for tooling", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	m := press(openAt(st, herdrDots, "security", cwd), "tab", "tab", "tab") // dot → project → global → all
	if m.(Model).view() != viewAll {
		t.Fatalf("view %s", m.(Model).view())
	}
	m = typePath(press(m, "E"), "all.toml")
	data, err := os.ReadFile(filepath.Join(cwd, "all.toml"))
	if err != nil {
		t.Fatalf("%v (status %q)", err, status(m))
	}
	back, _ := memory.ImportTOML(data)
	want, _ := st.List(pid, memory.Filter{Dot: "security", Statuses: everyStatus})
	sameEntries(t, back, want)
	if len(want) != 5 {
		t.Fatalf("the all view lists project, dot and global: %+v", want)
	}
}

const packAndNote = "version = 1\n" +
	"[[entry]]\nscope = \"project\"\nkind = \"convention\"\nstatus = \"proposed\"\nsource = \"user\"\nfrom = \"team\"\ntext = \"Every handler returns a typed error\"\n" +
	"[[entry]]\nscope = \"dot\"\ndot = \"security\"\nkind = \"note\"\nstatus = \"active\"\nsource = \"user\"\ntext = \"Rotate the signing keys quarterly\"\n"

func TestImportPreviewsThenYImportsThePackAndTheNote(t *testing.T) {
	st, cwd := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits()), t.TempDir()
	f := writeFile(t, cwd, "backup.toml", packAndNote)
	m := typePath(press(openAt(st, herdrDots, "", cwd), "I"), f)
	v := m.View()
	for _, want := range []string{`1 project proposal from pack "team"`, "1 active entry for security", "y imports · any other key cancels"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the preview must say %q:\n%s", want, v)
		}
	}
	if es, _ := st.List(pid, memory.Filter{Statuses: everyStatus}); len(es) != 0 {
		t.Fatalf("the preview wrote entries: %+v", es)
	}
	m = press(m, "y")
	if s := status(m); !strings.Contains(s, "imported 2 entries") {
		t.Fatalf("status %q", s)
	}
	proj, _ := st.List(pid, memory.Filter{Scope: memory.ScopeProject, Statuses: everyStatus})
	if len(proj) != 1 || proj[0].Status != memory.StatusProposed || proj[0].From != "team" || proj[0].Source != memory.SourceImport {
		t.Fatalf("the pack proposal stays proposed with From set: %+v", proj)
	}
	dot, _ := st.List(pid, memory.Filter{Scope: memory.ScopeDot, Dot: "security", Statuses: everyStatus})
	if len(dot) != 1 || dot[0].Source != memory.SourceImport || dot[0].Status != memory.StatusActive {
		t.Fatalf("the note is an import: %+v", dot)
	}
	if len(m.(Model).entries) != 1 {
		t.Fatalf("the list reloads after the import: %+v", m.(Model).entries)
	}
}

const secretPair = "version = 1\n" +
	"[[entry]]\nscope = \"dot\"\ndot = \"security\"\nkind = \"note\"\nstatus = \"active\"\ntext = \"Plain note about deploys\"\n" +
	"[[entry]]\nscope = \"dot\"\ndot = \"security\"\nkind = \"note\"\nstatus = \"active\"\ntext = \"the staging key is AKIAIOSFODNN7EXAMPLE\"\n" +
	"[[entry]]\nscope = \"dot\"\ndot = \"security\"\nkind = \"note\"\nstatus = \"active\"\npaths = [\"deploy/AKIAIOSFODNN7EXAMPLE/**\"]\ntext = \"Second plain note\"\n"

func TestImportOfSecretLookingEntriesNeedsCapitalY(t *testing.T) {
	st, cwd := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits()), t.TempDir()
	f := writeFile(t, cwd, "secret.toml", secretPair)
	m := typePath(press(openAt(st, herdrDots, "", cwd), "I"), f)
	v := m.View()
	if !strings.Contains(v, "2 entries look like secrets") || !strings.Contains(v, "entries 2, 3") || !strings.Contains(v, "Y imports") {
		t.Fatalf("the preview names the secret-looking entries by number:\n%s", v)
	}
	if strings.Contains(v, "AKIA") || strings.Contains(v, "staging key") {
		t.Fatalf("the preview shows entry text:\n%s", v)
	}
	m = press(m, "y")
	if es, _ := st.List(pid, memory.Filter{Statuses: everyStatus}); len(es) != 0 {
		t.Fatalf("y must not import secret-looking entries: %+v", es)
	}
	m = press(typePath(press(m, "I"), f), "Y")
	if es, _ := st.List(pid, memory.Filter{Statuses: everyStatus}); len(es) != 3 {
		t.Fatalf("Y imports them (status %q): %+v", status(m), es)
	}
}

func TestImportOfProjectMemoryWithoutAProjectIsRefused(t *testing.T) {
	root := t.TempDir()
	st, cwd := memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits()), t.TempDir()
	f := writeFile(t, cwd, "backup.toml", packAndNote)
	m := typePath(press(openAt(st, memory.Project{}, "security", cwd), "I"), f)
	if s := status(m); !strings.Contains(s, "no project") {
		t.Fatalf("status %q", s)
	}
	press(m, "y", "Y")
	if es, _ := st.List("", memory.Filter{Scope: memory.ScopeGlobal, Statuses: everyStatus}); len(es) != 0 {
		t.Fatalf("%+v", es)
	}
	if ps, _ := st.Projects(); len(ps) != 0 {
		t.Fatalf("a refused import wrote a project: %+v", ps)
	}
}

func TestPurgeNeedsTheExactProjectIDAndKeepsGlobalMemory(t *testing.T) {
	st, cwd := seed(t), t.TempDir()
	if _, err := st.Add("", memory.Entry{Scope: memory.ScopeGlobal, Dot: "security", Kind: memory.KindPreference, Text: "Prefer Go for tooling", Source: memory.SourceUser}); err != nil {
		t.Fatal(err)
	}
	m := press(openAt(st, herdrDots, "", cwd), "P")
	v := m.View()
	for _, want := range []string{"herdr-dots", pid, "4 entries", "Global memory is kept"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the purge screen must say %q:\n%s", want, v)
		}
	}
	m = press(m, "herdr-dots", "enter") // the name is not the id
	if es, _ := st.List(pid, memory.Filter{Statuses: everyStatus}); len(es) != 5 {
		t.Fatalf("a wrong answer purged memory: %+v", es)
	}
	m = press(m, "P", pid, "enter")
	if s := status(m); s != "purged "+pid {
		t.Fatalf("status %q", s)
	}
	if es, _ := st.List(pid, memory.Filter{Scope: memory.ScopeProject, Statuses: everyStatus}); len(es) != 0 {
		t.Fatalf("project memory survived: %+v", es)
	}
	if es, _ := st.List(pid, memory.Filter{Scope: memory.ScopeDot, Statuses: everyStatus}); len(es) != 0 {
		t.Fatalf("dot memory of the project survived: %+v", es)
	}
	if es, _ := st.List("", memory.Filter{Scope: memory.ScopeGlobal, Statuses: everyStatus}); len(es) != 1 {
		t.Fatalf("global memory must survive a purge: %+v", es)
	}
}

func TestPurgeWithoutAProjectIsRefused(t *testing.T) {
	st := seed(t)
	m := press(openAt(st, memory.Project{}, "security", t.TempDir()), "P")
	if s := status(m); s != "no project on this screen" {
		t.Fatalf("status %q", s)
	}
	if m.(Model).mode != modeBrowse {
		t.Fatal("no purge screen opens without a project")
	}
}

// The help line names E, I and P, and in a popup narrower than the line it
// breaks between keys instead of losing them on the right; the export screen
// shows the whole path it will write before enter.
func TestHelpAndExportPathAreNotCutOnTheRight(t *testing.T) {
	st, cwd := seed(t), t.TempDir()
	m, _ := openAt(st, herdrDots, "", cwd).Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	assertFits := func(v string) { // every line under the title line
		t.Helper()
		for _, l := range strings.Split(v, "\n")[1:] {
			if w := ansi.StringWidth(l); w > 60 {
				t.Fatalf("line of %d cells at width 60: %q\n%s", w, l, v)
			}
		}
	}
	v := m.View()
	assertFits(v)
	for _, want := range []string{"E export", "I import", "P purge", "esc back"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the help line must name %q:\n%s", want, v)
		}
	}
	long := filepath.Join(cwd, strings.Repeat("a-rather-long-directory-name/", 3), "out.toml")
	v = press(m, "E", "ctrl+u", long).View()
	screen, _, ok := strings.Cut(v, "enter writes") // the screen above its prompt, as for approve
	assertFits(screen)
	if !ok || !strings.Contains(noSpace(ansi.Strip(screen)), noSpace(long)) {
		t.Fatalf("the export screen must show the whole path:\n%s", v)
	}
}

// Every new screen obeys Task 10's width rule: in a popup too small to show
// it whole, it says "Too small" and its confirm key does nothing.
func TestNewScreensRefuseATooSmallPopup(t *testing.T) {
	small := tea.WindowSizeMsg{Width: 20, Height: 6}
	tiny := func(m tea.Model) tea.Model { m, _ = m.Update(small); return m }
	wantTooSmall := func(t *testing.T, m tea.Model) {
		t.Helper()
		if v := m.View(); !strings.Contains(noSpace(v), "Toosmall") {
			t.Fatalf("want Too small:\n%s", v)
		}
	}
	t.Run("export path", func(t *testing.T) {
		st, cwd := seed(t), t.TempDir()
		m := press(tiny(openAt(st, herdrDots, "", cwd)), "E")
		wantTooSmall(t, m)
		press(m, "enter")
		if files := exports(t, cwd); len(files) != 0 {
			t.Fatalf("a too-small export screen wrote %v", files)
		}
	})
	t.Run("import path", func(t *testing.T) {
		st, cwd := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits()), t.TempDir()
		f := writeFile(t, cwd, "b.toml", packAndNote)
		m := press(tiny(openAt(st, herdrDots, "", cwd)), "I")
		wantTooSmall(t, m)
		m = typePath(m, f)
		if m.(Model).mode == modeImportPreview {
			t.Fatal("enter on a too-small import screen opened the preview")
		}
	})
	t.Run("import preview", func(t *testing.T) {
		st, cwd := memory.Open(filepath.Join(t.TempDir(), "memory"), memory.DefaultLimits()), t.TempDir()
		f := writeFile(t, cwd, "b.toml", packAndNote)
		m := tiny(typePath(press(openAt(st, herdrDots, "", cwd), "I"), f))
		wantTooSmall(t, m)
		press(m, "y")
		if es, _ := st.List(pid, memory.Filter{Statuses: everyStatus}); len(es) != 0 {
			t.Fatalf("y on a too-small preview imported: %+v", es)
		}
	})
	t.Run("purge", func(t *testing.T) {
		st := seed(t)
		m := tiny(press(openAt(st, herdrDots, "", t.TempDir()), "P"))
		wantTooSmall(t, m)
		press(m, pid, "enter")
		if es, _ := st.List(pid, memory.Filter{Statuses: everyStatus}); len(es) != 4 {
			t.Fatalf("enter on a too-small purge screen purged: %+v", es)
		}
	})
}

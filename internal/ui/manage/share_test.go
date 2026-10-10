package manage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

const shareHelperTOML = "name = \"Helper\"\nglyph = \"h\"\npurpose = \"\"\"\nBe helpful.\nSecond line of the purpose.\"\"\"\n[delivery]\nmode = \"inject\"\n"

const shareHelperRel = ".herdr-dots/dots/helper.toml"

// shareLib is lib() plus a project dot of the target's checkout, the same
// dot in another checkout of the same repository (P9-R13), one of another
// repository, and origins on the user dots. It returns the snapshot, the
// helper file's absolute path and the target's checkout root.
func shareLib(t *testing.T) (view.Snapshot, string, string) {
	t.Helper()
	return shareLibWith(t, shareHelperTOML)
}

func shareLibWith(t *testing.T, body string) (view.Snapshot, string, string) {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, ".herdr-dots", "dots")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "helper.toml")
	os.WriteFile(path, []byte(body), 0o644)
	other := t.TempDir()
	s := lib()
	s.Library[0].Origin = dots.OriginImport
	s.Library[1].Origin = dots.OriginTemplate
	s.Library = append(s.Library,
		view.LibraryDot{Slug: "proj--helper--9f2c1a7b", Name: "Helper", Glyph: "h", Backend: "claude-cli", Model: "sonnet", Mode: "queue",
			Origin: dots.OriginProject, Requested: []string{"inject", "mcp"}, Clamped: []string{"inject", "mcp"},
			ProjectID: "g-9f2c1a7b00112233", ProjectName: "app", Path: path, SHA256: projectdots.Hex(projectdots.Sum([]byte(body))),
			Rel: shareHelperRel, Root: repo},
		view.LibraryDot{Slug: "proj--helper--9f2c1a7b", Name: "Helper", Glyph: "h", Backend: "claude-cli", Model: "sonnet", Mode: "queue",
			Origin: dots.OriginProject, Trusted: true, ProjectID: "g-9f2c1a7b00112233", ProjectName: "app",
			Path: filepath.Join(other, ".herdr-dots", "dots", "helper.toml"), Rel: shareHelperRel, Root: other},
		view.LibraryDot{Slug: "proj--other--00000000", Name: "Elsewhere", Glyph: "o", Origin: dots.OriginProject, ProjectID: "g-0000000011112222",
			Rel: ".herdr-dots/dots/other.toml", Root: t.TempDir()},
	)
	s.PaneProjects = map[string]string{"w1:p1": "g-9f2c1a7b00112233"}
	s.PaneRoots = map[string]string{"w1:p1": repo}
	return s, path, repo
}

func TestLibraryShowsOriginsAndOnlyThisCheckoutsProjectDots(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, path, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	v := m.View()
	for _, want := range []string{"Backend", "imported", "template-derived", "Helper", "⚠ project · untrusted", "i profile · I import · T trust"} {
		if !strings.Contains(v, want) {
			t.Errorf("library missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Elsewhere") {
		t.Fatalf("another repository's project dot is listed:\n%s", v)
	}
	if n := strings.Count(v, "Helper"); n != 1 {
		t.Fatalf("another checkout's copy is listed (%d Helper rows):\n%s", n, v)
	}
	if strings.Contains(v, path) {
		t.Fatalf("the absolute path is shown:\n%s", v)
	}
	m = press(m, "j", "j", "space")
	if !strings.Contains(m.View(), "untrusted project dot: press T") {
		t.Fatalf("space on an untrusted dot:\n%s", m.View())
	}
	press(m, "enter")
	for _, c := range fc.calls {
		if strings.Contains(c, "proj--helper") {
			t.Fatalf("an untrusted dot was attached: %v", fc.calls)
		}
	}
}

// A selection made while the dot was trusted is not attached once the
// snapshot says it is untrusted (its file changed).
func TestApplyNeverAttachesAnUntrustedProjectDot(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, _, _ := shareLib(t)
	s.Library[2].Trusted = true
	m, _ = m.Update(SnapshotMsg(s))
	m = press(m, "j", "j", "space")
	s.Library[2].Trusted = false
	m, _ = m.Update(SnapshotMsg(s))
	press(m, "enter")
	for _, c := range fc.calls {
		if strings.Contains(c, "proj--helper") {
			t.Fatalf("an untrusted dot was attached: %v", fc.calls)
		}
	}
}

func TestInitScansTheTargetsRepository(t *testing.T) {
	fc := &fakeCaller{}
	m := New("w1:p1", Deps{Caller: fc, DotsDir: t.TempDir()})
	m.Init()()
	if len(fc.calls) != 1 || fc.calls[0] != ipc.CmdReloadDots+` {"pane_id":"w1:p1"}` {
		t.Fatalf("calls %v", fc.calls)
	}
	if New("", Deps{Caller: fc}).Init() != nil {
		t.Fatal("no target, no scan")
	}
}

// Review Focus 1: the trust preview lists what the file asks for, each off,
// and needs the whole file seen before y.
func TestTrustPreviewNeedsTheWholeFileAndSendsTheHash(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, path, repo := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 8}) // a short popup: the preview scrolls
	m = press(m, "j", "j", "T")
	v := m.View()
	if !strings.Contains(v, "TRUST proj:helper") {
		t.Fatalf("trust preview:\n%s", v)
	}
	if !strings.Contains(v, "grants: nothing") {
		t.Fatalf("the footer must say what y grants:\n%s", v)
	}
	fc.calls = nil
	m = press(m, "y")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "scroll to the end") {
		t.Fatalf("y before the end: %v\n%s", fc.calls, m.View())
	}
	m = press(m, "space", "space", "space", "space", "space", "space", "space", "space", "space", "1") // read to the end, keep inject
	if !strings.Contains(m.View(), "grants: inject") {
		t.Fatalf("a toggle off screen must show in the footer:\n%s", m.View())
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 60})
	full := m.View()
	for _, want := range []string{"Be helpful.", "Second line of the purpose.", "[x] 1 inject: delivery.mode inject → queue", "[ ] 2 mcp",
		"file:     " + shareHelperRel + " in app", "THE FILE AS WRITTEN", `  mode = "inject"`, "untrusted again",
		"backend:  claude-cli · model sonnet", "budget:"} {
		if !strings.Contains(full, want) {
			t.Errorf("trust preview missing %q:\n%s", want, full)
		}
	}
	if strings.Contains(full, path) {
		t.Errorf("the absolute path is shown:\n%s", full)
	}
	m = press(m, "y")
	sum := projectdots.Hex(projectdots.Sum([]byte(shareHelperTOML)))
	want := ipc.CmdTrust + `{"pane_id":"w1:p1","root":` + jsonString(t, repo) + `,"slug":"proj--helper--9f2c1a7b","sha256":"` + sum + `","capabilities":["inject"]}`
	if len(fc.calls) != 1 || strings.ReplaceAll(fc.calls[0], " ", "") != strings.ReplaceAll(want, " ", "") {
		t.Fatalf("trust call:\n%v\nwant\n%s", fc.calls, want)
	}
	if !strings.Contains(m.View(), "trusted Helper") {
		t.Fatalf("status:\n%s", m.View())
	}
}

// Every byte trusted is shown: characters a terminal would hide or reorder
// appear as visible escapes, and the hash sent is of the bytes read.
func TestTrustPreviewShowsHiddenCharactersAndHashesTheBytesRead(t *testing.T) {
	body := "# reviewed \u202eevil\u202c ok\u200b\r\n" + shareHelperTOML
	m, fc, _ := setup(t, "w1:p1")
	s, _, repo := shareLibWith(t, body)
	m, _ = m.Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m = press(m, "j", "j", "T")
	v := m.View()
	if !strings.Contains(v, "# reviewed <U+202E>evil<U+202C> ok<U+200B>") {
		t.Fatalf("hidden characters not shown:\n%s", v)
	}
	if strings.ContainsRune(v, '\u202e') || strings.ContainsRune(v, '\r') {
		t.Fatalf("a raw control reached the terminal: %q", v)
	}
	fc.calls = nil
	press(m, "y")
	sum := projectdots.Hex(projectdots.Sum([]byte(body)))
	if len(fc.calls) != 1 || !strings.Contains(fc.calls[0], sum) || !strings.Contains(fc.calls[0], `"root":`+jsonString(t, repo)) {
		t.Fatalf("trust call %v, want sum %s", fc.calls, sum)
	}
}

// A snapshot that shows other bytes for the file under review warns that
// the preview is stale (the engine refuses a trust of it anyway).
func TestTrustPreviewWarnsWhenTheFileChangesDuringReview(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	s, _, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m = press(m, "j", "j", "T")
	if strings.Contains(m.View(), "differs from what the engine last read") {
		t.Fatalf("warned before any change:\n%s", m.View())
	}
	s2, _, _ := shareLib(t)
	s2.Library[2].SHA256, s2.Library[2].Root, s2.PaneRoots = strings.Repeat("0", 64), s.Library[2].Root, s.PaneRoots
	m, _ = m.Update(SnapshotMsg(s2))
	if !strings.Contains(m.View(), "differs from what the engine last read") {
		t.Fatalf("no warning after the file changed:\n%s", m.View())
	}
}

func TestTrustedElsewhereAndUntrustSendsTheCheckout(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, _, repo := shareLib(t)
	s.Library[2].Trusted, s.Library[2].TrustedAt, s.Library[2].Clamped = true, "~/src/app-main", []string{"mcp"}
	m, _ = m.Update(SnapshotMsg(s))
	if v := m.View(); !strings.Contains(v, "project · mcp off · trusted at ~/src/app-main") {
		t.Fatalf("badge:\n%s", v)
	}
	m = press(m, "j", "j", "T")
	if v := m.View(); !strings.Contains(v, "trusted at ~/src/app-main") || !strings.Contains(v, "u untrust") {
		t.Fatalf("trust preview of a trusted dot:\n%s", v)
	}
	fc.calls = nil
	m = press(m, "u")
	want := ipc.CmdUntrust + ` {"pane_id":"w1:p1","root":` + jsonString(t, repo) + `,"slug":"proj--helper--9f2c1a7b","sha256":""}`
	if len(fc.calls) != 1 || fc.calls[0] != want {
		t.Fatalf("untrust call:\n%v\nwant\n%s", fc.calls, want)
	}
	if !strings.Contains(m.View(), "untrusted Helper") {
		t.Fatalf("status:\n%s", m.View())
	}
}

// jsonString is s as a JSON string, quotes included: a Windows path's
// backslashes are escaped in the call's arguments (C:\\Users\\...).
func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProjectDotsCantBeEditedOrDeletedHere(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	s, path, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m = press(m, "j", "j", "e")
	if v := m.View(); !strings.Contains(v, "edited in their repository (app): "+shareHelperRel) || strings.Contains(v, path) {
		t.Fatalf("e:\n%s", v)
	}
	m = press(m, "D")
	if strings.Contains(m.View(), "Delete Helper?") {
		t.Fatal("D must not delete a project dot")
	}
}

// sharePress is press for screens that batch commands (the profile loads
// three things at once): it runs every command of a tea.BatchMsg.
func sharePress(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		queue := []tea.Cmd{cmd}
		for len(queue) > 0 {
			c := queue[0]
			queue = queue[1:]
			if c == nil {
				continue
			}
			switch msg := c().(type) {
			case nil, tea.QuitMsg:
			case tea.BatchMsg:
				queue = append(queue, msg...)
			default:
				var next tea.Cmd
				m, next = m.Update(msg)
				queue = append(queue, next)
			}
		}
	}
	return m
}

func shareDeps(t *testing.T, fc *fakeCaller) (Deps, *history.Store, *memory.Store) {
	t.Helper()
	root := t.TempDir()
	hs := history.Open(filepath.Join(root, "history"))
	st := memory.Open(filepath.Join(root, "memory"), memory.DefaultLimits())
	return Deps{Caller: fc, DotsDir: filepath.Join(root, "dots"), Check: func(context.Context, string) error { return nil },
		History: hs, Memory: st, SessionKey: "sess", ExportDir: root,
		Project: func(string) (memory.Project, error) {
			return memory.Project{ID: "g-0123456789abcdef", Name: "app"}, nil
		}}, hs, st
}

func TestProfileOpensFromTheLibraryAndExportsAPack(t *testing.T) {
	fc := &fakeCaller{}
	d, hs, st := shareDeps(t, fc)
	sec := dots.New("Security")
	sec.Purpose = "Review security."
	if err := dots.Save(d.DotsDir, sec, true); err != nil {
		t.Fatal(err)
	}
	hs.Append(history.Record{T: history.TypeRun, At: time.Now().Add(-time.Hour), Dot: "security", Kind: history.KindReview, Outcome: state.OutcomeFlagged, Card: "sess/1", Excerpt: "CSRF on /admin."})
	st.Add("g-0123456789abcdef", memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Prefer zod.", Source: memory.SourceModel})
	snap := view.Snapshot{Library: []view.LibraryDot{{Slug: "security", Name: "Security", Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Mode: "queue"}}}
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(snap))
	m = sharePress(m, "i")
	if v := m.View(); !strings.Contains(v, "LAST 30 DAYS") || !strings.Contains(v, "CSRF on /admin.") {
		t.Fatalf("profile:\n%s", v)
	}
	m = press(m, "x")
	if !strings.Contains(m.View(), "EXPORT Security") {
		t.Fatalf("export screen:\n%s", m.View())
	}
	m = press(m, "tab", "enter") // memory: dot
	out := filepath.Join(filepath.Dir(d.DotsDir), "security"+dotpack.Ext)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("export not written: %v\n%s", err, m.View())
	}
	if p, err := dotpack.Parse(data); err != nil || p.Dot.Name != "Security" || len(p.Memory) != 1 {
		t.Fatalf("pack %+v %v", p, err)
	}
	if !strings.Contains(m.View(), "exported to") || !strings.Contains(m.View(), "LAST 30 DAYS") {
		t.Fatalf("status (back on the profile):\n%s", m.View())
	}
	m = press(m, "x", "enter")
	if !strings.Contains(m.View(), "exists") {
		t.Fatalf("an export never overwrites:\n%s", m.View())
	}
}

// A memory entry that looks like a secret is named, never shown; ctrl+e
// leaves it out and says how many were left out.
func TestExportNamesSecretsAndForceDropsThem(t *testing.T) {
	fc := &fakeCaller{}
	d, _, st := shareDeps(t, fc)
	sec := dots.New("Security")
	sec.Purpose = "Review security."
	dots.Save(d.DotsDir, sec, true)
	const secret = "AKIAIOSFODNN7EXAMPLE"
	st.Add("g-0123456789abcdef", memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "Prefer zod.", Source: memory.SourceModel})
	if _, err := st.Add("g-0123456789abcdef", memory.Entry{Scope: memory.ScopeDot, Dot: "security", Kind: memory.KindConvention, Text: "The deploy key is " + secret, Source: memory.SourceModel}); err != nil {
		t.Fatal(err)
	}
	snap := view.Snapshot{Library: []view.LibraryDot{{Slug: "security", Name: "Security", Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Mode: "queue"}}}
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(snap))
	m = sharePress(m, "i")
	m = press(m, "x", "tab", "enter")
	v := m.View()
	if !strings.Contains(v, "looks like a secret: memory") || strings.Contains(v, secret) {
		t.Fatalf("secret refusal:\n%s", v)
	}
	out := filepath.Join(filepath.Dir(d.DotsDir), "security"+dotpack.Ext)
	if _, err := os.Stat(out); err == nil {
		t.Fatal("a refused export was written")
	}
	m = press(m, "ctrl+e")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("forced export not written: %v\n%s", err, m.View())
	}
	if p, err := dotpack.Parse(data); err != nil || len(p.Memory) != 1 || strings.Contains(string(data), secret) {
		t.Fatalf("forced pack %+v %v", p, err)
	}
	if v := m.View(); !strings.Contains(v, "1 memory entries") || !strings.Contains(v, "1 left out") {
		t.Fatalf("status:\n%s", v)
	}
}

// Review Focus 4: importing "Security" when one exists asks for a new name.
func TestImportPreviewCollisionRenameAndClamps(t *testing.T) {
	fc := &fakeCaller{}
	d, _, st := shareDeps(t, fc)
	sec := dots.New("Security")
	sec.Purpose = "Mine."
	dots.Save(d.DotsDir, sec, true)
	theirs := dots.New("Security")
	theirs.Purpose = "Theirs: review security."
	theirs.Delivery.Mode = dots.ModeInject
	data, _ := dotpack.Export(theirs, []memory.Entry{{Scope: memory.ScopeProject, Kind: memory.KindDecision, Text: "Sessions live in Redis."}}, dotpack.ExportOptions{Note: "team pack", Version: "0.9.0"})
	pack := filepath.Join(t.TempDir(), "sec"+dotpack.Ext)
	os.WriteFile(pack, data, 0o600)

	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{}))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 400, Height: 60}) // wide: the temp pack path stays on one line
	m = press(m, "I")
	for _, r := range pack {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = press(m, "enter")
	v := m.View()
	for _, want := range []string{"IMPORT Security from " + shortHome(pack), "note:     team pack", "by herdr-dots 0.9.0", "a dot named \"Security\" exists",
		"Theirs: review security.", "[ ] 1 inject: delivery.mode inject → queue", "Sessions live in Redis.", "arrive as proposals"} {
		if !strings.Contains(v, want) {
			t.Errorf("preview missing %q:\n%s", want, v)
		}
	}
	m = press(m, "y")
	if !strings.Contains(m.View(), "name is taken") {
		t.Fatalf("y over a taken name:\n%s", m.View())
	}
	m = press(m, "n", "ctrl+u")
	for _, r := range "Team Security" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = press(m, "enter", "y")
	got, err := dots.Load(d.DotsDir, "team-security")
	if err != nil || got.Origin != dots.OriginImport || got.Delivery.Mode != dots.ModeQueue || got.Purpose != "Theirs: review security." {
		t.Fatalf("imported %+v %v\n%s", got, err, m.View())
	}
	if mine, _ := dots.Load(d.DotsDir, "security"); mine.Purpose != "Mine." {
		t.Fatal("the existing dot was overwritten")
	}
	es, _ := st.List("g-0123456789abcdef", memory.Filter{Scope: memory.ScopeProject})
	if len(es) != 1 || es[0].Status != memory.StatusProposed || es[0].Source != memory.SourceImport {
		t.Fatalf("memory %+v", es)
	}
	if !strings.Contains(m.View(), "imported Team Security as team-security") || !strings.HasPrefix(fc.calls[len(fc.calls)-1], ipc.CmdReloadDots) {
		t.Fatalf("status %q calls %v", m.View(), fc.calls)
	}
	if !m.(Model).selected["team-security"] {
		t.Fatal("a dot imported with a target pane is pre-checked, like a new dot")
	}
}

// Fix round 1: before the first WindowSizeMsg the popup does not know how
// much of a preview is on screen, so nothing counts as seen.
func TestTrustAndImportWaitForTheWindowSize(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, _, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m = press(m, "j", "j", "T")
	fc.calls = nil
	m = press(m, "y")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "waiting for the window size") {
		t.Fatalf("y with no window size: %v\n%s", fc.calls, m.View())
	}

	d, _, _ := shareDeps(t, &fakeCaller{})
	small := dots.New("Small")
	small.Purpose = "Short."
	data, err := dotpack.Export(small, nil, dotpack.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(t.TempDir(), "s"+dotpack.Ext)
	os.WriteFile(pack, data, 0o600)
	var im tea.Model = New("w1:p1", d)
	im, _ = im.Update(SnapshotMsg(view.Snapshot{}))
	im = press(im, "I")
	for _, r := range pack {
		im, _ = im.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	im = press(im, "enter", "y")
	if _, err := dots.Load(d.DotsDir, "small"); err == nil || !strings.Contains(im.View(), "waiting for the window size") {
		t.Fatalf("imported with no window size (%v):\n%s", err, im.View())
	}
}

// Fix round 1: jumping to the end skips the middle, so it never counts as
// having read the file.
func TestJumpingToTheEndIsNotReading(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	s, _, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = press(m, "j", "j", "T")
	fc.calls = nil
	m = press(m, "G", "end", "y")
	if len(fc.calls) != 0 || !strings.Contains(m.View(), "scroll to the end") {
		t.Fatalf("G then y: %v\n%s", fc.calls, m.View())
	}
}

// Fix round 1: the profile stays sized while export covers it, and errors
// reach the status line while it is open.
func TestProfileKeepsItsSizeAndShowsErrors(t *testing.T) {
	fc := &fakeCaller{}
	d, hs, _ := shareDeps(t, fc)
	sec := dots.New("Security")
	sec.Purpose = "Review security."
	if err := dots.Save(d.DotsDir, sec, true); err != nil {
		t.Fatal(err)
	}
	hs.Append(history.Record{T: history.TypeRun, At: time.Now().Add(-time.Hour), Dot: "security", Kind: history.KindReview, Outcome: state.OutcomeFlagged, Card: "sess/1",
		Excerpt: "A long finding about cross-site request forgery on the admin endpoints that wraps."})
	snap := view.Snapshot{Library: []view.LibraryDot{{Slug: "security", Name: "Security", Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Mode: "queue"}}}
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(snap))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = sharePress(m, "i")
	if !strings.Contains(m.View(), "LAST 30 DAYS") {
		t.Fatalf("profile:\n%s", m.View())
	}
	m = press(m, "x")
	if !strings.Contains(m.View(), "EXPORT Security") {
		t.Fatalf("export:\n%s", m.View())
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 40})
	m = press(m, "esc")
	for _, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w > 30 {
			t.Fatalf("profile not resized while export was open (%d cells): %q", w, l)
		}
	}
	m, _ = m.Update(errMsg{errors.New("engine unreachable")})
	if !strings.Contains(m.View(), "engine unreachable") {
		t.Fatalf("error swallowed by the profile:\n%s", m.View())
	}
}

// Fix round 1: at a narrow width the untrusted badge stays; the name and
// model column is cut first.
func TestNarrowLibraryKeepsTheUntrustedBadge(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	s, _, _ := shareLib(t)
	m, _ = m.Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	v := m.View()
	if !strings.Contains(v, "⚠ project · untrusted") {
		t.Fatalf("badge cut at width 40:\n%s", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "untrusted") && ansi.StringWidth(l) > 40 {
			t.Fatalf("row wider than 40: %q", l)
		}
	}
}

// Fix round 1: an export is written whole or not at all, never over an
// existing file, and leaves no temporary file behind.
func TestWriteNewIsAtomicAndNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a"+dotpack.Ext)
	if err := writeNew(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writeNew(path, []byte("two")); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("second write: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "one" {
		t.Fatalf("replaced: %q", b)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("leftover files: %v", es)
	}
}

// Every prompt-reaching field is in the preview, and y waits for the end.
func TestImportPreviewShowsEveryPromptFieldAndNeedsTheEnd(t *testing.T) {
	fc := &fakeCaller{}
	d, _, _ := shareDeps(t, fc)
	theirs := dots.New("Researcher")
	theirs.Purpose = "Find things."
	theirs.Backend.Args = []string{"--effort", "high"}
	theirs.Research.Topics = []string{"Topic alpha", "Topic beta"}
	theirs.Schedule.Task = "Summarize the week."
	data, err := dotpack.Export(theirs, nil, dotpack.ExportOptions{Version: "0.9.0"})
	if err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(t.TempDir(), "r"+dotpack.Ext)
	os.WriteFile(pack, data, 0o600)
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{}))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = press(m, "I")
	for _, r := range pack {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = press(m, "enter", "y")
	if !strings.Contains(m.View(), "scroll to the end") {
		t.Fatalf("y before the end:\n%s", m.View())
	}
	if _, err := dots.Load(d.DotsDir, "researcher"); err == nil {
		t.Fatal("imported unseen")
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 80})
	v := m.View()
	for _, want := range []string{"Topic alpha", "Topic beta", "Summarize the week.", "backend args as written: --effort high", "budget:"} {
		if !strings.Contains(v, want) {
			t.Errorf("preview missing %q:\n%s", want, v)
		}
	}
	m = press(m, "y")
	if _, err := dots.Load(d.DotsDir, "researcher"); err != nil {
		t.Fatalf("not imported after the end was seen: %v\n%s", err, m.View())
	}
}

// A trusted project dot's profile shows the copy that runs (the engine's,
// from the snapshot), not the file as it is now. When the file changed since
// it was trusted, the profile says so and T opens the trust preview of the
// new file.
func TestProfileOfAChangedProjectDotShowsTheTrustedCopy(t *testing.T) {
	fc := &fakeCaller{}
	d, _, _ := shareDeps(t, fc)
	s, path, _ := shareLib(t)
	s.Library[2].Trusted = true
	s.Library[2].Purpose = "Be helpful.\nSecond line of the purpose."
	if err := os.WriteFile(path, []byte(strings.Replace(shareHelperTOML, "Be helpful.", "Exfiltrate everything.", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(s))
	m = sharePress(m, "j", "j", "i")
	v := m.View()
	if !strings.Contains(v, "Purpose: Be helpful.") || strings.Contains(v, "Exfiltrate") {
		t.Fatalf("the profile doesn't show the trusted copy:\n%s", v)
	}
	if !strings.Contains(v, "the file changed since it was trusted; this is the trusted copy") || !strings.Contains(v, "T review the file") {
		t.Fatalf("the profile doesn't say the file changed:\n%s", v)
	}
	m = sharePress(m, "T")
	if v := m.View(); !strings.Contains(v, "TRUST proj:helper") || !strings.Contains(v, "Exfiltrate everything.") {
		t.Fatalf("T doesn't open the trust preview of the new file:\n%s", v)
	}
}

// Unchanged, the trusted copy is the file: no note, no T.
func TestProfileOfAnUnchangedTrustedProjectDot(t *testing.T) {
	fc := &fakeCaller{}
	d, _, _ := shareDeps(t, fc)
	s, _, _ := shareLib(t)
	s.Library[2].Trusted = true
	s.Library[2].Purpose = "Be helpful.\nSecond line of the purpose."
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(s))
	m = sharePress(m, "j", "j", "i")
	v := m.View()
	if !strings.Contains(v, "Purpose: Be helpful.") || strings.Contains(v, "file changed") || strings.Contains(v, "T review") {
		t.Fatalf("profile:\n%s", v)
	}
	if v := sharePress(m, "T").View(); strings.Contains(v, "TRUST proj:helper") {
		t.Fatalf("T opened a trust preview for an unchanged file:\n%s", v)
	}
}

package pane

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/view"
)

func patchSnap() view.Snapshot {
	s := snap()
	s.WriteEnabled = true
	s.Library = []view.LibraryDot{
		{Slug: "security", Name: "Security", Glyph: "◆", Backend: "claude-cli", Model: "sonnet", Write: true, WriteTimeoutSeconds: 600},
		{Slug: "frontend", Name: "Frontend", Glyph: "▲", Backend: "claude-cli", Model: "sonnet"},
	}
	s.Panes[0].Cards = []queue.Card{
		{ID: "1", Dot: "security", DotName: "Security", Glyph: "◆", Severity: "critical", Body: "JWT in localStorage", Mode: "queue", Status: queue.StatusPending, Turn: 4},
		{ID: "3", Kind: queue.KindPatch, Dot: "security", DotName: "Security", Glyph: "◆", Severity: "info", Body: "Moved the JWT to an httpOnly cookie.",
			Mode: "queue", Status: queue.StatusPending, Turn: 31, PatchID: "0123456789abcdef", DiffStat: "3 files +42 −7", PatchFlags: []string{"binary"}},
	}
	return s
}

type viewerCall struct {
	target, patch, card string
	apply               bool
}

func patchReady(fc *fakeCaller, s view.Snapshot, opened *[]viewerCall) tea.Model {
	m := New("w1:p1", fc)
	if opened != nil {
		m = m.WithPatchViewer(func(target, patch, card string, apply bool) error {
			*opened = append(*opened, viewerCall{target, patch, card, apply})
			return nil
		})
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120}) // wide enough that no hint wraps
	mm, _ = mm.Update(SnapshotMsg(s))
	return mm
}

func TestDraftConfirmShowsEverythingBeforeItRuns(t *testing.T) {
	fc := &fakeCaller{}
	m := press(patchReady(fc, patchSnap(), nil), "w")
	v := m.View()
	for _, want := range []string{"Draft a fix with Security?", "claude-cli · sonnet · timeout 10m", "no shell", "isolated copy", "nothing changes in your files until you apply", "JWT in localStorage", "y draft"} {
		if !strings.Contains(v, want) {
			t.Errorf("confirm lacks %q:\n%s", want, v)
		}
	}
	if len(fc.calls) != 0 {
		t.Fatal("nothing runs before y")
	}
	press(m, "y")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdDraft || fc.calls[0].args != `{"pane_id":"w1:p1","slug":"security","instruction":"JWT in localStorage","from_card_id":"1"}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestDraftConfirmEditsTheInstructionAndCancels(t *testing.T) {
	fc := &fakeCaller{}
	m := press(patchReady(fc, patchSnap(), nil), "w", "e", " and add SameSite", "ctrl+s")
	if !strings.Contains(m.View(), "JWT in localStorage and add SameSite") {
		t.Fatalf("edited instruction not shown:\n%s", m.View())
	}
	if gone := press(m, "esc"); strings.Contains(gone.View(), "DRAFT A FIX") || len(fc.calls) != 0 {
		t.Fatalf("esc must close without a call: %+v", fc.calls)
	}
	press(m, "y")
	if len(fc.calls) != 1 || !strings.Contains(fc.calls[0].args, `"instruction":"JWT in localStorage and add SameSite"`) {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestDraftIsRefusedWithAReason(t *testing.T) {
	off := patchSnap()
	off.WriteEnabled = false
	if v := press(patchReady(&fakeCaller{}, off, nil), "w").View(); !strings.Contains(v, "write_enabled = true") {
		t.Errorf("write off:\n%s", v)
	}
	none := patchSnap()
	none.Library[0].Write = false
	if v := press(patchReady(&fakeCaller{}, none, nil), "w").View(); !strings.Contains(v, "no write-capable dot here") {
		t.Errorf("no write dot:\n%s", v)
	}
	if v := press(patchReady(&fakeCaller{}, patchSnap(), nil), "tab", "j", "w").View(); !strings.Contains(v, "Frontend can't draft fixes") {
		t.Errorf("roster, read-only dot:\n%s", v)
	}
	if v := press(patchReady(&fakeCaller{}, patchSnap(), nil), "j", "w").View(); !strings.Contains(v, "already a patch") {
		t.Errorf("w on a patch card:\n%s", v)
	}
	if v := press(patchReady(&fakeCaller{}, patchSnap(), nil), "tab", "w", "ctrl+s", "y").View(); !strings.Contains(v, "say what to fix first") {
		t.Errorf("roster draft without an instruction:\n%s", v)
	}
	d := press(patchReady(&fakeCaller{}, patchSnap(), nil), "w")
	d, _ = d.Update(DisconnectedMsg{})
	if v := press(d, "y").View(); !strings.Contains(v, offlineHint) {
		t.Errorf("offline:\n%s", v)
	}
}

func TestPatchCardRendersAndRoutesItsKeys(t *testing.T) {
	fc := &fakeCaller{}
	var opened []viewerCall
	m := press(patchReady(fc, patchSnap(), &opened), "j")
	v := m.View()
	for _, want := range []string{"PATCH", "Security · turn 31 · 3 files +42 −7", "Moved the JWT to an httpOnly cookie.", "[binary]", "v view  A apply  ⏎ send to agent  x discard"} {
		if !strings.Contains(v, want) {
			t.Errorf("patch card lacks %q:\n%s", want, v)
		}
	}
	press(m, "v")
	press(m, "A")
	want := []viewerCall{{"w1:p1", "0123456789abcdef", "3", false}, {"w1:p1", "0123456789abcdef", "3", true}}
	if len(opened) != 2 || opened[0] != want[0] || opened[1] != want[1] {
		t.Fatalf("viewer calls %+v", opened)
	}
	press(m, "x")
	press(m, "enter")
	if len(fc.calls) != 2 || fc.calls[0].cmd != ipc.CmdDiscardPatch || fc.calls[0].args != `{"id":"3"}` || fc.calls[1].cmd != ipc.CmdSend || fc.calls[1].args != `{"ids":["3"]}` {
		t.Fatalf("calls %+v", fc.calls)
	}
	if v := press(m, "e").View(); !strings.Contains(v, "can't be edited") {
		t.Errorf("e on a patch:\n%s", v)
	}
	if v := press(patchReady(&fakeCaller{}, patchSnap(), nil), "j", "A").View(); !strings.Contains(v, "Dots: patch pane") {
		t.Errorf("no viewer:\n%s", v)
	}
}

func TestSendAllSkipsPatchCards(t *testing.T) {
	fc := &fakeCaller{}
	press(patchReady(fc, patchSnap(), nil), "a")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"ids":["1"]}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestDraftProgressLineAndTick(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s := patchSnap()
	s.Panes[0].Drafts = []view.DraftState{{ID: "0123456789abcdef", Slug: "security", Phase: view.DraftRunning, StartedAt: now.Add(-134 * time.Second)}}
	m := New("w1:p1", &fakeCaller{})
	m.clock = func() time.Time { return now }
	mm, cmd := m.Update(SnapshotMsg(s))
	if cmd == nil {
		t.Fatal("a running draft starts the tick")
	}
	if v := mm.View(); !strings.Contains(v, "✎ Security drafting… 2:14") {
		t.Fatalf("progress line:\n%s", v)
	}
	if _, cmd := mm.Update(SnapshotMsg(s)); cmd != nil {
		t.Fatal("only one tick at a time")
	}
	s.Panes[0].Drafts = nil
	mm, _ = mm.Update(SnapshotMsg(s))
	if _, cmd := mm.Update(tickMsg(now)); cmd != nil {
		t.Fatal("the tick stops when no draft runs")
	}
}

func TestHelpMentionsDrafts(t *testing.T) {
	if v := press(patchReady(&fakeCaller{}, patchSnap(), nil), "?").View(); !strings.Contains(v, "w draft a fix") {
		t.Fatalf("help:\n%s", v)
	}
}

func TestPatchTextIsSanitizedAndEveryLineFitsTheWidth(t *testing.T) {
	s := patchSnap()
	s.Panes[0].Cards[1].Body = "Fixed\x1b]52;c;ZXZpbA==\x07 it " + strings.Repeat("long ", 40)
	s.Panes[0].Cards[1].Note = "note\x1b[2J " + strings.Repeat("n", 100)
	s.Panes[0].Cards[1].DiffStat = "3 files\x1b[31m"
	s.Panes[0].Cards[1].PatchFlags = []string{"bin\x1bary", strings.Repeat("f", 100)}
	m := patchReady(&fakeCaller{}, s, nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40})
	v := press(m, "j").View()
	if strings.Contains(v, "\x1b]") || strings.Contains(v, "\x1b[2J") || strings.Contains(v, "\a") {
		t.Fatalf("control sequences reached the screen:\n%q", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if n := lipgloss.Width(l); n > 40 {
			t.Errorf("line of width %d: %q", n, l)
		}
	}
}

func TestDraftConfirmLinesFitTheWidth(t *testing.T) {
	m := patchReady(&fakeCaller{}, patchSnap(), nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 30})
	for _, l := range strings.Split(press(m, "w").View(), "\n") {
		if n := lipgloss.Width(l); n > 30 {
			t.Errorf("line of width %d: %q", n, l)
		}
	}
}

func TestThePaneNeverAppliesAPatchItself(t *testing.T) {
	fc := &fakeCaller{}
	var opened []viewerCall
	press(patchReady(fc, patchSnap(), &opened), "j", "A", "y")
	for _, c := range fc.calls {
		if c.cmd == ipc.CmdApplyPatch {
			t.Fatalf("the pane must never apply: %+v", fc.calls)
		}
	}
}

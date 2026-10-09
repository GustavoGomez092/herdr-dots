package pane

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/view"
)

func councilSnapshot(cs *view.CouncilState) view.Snapshot {
	s := snap()
	s.Panes[0].Council = cs
	s.Panes[0].Cards = append([]queue.Card{{
		ID: "9", Kind: queue.KindCouncil, Dot: "council", DotName: "Council", Glyph: "⊕", Severity: "critical", Turn: 22,
		Mode: "queue", Status: queue.StatusPending, Question: "Ready to merge?",
		Body: "## Must fix\n- JWT in localStorage (Security, Frontend)", Note: "merged without chair (chair error: timed out)",
		Parts: []queue.CardPart{
			{Dot: "security", DotName: "Security", Glyph: "◆", Severity: "critical", Body: "JWT in localStorage.\n↳ OBJECT: cookies need CSRF.", Stance: "object"},
			{Dot: "frontend", DotName: "Frontend", Glyph: "▲"},
			{Dot: "copy", DotName: "Copy", Glyph: "✎", Body: "timed out", Failed: true},
		},
	}}, s.Panes[0].Cards...)
	return s
}

func councilReady(fc *fakeCaller, cs *view.CouncilState) tea.Model {
	m, _ := New("w1:p1", fc).Update(SnapshotMsg(councilSnapshot(cs)))
	return m
}

func TestCouncilCardHeaderAndExpand(t *testing.T) {
	m := councilReady(&fakeCaller{}, nil)
	v := m.View()
	for _, want := range []string{"⊕ CRITICAL  Council · turn 22 · 3 dots", "## Must fix", "⏎ send  e edit  x dismiss  space expand"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q\n%s", want, v)
		}
	}
	// The note wraps to the body width (final review item 2).
	if flat := strings.Join(strings.Fields(v), " "); !strings.Contains(flat, "merged without chair (chair error: timed out)") {
		t.Errorf("view missing the note\n%s", v)
	}
	if strings.Contains(v, "✗ timed out") {
		t.Fatal("parts stay hidden until expanded")
	}
	m = press(m, " ")
	v = m.View()
	for _, want := range []string{"asked: Ready to merge?", "◆ Security  CRITICAL · object", "↳ OBJECT: cookies need CSRF.", "▲ Frontend  nothing to add", "✎ Copy  ✗ timed out"} {
		if !strings.Contains(v, want) {
			t.Errorf("expanded view missing %q\n%s", want, v)
		}
	}
	if v := press(m, " ").View(); strings.Contains(v, "✗ timed out") {
		t.Fatal("space again collapses")
	}
}

func TestSpaceOnANormalCardDoesNothing(t *testing.T) {
	fc := &fakeCaller{}
	m := press(ready(fc), " ")
	if len(fc.calls) != 0 || strings.Contains(m.View(), "nothing to add") {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestCouncilCardSendsLikeAnyCard(t *testing.T) {
	fc := &fakeCaller{}
	press(councilReady(fc, nil), "enter")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdSend || fc.calls[0].args != `{"ids":["9"]}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestCouncilProgressLine(t *testing.T) {
	cases := map[string]view.CouncilState{
		"⊕ council: 1/3 members…":         {ID: "council-1", Phase: view.CouncilMembers, Done: 1, Total: 3, Round: 1},
		"⊕ council: round 2 2/3 members…": {ID: "council-1", Phase: view.CouncilRound2, Done: 2, Total: 3, Round: 2},
		"⊕ council: merging…":             {ID: "council-1", Phase: view.CouncilMerging, Round: 1},
	}
	for want, cs := range cases {
		cs := cs
		if v := councilReady(&fakeCaller{}, &cs).View(); !strings.Contains(v, want) {
			t.Errorf("missing %q\n%s", want, v)
		}
	}
	if v := councilReady(&fakeCaller{}, nil).View(); strings.Contains(v, "⊕ council:") {
		t.Fatal("no progress line when idle")
	}
}

func TestCKeyOpensTheCouncilPopup(t *testing.T) {
	var opened []string
	m, _ := New("w1:p1", &fakeCaller{}).WithOpener(func(target string) error {
		opened = append(opened, target)
		return nil
	}).Update(SnapshotMsg(snap()))
	press(m, "C")
	if len(opened) != 1 || opened[0] != "w1:p1" {
		t.Fatalf("opened %v", opened)
	}
	failing, _ := New("w1:p1", &fakeCaller{}).WithOpener(func(string) error { return errors.New("plugin_not_found") }).Update(SnapshotMsg(snap()))
	if v := press(failing, "C").View(); !strings.Contains(v, "plugin_not_found") {
		t.Fatal(v)
	}
	if v := press(ready(&fakeCaller{}), "C").View(); !strings.Contains(v, "herdr-dots.council action") {
		t.Fatalf("no opener: %s", v)
	}
	offline, _ := New("w1:p1", &fakeCaller{}).WithOpener(func(string) error { t.Fatal("must not open offline"); return nil }).Update(DisconnectedMsg{})
	if v := press(offline, "C").View(); !strings.Contains(v, offlineHint) {
		t.Fatal(v)
	}
}

func TestExpandedCouncilCardStaysWithinTheViewport(t *testing.T) {
	m, _ := councilReady(&fakeCaller{}, &view.CouncilState{ID: "council-1", Phase: view.CouncilMerging, Round: 1}).Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	v := press(m, " ").View()
	if n := strings.Count(v, "\n") + 1; n > 12 {
		t.Fatalf("%d lines in a 12-line pane\n%s", n, v)
	}
	for _, want := range []string{"⊕ CRITICAL  Council", "⊕ council: merging…"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q\n%s", want, v)
		}
	}
}

// Final review item 2: the footer note wraps like every other card line, so
// a long chair error reason is not cut off by the terminal.
func TestCouncilNoteWrapsToTheBodyWidth(t *testing.T) {
	s := councilSnapshot(nil)
	s.Panes[0].Cards[0].Note = "merged without chair (chair error: error: backend exited with status 1 after the upstream closed the connection reason-end)"
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 80})
	v := m.View()
	for _, line := range strings.Split(v, "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Fatalf("line of width %d in a 60-wide pane: %q\n%s", w, line, v)
		}
	}
	if !strings.Contains(v, "reason-end)") {
		t.Fatalf("the note's end is missing\n%s", v)
	}
}

// Final review item 5: a council card can only be remembered as a project
// convention, so the chooser offers c only and ignores f, r and g.
func TestRememberChooserOnACouncilCardOffersConventionOnly(t *testing.T) {
	fc := &fakeCaller{}
	m := press(councilReady(fc, nil), "M")
	v := m.View()
	if !strings.Contains(v, "c project convention · esc cancel") || strings.Contains(v, "f accepted finding") || strings.Contains(v, "r rule") {
		t.Fatalf("council chooser:\n%s", v)
	}
	for _, k := range []string{"f", "r", "g"} {
		m = press(m, k)
		if v := m.View(); len(fc.calls) != 0 || strings.Contains(v, "remembered as") || strings.Contains(v, "REMEMBER AS A RULE") {
			t.Fatalf("%s on a council card: calls %+v\n%s", k, fc.calls, v)
		}
	}
	m = press(m, "c")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdRemember || fc.calls[0].args != `{"card_id":"9","as":"convention"}` {
		t.Fatalf("calls %+v", fc.calls)
	}
	if !strings.Contains(m.View(), "remembered as convention") {
		t.Fatalf("status:\n%s", m.View())
	}
}

func TestHelpMentionsCouncil(t *testing.T) {
	if v := press(ready(&fakeCaller{}), "?").View(); !strings.Contains(v, "C council") {
		t.Fatal(v)
	}
}

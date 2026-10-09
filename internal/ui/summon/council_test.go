package summon

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/council"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

func ckey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// run sends keys and runs returned commands like the Bubble Tea runtime. It
// returns the last message a command produced.
func run(m tea.Model, keys ...string) (tea.Model, tea.Msg) {
	var last tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(ckey(k))
		if cmd == nil {
			continue
		}
		if msg := cmd(); msg != nil {
			last = msg
			if _, quit := msg.(tea.QuitMsg); !quit {
				m, _ = m.Update(msg)
			}
		}
	}
	return m, last
}

var trio = []view.DotState{
	{Slug: "security", Name: "Security", Glyph: "◆"},
	{Slug: "frontend", Name: "Frontend", Glyph: "▲"},
	{Slug: "copy", Name: "Copy", Glyph: "✎"},
}

func councilSnap(pf *council.Preflight) SnapshotMsg {
	return SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: trio, CouncilPreflight: pf}}})
}

func preflightFor(paneLeft int, members ...council.Candidate) *council.Preflight {
	p := council.Plan(council.PlanInput{Candidates: members, Rounds: 1, MaxMembers: 8, PaneLeft: paneLeft, Chair: true})
	return &p
}

var copyPaused = []council.Candidate{{Slug: "security", CallsLeft: 40}, {Slug: "frontend", CallsLeft: 40}, {Slug: "copy", Paused: true, CallsLeft: 40}}

func TestCouncilPopupAsksForAPreflightOnOpen(t *testing.T) {
	fc := &fakeCaller{}
	m := NewCouncil("w1:p1", fc)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init must request a preflight")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("unexpected message %v", msg)
	}
	if len(fc.calls) != 1 || fc.calls[0] != ipc.CmdCouncilPreflight+` {"pane_id":"w1:p1"}` {
		t.Fatalf("calls %v", fc.calls)
	}
}

func TestCouncilPopupWaitsForThePreflight(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(nil))
	if !strings.Contains(m.View(), "checking budgets…") {
		t.Fatal(m.View())
	}
	m, _ = run(m, "enter")
	if len(fc.calls) != 0 {
		t.Fatalf("enter before the preflight must do nothing: %v", fc.calls)
	}
}

func TestCouncilPopupPreselectsTogglesAndConvenes(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(50, copyPaused...)))
	v := m.View()
	for _, want := range []string{"COUNCIL → w1:p1", "[x] ◆ Security", "[x] ▲ Frontend", "[ ] ✎ Copy (paused)", "estimate: 3 calls (2 dots × 1 round + chair) · 50 left"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q\n%s", want, v)
		}
	}
	m, _ = run(m, "s", "h", "i", "p", "?") // the question has focus first
	m, _ = run(m, "tab", "space")          // focus the list, untick Security
	if v := m.View(); !strings.Contains(v, "[ ] ◆ Security") || !strings.Contains(v, "estimate: 1 call (1 dots × 1 round) · 50 left") {
		t.Fatalf("after toggling Security off:\n%s", v)
	}
	m, _ = run(m, "j", "j", "space") // Copy is paused: it can't be ticked
	if !strings.Contains(m.View(), "[ ] ✎ Copy (paused)") {
		t.Fatal(m.View())
	}
	_, last := run(m, "enter")
	if len(fc.calls) != 1 || fc.calls[0] != ipc.CmdCouncil+` {"pane_id":"w1:p1","question":"ship?","slugs":["frontend"]}` {
		t.Fatalf("calls %v", fc.calls)
	}
	if _, ok := last.(tea.QuitMsg); !ok {
		t.Fatalf("convening closes the popup, got %T", last)
	}
}

func TestCouncilPopupRefusesWhenTheBudgetIsShort(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(2, copyPaused...)))
	if !strings.Contains(m.View(), "pane budget too low: this council needs 3 calls, 2 left") {
		t.Fatal(m.View())
	}
	m, _ = run(m, "enter")
	if len(fc.calls) != 0 {
		t.Fatalf("a refused council must not be sent: %v", fc.calls)
	}
	m, _ = run(m, "tab", "space", "enter") // drop Security: 1 call fits
	if len(fc.calls) != 1 || !strings.Contains(fc.calls[0], `"slugs":["frontend"]`) {
		t.Fatalf("calls %v", fc.calls)
	}
}

func TestCouncilPopupWithNoEligibleDots(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(50, council.Candidate{Slug: "security", Paused: true}, council.Candidate{Slug: "frontend", CallsLeft: 0}, council.Candidate{Slug: "copy", Paused: true})))
	if v := m.View(); !strings.Contains(v, "no eligible dots (paused or over budget)") || !strings.Contains(v, "▲ Frontend (budget)") {
		t.Fatal(v)
	}
	m, _ = run(m, "enter")
	if len(fc.calls) != 0 {
		t.Fatalf("calls %v", fc.calls)
	}
}

func TestSummonListFirstEntryOpensTheCouncil(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: trio}}}))
	v := m.View()
	if !strings.Contains(v, "  ⊕ Council (all attached)") || !strings.Contains(v, "▸ ") || strings.Index(v, "Council") > strings.Index(v, "Security") {
		t.Fatalf("the council entry comes first and the cursor starts on the first dot:\n%s", v)
	}
	m, _ = run(m, "k")
	if !strings.Contains(m.View(), "▸ ⊕ Council (all attached)") {
		t.Fatal(m.View())
	}
	m, _ = run(m, "enter")
	if len(fc.calls) != 1 || !strings.HasPrefix(fc.calls[0], ipc.CmdCouncilPreflight) || !strings.Contains(m.View(), "COUNCIL → w1:p1") {
		t.Fatalf("calls %v\n%s", fc.calls, m.View())
	}
	m, _ = run(m, "esc")
	if !strings.Contains(m.View(), "SUMMON A DOT") {
		t.Fatalf("esc returns to the list:\n%s", m.View())
	}
}

func TestCouncilPopupShowsEngineErrors(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(50, copyPaused...)))
	fc.err = errString("council already running")
	m, last := run(m, "enter")
	if _, quit := last.(tea.QuitMsg); quit {
		t.Fatal("an error keeps the popup open")
	}
	if !strings.Contains(m.View(), "council already running") {
		t.Fatal(m.View())
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestCouncilPopupBlocksAZeroTickSelection(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(50, copyPaused...)))
	m, _ = run(m, "tab", "space", "j", "space") // untick Security and Frontend
	if !strings.Contains(m.View(), "select at least one dot") {
		t.Fatalf("a visible reason is expected:\n%s", m.View())
	}
	m, _ = run(m, "enter")
	if len(fc.calls) != 0 {
		t.Fatalf("a council with no ticked member must never be sent (nil slugs mean all): %v", fc.calls)
	}
	if !strings.Contains(m.View(), "select at least one dot") {
		t.Fatal(m.View())
	}
}

func manyDots(n int) []view.DotState {
	out := make([]view.DotState, n)
	for i := range out {
		out[i] = view.DotState{Slug: fmt.Sprintf("dot%02d", i), Name: fmt.Sprintf("Dot%02d", i), Glyph: "●"}
	}
	return out
}

func manySnap(n int) SnapshotMsg {
	ds := manyDots(n)
	cands := make([]council.Candidate, n)
	for i, d := range ds {
		cands[i] = council.Candidate{Slug: d.Slug, CallsLeft: 40}
	}
	p := council.Plan(council.PlanInput{Candidates: cands, Rounds: 1, MaxMembers: 30, PaneLeft: 500, Chair: true})
	return SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: ds, CouncilPreflight: &p}}})
}

func TestCouncilPopupKeepsItsFramingInAShortWindow(t *testing.T) {
	var m tea.Model = NewCouncil("w1:p1", &fakeCaller{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	m, _ = m.Update(manySnap(20))
	m, _ = run(m, "tab")
	for i := 0; i < 20; i++ {
		v := m.View()
		if n := strings.Count(v, "\n") + 1; n > 12 {
			t.Fatalf("step %d: %d lines in a 12-line window:\n%s", i, n, v)
		}
		for _, want := range []string{"COUNCIL → w1:p1", "▸ [x] ● " + fmt.Sprintf("Dot%02d", i), "estimate:", "esc cancel"} {
			if !strings.Contains(v, want) {
				t.Fatalf("step %d: missing %q:\n%s", i, want, v)
			}
		}
		m, _ = run(m, "j")
	}
	if v := m.View(); !strings.Contains(v, "↑") || strings.Contains(v, "↓") {
		t.Fatalf("at the end only the up marker shows:\n%s", v)
	}
}

func TestCouncilPopupNeverExceedsTheWidth(t *testing.T) {
	var m tea.Model = NewCouncil("w1:p1", &fakeCaller{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m, _ = m.Update(councilSnap(preflightFor(50, copyPaused...)))
	m, _ = run(m, strings.Repeat("x", 300))
	for _, line := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Fatalf("line of width %d: %q", w, line)
		}
	}
}

func TestSummonListKeepsTheCursorVisibleInAShortWindow(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: manyDots(20)}}}))
	for i := 0; i < 19; i++ {
		m, _ = run(m, "j")
	}
	v := m.View()
	if n := strings.Count(v, "\n") + 1; n > 10 || !strings.Contains(v, "SUMMON A DOT") || !strings.Contains(v, "▸ ● Dot19") {
		t.Fatalf("%d lines:\n%s", n, v)
	}
}

func TestCouncilPopupDropsStaleStatus(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(2, copyPaused...)))
	m, _ = run(m, "enter")
	if !strings.Contains(m.View(), "pane budget too low") {
		t.Fatal(m.View())
	}
	m, _ = run(m, "tab", "space") // drop Security: the refusal lifts
	if strings.Contains(m.View(), "pane budget too low") {
		t.Fatalf("stale refusal:\n%s", m.View())
	}
	fc.err = errString("council already running")
	m, _ = run(m, "enter")
	if !strings.Contains(m.View(), "council already running") {
		t.Fatal(m.View())
	}
	m, _ = run(m, "j")
	if strings.Contains(m.View(), "council already running") {
		t.Fatalf("an error clears on the next key:\n%s", m.View())
	}
	fc.err = errString("again")
	m, _ = run(m, "enter")
	m, _ = m.Update(councilSnap(preflightFor(2, copyPaused...)))
	if strings.Contains(m.View(), "again") {
		t.Fatalf("an error clears on the next snapshot:\n%s", m.View())
	}
}

func TestCouncilPopupSendsOnce(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(50, copyPaused...)))
	m, cmd := m.Update(ckey("enter"))
	if cmd == nil {
		t.Fatal("first enter must send")
	}
	if _, again := m.Update(ckey("enter")); again != nil {
		t.Fatal("enter while a send is in flight must do nothing")
	}
	fc.err = errString("boom")
	msg := cmd()
	if len(fc.calls) != 1 {
		t.Fatalf("calls %v", fc.calls)
	}
	m, _ = m.Update(msg) // the error reply re-arms enter
	if _, retry := m.Update(ckey("enter")); retry == nil {
		t.Fatal("enter works again after an error reply")
	}
}

func TestCouncilPopupDropsAStalePreflightButKeepsTicks(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = NewCouncil("w1:p1", fc)
	m, _ = m.Update(councilSnap(preflightFor(50, copyPaused...)))
	m, _ = run(m, "tab", "space") // untick Security
	m, _ = m.Update(councilSnap(nil))
	v := m.View()
	if !strings.Contains(v, "checking budgets…") || strings.Contains(v, "estimate:") {
		t.Fatalf("a snapshot without a preflight must not leave a stale estimate:\n%s", v)
	}
	m, _ = run(m, "enter")
	if len(fc.calls) != 0 {
		t.Fatalf("calls %v", fc.calls)
	}
	m, _ = m.Update(councilSnap(preflightFor(50, copyPaused...)))
	if !strings.Contains(m.View(), "[ ] ◆ Security") {
		t.Fatalf("ticks are kept:\n%s", m.View())
	}
}

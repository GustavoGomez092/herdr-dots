package pane

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

func TestProactiveTextIsSanitized(t *testing.T) {
	c := queue.Card{Origin: state.RunCommit, Label: "commit \x1b[31mabc‮\ndef\tx"}
	if got := cardWhere(c); strings.ContainsAny(got, "\x1b‮\n\t") {
		t.Fatalf("label not sanitized: %q", got)
	}
	d := view.DotState{Status: "idle", Next: "research\x1b[2J\nin 6m"}
	if got := dotStatus(d); strings.ContainsAny(got, "\x1b\n") {
		t.Fatalf("hint not sanitized: %q", got)
	}
}

func hostileSnap() view.Snapshot {
	s := awaySnap()
	evil := "\x1b[2J\x1b]52;c;ZXZpbA==\x07‮"
	s.Panes[0].Dots[1].Name = "Fro" + evil + "\nnt"
	c := &s.Panes[0].Cards[0]
	c.DotName, c.Label, c.Note, c.Question = "Aw"+evil+"\nay", "l"+evil+"\nabel"+strings.Repeat("x", 80), "note"+evil+"\nmore", "q"+evil
	c.Body = "body" + evil
	c.Parts[0].DotName, c.Parts[0].Body, c.Parts[0].Glyph, c.Parts[0].Stance = "Sec"+evil, "part"+evil+" "+strings.Repeat("word ", 40), "◆"+evil, "st"+evil
	s.Panes[0].Cards[1].DotName = "Sec" + evil
	s.Panes[0].Feed = []state.FeedEntry{{Dot: "d", Outcome: state.OutcomeResearch, Detail: "research: a" + evil + "\nb" + strings.Repeat("y", 90)}}
	return s
}

func TestHostileModelTextNeverReachesTheScreen(t *testing.T) {
	const w = 40
	m, _ := New("w1:p1", &fakeCaller{}).Update(tea.WindowSizeMsg{Width: w})
	m, _ = m.Update(SnapshotMsg(hostileSnap()))
	m = press(m, "f", " ")
	v := m.View()
	for _, bad := range []string{"\x1b[2J", "\x1b]52", "\x07", "‮"} {
		if strings.Contains(v, bad) {
			t.Fatalf("raw %q reached the view:\n%s", bad, v)
		}
	}
	for _, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > w {
			t.Errorf("line wider than %d (%d): %q", w, lipgloss.Width(line), ansi.Strip(line))
		}
	}
	if !strings.Contains(v, "part") {
		t.Fatalf("expanded part missing:\n%s", v)
	}
}

func TestEnterExpandsAnUnseenDigestBeforeSending(t *testing.T) {
	fc := &fakeCaller{}
	m := press(awayReady(fc), "enter")
	if len(fc.calls) != 0 {
		t.Fatalf("an unseen digest was sent: %+v", fc.calls)
	}
	if v := m.View(); !strings.Contains(v, "prototype pollution advisory") {
		t.Fatalf("first enter should expand:\n%s", v)
	}
	press(m, "enter")
	if len(fc.calls) != 1 || fc.calls[0].cmd != ipc.CmdSend || fc.calls[0].args != `{"ids":["7"],"parts":{"7":2}}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestDigestExpandedWithSpaceSendsOnFirstEnter(t *testing.T) {
	fc := &fakeCaller{}
	press(awayReady(fc), " ", "enter")
	if len(fc.calls) != 1 {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestSendAllSkipsUnexpandedDigests(t *testing.T) {
	fc := &fakeCaller{}
	m := press(awayReady(fc), "a")
	if len(fc.calls) != 1 || strings.Contains(fc.calls[0].args, `"7"`) || !strings.Contains(fc.calls[0].args, `"8"`) {
		t.Fatalf("calls %+v", fc.calls)
	}
	if v := m.View(); !strings.Contains(v, "skipped 1 digest") || !strings.Contains(v, "expand") {
		t.Fatalf("hint missing:\n%s", v)
	}
	fc2 := &fakeCaller{}
	press(awayReady(fc2), " ", "a")
	if len(fc2.calls) != 1 || !strings.Contains(fc2.calls[0].args, `"7"`) {
		t.Fatalf("expanded digest should be sent: %+v", fc2.calls)
	}
}

func TestTodaySpendHidesAZeroBudget(t *testing.T) {
	s := view.Snapshot{TodayRuns: 2, TodayUSD: 0.5}
	if got := todayPart(s); got != "  today $0.50" {
		t.Fatalf("got %q", got)
	}
}

func TestAwayLineSingular(t *testing.T) {
	p := view.Pane{Away: &view.AwayState{Since: time.Date(2026, 10, 21, 10, 12, 0, 0, time.Local), Pending: 1}}
	if got := awayLine(p); !strings.Contains(got, "1 research result waiting") || strings.Contains(got, "results") {
		t.Fatalf("got %q", got)
	}
}

func TestExpandedDigestWrapsAtNarrowWidth(t *testing.T) {
	m, _ := New("w1:p1", &fakeCaller{}).Update(tea.WindowSizeMsg{Width: 24})
	m, _ = m.Update(SnapshotMsg(awaySnap()))
	v := press(m, " ").View()
	for _, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 24 {
			t.Errorf("line wider than 24: %q", ansi.Strip(line))
		}
	}
	flat := strings.Join(strings.Fields(ansi.Strip(v)), " ")
	if !strings.Contains(flat, "autocomplete") || !strings.Contains(flat, "attributes.") {
		t.Fatalf("part text lost:\n%s", v)
	}
}

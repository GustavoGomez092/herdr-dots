package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/queue"
)

// A digest that gains a part after it was expanded has parts the developer
// never saw: it must be shown again before it can be sent.
func digestGrew(m tea.Model) tea.Model {
	s := awaySnap()
	c := &s.Panes[0].Cards[0]
	c.Parts = append(c.Parts, queue.CardPart{Dot: "docs", DotName: "Docs", Glyph: "■", Severity: "info", Body: "The changelog lacks an entry."})
	m, _ = m.Update(SnapshotMsg(s))
	return m
}

func TestEnterReExpandsADigestThatGainedParts(t *testing.T) {
	fc := &fakeCaller{}
	m := press(awayReady(fc), " ") // expanded at 2 parts
	m = digestGrew(m)
	m = press(m, "enter")
	if len(fc.calls) != 0 {
		t.Fatalf("a digest with unseen parts was sent: %+v", fc.calls)
	}
	v := m.View()
	if !strings.Contains(v, "digest has new parts: ⏎ again to send") || !strings.Contains(v, "changelog") {
		t.Fatalf("want re-expanded with the new part and a hint:\n%s", v)
	}
	press(m, "enter")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"ids":["7"],"parts":{"7":3}}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

// Ruling P8-R9 for `e`: editing a digest counts as reading it only if its
// parts were shown at the current count. Otherwise `e` first expands it,
// like ⏎, and a second `e` opens the editor.
func TestEditExpandsAnUnseenDigestFirst(t *testing.T) {
	fc := &fakeCaller{}
	m := press(awayReady(fc), "e")
	v := m.View()
	if strings.Contains(v, "ctrl+s save") {
		t.Fatalf("e opened the editor on a digest whose parts were never shown:\n%s", v)
	}
	if !strings.Contains(v, "prototype pollution advisory") || !strings.Contains(v, "digest expanded: e again to edit it") {
		t.Fatalf("first e should expand the digest with a hint:\n%s", v)
	}
	if v := press(m, "e").View(); !strings.Contains(v, "ctrl+s save") {
		t.Fatalf("second e should open the editor:\n%s", v)
	}
	if v := press(awayReady(&fakeCaller{}), " ", "e").View(); !strings.Contains(v, "ctrl+s save") {
		t.Fatalf("e on an expanded digest edits at once:\n%s", v)
	}
	m = digestGrew(press(awayReady(&fakeCaller{}), " "))
	v = press(m, "e").View()
	if strings.Contains(v, "ctrl+s save") || !strings.Contains(v, "digest has new parts: e again to edit it") || !strings.Contains(v, "changelog") {
		t.Fatalf("e on a digest that gained parts re-expands it:\n%s", v)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls %+v", fc.calls)
	}
}

func TestSendAllSkipsADigestThatGainedParts(t *testing.T) {
	fc := &fakeCaller{}
	m := press(awayReady(fc), " ")
	m = press(digestGrew(m), "a")
	if len(fc.calls) != 1 || strings.Contains(fc.calls[0].args, `"7"`) || strings.Contains(fc.calls[0].args, "parts") {
		t.Fatalf("calls %+v", fc.calls)
	}
	if v := m.View(); !strings.Contains(v, "skipped 1 digest") {
		t.Fatalf("hint missing:\n%s", v)
	}
}

func TestSendAllReportsTheDigestPartCountsItShowed(t *testing.T) {
	fc := &fakeCaller{}
	press(awayReady(fc), " ", "a")
	if len(fc.calls) != 1 || fc.calls[0].args != `{"ids":["7","8","1"],"parts":{"7":2}}` {
		t.Fatalf("calls %+v", fc.calls)
	}
}

package manage

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/dots"
)

// Hardening round: never act on an unseen selection.

func TestEditKeysAreIgnoredWhileTheFocusedLineIsNotDrawn(t *testing.T) {
	for _, h := range []int{1, 2, 3} {
		f := NewForm(dots.New("x"), true)
		f.SetHeight(h)
		focusOn(f, lResearch)
		drawn := strings.Contains(f.View(80), lResearch)
		f.Update(k("space"))
		if on := f.byLabel(lResearch).on; on != drawn {
			t.Fatalf("height %d: line drawn=%v but toggle on=%v", h, drawn, on)
		}
	}
	// The toggle's line is not drawn: edit keys do nothing, tab still moves.
	f := NewForm(dots.New("x"), true)
	f.SetHeight(3)
	focusOn(f, lOnCommit)
	f.View(80)
	f.unseen = true
	f.height = 0 // Update must not re-lay out and clear the flag
	f.Update(k("space"))
	if f.byLabel(lOnCommit).on {
		t.Fatal("space must be ignored while the line is unseen")
	}
	f.Update(k("tab"))
	if f.fields[f.focus].label == lOnCommit {
		t.Fatal("tab must still move focus")
	}
}

func TestSmallHeightIsFlooredAtThree(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	f.SetHeight(1)
	focusOn(f, lResearch)
	v := f.View(80)
	if !strings.Contains(v, lResearch) || !strings.Contains(v, "ctrl+s save") {
		t.Fatalf("floor of 3 lines shows title, focus and help:\n%s", v)
	}
	f.Update(k("space"))
	if !f.byLabel(lResearch).on {
		t.Fatal("a drawn toggle responds")
	}
}

func TestReadOnlyLineStaysWithACronError(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	typeInto(f, lCron, "0 25 * * *", 0)
	v := f.View(100)
	i := strings.Index(v, "must be 0-23")
	if i < 0 || !strings.Contains(v[i:], "read-only") {
		t.Fatalf("read-only after the cron error:\n%s", v)
	}
}

func TestNoLineIsWiderThanTheWidth(t *testing.T) {
	d := dots.New("x")
	d.Research.Web = true
	d.Backend.Kind = "codex-cli"
	f := NewForm(d, true)
	typeInto(f, lCron, "0 9 * * 1-5", 0)
	for _, h := range []int{0, 12, 30} {
		f.SetHeight(h)
		v := f.View(40)
		for _, l := range strings.Split(v, "\n") {
			if w := ansi.StringWidth(l); w > 40 {
				t.Fatalf("height %d: width %d > 40: %q", h, w, l)
			}
		}
		if !strings.Contains(v, "NEW DOT") || !strings.Contains(v, "ctrl+s save") {
			t.Fatalf("height %d: title or key help lost:\n%s", h, v)
		}
	}
}

func TestSavingReservesALine(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	m = press(m, "t", "j", "enter")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	m, cmd := m.Update(key("ctrl+s"))
	if cmd == nil {
		t.Fatal("want a save cmd")
	}
	v := m.View()
	if n := strings.Count(v, "\n") + 1; n > 14 {
		t.Fatalf("%d lines in a 14-line screen:\n%s", n, v)
	}
	if !strings.Contains(v, "EDIT DOT") && !strings.Contains(v, "NEW DOT") {
		t.Fatalf("title pushed off:\n%s", v)
	}
	if !strings.Contains(v, "saving") {
		t.Fatalf("saving marker:\n%s", v)
	}
}

func TestLookupsTolerateAMissingField(t *testing.T) {
	f := &Form{fields: []field{textField("Name", "x")}}
	if _, err := f.labelNum(lIdleMin); err == nil {
		t.Fatal("a missing field is an error, not a panic")
	}
	_ = f.proactiveNote(lCron)
	_ = f.proactiveNote(lWeb)
}

// Update must lay out at the width View really renders at: wrapped help and
// error lines take rows that an unclipped layout doesn't count. At width 30
// with a long error, for every height and every toggle, space changes the
// toggle exactly when its line is on the real screen.
func TestUnseenCheckUsesTheRealWidth(t *testing.T) {
	long := strings.Repeat("a long error that wraps ", 6)
	diverged := 0
	for h := 3; h <= 45; h++ {
		for _, label := range []string{lResearch, lWeb, lOnCommit, "Review every turn", "Read repo (read-only)"} {
			f := NewForm(dots.New("x"), true)
			f.SetError(long)
			f.SetHeight(h)
			focusOn(f, label)
			shown := false
			for _, l := range strings.Split(f.View(30), "\n") {
				if strings.Contains(l, label) {
					shown = true
				}
			}
			real := f.unseen
			f.View(0)
			if real != f.unseen {
				diverged++
			}
			f.View(30)
			before := f.byLabel(label).on
			f.Update(k("space"))
			if changed := f.byLabel(label).on != before; changed != shown {
				t.Fatalf("height %d, %q: drawn=%v but changed=%v", h, label, shown, changed)
			}
		}
	}
	t.Logf("%d cases where the unwrapped layout disagrees with the real one", diverged)
}

func TestNoWidthSeenYetIsUnseen(t *testing.T) {
	f := NewForm(dots.New("x"), true)
	f.SetHeight(10)
	focusOn(f, lResearch)
	f.Update(k("space"))
	if f.byLabel(lResearch).on {
		t.Fatal("without a rendered width the field is unseen; edits are ignored")
	}
}

package memui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/memory"
)

// noSpace drops every space, so text wrapped onto lines compares whole.
func noSpace(s string) string { return strings.Join(strings.Fields(s), "") }

// Fix round 1 (UI rule, Phase 2 VITAL): the terminal cuts a line wider than
// the popup on the right, so a proposal is approvable only when every cell
// of it is on screen: CJK text is measured in cells, and every line of the
// approve screen (title, who, kind, paths, text) fits the width.
func TestApproveNeverApprovesAProposalCutOnTheRight(t *testing.T) {
	cjk := strings.Repeat("請把令牌存放在僅限HTTP的Cookie中絕不使用本地存儲", 3)
	for _, c := range []struct {
		name           string
		width          int
		text           string
		paths          []string
		dot            string
		wantApprovable bool
	}{
		{"CJK text at width 40", 40, cjk, []string{"src/api/**"}, "frontend", true},
		{"long paths and dot at width 30", 30, "Use the fetch wrapper.", []string{"src/very/deeply/nested/module/path/**", "x"}, "a-dot-with-a-really-long-name", true},
		{"width 20 is too narrow", 20, "Use the fetch wrapper.", []string{"src/api/**/handlers/*.ts"}, "frontend", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, n := seed(t), 0
			ref := memory.Ref{Scope: memory.ScopeProject, ID: "m1"}
			if _, err := st.Update(pid, ref, func(x *memory.Entry) error {
				x.Text, x.Paths, x.Dot = c.text, c.paths, c.dot
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			m, _ := open(st, "", &n).Update(tea.WindowSizeMsg{Width: c.width, Height: 60})
			m = press(m, "a")
			v := m.View()
			approvable := strings.Contains(v, "y approve")
			if approvable != c.wantApprovable {
				t.Fatalf("approvable %v, want %v:\n%s", approvable, c.wantApprovable, v)
			}
			if approvable {
				// Every line of the approve screen (all above the y prompt)
				// fits, and every cell of the proposal is there.
				screen, _, _ := strings.Cut(v, "y approve")
				for _, line := range strings.Split(screen, "\n") {
					if w := ansi.StringWidth(line); w > c.width {
						t.Errorf("line of %d cells at width %d would be cut: %q", w, c.width, line)
					}
				}
				for _, want := range append([]string{c.text, c.dot}, c.paths...) {
					if !strings.Contains(noSpace(ansi.Strip(screen)), noSpace(want)) {
						t.Errorf("approve screen is missing %q:\n%s", want, v)
					}
				}
			} else if !strings.Contains(noSpace(v), noSpace("Too small to show the whole proposal (needs 24 columns, has 20)")) {
				t.Fatalf("a refused popup says why:\n%s", v)
			}
			press(m, "y")
			e, _ := st.Get(pid, ref)
			if c.wantApprovable && (e.Status != memory.StatusActive || n != 1) {
				t.Fatalf("y approves a proposal shown whole: %s %d", e.Status, n)
			}
			if !c.wantApprovable && (e.Status != memory.StatusProposed || n != 0) {
				t.Fatalf("y must not approve a proposal the popup cuts: %s %d", e.Status, n)
			}
		})
	}
}

// wrap measures display cells: a CJK rune takes two.
func TestWrapMeasuresCells(t *testing.T) {
	in := strings.Repeat("令牌", 20) + " ok " + strings.Repeat("ab", 30)
	got := wrap(in, 15)
	for _, l := range got {
		if w := ansi.StringWidth(l); w > 15 {
			t.Fatalf("line of %d cells: %q", w, l)
		}
	}
	if noSpace(strings.Join(got, "")) != noSpace(in) {
		t.Fatalf("wrap lost text: %q", got)
	}
	// Grapheme clusters whose width differs from their runes' (VS16 emoji)
	// still end, and lose nothing.
	emoji := strings.Repeat("☺️", 30)
	if g := wrap(emoji, 10); noSpace(strings.Join(g, "")) != emoji {
		t.Fatalf("emoji wrap: %q", g)
	}
}

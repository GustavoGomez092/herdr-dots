package pane

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A one-member council card says "1 dot", not "1 dots".
func TestCouncilHeaderSaysOneDot(t *testing.T) {
	s := councilSnapshot(nil)
	s.Panes[0].Cards[0].Parts = s.Panes[0].Cards[0].Parts[:1]
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	v := m.View()
	if !strings.Contains(v, "Council · turn 22 · 1 dot") || strings.Contains(v, "1 dots") {
		t.Fatalf("header:\n%s", v)
	}
}

// M3: only real headings (one to six #s and a space) outside code fences
// become labels: #include, #42 and a fenced "# comment" stay as written.
func TestCouncilTextRewritesOnlyHeadings(t *testing.T) {
	in := "## Must fix\n#42 is open\n#include <stdio.h>\n####### seven\n```\n# a shell comment\n```\n   ### Consider\n#\tTabbed"
	want := "Must fix:\n#42 is open\n#include <stdio.h>\n####### seven\n```\n# a shell comment\n```\nConsider:\nTabbed:"
	if got := councilText(in); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// The chair's Markdown headings read as plain labels, selected or not, and
// in the member answers too.
func TestCouncilHeadingsShowAsLabels(t *testing.T) {
	s := councilSnapshot(nil)
	s.Panes[0].Cards[0].Body = "## Must fix\n- JWT in localStorage\n\n### Consider\n- a skeleton"
	s.Panes[0].Cards[0].Parts[0].Body = "## Risk\nJWT in localStorage."
	m, _ := New("w1:p1", &fakeCaller{}).Update(SnapshotMsg(s))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 80})
	check := func(what string, v string, want ...string) {
		t.Helper()
		v = ansi.Strip(v)
		if strings.Contains(v, "#") {
			t.Errorf("%s: raw markdown heading on screen:\n%s", what, v)
		}
		for _, w := range want {
			if !strings.Contains(v, w) {
				t.Errorf("%s: missing %q:\n%s", what, w, v)
			}
		}
	}
	check("selected", m.View(), "Must fix:", "Consider:")
	check("expanded", press(m, " ").View(), "Must fix:", "Risk:")
	check("not selected", press(m, "j").View(), "Must fix: - JWT in localStorage")
}

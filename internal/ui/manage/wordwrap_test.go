package manage

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	setupui "herdr-dots/internal/ui/setup"
)

// B5: the library's key help wraps between words, never inside one.
func TestTheLibraryFooterWrapsBetweenWords(t *testing.T) {
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil },
		Setup: setupui.Deps{Check: func() (setupui.Offer, error) { return setupui.Offer{}, nil }}}
	m, _ := New("w1:p1", d).Update(SnapshotMsg(lib()))
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 40})
	v := ansi.Strip(m.View())
	i := strings.Index(v, "space toggle")
	if i < 0 {
		t.Fatalf("no key help:\n%s", v)
	}
	help := v[i:]
	for _, l := range strings.Split(help, "\n") {
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("line %q is %d wide", l, w)
		}
	}
	want := strings.Fields("space toggle · enter apply · n new · t template · e edit · D delete · i profile · I import · T trust · K providers · esc close · S setup")
	if got := strings.Fields(help); !slices.Equal(got, want) {
		t.Fatalf("the help was cut inside a word:\n%s", help)
	}
}

// B5: a single word longer than the width still breaks (nothing is cut off).
func TestFitLinesBreaksAWordLongerThanTheWidth(t *testing.T) {
	m, _ := New("", Deps{}).Update(tea.WindowSizeMsg{Width: 22, Height: 40}) // body width 20
	long := strings.Repeat("x", 45)
	got := m.(Model).fitLines([]string{"short words " + long})
	if len(got) == 0 || strings.TrimSpace(got[0]) != "short words" || strings.Count(strings.Join(got, ""), "x") != 45 {
		t.Fatalf("lines %q: want \"short words\", then the long word hard-broken", got)
	}
	for _, l := range got {
		if ansi.StringWidth(l) > 20 {
			t.Fatalf("line %q wider than 20", l)
		}
	}
}

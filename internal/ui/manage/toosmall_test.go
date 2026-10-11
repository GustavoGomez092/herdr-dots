package manage

import (
	"strings"
	"testing"

	setupui "herdr-dots/internal/ui/setup"
)

// The scroll hint ("the keys are at the end") shows only when the screen is
// longer than one page.
func TestTheSetupScrollHintShowsOnlyWhenItScrolls(t *testing.T) {
	const hint = "the keys are at the end"
	if v := screenText(boot(t, &setupFakes{}, 200, 200)); strings.Contains(v, hint) {
		t.Errorf("everything fits, yet the hint shows:\n%s", v)
	}
	if v := screenText(boot(t, &setupFakes{}, 96, 12)); !strings.Contains(v, hint) {
		t.Errorf("the screen scrolls, yet no hint:\n%s", v)
	}
	if v := screenText(boot(t, &setupFakes{}, 20, 40)); strings.Contains(v, "j/space down") {
		t.Errorf("Too small has nothing to scroll, yet the hint shows:\n%s", v)
	}
}

// B6: in a small popup the Setup screen is fitted to the height, so the
// terminal never drops its top: the first line is the title or "Too small".
func TestTheSetupScreenKeepsItsTopInASmallPopup(t *testing.T) {
	for _, sz := range [][2]int{{28, 15}, {20, 8}, {20, 12}, {16, 6}} {
		w, h := sz[0], sz[1]
		m := boot(t, &setupFakes{}, w, h)
		v := screenText(m)
		lines := strings.Split(v, "\n")
		if len(lines) > h {
			t.Errorf("%dx%d: %d lines, the terminal drops the top:\n%s", w, h, len(lines), v)
		}
		first := strings.TrimSpace(lines[0])
		if !strings.HasPrefix(setupui.Title, strings.TrimSuffix(first, "…")) && !strings.HasPrefix(first, "Too small") {
			t.Errorf("%dx%d: first line %q, want the title or Too small:\n%s", w, h, first, v)
		}
		if w < 24 && !strings.Contains(v, "Too small") {
			t.Errorf("%dx%d: no Too small:\n%s", w, h, v)
		}
	}
}

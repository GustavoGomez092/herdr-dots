package manage

import (
	"strings"
	"testing"

	"herdr-dots/internal/dots"
)

// U11 (post-rc hardening): editing a dot whose file names a color the editor
// doesn't offer keeps that color instead of silently rewriting it to red.
func TestEditKeepsAnUnknownColor(t *testing.T) {
	m, _, dir := setup(t, "w1:p1")
	d, _ := dots.FromTemplate("backend")
	d.Color = "orange"
	if err := dots.Save(dir, d, true); err != nil {
		t.Fatal(err)
	}
	m = press(m, "e", "ctrl+s") // Backend is under the cursor
	if !strings.Contains(m.View(), "saved backend") {
		t.Fatalf("status:\n%s", m.View())
	}
	if got, _ := dots.Load(dir, "backend"); got.Color != "orange" {
		t.Fatalf("color rewritten to %q", got.Color)
	}
}

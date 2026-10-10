package manage

import (
	"strings"
	"testing"

	"herdr-dots/internal/dots"
)

// U10 (post-rc hardening): renaming a dot to another library dot's name is
// refused (any case): two dots with one display name would share one
// delivery header, and the loop guard tells dots apart by that name.
func TestRenamingADotToAnotherDotsNameIsRefused(t *testing.T) {
	m, _, dir := setup(t, "w1:p1")
	d, _ := dots.FromTemplate("backend")
	_ = dots.Save(dir, d, true)
	m = press(m, "e", "ctrl+u") // Backend is under the cursor; the name field has focus
	m = typeText(m, "security")
	m = press(m, "ctrl+s")
	if v := m.View(); !strings.Contains(v, `a dot named "security" already exists`) {
		t.Fatalf("rename onto Security:\n%s", v)
	}
	if got, _ := dots.Load(dir, "backend"); got.Name != d.Name {
		t.Fatalf("the dot was saved as %q", got.Name)
	}
}

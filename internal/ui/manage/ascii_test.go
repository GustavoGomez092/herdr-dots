package manage

import (
	"strings"
	"testing"

	"herdr-dots/internal/ui/style"
)

// U6 (post-rc hardening): with HERDR_DOTS_ASCII=1 the template list draws its
// selection marker in ASCII too.
func TestASCIIModeTemplateMarkerIsASCII(t *testing.T) {
	old := style.SetASCII(true)
	defer style.SetASCII(old)
	m, _, _ := setup(t, "w1:p1")
	v := press(m, "t").View()
	if strings.Contains(v, "▸") || !strings.Contains(v, "\n> ") {
		t.Fatalf("template marker in ASCII mode:\n%s", v)
	}
}

package diff

import (
	"fmt"
	"strings"
	"testing"

	"herdr-dots/internal/patchstore"
)

func twelveLinePatch() string {
	var b strings.Builder
	b.WriteString("diff --git a/f.txt b/f.txt\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ b/f.txt\n@@ -0,0 +1,12 @@\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "+line %d\n", i)
	}
	return b.String()
}

// A 0x0 window draws nothing, so it must not mark rows as seen, and that
// coverage must not survive the next resize.
func TestAZeroSizedWindowMarksNothingSeen(t *testing.T) {
	meta := patchstore.Meta{ID: "0123456789abcdef", Dot: "x", Stat: "1 file", Paths: []string{"f.txt"}}
	m := sized(New([]byte(twelveLinePatch()), meta, "7", &recCaller{}), 0, 0)
	if u := m.unseen(); len(u) != 1 {
		t.Fatalf("a 0x0 window marked rows seen: unseen=%v", u)
	}
	if v := send(m, "A").View(); strings.Contains(v, "Apply 1 file") {
		t.Fatalf("the confirm opened in a 0x0 window:\n%s", v)
	}
	m = sized(m, 80, 8)
	if v := send(m, "A").View(); !strings.Contains(v, "Not yet viewed: f.txt") {
		t.Fatalf("a 12-line file must still be unviewed:\n%s", v)
	}
}

package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/patchstore"
)

func longPatch() string {
	var b strings.Builder
	b.WriteString("diff --git a/big.txt b/big.txt\n--- a/big.txt\n+++ b/big.txt\n@@ -1,60 +1,60 @@\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "+line %d\n", i)
	}
	b.WriteString("@@ -100,2 +100,3 @@\n ctx\n+tail\n ctx\n")
	return b.String()
}

func sized(m Model, w, h int) Model {
	nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return nm.(Model)
}

func TestNothingIsSeenBeforeTheWindowSizeIsKnown(t *testing.T) {
	m := New([]byte(samplePatch), sampleMeta(), "7", nil)
	if u := m.unseen(); len(u) != 2 {
		t.Fatalf("unseen before a size: %v", u)
	}
	m = sized(m, 120, 30)
	if u := m.unseen(); len(u) != 1 || u[0] != "img.bin" {
		t.Fatalf("after the size only the first file is seen: %v", u)
	}
}

func TestAHunkJumpThatSkipsLinesLeavesTheFileUnseen(t *testing.T) {
	meta := patchstore.Meta{ID: "0123456789abcdef", Dot: "x", Stat: "1 file", Paths: []string{"big.txt"}}
	m := sized(New([]byte(longPatch()), meta, "7", &recCaller{}), 120, 20)
	var tm tea.Model = m
	tm = send(tm, "n", "n") // first hunk, then straight to the second
	if v := send(tm, "A").View(); !strings.Contains(v, "Not yet viewed: big.txt") {
		t.Fatalf("skipped lines must leave the file unseen:\n%s", v)
	}
	for i := 0; i < 6; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	}
	for i := 0; i < 8; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if u := tm.(Model).unseen(); len(u) != 0 {
		t.Fatalf("scrolled through everything, still unseen: %v", u)
	}
}

func TestShrinkingTheWindowKeepsWhatWasSeen(t *testing.T) {
	m := sized(New([]byte(samplePatch), sampleMeta(), "7", nil), 120, 30)
	m = sized(m, 120, 12)
	if u := m.unseen(); len(u) != 1 {
		t.Fatalf("unseen %v", u)
	}
}

func TestTooSmallWindowsNeverOpenOrActOnTheConfirm(t *testing.T) {
	for _, w := range []int{30, 40} {
		for h := 4; h <= 8; h++ {
			c := &recCaller{}
			armed := sized(New([]byte(samplePatch), sampleMeta(), "7", c).Arm(), w, h)
			if armed.confirm {
				t.Errorf("%dx%d: armed confirm stayed open", w, h)
			}
			if v := armed.View(); !strings.Contains(v, "window too small") {
				t.Errorf("%dx%d: no explanation:\n%s", w, h, v)
			}
			var tm tea.Model = armed
			tm = send(tm, "A", "y")
			m := sized(New([]byte(samplePatch), sampleMeta(), "7", c), w, h)
			tm2 := send(m, "A", "y")
			if len(c.calls) != 0 || tm.(Model).confirm || tm2.(Model).confirm {
				t.Errorf("%dx%d: acted in a window too small to review: %v", w, h, c.calls)
			}
		}
	}
}

func TestShrinkingAnOpenConfirmClosesIt(t *testing.T) {
	c := &recCaller{}
	m := sized(New([]byte(samplePatch), sampleMeta(), "7", c), 120, 30)
	tm := send(m, "A")
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 30, Height: 6})
	tm = send(tm, "y")
	if len(c.calls) != 0 {
		t.Fatalf("applied from a tiny window: %v", c.calls)
	}
}

func TestUnsizedArmedViewerIgnoresY(t *testing.T) {
	c := &recCaller{}
	send(New([]byte(samplePatch), sampleMeta(), "7", c).Arm(), "y")
	if len(c.calls) != 0 {
		t.Fatalf("y before the diff was rendered applied: %v", c.calls)
	}
}

func TestSpecialFileKindsAreFlagged(t *testing.T) {
	p := "diff --git a/l b/l\nnew file mode 120000\nindex 0000000..1111111\n--- /dev/null\n+++ b/l\n@@ -0,0 +1 @@\n+target\n" +
		"diff --git a/sub b/sub\nnew file mode 160000\nindex 0000000..2222222\n" +
		"diff --git a/old.go b/new.go\nsimilarity index 90%\nrename from old.go\nrename to new.go\n" +
		"diff --git a/c.go b/d.go\nsimilarity index 90%\ncopy from c.go\ncopy to d.go\n" +
		"diff --git a/x.sh b/x.sh\nold mode 100644\nnew mode 100775\n"
	fs := Parse([]byte(p))
	if len(fs) != 5 {
		t.Fatalf("%d files", len(fs))
	}
	if !fs[0].Symlink || !fs[1].Submodule || !fs[2].Renamed || !fs[2].Deletion || !fs[3].Copied || fs[3].Deletion || !fs[4].Exec {
		t.Fatalf("flags %+v", fs)
	}
	if fs[0].Exec || fs[1].Exec {
		t.Fatalf("wrong exec %+v", fs)
	}
}

func TestAFakeHeaderBehindInvisibleCharactersIsNotAFile(t *testing.T) {
	p := strings.Replace(samplePatch, "+\tsetSameSite()", "+\tsetSameSite()\n​diff --git a/fake b/fake\n‮diff --git a/fake2 b/fake2", 1)
	if n := len(Parse([]byte(p))); n != 2 {
		t.Fatalf("%d files; git would apply 2", n)
	}
}

func TestCtrlCClosesEvenWhileApplying(t *testing.T) {
	m := sized(New([]byte(samplePatch), sampleMeta(), "7", &recCaller{}), 120, 30)
	m.busy = true
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c must quit while busy")
	}
}

func TestTheApplyHashCoversTheRawBytesNotTheDisplayedOnes(t *testing.T) {
	evil := strings.Replace(samplePatch, `+var store = "cookie"`, "+var store = \"\x1b[2J‮​cookie\"", 1)
	c := &recCaller{}
	m := sized(New([]byte(evil), sampleMeta(), "7", c), 120, 30)
	send(m, "A", "y")
	h := sha256.Sum256([]byte(evil))
	want := `"sha256":"` + hex.EncodeToString(h[:]) + `"`
	if len(c.calls) != 1 || !strings.Contains(c.calls[0], want) {
		t.Fatalf("calls %v, want %s", c.calls, want)
	}
}

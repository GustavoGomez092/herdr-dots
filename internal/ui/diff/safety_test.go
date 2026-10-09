package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/patchstore"
)

var samplePatchSHA = func() string {
	h := sha256.Sum256([]byte(samplePatch))
	return hex.EncodeToString(h[:])
}()

const riskyPatch = "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n" +
	"diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\nindex 1111111..0000000\n--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n"

func TestBidiAndFormatCharactersAreNeutralizedEverywhere(t *testing.T) {
	evil := strings.Replace(samplePatch, `+var store = "cookie"`, "+var store = \"\u202ekooc\u2066\u200b\ufeff\u200f\"\tx", 1)
	meta := sampleMeta()
	meta.Summary = "ok\u202e evil\u2069"
	meta.Paths = []string{"src/au\u202eth.go", "img.bin"}
	c := &recCaller{err: errors.New("bad \u202e reason")}
	m, _ := New([]byte(evil), meta, "7", c).Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = send(m, "A", "y")
	v := m.View()
	for _, r := range v {
		if (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || (r >= 0x200b && r <= 0x200f) || r == 0xfeff || r == '\t' {
			t.Fatalf("format character %U reached the view:\n%s", r, v)
		}
	}
	if !strings.Contains(v, "src/auth.go") || !strings.Contains(v, "not applied: bad  reason") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestViewNeverExceedsTheWindow(t *testing.T) {
	long := strings.Replace(samplePatch, `+var store = "cookie"`, "+"+strings.Repeat("宽", 200), 1)
	for _, sz := range [][2]int{{120, 30}, {80, 12}, {40, 10}, {50, 14}} {
		m, _ := New([]byte(long), sampleMeta(), "7", &recCaller{}).Arm().Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		v := m.View()
		lines := strings.Split(v, "\n")
		if len(lines) > sz[1] {
			t.Errorf("%v: %d lines:\n%s", sz, len(lines), v)
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > sz[0] {
				t.Errorf("%v: line of width %d: %q", sz, w, l)
			}
		}
		if !strings.Contains(lines[0], "PATCH") {
			t.Errorf("%v: header scrolled away: %q", sz, lines[0])
		}
	}
}

func TestFileListFlagsRiskyFilesAndConfirmNamesUnseenOnes(t *testing.T) {
	meta := patchstore.Meta{ID: "0123456789abcdef", Dot: "x", Stat: "2 files", Paths: []string{"run.sh", "gone.txt"}}
	m, _ := New([]byte(riskyPatch), meta, "7", &recCaller{}).Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	v := m.View()
	if !strings.Contains(v, "[exec]") || !strings.Contains(v, "[deleted]") {
		t.Fatalf("flags:\n%s", v)
	}
	v = send(m, "A").View()
	for _, want := range []string{"Not yet viewed: gone.txt", "1 deletion", "1 executable"} {
		if !strings.Contains(v, want) {
			t.Errorf("confirm lacks %q:\n%s", want, v)
		}
	}
	if v = send(m, "j", "A").View(); strings.Contains(v, "Not yet viewed") {
		t.Fatalf("every file was shown:\n%s", v)
	}
}

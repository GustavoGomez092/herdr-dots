package diff

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/patchstore"
)

const samplePatch = `diff --git a/src/auth.go b/src/auth.go
index 1111111..2222222 100644
--- a/src/auth.go
+++ b/src/auth.go
@@ -1,3 +1,3 @@
 package auth
-var store = "localStorage"
+var store = "cookie"
@@ -20,2 +20,3 @@ func Login() {
 	issue()
+	setSameSite()
 }
diff --git a/img.bin b/img.bin
new file mode 100644
index 0000000..3333333
GIT binary patch
literal 4
LcmZQzWMXCj00001

literal 0
HcmV?d00001

`

type recCaller struct {
	calls []string
	err   error
}

func (r *recCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	r.calls = append(r.calls, cmd+" "+string(b))
	return r.err
}

func keyOf(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// send presses keys and runs returned commands, like the Bubble Tea runtime.
func send(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(keyOf(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				if _, quit := msg.(tea.QuitMsg); !quit {
					m, _ = m.Update(msg)
				}
			}
		}
	}
	return m
}

func sampleMeta() patchstore.Meta {
	return patchstore.Meta{ID: "0123456789abcdef", Dot: "security", Summary: "Moved the JWT to a cookie.", Stat: "2 files +2 −1",
		Flags: []string{"binary"}, Paths: []string{"src/auth.go", "img.bin"}}
}

func viewer(c Caller) tea.Model {
	m, _ := New([]byte(samplePatch), sampleMeta(), "7", c).Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

func TestParseSplitsFilesHunksAndBinaries(t *testing.T) {
	fs := Parse([]byte(samplePatch))
	if len(fs) != 2 {
		t.Fatalf("%d files", len(fs))
	}
	if fs[0].Path != "src/auth.go" || len(fs[0].Hunks) != 2 || fs[0].Added != 2 || fs[0].Deleted != 1 || fs[0].Binary {
		t.Fatalf("file 0 %+v", fs[0])
	}
	if fs[1].Path != "img.bin" || !fs[1].Binary {
		t.Fatalf("file 1 %+v", fs[1])
	}
}

func TestViewShowsTheSummaryFilesAndDiff(t *testing.T) {
	v := viewer(&recCaller{}).View()
	for _, want := range []string{"PATCH  security · 2 files +2 −1  [binary]", "Moved the JWT to a cookie.", "▸ src/auth.go +2 −1", "img.bin binary", `+var store = "cookie"`, "A apply"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestNavigationMovesBetweenFilesAndHunks(t *testing.T) {
	m := send(viewer(&recCaller{}), "n")
	if got := m.(Model).top; got != 4 {
		t.Fatalf("first n: top %d, want the first hunk (4)", got)
	}
	m = send(m, "n")
	if got := m.(Model).top; got != 8 {
		t.Fatalf("second n: top %d, want the second hunk (8)", got)
	}
	m = send(m, "n") // no more hunks here: the next file
	if mm := m.(Model); mm.file != 1 {
		t.Fatalf("n past the last hunk selects the next file, got %d", mm.file)
	}
	m = send(m, "k", "down")
	if mm := m.(Model); mm.file != 0 || mm.top != 1 {
		t.Fatalf("k then down: file %d top %d", mm.file, mm.top)
	}
	if v := send(m, "j").View(); !strings.Contains(v, "▸ img.bin") {
		t.Fatalf("j selects the next file:\n%s", v)
	}
}

func TestApplyNeedsYes(t *testing.T) {
	c := &recCaller{}
	m := send(viewer(c), "A")
	if v := m.View(); !strings.Contains(v, "Apply 2 files to your working tree? Nothing is staged or committed. (y/N)") {
		t.Fatalf("confirm:\n%s", v)
	}
	m = send(m, "n")
	if len(c.calls) != 0 || !strings.Contains(m.View(), "not applied") {
		t.Fatalf("n must not apply: %v", c.calls)
	}
	m = send(m, "A", "y")
	if len(c.calls) != 1 || c.calls[0] != ipc.CmdApplyPatch+` {"id":"7"}` {
		t.Fatalf("calls %v", c.calls)
	}
	if v := m.View(); !strings.Contains(v, "applied to your working tree") {
		t.Fatalf("after apply:\n%s", v)
	}
	if v := send(m, "A").View(); !strings.Contains(v, "already applied") {
		t.Fatalf("a second A:\n%s", v)
	}
}

func TestApplyErrorStaysOpenWithTheReason(t *testing.T) {
	c := &recCaller{err: errors.New("agent is working; try when idle")}
	m := send(viewer(c), "A", "y")
	if v := m.View(); !strings.Contains(v, "not applied: agent is working; try when idle") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestArmOpensTheQuestionWithTheDiffOnScreen(t *testing.T) {
	m, _ := New([]byte(samplePatch), sampleMeta(), "7", &recCaller{}).Arm().Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	v := m.View()
	if !strings.Contains(v, "(y/N)") || !strings.Contains(v, `+var store = "cookie"`) {
		t.Fatalf("armed view:\n%s", v)
	}
	if New([]byte(samplePatch), sampleMeta(), "", nil).Arm().confirm {
		t.Fatal("no card, no apply question")
	}
}

func TestControlCharactersNeverReachTheTerminal(t *testing.T) {
	evil := strings.Replace(samplePatch, `+var store = "cookie"`, "+var store = \"\x1b[2J\x1b]52;c;ZXZpbA==\x07cookie\"", 1)
	v := New([]byte(evil), sampleMeta(), "7", nil).View()
	if strings.Contains(v, "\x1b[2J") || strings.Contains(v, "\x1b]52") || strings.Contains(v, "\x07") {
		t.Fatalf("raw control sequence in view: %q", v)
	}
}

func TestFailedViewerExplainsAndCloses(t *testing.T) {
	m := Failed(patchstore.ErrChanged)
	if !strings.Contains(m.View(), "can't show this patch: the patch file changed since it was drafted") {
		t.Fatal(m.View())
	}
	if _, cmd := m.Update(keyOf("x")); cmd == nil {
		t.Fatal("any key closes")
	}
}

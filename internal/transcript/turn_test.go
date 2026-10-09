package transcript

import (
	"strings"
	"testing"
)

func TestDotPrefixRoundTrip(t *testing.T) {
	msg := DotPrefix("◆", "Security") + " JWT issue\n\n" + DotPrefix("★", "Spec Critic") + " unclear scope"
	got := FromDots(msg)
	if len(got) != 2 || got[0] != "Security" || got[1] != "Spec Critic" {
		t.Fatalf("FromDots = %v", got)
	}
	if FromDots("I wrote [◆ Security dot] mid-sentence") != nil {
		t.Fatal("prefix must start a line")
	}
}

func TestTurnHelpers(t *testing.T) {
	tr := Turn{Tools: []ToolUse{{Name: "Read", Paths: []string{"a.ts"}}, {Name: "Edit", Paths: []string{"b.tsx", "a.ts"}}}, FromDots: []string{"Security"}}
	if p := tr.TouchedPaths(); len(p) != 2 || p[0] != "a.ts" || p[1] != "b.tsx" {
		t.Fatalf("TouchedPaths = %v", p)
	}
	if !tr.IsFromDot("Security") || tr.IsFromDot("Frontend") || !tr.FromAnyDot() {
		t.Fatal("FromDot helpers")
	}
}

func TestFormatClipsLongFields(t *testing.T) {
	long := strings.Repeat("x", MaxFieldChars+500) + "THE END"
	out := Format([]Turn{{Index: 4, User: "do it", Assistant: long, Tools: []ToolUse{{Name: "Edit", Paths: []string{"a.go"}}}}})
	for _, want := range []string{"### Turn 4", "USER:\ndo it", "THE END", "FILES TOUCHED: a.go", "…"} {
		if !strings.Contains(out, want) {
			t.Errorf("Format missing %q", want)
		}
	}
	if len([]rune(out)) > MaxFieldChars+200 {
		t.Fatalf("not clipped: %d runes", len([]rune(out)))
	}
}

func TestCouncilTurnIsFromEveryDot(t *testing.T) {
	text := DotPrefix(CouncilGlyph, CouncilName) + " ## Must fix\n- JWT in localStorage (Security)"
	tr := Turn{User: text, FromDots: FromDots(text)}
	if len(tr.FromDots) != 1 || tr.FromDots[0] != "Council" {
		t.Fatalf("FromDots = %v", tr.FromDots)
	}
	for _, name := range []string{"Security", "Frontend", "Anything"} {
		if !tr.IsFromDot(name) {
			t.Errorf("%s must treat a council turn as its own", name)
		}
	}
	if (Turn{FromDots: []string{"Security"}}).IsFromDot("Frontend") {
		t.Fatal("an ordinary dot turn is only from that dot")
	}
}

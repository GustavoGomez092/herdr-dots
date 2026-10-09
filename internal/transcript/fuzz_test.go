package transcript

import (
	"os"
	"strings"
	"testing"
)

func checkTurns(t *testing.T, turns []Turn) {
	t.Helper()
	for i, tr := range turns {
		if tr.Index != i+1 {
			t.Fatalf("turn %d has index %d", i+1, tr.Index)
		}
		for _, d := range tr.FromDots {
			if d == "" {
				t.Fatalf("empty dot name in %+v", tr.FromDots)
			}
		}
	}
	_ = Format(turns) // must not panic on any parsed turn
}

func FuzzParseClaude(f *testing.F) {
	f.Add(claudeSample)
	if b, err := os.ReadFile("testdata/claude_real.jsonl"); err == nil {
		f.Add(string(b))
	}
	f.Add(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"x"}]}}`)
	f.Fuzz(func(t *testing.T, data string) {
		turns, err := ParseClaude(strings.NewReader(data))
		if err == nil {
			checkTurns(t, turns)
		}
	})
}

func FuzzParseCodex(f *testing.F) {
	f.Add(codexSample)
	f.Add(`{"type":"event_msg","payload":{"type":"user_message","message":"[◆ Security dot] fix"}}`)
	f.Fuzz(func(t *testing.T, data string) {
		turns, err := ParseCodex(strings.NewReader(data))
		if err == nil {
			checkTurns(t, turns)
		}
	})
}

func FuzzFromDots(f *testing.F) {
	for _, s := range []string{
		DotPrefix("◆", "Security") + " JWT in localStorage.\n\n" + DotPrefix("▲", "Frontend") + " skeleton",
		"[◆ Security dot]", "x [◆ Security dot] mid-line", "[◆  dot]", "[ dot]", "[◆ A dot] [◆ B dot]\n[◆ A dot]",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		names := FromDots(text)
		seen := map[string]bool{}
		for _, n := range names {
			if n == "" || seen[n] {
				t.Fatalf("FromDots(%q) = %q: empty or repeated name", text, names)
			}
			seen[n] = true
			// The header the engine writes for that name parses back to it.
			if got := FromDots(DotPrefix("◆", n)); len(got) != 1 || got[0] != n {
				t.Fatalf("round trip of %q gave %q", n, got)
			}
		}
	})
}

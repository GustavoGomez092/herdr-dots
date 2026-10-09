package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectPrefersTranscriptAndFallsBack(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-r")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(claudeSample), 0o600)
	pane := &fakePane{}

	if r := Select(SelectOptions{Agent: "claude", SessionKind: "id", SessionValue: "s1", Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceClaude {
		t.Fatalf("want claude reader, got %s", r.Source())
	}
	if r := Select(SelectOptions{Agent: "claude", SessionKind: "id", SessionValue: "missing", Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceScrollback {
		t.Fatal("unresolvable session must fall back to scrollback")
	}
	if r := Select(SelectOptions{Agent: "claude", SessionKind: "id", SessionValue: "s1", ForceScrollback: true, Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceScrollback {
		t.Fatal("ForceScrollback must win")
	}
	if r := Select(SelectOptions{Agent: "gemini", Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceScrollback {
		t.Fatal("unsupported agents use scrollback")
	}
}

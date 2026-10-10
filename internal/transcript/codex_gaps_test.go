package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T9 (post-rc hardening, test-only): the codex lookups and parser get the
// tests the claude ones have.
func TestResolveCodexPathRejectsUnsafeIDs(t *testing.T) {
	for _, id := range []string{"", "../x", "a/b", `a\b`, "*", "a?", "[ab]"} {
		if _, err := ResolveCodexPath(t.TempDir(), "id", id); err == nil {
			t.Errorf("id %q must be rejected", id)
		}
	}
	if _, err := ResolveCodexPath(t.TempDir(), "bogus", "c1"); err == nil {
		t.Error("an unknown session kind must be rejected")
	}
}

// T6 (post-rc hardening): the id must be the whole session id after the
// rollout timestamp, so "c1" never matches another session "abc-c1".
func TestResolveCodexPathDoesNotMatchAnIDSuffix(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "rollout-2026-10-08T09-00-00-c1.jsonl")
	other := filepath.Join(dir, "rollout-2026-10-08T10-00-00-abc-c1.jsonl")
	for _, f := range []string{mine, other} {
		if err := os.WriteFile(f, []byte(codexSample), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if p, err := ResolveCodexPath(home, "id", "c1"); err != nil || p != mine {
		t.Fatalf("c1 resolved to %q (%v), want %q", p, err, mine)
	}
	if p, err := ResolveCodexPath(home, "id", "abc-c1"); err != nil || p != other {
		t.Fatalf("abc-c1 resolved to %q (%v)", p, err)
	}
}

func TestParseCodexHandlesHugeLines(t *testing.T) {
	huge := `{"type":"response_item","payload":{"type":"function_call_output","output":"` + strings.Repeat("z", 3<<20) + `"}}`
	src := `{"type":"event_msg","payload":{"type":"user_message","message":"first"}}` + "\n" + huge + "\n" +
		`{"type":"event_msg","payload":{"type":"agent_message","message":"done"}}` + "\n"
	turns, err := ParseCodex(strings.NewReader(src))
	if err != nil || len(turns) != 1 || turns[0].Assistant != "done" {
		t.Fatalf("huge line: %+v %v", turns, err)
	}
}

func TestSelectPicksCodexByIDAndByPath(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "rollout-2026-10-08T10-00-00-c1.jsonl")
	if err := os.WriteFile(f, []byte(codexSample), 0o600); err != nil {
		t.Fatal(err)
	}
	pane := &fakePane{}
	if r := Select(SelectOptions{Agent: "codex", SessionKind: "id", SessionValue: "c1", Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceCodex {
		t.Fatalf("by id: %s", r.Source())
	}
	if r := Select(SelectOptions{Agent: "codex", SessionKind: "path", SessionValue: f, Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceCodex {
		t.Fatalf("by path: %s", r.Source())
	}
	if r := Select(SelectOptions{Agent: "codex", SessionKind: "path", SessionValue: filepath.Join(dir, "gone.jsonl"), Home: home, Pane: pane, PaneID: "p"}); r.Source() != SourceScrollback {
		t.Fatalf("a missing path falls back: %s", r.Source())
	}
}

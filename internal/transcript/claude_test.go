package transcript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const claudeSample = `{"type":"permission-mode","permissionMode":"default","sessionId":"s1"}
{"type":"user","isSidechain":false,"message":{"role":"user","content":"Add login"},"sessionId":"s1"}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"}]}}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/r/auth.ts"}}]}}
{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]}}
{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent noise"}]}}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/r/login.tsx","old_string":"a","new_string":"b"}}]}}
{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"Added login form."}]}}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<system-reminder>meta</system-reminder>"}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[◆ Security dot] JWT in localStorage.\n\n[▲ Frontend dot] add a skeleton"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Switched to a cookie."}]}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}
{"type":"user","message":{"role":"user","content":"half-writ`

func TestParseClaudeBuildsTurns(t *testing.T) {
	turns, err := ParseClaude(strings.NewReader(claudeSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d: %+v", len(turns), turns)
	}
	t1 := turns[0]
	if t1.Index != 1 || t1.User != "Add login" || t1.Assistant != "Added login form." {
		t.Fatalf("turn 1 = %+v", t1)
	}
	if p := t1.TouchedPaths(); len(p) != 2 || p[0] != "/r/auth.ts" || p[1] != "/r/login.tsx" {
		t.Fatalf("turn 1 paths = %v", p)
	}
	if strings.Contains(t1.Assistant, "subagent") {
		t.Fatal("sidechain records must be skipped")
	}
	t2 := turns[1]
	if t2.Index != 2 || len(t2.FromDots) != 2 || t2.Assistant != "Switched to a cookie." {
		t.Fatalf("turn 2 = %+v", t2)
	}
}

// Review Focus 2: multi-megabyte lines must not break parsing.
func TestParseClaudeHandlesHugeLines(t *testing.T) {
	huge := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"` + strings.Repeat("z", 3<<20) + `"}]}}`
	src := `{"type":"user","message":{"role":"user","content":"first"}}` + "\n" + huge + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}` + "\n"
	turns, err := ParseClaude(strings.NewReader(src))
	if err != nil || len(turns) != 1 || turns[0].Assistant != "done" {
		t.Fatalf("huge line: %+v %v", turns, err)
	}
}

func TestResolveClaudePath(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-r")
	os.MkdirAll(dir, 0o700)
	f := filepath.Join(dir, "abc-123.jsonl")
	os.WriteFile(f, []byte(claudeSample), 0o600)

	if p, err := ResolveClaudePath(home, "id", "abc-123"); err != nil || p != f {
		t.Fatalf("by id: %q %v", p, err)
	}
	if p, err := ResolveClaudePath(home, "path", f); err != nil || p != f {
		t.Fatalf("by path: %q %v", p, err)
	}
	if _, err := ResolveClaudePath(home, "id", "missing"); err == nil {
		t.Fatal("missing id must error")
	}
	if _, err := ResolveClaudePath(home, "id", "../*"); err == nil {
		t.Fatal("glob characters in id must be rejected")
	}
}

func TestClaudeReader(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(f, []byte(claudeSample), 0o600)
	r := ClaudeReader{Path: f}
	turns, err := r.Turns(context.Background())
	if err != nil || len(turns) != 2 || r.Source() != SourceClaude {
		t.Fatalf("%+v %v", turns, err)
	}
}

// Real capture from Task 1. Skipped until the fixture exists.
func TestParseClaudeRealFixture(t *testing.T) {
	f, err := os.Open("testdata/claude_real.jsonl")
	if err != nil {
		t.Skip("no real fixture yet")
	}
	defer f.Close()
	turns, err := ParseClaude(f)
	if err != nil || len(turns) == 0 {
		t.Fatalf("real fixture: %d turns, %v", len(turns), err)
	}
	for _, tr := range turns {
		if strings.TrimSpace(tr.User) == "" {
			t.Errorf("turn %d has empty user text", tr.Index)
		}
	}
}

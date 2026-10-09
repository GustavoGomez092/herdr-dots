package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexSample = `{"timestamp":"2026-10-08T10:00:00Z","type":"session_meta","payload":{"id":"c1","cwd":"/r"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>x</environment_context>"}]}}
{"type":"event_msg","payload":{"type":"user_message","message":"Fix the API"}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"rg\",\"todo\"]}"}}
{"type":"response_item","payload":{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: api/users.go\n@@\n-a\n+b\n*** Add File: api/users_test.go\n*** End Patch"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"Patched users.go"}}
{"type":"event_msg","payload":{"type":"user_message","message":"[✓ Test Writer dot] add a test"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"Added the test"}}
not json at all
`

func TestParseCodex(t *testing.T) {
	turns, err := ParseCodex(strings.NewReader(codexSample))
	if err != nil || len(turns) != 2 {
		t.Fatalf("turns %+v %v", turns, err)
	}
	if turns[0].User != "Fix the API" || turns[0].Assistant != "Patched users.go" {
		t.Fatalf("turn 1 %+v", turns[0])
	}
	if p := turns[0].TouchedPaths(); len(p) != 2 || p[0] != "api/users.go" || p[1] != "api/users_test.go" {
		t.Fatalf("paths %v", p)
	}
	if !turns[1].IsFromDot("Test Writer") {
		t.Fatalf("turn 2 FromDots %v", turns[1].FromDots)
	}
}

func TestResolveCodexPath(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "08")
	os.MkdirAll(dir, 0o700)
	f := filepath.Join(dir, "rollout-2026-10-08T10-00-00-c1.jsonl")
	os.WriteFile(f, []byte(codexSample), 0o600)
	if p, err := ResolveCodexPath(home, "id", "c1"); err != nil || p != f {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := ResolveCodexPath(home, "id", "nope"); err == nil {
		t.Fatal("missing must error")
	}
}

func TestResolveCodexPathHonoursCodexHome(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(t.TempDir(), "my codex")
	t.Setenv("CODEX_HOME", codexHome)
	dir := filepath.Join(codexHome, "sessions", "2026", "10", "08")
	os.MkdirAll(dir, 0o700)
	f := filepath.Join(dir, "rollout-2026-10-08T10-00-00-c2.jsonl")
	os.WriteFile(f, []byte(codexSample), 0o600)
	if p, err := ResolveCodexPath(home, "id", "c2"); err != nil || p != f {
		t.Fatalf("CODEX_HOME not used: %q %v", p, err)
	}
	t.Setenv("CODEX_HOME", "")
	if _, err := ResolveCodexPath(home, "id", "c2"); err == nil {
		t.Fatal("without CODEX_HOME the session lives elsewhere and must not be found under home")
	}
}

//go:build unix

package mcpinstall

import (
	"os"
	"strings"
	"testing"
)

// Mode 000 makes a file unreadable only on Unix (on Windows it only sets the
// read-only attribute), so this Phase 5 test runs there; the refusal itself
// is the same code on every OS.

// Phase 5 final review M7: a Claude config that exists but can't be read is
// never "not installed"; both remove and install refuse and say how to fix it.
func TestUnreadableClaudeConfigIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 file")
	}
	env := tempEnv(t)
	claudeEntryJSON(t, env, `{"type": "stdio"}`)
	path := env.ClaudeConfigPath()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	want := "can't read " + path + ": permission denied; fix it or remove manually with `claude mcp remove --scope user herdr-dots`"
	p, err := PlanRemove(ClientClaude, env)
	if err == nil || err.Error() != want {
		t.Fatalf("remove: %+v %v", p, err)
	}
	p, err = PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	if err == nil || !strings.HasPrefix(err.Error(), "can't read "+path+": permission denied") {
		t.Fatalf("install: %+v %v", p, err)
	}
}

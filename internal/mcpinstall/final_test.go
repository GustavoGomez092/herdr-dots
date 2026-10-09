package mcpinstall

import (
	"os"
	"strings"
	"testing"
)

// Phase 5 final review M11: reinstalling over a managed block the user added
// keys to is allowed (the diff shows it) but the preview says so in words.
func TestCodexReinstallNotesTheUsersEditsInsideTheBlock(t *testing.T) {
	env := tempEnv(t)
	edited := strings.Replace(wantBlock, "tool_timeout_sec = 180\n", "tool_timeout_sec = 180\nenabled = false\n[mcp_servers.herdr-dots.env]\nFOO = \"1\"\n", 1)
	writeFile(t, env.CodexConfigPath(), userCodex+"\n"+edited)
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil || p.Nothing {
		t.Fatalf("%+v %v", p, err)
	}
	if !strings.Contains(p.Preview, "your edits inside the managed block will be replaced: enabled, env") {
		t.Fatalf("preview:\n%s", p.Preview)
	}
	// A plain reinstall (a moved binary) has nothing to warn about.
	writeFile(t, env.CodexConfigPath(), userCodex+"\n"+strings.Replace(wantBlock, tBin, "/old/herdr-dots", 1))
	p, err = PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil || p.Nothing || strings.Contains(p.Preview, "your edits") {
		t.Fatalf("plain reinstall: %+v %v", p, err)
	}
}

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

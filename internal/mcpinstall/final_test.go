package mcpinstall

import (
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

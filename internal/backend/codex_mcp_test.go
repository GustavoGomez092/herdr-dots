package backend

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/platform"
)

func codexHomeWith(t *testing.T, config string) string {
	t.Helper()
	home := t.TempDir()
	if config != "" {
		if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func codexArgs(env ...string) []string {
	return CodexCLI{Environ: func() []string { return env }}.BuildCmd(req()).Args
}

const withServer = "model = \"x\"\n\n[mcp_servers.docs]\ncommand = \"docs\"\n\n[mcp_servers.herdr-dots]\ncommand = \"/bin/herdr-dots\"\nargs = [\"mcp\"]\n"

// Review Focus 1: a codex dot run must not start the Dots MCP server.
func TestCodexDisablesTheDotsMCPServerWhenConfigured(t *testing.T) {
	home := codexHomeWith(t, withServer)
	args := codexArgs(platform.HomeEnv+"="+home, "PATH=/bin", "HERDR_PANE_ID=w1:p1")
	n := len(args)
	if args[n-1] != "-" || args[n-3] != "-c" || args[n-2] != "mcp_servers.herdr-dots.enabled=false" {
		t.Fatalf("args %v: the override sits right before the stdin marker", args)
	}
	cmd := CodexCLI{Environ: func() []string { return []string{platform.HomeEnv + "=" + home, "HERDR_PANE_ID=w1:p1"} }}.BuildCmd(req())
	if slices.Contains(cmd.Env, "HERDR_PANE_ID=w1:p1") {
		t.Fatal("HERDR_* must still be stripped")
	}
}

func TestCodexHomeOverridesHome(t *testing.T) {
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(withServer), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := codexHomeWith(t, "")
	if args := codexArgs(platform.HomeEnv+"="+empty, "CODEX_HOME="+codexHome); !slices.Contains(args, "mcp_servers.herdr-dots.enabled=false") {
		t.Fatalf("CODEX_HOME config not read: %v", args)
	}
}

func TestCodexLeavesTheOverrideOutWithoutTheServer(t *testing.T) {
	for name, cfg := range map[string]string{
		"no config":       "",
		"other servers":   "[mcp_servers.docs]\ncommand = \"docs\"\n",
		"malformed":       "[mcp_servers.herdr-dots\ncommand = ",
		"commented table": "# [mcp_servers.herdr-dots]\n",
	} {
		home := codexHomeWith(t, cfg)
		if args := codexArgs(platform.HomeEnv+"="+home, "PATH=/bin"); strings.Contains(strings.Join(args, " "), "mcp_servers.") {
			t.Errorf("%s: args %v", name, args)
		}
	}
	if args := codexArgs("PATH=/bin"); strings.Contains(strings.Join(args, " "), "mcp_servers.") {
		t.Errorf("no HOME: args %v", args)
	}
}

// The v1 denylist still refuses a user's own mcp_servers overrides, and the
// engine's flag stays last so no user arg can follow or replace it.
func TestCodexUserMCPArgsCannotReplaceTheOverride(t *testing.T) {
	home := codexHomeWith(t, withServer)
	r := req()
	r.Args = []string{"-c", "mcp_servers.herdr-dots.enabled=true", "--config=mcp_servers.x.command=y"}
	args := CodexCLI{Environ: func() []string { return []string{platform.HomeEnv + "=" + home} }}.BuildCmd(r).Args
	if strings.Contains(strings.Join(args, " "), "enabled=true") || strings.Contains(strings.Join(args, " "), "mcp_servers.x") {
		t.Fatalf("denied user args leaked: %v", args)
	}
	n := len(args)
	if args[n-3] != "-c" || args[n-2] != "mcp_servers.herdr-dots.enabled=false" || args[n-1] != "-" {
		t.Fatalf("override not last: %v", args)
	}
}

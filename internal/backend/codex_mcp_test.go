package backend

import (
	"context"
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
		"no servers":      "model = \"x\"\n",
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

// Ruling P10-R7: a codex dot run starts none of the developer's MCP servers,
// not only the Dots one. Ruling P10-R8: Codex reads a `-c` key by splitting
// it on '=' and then on '.', with no TOML quoting, so every name goes in raw
// (a space or a quote is fine) and in name order.
func TestCodexDisablesEveryDeclaredMCPServer(t *testing.T) {
	cfg := "[mcp_servers.docs]\ncommand = \"docs\"\n\n" +
		"[mcp_servers.'say \"hi\"']\ncommand = \"h\"\n\n" +
		"[mcp_servers.herdr-dots]\ncommand = \"/bin/herdr-dots\"\n"
	home := codexHomeWith(t, cfg)
	args := codexArgs(platform.HomeEnv+"="+home, "PATH=/bin")
	var got []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" && strings.HasPrefix(args[i+1], "mcp_servers.") {
			got = append(got, args[i+1])
		}
	}
	want := []string{
		`mcp_servers.docs.enabled=false`,
		`mcp_servers.herdr-dots.enabled=false`,
		`mcp_servers.say "hi".enabled=false`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("overrides %q, want %q", got, want)
	}
	if args[len(args)-1] != "-" {
		t.Fatalf("args %v: the stdin marker stays last", args)
	}
}

// Ruling P10-R8: a server name with '.' or '=' can't be disabled from the
// command line (Codex would split it into other keys and leave the real
// server enabled), so the codex dot run is refused, naming the server, and
// nothing starts.
func TestCodexRefusesAServerItCantDisable(t *testing.T) {
	for _, name := range []string{"acme.search", "a=b"} {
		cfg := "[mcp_servers.docs]\ncommand = \"docs\"\n\n[mcp_servers.\"" + name + "\"]\ncommand = \"s\"\n"
		home := codexHomeWith(t, cfg)
		run := &fakeRunner{stdout: "{}"}
		c := CodexCLI{Runner: run, Environ: func() []string { return []string{platform.HomeEnv + "=" + home} }}
		_, err := c.Run(context.Background(), req())
		want := "rename MCP server '" + name + "' (Codex can't disable a dotted name from the command line)"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want %q", name, err, want)
		}
		if run.got.Name != "" {
			t.Fatalf("%s: codex was started", name)
		}
	}
}

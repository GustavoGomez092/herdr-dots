package backend

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// CodexMCPServer is the Dots MCP server's name in Codex's config.toml
// (herdr-dots setup-mcp writes [mcp_servers.herdr-dots]).
const CodexMCPServer = "herdr-dots"

// codexMCPOff returns the -c override that disables the Dots MCP server for
// one codex dot run, when the config.toml that run reads declares it. `codex
// exec` loads the user's MCP servers, so a dot would otherwise start ours
// (which refuses every tool anyway: CleanEnv strips HERDR_PANE_ID). Without
// a declared server the override is left out, so Codex never sees a server
// entry that has no command.
func codexMCPOff(env []string) []string {
	path := codexConfigPath(env)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		MCPServers map[string]toml.Primitive `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil
	}
	if _, ok := cfg.MCPServers[CodexMCPServer]; !ok {
		return nil
	}
	return []string{"-c", "mcp_servers." + CodexMCPServer + ".enabled=false"}
}

// codexConfigPath is the config.toml a codex child with env reads:
// $CODEX_HOME/config.toml, else $HOME/.codex/config.toml ("" when neither is set).
func codexConfigPath(env []string) string {
	var home, codexHome string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "CODEX_HOME":
			codexHome = v
		case "HOME":
			home = v
		}
	}
	switch {
	case codexHome != "":
		return filepath.Join(codexHome, "config.toml")
	case home != "":
		return filepath.Join(home, ".codex", "config.toml")
	}
	return ""
}

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/platform"
)

// codexMCPOff returns the -c overrides that disable every MCP server the
// config.toml of one codex dot run declares (ruling P10-R7). `codex exec`
// loads the user's MCP servers, so a dot would otherwise start them: ours
// (which refuses every tool anyway: CleanEnv strips HERDR_PANE_ID) and any
// other, which a read-only dot has no business running. Servers come in
// name order; a name that isn't a bare TOML key is quoted. Without declared
// servers nothing is added, so Codex never sees a server entry that has no
// command.
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
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	var args []string
	for _, name := range names {
		args = append(args, "-c", "mcp_servers."+tomlKey(name)+".enabled=false")
	}
	return args
}

// tomlKey is name as one TOML key segment: bare when it only has A-Z a-z
// 0-9 _ -, else a basic string with quotes, backslashes and control
// characters escaped.
func tomlKey(name string) string {
	bare := name != ""
	for _, r := range name {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			bare = false
			break
		}
	}
	if bare {
		return name
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range name {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// codexConfigPath is the config.toml a codex child with env reads:
// $CODEX_HOME/config.toml, else platform.CodexConfig of the home variable
// (HOME; USERPROFILE on Windows), "" when neither is set. Names compare in
// platform.EnvKey form, so case-insensitively on Windows.
func codexConfigPath(env []string) string {
	var home, codexHome string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch platform.EnvKey(k) {
		case platform.EnvKey("CODEX_HOME"):
			codexHome = v
		case platform.EnvKey(platform.HomeEnv):
			home = v
		}
	}
	switch {
	case codexHome != "":
		return filepath.Join(codexHome, "config.toml")
	case home != "":
		return platform.CodexConfig(home)
	}
	return ""
}

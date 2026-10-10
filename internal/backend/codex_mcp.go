package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/platform"
	"herdr-dots/internal/textutil"
)

// codexMCPOff returns the -c overrides that disable every MCP server the
// config.toml of one codex dot run declares (ruling P10-R7). `codex exec`
// loads the user's MCP servers, so a dot would otherwise start them: ours
// (which refuses every tool anyway: CleanEnv strips HERDR_PANE_ID) and any
// other, which a read-only dot has no business running. Servers come in
// name order. Without declared servers nothing is added, so Codex never sees
// a server entry that has no command.
//
// Codex doesn't read a -c key as TOML: it splits the override on '=' and the
// key on '.', with no quoting (ruling P10-R8). A name goes in raw, and a name
// holding '.' or '=' can't be addressed at all: the error names it, and the
// run is refused rather than started with that server enabled.
func codexMCPOff(env []string) ([]string, error) {
	path := codexConfigPath(env)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var cfg struct {
		MCPServers map[string]toml.Primitive `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, nil
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	var args []string
	for _, name := range names {
		if strings.ContainsAny(name, ".=") {
			return nil, fmt.Errorf("rename MCP server '%s' (Codex can't disable a dotted name from the command line): a codex dot won't run while it could start that server", textutil.SanitizeModelText(name))
		}
		args = append(args, "-c", "mcp_servers."+name+".enabled=false")
	}
	return args, nil
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

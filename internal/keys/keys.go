// Package keys implements `herdr-dots setup-keys`: it proposes keybindings for
// the plugin's actions, refuses to override existing or built-in keys, and
// appends the block to the user's Herdr config after confirmation.
package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Binding struct {
	Key         string
	Action      string
	Description string
}

func Defaults() []Binding {
	return []Binding{
		{Key: "prefix+d", Action: "herdr-dots.manage", Description: "herdr-dots: manage dots"},
		{Key: "prefix+a", Action: "herdr-dots.toggle-pane", Description: "herdr-dots: toggle Dots pane"},
		{Key: "prefix+shift+s", Action: "herdr-dots.summon", Description: "herdr-dots: summon a dot"},
	}
}

// BuiltinPrefixKeys are Herdr 0.8.2's default prefix bindings
// (`herdr --default-config`), treated as taken.
var BuiltinPrefixKeys = func() []string {
	ks := []string{
		"prefix+?", "prefix+b", "prefix+c", "prefix+e", "prefix+g", "prefix+h", "prefix+j", "prefix+k",
		"prefix+l", "prefix+minus", "prefix+n", "prefix+o", "prefix+p", "prefix+q", "prefix+r", "prefix+s",
		"prefix+v", "prefix+w", "prefix+x", "prefix+z", "prefix+tab", "prefix+shift+tab", "prefix+alt+g",
		"prefix+shift+d", "prefix+shift+g", "prefix+shift+n", "prefix+shift+p", "prefix+shift+r",
		"prefix+shift+t", "prefix+shift+w", "prefix+shift+x",
	}
	for i := 1; i <= 9; i++ {
		ks = append(ks, fmt.Sprintf("prefix+%d", i), fmt.Sprintf("prefix+shift+%d", i), fmt.Sprintf("prefix+alt+%d", i))
	}
	return ks
}()

func ConfigPath(getenv func(string) string, home string) string {
	if p := getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "herdr", "config.toml")
	}
	return filepath.Join(home, ".config", "herdr", "config.toml")
}

// Existing maps every bound key in the user's config to what it triggers.
func Existing(data []byte) (map[string]string, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	out := map[string]string{}
	keysTable, _ := raw["keys"].(map[string]any)
	for name, v := range keysTable {
		switch val := v.(type) {
		case string:
			if name != "prefix" && val != "" {
				out[val] = "herdr: " + name
			}
		case []map[string]any:
			for _, c := range val {
				k, _ := c["key"].(string)
				cmd, _ := c["command"].(string)
				if k != "" {
					out[k] = cmd
				}
			}
		case []any:
			for _, c := range val {
				if cm, ok := c.(map[string]any); ok {
					k, _ := cm["key"].(string)
					cmd, _ := cm["command"].(string)
					if k != "" {
						out[k] = cmd
					}
				}
			}
		}
	}
	return out, nil
}

type Conflict struct {
	Binding Binding
	With    string
}

func Plan(want []Binding, existing map[string]string) (add, already []Binding, conflicts []Conflict) {
	builtin := map[string]bool{}
	for _, k := range BuiltinPrefixKeys {
		builtin[k] = true
	}
	for _, b := range want {
		if cur, ok := existing[b.Key]; ok {
			if cur == b.Action {
				already = append(already, b)
			} else {
				conflicts = append(conflicts, Conflict{Binding: b, With: cur})
			}
			continue
		}
		if builtin[b.Key] {
			conflicts = append(conflicts, Conflict{Binding: b, With: "herdr built-in"})
			continue
		}
		add = append(add, b)
	}
	return add, already, conflicts
}

func Block(bs []Binding) string {
	var b strings.Builder
	b.WriteString("\n# herdr-dots keybindings (added by `herdr-dots setup-keys`)\n")
	for _, k := range bs {
		fmt.Fprintf(&b, "\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\ndescription = %q\n", k.Key, k.Action, k.Description)
	}
	return b.String()
}

// Append backs up the config next to itself, then appends the block.
func Append(path string, bs []Binding, now time.Time) (string, error) {
	orig, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	backup := path + ".bak-" + now.Format("20060102-150405")
	if err := os.WriteFile(backup, orig, 0o600); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return backup, err
	}
	defer f.Close()
	_, err = f.WriteString(Block(bs))
	return backup, err
}

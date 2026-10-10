// Package keys implements `herdr-dots setup-keys`: it proposes keybindings for
// the plugin's actions, refuses to override existing or built-in keys, and
// appends the block to the user's Herdr config after confirmation.
package keys

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/platform"
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
		{Key: "prefix+shift+c", Action: "herdr-dots.council", Description: "herdr-dots: council (ask every dot)"},
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

// ConfigPath is Herdr's config.toml (platform.HerdrConfigPath: on Windows
// %APPDATA%\herdr\config.toml unless HERDR_CONFIG_PATH or XDG_CONFIG_HOME
// says otherwise).
func ConfigPath(getenv func(string) string, home string) string {
	return platform.HerdrConfigPath(getenv, home)
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
		cur, ok := existing[b.Key]
		if ok && cur == b.Action {
			already = append(already, b)
			continue
		}
		if k := boundKey(existing, b.Action); k != "" { // bound to another key: never a second one
			b.Key = k
			already = append(already, b)
			continue
		}
		if ok {
			conflicts = append(conflicts, Conflict{Binding: b, With: cur})
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

// boundKey is the first key (sorted) that already runs action, or "".
func boundKey(existing map[string]string, action string) string {
	var ks []string
	for k, cmd := range existing {
		if cmd == action {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	if len(ks) == 0 {
		return ""
	}
	return ks[0]
}

func Block(bs []Binding) string {
	var b strings.Builder
	b.WriteString("\n# herdr-dots keybindings (added by `herdr-dots setup-keys`)\n")
	for _, k := range bs {
		fmt.Fprintf(&b, "\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\ndescription = %q\n", k.Key, k.Action, k.Description)
	}
	return b.String()
}

// Append backs up the config next to itself, then appends the block. The new
// content is built in memory and written (atomically, keeping the file's mode
// and following a symlink) only if it still parses as TOML; otherwise the file
// is left untouched and an error is returned.
func Append(path string, bs []Binding, now time.Time) (string, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // write through a dotfiles symlink instead of replacing it
	}
	orig, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	next := string(orig) + Block(bs)
	var check map[string]any
	if _, err := toml.Decode(next, &check); err != nil {
		return "", fmt.Errorf("adding the keybindings would make %s invalid TOML (%v); nothing changed: add them by hand", path, err)
	}
	backup := ""
	if orig != nil { // no config yet: nothing to back up
		if backup, err = writeBackup(path+".bak-"+now.Format("20060102-150405"), orig); err != nil {
			return "", err
		}
	}
	return backup, fsutil.WriteFileAtomic(path, []byte(next), mode)
}

// writeBackup writes data to base, or base-2, base-3, … when an earlier run
// in the same second took the name: a backup is never overwritten.
func writeBackup(base string, data []byte) (string, error) {
	for i := 1; i <= 100; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(name)
			return "", werr
		}
		return name, nil
	}
	return "", fmt.Errorf("no free backup name next to %s", base)
}

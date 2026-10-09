package keys

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// blockHeader is the comment setup-keys writes above its bindings.
const blockHeader = "# herdr-dots keybindings (added by `herdr-dots setup-keys`)"

var (
	tableHeaderRe  = regexp.MustCompile(`^\s*\[`)
	commandTableRe = regexp.MustCompile(`^\s*\[\[\s*keys\.command\s*\]\]\s*(#.*)?$`)
	ourCommandRe   = regexp.MustCompile(`^\s*command\s*=\s*["']herdr-dots\.`)
)

// RemoveBlock deletes every [[keys.command]] table whose command is one of
// this plugin's actions (herdr-dots.*), and setup-keys's header comment, and
// keeps every other byte of the file. removed counts deleted tables; left
// counts plugin bindings it could not remove (other syntax, such as an
// inline keys.command array), which the developer removes by hand. The
// result must still parse; otherwise the original comes back with an error.
func RemoveBlock(data string) (out string, removed, left int, err error) {
	// First drop setup-keys's header comment and the blank line it wrote
	// before it (the header can sit inside the span of the user's last table).
	var lines []string
	for _, l := range strings.SplitAfter(data, "\n") {
		if strings.TrimRight(l, "\r\n") == blockHeader {
			if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == "" {
				lines = lines[:n-1]
			}
			continue
		}
		lines = append(lines, l)
	}
	var keep []string
	for i := 0; i < len(lines); {
		l := lines[i]
		if !commandTableRe.MatchString(strings.TrimRight(l, "\r\n")) {
			keep = append(keep, l)
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && !tableHeaderRe.MatchString(lines[end]) {
			end++
		}
		ours := false
		for _, b := range lines[i+1 : end] {
			ours = ours || ourCommandRe.MatchString(b)
		}
		if !ours {
			keep = append(keep, lines[i:end]...)
			i = end
			continue
		}
		removed++
		// Drop one blank line before the table, as setup-keys wrote one.
		if n := len(keep); n > 0 && strings.TrimSpace(keep[n-1]) == "" {
			keep = keep[:n-1]
		}
		i = end
	}
	out = strings.Join(keep, "")
	var check map[string]any
	if _, perr := toml.Decode(out, &check); perr != nil {
		return data, 0, 0, fmt.Errorf("removing the keybindings would make the config invalid TOML (%v); nothing changed: remove them by hand", perr)
	}
	ex, perr := Existing([]byte(out))
	if perr != nil {
		return data, 0, 0, perr
	}
	for _, cmd := range ex {
		if strings.HasPrefix(cmd, "herdr-dots.") {
			left++
		}
	}
	return out, removed, left, nil
}

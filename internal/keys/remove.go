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

// RemoveBlock removes what setup-keys added and nothing else. It first cuts
// every exact copy of Block(Defaults()) out of data, byte for byte, so the
// user's own comments, blank lines and line endings around it survive. It
// then looks for hand-written or edited [[keys.command]] tables whose
// command is one of this plugin's actions (herdr-dots.*) and deletes each
// from its header to its last key line only: the comments and blank lines
// after it stay with whatever follows. A blank line is dropped only when it
// sits directly before a removed header (together with setup-keys's header
// comment, when that comment is directly above it). removed counts deleted
// bindings; left counts plugin bindings it could not remove (other syntax,
// such as an inline keys.command array), which the developer removes by
// hand. The result must still parse; otherwise the original comes back with
// an error.
func RemoveBlock(data string) (out string, removed, left int, err error) {
	rest := data
	if block := Block(Defaults()); strings.Contains(rest, block) {
		removed += strings.Count(rest, block) * len(Defaults())
		rest = strings.ReplaceAll(rest, block, "")
	}
	lines := strings.SplitAfter(rest, "\n")
	var keep []string
	for i := 0; i < len(lines); {
		l := lines[i]
		if !commandTableRe.MatchString(strings.TrimRight(l, "\r\n")) {
			keep = append(keep, l)
			i++
			continue
		}
		next := i + 1
		for next < len(lines) && !tableHeaderRe.MatchString(lines[next]) {
			next++
		}
		ours := false
		last := i // the table's last key line
		for j := i + 1; j < next; j++ {
			t := strings.TrimSpace(lines[j])
			if t != "" && !strings.HasPrefix(t, "#") {
				last = j
			}
			ours = ours || ourCommandRe.MatchString(lines[j])
		}
		if !ours {
			keep = append(keep, lines[i:next]...)
			i = next
			continue
		}
		removed++
		if n := len(keep); n > 0 && strings.TrimSpace(keep[n-1]) == "" {
			keep = keep[:n-1] // the separator setup-keys wrote
			if n := len(keep); n > 0 && strings.TrimRight(keep[n-1], "\r\n") == blockHeader {
				keep = keep[:n-1]
				if n := len(keep); n > 0 && strings.TrimSpace(keep[n-1]) == "" {
					keep = keep[:n-1]
				}
			}
		}
		keep = append(keep, lines[last+1:next]...)
		i = next
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

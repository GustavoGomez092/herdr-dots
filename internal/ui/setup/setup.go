// Package setup holds the first-run Setup screen's content and rules
// (post-rc): what it offers (installing the herdr-dots MCP server for the
// detected agent CLIs and the keybindings in Herdr's config), what it shows,
// what y installs and what n and x answer. The manage popup shows it in a
// pager (y only after every line has been on screen, ruling P10-R14); the
// Dots pane only says that setup is pending. Nothing is written without y.
package setup

import (
	"strings"
	"time"

	"herdr-dots/internal/onboarding"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/style"
)

// Client is one detected agent CLI and what installing the server changes.
type Client struct {
	Title     string   // "Claude Code" or "Codex"
	Installed bool     // the server is already installed as planned
	Change    []string // what y does: the plan text setup-mcp prints, one line each
	Err       string   // the plan failed: shown, never applied
}

// Keys is Herdr's config and the keybindings setup-keys would add.
type Keys struct {
	Installed bool     // a herdr-dots keybinding is already in the config
	Path      string   // Herdr's config.toml
	Change    []string // one line per binding y would append
	Err       string   // the config can't be read or parsed
}

// Offer is the state of every item, and which ones the first-run screen
// asks about (S shows them all).
type Offer struct {
	Clients         []Client // the detected clients; none = neither CLI found
	Keys            Keys
	AskMCP, AskKeys bool
	// Plan is what Check planned, opaque here. y hands it back to Apply, so
	// what is applied is what the screen showed (a file changed since then
	// is refused, as setup-mcp refuses it after its preview).
	Plan any
}

// MCPMissing reports a detected client without the server whose plan worked.
func (o Offer) MCPMissing() bool {
	for _, c := range o.Clients {
		if !c.Installed && c.Err == "" {
			return true
		}
	}
	return false
}

// KeysMissing reports that no herdr-dots keybinding is in Herdr's config and
// setup-keys has some to add.
func (o Offer) KeysMissing() bool {
	return !o.Keys.Installed && o.Keys.Err == "" && len(o.Keys.Change) > 0
}

// FirstRun sets AskMCP and AskKeys for the first-run screen: an item is
// asked about when it is missing and the record allows it (no "never", no
// running snooze).
func (o Offer) FirstRun(r onboarding.Record, now time.Time) Offer {
	o.AskMCP = o.MCPMissing() && r.Asks(onboarding.MCP, now)
	o.AskKeys = o.KeysMissing() && r.Asks(onboarding.Keys, now)
	return o
}

// Show reports whether the first-run screen has anything to ask.
func (o Offer) Show() bool { return o.AskMCP || o.AskKeys }

// Result is how one item's install went.
type Result struct {
	Topic onboarding.Topic
	Item  string // "Claude Code", "Codex", "Keybindings"
	Err   error
	Note  string
}

// Deps are the screen's effects; each runs inside a tea.Cmd, never in Update.
type Deps struct {
	// FirstRun detects what the first-run screen asks about: nil when it
	// has nothing to ask. An error is a damaged onboarding.json.
	FirstRun func() (*Offer, error)
	// Check reads the current state of every item (manage's S).
	Check func() (Offer, error)
	// Apply runs setup-mcp --yes for every detected client (mcp) and
	// setup-keys --yes (keys), with the plan the screen showed (Offer.Plan).
	Apply func(plan any, mcp, keys bool) []Result
	// Record saves an answer in onboarding.json.
	Record func(state onboarding.State, topics ...onboarding.Topic) error
}

// Title is the screen's title.
const Title = "Let your agent consult your dots?"

// Screen is what one open Setup screen shows and does.
type Screen struct {
	Offer       Offer
	Forced      bool // opened with S: every item shown, n/esc only close
	IncludeKeys bool // k: the keybindings go in with y
}

func (s Screen) showMCP() bool  { return s.Forced || s.Offer.AskMCP }
func (s Screen) showKeys() bool { return s.Forced || s.Offer.AskKeys }

// MCPPending and KeysPending are the shown items that are missing: what y
// installs and what n and x answer for.
func (s Screen) MCPPending() bool  { return s.showMCP() && s.Offer.MCPMissing() }
func (s Screen) KeysPending() bool { return s.showKeys() && s.Offer.KeysMissing() }

// Pending are the topics n and x answer for.
func (s Screen) Pending() []onboarding.Topic {
	var ts []onboarding.Topic
	if s.MCPPending() {
		ts = append(ts, onboarding.MCP)
	}
	if s.KeysPending() {
		ts = append(ts, onboarding.Keys)
	}
	return ts
}

// Install is what y does: install the MCP server, the keybindings, and
// whether keybindings left out with k on the first-run screen are "not now".
func (s Screen) Install() (mcp, keys, snoozeKeys bool) {
	return s.MCPPending(), s.KeysPending() && s.IncludeKeys, s.KeysPending() && !s.IncludeKeys && !s.Forced
}

// hard breaks each line of s to w display cells byte for byte, keeping
// every space (plan text: diffs, TOML and command lines), with control
// characters dropped: the rows joined, without their indent, are the line.
// A command line ("Will run: ...") breaks after a space where it can, so
// its arguments stay whole (B5); everything else breaks by display cells.
func hard(s string, w, indent int) []string {
	pad := strings.Repeat(" ", indent)
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(textutil.SanitizeModelText(s), "\t", "    "), "\n") {
		wrap := style.WrapExact
		if strings.HasPrefix(l, "Will run: ") {
			wrap = style.WrapExactWords
		}
		for _, p := range wrap(l, max(1, w-indent)) {
			out = append(out, pad+p)
		}
	}
	return out
}

func dot() string { return " " + style.Sym("·") + " " }

// Lines is the whole screen at width w, its key help included: everything
// that must have been on screen before y or x acts.
func (s Screen) Lines(w int) []string {
	var lines []string
	for _, l := range style.Wrap(Title, w) {
		lines = append(lines, style.Title.Render(l))
	}
	lines = append(lines, style.Wrap("With the herdr-dots MCP server, your agent can ask a dot, convene a council, and read the queue and memory whenever it wants.", w)...)
	if s.showMCP() {
		lines = append(lines, "")
		if len(s.Offer.Clients) == 0 {
			lines = append(lines, style.Wrap("MCP server: neither Claude Code nor Codex found (claude or codex on PATH, or ~/.codex/config.toml).", w)...)
		}
		for _, c := range s.Offer.Clients {
			switch {
			case c.Err != "":
				lines = append(lines, hard(c.Title+": can't plan the install: "+c.Err, w, 0)...)
			case c.Installed:
				lines = append(lines, hard(c.Title+": installed", w, 0)...)
			default:
				lines = append(lines, hard(c.Title+":", w, 0)...)
				for _, ch := range c.Change {
					lines = append(lines, hard(ch, w, 2)...)
				}
			}
		}
	}
	if s.showKeys() {
		lines = append(lines, "")
		k := s.Offer.Keys
		switch {
		case k.Err != "":
			lines = append(lines, hard("Keybindings: "+k.Err, w, 0)...)
		case k.Installed:
			lines = append(lines, hard("Keybindings: installed in "+k.Path, w, 0)...)
		case len(k.Change) == 0:
			lines = append(lines, hard("Keybindings: none to add in "+k.Path+" (their keys are taken)", w, 0)...)
		default:
			head := "Keybindings: will append to " + k.Path + ":"
			if !s.IncludeKeys {
				head = "Keybindings (left out): would append to " + k.Path + ":"
			}
			lines = append(lines, hard(head, w, 0)...)
			for _, ch := range k.Change {
				lines = append(lines, hard(ch, w, 2)...)
			}
		}
	}
	lines = append(lines, "")
	lines = append(lines, style.Wrap("Backups are made first. Undo with herdr-dots setup-mcp --remove / uninstall-extras.", w)...)
	lines = append(lines, "")
	for _, l := range style.Wrap(s.help(), w) {
		lines = append(lines, style.Dim.Render(l))
	}
	return lines
}

func (s Screen) help() string {
	var parts []string
	if len(s.Pending()) > 0 {
		parts = append(parts, "y install")
		if !s.Forced {
			parts = append(parts, "n not now")
		}
		parts = append(parts, "x don't ask again")
	}
	if s.KeysPending() {
		if s.IncludeKeys {
			parts = append(parts, "k leave out keybindings")
		} else {
			parts = append(parts, "k include keybindings")
		}
	}
	if s.Forced || len(s.Pending()) == 0 {
		parts = append(parts, "esc close")
	}
	return strings.Join(parts, dot())
}

const restartLine = "Restart the agent in its pane so it loads the server."

// ResultLines is the screen after y: each item's result, the restart line
// when the server went in, and how to go on.
func ResultLines(results []Result, w int) []string {
	var lines []string
	for _, l := range style.Wrap("Setup", w) {
		lines = append(lines, style.Title.Render(l))
	}
	mcpOK := false
	for _, r := range results {
		line := r.Item + ": ok"
		if r.Note != "" {
			line += dot() + r.Note
		}
		if r.Err != nil {
			line = r.Item + ": failed: " + r.Err.Error()
		} else if r.Topic == onboarding.MCP {
			mcpOK = true
		}
		lines = append(lines, style.Wrap(line, w)...)
	}
	if mcpOK {
		lines = append(lines, "")
		lines = append(lines, style.Wrap(restartLine, w)...)
	}
	lines = append(lines, "")
	for _, l := range style.Wrap("any key continues", w) {
		lines = append(lines, style.Dim.Render(l))
	}
	return lines
}

// ApplyAndRecord installs (Deps.Apply), then records what went in, and
// keybindings left out on the first-run screen as "not now". It does I/O:
// call it inside a tea.Cmd.
func ApplyAndRecord(d Deps, plan any, mcp, keys, snoozeKeys bool) []Result {
	results := d.Apply(plan, mcp, keys)
	ok := map[onboarding.Topic]bool{onboarding.MCP: mcp, onboarding.Keys: keys}
	for _, r := range results {
		if r.Err != nil {
			ok[r.Topic] = false
		}
	}
	for _, t := range []onboarding.Topic{onboarding.MCP, onboarding.Keys} {
		if ok[t] {
			if err := d.Record(onboarding.Installed, t); err != nil {
				results = append(results, Result{Item: "Record", Err: err})
			}
		}
	}
	if snoozeKeys {
		if err := d.Record(onboarding.Snoozed, onboarding.Keys); err != nil {
			results = append(results, Result{Item: "Record", Err: err})
		}
	}
	return results
}

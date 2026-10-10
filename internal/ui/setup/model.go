// Package setup is the first-run Setup screen (post-rc): it offers to
// install the herdr-dots MCP server for the detected agent CLIs and the
// keybindings in Herdr's config. It opens before the manage popup or the
// Dots pane the first time, and from manage's S at any time. Nothing is
// written without y; the whole screen must be on screen (display cells,
// style.ScreenFits) before y or x acts.
package setup

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

// Deps are the screen's effects; each runs inside a tea.Cmd.
type Deps struct {
	// Check reads the current state of every item (manage's S).
	Check func() (Offer, error)
	// Apply runs setup-mcp --yes for every detected client (mcp) and
	// setup-keys --yes (keys), with the plan the screen showed (Offer.Plan).
	Apply func(plan any, mcp, keys bool) []Result
	// Record saves an answer in onboarding.json.
	Record func(state onboarding.State, topics ...onboarding.Topic) error
}

// DoneMsg: the screen is finished; the parent shows what was opened.
type DoneMsg struct{}

type (
	appliedMsg  struct{ results []Result }
	answeredMsg struct{ err error }
)

type phase int

const (
	asking phase = iota
	applying
	finished
)

// Model is the Setup screen.
type Model struct {
	d           Deps
	o           Offer
	forced      bool // opened with S: every item shown, n/esc only close
	includeKeys bool
	ph          phase
	results     []Result
	status      string
	width       int
	height      int
}

// New opens the screen. forced is manage's S: every item is shown with its
// state, and closing records nothing.
func New(d Deps, o Offer, forced bool) Model {
	return Model{d: d, o: o, forced: forced, includeKeys: true}
}

// Title is the screen's title.
const Title = "Let your agent consult your dots?"

const restartLine = "Restart the agent in its pane so it loads the server."

const waitingForSize = "waiting for the window size before anything counts as read"

func (m Model) showMCP() bool  { return m.forced || m.o.AskMCP }
func (m Model) showKeys() bool { return m.forced || m.o.AskKeys }

// mcpPending and keysPending are the shown items that are missing: what y
// installs and what n and x answer for.
func (m Model) mcpPending() bool  { return m.showMCP() && m.o.MCPMissing() }
func (m Model) keysPending() bool { return m.showKeys() && m.o.KeysMissing() }

func (m Model) pending() []onboarding.Topic {
	var ts []onboarding.Topic
	if m.mcpPending() {
		ts = append(ts, onboarding.MCP)
	}
	if m.keysPending() {
		ts = append(ts, onboarding.Keys)
	}
	return ts
}

func (m Model) measured() bool { return m.width > 0 && m.height > 0 }

func (m Model) screenWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// tail is the status line under the screen.
const tail = 1

func (m Model) fits() bool {
	return m.measured() && style.ScreenFits(m.screen(m.screenWidth()), m.screenWidth(), m.height, tail)
}

func done() tea.Msg { return DoneMsg{} }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case appliedMsg:
		m.ph, m.results = finished, msg.results
	case answeredMsg:
		if msg.err == nil {
			return m, done
		}
		m.ph = finished
		m.results = []Result{{Item: "Your answer", Err: msg.err}}
	case tea.KeyMsg:
		switch m.ph {
		case applying:
			return m, nil
		case finished:
			return m, done
		}
		return m.keys(msg)
	}
	return m, nil
}

func (m Model) keys(k tea.KeyMsg) (Model, tea.Cmd) {
	m.status = ""
	switch k.String() {
	case "n", "esc", "ctrl+c":
		if m.forced || len(m.pending()) == 0 {
			return m, done
		}
		return m, m.answer(onboarding.Snoozed, m.pending()...)
	case "x", "y", "k":
		if !m.measured() {
			m.status = waitingForSize
			return m, nil
		}
		if !m.fits() {
			m.status = "enlarge the popup: the whole screen must be visible first"
			return m, nil
		}
	default:
		return m, nil
	}
	switch k.String() {
	case "k":
		if m.keysPending() {
			m.includeKeys = !m.includeKeys
		}
	case "x":
		if len(m.pending()) == 0 {
			return m, done
		}
		return m, m.answer(onboarding.Never, m.pending()...)
	case "y":
		mcp, keys := m.mcpPending(), m.keysPending() && m.includeKeys
		if !mcp && !keys {
			m.status = "nothing to install"
			return m, nil
		}
		m.ph = applying
		return m, m.apply(mcp, keys, m.keysPending() && !m.includeKeys && !m.forced)
	}
	return m, nil
}

func (m Model) answer(state onboarding.State, topics ...onboarding.Topic) tea.Cmd {
	rec := m.d.Record
	return func() tea.Msg { return answeredMsg{err: rec(state, topics...)} }
}

// apply installs, then records what went in; keybindings left out with k on
// the first-run screen are "not now".
func (m Model) apply(mcp, keys, snoozeKeys bool) tea.Cmd {
	apply, rec, plan := m.d.Apply, m.d.Record, m.o.Plan
	return func() tea.Msg {
		results := apply(plan, mcp, keys)
		ok := map[onboarding.Topic]bool{onboarding.MCP: mcp, onboarding.Keys: keys}
		for _, r := range results {
			if r.Err != nil {
				ok[r.Topic] = false
			}
		}
		for _, t := range []onboarding.Topic{onboarding.MCP, onboarding.Keys} {
			if ok[t] {
				if err := rec(onboarding.Installed, t); err != nil {
					results = append(results, Result{Item: "Record", Err: err})
				}
			}
		}
		if snoozeKeys {
			if err := rec(onboarding.Snoozed, onboarding.Keys); err != nil {
				results = append(results, Result{Item: "Record", Err: err})
			}
		}
		return appliedMsg{results: results}
	}
}

// hard breaks each line of s at w display cells, keeping its spaces (plan
// text: diffs, TOML and command lines), with control characters dropped.
func hard(s string, w, indent int) []string {
	pad := strings.Repeat(" ", indent)
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(textutil.SanitizeModelText(s), "\t", "    "), "\n") {
		for _, p := range strings.Split(ansi.Hardwrap(l, max(1, w-indent), true), "\n") {
			out = append(out, pad+p)
		}
	}
	return out
}

func dot() string { return " " + style.Sym("·") + " " }

// screen is the whole asking screen at width w, footer included.
func (m Model) screen(w int) []string {
	var lines []string
	for _, l := range style.Wrap(Title, w) {
		lines = append(lines, style.Title.Render(l))
	}
	lines = append(lines, style.Wrap("With the herdr-dots MCP server, your agent can ask a dot, convene a council, and read the queue and memory whenever it wants.", w)...)
	if m.showMCP() {
		lines = append(lines, "")
		if len(m.o.Clients) == 0 {
			lines = append(lines, style.Wrap("MCP server: neither Claude Code nor Codex found (claude or codex on PATH, or ~/.codex/config.toml).", w)...)
		}
		for _, c := range m.o.Clients {
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
	if m.showKeys() {
		lines = append(lines, "")
		k := m.o.Keys
		switch {
		case k.Err != "":
			lines = append(lines, hard("Keybindings: "+k.Err, w, 0)...)
		case k.Installed:
			lines = append(lines, hard("Keybindings: installed in "+k.Path, w, 0)...)
		case len(k.Change) == 0:
			lines = append(lines, hard("Keybindings: none to add in "+k.Path+" (their keys are taken)", w, 0)...)
		default:
			head := "Keybindings: will append to " + k.Path + ":"
			if !m.includeKeys {
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
	for _, l := range style.Wrap(m.help(), w) {
		lines = append(lines, style.Dim.Render(l))
	}
	return lines
}

func (m Model) help() string {
	var parts []string
	if len(m.pending()) > 0 {
		parts = append(parts, "y install")
		if !m.forced {
			parts = append(parts, "n not now")
		}
		parts = append(parts, "x don't ask again")
	}
	if m.keysPending() {
		if m.includeKeys {
			parts = append(parts, "k leave out keybindings")
		} else {
			parts = append(parts, "k include keybindings")
		}
	}
	if m.forced || len(m.pending()) == 0 {
		parts = append(parts, "esc close")
	}
	return strings.Join(parts, dot())
}

// resultLines is the screen after y (or a failed answer).
func (m Model) resultLines(w int) []string {
	var lines []string
	for _, l := range style.Wrap("Setup", w) {
		lines = append(lines, style.Title.Render(l))
	}
	mcpOK := false
	for _, r := range m.results {
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

func (m Model) View() string {
	w := m.screenWidth()
	var lines []string
	switch m.ph {
	case applying:
		lines = style.Wrap("Installing"+style.Sym("…"), w)
	case finished:
		lines = m.resultLines(w)
	default:
		lines = m.screen(w)
		if m.measured() && !m.fits() {
			lines = style.TooSmall(Title, len(lines)+tail, w, m.height, "setup screen", "review and install")
			for _, l := range style.Wrap("n not now"+dot()+"esc close", w) {
				lines = append(lines, style.Dim.Render(l))
			}
		}
	}
	if m.status != "" {
		lines = append(lines, style.Wrap(m.status, w)...)
	}
	return strings.Join(lines, "\n")
}

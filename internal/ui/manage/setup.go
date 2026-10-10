package manage

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/onboarding"
	setupui "herdr-dots/internal/ui/setup"
	"herdr-dots/internal/ui/style"
)

// The Setup screen (post-rc): the first-run offer to install the MCP server
// and the keybindings, and S at any time. It is a pager like the trust and
// import previews: y and x act only once every line has been on screen
// (ruling P10-R14). Detection, installing and recording run in tea.Cmds.

// setupState is the open Setup screen.
type setupState struct {
	scr      setupui.Screen
	pg       pager
	applying bool
	results  []setupui.Result // after y (or a failed answer): any key closes
	done     bool
}

type (
	// setupFirstMsg is Init's detection: the first-run offer (nil: nothing
	// to ask), or a damaged onboarding.json.
	setupFirstMsg struct {
		offer *setupui.Offer
		err   error
	}
	// setupOpenMsg carries a fresh offer for S.
	setupOpenMsg struct {
		offer setupui.Offer
		err   error
	}
	setupAppliedMsg  struct{ results []setupui.Result }
	setupAnsweredMsg struct{ err error }
	mcpStatusMsg     string
)

// damagedOnboarding is the footer line for an onboarding.json that can't be
// read or parsed: the first-run screen stays shut until it is fixed.
const damagedOnboarding = "onboarding.json is damaged: fix or delete it"

// detectSetup runs the first-run detection off the UI goroutine.
func (m Model) detectSetup() tea.Cmd {
	first := m.d.Setup.FirstRun
	if first == nil {
		return nil
	}
	return func() tea.Msg {
		o, err := first()
		return setupFirstMsg{offer: o, err: err}
	}
}

// openSetup reads every item's state off the UI goroutine for S.
func (m Model) openSetup() tea.Cmd {
	check := m.d.Setup.Check
	return func() tea.Msg {
		o, err := check()
		return setupOpenMsg{offer: o, err: err}
	}
}

func (m Model) openSetupScreen(o setupui.Offer, forced bool) Model {
	m.setup = &setupState{scr: setupui.Screen{Offer: o, Forced: forced, IncludeKeys: true}}
	m.scr, m.status = scrSetup, ""
	m.setup.pg.fit(len(m.setupLines()), m.bodyHeight(), m.measured())
	return m
}

func (m Model) setupLines() []string { return m.setup.scr.Lines(m.bodyWidth()) }

// closeSetup returns to the library and reads the footer's MCP status again
// (an install may have changed it).
func (m Model) closeSetup() (Model, tea.Cmd) {
	m.scr, m.setup = scrLibrary, nil
	status := m.d.MCPStatus
	if status == nil {
		return m, nil
	}
	return m, func() tea.Msg { return mcpStatusMsg(status()) }
}

// setupMsg handles the Setup screen's own messages, whatever screen is open.
func (m Model) setupMsg(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case setupFirstMsg:
		if msg.err != nil {
			m.onboardNote = damagedOnboarding
		}
		// The developer may already be elsewhere (a template, the editor):
		// the offer waits for the next open rather than interrupt.
		if msg.offer != nil && m.scr == scrLibrary {
			m = m.openSetupScreen(*msg.offer, false)
		}
		return m, nil, true
	case setupOpenMsg:
		if msg.err != nil {
			m.status = style.Clean(msg.err.Error())
			return m, nil, true
		}
		return m.openSetupScreen(msg.offer, true), nil, true
	case setupAppliedMsg:
		if m.setup != nil {
			m.setup.applying, m.setup.results, m.setup.done = false, msg.results, true
		}
		return m, nil, true
	case setupAnsweredMsg:
		if msg.err == nil {
			mm, cmd := m.closeSetup()
			return mm, cmd, true
		}
		if m.setup != nil {
			m.setup.results = []setupui.Result{{Item: "Your answer", Err: msg.err}}
			m.setup.done = true
		}
		return m, nil, true
	case mcpStatusMsg:
		m.mcpStatus = string(msg)
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) keysSetup(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.setup
	if k.String() == "ctrl+c" {
		return m, tea.Quit // quits; answers nothing
	}
	if st.done {
		mm, cmd := m.closeSetup()
		return mm, cmd
	}
	if st.applying {
		return m, nil
	}
	m.status = ""
	tooNarrow := m.measured() && m.width < style.MinScreenWidth
	if k.String() == "k" { // before the pager: k toggles here, ↑ scrolls up
		if st.scr.KeysPending() && !tooNarrow {
			st.scr.IncludeKeys = !st.scr.IncludeKeys
			// The text changed: every line must be on screen again before y.
			st.pg.offset, st.pg.seenEnd = 0, false
			st.pg.fit(len(m.setupLines()), m.bodyHeight(), m.measured())
		}
		return m, nil
	}
	if st.pg.key(k.String(), len(m.setupLines()), m.bodyHeight(), m.measured()) {
		return m, nil
	}
	switch k.String() {
	case "n", "esc":
		if st.scr.Forced || len(st.scr.Pending()) == 0 {
			mm, cmd := m.closeSetup()
			return mm, cmd
		}
		return m, m.setupAnswer(onboarding.Snoozed, st.scr.Pending()...)
	case "y", "x":
		switch {
		case !m.measured():
			m.status = waitingForSize
			return m, nil
		case tooNarrow:
			m.status = "enlarge the popup first"
			return m, nil
		case !st.pg.seenEnd:
			m.status = "scroll to the end first (j or space): every line must have been on screen"
			return m, nil
		}
		if k.String() == "x" {
			if len(st.scr.Pending()) == 0 {
				mm, cmd := m.closeSetup()
				return mm, cmd
			}
			return m, m.setupAnswer(onboarding.Never, st.scr.Pending()...)
		}
		mcp, keys, snooze := st.scr.Install()
		if !mcp && !keys {
			m.status = "nothing to install"
			return m, nil
		}
		st.applying = true
		d, plan := m.d.Setup, st.scr.Offer.Plan
		return m, func() tea.Msg {
			return setupAppliedMsg{results: setupui.ApplyAndRecord(d, plan, mcp, keys, snooze)}
		}
	}
	return m, nil
}

func (m Model) setupAnswer(state onboarding.State, topics ...onboarding.Topic) tea.Cmd {
	rec := m.d.Setup.Record
	return func() tea.Msg { return setupAnsweredMsg{err: rec(state, topics...)} }
}

func (m Model) setupView() string {
	st := m.setup
	w := m.bodyWidth()
	var lines []string
	switch {
	case st.done:
		lines = setupui.ResultLines(st.results, w)
	case st.applying:
		lines = style.Wrap("Installing"+style.Sym("…"), w)
	case m.measured() && m.width < style.MinScreenWidth:
		lines = style.TooSmall(setupui.Title, 0, m.width, m.height, "setup screen", "review and install")
		lines = append(lines, style.Dim.Render("n not now"+" "+style.Sym("·")+" esc close"))
	default:
		lines = st.pg.window(m.setupLines(), m.bodyHeight())
	}
	var b strings.Builder
	b.WriteString(strings.Join(lines, "\n"))
	if m.status != "" {
		b.WriteString("\n" + m.cutLine(style.Clean(m.status)))
	}
	if !st.done && !st.applying {
		sep := " " + style.Sym("·") + " "
		b.WriteString("\n" + style.Dim.Render(m.cutLine(strings.Join([]string{"j/space down", "b/pgup up", "the keys are at the end"}, sep))))
	}
	return b.String()
}

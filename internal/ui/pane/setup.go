package pane

import tea "github.com/charmbracelet/bubbletea"

// setupHint is the pane's one line while the first-run Setup screen has
// something to ask. The pane never shows that screen (ruling P10-R14): it is
// a pager in the manage popup, where there is room for it.
const setupHint = "Setup pending: open Dots manage (prefix+d or the action menu)"

type setupPendingMsg bool

// WithSetupPending makes Init check, off the UI goroutine, whether the
// Setup screen has anything to ask; the pane then shows setupHint. The
// check records nothing.
func (m Model) WithSetupPending(pending func() bool) Model {
	m.setupPending = pending
	return m
}

func (m Model) setupCheck() tea.Cmd {
	pending := m.setupPending
	if pending == nil {
		return nil
	}
	return func() tea.Msg { return setupPendingMsg(pending()) }
}

package pane

import (
	tea "github.com/charmbracelet/bubbletea"

	setupui "herdr-dots/internal/ui/setup"
)

// WithSetup opens the first-run Setup screen (post-rc) before the pane when
// first is non-nil. The pane keeps tracking snapshots underneath; any key
// after the screen is done shows the pane.
func (m Model) WithSetup(d setupui.Deps, first *setupui.Offer) Model {
	if first != nil {
		s := setupui.New(d, *first, false)
		m.setup = &s
	}
	return m
}

// setupUpdate routes a message to the open Setup screen. ok=false: the pane
// handles msg itself (snapshots and engine state keep flowing).
func (m Model) setupUpdate(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch ws := msg.(type) {
	case SnapshotMsg, DisconnectedMsg, tickMsg, errMsg:
		return m, nil, false
	case setupui.DoneMsg:
		m.setup = nil
		return m, nil, true
	case tea.WindowSizeMsg:
		m.width, m.height = ws.Width, ws.Height
		m.editor.SetWidth(max(20, ws.Width-2))
	}
	s, cmd := m.setup.Update(msg)
	m.setup = &s
	return m, cmd, true
}

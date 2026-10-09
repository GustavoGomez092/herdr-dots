package summon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

type fakeCaller struct{ calls []string }

func (f *fakeCaller) Call(_ context.Context, cmd string, args any) error {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, cmd+" "+string(b))
	return nil
}

func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPickDotAskAndSummon(t *testing.T) {
	fc := &fakeCaller{}
	var m tea.Model = New("w1:p1", fc)
	m, _ = m.Update(SnapshotMsg(view.Snapshot{Panes: []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{
		{Slug: "security", Name: "Security", Glyph: "◆"}, {Slug: "copy", Name: "Copy", Glyph: "✎"},
	}}}}))
	if !strings.Contains(m.View(), "Copy") {
		t.Fatal(m.View())
	}
	for _, k := range []string{"j", "enter", "o", "k", "?", "enter"} {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				if _, quit := msg.(tea.QuitMsg); !quit {
					m, _ = m.Update(msg)
				}
			}
		}
	}
	if len(fc.calls) != 1 || fc.calls[0] != ipc.CmdSummon+` {"pane_id":"w1:p1","slug":"copy","question":"ok?"}` {
		t.Fatalf("calls %v", fc.calls)
	}
}

func TestNoDotsAttached(t *testing.T) {
	var m tea.Model = New("w1:p1", &fakeCaller{})
	m, _ = m.Update(SnapshotMsg(view.Snapshot{}))
	if !strings.Contains(m.View(), "prefix+d") {
		t.Fatal(m.View())
	}
}

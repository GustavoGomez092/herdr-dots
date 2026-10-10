package pane

import (
	"strings"
	"testing"

	"herdr-dots/internal/ipc"
)

// U20 (post-rc hardening): a chooser, editor or question opened while
// connected doesn't send once the engine went away; it stays open with the
// offline hint, and sends after the engine is back.
func TestOpenPromptsDontSendWhileDisconnected(t *testing.T) {
	cases := []struct {
		name, cmd string
		open      []string
		send      string
	}{
		{"remember chooser", ipc.CmdRemember, []string{"M"}, "f"},
		{"rule reason", ipc.CmdRemember, []string{"M", "r"}, "enter"},
		{"editor", ipc.CmdEdit, []string{"e"}, "ctrl+s"},
		{"summon question", ipc.CmdSummon, []string{"tab", "s"}, "enter"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fc := &fakeCaller{}
			m := press(ready(fc), c.open...)
			m, _ = m.Update(DisconnectedMsg{})
			m = press(m, c.send)
			for _, cl := range fc.calls {
				if cl.cmd == c.cmd {
					t.Fatalf("sent %s while disconnected: %v", c.cmd, fc.calls)
				}
			}
			if v := m.View(); !strings.Contains(v, "offline") {
				t.Fatalf("no offline hint:\n%s", v)
			}
			m, _ = m.Update(SnapshotMsg(snap()))
			press(m, c.send)
			sent := false
			for _, cl := range fc.calls {
				sent = sent || cl.cmd == c.cmd
			}
			if !sent {
				t.Fatalf("the prompt must stay open and send once reconnected: %v", fc.calls)
			}
		})
	}
}

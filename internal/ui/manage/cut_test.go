package manage

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Ruling P10-R3: the snapshot cuts long purposes. A trusted project dot's
// profile takes its purpose from the snapshot, so it must say when that copy
// was cut instead of presenting it as the full trusted purpose.
func TestProfileOfAProjectDotWithACutPurposeSaysSo(t *testing.T) {
	fc := &fakeCaller{}
	d, _, _ := shareDeps(t, fc)
	s, _, _ := shareLib(t)
	s.Library[2].Trusted = true
	s.Library[2].Purpose, s.Library[2].PurposeCut = "Be helpful.…", true
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(SnapshotMsg(s))
	m = sharePress(m, "j", "j", "i")
	if v := m.View(); !strings.Contains(v, "Purpose (cut): Be helpful.") {
		t.Fatalf("the profile presents a cut purpose as the whole trusted copy:\n%s", v)
	}
}

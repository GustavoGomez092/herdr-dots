package manage

import (
	"strings"
	"testing"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/view"
)

// U13 (post-rc hardening): a dot attached (or detached) from elsewhere while
// the popup is open follows the pane in the checkboxes, unless the developer
// toggled it here; apply then never detaches what they didn't untick.
func TestCheckboxesFollowAttachmentsMadeElsewhere(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1") // security attached, backend not
	later := lib()
	later.Panes = []view.Pane{{PaneID: "w1:p1", Dots: []view.DotState{{Slug: "security"}, {Slug: "backend"}}}}
	m, _ = m.Update(SnapshotMsg(later)) // backend attached from another popup
	press(m, "enter")
	for _, c := range fc.calls {
		if strings.HasPrefix(c, ipc.CmdDetach) {
			t.Fatalf("apply detached a dot attached elsewhere: %v", fc.calls)
		}
	}
}

func TestATickedBoxSurvivesASnapshot(t *testing.T) {
	m, fc, _ := setup(t, "w1:p1")
	m = press(m, " ") // the cursor is on Backend: tick it here
	m, _ = m.Update(SnapshotMsg(lib()))
	press(m, "enter")
	attached := false
	for _, c := range fc.calls {
		attached = attached || (strings.HasPrefix(c, ipc.CmdAttach) && strings.Contains(c, "backend"))
	}
	if !attached {
		t.Fatalf("the developer's tick was lost on a snapshot: %v", fc.calls)
	}
}

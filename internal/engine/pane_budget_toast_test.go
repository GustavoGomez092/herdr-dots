package engine

import (
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// E12: the pane budget is the pane's, so its toast goes out once per pane
// and agent session, not once for every dot it stops.
func TestPaneBudgetToastsOncePerPane(t *testing.T) {
	r := newRigWith(t, func(d *Deps) { d.Config.SessionBudgetCalls = 1 },
		dot("Security", dots.ModeQueue), dot("Tests", dots.ModeQueue))
	r.attach(t, "security", "tests")
	r.turnEnds(t, transcript.Turn{User: "one"})
	eventually(t, "one run, the other dot over the pane budget", func() bool { return r.be.n() == 1 })
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "both dots over the pane budget", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return len(p.Dots) == 2 && p.Dots[0].Status == view.StatusBudget && p.Dots[1].Status == view.StatusBudget
	})
	time.Sleep(100 * time.Millisecond) // a second toast would be on its way
	var toasts []string
	for _, c := range r.srv.Calls("notification.show") {
		if body, _ := c.Params["body"].(string); strings.HasPrefix(body, "pane call budget") {
			toasts = append(toasts, body)
		}
	}
	if len(toasts) != 1 {
		t.Fatalf("pane budget toasts = %q, want exactly one", toasts)
	}
}

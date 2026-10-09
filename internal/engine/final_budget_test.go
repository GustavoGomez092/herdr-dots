package engine

import (
	"strings"
	"testing"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/view"
)

// The pane's call budget toast names the pane and doesn't suggest r.
func TestPaneBudgetToastDoesNotSuggestReset(t *testing.T) {
	r := newRigWith(t, func(d *Deps) { d.Config.SessionBudgetCalls = 1 }, dot("Security", dots.ModeQueue))
	r.readers["s1"].add(transcript.Turn{User: "one"})
	r.attach(t, "security")
	eventually(t, "first run finished", func() bool {
		p, _ := r.e.Snapshot().Pane(pane)
		return r.be.n() == 1 && p.Dots[0].Status == view.StatusPassed
	})
	r.turnEnds(t, transcript.Turn{User: "two"})
	eventually(t, "budget toast", func() bool { return len(r.srv.Calls("notification.show")) == 1 })
	body, _ := r.srv.Calls("notification.show")[0].Params["body"].(string)
	if body != "pane call budget reached (1 call); resets on a new agent session" {
		t.Fatalf("toast %q", body)
	}
}

// Final review item 13: the budget toast says which budget ran out (calls or
// dollars, the dot's or the pane's), and only a dot budget suggests r.
func TestBudgetMessageSaysWhichBudget(t *testing.T) {
	cases := []struct {
		name   string
		mod    func(*FilterInput)
		reason Reason
		want   []string
		r      bool
	}{
		{"dot calls", func(in *FilterInput) { in.Dot.Budget.MaxCallsPerSession, in.DotCalls = 5, 5 }, ReasonBudget,
			[]string{"dot call budget reached", "5 calls"}, true},
		{"dot dollars", func(in *FilterInput) {
			in.Dot.Budget.MaxCallsPerSession, in.DotBudgetUSD, in.DotCostUSD = 50, 0.5, 0.52
		}, ReasonBudget,
			[]string{"dot dollar budget reached", "$0.52 of $0.50"}, true},
		{"pane calls", func(in *FilterInput) { in.PaneBudget, in.PaneCalls = 120, 120 }, ReasonSessionBudget,
			[]string{"pane call budget reached", "120 calls"}, false},
		{"pane dollars", func(in *FilterInput) { in.PaneBudgetUSD, in.PaneCostUSD = 2, 2.5 }, ReasonSessionBudget,
			[]string{"pane dollar budget reached", "$2.50 of $2.00"}, false},
	}
	for _, c := range cases {
		in := baseInput()
		in.Dot.Budget.MaxCallsPerSession = 50
		c.mod(&in)
		msg := budgetMessage(in, c.reason)
		for _, w := range c.want {
			if !strings.Contains(msg, w) {
				t.Errorf("%s: %q lacks %q", c.name, msg, w)
			}
		}
		if got := strings.Contains(msg, "press r"); got != c.r {
			t.Errorf("%s: %q: suggests r = %v, want %v", c.name, msg, got, c.r)
		}
		if !strings.Contains(msg, "new agent session") {
			t.Errorf("%s: %q must say how it resets", c.name, msg)
		}
	}
}

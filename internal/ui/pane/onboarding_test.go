package pane

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/onboarding"
	setupui "herdr-dots/internal/ui/setup"
)

// drive sends keys and runs every command they lead to, as Bubble Tea would.
func drive(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				break
			}
			m, cmd = m.Update(msg)
		}
	}
	return m
}

func paneWithSetup(t *testing.T, marks *[]string, applied *int) tea.Model {
	t.Helper()
	d := setupui.Deps{
		Apply: func(_ any, mcp, keys bool) []setupui.Result {
			*applied++
			return []setupui.Result{{Topic: onboarding.MCP, Item: "Codex"}}
		},
		Record: func(s onboarding.State, ts ...onboarding.Topic) error {
			for _, tp := range ts {
				*marks = append(*marks, string(tp)+"="+string(s))
			}
			return nil
		},
	}
	o := setupui.Offer{Clients: []setupui.Client{{Title: "Codex", Change: []string{"Will edit /h/.codex/config.toml:", "+ [mcp_servers.herdr-dots]"}}}}
	o = o.FirstRun(onboarding.Record{}, time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	var m tea.Model = New("w1:p1", &fakeCaller{}).WithSetup(d, &o)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m, _ = m.Update(SnapshotMsg(snap()))
	return m
}

func TestTheDotsPaneOpensWithTheSetupScreenOnTheFirstRun(t *testing.T) {
	var marks []string
	applied := 0
	m := paneWithSetup(t, &marks, &applied)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Let your agent consult your dots?") || strings.Contains(v, "DOTS →") {
		t.Fatalf("the Setup screen comes first:\n%s", v)
	}
	if applied != 0 || len(marks) != 0 {
		t.Fatal("nothing is written before y")
	}
	m = drive(m, "y", "z")
	if applied != 1 || strings.Join(marks, ",") != "mcp=installed" {
		t.Fatalf("applied %d marks %v", applied, marks)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "DOTS →") {
		t.Fatalf("any key after the results opens the pane:\n%s", v)
	}
}

func TestEscOnThePaneSetupScreenSnoozes(t *testing.T) {
	var marks []string
	applied := 0
	m := drive(paneWithSetup(t, &marks, &applied), "esc")
	if applied != 0 || strings.Join(marks, ",") != "mcp=snoozed" {
		t.Fatalf("applied %d marks %v", applied, marks)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "DOTS →") {
		t.Fatalf("view:\n%s", v)
	}
	// The pane's own keys work again (q quits).
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q no longer reaches the pane")
	}
}

func TestWithSetupWithoutAnOfferChangesNothing(t *testing.T) {
	m := New("w1:p1", &fakeCaller{}).WithSetup(setupui.Deps{}, nil)
	mm, _ := m.Update(SnapshotMsg(snap()))
	if v := ansi.Strip(mm.View()); !strings.Contains(v, "DOTS →") {
		t.Fatalf("view:\n%s", v)
	}
}

package manage

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/onboarding"
	setupui "herdr-dots/internal/ui/setup"
)

type setupFakes struct {
	checks  int
	applied int
	marks   []string
}

func (f *setupFakes) deps() setupui.Deps {
	return setupui.Deps{
		Check: func() (setupui.Offer, error) {
			f.checks++
			return setupui.Offer{
				Clients: []setupui.Client{{Title: "Claude Code", Installed: true}},
				Keys:    setupui.Keys{Path: "/k/config.toml", Change: []string{"prefix+d  herdr-dots: manage dots"}},
			}, nil
		},
		Apply: func(_ any, mcp, keys bool) []setupui.Result {
			f.applied++
			return []setupui.Result{{Topic: onboarding.MCP, Item: "Claude Code"}}
		},
		Record: func(s onboarding.State, ts ...onboarding.Topic) error {
			for _, t := range ts {
				f.marks = append(f.marks, string(t)+"="+string(s))
			}
			return nil
		},
	}
}

func t0() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }

func firstOffer() *setupui.Offer {
	o := setupui.Offer{
		Clients: []setupui.Client{{Title: "Claude Code", Change: []string{"Will run: claude mcp add --scope user herdr-dots"}}},
		Keys:    setupui.Keys{Path: "/k/config.toml", Change: []string{"prefix+d  herdr-dots: manage dots"}},
	}
	o = o.FirstRun(onboarding.Record{}, t0())
	return &o
}

func setupManage(t *testing.T, f *setupFakes, first *setupui.Offer, w, h int) tea.Model {
	t.Helper()
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil },
		Setup: f.deps(), SetupFirst: first}
	var m tea.Model = New("w1:p1", d)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.Update(SnapshotMsg(lib()))
	return m
}

func TestFirstOpenShowsSetupBeforeTheLibrary(t *testing.T) {
	f := &setupFakes{}
	m := setupManage(t, f, firstOffer(), 100, 50)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Let your agent consult your dots?") || strings.Contains(v, "DOTS LIBRARY") {
		t.Fatalf("the Setup screen comes first:\n%s", v)
	}
	if f.applied != 0 || len(f.marks) != 0 {
		t.Fatalf("nothing is written before y: applied %d marks %v", f.applied, f.marks)
	}
	m = press(m, "n")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "DOTS LIBRARY") {
		t.Fatalf("n continues to the library:\n%s", v)
	}
	if strings.Join(f.marks, ",") != "mcp=snoozed,keys=snoozed" {
		t.Fatalf("marks %v", f.marks)
	}
}

func TestYOnTheFirstRunInstallsThenAnyKeyOpensTheLibrary(t *testing.T) {
	f := &setupFakes{}
	m := press(setupManage(t, f, firstOffer(), 100, 50), "y")
	if f.applied != 1 {
		t.Fatalf("applied %d", f.applied)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Restart the agent in its pane") {
		t.Fatalf("results:\n%s", v)
	}
	m = press(m, "j")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "DOTS LIBRARY") {
		t.Fatalf("any key continues:\n%s", v)
	}
}

func TestNoFirstRunOfferOpensTheLibrary(t *testing.T) {
	f := &setupFakes{}
	m := setupManage(t, f, nil, 100, 50)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "DOTS LIBRARY") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestSInManageOpensTheSetupScreen(t *testing.T) {
	f := &setupFakes{}
	m := setupManage(t, f, nil, 160, 50)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "S setup") {
		t.Fatalf("the help line names S:\n%s", v)
	}
	m = press(m, "S")
	v := ansi.Strip(m.View())
	if f.checks != 1 || !strings.Contains(v, "Let your agent consult your dots?") || !strings.Contains(v, "Claude Code: installed") ||
		!strings.Contains(v, "prefix+d") {
		t.Fatalf("checks %d view:\n%s", f.checks, v)
	}
	m = press(m, "esc")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "DOTS LIBRARY") || len(f.marks) != 0 {
		t.Fatalf("esc returns without recording: marks %v\n%s", f.marks, v)
	}
}

func TestSWithoutSetupSaysSo(t *testing.T) {
	m, _, _ := setup(t, "w1:p1")
	m = press(m, "S")
	if v := m.View(); !strings.Contains(v, "setup is not available here") || strings.Contains(v, "S setup") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestTheFirstRunScreenInATinyPopupIsTooSmall(t *testing.T) {
	f := &setupFakes{}
	m := press(setupManage(t, f, firstOffer(), 30, 8), "y")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Too small") || f.applied != 0 {
		t.Fatalf("applied %d view:\n%s", f.applied, v)
	}
}

package setup

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/onboarding"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// nothingInstalled is a machine with Claude Code and Codex and no herdr-dots
// server or keybinding yet.
func nothingInstalled() Offer {
	return Offer{
		Clients: []Client{
			{Title: "Claude Code", Change: []string{
				"Will run: claude mcp remove --scope user herdr-dots",
				"Will run: claude mcp add --scope user --env 'HERDR_PANE_ID=${HERDR_PANE_ID:-}' --transport stdio herdr-dots -- /opt/herdr-dots/bin/herdr-dots mcp --config-dir /cfg --state-dir /state",
			}},
			{Title: "Codex", Change: []string{
				"Will edit /home/me/.codex/config.toml:",
				"+ # >>> herdr-dots MCP server",
				"+ [mcp_servers.herdr-dots]",
				"+ # <<< herdr-dots MCP server",
			}},
		},
		Keys: Keys{Path: "/home/me/.config/herdr/config.toml", Change: []string{
			"prefix+d        herdr-dots: manage dots",
			"prefix+a        herdr-dots: toggle Dots pane",
			"prefix+shift+s  herdr-dots: summon a dot",
			"prefix+shift+c  herdr-dots: council (ask every dot)",
		}},
	}
}

// fakes records what the screen asked for.
type fakes struct {
	applied [][2]bool
	marks   []string
	results []Result
	markErr error
}

func (f *fakes) deps() Deps {
	return Deps{
		Apply: func(_ any, mcp, keys bool) []Result {
			f.applied = append(f.applied, [2]bool{mcp, keys})
			return f.results
		},
		Record: func(s onboarding.State, ts ...onboarding.Topic) error {
			for _, t := range ts {
				f.marks = append(f.marks, string(t)+"="+string(s))
			}
			return f.markErr
		},
	}
}

func sized(m Model, w, h int) Model {
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// press sends a key and runs the commands it returns, as Bubble Tea would.
// done reports a DoneMsg (the screen handed control back).
func press(m Model, k string) (Model, bool) {
	m, cmd := m.Update(key(k))
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(DoneMsg); ok {
			return m, true
		}
		m, cmd = m.Update(msg)
	}
	return m, false
}

func firstRun(f *fakes) Model {
	o := nothingInstalled().FirstRun(onboarding.Record{}, t0)
	return sized(New(f.deps(), o, false), 100, 60)
}

func TestFirstRunAsksAboutWhatIsMissingAndAllowed(t *testing.T) {
	o := nothingInstalled().FirstRun(onboarding.Record{}, t0)
	if !o.AskMCP || !o.AskKeys || !o.Show() {
		t.Fatalf("nothing installed, no record: %+v", o)
	}
	installed := nothingInstalled()
	for i := range installed.Clients {
		installed.Clients[i].Installed = true
	}
	installed.Keys.Installed = true
	if installed.FirstRun(onboarding.Record{}, t0).Show() {
		t.Fatal("MCP and keys installed: no screen")
	}
	none := nothingInstalled()
	none.Clients = nil
	if o := none.FirstRun(onboarding.Record{}, t0); o.AskMCP || !o.AskKeys {
		t.Fatalf("no client detected: no MCP question: %+v", o)
	}
	partial := nothingInstalled()
	partial.Clients[0].Installed = true
	if o := partial.FirstRun(onboarding.Record{}, t0); !o.AskMCP {
		t.Fatal("Codex still lacks the server: ask")
	}
	never := onboarding.Record{MCP: onboarding.Item{State: onboarding.Never}}
	if o := nothingInstalled().FirstRun(never, t0); o.AskMCP || !o.AskKeys {
		t.Fatalf("never for MCP: %+v", o)
	}
	snoozed := onboarding.Record{Keys: onboarding.Item{State: onboarding.Snoozed, Until: t0.Add(time.Hour).Format(time.RFC3339)}}
	if o := nothingInstalled().FirstRun(snoozed, t0); o.AskKeys {
		t.Fatal("keys snoozed: not asked yet")
	}
	if o := nothingInstalled().FirstRun(snoozed, t0.Add(2*time.Hour)); !o.AskKeys {
		t.Fatal("keys snoozed: asked after the snooze")
	}
	broken := nothingInstalled()
	broken.Keys = Keys{Path: "/x", Err: "not valid TOML"}
	if o := broken.FirstRun(onboarding.Record{}, t0); o.AskKeys {
		t.Fatal("an unreadable Herdr config: keys not offered on the first run")
	}
}

func TestTheScreenShowsEveryChangeAndWritesNothingBeforeY(t *testing.T) {
	f := &fakes{}
	m := firstRun(f)
	m, _ = m.Update(key("j")) // an unknown key does nothing
	v := ansi.Strip(m.View())
	if !strings.Contains(strings.Join(strings.Fields(v), " "), "ask a dot, convene a council, and read the queue and memory") {
		t.Errorf("the MCP line is missing:\n%s", v)
	}
	for _, want := range []string{
		"Let your agent consult your dots?",
		"Claude Code", "claude mcp add --scope user",
		"Codex", "Will edit /home/me/.codex/config.toml:", "+ [mcp_servers.herdr-dots]",
		"/home/me/.config/herdr/config.toml", "prefix+shift+c",
		"Backups are made first.", "herdr-dots setup-mcp --remove", "uninstall-extras",
		"y install", "n not now", "x don't ask again", "k ",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
	if len(f.applied) != 0 || len(f.marks) != 0 {
		t.Fatalf("nothing may be written before y: applied %v marks %v", f.applied, f.marks)
	}
}

func TestYInstallsBothAndRecordsInstalled(t *testing.T) {
	f := &fakes{results: []Result{
		{Topic: onboarding.MCP, Item: "Claude Code"}, {Topic: onboarding.MCP, Item: "Codex"},
		{Topic: onboarding.Keys, Item: "Keybindings", Note: "Herdr config reloaded."},
	}}
	m, done := press(firstRun(f), "y")
	if done {
		t.Fatal("the results stay on screen until a key")
	}
	if !slices.Equal(f.applied, [][2]bool{{true, true}}) {
		t.Fatalf("applied %v", f.applied)
	}
	if !slices.Equal(f.marks, []string{"mcp=installed", "keys=installed"}) {
		t.Fatalf("marks %v", f.marks)
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"Claude Code", "Codex", "Keybindings", "Herdr config reloaded.",
		"Restart the agent in its pane so it loads the server."} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
	if _, done := press(m, "q"); !done {
		t.Fatal("any key continues to the screen the developer opened")
	}
}

func TestAFailedItemIsShownAndNotRecorded(t *testing.T) {
	f := &fakes{results: []Result{
		{Topic: onboarding.MCP, Item: "Claude Code", Err: errors.New("`claude mcp add` failed: exit status 1")},
		{Topic: onboarding.MCP, Item: "Codex"},
		{Topic: onboarding.Keys, Item: "Keybindings"},
	}}
	m, _ := press(firstRun(f), "y")
	if !slices.Equal(f.marks, []string{"keys=installed"}) {
		t.Fatalf("a failed MCP install is not recorded as installed: %v", f.marks)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "claude mcp add` failed: exit status 1") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestKTogglesTheKeybindings(t *testing.T) {
	f := &fakes{results: []Result{{Topic: onboarding.MCP, Item: "Claude Code"}}}
	m := firstRun(f)
	m, _ = m.Update(key("k"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "k include") {
		t.Fatalf("the help says how to turn them back on:\n%s", v)
	}
	press(m, "y")
	if !slices.Equal(f.applied, [][2]bool{{true, false}}) {
		t.Fatalf("applied %v", f.applied)
	}
	// Left out on y: "not now" for the keybindings.
	if !slices.Equal(f.marks, []string{"mcp=installed", "keys=snoozed"}) {
		t.Fatalf("marks %v", f.marks)
	}
}

func TestNSnoozesEscToo(t *testing.T) {
	for _, k := range []string{"n", "esc", "ctrl+c"} {
		f := &fakes{}
		if _, done := press(firstRun(f), k); !done {
			t.Fatalf("%s: closes", k)
		}
		if len(f.applied) != 0 || !slices.Equal(f.marks, []string{"mcp=snoozed", "keys=snoozed"}) {
			t.Fatalf("%s: applied %v marks %v", k, f.applied, f.marks)
		}
	}
}

func TestXSetsNeverForTheShownItemsOnly(t *testing.T) {
	f := &fakes{}
	o := nothingInstalled()
	o.Keys.Installed = true
	m := sized(New(f.deps(), o.FirstRun(onboarding.Record{}, t0), false), 100, 60)
	if v := ansi.Strip(m.View()); strings.Contains(v, "prefix+shift+c") || strings.Contains(v, "k include") || strings.Contains(v, "k leave") {
		t.Fatalf("keys installed: not shown, no k:\n%s", v)
	}
	if _, done := press(m, "x"); !done {
		t.Fatal("x closes")
	}
	if !slices.Equal(f.marks, []string{"mcp=never"}) {
		t.Fatalf("marks %v", f.marks)
	}
}

func TestASavedAnswerThatFailsIsShown(t *testing.T) {
	f := &fakes{markErr: errors.New("onboarding.json: written by a newer herdr-dots")}
	m, done := press(firstRun(f), "n")
	if done {
		t.Fatal("the error stays on screen")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "written by a newer herdr-dots") {
		t.Fatalf("view:\n%s", v)
	}
	if _, done := press(m, "z"); !done {
		t.Fatal("any key continues")
	}
}

func TestTooSmallShowsWhyAndYDoesNothing(t *testing.T) {
	f := &fakes{}
	m := sized(New(f.deps(), nothingInstalled().FirstRun(onboarding.Record{}, t0), false), 30, 8)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Too small") || strings.Contains(v, "claude mcp add") {
		t.Fatalf("view:\n%s", v)
	}
	for _, k := range []string{"y", "x", "k"} {
		var done bool
		m, done = press(m, k)
		if done {
			t.Fatalf("%s acted on an unseen screen", k)
		}
	}
	if len(f.applied) != 0 || len(f.marks) != 0 {
		t.Fatalf("nothing unseen is acted on: applied %v marks %v", f.applied, f.marks)
	}
	for _, l := range strings.Split(m.View(), "\n") {
		if ansi.StringWidth(l) > 30 {
			t.Fatalf("line %q is wider than 30 cells", l)
		}
	}
	// n still answers "not now".
	if _, done := press(m, "n"); !done || !slices.Equal(f.marks, []string{"mcp=snoozed", "keys=snoozed"}) {
		t.Fatalf("n: done=%v marks %v", done, f.marks)
	}
}

func TestYWaitsForTheWindowSize(t *testing.T) {
	f := &fakes{}
	m := New(f.deps(), nothingInstalled().FirstRun(onboarding.Record{}, t0), false)
	if m, _ = press(m, "y"); len(f.applied) != 0 {
		t.Fatal("y before the size is known acts on a screen nobody measured")
	}
	if v := m.View(); !strings.Contains(v, "waiting for the window size") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestEveryLineFitsInDisplayCells(t *testing.T) {
	o := nothingInstalled()
	o.Clients[0].Change = append(o.Clients[0].Change, "Will run: claude mcp add -- /opt/漢字漢字漢字漢字漢字漢字漢字漢字漢字/bin/herdr-dots mcp --config-dir /cfg/😀😀😀😀😀😀😀😀😀😀😀😀")
	o.Keys.Path = "/home/漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字/.config/herdr/config.toml"
	for _, w := range []int{24, 31, 46, 60, 80, 120} {
		f := &fakes{results: []Result{{Topic: onboarding.MCP, Item: "Claude Code", Err: errors.New(strings.Repeat("漢字 ", 40))}}}
		m := sized(New(f.deps(), o.FirstRun(onboarding.Record{}, t0), false), w, 200)
		check := func(stage string) {
			for _, l := range strings.Split(m.View(), "\n") {
				if ansi.StringWidth(l) > w {
					t.Errorf("w=%d %s: line %q is %d cells", w, stage, l, ansi.StringWidth(l))
				}
			}
		}
		check("asking")
		m, _ = press(m, "y")
		check("results")
	}
}

func TestSetupFromManageShowsTheStateOfEveryItem(t *testing.T) {
	f := &fakes{}
	o := nothingInstalled()
	o.Clients[0].Installed, o.Clients[0].Change = true, nil
	o.Keys.Installed, o.Keys.Change = true, nil
	m := sized(New(f.deps(), o, true), 100, 60)
	v := ansi.Strip(m.View())
	for _, want := range []string{"Claude Code: installed", "Codex", "+ [mcp_servers.herdr-dots]", "Keybindings: installed"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
	// Opened by the developer: esc closes without touching the record.
	if _, done := press(m, "esc"); !done || len(f.marks) != 0 {
		t.Fatalf("esc: done=%v marks %v", done, f.marks)
	}
	// y installs what is missing only.
	f2 := &fakes{results: []Result{{Topic: onboarding.MCP, Item: "Codex"}}}
	press(sized(New(f2.deps(), o, true), 100, 60), "y")
	if !slices.Equal(f2.applied, [][2]bool{{true, false}}) || !slices.Equal(f2.marks, []string{"mcp=installed"}) {
		t.Fatalf("applied %v marks %v", f2.applied, f2.marks)
	}
}

func TestSetupWithNothingMissingHasNothingToInstall(t *testing.T) {
	f := &fakes{}
	o := Offer{Keys: Keys{Installed: true, Path: "/k"}}
	m := sized(New(f.deps(), o, true), 100, 60)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "neither Claude Code nor Codex found") {
		t.Fatalf("view:\n%s", v)
	}
	m, _ = press(m, "y")
	if len(f.applied) != 0 || !strings.Contains(m.View(), "nothing to install") {
		t.Fatalf("applied %v view:\n%s", f.applied, m.View())
	}
}

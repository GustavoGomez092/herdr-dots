package setup

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

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
				"Claude Code stores this under \"mcpServers\" in /home/me/.claude.json (user scope)",
				"Backs up /home/me/.claude.json first.",
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

func firstScreen() Screen {
	return Screen{Offer: nothingInstalled().FirstRun(onboarding.Record{}, t0), IncludeKeys: true}
}

func TestTheScreenShowsEveryChange(t *testing.T) {
	v := ansi.Strip(strings.Join(firstScreen().Lines(100), "\n"))
	if !strings.Contains(strings.Join(strings.Fields(v), " "), "ask a dot, convene a council, and read the queue and memory") {
		t.Errorf("the MCP line is missing:\n%s", v)
	}
	for _, want := range []string{
		"Let your agent consult your dots?",
		"Claude Code", "claude mcp add --scope user", "in /home/me/.claude.json (user scope)", "Backs up /home/me/.claude.json first.",
		"Codex", "Will edit /home/me/.codex/config.toml:", "+ [mcp_servers.herdr-dots]",
		"/home/me/.config/herdr/config.toml", "prefix+shift+c",
		"Backups are made first.", "herdr-dots setup-mcp --remove", "uninstall-extras",
		"y install", "n not now", "x don't ask again", "k leave out keybindings",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
}

func TestWhatYInstallsAndWhatNAndXAnswer(t *testing.T) {
	s := firstScreen()
	if mcp, keys, snooze := s.Install(); !mcp || !keys || snooze {
		t.Fatalf("install %v %v %v", mcp, keys, snooze)
	}
	if !slices.Equal(s.Pending(), []onboarding.Topic{onboarding.MCP, onboarding.Keys}) {
		t.Fatalf("pending %v", s.Pending())
	}
	s.IncludeKeys = false
	if mcp, keys, snooze := s.Install(); !mcp || keys || !snooze {
		t.Fatalf("keys left out on the first run: snoozed: %v %v %v", mcp, keys, snooze)
	}
	if v := strings.Join(s.Lines(100), "\n"); !strings.Contains(v, "k include keybindings") || !strings.Contains(v, "(left out)") {
		t.Fatalf("view:\n%s", v)
	}
	s.Forced = true
	if _, _, snooze := s.Install(); snooze {
		t.Fatal("S never snoozes")
	}
	keysIn := nothingInstalled()
	keysIn.Keys.Installed = true
	ks := Screen{Offer: keysIn.FirstRun(onboarding.Record{}, t0), IncludeKeys: true}
	if v := strings.Join(ks.Lines(100), "\n"); strings.Contains(v, "prefix+shift+c") || strings.Contains(v, "k leave") {
		t.Fatalf("keys installed: not shown on the first run, no k:\n%s", v)
	}
	if !slices.Equal(ks.Pending(), []onboarding.Topic{onboarding.MCP}) {
		t.Fatalf("pending %v", ks.Pending())
	}
}

func TestSShowsTheStateOfEveryItem(t *testing.T) {
	o := nothingInstalled()
	o.Clients[0].Installed, o.Clients[0].Change = true, nil
	o.Keys.Installed, o.Keys.Change = true, nil
	s := Screen{Offer: o, Forced: true, IncludeKeys: true}
	v := ansi.Strip(strings.Join(s.Lines(100), "\n"))
	for _, want := range []string{"Claude Code: installed", "Codex", "+ [mcp_servers.herdr-dots]", "Keybindings: installed", "esc close"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
	if mcp, keys, _ := s.Install(); !mcp || keys {
		t.Fatalf("S installs what is missing only: %v %v", mcp, keys)
	}
	empty := Screen{Offer: Offer{Keys: Keys{Installed: true, Path: "/k"}}, Forced: true}
	if v := strings.Join(empty.Lines(100), "\n"); !strings.Contains(v, "neither Claude Code nor Codex found") {
		t.Fatalf("view:\n%s", v)
	}
	if mcp, keys, _ := empty.Install(); mcp || keys {
		t.Fatal("nothing to install")
	}
}

func TestEveryLineFitsInDisplayCells(t *testing.T) {
	o := nothingInstalled()
	o.Clients[0].Change = append(o.Clients[0].Change, "Will run: claude mcp add -- /opt/漢字漢字漢字漢字漢字漢字漢字漢字漢字/bin/herdr-dots mcp --config-dir /cfg/😀😀😀😀😀😀😀😀😀😀😀😀")
	o.Keys.Path = "/home/漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字/.config/herdr/config.toml"
	s := Screen{Offer: o.FirstRun(onboarding.Record{}, t0), IncludeKeys: true}
	results := []Result{{Topic: onboarding.MCP, Item: "Claude Code", Err: errors.New(strings.Repeat("漢字 ", 40))},
		{Topic: onboarding.Keys, Item: "Keybindings", Note: strings.Repeat("/路径", 30)}}
	for _, w := range []int{24, 31, 46, 60, 80, 120} {
		for _, l := range append(s.Lines(w), ResultLines(results, w)...) {
			if ansi.StringWidth(l) > w {
				t.Errorf("w=%d: line %q is %d cells", w, l, ansi.StringWidth(l))
			}
		}
	}
}

func TestResultLinesSayRestartAfterAnMCPInstall(t *testing.T) {
	v := strings.Join(ResultLines([]Result{{Topic: onboarding.MCP, Item: "Codex"}, {Topic: onboarding.Keys, Item: "Keybindings", Err: errors.New("boom")}}, 80), "\n")
	for _, want := range []string{"Codex: ok", "Keybindings: failed: boom", "Restart the agent in its pane so it loads the server.", "any key continues"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
	if v := strings.Join(ResultLines([]Result{{Topic: onboarding.MCP, Item: "Codex", Err: errors.New("x")}}, 80), "\n"); strings.Contains(v, "Restart") {
		t.Fatal("no restart line when nothing was installed")
	}
}

// fakes records what ApplyAndRecord asked for.
type fakes struct {
	applied [][2]bool
	marks   []string
	results []Result
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
			return nil
		},
	}
}

func TestApplyAndRecordRecordsOnlyWhatWentIn(t *testing.T) {
	f := &fakes{results: []Result{
		{Topic: onboarding.MCP, Item: "Claude Code", Err: errors.New("`claude mcp add` failed")},
		{Topic: onboarding.MCP, Item: "Codex"},
		{Topic: onboarding.Keys, Item: "Keybindings"},
	}}
	ApplyAndRecord(f.deps(), nil, true, true, false)
	if !slices.Equal(f.applied, [][2]bool{{true, true}}) || !slices.Equal(f.marks, []string{"keys=installed"}) {
		t.Fatalf("applied %v marks %v", f.applied, f.marks)
	}
	f2 := &fakes{results: []Result{{Topic: onboarding.MCP, Item: "Codex"}}}
	ApplyAndRecord(f2.deps(), nil, true, false, true)
	if !slices.Equal(f2.marks, []string{"mcp=installed", "keys=snoozed"}) {
		t.Fatalf("marks %v", f2.marks)
	}
}

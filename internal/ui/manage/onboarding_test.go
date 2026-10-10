package manage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"herdr-dots/internal/onboarding"
	setupui "herdr-dots/internal/ui/setup"
)

func t0() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }

// bigOffer is Claude Code plus Codex with setup-mcp's full plan text: more
// lines than a 96×32 popup shows at once.
func bigOffer() setupui.Offer {
	codex := []string{"Will edit /home/me/.codex/config.toml:"}
	for i := 0; i < 12; i++ {
		codex = append(codex, fmt.Sprintf("  existing_line_%d = true", i))
	}
	codex = append(codex, "+ # >>> herdr-dots MCP server", "+ [mcp_servers.herdr-dots]", `+ command = "/opt/herdr-dots/bin/herdr-dots"`,
		`+ args = ["mcp", "--config-dir", "/cfg", "--state-dir", "/state"]`, `+ env_vars = ["HERDR_PANE_ID", "HERDR_SOCKET_PATH"]`,
		"+ startup_timeout_sec = 20", "+ tool_timeout_sec = 180", "+ # <<< herdr-dots MCP server", "Backs up /home/me/.codex/config.toml first.")
	return setupui.Offer{
		Clients: []setupui.Client{
			{Title: "Claude Code", Change: []string{
				"Will run: claude mcp remove --scope user herdr-dots",
				"Will run: claude mcp add --scope user --env 'HERDR_PANE_ID=${HERDR_PANE_ID:-}' --env 'HERDR_SOCKET_PATH=${HERDR_SOCKET_PATH:-}' --transport stdio herdr-dots -- /opt/herdr-dots/bin/herdr-dots mcp --config-dir /cfg --state-dir /state",
				`Claude Code stores this under "mcpServers" in /home/me/.claude.json (user scope)`,
				"Backs up /home/me/.claude.json first.",
			}},
			{Title: "Codex", Change: codex},
		},
		Keys: setupui.Keys{Path: "/home/me/.config/herdr/config.toml", Change: []string{
			"prefix+d        herdr-dots: manage dots", "prefix+a        herdr-dots: toggle Dots pane",
			"prefix+shift+s  herdr-dots: summon a dot", "prefix+shift+c  herdr-dots: council (ask every dot)"}},
	}
}

type setupFakes struct {
	firstRuns, checks, applied, statuses int
	applyArgs                            [2]bool
	marks                                []string
	firstErr                             error
	noOffer                              bool
}

func (f *setupFakes) deps() setupui.Deps {
	return setupui.Deps{
		FirstRun: func() (*setupui.Offer, error) {
			f.firstRuns++
			if f.firstErr != nil || f.noOffer {
				return nil, f.firstErr
			}
			o := bigOffer().FirstRun(onboarding.Record{}, t0())
			return &o, nil
		},
		Check: func() (setupui.Offer, error) {
			f.checks++
			o := bigOffer()
			o.Clients[0].Installed, o.Clients[0].Change = true, nil
			return o, nil
		},
		Apply: func(_ any, mcp, keys bool) []setupui.Result {
			f.applied++
			f.applyArgs = [2]bool{mcp, keys}
			return []setupui.Result{{Topic: onboarding.MCP, Item: "Claude Code"}, {Topic: onboarding.MCP, Item: "Codex"}}
		},
		Record: func(s onboarding.State, ts ...onboarding.Topic) error {
			for _, t := range ts {
				f.marks = append(f.marks, string(t)+"="+string(s))
			}
			return nil
		},
	}
}

// boot opens the popup as Bubble Tea does: a size, then Init's command and
// its message.
func boot(t *testing.T, f *setupFakes, w, h int) tea.Model {
	t.Helper()
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil },
		Setup: f.deps(), MCPStatus: func() string { f.statuses++; return fmt.Sprintf("MCP: status read %d", f.statuses) }}
	var m tea.Model = New("", d)
	if f.firstRuns != 0 {
		t.Fatal("New must not detect anything")
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.Update(SnapshotMsg(lib()))
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init must return the detection command")
	}
	if f.firstRuns != 0 {
		t.Fatal("nothing is read before Init's command runs")
	}
	m, _ = m.Update(cmd())
	if f.firstRuns != 1 {
		t.Fatalf("detection ran %d times", f.firstRuns)
	}
	return m
}

func screenText(m tea.Model) string { return ansi.Strip(m.View()) }

func TestDetectionRunsInInitAndOpensTheScreen(t *testing.T) {
	f := &setupFakes{}
	m := boot(t, f, 96, 32)
	if v := screenText(m); !strings.Contains(v, "Let your agent consult your dots?") || strings.Contains(v, "DOTS LIBRARY") {
		t.Fatalf("the Setup screen opens when detection arrives:\n%s", v)
	}
	if f.applied != 0 || len(f.marks) != 0 {
		t.Fatal("nothing is written before y")
	}
}

func TestNothingToAskOpensTheLibrary(t *testing.T) {
	m := boot(t, &setupFakes{noOffer: true}, 96, 32)
	if v := screenText(m); !strings.Contains(v, "DOTS LIBRARY") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestDetectionDoesNotInterruptAnotherScreen(t *testing.T) {
	f := &setupFakes{}
	d := Deps{Caller: &fakeCaller{}, DotsDir: t.TempDir(), Check: func(context.Context, string) error { return nil }, Setup: f.deps()}
	var m tea.Model = New("", d)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 96, Height: 32})
	cmd := m.Init()
	m = press(m, "t") // the developer is already in the template list
	m, _ = m.Update(cmd())
	if v := screenText(m); strings.Contains(v, "Let your agent consult") || !strings.Contains(v, "NEW DOT FROM TEMPLATE") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestThePagerAllowsYOnlyAfterTheEndWasSeen(t *testing.T) {
	f := &setupFakes{}
	m := boot(t, f, 96, 32)
	if v := screenText(m); strings.Contains(v, "Backups are made first") {
		t.Fatalf("the test needs a screen longer than the popup:\n%s", v)
	}
	m = press(m, "y")
	if f.applied != 0 || !strings.Contains(screenText(m), "scroll to the end first") {
		t.Fatalf("y before the end was seen: applied %d\n%s", f.applied, screenText(m))
	}
	m = press(m, "x")
	if len(f.marks) != 0 {
		t.Fatalf("x before the end was seen: %v", f.marks)
	}
	for i := 0; i < 20 && !strings.Contains(screenText(m), "Backups are made first"); i++ {
		m = press(m, "space")
	}
	if !strings.Contains(screenText(m), "x don't ask again") {
		t.Fatalf("scrolled to the end:\n%s", screenText(m))
	}
	m = press(m, "y")
	if f.applied != 1 || f.applyArgs != [2]bool{true, true} {
		t.Fatalf("applied %d %v", f.applied, f.applyArgs)
	}
	if strings.Join(f.marks, ",") != "mcp=installed,keys=installed" {
		t.Fatalf("marks %v", f.marks)
	}
	if v := screenText(m); !strings.Contains(v, "Restart the agent in its pane so it loads the server.") {
		t.Fatalf("results:\n%s", v)
	}
	m = press(m, "j")
	if v := screenText(m); !strings.Contains(v, "DOTS LIBRARY") || !strings.Contains(v, "MCP: status read 2") {
		t.Fatalf("any key continues, and the footer's MCP status is read again:\n%s", v)
	}
}

func TestKTogglesTheKeybindingsInsteadOfScrolling(t *testing.T) {
	f := &setupFakes{}
	m := press(boot(t, f, 200, 200), "k")
	if v := screenText(m); !strings.Contains(v, "k include keybindings") {
		t.Fatalf("view:\n%s", v)
	}
	press(m, "y")
	if f.applyArgs != [2]bool{true, false} || strings.Join(f.marks, ",") != "mcp=installed,keys=snoozed" {
		t.Fatalf("applied %v marks %v", f.applyArgs, f.marks)
	}
}

func TestNAndEscSnoozeOnTheFirstRun(t *testing.T) {
	for _, k := range []string{"n", "esc"} {
		f := &setupFakes{}
		m := press(boot(t, f, 96, 32), k)
		if v := screenText(m); !strings.Contains(v, "DOTS LIBRARY") || strings.Join(f.marks, ",") != "mcp=snoozed,keys=snoozed" {
			t.Fatalf("%s: marks %v\n%s", k, f.marks, v)
		}
	}
}

func TestCtrlCQuitsWithoutSnoozing(t *testing.T) {
	f := &setupFakes{}
	m := boot(t, f, 96, 32)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok || len(f.marks) != 0 {
		t.Fatalf("ctrl+c quits and records nothing: %v", f.marks)
	}
}

func TestXRecordsNeverAfterTheEnd(t *testing.T) {
	f := &setupFakes{}
	m := press(boot(t, f, 200, 200), "x")
	if v := screenText(m); !strings.Contains(v, "DOTS LIBRARY") || strings.Join(f.marks, ",") != "mcp=never,keys=never" {
		t.Fatalf("marks %v\n%s", f.marks, v)
	}
}

func TestATooNarrowPopupIsTooSmall(t *testing.T) {
	f := &setupFakes{}
	m := boot(t, f, 20, 40)
	if v := screenText(m); !strings.Contains(v, "Too small") {
		t.Fatalf("view:\n%s", v)
	}
	for i := 0; i < 30; i++ {
		m = press(m, "space")
	}
	m = press(m, "y")
	if f.applied != 0 {
		t.Fatal("y on a screen too narrow to show")
	}
}

func TestADamagedOnboardingRecordIsShownInTheFooter(t *testing.T) {
	m := boot(t, &setupFakes{firstErr: errors.New("onboarding.json is not valid JSON")}, 120, 40)
	if v := screenText(m); !strings.Contains(v, "DOTS LIBRARY") || !strings.Contains(v, "onboarding.json is damaged: fix or delete it") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestSInManageOpensTheSetupScreen(t *testing.T) {
	f := &setupFakes{noOffer: true}
	m := boot(t, f, 160, 60)
	if v := screenText(m); !strings.Contains(v, "S setup") {
		t.Fatalf("the help line names S:\n%s", v)
	}
	m = press(m, "S")
	v := screenText(m)
	if f.checks != 1 || !strings.Contains(v, "Let your agent consult your dots?") || !strings.Contains(v, "Claude Code: installed") {
		t.Fatalf("checks %d view:\n%s", f.checks, v)
	}
	m = press(m, "esc")
	if v := screenText(m); !strings.Contains(v, "DOTS LIBRARY") || len(f.marks) != 0 {
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

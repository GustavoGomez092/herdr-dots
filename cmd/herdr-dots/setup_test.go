package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/onboarding"
	"herdr-dots/internal/ui/setup"
)

// onboardRig is the Setup screen's and `herdr-dots setup`'s effects with
// everything outside faked: a temp home (so no real ~/.claude*, ~/.codex
// or ~/.config/herdr), a fake claude runner (mcpSetupRig) and a fake Herdr
// reload.
type onboardRig struct {
	*mcpSetupRig
	o       onboardIO
	reloads int
}

func newOnboardRig(t *testing.T, onPath ...string) *onboardRig {
	t.Helper()
	r := &onboardRig{mcpSetupRig: newMCPSetupRig(t, onPath...)}
	r.o = onboardIO{
		s:        r.io,
		keysPath: filepath.Join(r.home, ".config", "herdr", "config.toml"),
		store:    onboarding.Store{Path: r.p.OnboardingFile(), Now: r.io.now},
		reload:   func() ([]byte, error) { r.reloads++; return []byte("ok"), nil },
	}
	return r
}

func (r *onboardRig) at(now time.Time) {
	r.o.s.now = func() time.Time { return now }
	r.o.store.Now = r.o.s.now
}

func (r *onboardRig) writeKeys(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.o.keysPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.o.keysPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// drive presses a key on the Setup screen and runs what it returns, as
// Bubble Tea would; done reports the screen handing control back.
func drive(m setup.Model, k string) (setup.Model, bool) {
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(setup.DoneMsg); ok {
			return m, true
		}
		m, cmd = m.Update(msg)
	}
	return m, false
}

func screen(t *testing.T, r *onboardRig) setup.Model {
	t.Helper()
	offer := r.o.firstRun()
	if offer == nil {
		t.Fatal("expected the Setup screen")
	}
	m, _ := setup.New(r.o.deps(), *offer, false).Update(tea.WindowSizeMsg{Width: 200, Height: 120})
	return m
}

func TestFirstOpenWithNothingInstalledShowsSetupAndWritesNothing(t *testing.T) {
	r := newOnboardRig(t, "claude", "codex")
	offer := r.o.firstRun()
	if offer == nil || !offer.AskMCP || !offer.AskKeys || len(offer.Clients) != 2 {
		t.Fatalf("offer %+v", offer)
	}
	if !strings.Contains(strings.Join(offer.Clients[0].Change, "\n"), "Will run: claude mcp add --scope user") ||
		!strings.Contains(strings.Join(offer.Clients[1].Change, "\n"), "+ "+mcpinstall.BlockBegin) {
		t.Fatalf("the change is setup-mcp's plan text: %+v", offer.Clients)
	}
	if len(offer.Keys.Change) != 4 || offer.Keys.Path != r.o.keysPath {
		t.Fatalf("keys %+v", offer.Keys)
	}
	m := screen(t, r)
	_ = m.View()
	for _, f := range []string{r.codexConfig(), r.o.keysPath, r.p.OnboardingFile(), mcpinstall.RecordPath(r.p.ConfigDir)} {
		if exists(f) {
			t.Errorf("%s written before y", f)
		}
	}
	if len(r.ran) != 0 || r.reloads != 0 {
		t.Fatalf("ran %v reloads %d before y", r.ran, r.reloads)
	}
}

func TestYRunsSetupMCPAndSetupKeysAndRecordsInstalled(t *testing.T) {
	r := newOnboardRig(t, "claude", "codex")
	m, done := drive(screen(t, r), "y")
	if done {
		t.Fatal("the results stay on screen")
	}
	ranAdd := false
	for _, a := range r.ran {
		ranAdd = ranAdd || strings.Join(a[:3], " ") == "claude mcp add"
	}
	if !ranAdd {
		t.Fatalf("the fake claude runner never got `claude mcp add`: %v", r.ran)
	}
	if b, _ := os.ReadFile(r.codexConfig()); !strings.Contains(string(b), "[mcp_servers.herdr-dots]") {
		t.Fatalf("codex config %q", b)
	}
	if b, _ := os.ReadFile(r.o.keysPath); !strings.Contains(string(b), `command = "herdr-dots.council"`) || r.reloads != 1 {
		t.Fatalf("keys %q reloads %d", b, r.reloads)
	}
	rec, _, err := r.o.store.Load()
	if err != nil || rec.MCP.State != onboarding.Installed || rec.Keys.State != onboarding.Installed {
		t.Fatalf("record %+v %v", rec, err)
	}
	if got := mcpinstall.StatusLine(r.p.ConfigDir); got != "MCP: installed for Claude Code, Codex" {
		t.Fatalf("status %q", got)
	}
	if v := m.View(); !strings.Contains(v, "Restart the agent in its pane so it loads the server.") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestYAppliesWhatTheScreenShowed(t *testing.T) {
	r := newOnboardRig(t, "codex")
	m := screen(t, r)
	if err := os.MkdirAll(filepath.Dir(r.codexConfig()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.codexConfig(), []byte("model = \"o3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ = drive(m, "y")
	if b, _ := os.ReadFile(r.codexConfig()); string(b) != "model = \"o3\"\n" {
		t.Fatalf("a config changed after the screen showed it must not be edited: %q", b)
	}
	if v := m.View(); !strings.Contains(v, "changed since the preview") {
		t.Fatalf("view:\n%s", v)
	}
	if rec, _, _ := r.o.store.Load(); rec.MCP.State == onboarding.Installed {
		t.Fatal("a failed install is not recorded as installed")
	}
}

func TestNSnoozesTheScreenForSevenDays(t *testing.T) {
	r := newOnboardRig(t, "claude")
	if _, done := drive(screen(t, r), "n"); !done {
		t.Fatal("n closes")
	}
	rec, _, _ := r.o.store.Load()
	if rec.MCP.State != onboarding.Snoozed || rec.Keys.State != onboarding.Snoozed {
		t.Fatalf("record %+v", rec)
	}
	r.at(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	if r.o.firstRun() != nil {
		t.Fatal("snoozed: no screen before 7 days")
	}
	r.at(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if r.o.firstRun() == nil {
		t.Fatal("snoozed: the screen is back after 7 days")
	}
	if len(r.ran) != 0 || exists(r.o.keysPath) {
		t.Fatal("n installs nothing")
	}
}

func TestXRecordsNever(t *testing.T) {
	r := newOnboardRig(t, "claude")
	if _, done := drive(screen(t, r), "x"); !done {
		t.Fatal("x closes")
	}
	rec, _, _ := r.o.store.Load()
	if rec.MCP.State != onboarding.Never || rec.Keys.State != onboarding.Never {
		t.Fatalf("record %+v", rec)
	}
	r.at(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if r.o.firstRun() != nil {
		t.Fatal("never means never")
	}
}

func TestMCPAlreadyInstalledMeansNoScreenAndIsRecorded(t *testing.T) {
	r := newOnboardRig(t, "codex")
	if err := setupMCP([]string{"--yes"}, r.io); err != nil { // installed another way
		t.Fatal(err)
	}
	r.writeKeys(t, "[[keys.command]]\nkey = \"prefix+m\"\ntype = \"plugin_action\"\ncommand = \"herdr-dots.manage\"\n")
	if o := r.o.firstRun(); o != nil {
		t.Fatalf("everything installed: no screen, got %+v", o)
	}
	rec, _, err := r.o.store.Load()
	if err != nil || rec.MCP.State != onboarding.Installed {
		t.Fatalf("an install by any route is recorded: %+v %v", rec, err)
	}
}

func TestANewerRecordMeansNoScreenAndNoWrite(t *testing.T) {
	r := newOnboardRig(t, "codex")
	if err := setupMCP([]string{"--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	newer := `{"version":2,"mcp":{"state":"later"},"keys":{}}`
	if err := os.WriteFile(r.p.OnboardingFile(), []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	if o := r.o.firstRun(); o != nil {
		t.Fatalf("a newer record: no screen, got %+v", o)
	}
	if b, _ := os.ReadFile(r.p.OnboardingFile()); string(b) != newer {
		t.Fatalf("a newer record is never rewritten: %s", b)
	}
}

// `herdr-dots setup` runs both plans as setup-mcp and setup-keys print
// them, and asks once.
func TestSetupCommandRunsBothPlansAndAsksOnce(t *testing.T) {
	r := newOnboardRig(t, "claude", "codex")
	r.io.in = strings.NewReader("y\n")
	r.o.s = r.io
	if err := setupCmd(nil, r.o); err != nil {
		t.Fatal(err)
	}
	out := r.out.String()
	for _, want := range []string{"== Claude Code ==", "Will run: claude mcp add --scope user", "== Codex ==",
		"== Keybindings ==", "This will be appended to " + r.o.keysPath, "Installed. Restart Claude Code or Codex",
		"Appended. There was no config to back up.", "Herdr config reloaded."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "[y/N]"); n != 1 {
		t.Fatalf("asked %d times:\n%s", n, out)
	}
	rec, _, _ := r.o.store.Load()
	if rec.MCP.State != onboarding.Installed || rec.Keys.State != onboarding.Installed {
		t.Fatalf("record %+v", rec)
	}
}

func TestSetupCommandCancelChangesNothing(t *testing.T) {
	r := newOnboardRig(t, "claude", "codex")
	r.io.in = strings.NewReader("\n")
	r.o.s = r.io
	if err := setupCmd(nil, r.o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.out.String(), "Cancelled. Nothing changed.") || len(r.ran) != 0 ||
		exists(r.codexConfig()) || exists(r.o.keysPath) || exists(r.p.OnboardingFile()) {
		t.Fatalf("cancel must change nothing: ran %v\n%s", r.ran, r.out.String())
	}
}

func TestSetupCommandFlags(t *testing.T) {
	r := newOnboardRig(t, "codex")
	if err := setupCmd([]string{"--yes", "--no-keys"}, r.o); err != nil {
		t.Fatal(err)
	}
	if !exists(r.codexConfig()) || exists(r.o.keysPath) || strings.Contains(r.out.String(), "Keybindings") {
		t.Fatalf("--no-keys:\n%s", r.out.String())
	}
	r2 := newOnboardRig(t, "claude", "codex")
	if err := setupCmd([]string{"--yes", "--no-mcp"}, r2.o); err != nil {
		t.Fatal(err)
	}
	if len(r2.ran) != 0 || exists(r2.codexConfig()) || !exists(r2.o.keysPath) {
		t.Fatalf("--no-mcp: ran %v\n%s", r2.ran, r2.out.String())
	}
	// Neither agent CLI: the keybindings still go in.
	r3 := newOnboardRig(t)
	if err := setupCmd([]string{"--yes"}, r3.o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r3.out.String(), "neither Claude Code nor Codex found") || !exists(r3.o.keysPath) {
		t.Fatalf("no clients:\n%s", r3.out.String())
	}
}

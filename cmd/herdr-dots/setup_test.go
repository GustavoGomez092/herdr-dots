package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	looks   int
}

func newOnboardRig(t *testing.T, onPath ...string) *onboardRig {
	t.Helper()
	r := &onboardRig{mcpSetupRig: newMCPSetupRig(t, onPath...)}
	look := r.io.lookPath
	r.io.lookPath = func(bin string) (string, error) { r.looks++; return look(bin) }
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

// first runs the manage popup's first-run detection (it may record an
// install found by another route).
func (r *onboardRig) first(t *testing.T) *setup.Offer {
	t.Helper()
	o, err := r.o.firstRun(true)
	if err != nil {
		t.Fatalf("firstRun: %v", err)
	}
	return o
}

// pressY applies what y on the screen would, with the plan it showed.
func (r *onboardRig) pressY(o setup.Offer) []setup.Result {
	mcp, keys, snooze := setup.Screen{Offer: o, IncludeKeys: true}.Install()
	return setup.ApplyAndRecord(r.o.deps(), o.Plan, mcp, keys, snooze)
}

func TestNothingIsReadBeforeTheDetectionRuns(t *testing.T) {
	r := newOnboardRig(t, "claude", "codex")
	d := r.o.deps()
	if r.looks != 0 || d.FirstRun == nil {
		t.Fatalf("building the deps looked up %d programs", r.looks)
	}
	if _, err := d.FirstRun(); err != nil || r.looks == 0 {
		t.Fatalf("FirstRun detects: looks %d err %v", r.looks, err)
	}
}

func TestFirstOpenWithNothingInstalledOffersEverythingAndWritesNothing(t *testing.T) {
	r := newOnboardRig(t, "claude", "codex")
	if err := os.WriteFile(r.io.env.ClaudeConfigPath(), []byte(`{"numStartups": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	offer := r.first(t)
	if offer == nil || !offer.AskMCP || !offer.AskKeys || len(offer.Clients) != 2 {
		t.Fatalf("offer %+v", offer)
	}
	claude := strings.Join(offer.Clients[0].Change, "\n")
	cfg := r.io.env.ClaudeConfigPath()
	for _, want := range []string{"Will run: claude mcp add --scope user", `stores this under "mcpServers" in ` + cfg + " (user scope)", "Backs up " + cfg + " first."} {
		if !strings.Contains(claude, want) {
			t.Errorf("Claude's change lacks %q:\n%s", want, claude)
		}
	}
	if !strings.Contains(strings.Join(offer.Clients[1].Change, "\n"), "+ "+mcpinstall.BlockBegin) {
		t.Fatalf("Codex's change is setup-mcp's diff: %+v", offer.Clients[1])
	}
	if len(offer.Keys.Change) != 4 || offer.Keys.Path != r.o.keysPath {
		t.Fatalf("keys %+v", offer.Keys)
	}
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
	results := r.pressY(*r.first(t))
	for _, res := range results {
		if res.Err != nil {
			t.Fatalf("%s: %v", res.Item, res.Err)
		}
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
}

func TestYAppliesWhatTheScreenShowed(t *testing.T) {
	r := newOnboardRig(t, "codex")
	offer := r.first(t)
	if err := os.MkdirAll(filepath.Dir(r.codexConfig()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.codexConfig(), []byte("model = \"o3\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	results := r.pressY(*offer)
	if b, _ := os.ReadFile(r.codexConfig()); string(b) != "model = \"o3\"\n" {
		t.Fatalf("a config changed after the screen showed it must not be edited: %q", b)
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "changed since the preview") {
		t.Fatalf("results %+v", results)
	}
	if rec, _, _ := r.o.store.Load(); rec.MCP.State == onboarding.Installed {
		t.Fatal("a failed install is not recorded as installed")
	}
}

// Fix round 1: the keybindings plan is checked like setup-mcp's: Herdr's
// config changed between the preview and y means nothing is appended.
func TestAStaleKeysPlanIsRefused(t *testing.T) {
	r := newOnboardRig(t)
	r.writeKeys(t, "[keys]\nprefix = \"ctrl+b\"\n")
	offer := r.first(t)
	edited := "[keys]\nprefix = \"ctrl+a\"\n"
	r.writeKeys(t, edited)
	results := r.pressY(*offer)
	if len(results) != 1 || results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "changed since the preview; nothing changed") {
		t.Fatalf("results %+v", results)
	}
	if b, _ := os.ReadFile(r.o.keysPath); string(b) != edited || r.reloads != 0 {
		t.Fatalf("nothing may be appended: %q reloads %d", b, r.reloads)
	}
	if rec, _, _ := r.o.store.Load(); rec.Keys.State == onboarding.Installed {
		t.Fatal("not recorded as installed")
	}
}

func TestASnoozeHidesTheOfferForSevenDays(t *testing.T) {
	r := newOnboardRig(t, "claude")
	if err := r.o.store.Mark(onboarding.Snoozed, onboarding.MCP, onboarding.Keys); err != nil {
		t.Fatal(err)
	}
	r.at(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	if r.first(t) != nil {
		t.Fatal("snoozed: no screen before 7 days")
	}
	r.at(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if r.first(t) == nil {
		t.Fatal("snoozed: the screen is back after 7 days")
	}
}

// Fix round 1: when the record forbids both topics, nothing is checked.
func TestNeverForBothSkipsTheDetection(t *testing.T) {
	r := newOnboardRig(t, "claude")
	if err := r.o.store.Mark(onboarding.Never, onboarding.MCP, onboarding.Keys); err != nil {
		t.Fatal(err)
	}
	r.looks = 0
	if r.first(t) != nil || r.looks != 0 {
		t.Fatalf("never for both: no screen and no detection (looks %d)", r.looks)
	}
}

func TestMCPAlreadyInstalledMeansNoScreenAndIsRecorded(t *testing.T) {
	r := newOnboardRig(t, "codex")
	if err := setupMCP([]string{"--yes"}, r.io); err != nil { // installed another way
		t.Fatal(err)
	}
	r.writeKeys(t, "[[keys.command]]\nkey = \"prefix+m\"\ntype = \"plugin_action\"\ncommand = \"herdr-dots.manage\"\n")
	if o := r.first(t); o != nil {
		t.Fatalf("everything installed: no screen, got %+v", o)
	}
	rec, _, err := r.o.store.Load()
	if err != nil || rec.MCP.State != onboarding.Installed {
		t.Fatalf("an install by any route is recorded: %+v %v", rec, err)
	}
}

// Ruling P10-R14: the Dots pane's check records nothing.
func TestThePaneCheckRecordsNothing(t *testing.T) {
	r := newOnboardRig(t, "codex")
	if err := setupMCP([]string{"--yes"}, r.io); err != nil {
		t.Fatal(err)
	}
	if !r.o.pending() {
		t.Fatal("the keybindings are still missing: pending")
	}
	if exists(r.p.OnboardingFile()) {
		t.Fatal("the pane wrote onboarding.json")
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
	if o := r.first(t); o != nil {
		t.Fatalf("a newer record: no screen, got %+v", o)
	}
	if b, _ := os.ReadFile(r.p.OnboardingFile()); string(b) != newer {
		t.Fatalf("a newer record is never rewritten: %s", b)
	}
}

func TestADamagedRecordIsReported(t *testing.T) {
	r := newOnboardRig(t, "codex")
	if err := os.WriteFile(r.p.OnboardingFile(), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if o, err := r.o.firstRun(true); o != nil || !errors.Is(err, onboarding.ErrCorrupt) {
		t.Fatalf("offer %+v err %v", o, err)
	}
	if b, _ := os.ReadFile(r.p.OnboardingFile()); string(b) != "{nope" {
		t.Fatalf("rewritten: %s", b)
	}
}

// Fix round 1: a setup child on the UI path that hangs is stopped after its
// timeout, and the timeout is an error the screen shows.
func TestAHangingSetupChildTimesOut(t *testing.T) {
	t.Setenv("HERDR_DOTS_TEST_SLEEP", "1")
	start := time.Now()
	_, err := setupChildRunner(200 * time.Millisecond)([]string{os.Args[0], "-test.run=^TestSetupChildSleeps$"})
	if err == nil || !strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the child was not stopped")
	}
}

// TestSetupChildSleeps is not a test: TestAHangingSetupChildTimesOut starts
// this test binary as a hanging child.
func TestSetupChildSleeps(t *testing.T) {
	if os.Getenv("HERDR_DOTS_TEST_SLEEP") != "1" {
		t.Skip("helper process for TestAHangingSetupChildTimesOut")
	}
	time.Sleep(30 * time.Second)
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

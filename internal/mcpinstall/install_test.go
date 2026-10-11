package mcpinstall

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-dots/internal/platform/platformtest"
)

// tRoot is "" on Unix and the temp dir's drive ("C:") on Windows, so the
// paths below are absolute everywhere (checkInputs) and hold no backslash,
// which keeps the expected TOML and JSON byte-exact on every OS.
var tRoot = filepath.VolumeName(os.TempDir())

var (
	tBin   = tRoot + "/opt/herdr dots/bin/herdr-dots"
	tCfg   = tRoot + "/home/u/.config/herdr/plugins/herdr-dots"
	tState = tRoot + "/home/u/.local/state/herdr/plugins/herdr-dots"
)

var tNow = time.Date(2026, 10, 8, 14, 30, 0, 0, time.UTC)

// tempEnv points both clients at a temp home; never the real one.
func tempEnv(t *testing.T) Env {
	t.Helper()
	return Env{Getenv: func(string) string { return "" }, Home: t.TempDir()}
}

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o640); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// symlinkOrSkip creates a symlink, skipping the test where the OS refuses
// (Windows without Developer Mode or admin rights).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func noRun(t *testing.T) func(Command) error {
	return func(c Command) error { t.Fatalf("unexpected command %v", c.Args); return nil }
}

const userCodex = `# Codex config, hand edited
model = "gpt-6.1-sol"

[mcp_servers.docs]
command = "docs-mcp"   # keep me
args = ["--port", "0"]

[profiles.fast]
model = "gpt-6-luna"
`

var wantBlock = "# >>> herdr-dots MCP server (managed by `herdr-dots setup-mcp`; `herdr-dots setup-mcp --remove` deletes it)\n" +
	"[mcp_servers.herdr-dots]\n" +
	"command = \"" + tBin + "\"\n" +
	"args = [\"mcp\", \"--config-dir\", \"" + tCfg + "\", \"--state-dir\", \"" + tState + "\"]\n" +
	"env_vars = [\"HERDR_PANE_ID\", \"HERDR_SOCKET_PATH\"]\n" +
	"startup_timeout_sec = 20\n" +
	"tool_timeout_sec = 180\n" +
	"# <<< herdr-dots MCP server\n"

func TestClaudePlanIsTheExactCLICommand(t *testing.T) {
	env := tempEnv(t)
	writeFile(t, env.ClaudeConfigPath(), `{"numStartups": 3}`)
	p, err := PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "mcp", "add", "--scope", "user",
		"--env", "HERDR_PANE_ID=${HERDR_PANE_ID:-}", "--env", "HERDR_SOCKET_PATH=${HERDR_SOCKET_PATH:-}",
		"--transport", "stdio", "herdr-dots", "--", tBin, "mcp", "--config-dir", tCfg, "--state-dir", tState}
	if len(p.Commands) != 2 || !p.Commands[0].MayFail || !slices.Equal(p.Commands[0].Args, []string{"claude", "mcp", "remove", "--scope", "user", "herdr-dots"}) ||
		p.Commands[1].MayFail || !slices.Equal(p.Commands[1].Args, want) {
		t.Fatalf("commands %+v", p.Commands)
	}
	if !slices.Equal(p.Backups, []string{env.ClaudeConfigPath()}) || len(p.Edits) != 0 {
		t.Fatalf("plan %+v: Claude's file is backed up and edited only by the claude CLI", p)
	}
	if !strings.Contains(p.Preview, `+   "herdr-dots": {`) || !strings.Contains(p.Preview, `"HERDR_PANE_ID": "${HERDR_PANE_ID:-}"`) {
		t.Fatalf("preview:\n%s", p.Preview)
	}
}

func TestClaudeConfigDirIsHonored(t *testing.T) {
	dir := t.TempDir()
	env := Env{Getenv: func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return dir
		}
		return ""
	}, Home: t.TempDir()}
	if got := env.ClaudeConfigPath(); got != filepath.Join(dir, ".claude.json") {
		t.Fatalf("path %s", got)
	}
}

// Review Focus 5: the user's Codex config keeps its other tables and
// comments, is backed up, and --remove restores it byte for byte.
func TestCodexInstallPreservesOtherTablesAndRemoveRestores(t *testing.T) {
	env := tempEnv(t)
	path := env.CodexConfigPath()
	writeFile(t, path, userCodex)
	before, _ := os.Stat(path) // 0640 on Unix; Windows reports 0666
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Preview, "+ [mcp_servers.herdr-dots]") || strings.Contains(p.Preview, "- ") {
		t.Fatalf("preview:\n%s", p.Preview)
	}
	backups, err := Apply(p, noRun(t), tNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != userCodex+"\n"+wantBlock {
		t.Fatalf("after install:\n%s", got)
	}
	if len(backups) != 1 || backups[0] != path+".bak-herdr-dots-20261008-143000" || readFile(t, backups[0]) != userCodex {
		t.Fatalf("backups %v", backups)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("mode %v -> %v: the file keeps its mode", before.Mode().Perm(), st.Mode().Perm())
	}
	platformtest.AssertPrivateFile(t, backups[0])

	again, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil || !again.Nothing {
		t.Fatalf("re-install must be a no-op: %+v %v", again, err)
	}

	rm, err := PlanRemove(ClientCodex, env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(rm, noRun(t), tNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != userCodex {
		t.Fatalf("remove must restore the original exactly:\n%q", got)
	}
	if rm2, _ := PlanRemove(ClientCodex, env); !rm2.Nothing {
		t.Fatalf("second remove: %+v", rm2)
	}
}

func TestCodexInstallIntoAMissingFile(t *testing.T) {
	env := tempEnv(t)
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Backups) != 0 {
		t.Fatalf("nothing to back up: %v", p.Backups)
	}
	if _, err := Apply(p, noRun(t), tNow); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, env.CodexConfigPath()); got != wantBlock {
		t.Fatalf("got:\n%s", got)
	}
	platformtest.AssertPrivateFile(t, env.CodexConfigPath()) // a new config is private
}

func TestCodexUnmarkedTableIsReplacedInPlace(t *testing.T) {
	before := "[mcp_servers.herdr-dots]\ncommand = \"/old/herdr-dots\"\nargs = [\"mcp\"]\n\n[mcp_servers.herdr-dots.env]\nX = \"1\"\n\n# profiles\n[profiles.fast]\nmodel = \"m\"\n"
	after, err := UpsertCodexBlock(before, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	if want := wantBlock + "\n# profiles\n[profiles.fast]\nmodel = \"m\"\n"; after != want {
		t.Fatalf("got:\n%s\nwant:\n%s", after, want)
	}
}

func TestCodexRefusesWhatItCannotEditSafely(t *testing.T) {
	for name, before := range map[string]string{
		"inline table":  "[mcp_servers]\nherdr-dots = { command = \"x\" }\n",
		"dotted keys":   "mcp_servers.herdr-dots.command = \"x\"\n",
		"invalid TOML":  "[mcp_servers.docs\ncommand = ",
		"no end marker": BlockBegin + "\n[mcp_servers.herdr-dots]\ncommand = \"x\"\n",
	} {
		if _, err := UpsertCodexBlock(before, tBin, tCfg, tState); !errors.Is(err, ErrUnsafeEdit) {
			t.Errorf("%s: install got %v", name, err)
		}
		if _, _, err := RemoveCodexBlock(before); !errors.Is(err, ErrUnsafeEdit) {
			t.Errorf("%s: remove got %v", name, err)
		}
	}
}

func TestApplyRefusesAFileChangedAfterThePreview(t *testing.T) {
	env := tempEnv(t)
	path := env.CodexConfigPath()
	writeFile(t, path, userCodex)
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, userCodex+"# edited meanwhile\n")
	if _, err := Apply(p, noRun(t), tNow); err == nil || !strings.Contains(err.Error(), "changed since the preview") {
		t.Fatalf("got %v", err)
	}
	if got := readFile(t, path); got != userCodex+"# edited meanwhile\n" {
		t.Fatal("the file must stay untouched")
	}
	if m, _ := filepath.Glob(path + ".bak-*"); len(m) != 0 {
		t.Fatalf("no backup without a change: %v", m)
	}
}

func TestApplyRunsCommandsAndIgnoresOnlyMayFail(t *testing.T) {
	env := tempEnv(t)
	p, _ := PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	var ran [][]string
	_, err := Apply(p, func(c Command) error {
		ran = append(ran, c.Args)
		if c.MayFail {
			return errors.New("No MCP server found")
		}
		return nil
	}, tNow)
	if err != nil || len(ran) != 2 {
		t.Fatalf("ran %v err %v", ran, err)
	}
	_, err = Apply(p, func(c Command) error {
		if !c.MayFail {
			return errors.New("exit status 1")
		}
		return nil
	}, tNow)
	if err == nil || !strings.Contains(err.Error(), "claude mcp add") {
		t.Fatalf("a failing add must fail the apply: %v", err)
	}
}

func TestPlanRefusesRelativeOrControlPaths(t *testing.T) {
	env := tempEnv(t)
	for _, bin := range []string{"bin/herdr-dots", tRoot + "/bin/herdr\n-dots"} {
		if _, err := PlanInstall(ClientCodex, env, bin, tCfg, tState); err == nil {
			t.Errorf("bin %q accepted", bin)
		}
	}
}

func TestRecordsAndStatusLine(t *testing.T) {
	dir := t.TempDir()
	if got := StatusLine(dir); got != "MCP: not installed (run herdr-dots setup-mcp)" {
		t.Fatalf("status %q", got)
	}
	rs := WithRecord(nil, Record{Client: ClientCodex, Scope: "user", At: tNow})
	rs = WithRecord(rs, Record{Client: ClientClaude, Scope: "user", At: tNow})
	rs = WithRecord(rs, Record{Client: ClientCodex, Scope: "user", At: tNow.Add(time.Hour)})
	if err := SaveRecords(RecordPath(dir), rs); err != nil {
		t.Fatal(err)
	}
	if got := StatusLine(dir); got != "MCP: installed for Claude Code, Codex" {
		t.Fatalf("status %q", got)
	}
	loaded, err := LoadRecords(RecordPath(dir))
	if err != nil || len(loaded) != 2 {
		t.Fatalf("loaded %+v %v", loaded, err)
	}
	platformtest.AssertPrivateFile(t, RecordPath(dir))
	if err := SaveRecords(RecordPath(dir), WithoutClient(loaded, ClientClaude)); err != nil {
		t.Fatal(err)
	}
	if got := StatusLine(dir); got != "MCP: installed for Codex" {
		t.Fatalf("status %q", got)
	}
}

func TestDiffShowsChangesWithContext(t *testing.T) {
	d := Diff("a\nb\nc\nd\ne\nf\n", "a\nb\nc\nX\ne\nf\n")
	if d != "  b\n  c\n- d\n+ X\n  e\n  f\n" {
		t.Fatalf("diff:\n%s", d)
	}
}

func claudeEntryJSON(t *testing.T, env Env, entry string) {
	t.Helper()
	writeFile(t, env.ClaudeConfigPath(), `{"numStartups": 3, "mcpServers": {"other": {"type": "stdio", "command": "x"}, "herdr-dots": `+entry+`}}`)
}

func TestClaudeRemoveIsNothingWhenNotInstalled(t *testing.T) {
	env := tempEnv(t)
	for name, setup := range map[string]func(){
		"missing file": func() {},
		"no entry":     func() { writeFile(t, env.ClaudeConfigPath(), `{"mcpServers": {"other": {}}}`) },
	} {
		setup()
		p, err := PlanRemove(ClientClaude, env)
		if err != nil || !p.Nothing || len(p.Commands) != 0 || len(p.Backups) != 0 {
			t.Errorf("%s: %+v %v", name, p, err)
		}
	}
}

func TestClaudeRemoveWhenInstalledRunsTheCLIAfterABackup(t *testing.T) {
	env := tempEnv(t)
	claudeEntryJSON(t, env, `{"type": "stdio"}`)
	p, err := PlanRemove(ClientClaude, env)
	if err != nil || p.Nothing || len(p.Commands) != 1 || p.Commands[0].MayFail ||
		!slices.Equal(p.Commands[0].Args, []string{"claude", "mcp", "remove", "--scope", "user", "herdr-dots"}) ||
		!slices.Equal(p.Backups, []string{env.ClaudeConfigPath()}) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestClaudeInstallIsNothingWhenTheEntryAlreadyMatches(t *testing.T) {
	env := tempEnv(t)
	entry := `{"type":"stdio","command":"` + tBin + `","args":["mcp","--config-dir","` + tCfg + `","--state-dir","` + tState + `"],"env":{"HERDR_PANE_ID":"${HERDR_PANE_ID:-}","HERDR_SOCKET_PATH":"${HERDR_SOCKET_PATH:-}"}}`
	claudeEntryJSON(t, env, entry)
	p, err := PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	if err != nil || !p.Nothing || len(p.Backups) != 0 || len(p.Commands) != 0 {
		t.Fatalf("a matching entry is a no-op with no backup: %+v %v", p, err)
	}
	claudeEntryJSON(t, env, strings.Replace(entry, tBin, "/old/herdr-dots", 1))
	p, err = PlanInstall(ClientClaude, env, tBin, tCfg, tState)
	if err != nil || p.Nothing || len(p.Backups) != 1 {
		t.Fatalf("a different entry is replaced: %+v %v", p, err)
	}
}

func TestCodexNoTrailingNewlineRoundTrips(t *testing.T) {
	before := "model = \"x\""
	after, err := UpsertCodexBlock(before, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	back, found, err := RemoveCodexBlock(after)
	if err != nil || !found || back != before {
		t.Fatalf("got %q %v %v", back, found, err)
	}
}

func TestCodexCRLFFilesGetACRLFBlockAndRoundTrip(t *testing.T) {
	for _, before := range []string{"model = \"x\"\r\n[a]\r\nk = 1\r\n", "model = \"x\"\r\n[a]\r\nk = 1"} {
		after, err := UpsertCodexBlock(before, tBin, tCfg, tState)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(after, "\n") != strings.Count(after, "\r\n") {
			t.Fatalf("bare LF in a CRLF file: %q", after)
		}
		back, found, err := RemoveCodexBlock(after)
		if err != nil || !found || back != before {
			t.Fatalf("got %q want %q (%v %v)", back, before, found, err)
		}
	}
}

func TestCodexMarkersInsideAStringAreRefused(t *testing.T) {
	before := "note = '''\n" + BlockBegin + "\n[mcp_servers.herdr-dots]\ncommand = \"x\"\n" + BlockEnd + "\n'''\n"
	if _, _, err := RemoveCodexBlock(before); !errors.Is(err, ErrUnsafeEdit) {
		t.Errorf("remove: %v", err)
	}
	if _, err := UpsertCodexBlock(before, tBin, tCfg, tState); !errors.Is(err, ErrUnsafeEdit) {
		t.Errorf("install: %v", err)
	}
	onlyBegin := "note = '''\n" + BlockBegin + "\n'''\n"
	_, err := UpsertCodexBlock(onlyBegin, tBin, tCfg, tState)
	if !errors.Is(err, ErrUnsafeEdit) || !strings.Contains(err.Error(), "string") {
		t.Errorf("begin marker only in a string: %v", err)
	}
}

func TestCodexRefusalMessagesNameTheForm(t *testing.T) {
	for want, before := range map[string]string{
		"array of tables": "[[mcp_servers.herdr-dots]]\ncommand = \"x\"\n",
		"inline table":    "[mcp_servers]\nherdr-dots = { command = \"x\" }\n",
	} {
		_, err := UpsertCodexBlock(before, tBin, tCfg, tState)
		if !errors.Is(err, ErrUnsafeEdit) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
	quoted := "[\"mcp_servers\".\"herdr-dots\"]\ncommand = \"/old\"\n"
	after, err := UpsertCodexBlock(quoted, tBin, tCfg, tState)
	if err != nil || after != wantBlock {
		t.Fatalf("a quoted-key header is replaced in place: %q %v", after, err)
	}
}

func TestDanglingSymlinkConfigIsRefused(t *testing.T) {
	env := tempEnv(t)
	path := env.CodexConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, filepath.Join(t.TempDir(), "nope.toml"), path)
	for _, plan := range []func() (Plan, error){
		func() (Plan, error) { return PlanInstall(ClientCodex, env, tBin, tCfg, tState) },
		func() (Plan, error) { return PlanRemove(ClientCodex, env) },
	} {
		if _, err := plan(); err == nil || !strings.Contains(err.Error(), "dangling") {
			t.Fatalf("got %v", err)
		}
	}
	if st, err := os.Lstat(path); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link must stay a link")
	}
}

func TestSymlinkedConfigIsEditedThroughTheLink(t *testing.T) {
	env := tempEnv(t)
	target := filepath.Join(t.TempDir(), "dotfiles", "codex.toml")
	writeFile(t, target, userCodex)
	path := env.CodexConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, target, path)
	p, err := PlanInstall(ClientCodex, env, tBin, tCfg, tState)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(p, noRun(t), tNow); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Lstat(path); st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced")
	}
	if got := readFile(t, target); got != userCodex+"\n"+wantBlock {
		t.Fatalf("target:\n%s", got)
	}
}

// Herdr may start the plugin with a \\?\ path on Windows: an entry recorded
// with that prefix is the same command as one without it.
func TestClaudeEntryWithALongPathPrefixStillMatches(t *testing.T) {
	bin := `C:\Users\me\herdr-dots\bin\herdr-dots.exe`
	e := claudeEntry{Type: "stdio", Command: `\\?\` + bin, Args: ServerArgs(tCfg, tState), Env: claudeEnv()}
	if !claudeEntryMatches(e, bin, tCfg, tState) {
		t.Fatal(`an entry whose command has the \\?\ prefix must match the same binary without it`)
	}
	e.Command = bin
	if !claudeEntryMatches(e, `\\?\`+bin, tCfg, tState) {
		t.Fatal(`a \\?\ binary path must match the entry without the prefix`)
	}
}

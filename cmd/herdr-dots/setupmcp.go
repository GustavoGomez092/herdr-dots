package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/platform"
)

// setupIO is everything setup-mcp touches, so tests can fake it.
type setupIO struct {
	in       io.Reader
	out      io.Writer
	run      func(args []string) (string, error) // combined output
	lookPath func(string) (string, error)
	env      mcpinstall.Env
	now      func() time.Time
	exe      string // absolute path of the herdr-dots binary to install
	paths    func() (paths.Paths, error)
}

// newSetupIO is setup-mcp's real effects: this binary (symlinks resolved),
// the user's home, and child processes started by runSetupChild.
func newSetupIO(in io.Reader, out io.Writer) (setupIO, error) {
	exe, err := os.Executable()
	if err != nil {
		return setupIO{}, err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	home, _ := os.UserHomeDir()
	return setupIO{
		in: in, out: out,
		run:      runSetupChild,
		lookPath: platform.LookPath,
		env:      mcpinstall.Env{Getenv: os.Getenv, Home: home},
		now:      time.Now,
		exe:      exe,
		paths:    paths.Discover,
	}, nil
}

func runSetupMCP(args []string) error {
	s, err := newSetupIO(os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	return setupMCP(args, s)
}

// runSetupChild runs one setup command (claude mcp add/remove/get, codex mcp
// list) and returns its combined output. platform.Command runs an npm
// claude.cmd/codex.cmd shim through cmd.exe with every argument quoted, and
// refuses a path cmd.exe can't carry (a %, " or !) before anything runs. The
// child gets no CLAUDE* variable but CLAUDE_CONFIG_DIR
// (platform.WithoutClaudeEnv): a plugin process can inherit a Claude Code
// session's variables, and `claude mcp add` must not join that session.
func runSetupChild(a []string) (string, error) { return runSetupChildCtx(context.Background(), a) }

func runSetupChildCtx(ctx context.Context, a []string) (string, error) {
	cmd, err := platform.CommandContext(ctx, a[0], a[1:])
	if err != nil {
		return "", err
	}
	cmd.Env = platform.ChildEnv(platform.WithoutClaudeEnv(os.Environ()))
	cmd.WaitDelay = 2 * time.Second // a grandchild holding the output can't keep a killed child's Wait open
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// uiChildTimeout bounds each child the Setup screen starts: a client CLI
// that hangs (a login prompt, a dead network) can't hold the popup.
const uiChildTimeout = 60 * time.Second

// setupChildRunner is runSetupChild with a timeout per child; a timeout is
// an error that names the command.
func setupChildRunner(timeout time.Duration) func([]string) (string, error) {
	return func(a []string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := runSetupChildCtx(ctx, a)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return out, fmt.Errorf("`%s` timed out after %s", strings.Join(a[:min(3, len(a))], " "), timeout)
		}
		return out, err
	}
}

func setupMCP(args []string, s setupIO) error {
	fs := flag.NewFlagSet("setup-mcp", flag.ContinueOnError)
	client := fs.String("client", "both", "claude, codex or both")
	remove := fs.Bool("remove", false, "remove the server instead of installing it")
	yes := fs.Bool("yes", false, "apply without asking")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := s.planMCP(*client, *remove)
	if err != nil {
		return err
	}
	if m.print() == 0 {
		fmt.Fprintln(s.out, "\nNothing changed.")
		return m.nothingToDo()
	}
	if !*yes {
		fmt.Fprint(s.out, "\nApply these changes? [y/N] ")
		line, _ := bufio.NewReader(s.in).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Fprintln(s.out, "Cancelled. Nothing changed.")
			return nil
		}
	}
	if err := m.apply(); err != nil {
		return err
	}
	m.printDone()
	return nil
}

// mcpSetup is setup-mcp's plan for the clients it handles. Planning,
// printing and applying are separate steps, shared by setup-mcp, setup and
// the Setup screen, so all three make the same change.
type mcpSetup struct {
	s      setupIO
	p      paths.Paths
	remove bool
	plans  []mcpinstall.Plan
}

// goRunErr refuses a `go run` binary: the clients would point at a
// temporary file.
func (s setupIO) goRunErr() error {
	if strings.Contains(s.exe, string(filepath.Separator)+"go-build") {
		return errors.New("run the built plugin binary (./bin/herdr-dots setup-mcp), not `go run`: the clients would point at a temporary file")
	}
	return nil
}

// recordsNewerErr refuses an install record file a newer herdr-dots wrote:
// it can't be updated afterwards, so nothing is planned, asked or changed.
func recordsNewerErr(p paths.Paths) error {
	if _, err := mcpinstall.LoadRecords(mcpinstall.RecordPath(p.ConfigDir)); errors.Is(err, mcpinstall.ErrRecordsVersion) {
		return fmt.Errorf("%w; nothing changed: update herdr-dots first", err)
	}
	return nil
}

// errNoClients: neither client CLI (nor a Codex config) was found.
var errNoClients = errors.New("neither Claude Code nor Codex found; nothing changed")

// planMCP plans installing (or removing) the server for --client. Nothing
// is changed.
func (s setupIO) planMCP(client string, remove bool) (*mcpSetup, error) {
	if err := s.goRunErr(); err != nil {
		return nil, err
	}
	clients, err := s.pickClients(client, remove)
	if err != nil {
		return nil, err
	}
	p, err := s.paths()
	if err != nil {
		return nil, err
	}
	if err := recordsNewerErr(p); err != nil {
		return nil, err
	}
	m := &mcpSetup{s: s, p: p, remove: remove}
	for _, c := range clients {
		var pl mcpinstall.Plan
		if remove {
			pl, err = mcpinstall.PlanRemove(c, s.env)
		} else {
			pl, err = mcpinstall.PlanInstall(c, s.env, s.exe, p.ConfigDir, p.StateDir)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Title(), err)
		}
		m.plans = append(m.plans, pl)
	}
	return m, nil
}

// print writes the plan as setup-mcp shows it and returns how many clients
// have something to do.
func (m *mcpSetup) print() int {
	todo := 0
	for _, pl := range m.plans {
		fmt.Fprintf(m.s.out, "\n== %s ==\n", pl.Client.Title())
		if pl.Nothing {
			fmt.Fprintf(m.s.out, "Nothing to do: %s", pl.Preview)
			continue
		}
		todo++
		for _, l := range mcpWillLines(pl) {
			fmt.Fprintln(m.s.out, l)
		}
		fmt.Fprint(m.s.out, pl.Preview)
		for _, b := range pl.Backups {
			fmt.Fprintf(m.s.out, "Backs up %s first.\n", b)
		}
	}
	return todo
}

// mcpWillLines are a plan's "Will run" and "Will edit" lines.
func mcpWillLines(pl mcpinstall.Plan) []string {
	var out []string
	for _, c := range pl.Commands {
		out = append(out, "Will run: "+mcpShellJoin(c.Args))
	}
	for _, e := range pl.Edits {
		out = append(out, "Will edit "+e.Path+":")
	}
	return out
}

// recordFailed names a failed record save as only that: the client's
// config is already as the preview showed.
func (m *mcpSetup) recordFailed(who string, applied bool, err error) error {
	if err == nil {
		return nil
	}
	what := "nothing needed changing"
	if applied {
		what = "the change was applied"
	}
	return fmt.Errorf("%s%s; only the install record could not be saved (%s): %w", who, what, mcpinstall.RecordPath(m.p.ConfigDir), err)
}

// nothingToDo saves the records when no client needs a change.
func (m *mcpSetup) nothingToDo() error {
	return m.recordFailed("", false, m.s.saveRecords(m.p, m.plans, nil, m.remove))
}

// apply applies every plan. Every client is handled on its own: one
// client's failure can't block another's change or record update.
func (m *mcpSetup) apply() error {
	var errs []error
	for _, pl := range m.plans {
		errs = append(errs, m.applyOne(pl))
	}
	return errors.Join(errs...)
}

// applyOne applies one client's plan, shows the result and updates its
// install record.
func (m *mcpSetup) applyOne(pl mcpinstall.Plan) error {
	s := m.s
	if pl.Nothing {
		return m.recordFailed(pl.Client.Title()+": ", false, s.saveRecords(m.p, []mcpinstall.Plan{pl}, nil, m.remove))
	}
	bs, err := mcpinstall.Apply(pl, func(c mcpinstall.Command) error {
		out, err := s.run(c.Args)
		if err == nil || !c.MayFail {
			fmt.Fprint(s.out, mcpIndent(out))
		}
		return err
	}, s.now())
	for _, b := range bs {
		fmt.Fprintf(s.out, "Backup: %s\n", b)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", pl.Client.Title(), err)
	}
	s.verify(pl)
	return m.recordFailed(pl.Client.Title()+": ", true,
		s.saveRecords(m.p, []mcpinstall.Plan{pl}, map[mcpinstall.Client][]string{pl.Client: bs}, m.remove))
}

// printDone is setup-mcp's closing line after a successful apply.
func (m *mcpSetup) printDone() {
	if m.remove {
		fmt.Fprintln(m.s.out, "\nRemoved. Restart the agent sessions in your Herdr panes to drop the tools.")
		return
	}
	fmt.Fprintln(m.s.out, "\nInstalled. Restart Claude Code or Codex in your Herdr panes to load the tools (mcp__herdr-dots__*).")
	fmt.Fprintln(m.s.out, "Note: answers from these tools reach the main agent directly; they bypass the Dots approval queue.")
}

// detected reports which clients setup-mcp can handle: Claude needs the
// claude CLI (it edits its own config); Codex needs the codex CLI or an
// existing config file.
func (s setupIO) detected() (claude, codex bool) {
	return s.has("claude"), s.has("codex") || mcpFileExists(s.env.CodexConfigPath())
}

// pickClients resolves --client (see detected).
func (s setupIO) pickClients(flagVal string, remove bool) ([]mcpinstall.Client, error) {
	claudeOK, codexOK := s.detected()
	switch flagVal {
	case "claude":
		if !claudeOK {
			return nil, errors.New("claude CLI not found on PATH: install Claude Code first; nothing changed")
		}
		return []mcpinstall.Client{mcpinstall.ClientClaude}, nil
	case "codex":
		if !codexOK && remove {
			return nil, fmt.Errorf("no Codex config at %s; nothing to remove", s.env.CodexConfigPath())
		}
		if !codexOK {
			return nil, fmt.Errorf("codex CLI not found on PATH and no %s: install Codex first; nothing changed", s.env.CodexConfigPath())
		}
		return []mcpinstall.Client{mcpinstall.ClientCodex}, nil
	case "both":
		var out []mcpinstall.Client
		if claudeOK {
			out = append(out, mcpinstall.ClientClaude)
		} else {
			fmt.Fprintln(s.out, "Skipping Claude Code: claude CLI not found on PATH.")
		}
		if codexOK {
			out = append(out, mcpinstall.ClientCodex)
		} else {
			fmt.Fprintln(s.out, "Skipping Codex: codex CLI not found on PATH and no "+s.env.CodexConfigPath()+".")
		}
		if len(out) == 0 {
			return nil, errNoClients
		}
		return out, nil
	}
	return nil, fmt.Errorf("--client %q: want claude, codex or both", flagVal)
}

func (s setupIO) has(bin string) bool {
	_, err := s.lookPath(bin)
	return err == nil
}

func (s setupIO) verify(pl mcpinstall.Plan) {
	if len(pl.Verify) == 0 {
		return
	}
	if !s.has(pl.Verify[0]) {
		fmt.Fprintf(s.out, "(%s is not on PATH: not verified)\n", pl.Verify[0])
		return
	}
	out, err := s.run(pl.Verify)
	fmt.Fprintf(s.out, "$ %s\n%s", mcpShellJoin(pl.Verify), mcpIndent(out))
	if err != nil {
		fmt.Fprintf(s.out, "(verification failed: %v)\n", err)
	}
}

// saveRecords updates mcp-install.json for the clients just handled.
func (s setupIO) saveRecords(p paths.Paths, plans []mcpinstall.Plan, backups map[mcpinstall.Client][]string, remove bool) error {
	path := mcpinstall.RecordPath(p.ConfigDir)
	rs, err := mcpinstall.LoadRecords(path)
	if err != nil {
		return err
	}
	for _, pl := range plans {
		switch {
		case remove:
			rs = mcpinstall.WithoutClient(rs, pl.Client)
		case pl.Nothing:
			if !mcpHasRecord(rs, pl.Client) {
				rs = mcpinstall.WithRecord(rs, s.record(p, pl, nil))
			}
		default:
			rs = mcpinstall.WithRecord(rs, s.record(p, pl, backups[pl.Client]))
		}
	}
	return mcpinstall.SaveRecords(path, rs)
}

func (s setupIO) record(p paths.Paths, pl mcpinstall.Plan, backups []string) mcpinstall.Record {
	return mcpinstall.Record{Client: pl.Client, Scope: "user", ConfigPath: pl.Config, Backups: backups, At: s.now(),
		Command: append([]string{s.exe}, mcpinstall.ServerArgs(p.ConfigDir, p.StateDir)...)}
}

func mcpHasRecord(rs []mcpinstall.Record, c mcpinstall.Client) bool {
	for _, r := range rs {
		if r.Client == c {
			return true
		}
	}
	return false
}

func mcpFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// mcpShellJoin quotes arguments for display.
func mcpShellJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r == '/' || r == '-' || r == '_' || r == '.' || r == ':' || r == '=' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
		}) < 0 {
			q[i] = a
			continue
		}
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

func mcpIndent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return "  " + strings.ReplaceAll(s, "\n", "\n  ") + "\n"
}

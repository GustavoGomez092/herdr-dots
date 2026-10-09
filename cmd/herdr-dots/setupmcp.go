package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/paths"
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

func runSetupMCP(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	home, _ := os.UserHomeDir()
	return setupMCP(args, setupIO{
		in: os.Stdin, out: os.Stdout,
		run: func(a []string) (string, error) {
			out, err := exec.Command(a[0], a[1:]...).CombinedOutput()
			return string(out), err
		},
		lookPath: exec.LookPath,
		env:      mcpinstall.Env{Getenv: os.Getenv, Home: home},
		now:      time.Now,
		exe:      exe,
		paths:    paths.Discover,
	})
}

func setupMCP(args []string, s setupIO) error {
	fs := flag.NewFlagSet("setup-mcp", flag.ContinueOnError)
	client := fs.String("client", "both", "claude, codex or both")
	remove := fs.Bool("remove", false, "remove the server instead of installing it")
	yes := fs.Bool("yes", false, "apply without asking")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.Contains(s.exe, string(filepath.Separator)+"go-build") {
		return errors.New("run the built plugin binary (./bin/herdr-dots setup-mcp), not `go run`: the clients would point at a temporary file")
	}
	clients, err := s.pickClients(*client, *remove)
	if err != nil {
		return err
	}
	p, err := s.paths()
	if err != nil {
		return err
	}
	var plans []mcpinstall.Plan
	for _, c := range clients {
		var pl mcpinstall.Plan
		if *remove {
			pl, err = mcpinstall.PlanRemove(c, s.env)
		} else {
			pl, err = mcpinstall.PlanInstall(c, s.env, s.exe, p.ConfigDir, p.StateDir)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", c.Title(), err)
		}
		plans = append(plans, pl)
	}

	todo := 0
	for _, pl := range plans {
		fmt.Fprintf(s.out, "\n== %s ==\n", pl.Client.Title())
		if pl.Nothing {
			fmt.Fprintf(s.out, "Nothing to do: %s", pl.Preview)
			continue
		}
		todo++
		for _, c := range pl.Commands {
			fmt.Fprintf(s.out, "Will run: %s\n", mcpShellJoin(c.Args))
		}
		for _, e := range pl.Edits {
			fmt.Fprintf(s.out, "Will edit %s:\n", e.Path)
		}
		fmt.Fprint(s.out, pl.Preview)
		for _, b := range pl.Backups {
			fmt.Fprintf(s.out, "Backs up %s first.\n", b)
		}
	}
	// recordFailed names a failed record save as only that: the client's
	// config is already as the preview showed.
	recordFailed := func(who string, applied bool, err error) error {
		if err == nil {
			return nil
		}
		what := "nothing needed changing"
		if applied {
			what = "the change was applied"
		}
		return fmt.Errorf("%s%s; only the install record could not be saved (%s): %w", who, what, mcpinstall.RecordPath(p.ConfigDir), err)
	}
	if todo == 0 {
		fmt.Fprintln(s.out, "\nNothing changed.")
		return recordFailed("", false, s.saveRecords(p, plans, nil, *remove))
	}
	if !*yes {
		fmt.Fprint(s.out, "\nApply these changes? [y/N] ")
		line, _ := bufio.NewReader(s.in).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Fprintln(s.out, "Cancelled. Nothing changed.")
			return nil
		}
	}
	// Every client is handled on its own: one client's failure can't block
	// another's change or record update.
	var errs []error
	for _, pl := range plans {
		if pl.Nothing {
			errs = append(errs, recordFailed(pl.Client.Title()+": ", false, s.saveRecords(p, []mcpinstall.Plan{pl}, nil, *remove)))
			continue
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
			errs = append(errs, fmt.Errorf("%s: %w", pl.Client.Title(), err))
			continue
		}
		s.verify(pl)
		errs = append(errs, recordFailed(pl.Client.Title()+": ", true,
			s.saveRecords(p, []mcpinstall.Plan{pl}, map[mcpinstall.Client][]string{pl.Client: bs}, *remove)))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if *remove {
		fmt.Fprintln(s.out, "\nRemoved. Restart the agent sessions in your Herdr panes to drop the tools.")
		return nil
	}
	fmt.Fprintln(s.out, "\nInstalled. Restart Claude Code or Codex in your Herdr panes to load the tools (mcp__herdr-dots__*).")
	fmt.Fprintln(s.out, "Note: answers from these tools reach the main agent directly; they bypass the Dots approval queue.")
	return nil
}

// pickClients resolves --client. Claude needs the claude CLI (it edits its
// own config); Codex needs the codex CLI or an existing config file.
func (s setupIO) pickClients(flagVal string, remove bool) ([]mcpinstall.Client, error) {
	claudeOK := s.has("claude")
	codexOK := s.has("codex") || mcpFileExists(s.env.CodexConfigPath())
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
			return nil, errors.New("neither Claude Code nor Codex found; nothing changed")
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

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"herdr-dots/internal/keys"
	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/onboarding"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/ui/setup"
)

func init() { commands["setup"] = runSetup }

// onboardIO is what the first-run Setup screen and `herdr-dots setup` touch:
// setup-mcp's effects, Herdr's config for setup-keys, and onboarding.json.
type onboardIO struct {
	s        setupIO
	keysPath string // Herdr's config.toml
	store    onboarding.Store
	reload   func() ([]byte, error) // herdr server reload-config
}

func newOnboardIO(in io.Reader, out io.Writer, p paths.Paths) (onboardIO, error) {
	s, err := newSetupIO(in, out)
	if err != nil {
		return onboardIO{}, err
	}
	s.paths = func() (paths.Paths, error) { return p, nil }
	home, _ := os.UserHomeDir()
	return onboardIO{s: s, keysPath: keys.ConfigPath(os.Getenv, home),
		store: onboarding.Store{Path: p.OnboardingFile()}, reload: herdrReload}, nil
}

// onboardingFor is the Setup screen's effects for a UI process, and the
// Dots pane's "setup pending" check. Nothing is read here: detection runs
// later, inside a tea.Cmd (manage's and the pane's Init). The children it
// starts are bounded by uiChildTimeout.
func onboardingFor(p paths.Paths) (setup.Deps, func() bool) {
	o, err := newOnboardIO(nil, io.Discard, p)
	if err != nil {
		return setup.Deps{}, nil
	}
	o.s.run = setupChildRunner(uiChildTimeout)
	o.reload = func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), uiChildTimeout)
		defer cancel()
		bin := os.Getenv("HERDR_BIN_PATH")
		if bin == "" {
			bin = "herdr"
		}
		out, err := reloadHerdrConfig(ctx, bin)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("timed out after %s", uiChildTimeout)
		}
		return out, err
	}
	return o.deps(), o.pending
}

func (o onboardIO) deps() setup.Deps {
	return setup.Deps{
		FirstRun: func() (*setup.Offer, error) { return o.firstRun(true) },
		Check:    o.check, Apply: o.apply, Record: o.store.Mark,
	}
}

// onboardPlan is what check planned; the screen hands it back to apply, so
// y applies what the screen showed.
type onboardPlan struct {
	mcp  *mcpSetup // nil: no client to install for
	keys *keysSetup
}

// mcpChange is a plan's change as the screen shows it, in setup-mcp's
// words: the commands it runs, the file it edits with the diff, the file
// Claude Code writes, and the backups.
func mcpChange(pl mcpinstall.Plan) []string {
	out := mcpWillLines(pl)
	preview := strings.Split(strings.TrimSuffix(pl.Preview, "\n"), "\n")
	if len(pl.Edits) > 0 { // a file edit: its diff is the change
		out = append(out, preview...)
	} else if len(preview) > 0 && preview[0] != "" { // the CLI edits its own file: name it
		out = append(out, strings.TrimSuffix(preview[0], ":"))
	}
	for _, b := range pl.Backups {
		out = append(out, "Backs up "+b+" first.")
	}
	return out
}

// check reads the state of every item and plans what y would change, as
// setup-mcp and setup-keys plan it. Nothing is written.
func (o onboardIO) check() (setup.Offer, error) {
	p, err := o.s.paths()
	if err != nil {
		return setup.Offer{}, err
	}
	var offer setup.Offer
	plan := onboardPlan{}
	m := &mcpSetup{s: o.s, p: p}
	planErr := o.s.goRunErr()
	if planErr == nil {
		planErr = recordsNewerErr(p)
	}
	claudeOK, codexOK := o.s.detected()
	for _, c := range []struct {
		client mcpinstall.Client
		ok     bool
	}{{mcpinstall.ClientClaude, claudeOK}, {mcpinstall.ClientCodex, codexOK}} {
		if !c.ok {
			continue
		}
		cl := setup.Client{Title: c.client.Title()}
		if planErr != nil {
			cl.Err = planErr.Error()
		} else if pl, err := mcpinstall.PlanInstall(c.client, o.s.env, o.s.exe, p.ConfigDir, p.StateDir); err != nil {
			cl.Err = err.Error()
		} else {
			cl.Installed = pl.Nothing
			if !pl.Nothing {
				cl.Change = mcpChange(pl)
			}
			m.plans = append(m.plans, pl)
		}
		offer.Clients = append(offer.Clients, cl)
	}
	if len(m.plans) > 0 {
		plan.mcp = m
	}
	offer.Keys = setup.Keys{Path: o.keysPath}
	k, err := planKeys(o.keysPath)
	if err != nil {
		offer.Keys.Err = err.Error()
	} else {
		plan.keys = &k
		offer.Keys.Installed = len(k.already) > 0
		if len(k.add) > 0 {
			for _, b := range k.add {
				offer.Keys.Change = append(offer.Keys.Change, fmt.Sprintf("%-15s %s", b.Key, b.Description))
			}
			for _, c := range k.conflicts {
				offer.Keys.Change = append(offer.Keys.Change, fmt.Sprintf("(%s is taken by %s: %s is skipped)", c.Binding.Key, c.With, c.Binding.Action))
			}
		}
	}
	offer.Plan = plan
	return offer, nil
}

// apply runs the plan the screen showed: setup-mcp --yes for every client,
// setup-keys --yes for the keybindings. Their output is not shown; each
// item's result is.
func (o onboardIO) apply(plan any, mcp, keys bool) []setup.Result {
	pl, _ := plan.(onboardPlan)
	var res []setup.Result
	if mcp {
		if pl.mcp == nil {
			res = append(res, setup.Result{Topic: onboarding.MCP, Item: "MCP server", Err: errNoClients})
		} else {
			for _, p := range pl.mcp.plans {
				r := setup.Result{Topic: onboarding.MCP, Item: p.Client.Title(), Err: pl.mcp.applyOne(p)}
				if p.Nothing && r.Err == nil {
					r.Note = "already installed"
				}
				res = append(res, r)
			}
		}
	}
	if keys {
		r := setup.Result{Topic: onboarding.Keys, Item: "Keybindings"}
		switch {
		case pl.keys == nil:
			r.Err = errors.New("Herdr's config could not be read")
		case len(pl.keys.add) == 0:
			r.Note = "nothing to add"
		default:
			var out strings.Builder
			r.Err = pl.keys.apply(&out, o.s.now(), o.reload)
			r.Note = strings.Join(strings.Split(strings.TrimSpace(out.String()), "\n"), "; ")
		}
		res = append(res, r)
	}
	return res
}

// mcpInstalled reports that every detected client has the server.
func mcpInstalled(o setup.Offer) bool {
	for _, c := range o.Clients {
		if !c.Installed {
			return false
		}
	}
	return len(o.Clients) > 0
}

// firstRun is what the first-run screen asks on this open (nil: nothing).
// A record a newer herdr-dots wrote keeps the screen shut and is never
// written; one that can't be read or parsed is returned as the error (the
// manage footer names it). When the record forbids both topics nothing is
// checked at all. With record (the manage popup, never the Dots pane), an
// MCP server installed by any route is recorded as installed.
func (o onboardIO) firstRun(record bool) (*setup.Offer, error) {
	rec, _, err := o.store.Load()
	if errors.Is(err, onboarding.ErrNewer) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := o.s.now()
	if !rec.Asks(onboarding.MCP, now) && !rec.Asks(onboarding.Keys, now) {
		return nil, nil
	}
	offer, err := o.check()
	if err != nil {
		return nil, nil
	}
	if record && mcpInstalled(offer) && rec.MCP.State != onboarding.Installed {
		_ = o.store.Mark(onboarding.Installed, onboarding.MCP)
	}
	f := offer.FirstRun(rec, now)
	if !f.Show() {
		return nil, nil
	}
	return &f, nil
}

// pending is the Dots pane's check: the first-run screen has something to
// ask. It records nothing (ruling P10-R14).
func (o onboardIO) pending() bool {
	f, _ := o.firstRun(false)
	return f != nil
}

func runSetup(args []string) error {
	p, err := paths.Discover()
	if err != nil {
		return err
	}
	o, err := newOnboardIO(os.Stdin, os.Stdout, p)
	if err != nil {
		return err
	}
	return setupCmd(args, o)
}

// setupCmd is `herdr-dots setup`: setup-mcp's and setup-keys' plans, shown
// as they show them, one question, then both applied.
func setupCmd(args []string, o onboardIO) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "apply without asking")
	noKeys := fs.Bool("no-keys", false, "leave the keybindings out")
	noMCP := fs.Bool("no-mcp", false, "leave the MCP server out")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	out := o.s.out
	var m *mcpSetup
	mcpTodo := 0
	if !*noMCP {
		var err error
		m, err = o.s.planMCP("both", false)
		switch {
		case errors.Is(err, errNoClients):
			fmt.Fprintln(out, "MCP server: "+err.Error())
		case err != nil:
			return err
		default:
			mcpTodo = m.print()
		}
	}
	var k keysSetup
	keysTodo := false
	if !*noKeys {
		fmt.Fprintln(out, "\n== Keybindings ==")
		var err error
		if k, err = planKeys(o.keysPath); err != nil {
			return fmt.Errorf("%w; nothing changed", err)
		}
		keysTodo = k.print(out)
	}
	if mcpTodo == 0 && !keysTodo {
		fmt.Fprintln(out, "\nNothing changed.")
		if m != nil {
			if err := m.nothingToDo(); err != nil {
				return err
			}
			o.record(onboarding.MCP)
		}
		return nil
	}
	if !*yes {
		fmt.Fprint(out, "\nApply these changes? [y/N] ")
		line, _ := bufio.NewReader(o.s.in).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Fprintln(out, "Cancelled. Nothing changed.")
			return nil
		}
	}
	var errs []error
	if m != nil {
		var err error
		if mcpTodo == 0 {
			err = m.nothingToDo()
		} else if err = m.apply(); err == nil {
			m.printDone()
		}
		if errs = append(errs, err); err == nil {
			o.record(onboarding.MCP)
		}
	}
	if keysTodo {
		err := k.apply(out, o.s.now(), o.reload)
		if errs = append(errs, err); err == nil {
			o.record(onboarding.Keys)
		}
	}
	return errors.Join(errs...)
}

// record notes an install in onboarding.json, so the Setup screen doesn't
// offer it again; a failure is reported, not fatal (the install is done).
func (o onboardIO) record(t onboarding.Topic) {
	if err := o.store.Mark(onboarding.Installed, t); err != nil {
		fmt.Fprintf(o.s.out, "(could not record this in %s: %v)\n", o.store.Path, err)
	}
}

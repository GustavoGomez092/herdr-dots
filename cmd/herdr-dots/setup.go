package main

import (
	"bufio"
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

// onboardingFor is the Setup screen for a UI process (manage, the Dots
// pane): its effects, and the first-run offer (nil: the screen stays shut).
// It only reads, except that an MCP server found installed is recorded.
func onboardingFor(p paths.Paths) (setup.Deps, *setup.Offer) {
	o, err := newOnboardIO(nil, io.Discard, p)
	if err != nil {
		return setup.Deps{}, nil
	}
	return o.deps(), o.firstRun()
}

func (o onboardIO) deps() setup.Deps {
	return setup.Deps{Check: o.check, Apply: o.apply, Record: o.store.Mark}
}

// onboardPlan is what check planned; the screen hands it back to apply, so
// y applies what the screen showed.
type onboardPlan struct {
	mcp  *mcpSetup // nil: no client to install for
	keys *keysSetup
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
	planErr := ""
	if strings.Contains(o.s.exe, string(os.PathSeparator)+"go-build") {
		planErr = "run the built plugin binary, not `go run`: the clients would point at a temporary file"
	} else if _, err := mcpinstall.LoadRecords(mcpinstall.RecordPath(p.ConfigDir)); errors.Is(err, mcpinstall.ErrRecordsVersion) {
		planErr = err.Error() + "; update herdr-dots first"
	}
	claudeOK, codexOK := o.s.detected()
	for _, c := range []struct {
		client mcpinstall.Client
		ok     bool
	}{{mcpinstall.ClientClaude, claudeOK}, {mcpinstall.ClientCodex, codexOK}} {
		if !c.ok {
			continue
		}
		cl := setup.Client{Title: c.client.Title(), Err: planErr}
		if cl.Err == "" {
			pl, err := mcpinstall.PlanInstall(c.client, o.s.env, o.s.exe, p.ConfigDir, p.StateDir)
			switch {
			case err != nil:
				cl.Err = err.Error()
			case pl.Nothing:
				cl.Installed = true
				m.plans = append(m.plans, pl)
			default:
				cl.Change = mcpWillLines(pl)
				if len(pl.Edits) > 0 { // a file edit: its diff is the change
					cl.Change = append(cl.Change, strings.Split(strings.TrimSuffix(pl.Preview, "\n"), "\n")...)
				}
				m.plans = append(m.plans, pl)
			}
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
		for _, b := range k.add {
			offer.Keys.Change = append(offer.Keys.Change, fmt.Sprintf("%-15s %s", b.Key, b.Description))
		}
		for _, c := range k.conflicts {
			offer.Keys.Change = append(offer.Keys.Change, fmt.Sprintf("(%s is taken by %s: %s is skipped)", c.Binding.Key, c.With, c.Binding.Action))
		}
		if len(k.add) == 0 {
			offer.Keys.Change = nil
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

// firstRun is the offer the screen opens with on this open, or nil. A
// record a newer herdr-dots wrote (or one that doesn't parse) keeps the
// screen shut and is never written. An MCP server installed by any route is
// recorded as installed.
func (o onboardIO) firstRun() *setup.Offer {
	rec, _, err := o.store.Load()
	if err != nil {
		return nil
	}
	offer, err := o.check()
	if err != nil {
		return nil
	}
	if mcpInstalled(offer) && rec.MCP.State != onboarding.Installed {
		_ = o.store.Mark(onboarding.Installed, onboarding.MCP)
	}
	f := offer.FirstRun(rec, o.s.now())
	if !f.Show() {
		return nil
	}
	return &f
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

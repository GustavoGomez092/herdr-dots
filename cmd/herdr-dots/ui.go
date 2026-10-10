package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/mcpinstall"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/ui/manage"
	"herdr-dots/internal/ui/pane"
	"herdr-dots/internal/ui/summon"
	"herdr-dots/internal/view"
)

// envTarget carries the main agent pane id into the panes our actions open.
const envTarget = "HERDR_DOTS_TARGET"

// resolveTarget returns the main agent pane to act on: the target an action
// passed down, else the focused pane mapped through targetFor. dotsPanesFile
// is the main-pane -> Dots-pane map.
func resolveTarget(dotsPanesFile string) string {
	if t := os.Getenv(envTarget); t != "" {
		return t
	}
	ic, _ := herdr.ParseInvocationContext(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"))
	focused, agent := ic.FocusedPaneID, ic.FocusedPaneAgent
	if focused == "" {
		// No invocation context: the agent is unknown, so only a Dots-pane
		// mapping can turn this pane into a target.
		focused = os.Getenv("HERDR_PANE_ID")
	}
	m, _ := loadDotsPanes(dotsPanesFile)
	return targetFor(focused, agent, m)
}

// targetFor maps the focused pane to the main pane dots act on. A focused Dots
// pane maps back to its main pane; a pane without an agent (a shell, a plugin
// pane) maps to "", which opens the library only.
func targetFor(focused, agent string, dotsPanes map[string]string) string {
	if focused == "" {
		return ""
	}
	for main, id := range dotsPanes {
		if id == focused {
			return main
		}
	}
	if agent == "" {
		return ""
	}
	return focused
}

// uiSetup resolves paths, makes sure the engine runs, and returns its client.
func uiSetup() (paths.Paths, ipc.Client, error) {
	p, err := paths.FromEnv()
	if err != nil {
		return p, ipc.Client{}, err
	}
	if err := p.Ensure(); err != nil {
		return p, ipc.Client{}, err
	}
	if err := ensureEngine(p); err != nil {
		return p, ipc.Client{}, err
	}
	return p, ipc.Client{Path: p.EngineEndpoint()}, nil
}

// pumpSnapshots forwards engine snapshots to a program, restarting the engine
// and reconnecting whenever the stream drops.
func pumpSnapshots(ctx context.Context, p paths.Paths, cl ipc.Client, send func(tea.Msg), wrap func(view.Snapshot) tea.Msg, disconnected tea.Msg) {
	for ctx.Err() == nil {
		if ch, err := cl.Watch(ctx); err == nil {
			for s := range ch {
				send(wrap(s))
			}
		}
		if ctx.Err() != nil {
			return
		}
		if disconnected != nil {
			send(disconnected)
		}
		_ = ensureEngine(p)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func runPane([]string) error {
	p, cl, err := uiSetup()
	if err != nil {
		return err
	}
	setupDeps, first := onboardingFor(p) // the first-run Setup screen, if it has anything to ask
	m := pane.New(resolveTarget(p.DotsPanesFile()), cl).WithOpener(paneOpener).WithSetup(setupDeps, first)
	if hc, err := herdr.NewFromEnv(); err == nil {
		m = m.WithPatchViewer(patchViewer(hc)) // v and A on a patch card open the diff pane
	}
	prog := tea.NewProgram(m, tea.WithAltScreen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpSnapshots(ctx, p, cl, prog.Send, func(s view.Snapshot) tea.Msg { return pane.SnapshotMsg(s) }, pane.DisconnectedMsg{})
	_, err = prog.Run()
	return err
}

func newDotFromConfig(cfg config.Config) dots.Dot {
	d := dots.New("")
	d.Backend.Kind, d.Backend.Model = cfg.DefaultBackend, cfg.DefaultModel
	return d
}

func runManage([]string) error {
	p, cl, err := uiSetup()
	if err != nil {
		return err
	}
	check := func(ctx context.Context, kind string) error {
		b, err := backend.New(kind, nil)
		if err != nil {
			return err
		}
		return b.Check(ctx)
	}
	cfg, _ := config.Load(p.ConfigFile()) // malformed config -> defaults; the engine logs the error
	newDot := func() dots.Dot { return newDotFromConfig(cfg) }
	set := newBackendSet(p, configLoader(p.ConfigFile()), nil)
	prices := set.Prices()
	client := &http.Client{Timeout: 20 * time.Second}
	home, _ := os.UserHomeDir()
	setupDeps, first := onboardingFor(p) // the first-run Setup screen, if it has anything to ask
	prog := tea.NewProgram(manage.New(resolveTarget(p.DotsPanesFile()), manage.Deps{Caller: cl, DotsDir: p.DotsDir(), Check: check, NewDot: newDot,
		Memory: memoryStoreFor(p, cfg, nil), Project: paneProject,
		History: historyStoreFor(p), SessionKey: paths.SessionKey(p.SocketPath), ExportDir: home,
		Fetch: func(ctx context.Context, url string) ([]byte, string, error) {
			return dotpack.Fetch(ctx, client, url, dotpack.MaxBytes)
		},
		MCPStatus:  func() string { return mcpinstall.StatusLine(p.ConfigDir) },
		CheckDot:   set.CheckDot,
		Models:     set.Models,
		Estimate:   func(d dots.Dot) string { return estimateLine(prices, d) },
		Keys:       set.creds(cfg),
		TestKey:    set.TestProvider,
		OllamaURL:  set.OllamaURL(),
		Setup:      setupDeps,
		SetupFirst: first,
	}), tea.WithAltScreen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpSnapshots(ctx, p, cl, prog.Send, func(s view.Snapshot) tea.Msg { return manage.SnapshotMsg(s) }, nil)
	_, err = prog.Run()
	return err
}

func runSummonUI(args []string) error {
	fs := flag.NewFlagSet("summon-ui", flag.ContinueOnError)
	councilMode := fs.Bool("council", false, "open the council popup instead of the summon list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, cl, err := uiSetup()
	if err != nil {
		return err
	}
	target := resolveTarget(p.DotsPanesFile())
	m := summon.New(target, cl)
	if *councilMode {
		m = summon.NewCouncil(target, cl)
	}
	prog := tea.NewProgram(m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpSnapshots(ctx, p, cl, prog.Send, func(s view.Snapshot) tea.Msg { return summon.SnapshotMsg(s) }, nil)
	_, err = prog.Run()
	return err
}

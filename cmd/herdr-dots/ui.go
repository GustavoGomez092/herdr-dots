package main

import (
	"context"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/ui/manage"
	"herdr-dots/internal/ui/pane"
	"herdr-dots/internal/view"
)

// envTarget carries the main agent pane id into the panes our actions open.
const envTarget = "HERDR_DOTS_TARGET"

func resolveTarget() string {
	if t := os.Getenv(envTarget); t != "" {
		return t
	}
	ic, _ := herdr.ParseInvocationContext(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"))
	return ic.FocusedPaneID
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
	return p, ipc.Client{Path: p.EngineSock()}, nil
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
	prog := tea.NewProgram(pane.New(resolveTarget(), cl), tea.WithAltScreen())
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
	prog := tea.NewProgram(manage.New(resolveTarget(), manage.Deps{Caller: cl, DotsDir: p.DotsDir(), Check: check, NewDot: newDot}), tea.WithAltScreen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pumpSnapshots(ctx, p, cl, prog.Send, func(s view.Snapshot) tea.Msg { return manage.SnapshotMsg(s) }, nil)
	_, err = prog.Run()
	return err
}

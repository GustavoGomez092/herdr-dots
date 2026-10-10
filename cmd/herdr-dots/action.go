package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

func runAction(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: herdr-dots action <manage|toggle-pane|summon|council|pause-all>")
	}
	p, err := paths.FromEnv()
	if err != nil {
		return err
	}
	if err := p.Ensure(); err != nil {
		return err
	}
	if err := ensureEngine(p); err != nil {
		return err
	}
	hc, err := herdr.NewFromEnv()
	if err != nil {
		return err
	}
	target := resolveTarget(p.DotsPanesFile()) // "" = no agent pane focused: library only
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	env := map[string]string{envTarget: target}
	switch args[0] {
	case "manage":
		_, err = hc.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "manage", Placement: "popup", Width: "80%", Height: "80%", Env: env, Focus: true})
	case "summon":
		_, err = hc.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "summon", Placement: "popup", Width: "60%", Height: 14, Env: env, Focus: true})
	case "council":
		err = openCouncil(ctx, hc, target)
	case "toggle-pane":
		err = togglePane(ctx, p, hc, target)
	case "pause-all":
		err = ipc.Client{Path: p.EngineEndpoint()}.Call(ctx, ipc.CmdPauseAll, nil)
	default:
		err = fmt.Errorf("unknown action %q", args[0])
	}
	return err
}

// toggle decides what toggle-pane does. m maps main pane -> its Dots pane and
// is updated in place.
func toggle(m map[string]string, target string, exists func(string) bool) (closeID string, open bool) {
	for t, id := range m {
		if id == target { // the focused pane is itself a Dots pane
			delete(m, t)
			return id, false
		}
	}
	if id, ok := m[target]; ok {
		delete(m, target)
		if exists(id) {
			return id, false
		}
	}
	return "", true
}

// loadDotsPanes reads the main-pane -> Dots-pane map. It always returns a
// non-nil map; a missing, null or corrupt file yields an empty one so a broken
// file never blocks toggling.
func loadDotsPanes(path string) (map[string]string, error) {
	m := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return m, nil
		}
		return nil, err
	}
	if json.Unmarshal(data, &m) != nil || m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// toggleLockWait bounds how long a toggle waits for one already running
// (each makes at most three Herdr calls; the action's deadline is 15 s).
const toggleLockWait = 10 * time.Second

func togglePane(ctx context.Context, p paths.Paths, hc herdr.Client, target string) error {
	if target == "" {
		return errors.New("focus an agent pane first")
	}
	// Two toggles at once (two key presses, two panes) would each write the
	// map they read and drop the other's pane: the read-modify-write holds
	// a file lock, across processes.
	lock, err := lockfile.Acquire(p.DotsPanesFile()+".lock", toggleLockWait)
	if err != nil {
		return fmt.Errorf("another toggle-pane is still running: %w", err)
	}
	defer lock.Release()
	m, err := loadDotsPanes(p.DotsPanesFile())
	if err != nil {
		return err
	}
	closeID, open := toggle(m, target, func(id string) bool {
		_, err := hc.PaneGet(ctx, id)
		return err == nil
	})
	if closeID != "" {
		if err := hc.ClosePluginPane(ctx, closeID); err != nil && !herdr.IsNotFound(err) {
			return err
		}
	}
	if open {
		id, err := hc.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "pane", Placement: "split", Direction: "right",
			TargetPaneID: target, Env: map[string]string{envTarget: target}, Focus: false})
		if err != nil {
			return err
		}
		m[target] = id
	}
	out, _ := json.Marshal(m)
	return fsutil.WriteFileAtomic(p.DotsPanesFile(), out, 0o600)
}

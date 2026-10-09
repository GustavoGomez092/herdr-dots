package paths

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/platform"
)

// PluginID is the id Herdr knows the plugin by.
const PluginID = "herdr-dots"

// StatePointer is a file in the config dir that records the state dir. Herdr
// has a `plugin config-dir` command but no state-dir equivalent, so commands
// run outside Herdr (the memory CLI) find the state dir through this file.
const StatePointer = ".state-dir"

// MemoryDir holds dot memory. It is not under SessionDir: memory is shared by
// every named Herdr session.
func (p Paths) MemoryDir() string { return filepath.Join(p.StateDir, "memory") }

// StatePointerFile is CONFIG_DIR/.state-dir.
func (p Paths) StatePointerFile() string { return filepath.Join(p.ConfigDir, StatePointer) }

func (p Paths) writeStatePointer() error {
	f := p.StatePointerFile()
	if b, err := os.ReadFile(f); err == nil && strings.TrimSpace(string(b)) == p.StateDir {
		return nil
	}
	return fsutil.WriteFileAtomic(f, []byte(p.StateDir+"\n"), 0o600)
}

// CommandRunner runs a command and returns its stdout.
type CommandRunner func(ctx context.Context, name string, args ...string) (string, error)

func execRunner(ctx context.Context, name string, args ...string) (string, error) {
	cmd, err := platform.CommandContext(ctx, name, args)
	if err != nil {
		return "", err
	}
	cmd.Env = platform.ChildEnv(os.Environ())
	out, err := cmd.Output()
	return string(out), err
}

// Discover resolves paths for commands that may run outside Herdr. Inside
// Herdr it uses the injected variables. Outside, it asks `herdr plugin
// config-dir herdr-dots` for the config dir and reads the state dir from the
// pointer file the engine writes (Ensure). SocketPath may be empty.
func Discover() (Paths, error) { return discover(os.Getenv, execRunner) }

func discover(getenv func(string) string, run CommandRunner) (Paths, error) {
	c, s, h := getenv("HERDR_PLUGIN_CONFIG_DIR"), getenv("HERDR_PLUGIN_STATE_DIR"), getenv("HERDR_SOCKET_PATH")
	if c == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := run(ctx, "herdr", "plugin", "config-dir", PluginID)
		if err != nil {
			return Paths{}, fmt.Errorf("find the plugin config dir with `herdr plugin config-dir %s`: %w", PluginID, err)
		}
		c = firstLine(out)
		if c == "" {
			return Paths{}, errors.New("`herdr plugin config-dir` printed nothing")
		}
		if !filepath.IsAbs(c) {
			return Paths{}, fmt.Errorf("`herdr plugin config-dir` returned a relative path: %s", c)
		}
	} else {
		if !filepath.IsAbs(c) {
			return Paths{}, fmt.Errorf("HERDR_PLUGIN_CONFIG_DIR is not absolute: %s", c)
		}
	}
	c = filepath.Clean(c)

	if s == "" {
		b, err := os.ReadFile(filepath.Join(c, StatePointer))
		if err != nil {
			return Paths{}, fmt.Errorf("the plugin state dir is unknown: open Herdr once with herdr-dots enabled, or set HERDR_PLUGIN_STATE_DIR (%w)", err)
		}
		s = firstLine(string(b))
		if s == "" {
			return Paths{}, fmt.Errorf("%s is empty: set HERDR_PLUGIN_STATE_DIR", filepath.Join(c, StatePointer))
		}
		if !filepath.IsAbs(s) {
			return Paths{}, fmt.Errorf("%s is not absolute: %s", filepath.Join(c, StatePointer), s)
		}
	} else {
		if !filepath.IsAbs(s) {
			return Paths{}, fmt.Errorf("HERDR_PLUGIN_STATE_DIR is not absolute: %s", s)
		}
	}
	s = filepath.Clean(s)

	return New(c, s, h), nil
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"herdr-dots/internal/keys"
	"herdr-dots/internal/platform"
)

func runSetupKeys(args []string) error {
	fs := flag.NewFlagSet("setup-keys", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "append without asking")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) { // -h: the flag set printed its usage
			return nil
		}
		return err
	}
	home, _ := os.UserHomeDir()
	k, err := planKeys(keys.ConfigPath(os.Getenv, home))
	if err != nil {
		return err
	}
	if !k.print(os.Stdout) {
		return nil
	}
	if !*yes {
		fmt.Print("Append it? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
	}
	return k.apply(os.Stdout, time.Now(), herdrReload)
}

// keysSetup is setup-keys' plan for Herdr's config. Planning, printing and
// applying are separate steps, shared by setup-keys, setup and the Setup
// screen, so all three make the same change.
type keysSetup struct {
	path      string // Herdr's config.toml
	before    []byte // the config as planned (nil: there was none)
	add       []keys.Binding
	already   []keys.Binding
	conflicts []keys.Conflict
}

// planKeys reads Herdr's config at path (a missing file has no bindings)
// and plans the herdr-dots keybindings. Nothing is changed.
func planKeys(path string) (keysSetup, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return keysSetup{}, err
	}
	existing, err := keys.Existing(data)
	if err != nil {
		return keysSetup{}, fmt.Errorf("%s is not valid TOML: %w", path, err)
	}
	k := keysSetup{path: path, before: data}
	k.add, k.already, k.conflicts = keys.Plan(keys.Defaults(), existing)
	return k, nil
}

// print writes the plan as setup-keys shows it and reports whether there is
// anything to append.
func (k keysSetup) print(out io.Writer) bool {
	for _, b := range k.already {
		fmt.Fprintf(out, "✓ %s already runs %s\n", b.Key, b.Action)
	}
	for _, c := range k.conflicts {
		fmt.Fprintf(out, "✗ %s is taken by %s; skipping %s (bind it yourself to another key)\n", c.Binding.Key, c.With, c.Binding.Action)
	}
	if len(k.add) == 0 {
		fmt.Fprintln(out, "Nothing to add.")
		return false
	}
	fmt.Fprintf(out, "\nThis will be appended to %s:\n%s\n", k.path, keys.Block(k.add))
	return true
}

// apply appends the planned bindings (after a backup) and asks Herdr to
// reload its config (herdrReload; tests pass a fake). A config that changed
// since the plan was shown is refused, as mcpinstall.Apply refuses a stale
// preview: nothing is appended.
func (k keysSetup) apply(out io.Writer, now time.Time, reload func() ([]byte, error)) error {
	cur, err := os.ReadFile(k.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(cur, k.before) || (cur == nil) != (k.before == nil) {
		return fmt.Errorf("%s changed since the preview; nothing changed: run setup again", k.path)
	}
	backup, err := keys.Append(k.path, k.add, now)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, appendedLine(backup))
	if res, err := reload(); err != nil {
		fmt.Fprintf(out, "Run `herdr server reload-config` yourself (%v: %s)\n", err, strings.TrimSpace(string(res)))
	} else {
		fmt.Fprintln(out, "Herdr config reloaded.")
	}
	return nil
}

// appendedLine reports where the original config was saved ("" when there
// was no config yet, so nothing to back up).
func appendedLine(backup string) string {
	if backup == "" {
		return "Appended. There was no config to back up."
	}
	return "Appended. Backup: " + backup
}

// herdrReload asks the running Herdr server to reload its config, with the
// Herdr binary Herdr names (HERDR_BIN_PATH), else herdr on PATH.
func herdrReload() ([]byte, error) {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	return reloadHerdrConfig(context.Background(), bin)
}

// reloadHerdrConfig runs `<bin> server reload-config` with the child
// environment every product child gets (platform.ChildEnv), without a Claude
// Code session's variables (platform.WithoutClaudeEnv).
func reloadHerdrConfig(ctx context.Context, bin string) ([]byte, error) {
	cmd, err := platform.CommandContext(ctx, bin, []string{"server", "reload-config"})
	if err != nil {
		return nil, err
	}
	cmd.Env = platform.ChildEnv(platform.WithoutClaudeEnv(os.Environ()))
	return cmd.CombinedOutput()
}

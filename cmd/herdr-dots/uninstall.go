package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/keys"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
)

func init() { commands["uninstall-extras"] = runUninstallExtras }

// uninstallSessionRe matches a session directory name (paths.SessionKey).
var uninstallSessionRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

// purgeConfirm is what the developer types to delete every dot and all memory.
const purgeConfirm = "purge herdr-dots"

// uninstallIO is what uninstall-extras touches, so tests can fake it.
type uninstallIO struct {
	in        io.Reader
	out       io.Writer
	keysPath  string               // Herdr's config.toml
	removeMCP func(yes bool) error // setup-mcp --remove
	paths     func() (paths.Paths, error)
	home      string
	now       func() time.Time
}

func runUninstallExtras(args []string) error {
	home, _ := os.UserHomeDir()
	return uninstallExtras(args, uninstallIO{
		in: os.Stdin, out: os.Stdout,
		keysPath: keys.ConfigPath(os.Getenv, home),
		removeMCP: func(yes bool) error {
			a := []string{"--remove"}
			if yes {
				a = append(a, "--yes")
			}
			return runSetupMCP(a)
		},
		paths: paths.Discover, home: home, now: time.Now,
	})
}

// uninstallExtras removes what herdr-dots added outside its own directories:
// the MCP server entries in Claude Code and Codex, and the keybinding block
// in Herdr's config.toml, each after a preview and a backup. `herdr plugin
// uninstall` removes the plugin itself. With --purge it also deletes the
// plugin's config and state directories (every dot, setting, credential and
// all memory) after the developer types "purge herdr-dots"; --yes never
// skips that.
func uninstallExtras(args []string, u uninstallIO) error {
	fs := flag.NewFlagSet("uninstall-extras", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also delete every dot, setting, credential and all memory")
	yes := fs.Bool("yes", false, "apply without asking (except --purge, which always asks)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	in := bufio.NewReader(u.in)

	fmt.Fprintln(u.out, "== MCP server ==")
	if err := u.removeMCP(*yes); err != nil {
		fmt.Fprintf(u.out, "MCP: %v (continuing)\n", err)
	}

	fmt.Fprintln(u.out, "\n== Keybindings ==")
	if err := removeKeyBlock(u, in, *yes); err != nil {
		return err
	}

	p, perr := u.paths()
	if !*purge {
		if perr == nil {
			fmt.Fprintf(u.out, "\nKept your dots, settings, credentials and memory in %s and %s.\nRun `herdr-dots uninstall-extras --purge` to delete them too.\n", p.ConfigDir, p.StateDir)
		}
		return nil
	}
	if perr != nil {
		return fmt.Errorf("find the plugin directories to purge: %w", perr)
	}
	return purgeDirs(u, in, p)
}

func removeKeyBlock(u uninstallIO, in *bufio.Reader, yes bool) error {
	path := u.keysPath
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // edit through a dotfiles symlink
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(u.out, "No Herdr config at %s: nothing to remove.\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	out, removed, left, err := keys.RemoveBlock(string(data))
	if err != nil {
		return err
	}
	if left > 0 {
		fmt.Fprintf(u.out, "%d herdr-dots binding(s) in %s use a syntax this command can't edit: remove them by hand.\n", left, path)
	}
	if removed == 0 {
		fmt.Fprintln(u.out, "No herdr-dots keybindings to remove.")
		return nil
	}
	fmt.Fprintf(u.out, "Will remove %d herdr-dots keybinding(s) from %s.\n", removed, path)
	if !yes && !uninstallConfirm(u.out, in, "Remove them? [y/N] ") {
		fmt.Fprintln(u.out, "Kept the keybindings.")
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	backup := path + ".bak-herdr-dots-" + u.now().Format("20060102-150405")
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(path, []byte(out), st.Mode().Perm()); err != nil {
		return err
	}
	fmt.Fprintf(u.out, "Removed. Backup: %s\nRun `herdr server reload-config` (or restart Herdr) to drop them now.\n", backup)
	return nil
}

func purgeDirs(u uninstallIO, in *bufio.Reader, p paths.Paths) error {
	for _, d := range []string{p.ConfigDir, p.StateDir} {
		if !safeToPurge(d, u.home) {
			return fmt.Errorf("refusing to delete %q: it is not a plugin directory", d)
		}
	}
	if busy := runningEngines(p.StateDir); len(busy) > 0 {
		return fmt.Errorf("%d Dots engine(s) are running (%s): quit Herdr first, then purge", len(busy), strings.Join(busy, ", "))
	}
	fmt.Fprintf(u.out, "\nThis deletes, with no undo:\n  %s (dots, settings, credentials)\n  %s (memory of every project, queues, history)\n", p.ConfigDir, p.StateDir)
	fmt.Fprintf(u.out, "Type %q to confirm: ", purgeConfirm)
	line, _ := in.ReadString('\n')
	if strings.TrimSpace(line) != purgeConfirm {
		fmt.Fprintln(u.out, "Not confirmed. Nothing deleted.")
		return nil
	}
	for _, d := range []string{p.ConfigDir, p.StateDir} {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	fmt.Fprintln(u.out, "Deleted. Run `herdr plugin uninstall herdr-dots` to remove the plugin itself.")
	return nil
}

func uninstallConfirm(out io.Writer, in *bufio.Reader, prompt string) bool {
	fmt.Fprint(out, prompt)
	line, _ := in.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

// safeToPurge refuses relative paths, the filesystem root, the home
// directory and anything above it, and very short paths.
func safeToPurge(dir, home string) bool {
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	dir = filepath.Clean(dir)
	if home != "" {
		h := filepath.Clean(home)
		if dir == h || strings.HasPrefix(h, dir+string(filepath.Separator)) {
			return false
		}
	}
	return len(strings.Split(strings.Trim(filepath.ToSlash(dir), "/"), "/")) >= 3
}

// runningEngines lists session directories whose engine holds its lock.
func runningEngines(stateDir string) []string {
	ents, _ := os.ReadDir(stateDir)
	var busy []string
	for _, e := range ents {
		if e.IsDir() && uninstallSessionRe.MatchString(e.Name()) && lockfile.Held(filepath.Join(stateDir, e.Name(), "engine.lock")) {
			busy = append(busy, e.Name())
		}
	}
	return busy
}

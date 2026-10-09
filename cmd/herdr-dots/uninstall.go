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
// skips that. Every purge precondition is checked before anything is edited,
// so a refused purge leaves nothing half-done.
func uninstallExtras(args []string, u uninstallIO) error {
	flags := flag.NewFlagSet("uninstall-extras", flag.ContinueOnError)
	purge := flags.Bool("purge", false, "also delete every dot, setting, credential and all memory")
	yes := flags.Bool("yes", false, "apply without asking (except --purge, which always asks)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	in := bufio.NewReader(u.in)

	p, perr := u.paths()
	if *purge {
		if perr != nil {
			return fmt.Errorf("find the plugin directories to purge: %w", perr)
		}
		if err := purgePreflight(u, p); err != nil {
			return err
		}
	}

	fmt.Fprintln(u.out, "== MCP server ==")
	if err := u.removeMCP(*yes); err != nil {
		fmt.Fprintf(u.out, "MCP: %v (continuing)\n", err)
	}

	fmt.Fprintln(u.out, "\n== Keybindings ==")
	if err := removeKeyBlock(u, in, *yes); err != nil {
		return err
	}

	if !*purge {
		if perr == nil {
			fmt.Fprintf(u.out, "\nKept your dots, settings, credentials and memory in %s and %s.\nRun `herdr-dots uninstall-extras --purge` to delete them too.\n", tilde(p.ConfigDir, u.home), tilde(p.StateDir, u.home))
		}
		return nil
	}
	return purgeDirs(u, in, p)
}

// tilde shortens a path under the home directory to ~/...
func tilde(path, home string) string {
	homes := []string{home}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		homes = append(homes, real)
	}
	for _, h := range homes {
		if h == "" {
			continue
		}
		if path == h {
			return "~"
		}
		if strings.HasPrefix(path, h+string(filepath.Separator)) {
			return "~" + path[len(h):]
		}
	}
	return path
}

// removedLines lists the lines of before that are missing from after, in
// order. after is before with whole lines deleted, so a greedy match is exact.
func removedLines(before, after string) []string {
	a := strings.SplitAfter(before, "\n")
	b := strings.SplitAfter(after, "\n")
	var gone []string
	j := 0
	for _, l := range a {
		if j < len(b) && b[j] == l {
			j++
			continue
		}
		gone = append(gone, strings.TrimRight(l, "\r\n"))
	}
	return gone
}

func removeKeyBlock(u uninstallIO, in *bufio.Reader, yes bool) error {
	path := u.keysPath
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // edit through a dotfiles symlink
	}
	shown := tilde(path, u.home)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(u.out, "No Herdr config at %s: nothing to remove.\n", shown)
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
		fmt.Fprintf(u.out, "%d herdr-dots binding(s) in %s use a syntax this command can't edit: remove them by hand.\n", left, shown)
	}
	if removed == 0 {
		fmt.Fprintln(u.out, "No herdr-dots keybindings to remove.")
		return nil
	}
	fmt.Fprintf(u.out, "Will remove %d herdr-dots keybinding(s) from %s. These exact lines go; everything else stays:\n", removed, shown)
	for _, l := range removedLines(string(data), out) {
		fmt.Fprintf(u.out, "- %s\n", l)
	}
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
	fmt.Fprintf(u.out, "Removed. Backup: %s\nRun `herdr server reload-config` (or restart Herdr) to drop them now.\n", tilde(backup, u.home))
	return nil
}

// purgePreflight checks everything a purge needs, without changing anything:
// the directories look like the plugin's, do not hold each other or Herdr's
// config, are not links, and no engine is running.
func purgePreflight(u uninstallIO, p paths.Paths) error {
	dirs := []string{p.ConfigDir, p.StateDir}
	for _, d := range dirs {
		if !safeToPurge(d, u.home) {
			return fmt.Errorf("refusing to delete %q: it is not a plugin directory", tilde(d, u.home))
		}
	}
	a, b := filepath.Clean(p.ConfigDir), filepath.Clean(p.StateDir)
	if within(a, b) || within(b, a) {
		return fmt.Errorf("refusing to purge: %s contains %s", tilde(a, u.home), tilde(b, u.home))
	}
	for _, d := range dirs {
		if u.keysPath != "" && within(filepath.Clean(d), filepath.Clean(u.keysPath)) {
			return fmt.Errorf("refusing to purge: %s contains Herdr's config %s", tilde(d, u.home), tilde(u.keysPath, u.home))
		}
		st, err := os.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("refusing to purge %s: %v", tilde(d, u.home), err)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			target, _ := filepath.EvalSymlinks(d)
			if target == "" {
				target, _ = os.Readlink(d)
			}
			return fmt.Errorf("%s is a link to %s; remove that yourself", tilde(d, u.home), tilde(target, u.home))
		}
	}
	release, err := lockEngines(p.StateDir)
	if err != nil {
		return err
	}
	release()
	return nil
}

func purgeDirs(u uninstallIO, in *bufio.Reader, p paths.Paths) error {
	fmt.Fprintf(u.out, "\nThis deletes, with no undo:\n  %s (dots, settings, credentials)\n  %s (memory of every project, queues, history)\n", tilde(p.ConfigDir, u.home), tilde(p.StateDir, u.home))
	fmt.Fprintf(u.out, "Type %q to confirm: ", purgeConfirm)
	line, _ := in.ReadString('\n')
	if strings.TrimSpace(line) != purgeConfirm {
		fmt.Fprintln(u.out, "Not confirmed. Nothing deleted.")
		return nil
	}
	// An engine may have started while the developer typed: lock every
	// session and keep the locks until the data is gone.
	release, err := lockEngines(p.StateDir)
	if err != nil {
		return err
	}
	defer release()
	if err := removeStateKeepingLocks(p.StateDir); err != nil {
		return err
	}
	release() // nothing but lock files and empty session dirs is left
	for _, d := range []string{p.StateDir, p.ConfigDir} {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	for _, d := range []string{p.ConfigDir, p.StateDir} {
		if _, err := os.Lstat(d); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s was not fully deleted; nothing more was done", tilde(d, u.home))
		}
	}
	fmt.Fprintln(u.out, "Deleted. Run `herdr plugin uninstall herdr-dots` to remove the plugin itself.")
	return nil
}

// removeStateKeepingLocks deletes everything in the state dir except the
// session engine.lock files, which stay (and stay locked) until the caller
// releases them: an open file cannot be removed on Windows.
func removeStateKeepingLocks(stateDir string) error {
	ents, err := os.ReadDir(stateDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range ents {
		path := filepath.Join(stateDir, e.Name())
		if !e.IsDir() || !uninstallSessionRe.MatchString(e.Name()) {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			continue
		}
		inner, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, f := range inner {
			if f.Name() == "engine.lock" {
				continue
			}
			if err := os.RemoveAll(filepath.Join(path, f.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// lockEngines takes the engine lock of every session under stateDir and
// returns a function that releases them all. If any lock is busy, or
// anything cannot be checked, nothing stays locked and it refuses.
func lockEngines(stateDir string) (func(), error) {
	ents, err := os.ReadDir(stateDir)
	if errors.Is(err, fs.ErrNotExist) {
		return func() {}, nil // no state, no engine
	}
	if err != nil {
		return nil, fmt.Errorf("can't tell whether a Dots engine is running (%v): quit Herdr first, then purge", err)
	}
	var held []*lockfile.Lock
	release := func() {
		for _, l := range held {
			_ = l.Release()
		}
		held = nil
	}
	var busy []string
	for _, e := range ents {
		if !e.IsDir() || !uninstallSessionRe.MatchString(e.Name()) {
			continue
		}
		l, err := lockfile.TryLock(filepath.Join(stateDir, e.Name(), "engine.lock"))
		switch {
		case err == nil:
			held = append(held, l)
		case errors.Is(err, lockfile.ErrLocked):
			busy = append(busy, e.Name())
		default:
			release()
			return nil, fmt.Errorf("can't tell whether the Dots engine of session %s is running (%v): quit Herdr first, then purge", e.Name(), err)
		}
	}
	if len(busy) > 0 {
		release()
		return nil, fmt.Errorf("%d Dots engine(s) are running (%s): quit Herdr first, then purge", len(busy), strings.Join(busy, ", "))
	}
	return release, nil
}

func uninstallConfirm(out io.Writer, in *bufio.Reader, prompt string) bool {
	fmt.Fprint(out, prompt)
	line, _ := in.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

// within reports whether child is parent or lies under it.
func within(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}

// safeToPurge accepts only a directory that looks like the plugin's own: not
// relative, not a filesystem root, not the home directory, anything above
// it, or any of its first-level directories (~/.config, ~/Documents), and
// either named like the plugin or holding a plugin marker.
func safeToPurge(dir, home string) bool {
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	dir = filepath.Clean(dir)
	if filepath.Dir(dir) == dir {
		return false
	}
	if home != "" {
		h := filepath.Clean(home)
		if within(dir, h) || filepath.Dir(dir) == h {
			return false
		}
	}
	if len(strings.Split(strings.Trim(filepath.ToSlash(dir), "/"), "/")) < 3 {
		return false
	}
	return filepath.Base(dir) == paths.PluginID || hasPluginMarker(dir)
}

// hasPluginMarker reports whether dir holds something only the plugin's
// config dir (.state-dir, credentials.toml, dots/) or state dir (memory/, a
// session dir) holds.
func hasPluginMarker(dir string) bool {
	for _, f := range []string{paths.StatePointer, "credentials.toml"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	for _, d := range []string{"dots", "memory"} {
		if st, err := os.Stat(filepath.Join(dir, d)); err == nil && st.IsDir() {
			return true
		}
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() && uninstallSessionRe.MatchString(e.Name()) {
			return true
		}
	}
	return false
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

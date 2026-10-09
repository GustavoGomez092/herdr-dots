package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/migrate"
	"herdr-dots/internal/paths"
)

func init() { commands["migrate"] = runMigrate }

// startupMigrations runs at engine start, while the engine holds its session
// lock: it upgrades config, shared state (under each file's lock) and this
// session's files, and marks files from a newer version read-only.
func startupMigrations(p paths.Paths, logf func(string, ...any)) migrate.Report {
	rep, err := migrate.Default().Run(migrate.RootsOf(p), migrate.Options{Logf: logf})
	if err != nil {
		logf("migrate: %v", err)
	}
	if !rep.Empty() {
		logf("migrate: %s", rep.Summary())
	}
	return rep
}

// readOnlyNotice is the toast for files a newer herdr-dots wrote ("" for none).
func readOnlyNotice(rep migrate.Report) string {
	if len(rep.ReadOnly) == 0 {
		return ""
	}
	names := make([]string, 0, len(rep.ReadOnly))
	for _, f := range rep.ReadOnly {
		names = append(names, filepath.Base(f))
	}
	return fmt.Sprintf("%d file(s) were written by a newer herdr-dots and stay read-only: %s. Update the plugin, or see engine.log.",
		len(names), strings.Join(names, ", "))
}

// writingCommands are the commands that write persisted files outside the
// engine. Before they run, files a newer version wrote are marked read-only
// in this process too: credentials (keys, manage), dots edit/import (dots,
// manage), mcp-install.json (setup-mcp), uninstall-extras, and the memory
// commands (memory files also refuse newer versions themselves, under their
// lock, but the gate costs one read per file and memory is VITAL). `mcp`
// is left out: it writes through the engine, and its start stays fast.
var writingCommands = map[string]bool{
	"manage": true, "keys": true, "dots": true, "setup-mcp": true, "uninstall-extras": true, "memory": true,
}

// protectNewerFiles marks newer files read-only for this process. It only
// reads versions; any error just means nothing is marked.
func protectNewerFiles(cmd string) {
	if !writingCommands[cmd] {
		return
	}
	p, err := paths.FromEnv()
	if err != nil {
		if p, err = paths.Discover(); err != nil {
			return
		}
	}
	_, _ = migrate.Default().Run(migrate.RootsOf(p), migrate.Options{DryRun: true})
}

var sessionDirRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

func runMigrate(args []string) error {
	p, err := paths.Discover()
	if err != nil {
		return err
	}
	return migrateCmd(args, p, os.Stdout)
}

// migrateCmd upgrades config and shared state, then every session directory
// whose engine is not running (a running engine migrates its own session at
// its next start). --dry-run only reports.
func migrateCmd(args []string, p paths.Paths, out io.Writer) error {
	return migrateWith(migrate.Default(), args, p, out)
}

// migrateWith is migrateCmd with the set of kinds and migrations injected.
func migrateWith(set migrate.Set, args []string, p paths.Paths, out io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "report what would change; write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o := migrate.Options{DryRun: *dry}
	total := migrate.Report{}
	add := func(r migrate.Report) {
		total.Migrated = append(total.Migrated, r.Migrated...)
		total.Pending = append(total.Pending, r.Pending...)
		total.ReadOnly = append(total.ReadOnly, r.ReadOnly...)
		total.Failed = append(total.Failed, r.Failed...)
		total.Corrupt = append(total.Corrupt, r.Corrupt...)
		total.Backups = append(total.Backups, r.Backups...)
	}
	rep, err := set.Run(migrate.Roots{Config: p.ConfigDir, State: p.StateDir}, o)
	if err != nil {
		return err
	}
	add(rep)
	ents, _ := os.ReadDir(p.StateDir)
	var busy []string
	for _, e := range ents {
		if !e.IsDir() || !sessionDirRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(p.StateDir, e.Name())
		// The session kinds (queue, attachments, usage, patch-meta) have no
		// lock of their own: engine.lock guards them. Hold it for the whole
		// session, so no engine starts on the files mid-rewrite.
		lock, err := lockfile.TryLock(filepath.Join(dir, "engine.lock"))
		if errors.Is(err, lockfile.ErrLocked) {
			busy = append(busy, e.Name())
			continue
		}
		if err != nil {
			total.Failed = append(total.Failed, fmt.Sprintf("%s: engine.lock: %v", dir, err))
			continue
		}
		rep, err := set.Run(migrate.Roots{Session: dir}, o)
		_ = lock.Release()
		if err != nil {
			return err
		}
		add(rep)
	}
	sort.Strings(busy)
	fmt.Fprintln(out, total.Summary())
	for _, b := range total.Backups {
		fmt.Fprintf(out, "backup: %s\n", b)
	}
	if len(busy) > 0 {
		fmt.Fprintf(out, "skipped %d running session(s) (%s): their engines migrate them at the next start\n", len(busy), strings.Join(busy, ", "))
	}
	if len(total.Failed) > 0 {
		return fmt.Errorf("%d file(s) could not be migrated", len(total.Failed))
	}
	return nil
}

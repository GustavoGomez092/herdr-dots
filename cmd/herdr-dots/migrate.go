package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/migrate"
	"herdr-dots/internal/paths"
)

func init() { commands["migrate"] = runMigrate }

// migrationSet is the set the engine, the CLI gate and `migrate` run (tests
// swap it).
var migrationSet = migrate.Default

// startupMigrations runs at engine start, while the engine holds its session
// lock: it upgrades config, shared state (under each file's lock) and this
// session's files, and marks files from a newer version read-only. A set
// that can't run (an invalid migration chain) is an error: the engine then
// refuses to start rather than run without knowing which files are newer.
func startupMigrations(p paths.Paths, logf func(string, ...any)) (migrate.Report, error) {
	rep, err := migrationSet().Run(migrate.RootsOf(p), migrate.Options{Logf: logf})
	if err != nil {
		return rep, fmt.Errorf("schema migrations can't run, so newer files can't be protected: %w", err)
	}
	if !rep.Empty() {
		logf("migrate: %s", rep.Summary())
	}
	return rep, nil
}

type notifier interface {
	Notify(ctx context.Context, title, body string) error
}

// readOnlyToastTimeout bounds the read-only toast: a slow Herdr socket must
// not hold the engine up.
const readOnlyToastTimeout = 2 * time.Second

// notifyReadOnly shows the read-only toast, giving up after timeout.
func notifyReadOnly(ctx context.Context, n notifier, msg string, timeout time.Duration, logf func(string, ...any)) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := n.Notify(ctx, "Herdr Dots", msg); err != nil {
		logf("notify: %v", err)
	}
}

// gateExempt are the commands that skip the CLI gate, with the reason. Every
// command is in exactly one of writingCommands and gateExempt
// (TestEveryCommandIsClassifiedForTheGate).
var gateExempt = map[string]string{
	"version":        "writes nothing",
	"--version":      "writes nothing",
	"engine":         "runs the full migration itself at start, under engine.lock (serveEngine)",
	"migrate":        "is the migration",
	"pane":           paneGateReason,
	"summon-ui":      "UI: writes through the engine (IPC)",
	"diff-ui":        "UI: reads one patch; apply and discard go through the engine (IPC)",
	"action":         "writes only dotspanes.json, an unversioned runtime cache",
	"setup-keys":     "writes only Herdr's own config.toml, not a herdr-dots file",
	"verify-release": "reads bin/INSTALL_SOURCE and the kept release archive; writes nothing",
	"mcp": "writes memory proposals directly through memory.Store, which is safe only because the memory " +
		"kinds are self-guarded (memory.ErrVersion under memory.lock); everything else goes through the engine",
}

// paneGateReason: the Dots pane's first-run Setup screen (post-rc) writes
// outside the engine too, but only files whose own writers refuse a newer
// version, and configs that aren't herdr-dots files.
const paneGateReason = "UI: writes through the engine (IPC); its first-run Setup screen writes only onboarding.json " +
	"and mcp-install.json, whose writers refuse a newer version, and the agents' and Herdr's own configs"

// readOnlyNotice is the toast for files kept read-only ("" for none): a
// newer herdr-dots wrote them, or their migration failed.
func readOnlyNotice(rep migrate.Report) string {
	if len(rep.ReadOnly) == 0 {
		return ""
	}
	names := make([]string, 0, len(rep.ReadOnly))
	for _, f := range rep.ReadOnly {
		names = append(names, filepath.Base(f))
	}
	return fmt.Sprintf("%d file(s) stay read-only (written by a newer herdr-dots, or could not be migrated; see engine.log): %s.",
		len(names), strings.Join(names, ", "))
}

// writingCommands are the commands that write persisted files outside the
// engine. Before they run, files a newer version wrote are marked read-only
// in this process too: credentials (keys, manage), dots edit/import (dots,
// manage), mcp-install.json and onboarding.json (setup-mcp, setup),
// uninstall-extras, and the memory
// commands (memory files also refuse newer versions themselves, under their
// lock, but the gate costs one read per file and memory is VITAL). `mcp`
// is left out (see gateExempt): it writes memory proposals directly, which
// is safe only because the memory kinds are self-guarded, and its start
// stays fast.
var writingCommands = map[string]bool{
	"manage": true, "keys": true, "dots": true, "setup-mcp": true, "setup": true, "uninstall-extras": true, "memory": true,
}

// protectNewerFiles marks newer files read-only for this process. It only
// reads versions. When the plugin dirs can't be found nothing is marked (the
// command can't find its files either); a migration set that can't run is
// an error, so the command is refused rather than run unprotected.
func protectNewerFiles(cmd string) error {
	if !writingCommands[cmd] {
		return nil
	}
	p, err := paths.FromEnv()
	if err != nil {
		if p, err = paths.Discover(); err != nil {
			return nil
		}
	}
	if _, err := migrationSet().Run(migrate.RootsOf(p), migrate.Options{DryRun: true}); err != nil {
		return fmt.Errorf("%s: schema migrations can't run, so newer files can't be protected: %w", cmd, err)
	}
	return nil
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
	return migrateWith(migrationSet(), args, p, out)
}

// migrateWith is migrateCmd with the set of kinds and migrations injected.
func migrateWith(set migrate.Set, args []string, p paths.Paths, out io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dry := flags.Bool("dry-run", false, "report what would change; write nothing")
	if err := flags.Parse(args); err != nil {
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
	ents, err := os.ReadDir(p.StateDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// The sessions can't be listed, so they can't be checked: never
		// "all files current".
		total.Failed = append(total.Failed, fmt.Sprintf("%s: %v", p.StateDir, err))
	}
	var busy []string
	for _, e := range ents {
		if !e.IsDir() || !sessionDirRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(p.StateDir, e.Name())
		lockPath := filepath.Join(dir, "engine.lock")
		if _, err := os.Lstat(lockPath); *dry && errors.Is(err, fs.ErrNotExist) {
			// No engine ever ran here, and a dry run writes nothing: not
			// even a lock file.
			rep, err := set.Run(migrate.Roots{Session: dir}, o)
			if err != nil {
				return err
			}
			add(rep)
			continue
		}
		// The session kinds (queue, attachments, usage, patch-meta) have no
		// lock of their own: engine.lock guards them. Hold it for the whole
		// session, so no engine starts on the files mid-rewrite.
		lock, err := lockfile.TryLock(lockPath)
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
		return fmt.Errorf("%d file(s) or folder(s) could not be migrated", len(total.Failed))
	}
	return nil
}

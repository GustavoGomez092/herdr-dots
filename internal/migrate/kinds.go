package migrate

import "path/filepath"

// memoryLock is the lock that guards a memory file: memory.lock in its
// project (or _global) directory, one level up for files under dots/.
func memoryLock(file string) string {
	dir := filepath.Dir(file)
	if filepath.Base(dir) == "dots" {
		dir = filepath.Dir(dir)
	}
	return filepath.Join(dir, "memory.lock")
}

// siblingLock is <file>.lock, the lock daily.json, inbox.json,
// trusted.json, onboarding.json and credentials.toml are written under.
func siblingLock(file string) string { return file + ".lock" }

// Default is every persisted file kind herdr-dots 1.0 knows. Every kind is
// at version 1 and there are no migrations yet. Kinds a phase adds must be
// listed here: TestEveryPathsFileIsClassified fails until they are.
//
// Migrations are registered in Set.Migrations here. Phase 2 memory is VITAL:
// a migration must never lose or reactivate memory. When the first real
// migration of a memory kind (memory-scope, memory-meta, schedule) is
// registered (none exists yet), it must add a post-Apply invariant check
// that compares the old and new file and refuses the upgrade unless every
// entry's id, order and status and the entry count are unchanged (no entry
// dropped, no dismissed, archived or forgotten entry made active again).
//
// The first migration of any kind: the CLI gate (protectNewerFiles in
// cmd/herdr-dots, a DryRun) reports files that need a migration as Pending
// but does not protect them, so until then a writing command could rewrite
// an older file without its migration and backup. No file can be pending
// today (every kind is at version 1). Before registering it, make the gate
// protect that kind's Pending files too, or migrate them first (triage Q16).
func Default() Set {
	return Set{Kinds: []Kind{
		// Config dir (the developer edits these; atomic replace keeps them safe).
		{Name: "config", Root: ConfigRoot, Globs: []string{"config.toml"}, Format: TOML, Current: 1},
		{Name: "dot", Root: ConfigRoot, Globs: []string{"dots/*.toml"}, Format: TOML, Current: 1},
		// credentials.toml is a table per provider: it can never hold a
		// top-level version key, so it stays at 1 until its format changes
		// shape. It holds keys, so a parse error's text is never reported.
		// Its lock is <file>.lock, the one `keys set` takes (credentials.File).
		{Name: "credentials", Root: ConfigRoot, Globs: []string{"credentials.toml"}, Format: TOML, Current: 1, Secret: true,
			Lock: siblingLock},
		{Name: "prices", Root: ConfigRoot, Globs: []string{"prices.toml"}, Format: TOML, Current: 1},
		{Name: "mcp-install", Root: ConfigRoot, Globs: []string{"mcp-install.json"}, Format: JSON, Current: 1,
			SelfGuarded: true}, // mcpinstall.ErrRecordsVersion; no lock yet, so no migration can be registered
		{Name: "trust", Root: ConfigRoot, Globs: []string{"trusted.json"}, Format: JSON, Current: 1,
			Lock: siblingLock, SelfGuarded: true}, // projectdots.ErrNewer
		// The first-run Setup screen's answers (post-rc); onboarding.Store
		// writes it under <file>.lock and refuses a newer one (ErrNewer).
		{Name: "onboarding", Root: ConfigRoot, Globs: []string{"onboarding.json"}, Format: JSON, Current: 1,
			Lock: siblingLock, SelfGuarded: true},

		// State dir, shared by every session.
		{Name: "memory-scope", Root: StateRoot, Globs: []string{"memory/*/project.json", "memory/*/dots/*.json"},
			Format: JSON, Current: 1, Lock: memoryLock, SelfGuarded: true}, // memory.ErrVersion
		{Name: "memory-meta", Root: StateRoot, Globs: []string{"memory/*/meta.json"},
			Format: JSON, Current: 1, Lock: memoryLock, SelfGuarded: true},
		{Name: "schedule", Root: StateRoot, Globs: []string{"memory/*/schedule.json"},
			Format: JSON, Current: 1, Lock: memoryLock, SelfGuarded: true},
		{Name: "daily", Root: StateRoot, Globs: []string{"daily.json"}, Format: JSON, Current: 1,
			Lock: siblingLock, SelfGuarded: true}, // state.ErrDailyVersion
		{Name: "inbox", Root: StateRoot, Globs: []string{"inbox.json"}, Format: JSON, Current: 1,
			Lock: siblingLock, SelfGuarded: true}, // queue.ErrInboxVersion

		// One session's directory (the engine that runs Run holds its lock).
		{Name: "attachments", Root: SessionRoot, Globs: []string{"attachments.json"}, Format: JSON, Current: 1},
		{Name: "queue", Root: SessionRoot, Globs: []string{"queue.json"}, Format: JSON, Current: 1},
		{Name: "usage", Root: SessionRoot, Globs: []string{"usage.json"}, Format: JSON, Current: 1},
		{Name: "patch-meta", Root: SessionRoot, Globs: []string{"patches/*.json"}, Format: JSON, Current: 1},
	}}
}

// Unversioned lists the persisted paths that are deliberately not kinds, by
// paths.Paths method name, with the reason. TestEveryPathsFileIsClassified
// accepts a method only if it is here or maps to a kind in Default.
var Unversioned = map[string]string{
	"EngineLock":       "runtime lock, rewritten by every engine",
	"EngineLog":        "log text",
	"EngineSock":       "socket (Unix)",
	"EngineEndpoint":   "socket or named pipe",
	"FeedFile":         "append-only JSONL: records only gain optional fields",
	"DotsPanesFile":    "runtime cache of open Dots panes, rebuilt on demand",
	"StatePointerFile": "one line of text: the state dir",
	"HistoryDir":       "append-only JSONL per dot: records only gain optional fields",
	"WorktreesDir":     "transient git worktrees, owned by git and pruned",
}

// KindPaths maps paths.Paths methods to the kind that covers what they
// return.
var KindPaths = map[string]string{
	"ConfigFile":      "config",
	"DotsDir":         "dot",
	"CredentialsFile": "credentials",
	"PricesFile":      "prices",
	"TrustFile":       "trust",
	"OnboardingFile":  "onboarding",
	"MemoryDir":       "memory-scope",
	"DailyFile":       "daily",
	"InboxFile":       "inbox",
	"AttachmentsFile": "attachments",
	"QueueFile":       "queue",
	"UsageFile":       "usage",
	"PatchesDir":      "patch-meta",
}

// Appended lists the append-only JSONL logs. They are not kinds: each line
// is one record that only ever gains optional fields, and a breaking change
// must use a new file name (CONTRIBUTING.md).
var Appended = map[string]string{
	"feed.jsonl":            "session feed (state.Feed)",
	"memory/*/ledger.jsonl": "card outcomes a dot learned from (memory.Store ledger)",
	"history/*.jsonl":       "per-dot run history (history.Store)",
}

// External lists formats herdr-dots reads from files outside its own
// directories. Run never touches them; each reader checks its own version
// and refuses a newer one.
var External = map[string]string{
	".dotpack.toml":           "dot pack, top-level format = 1; dotpack.Parse refuses a newer format (ErrFormat)",
	"memory export (.toml)":   "herdr-dots memory export, version = 1; memory.ImportTOML refuses a newer version (ErrVersion)",
	".herdr-dots/dots/*.toml": "project dot files in a repository; dots.DecodeStrict refuses keys it doesn't know",
}

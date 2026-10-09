// Package platform holds herdr-dots' operating-system differences: where
// other programs keep their files, how child processes start and die, how
// command lines reach cmd.exe shims, file privacy and named pipes. Business
// logic calls these helpers instead of branching on runtime.GOOS; each helper
// has a Unix file and a Windows file behind build tags.
package platform

import (
	"errors"
	"path/filepath"
)

var (
	// ErrCmdTooLong means a command line exceeds cmd.exe's limit.
	ErrCmdTooLong = errors.New("command line too long")
	// ErrUnsafeBatchArg means an argument holds a character cmd.exe would
	// interpret (" % ! or a line break) and can't be passed to a .cmd shim.
	ErrUnsafeBatchArg = errors.New("argument can't be passed safely to a .cmd/.bat program")
)

// MaxInlinePrompt is the longest system prompt passed as a command-line
// argument; longer prompts go through a private temp file on every OS.
const MaxInlinePrompt = 4000

// MaxBatchCmdLine is cmd.exe's command-line limit, in UTF-16 code units.
const MaxBatchCmdLine = 8191

// HerdrConfigPath is Herdr's config.toml: HERDR_CONFIG_PATH, then
// $XDG_CONFIG_HOME/herdr/config.toml, then the OS default (Unix:
// ~/.config/herdr; Windows: %APPDATA%\herdr). These are Herdr's own rules.
func HerdrConfigPath(getenv func(string) string, home string) string {
	if p := getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "herdr", "config.toml")
	}
	return filepath.Join(herdrConfigDir(getenv, home), "config.toml")
}

// unixHerdrConfigDir is Herdr's default config dir on macOS and Linux.
func unixHerdrConfigDir(home string) string {
	return filepath.Join(home, ".config", "herdr")
}

// windowsHerdrConfigDir is Herdr's default config dir on Windows:
// %APPDATA%\herdr, then %USERPROFILE%\AppData\Roaming\herdr, then
// ~/.config/herdr.
func windowsHerdrConfigDir(getenv func(string) string, home string) string {
	if a := getenv("APPDATA"); a != "" {
		return filepath.Join(a, "herdr")
	}
	if u := getenv("USERPROFILE"); u != "" {
		return filepath.Join(u, "AppData", "Roaming", "herdr")
	}
	return unixHerdrConfigDir(home)
}

// ClaudeProjectsDir holds Claude Code's transcripts, one folder per project,
// on every OS (Claude Code uses the user's home, %USERPROFILE% on Windows).
func ClaudeProjectsDir(home string) string { return filepath.Join(home, ".claude", "projects") }

// CodexSessionsDir holds Codex rollout files (YYYY/MM/DD/rollout-*.jsonl).
func CodexSessionsDir(home string) string { return filepath.Join(home, ".codex", "sessions") }

// ClaudeUserConfig is Claude Code's user config (MCP servers at user scope).
func ClaudeUserConfig(home string) string { return filepath.Join(home, ".claude.json") }

// CodexConfig is Codex's config.toml.
func CodexConfig(home string) string { return filepath.Join(home, ".codex", "config.toml") }

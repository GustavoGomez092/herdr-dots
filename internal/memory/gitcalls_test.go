package memory

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// gitExecRe finds a git child process started from Go: exec.Command("git",
// exec.CommandContext(ctx, "git", platform.Command(..., "git", and so on.
var gitExecRe = regexp.MustCompile(`Command(Context)?\([^)\n]*"git"`)

// childRe finds any child process start (exec.Command, platform.Command,
// with or without Context).
var childRe = regexp.MustCompile(`Command(Context)?\(`)

// gitSites are the files known to run git. The scan must find each one, so a
// rewrite that slips past the patterns fails here instead of going unchecked.
var gitSites = []string{
	"internal/backend/toolloop/tools.go", // repository listing (gitFiles)
	"internal/memory/project.go",         // ExecGit
	"internal/worktree/git.go",           // worktree.Git
}

// gitPins are the settings every git child carries: no hooks, no fsmonitor
// program, no transport (the -c keys and the environment overrides).
var gitPins = []string{"core.fsmonitor=false", "core.hooksPath=", "protocol.allow=never",
	"GIT_ALLOW_PROTOCOL=none", "GIT_NO_LAZY_FETCH=1"}

// Every place that runs git in a developer's repository must override the
// repository settings that run programs (hooks, fsmonitor). A repository is
// untrusted input: cloning one must never let it run code through herdr-dots.
// Phase 7's internal/worktree runs git through a configurable binary, so its
// files that start a child process are checked even without a "git" literal.
func TestEveryGitCallDisablesRepositoryPrograms(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Dot directories hold other checkouts (.claude/worktrees,
			// .worktrees) and tool state, not this tree's code.
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			switch d.Name() {
			case ".git", "docs", "dist", "bin", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := string(b)
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		inWorktree := strings.HasPrefix(rel, "internal/worktree/") && childRe.MatchString(src)
		if !gitExecRe.MatchString(src) && !inWorktree {
			return nil
		}
		checked[rel] = true
		// The quoted string literal, so a comment naming the setting
		// doesn't count as passing it.
		for _, want := range gitPins {
			if !strings.Contains(src, `"`+want+`"`) {
				t.Errorf("%s runs git without %s", rel, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) == 0 {
		t.Fatal("found no git calls at all: the pattern is broken")
	}
	for _, site := range gitSites {
		if !checked[site] {
			t.Errorf("%s runs git but the scan didn't find it: the pattern is broken", site)
		}
	}
}

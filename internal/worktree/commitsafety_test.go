package worktree

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ruling P8-R13: every command Git runs pins diff.submodule=short (no child
// `git diff` inside a submodule, which would read the submodule's own
// diff.external or textconv drivers) and protocol.allow=never (no fetch,
// lazy or not, so no transport program from the repository's config runs),
// whatever the repository's config says.
func TestEveryCommandPinsSubmoduleDiffAndProtocol(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	sh(t, repo, "init", "-q")
	sh(t, repo, "config", "diff.submodule", "diff")
	sh(t, repo, "config", "protocol.allow", "always")
	g := isoGit(t)
	gf, err := g.WithoutFilters(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	runs := map[string]func(args ...string) (string, error){
		"Run": func(a ...string) (string, error) { return g.Run(ctx, repo, nil, a...) },
		"Output": func(a ...string) (string, error) {
			out, err := g.Output(ctx, repo, nil, a...)
			return string(out), err
		},
		"OutputIn": func(a ...string) (string, error) {
			out, err := g.OutputIn(ctx, repo, nil, []byte("x"), a...)
			return string(out), err
		},
		"WithoutFilters": func(a ...string) (string, error) { return gf.Run(ctx, repo, nil, a...) },
	}
	for name, run := range runs {
		for key, want := range map[string]string{"diff.submodule": "short", "protocol.allow": "never"} {
			got, err := run("config", "--get", key)
			if err != nil {
				t.Fatalf("%s: git config --get %s: %v", name, key, err)
			}
			if strings.TrimSpace(got) != want {
				t.Errorf("%s: %s = %q, want %q pinned on every command", name, key, got, want)
			}
		}
	}
}

// MaxOutput bounds what Git keeps of a command's stdout: 50 MB of output
// comes back as exactly the cap, read and dropped past it, not buffered.
func TestMaxOutputCapsWhatIsKept(t *testing.T) {
	repo := t.TempDir()
	sh(t, repo, "init", "-q")
	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), 50<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	blob := sh(t, repo, "hash-object", "-w", big)
	g := isoGit(t)
	g.MaxOutput = 1 << 20
	out, err := g.Output(context.Background(), repo, nil, "cat-file", "-p", blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1<<20 {
		t.Fatalf("kept %d bytes of 50 MB, want the %d-byte cap", len(out), 1<<20)
	}
}

// A partial clone's missing blob is never fetched lazily, even by a git that
// ignores GIT_NO_LAZY_FETCH (before 2.44): protocol.allow=never refuses the
// transport, so the repository's remote.origin.uploadpack program never runs.
func TestNoLazyFetchWhereGitIgnoresNoLazyFetch(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	sh(t, src, "init", "-q")
	writeFile(t, filepath.Join(src, "f.txt"), "remote only\n")
	sh(t, src, "add", "-A")
	sh(t, src, "commit", "-q", "-m", "one")
	sh(t, src, "config", "uploadpack.allowFilter", "true")
	sh(t, src, "config", "uploadpack.allowAnySHA1InWant", "true")
	clone := filepath.Join(t.TempDir(), "clone")
	sh(t, src, "-c", "protocol.file.allow=always", "clone", "-q", "--no-checkout", "--filter=blob:none",
		"file://"+filepath.ToSlash(src), clone)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "upload.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$0 $*\" >> '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	sh(t, clone, "config", "remote.origin.uploadpack", script)
	sh(t, clone, "config", "protocol.allow", "always")
	// GIT_NO_LAZY_FETCH=0 stands in for a git that does not know the variable.
	_, _ = isoGit(t).Run(ctx, clone, []string{"GIT_NO_LAZY_FETCH=0"}, "show", "--patch", "--no-ext-diff", "--no-textconv", "HEAD")
	if b, err := os.ReadFile(marker); err == nil {
		t.Fatalf("a lazy fetch ran the repository's upload-pack program:\n%s", b)
	}
}

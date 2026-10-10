package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// char is the one-character string for a code point, so invisible characters
// stay visible in this source file.
func char(cp rune) string { return string(cp) }

func TestInvisibleCharactersDoNotHideASecret(t *testing.T) {
	key := "sk-ant-api03-" + strings.Repeat("Ab3d", 6)
	for name, text := range map[string]string{
		"zero-width space":  "use " + key[:7] + char(0x200b) + key[7:],
		"zero-width joiner": "use " + key[:10] + char(0x200d) + key[10:],
		"word joiner":       "AKIA" + char(0x2060) + "ABCDEFGHIJKLMNOP",
		"soft hyphen":       "ghp_" + strings.Repeat("a1B2", 4) + char(0x00ad) + strings.Repeat("c3D4", 6),
		"byte order mark":   "token" + char(0xfeff) + " = s3cr3tV4lu3XyZw",
		"combining mark":    "sk-" + char(0x0336) + "ant-" + strings.Repeat("Ab3d", 6),
	} {
		if found, _ := ScanSecrets(text); !found {
			t.Errorf("%s: %q passed the scanner", name, text)
		}
	}
	prose := "Prefer the shared fetch wrapper." + char(0x200b) + " Emoji " + char(0x1f44d) + char(0x1f3fd) + " and caf" + char(0x00e9) + " are fine."
	if found, kind := ScanSecrets(prose); found {
		t.Fatalf("plain prose with invisible characters flagged as %s", kind)
	}
}

// Fillers that render as nothing but are letters or symbols, not format
// characters (Other_Default_Ignorable_Code_Point such as the Hangul fillers,
// and U+2800 BRAILLE PATTERN BLANK), can't split a key either.
func TestIgnorableFillersDoNotHideASecret(t *testing.T) {
	key := "sk-ant-api03-" + strings.Repeat("Ab3d", 6)
	for name, text := range map[string]string{
		"hangul filler":           "use " + key[:7] + char(0x3164) + key[7:],
		"halfwidth hangul filler": "AKIA" + char(0xffa0) + "ABCDEFGHIJKLMNOP",
		"braille blank":           "ghp_" + strings.Repeat("a1B2", 4) + char(0x2800) + strings.Repeat("c3D4", 6),
	} {
		if found, _ := ScanSecrets(text); !found {
			t.Errorf("%s: %q passed the scanner", name, text)
		}
	}
}

// A repository's own config can name an fsmonitor hook; ExecGit must not run
// it. (A core.pager is not set up here: git starts a pager only on a
// terminal, and ExecGit's stdout is a pipe, so such a setup would assert
// nothing.)
func TestExecGitRunsNothingTheRepositoryConfigures(t *testing.T) {
	isolateGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a shell script")
	}
	repo := newRepo(t, 1)
	marker := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "config", "core.fsmonitor", hook)
	// Plain git runs the hook, so the test can see the difference.
	_, _ = exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git does not run core.fsmonitor hooks; nothing to guard against here")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecGit(context.Background(), repo, "status", "--porcelain"); err != nil {
		t.Fatalf("ExecGit status: %v", err)
	}
	if _, err := ResolveProject(context.Background(), repo, ExecGit); err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("ExecGit ran a program from the repository's config")
	}
}

func TestFindProjectPrefersTheNestedRepository(t *testing.T) {
	s := testStore(t)
	outer := t.TempDir()
	inner := filepath.Join(outer, "vendor", "lib")
	if err := os.MkdirAll(filepath.Join(inner, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Projects are listed by name: the outer one sorts first, so a
	// first-match search finds it for every path under it.
	if err := s.SeeProject(Project{ID: "g-00000000000000aa", Name: "a-outer", Root: realPath(outer)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SeeProject(Project{ID: "g-00000000000000bb", Name: "z-lib", Root: realPath(inner)}); err != nil {
		t.Fatal(err)
	}
	for arg, want := range map[string]string{
		filepath.Join(inner, "src"):    "g-00000000000000bb",
		inner:                          "g-00000000000000bb",
		filepath.Join(outer, "vendor"): "g-00000000000000aa",
		outer:                          "g-00000000000000aa",
	} {
		p, ok, err := s.FindProject(arg)
		if err != nil || !ok || p.ID != want {
			t.Errorf("FindProject(%s) = %s %v %v, want %s", arg, p.ID, ok, err, want)
		}
	}
}

// Real repositories: an outer repository with a worktree inside it, and an
// independent repository vendored inside the outer one with its own worktree
// also inside the outer tree. Every path finds its own project, and the
// memory written for one project is never read back under the other. The
// names sort the outer project first, so a first-match search would hand the
// outer project the inner repository's paths.
func TestFindProjectKeepsNestedReposAndWorktreesApart(t *testing.T) {
	isolateGit(t)
	s := testStore(t)
	commit := func(dir, content string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		gitT(t, dir, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		gitT(t, dir, "add", "f.txt")
		gitT(t, dir, "commit", "-q", "-m", content)
	}
	outer := filepath.Join(t.TempDir(), "a-app")
	commit(outer, "outer")
	inner := filepath.Join(outer, "vendor", "zz-lib")
	commit(inner, "inner")
	outerWT := filepath.Join(outer, ".worktrees", "feature")
	gitT(t, outer, "worktree", "add", "-q", "-b", "feature", outerWT)
	innerWT := filepath.Join(outer, "zz-lib-fix")
	gitT(t, inner, "worktree", "add", "-q", "-b", "fix", innerWT)

	ids := map[string]string{}
	for _, dir := range []string{outer, inner, outerWT, innerWT} {
		pr, err := resolve(t, dir)
		if err != nil || !strings.HasPrefix(pr.ID, "g-") {
			t.Fatalf("resolve %s: %+v %v", dir, pr, err)
		}
		if err := s.SeeProject(pr); err != nil {
			t.Fatal(err)
		}
		ids[dir] = pr.ID
	}
	if ids[outer] == ids[inner] || ids[outerWT] != ids[outer] || ids[innerWT] != ids[inner] {
		t.Fatalf("identities: %v", ids)
	}
	mustAdd(t, s, ids[outer], Entry{Scope: ScopeProject, Kind: KindConvention, Text: "Outer app convention.", Source: SourceUser})
	mustAdd(t, s, ids[inner], Entry{Scope: ScopeProject, Kind: KindConvention, Text: "Vendored lib convention.", Source: SourceUser})

	for _, c := range []struct{ path, wantID, wantText string }{
		{outer, ids[outer], "Outer app convention."},
		{filepath.Join(outer, "vendor"), ids[outer], "Outer app convention."},
		{outerWT, ids[outer], "Outer app convention."},
		{filepath.Join(outerWT, "f.txt"), ids[outer], "Outer app convention."},
		{inner, ids[inner], "Vendored lib convention."},
		{filepath.Join(inner, "f.txt"), ids[inner], "Vendored lib convention."},
		{innerWT, ids[inner], "Vendored lib convention."},
		{filepath.Join(innerWT, "f.txt"), ids[inner], "Vendored lib convention."},
	} {
		p, ok, err := s.FindProject(c.path)
		if err != nil || !ok || p.ID != c.wantID {
			t.Errorf("FindProject(%s) = %s %v %v, want %s", c.path, p.ID, ok, err, c.wantID)
			continue
		}
		es := list(t, s, p.ID, Filter{Scope: ScopeProject})
		if len(es) != 1 || es[0].Text != c.wantText {
			t.Errorf("memory for %s: %+v, want only %q", c.path, es, c.wantText)
		}
	}
}

// Scanning without invisible characters must not turn ordinary writing into
// a "secret": scripts that use combining marks (Devanagari, Hebrew points,
// Arabic vowels, decomposed accents), the Persian zero-width non-joiner and
// emoji joiners all stay unflagged.
func TestInvisibleCharacterStrippingKeepsOrdinaryTextClean(t *testing.T) {
	for name, text := range map[string]string{
		"decomposed accents": "Use the cafe" + char(0x0301) + " helper; Tie" + char(0x0302) + char(0x0301) + "ng Vie" + char(0x0323) + char(0x0302) + "t labels stay.",
		"devanagari":         "हिन्दी में लिखे गए संदेश ठीक हैं।",
		"hebrew points":      "שָׁלוֹם עוֹלָם, the greeting stays.",
		"arabic vowels":      "مَرْحَبًا بِكُمْ in the welcome banner.",
		"persian zwnj":       "می" + char(0x200c) + "خواهم a short note.",
		"hangul":             "한국어 문장은 괜찮습니다.",
		"braille":            "⠓⠑⠇⠇⠕" + char(0x2800) + "⠺⠕⠗⠇⠙ in the docs.",
		"emoji sequences":    "Family " + char(0x1f468) + char(0x200d) + char(0x1f469) + char(0x200d) + char(0x1f467) + " and flag " + char(0x1f3f3) + char(0xfe0f) + char(0x200d) + char(0x1f308) + ".",
		"identifiers":        "func scanSecrets(text string) calls stripInvisible" + char(0x200b) + "Text before tokenize" + char(0x00ad) + "Config.",
	} {
		if found, kind := ScanSecrets(text); found {
			t.Errorf("%s: %q flagged as %s", name, text, kind)
		}
	}
}

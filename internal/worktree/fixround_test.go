package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// userConfig writes the isolated test user's global git config, the one the
// runner under test reads (testEnv gives every test an empty HOME, so no user
// config is exercised unless a test says so).
func userConfig(t *testing.T, g Git, text string) {
	t.Helper()
	for _, kv := range g.Environ() {
		if home, ok := strings.CutPrefix(kv, "HOME="); ok {
			if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("no HOME in the test environment")
}

func TestCaptureAndApplyIgnoreTheUsersDiffConfig(t *testing.T) {
	for name, cfg := range map[string]string{
		"noprefix":  "[diff]\n\tnoprefix = true\n",
		"mnemonic":  "[diff]\n\tmnemonicPrefix = true\n",
		"context0":  "[diff]\n\tcontext = 0\n",
		"all three": "[diff]\n\tnoprefix = true\n\tmnemonicPrefix = true\n\tcontext = 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			g := isoGit(t)
			userConfig(t, g, cfg)
			repo := newRepo(t)
			writeFile(t, filepath.Join(repo, "a.txt"), "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
			_, co, p, diff := draftT(t, g, repo, func(dir string) {
				writeFile(t, filepath.Join(dir, "src", "new.txt"), "new\n")
				writeFile(t, filepath.Join(dir, "a.txt"), "1\n2\n3\n4\nFIVE\n6\n7\n8\n9\n10\n")
			})
			if !strings.Contains(string(diff), "diff --git a/src/new.txt b/src/new.txt") {
				t.Fatalf("the diff header depends on the user's config:\n%s", diff)
			}
			if err := Validate(context.Background(), g, co, p, Caps{}); err != nil {
				t.Fatal(err)
			}
			if _, err := Apply(context.Background(), g, repo, diff); err != nil {
				t.Fatal(err)
			}
			if readFile(t, filepath.Join(repo, "src", "new.txt")) != "new\n" {
				t.Fatal("src/new.txt was not created")
			}
			if _, err := os.Stat(filepath.Join(repo, "new.txt")); err == nil {
				t.Fatal("a file landed at the repository root")
			}
			if !strings.Contains(readFile(t, filepath.Join(repo, "a.txt")), "FIVE") {
				t.Fatal("a.txt was not edited")
			}
		})
	}
}

func TestValidateRefusesHeaderPathsThatDifferFromTheChanges(t *testing.T) {
	g := isoGit(t)
	_, co, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		writeFile(t, filepath.Join(dir, "src", "new.txt"), "new\n")
	})
	r := strings.NewReplacer("a/src/new.txt", "src/new.txt", "b/src/new.txt", "src/new.txt")
	p.Diff = []byte(r.Replace(string(p.Diff))) // -p1 now lands it at new.txt
	if err := Validate(context.Background(), g, co, p, Caps{}); !errors.Is(err, ErrEscape) {
		t.Fatalf("err %v, want ErrEscape", err)
	}
}

func TestApplyDerivesItsPathsFromTheDiffItself(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	_, _, _, diff := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b2\n")
	})
	writeFile(t, filepath.Join(repo, "b.txt"), "b\nuser\n") // staged-or-not content must be "ours"
	if _, err := Apply(context.Background(), g, repo, diff); err != nil {
		t.Fatal(err)
	}
}

// runLoose runs git and reports the error instead of failing the test.
func runLoose(t *testing.T, dir string, args ...string) error {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = testEnv(t)
	_, err := cmd.CombinedOutput()
	return err
}

func TestAFailedApplyIsNotBlamedOnAnUnrelatedConflict(t *testing.T) {
	g := isoGit(t)
	repo := newRepo(t)
	sh(t, repo, "checkout", "-qb", "other")
	writeFile(t, filepath.Join(repo, "c.txt"), "other\n")
	sh(t, repo, "commit", "-qam", "other")
	sh(t, repo, "checkout", "-q", "main")
	writeFile(t, filepath.Join(repo, "c.txt"), "main\n")
	sh(t, repo, "commit", "-qam", "main")
	if err := runLoose(t, repo, "merge", "other"); err == nil {
		t.Fatal("the merge should conflict")
	}
	if sh(t, repo, "ls-files", "-u") == "" {
		t.Fatal("no unmerged entries")
	}
	_, _, _, diff := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b fixed\n")
	})
	if err := os.Remove(filepath.Join(repo, "b.txt")); err != nil {
		t.Fatal(err)
	}
	conflicts, err := Apply(context.Background(), g, repo, diff)
	if err == nil {
		t.Fatalf("a hard failure must be an error, got conflicts %v", conflicts)
	}
}

func TestValidateRefusesDeniedPaths(t *testing.T) {
	g := isoGit(t)
	deny := func(rel string) bool {
		base := strings.ToLower(filepath.Base(rel))
		return base == ".env" || strings.HasPrefix(base, "id_rsa") || base == ".npmrc"
	}
	for _, name := range []string{".env", "id_rsa", "sub/.NPMRC"} {
		_, co, p, _ := draftT(t, g, newRepo(t), func(dir string) {
			writeFile(t, filepath.Join(dir, name), "secret\n")
		})
		if err := Validate(context.Background(), g, co, p, Caps{Deny: deny}); !errors.Is(err, ErrDenied) {
			t.Errorf("%s: err %v, want ErrDenied", name, err)
		}
		if err := Validate(context.Background(), g, co, p, Caps{}); err != nil {
			t.Errorf("%s: without a deny func: %v", name, err)
		}
	}
}

func TestCaptureFlagsANewExecutableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit on Windows")
	}
	g := isoGit(t)
	_, _, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		writeFile(t, filepath.Join(dir, "run.sh"), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(dir, "run.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
	if !slices.Contains(p.Flags, FlagExecutable) {
		t.Fatalf("flags %v, want %s", p.Flags, FlagExecutable)
	}
	_, _, p, _ = draftT(t, g, newRepo(t), func(dir string) { writeFile(t, filepath.Join(dir, "plain.txt"), "x\n") })
	if slices.Contains(p.Flags, FlagExecutable) {
		t.Fatalf("flags %v: a plain file is not executable", p.Flags)
	}
}

func TestApplyWritesNoSharedIndexIntoTheUsersGitDir(t *testing.T) {
	g := isoGit(t)
	userConfig(t, g, "[core]\n\tsplitIndex = true\n")
	repo := newRepo(t)
	sh(t, repo, "update-index", "--split-index")
	_, _, _, diff := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b fixed\n")
	})
	before, _ := filepath.Glob(filepath.Join(repo, ".git", "sharedindex.*"))
	if _, err := Apply(context.Background(), g, repo, diff); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(filepath.Join(repo, ".git", "sharedindex.*"))
	if !slices.Equal(before, after) {
		t.Fatalf("sharedindex files before %v, after %v", before, after)
	}
}

func TestApplyRefusesASubdirectoryAndRunsUserFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell wrapper")
	}
	repo := newRepo(t)
	g := isoGit(t)
	_, _, _, diff := draftT(t, g, repo, func(dir string) {
		writeFile(t, filepath.Join(dir, "b.txt"), "b fixed\n")
	})
	sub := filepath.Join(repo, "sub")
	writeFile(t, filepath.Join(sub, "keep.txt"), "k\n")
	if _, err := Apply(context.Background(), g, sub, diff); err == nil {
		t.Fatal("Apply from a subdirectory must be refused")
	}
	if readFile(t, filepath.Join(repo, "b.txt")) != "b\n" {
		t.Fatal("a refused apply changed a file")
	}
	// The user's own LFS smudge filter must not be switched off for Apply.
	envFile := filepath.Join(t.TempDir(), "env")
	wrapper := filepath.Join(t.TempDir(), "spygit")
	writeFile(t, wrapper, "#!/bin/sh\ncase \"$*\" in *\" apply \"*) echo \"skip=${GIT_LFS_SKIP_SMUDGE-unset}\" >> '"+filepath.ToSlash(envFile)+"';; esac\nexec git \"$@\"\n")
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	spy := g
	spy.Bin = wrapper
	if _, err := Apply(context.Background(), spy, repo, diff); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, envFile); strings.Contains(got, "skip=1") {
		t.Fatalf("Apply ran with GIT_LFS_SKIP_SMUDGE set: %q", got)
	}
}

func TestValidateReadsASymlinkTargetFromThePatchNotTheWorktree(t *testing.T) {
	g := isoGit(t)
	var evil string
	_, co, p, _ := draftT(t, g, newRepo(t), func(dir string) {
		evil = filepath.Join(dir, "evil")
		symlinkOrSkip(t, "/etc/passwd", evil)
	})
	// After capture the dot swaps the link for a harmless one.
	if err := os.Remove(evil); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, "a.txt", evil)
	if err := Validate(context.Background(), g, co, p, Caps{}); !errors.Is(err, ErrEscape) {
		t.Fatalf("err %v, want ErrEscape from the patch's own blob", err)
	}
}

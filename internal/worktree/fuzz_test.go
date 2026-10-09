package worktree

import (
	"strings"
	"testing"
)

// FuzzDiffHeaders feeds arbitrary bytes to the diff header parser that
// guards every drafted patch (Phase 7): a patch is model output. It must
// never panic, and a diff it accepts never creates or changes a symlink and
// only states modes git prints.
func FuzzDiffHeaders(f *testing.F) {
	for _, s := range []string{
		"diff --git a/x.go b/x.go\nindex 1111111..2222222 100644\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n",
		"diff --git a/n b/n\nnew file mode 100755\nindex 0000000..1111111\n--- /dev/null\n+++ b/n\n@@ -0,0 +1 @@\n+x\n",
		"diff --git a/l b/l\nnew file mode 120000\nindex 0000000..1111111\n",
		"diff --git \"a/q\\\"q.txt\" \"b/q\\\"q.txt\"\nindex 1111111..2222222 100644\n",
		"diff --git a/a b/b\nrename from a\nrename to b\n",
		"diff --git a/x b/x\nold mode 100644\nnew mode 100755\n@@ -1 +1 @@\nnew mode 120000\n",
		"--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n",
		"diff --git a/x b/x\n+++ b/x\nnew mode 120000\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, diff []byte) {
		modes, renames, err := diffHeaders(diff)
		if err != nil || renames {
			return
		}
		if len(modes) == 0 {
			t.Fatal("an accepted diff states no path")
		}
		for path, m := range modes {
			if path == "" {
				t.Fatalf("empty path in %q", diff)
			}
			if m.new == symlinkMode {
				t.Fatalf("%s becomes a symlink in an accepted diff: %q", path, diff)
			}
			for _, mode := range []string{m.old, m.new} {
				if mode != "000000" && !gitModes[mode] {
					t.Fatalf("%s has mode %q, which git never prints", path, mode)
				}
			}
		}
	})
}

// FuzzUnquoteC checks git's C-quoting reader: a name without quotes,
// backslashes or control bytes reads back as itself, and nothing panics.
func FuzzUnquoteC(f *testing.F) {
	for _, s := range []string{`"a/b c"`, `"\303\251"`, `"\"x\""`, `"\777"`, `"unterminated`, `"a\`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _, _ = unquoteC(s)
		if strings.ContainsAny(s, "\"\\") || strings.IndexFunc(s, func(r rune) bool { return r < 0x20 }) >= 0 {
			return
		}
		name, rest, ok := unquoteC(`"` + s + `" tail`)
		if !ok || name != s || rest != " tail" {
			t.Fatalf("unquoteC(%q) = %q, %q, %v", s, name, rest, ok)
		}
	})
}

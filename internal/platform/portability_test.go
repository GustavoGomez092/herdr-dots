package platform

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unixOnly maps a source pattern that only works on Unix (or hides an OS
// branch) to what portable code uses instead.
var unixOnly = map[string]string{
	`"/dev/null"`:                    "platform.DevNull",
	"syscall.Kill(":                  "platform.StartTree / Tree.Kill",
	"Setpgid":                        "platform.StartTree",
	"Setsid":                         "platform.StartDetached",
	"syscall.Flock":                  "internal/lockfile",
	"syscall.O_NOFOLLOW":             "platform.OpenNoFollow",
	"syscall.Stat_t":                 "platform.CheckPrivate",
	"syscall.ECONNREFUSED":           "platform.ConnRefused",
	`"unix"`:                         "ipc.Listen / ipc.Dial (build-tagged)",
	"os.Getuid()":                    "platform.CheckPrivate",
	"exec.LookPath(":                 "platform.LookPath (prefers .exe over .cmd shims)",
	"runtime.GOOS":                   "a _unix.go / _windows.go file or internal/platform",
	".EngineSock()":                  "Paths.EngineEndpoint",
	`"/tmp`:                          "os.TempDir()",
	`".claude"`:                      "platform.ClaudeProjectsDir / ClaudeUserConfig",
	`".claude.json"`:                 "platform.ClaudeUserConfig",
	`".codex"`:                       "platform.CodexSessionsDir / CodexConfig",
	`Command("claude"`:               "platform.Command (safe for a claude.cmd shim)",
	`Command("codex"`:                "platform.Command (safe for a codex.cmd shim)",
	`(ctx, "claude"`:                 "platform.Command (safe for a claude.cmd shim)",
	`(ctx, "codex"`:                  "platform.Command (safe for a codex.cmd shim)",
	`exec.Command("sh"`:              "platform.EditorCommand (no sh on Windows)",
	`exec.CommandContext(ctx, "git"`: "platform.CommandContext (a repo-local git.exe in the cwd is ErrDot on Windows)",
	`exec.Command("git"`:             "platform.Command (a repo-local git.exe in the cwd is ErrDot on Windows)",
	`"HOME"`:                         "platform.HomeEnv or os.UserHomeDir (Windows reads USERPROFILE)",
}

// moduleRoot walks up from the test's directory to go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// osSpecific reports whether a file only builds on some OSes: a _unix.go or
// _windows.go name, or a //go:build line.
func osSpecific(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_unix.go") || strings.HasSuffix(base, "_windows.go") {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "//go:build") {
			return true
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
	}
	return false
}

// TestProductCodeHasNoUnixOnlyCalls keeps every later phase portable: shared
// (untagged) product code may not use Unix-only calls or branch on the OS.
// internal/platform, OS-specific files and tests are exempt.
func TestProductCodeHasNoUnixOnlyCalls(t *testing.T) {
	root := moduleRoot(t)
	var bad []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && rel != "." || rel == "internal/platform" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || osSpecific(p) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			code, _, _ := strings.Cut(line, "//")
			for pat, use := range unixOnly {
				if strings.Contains(code, pat) {
					bad = append(bad, rel+":"+itoa(i+1)+": "+pat+" (use "+use+")")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Error(b)
	}
}

// execAllowlist names (file:function) the only product code outside
// internal/platform that may build an exec.Cmd directly instead of through
// platform.Command / platform.CommandContext, with the reason.
var execAllowlist = map[string]string{
	"cmd/herdr-dots/engine.go:ensureEngine": "starts this binary (os.Executable: an absolute path, never a shim or a PATH search) through platform.StartDetached",
}

// childGuard checks one product file's child processes: exec.Command,
// exec.CommandContext and exec.Cmd literals must be allowlisted (file:func),
// every .Env assignment must be platform.ChildEnv(...), and every function
// (or closure) that builds a child must give it a platform.ChildEnv
// environment, so a child never searches its working directory (the pane's
// project) for a program on Windows.
func childGuard(t *testing.T, rel, src string, allow map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	var out []string
	report := func(pos token.Pos, fn, msg string) {
		out = append(out, rel+":"+itoa(fset.Position(pos).Line)+": "+fn+": "+msg)
	}
	isSel := func(e ast.Expr, pkg string, names ...string) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && id.Name == pkg && slices.Contains(names, s.Sel.Name)
	}
	isChildEnv := func(e ast.Expr) bool {
		c, ok := e.(*ast.CallExpr)
		return ok && isSel(c.Fun, "platform", "ChildEnv")
	}
	var check func(body ast.Node, fn string)
	check = func(body ast.Node, fn string) {
		var starts []token.Pos
		hasEnv := false
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				if n != body {
					check(n.Body, fn)
					return false
				}
			case *ast.CallExpr:
				if isSel(n.Fun, "exec", "Command", "CommandContext") {
					if _, ok := allow[rel+":"+fn]; !ok {
						report(n.Pos(), fn, "exec."+n.Fun.(*ast.SelectorExpr).Sel.Name+" is not on the allowlist (use platform.Command / CommandContext)")
					}
					starts = append(starts, n.Pos())
				}
				if isSel(n.Fun, "platform", "Command", "CommandContext") {
					starts = append(starts, n.Pos())
				}
			case *ast.CompositeLit:
				if isSel(n.Type, "exec", "Cmd") {
					if _, ok := allow[rel+":"+fn]; !ok {
						report(n.Pos(), fn, "exec.Cmd is not on the allowlist (use platform.Command / CommandContext)")
					}
					starts = append(starts, n.Pos())
				}
			case *ast.AssignStmt:
				for i, l := range n.Lhs {
					if s, ok := l.(*ast.SelectorExpr); !ok || s.Sel.Name != "Env" {
						continue
					}
					if len(n.Rhs) == len(n.Lhs) && isChildEnv(n.Rhs[i]) {
						hasEnv = true
						continue
					}
					report(n.Pos(), fn, ".Env = without platform.ChildEnv")
				}
			}
			return true
		})
		if len(starts) > 0 && !hasEnv {
			report(starts[0], fn, "starts a child without platform.ChildEnv (set cmd.Env = platform.ChildEnv(...))")
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body != nil {
				check(d.Body, d.Name.Name)
			}
		case *ast.GenDecl:
			ast.Inspect(d, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					check(lit.Body, "<package var>")
					return false
				}
				return true
			})
		}
	}
	return out
}

// TestProductChildrenGoThroughPlatform runs childGuard over every product
// file outside internal/platform, OS-specific ones included (tests and
// testdata are not product code).
func TestProductChildrenGoThroughPlatform(t *testing.T) {
	root := moduleRoot(t)
	used := map[string]bool{}
	var bad []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && rel != "." || rel == "internal/platform" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Contains(data, []byte("exec")) && !bytes.Contains(data, []byte(".Env")) && !bytes.Contains(data, []byte("platform.Command")) {
			return nil // cheap: nothing to check
		}
		for k := range execAllowlist {
			if strings.HasPrefix(k, rel+":") && bytes.Contains(data, []byte("func "+strings.TrimPrefix(k, rel+":")+"(")) {
				used[k] = true
			}
		}
		bad = append(bad, childGuard(t, rel, string(data), execAllowlist)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Error(b)
	}
	for k := range execAllowlist {
		if !used[k] {
			t.Errorf("allowlist entry %s matches no function: remove it", k)
		}
	}
}

// The child-process guard on small sources: what it flags and what it lets
// through.
func TestChildGuardRules(t *testing.T) {
	allow := map[string]string{"x.go:allowed": "test entry"}
	for _, c := range []struct {
		name, src string
		want      []string // substrings, one per expected finding
	}{
		{"platform command with ChildEnv", `package p
func ok() { cmd, _ := platform.Command("git", nil); cmd.Env = platform.ChildEnv(os.Environ()); _ = cmd }`, nil},
		{"platform command without Env", `package p
func noEnv() { cmd, _ := platform.CommandContext(ctx, "git", nil); cmd.Run() }`, []string{"noEnv: starts a child without platform.ChildEnv"}},
		{"Env set without ChildEnv", `package p
func rawEnv() { cmd, _ := platform.Command("git", nil); cmd.Env = os.Environ(); cmd.Run() }`,
			[]string{"rawEnv: .Env = without platform.ChildEnv", "rawEnv: starts a child without platform.ChildEnv"}},
		{"exec.Command not allowlisted", `package p
func direct() { cmd := exec.Command("x"); cmd.Env = platform.ChildEnv(nil) }`, []string{"direct: exec.Command is not on the allowlist"}},
		{"exec.CommandContext allowlisted", `package p
func allowed() { cmd := exec.CommandContext(ctx, "x"); cmd.Env = platform.ChildEnv(nil) }`, nil},
		{"exec.Cmd literal", `package p
func lit() { cmd := &exec.Cmd{Path: "x"}; cmd.Env = platform.ChildEnv(nil) }`, []string{"lit: exec.Cmd is not on the allowlist"}},
		{"closure is its own scope", `package p
func outer() { cmd, _ := platform.Command("a", nil); cmd.Env = platform.ChildEnv(nil); f := func() { c, _ := platform.Command("b", nil); c.Run() }; f() }`,
			[]string{"outer: starts a child without platform.ChildEnv"}},
		{"multi-assign Env", `package p
func multi() { cmd, _ := platform.Command("a", nil); cmd.Dir, cmd.Env = "", os.Environ() }`,
			[]string{"multi: .Env = without platform.ChildEnv", "multi: starts a child without platform.ChildEnv"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := childGuard(t, "x.go", c.src, allow)
			if len(got) != len(c.want) {
				t.Fatalf("findings %q, want %d: %q", got, len(c.want), c.want)
			}
			for i, w := range c.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("finding %q, want it to contain %q", got[i], w)
				}
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

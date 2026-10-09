// Package toolloop gives API dots read-only access to the pane's working
// directory through three tools (read_file, grep, list_files) and drives the
// provider-neutral tool-call loop. Every path is resolved inside the root
// (symlinks included) and checked against a deny-list of secret files.
package toolloop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bmatcuk/doublestar/v4"
)

var (
	ErrOutsideRoot = errors.New("path outside project")
	ErrDenied      = errors.New("path denied: it may hold secrets")
)

// DefaultDeny lists secret files no tool may read or list. Config
// tool_deny_globs can add to it but never remove from it.
var DefaultDeny = []string{
	".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_ed25519*", "*.p12",
	".git/**", "**/secrets/**", ".netrc", "*.kdbx",
	"credentials", "credentials.toml", "credentials.json", "*.pfx", "*.jks", "*.keystore",
	".npmrc", ".pypirc", ".pgpass", ".htpasswd",
	"id_dsa*", "**/.ssh/**", "**/.gnupg/**", "**/.aws/**", "**/.config/gh/**", "**/.kube/config",
	"**/.docker/config.json", ".git-credentials", "*.tfstate", "*.tfstate.*", "*_history", "*.ppk", "**/.netrc",
}

// Denied reports whether rel (slash-separated, relative to the root) matches a
// deny glob, at the root or in any subdirectory. Unmatchable globs are skipped.
// Matching ignores case (macOS volumes are case-insensitive, so ".ENV" opens
// ".env"); over-denying is the safe side.
func Denied(rel string, globs []string) bool {
	rel = strings.ToLower(strings.TrimPrefix(filepath.ToSlash(rel), "./"))
	for _, g := range globs {
		g = strings.ToLower(g)
		if ok, _ := doublestar.Match(g, rel); ok {
			return true
		}
		if !strings.HasPrefix(g, "**/") {
			if ok, _ := doublestar.Match("**/"+g, rel); ok {
				return true
			}
		}
		// A denied directory denies itself too (list_files on ".git").
		if strings.HasSuffix(g, "/**") {
			dir := strings.TrimSuffix(g, "/**")
			if ok, _ := doublestar.Match(dir, rel); ok {
				return true
			}
			if ok, _ := doublestar.Match("**/"+strings.TrimPrefix(dir, "**/"), rel); ok {
				return true
			}
		}
	}
	return false
}

func within(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// ResolveInRoot maps a tool path to a real file under root. p may be relative
// to root or absolute; either way the lexical path and the path after
// resolving every symlink must stay under the real root. rel is the real
// path relative to the root. Both the requested and the real path are checked
// against deny, so a symlink can't launder a secret file's name.
func ResolveInRoot(root, p string, deny []string) (abs, rel string, err error) {
	if strings.ContainsRune(p, 0) {
		return "", "", errors.New("invalid path")
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", errors.New("path is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	rr, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", "", fmt.Errorf("project root: %w", err)
	}
	joined := filepath.Join(rr, p)
	if filepath.IsAbs(p) {
		joined = filepath.Clean(p)
		// The pane may report the root through a symlink (/var vs /private/var).
		if r, ok := within(absRoot, joined); ok {
			joined = filepath.Join(rr, r)
		}
	}
	lexRel, ok := within(rr, joined)
	if !ok {
		return "", "", ErrOutsideRoot
	}
	if Denied(lexRel, deny) {
		return "", "", ErrDenied
	}
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		// A symlink leaving the root must read the same whether or not its
		// target exists: no existence oracle outside the project.
		if werr := walkInside(rr, lexRel); werr != nil {
			return "", "", werr
		}
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("no such file: %s", lexRel)
		}
		return "", "", fmt.Errorf("cannot resolve %s", lexRel)
	}
	rel, ok = within(rr, real)
	if !ok {
		return "", "", ErrOutsideRoot
	}
	if Denied(rel, deny) {
		return "", "", ErrDenied
	}
	return real, rel, nil
}

// openVerified opens abs (a path ResolveInRoot returned) for reading and closes
// the check-to-open race: O_NOFOLLOW stops a symlink swapped in for the last
// component, and after the open the path must still be its own fully resolved
// form (so no parent was swapped for a symlink) and name the very file opened.
// O_NONBLOCK keeps a FIFO swapped in from hanging the run; callers must still
// require a regular file or directory.
func openVerified(abs string) (*os.File, error) {
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil || real != abs {
		f.Close()
		return nil, ErrOutsideRoot
	}
	fst, err1 := f.Stat()
	pst, err2 := os.Lstat(abs)
	if err1 != nil || err2 != nil || !os.SameFile(fst, pst) {
		f.Close()
		return nil, ErrOutsideRoot
	}
	return f, nil
}

// walkInside resolves rel under the real root one component at a time, without
// touching anything outside it. It returns ErrOutsideRoot when a symlink on the
// way points out of the root, and nil when every step stays inside (the caller
// then reports its own error).
func walkInside(rr, rel string) error {
	queue := strings.Split(rel, "/")
	cur := rr
	for links := 0; len(queue) > 0; {
		part := queue[0]
		queue = queue[1:]
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		if _, ok := within(rr, next); !ok {
			return ErrOutsideRoot
		}
		fi, err := os.Lstat(next)
		if err != nil {
			return nil
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			cur = next
			continue
		}
		if links++; links > 40 {
			return nil
		}
		target, err := os.Readlink(next)
		if err != nil {
			return nil
		}
		var tgt string
		if filepath.IsAbs(target) {
			tgt = filepath.Clean(target)
		} else {
			tgt = filepath.Join(cur, target)
		}
		r, ok := within(rr, tgt)
		if !ok {
			return ErrOutsideRoot
		}
		queue = append(strings.Split(r, "/"), queue...)
		cur = rr
	}
	return nil
}

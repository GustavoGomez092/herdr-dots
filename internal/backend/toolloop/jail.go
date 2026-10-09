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

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/text/cases"

	"herdr-dots/internal/platform"
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
// Matching uses full Unicode case folding on both sides (APFS folds ſ to s and
// ß to ss, so ".ENV" and "id_rſa" open the real files); over-denying is the
// safe side. ResolveInRoot adds a second layer: exact on-disk spellings only.
func Denied(rel string, globs []string) bool {
	rel = fold(strings.TrimPrefix(filepath.ToSlash(rel), "./"))
	for _, g := range globs {
		g = fold(g)
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

// fold applies Unicode case folding. A fresh folder per call: they are stateful.
func fold(s string) string { return cases.Fold().String(s) }

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
	return newResolver().resolve(root, p, deny)
}

// resolver memoizes directory listings and the home lookup across the many
// resolutions one grep or listing makes.
type resolver struct {
	dirs     map[string]map[string]struct{}
	home     string
	homeInfo os.FileInfo
	under    map[string]homeHit
}

type homeHit struct {
	rel string
	ok  bool
}

func newResolver() *resolver {
	r := &resolver{dirs: map[string]map[string]struct{}{}, under: map[string]homeHit{}}
	if h := homeDir(); h != "" {
		if real, err := filepath.EvalSymlinks(h); err == nil {
			h = real
		}
		if fi, err := os.Stat(h); err == nil {
			r.home, r.homeInfo = h, fi
		}
	}
	return r
}

// exact reports whether every component of rel appears byte for byte in its
// parent's directory listing. Case-insensitive and normalizing filesystems
// otherwise open ".ENV" or "id_rſa" for ".env" / "id_rsa", and the deny check
// must run against the real name. Callers have already established that rel
// stays inside the root.
func (r *resolver) exact(rr, rel string) bool {
	cur := rr
	for _, comp := range strings.Split(rel, "/") {
		if comp == "" || comp == "." {
			continue
		}
		names, ok := r.dirs[cur]
		if !ok {
			ents, err := os.ReadDir(cur)
			if err != nil {
				return false
			}
			names = make(map[string]struct{}, len(ents))
			for _, e := range ents {
				names[e.Name()] = struct{}{}
			}
			r.dirs[cur] = names
		}
		if _, ok := names[comp]; !ok {
			return false
		}
		cur = filepath.Join(cur, comp)
	}
	return true
}

// homeRel returns dir's path relative to the user's home directory when dir is
// the home directory or below it (compared by file identity, so case variants
// and firmlink paths count).
func (r *resolver) homeRel(dir string) (string, bool) {
	if r.homeInfo == nil {
		return "", false
	}
	if h, ok := r.under[dir]; ok {
		return h.rel, h.ok
	}
	var hit homeHit
	if fi, err := os.Stat(dir); err == nil && os.SameFile(fi, r.homeInfo) {
		hit = homeHit{".", true}
	} else if parent := filepath.Dir(dir); parent != dir {
		if pr, ok := r.homeRel(parent); ok {
			hit = homeHit{filepath.ToSlash(filepath.Join(pr, filepath.Base(dir))), true}
		}
	}
	r.under[dir] = hit
	return hit.rel, hit.ok
}

func (r *resolver) resolve(root, p string, deny []string) (abs, rel string, err error) {
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
	// Windows: an alternate data stream (".env::$DATA"), a device name (NUL)
	// or a trailing dot would reach a file the deny-list can't see.
	if platform.ReservedPathName(lexRel) || Denied(lexRel, deny) {
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
	// Everything now stays inside the root, so listing directories is safe.
	// The requested spelling and the resolved one must both be exact.
	if !r.exact(rr, lexRel) || !r.exact(rr, rel) {
		return "", "", fmt.Errorf("no such file: %s", lexRel)
	}
	if Denied(rel, deny) {
		return "", "", ErrDenied
	}
	// Home-anchored globs (.config/gh/**, .kube/config) also apply when the
	// root sits below the home directory.
	if hr, ok := r.homeRel(filepath.Dir(real)); ok {
		if Denied(filepath.ToSlash(filepath.Join(hr, filepath.Base(real))), deny) {
			return "", "", ErrDenied
		}
	}
	return real, rel, nil
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

// openVerified opens abs (a path ResolveInRoot returned) for reading and closes
// the check-to-open race: platform.OpenNoFollow stops a symlink swapped in for
// the last component (O_NOFOLLOW on Unix; on Windows an Lstat that refuses
// symlinks and reparse points), and after the open the path must still be its
// own fully resolved form (so no parent was swapped for a symlink) and name
// the very file opened. On Unix O_NONBLOCK keeps a FIFO swapped in from
// hanging the run; callers must still require a regular file or directory.
func openVerified(abs string) (*os.File, error) {
	f, err := platform.OpenNoFollow(abs)
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

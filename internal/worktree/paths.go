package worktree

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"herdr-dots/internal/platform"
)

// Escapes reports whether a symlink at the slash-separated path rel with the
// given target points outside the repository or into its .git directory.
func Escapes(rel, target string) bool {
	t := filepath.ToSlash(target)
	if t == "" || filepath.IsAbs(target) || strings.HasPrefix(t, "/") || (len(t) >= 2 && t[1] == ':') {
		return true
	}
	joined := path.Clean(path.Join(path.Dir(rel), t))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return true
	}
	first, _, _ := strings.Cut(joined, "/")
	return strings.EqualFold(first, ".git")
}

// CheckPath validates one slash-separated, repository-relative patch path.
func CheckPath(rel string) error {
	if rel == "" || strings.ContainsRune(rel, '\\') || strings.IndexFunc(rel, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: unsafe path %q", ErrEscape, rel)
	}
	if strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) || (len(rel) >= 2 && rel[1] == ':') {
		return fmt.Errorf("%w: absolute path %q", ErrEscape, rel)
	}
	for _, el := range strings.Split(rel, "/") {
		if el == "" || el == "." || el == ".." {
			return fmt.Errorf("%w: path %q", ErrEscape, rel)
		}
		// .git, .GIT, ".git." / ".git " (NTFS) and GIT~1 (8.3 short name).
		if t := strings.TrimRight(el, ". "); strings.EqualFold(t, ".git") || strings.EqualFold(el, "git~1") {
			return fmt.Errorf("%w: %s", ErrGitDir, rel)
		}
	}
	if platform.ReservedPathName(rel) {
		return fmt.Errorf("%w: %q is a reserved name on Windows", ErrEscape, rel)
	}
	return nil
}

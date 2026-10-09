package worktree

import (
	"fmt"
	"os"
	"path/filepath"
)

// Caps bound a patch. Zero means no cap.
type Caps struct {
	MaxBytes int
	MaxFiles int
}

// Validate rejects a patch before anyone sees it: an empty patch
// (ErrEmptyPatch), one over the caps (ErrTooLarge), a path that is absolute,
// has a ".." or "." element, a backslash, a control character or a name
// Windows reserves (ErrEscape), any ".git" path element in any spelling git
// itself guards against (ErrGitDir), a submodule change (ErrEscape), or a
// symlink whose target, read from the worktree, points outside the
// repository (ErrEscape). Binary files and mode changes pass; Capture flags
// them for the card.
func Validate(p Patch, worktreeDir string, caps Caps) error {
	if len(p.Changes) == 0 || len(p.Diff) == 0 {
		return ErrEmptyPatch
	}
	if caps.MaxFiles > 0 && p.Files > caps.MaxFiles {
		return fmt.Errorf("%w: %d files (max %d)", ErrTooLarge, p.Files, caps.MaxFiles)
	}
	if caps.MaxBytes > 0 && p.Bytes > caps.MaxBytes {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrTooLarge, p.Bytes, caps.MaxBytes)
	}
	for _, c := range p.Changes {
		if err := CheckPath(c.Path); err != nil {
			return err
		}
		if c.OldMode == "160000" || c.NewMode == "160000" {
			return fmt.Errorf("%w: %s is a submodule", ErrEscape, c.Path)
		}
		if c.NewMode != "120000" {
			continue
		}
		target, err := os.Readlink(filepath.Join(worktreeDir, filepath.FromSlash(c.Path)))
		if err != nil {
			return fmt.Errorf("%w: can't read symlink %s: %v", ErrEscape, c.Path, err)
		}
		if Escapes(c.Path, target) {
			return fmt.Errorf("%w: symlink %s points to %s", ErrEscape, c.Path, target)
		}
	}
	return nil
}

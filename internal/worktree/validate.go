package worktree

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Caps bound a patch. Zero means no cap.
type Caps struct {
	MaxBytes int
	MaxFiles int
	// Deny reports whether a slash-separated, repository-relative path is one
	// the dot may not write (secrets: .env, id_rsa, .npmrc ...). The caller
	// passes the jail's own check, e.g. func(rel) bool { return
	// toolloop.Denied(rel, deny) } with toolloop.DefaultDeny plus the dot's
	// tool_deny_globs, so the case folding matches the jail's. Required: nil makes
	// Validate fail closed with ErrNoDenyCheck; pass a func returning false to
	// deny nothing.
	Deny func(rel string) bool
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

// Validate rejects a patch before anyone sees it: an empty patch
// (ErrEmptyPatch), one over the caps (ErrTooLarge), a path that is absolute,
// has a ".." or "." element, a backslash, a control character or a name
// Windows reserves (ErrEscape), any ".git" path element in any spelling git
// itself guards against (ErrGitDir), a path on the deny-list (ErrDenied), a
// submodule change (ErrEscape), a symlink whose target, read from the
// patch's own blob, points outside the repository (ErrEscape), or a diff
// whose header paths (as `git apply --numstat -z` reads them) are not
// exactly the paths in p.Changes (ErrEscape). Binary files, new executables
// and mode changes pass; Capture flags them for the card.
//
// g runs only `git apply --numstat` on a private copy of p.Diff; co is the
// draft the patch came from.
func Validate(ctx context.Context, g Git, co Checkout, p Patch, caps Caps) error {
	if len(p.Changes) == 0 || len(p.Diff) == 0 {
		return ErrEmptyPatch
	}
	if caps.Deny == nil {
		return ErrNoDenyCheck // fail closed: a wiring mistake must not let .env through
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
		if caps.Deny(c.Path) {
			return fmt.Errorf("%w: %s", ErrDenied, c.Path)
		}
		if c.OldMode == "160000" || c.NewMode == "160000" {
			return fmt.Errorf("%w: %s is a submodule", ErrEscape, c.Path)
		}
		if c.NewMode != "120000" {
			continue
		}
		if !shaRe.MatchString(c.NewSHA) {
			return fmt.Errorf("%w: symlink %s has no blob", ErrEscape, c.Path)
		}
		out, err := co.GitOutput(ctx, "cat-file", "blob", c.NewSHA)
		if err != nil {
			return fmt.Errorf("%w: can't read symlink %s: %v", ErrEscape, c.Path, err)
		}
		if target := string(out); Escapes(c.Path, target) {
			return fmt.Errorf("%w: symlink %s points to %s", ErrEscape, c.Path, target)
		}
	}
	// What the diff bytes themselves say (modes, renames) must be what
	// Changes say: Apply sees only the bytes.
	if err := checkDiffAgainstChanges(p.Diff, p.Changes); err != nil {
		return err
	}
	dir, file, err := tempPatch(p.Diff)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	got, err := patchPaths(ctx, g, dir, file)
	if err != nil {
		return fmt.Errorf("invalid patch: %w", err)
	}
	if err := checkSummary(ctx, g, dir, file, p.Changes); err != nil {
		return err
	}
	want := p.Paths()
	slices.Sort(got)
	slices.Sort(want)
	want = slices.Compact(want)
	if !slices.Equal(got, want) {
		return fmt.Errorf("%w: the diff headers name [%s], the patch lists [%s]", ErrEscape, strings.Join(got, " "), strings.Join(want, " "))
	}
	return nil
}

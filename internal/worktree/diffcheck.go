package worktree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// fileModes is what a diff says about one path: the mode before and after.
type fileModes struct{ old, new string }

// symlinkMode is a symlink's git mode. Drafts can't create or change one.
const symlinkMode = "120000"

// errSymlink refuses a diff block that would leave a symlink at path: a new
// symlink, a file turned into one, or a retarget. A lexical target check
// can't see chained links (up -> ., l -> up/../x) or links through a
// committed symlink, so a draft makes none at all.
func errSymlink(path string) error {
	return fmt.Errorf("%w: %s: drafts can't create or change symlinks; make it by hand", ErrEscape, path)
}

// gitModes are the only modes `git diff` prints. `git apply` canonicalizes
// any other spelling (120777 becomes a symlink, 777 a gitlink), so a diff
// that spells a mode differently is refused, never compared.
var gitModes = map[string]bool{"100644": true, "100755": true, symlinkMode: true, "160000": true}

// strayOps are header lines that never appear outside a header in a diff git
// wrote. One in a block's content (after a hunk, say) is refused.
var strayOps = []string{"old mode ", "new mode ", "deleted file mode ", "new file mode ", "index ",
	"rename ", "copy ", "similarity index", "dissimilarity index"}

// diffHeaders reads the extended headers of a `git diff` and returns each
// path's modes as the diff itself states them, plus whether any block is a
// rename or a copy. A path split into a delete and a create (a type change)
// merges: old from the first block, new from the last.
//
// A block's header runs, as `git apply` reads it, from "diff --git " through
// the "--- " and "+++ " lines and any mode or index lines after them, up to
// the first other line ("@@ -", "GIT binary patch", ...): a mode line placed
// after "+++ " still counts. The content after the header is not parsed, but
// a mode, index, rename or copy line there is refused.
//
// Any block whose new mode is a symlink's (120000) is refused (errSymlink),
// whether a "new file mode", a "new mode" or the index line says so;
// deleting a symlink or replacing it with a regular file passes. Header
// paths are unquoted the way git quotes them ("a/q\"q.txt", octal escapes).
// It fails closed on anything it can't read: a malformed or mixed-quoted
// header, a mode git never prints, a block with no mode or half a mode
// change.
func diffHeaders(diff []byte) (modes map[string]fileModes, renames bool, err error) {
	modes = map[string]fileModes{}
	var (
		path             string
		old, new, idx    string
		inHeader         bool
		haveOld, haveNew bool
		blocks           int
	)
	// endHeader records the block's modes once its header is over.
	endHeader := func() error {
		if !inHeader {
			return nil
		}
		inHeader = false
		switch {
		case !haveOld && !haveNew:
			if idx == "" {
				return fmt.Errorf("%w: no mode for %s in the diff", ErrEscape, path)
			}
			old, new = idx, idx
		case idx != "":
			return fmt.Errorf("%w: %s has both an index mode and mode lines", ErrEscape, path)
		case haveOld != haveNew:
			return fmt.Errorf("%w: %s has half a mode change", ErrEscape, path)
		}
		if new == symlinkMode {
			return errSymlink(path)
		}
		if prev, ok := modes[path]; ok {
			old = prev.old
		}
		modes[path] = fileModes{old, new}
		return nil
	}
	readMode := func(line, prefix string) (string, error) {
		m := strings.TrimPrefix(line, prefix)
		if !gitModes[m] {
			return "", fmt.Errorf("%w: unexpected mode in %q", ErrEscape, line)
		}
		return m, nil
	}
	for _, line := range strings.Split(string(diff), "\n") {
		if rest, ok := strings.CutPrefix(line, "diff --git "); ok {
			if err := endHeader(); err != nil {
				return nil, false, err
			}
			p, differ, err := headerPath(rest)
			if err != nil {
				return nil, false, err
			}
			if differ {
				return nil, true, nil // two different paths in one header: a rename or copy in disguise
			}
			path, inHeader, blocks = p, true, blocks+1
			old, new, idx, haveOld, haveNew = "", "", "", false, false
			continue
		}
		if !inHeader {
			// Before the first block or in a block's content: no header line.
			for _, op := range strayOps {
				if strings.HasPrefix(line, op) {
					return nil, false, fmt.Errorf("%w: header line %q outside a diff header", ErrEscape, line)
				}
			}
			continue
		}
		var m string
		switch {
		case strings.HasPrefix(line, "rename "), strings.HasPrefix(line, "copy "),
			strings.HasPrefix(line, "similarity index"), strings.HasPrefix(line, "dissimilarity index"):
			return nil, true, nil
		case strings.HasPrefix(line, "new file mode "):
			if m, err = readMode(line, "new file mode "); err != nil {
				return nil, false, err
			}
			old, new, haveOld, haveNew = "000000", m, true, true
		case strings.HasPrefix(line, "deleted file mode "):
			if m, err = readMode(line, "deleted file mode "); err != nil {
				return nil, false, err
			}
			old, new, haveOld, haveNew = m, "000000", true, true
		case strings.HasPrefix(line, "old mode "):
			if m, err = readMode(line, "old mode "); err != nil {
				return nil, false, err
			}
			old, haveOld = m, true
		case strings.HasPrefix(line, "new mode "):
			if m, err = readMode(line, "new mode "); err != nil {
				return nil, false, err
			}
			new, haveNew = m, true
		case strings.HasPrefix(line, "index "):
			switch f := strings.Fields(line); len(f) {
			case 2:
			case 3:
				if idx, err = readMode(f[2], ""); err != nil {
					return nil, false, err
				}
			default:
				return nil, false, fmt.Errorf("%w: unreadable index line %q", ErrEscape, line)
			}
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			// Still header for git apply: mode lines may follow.
		default:
			// "@@ -", "GIT binary patch", "Binary files ...": content follows
			// until the next "diff --git ".
			if err := endHeader(); err != nil {
				return nil, false, err
			}
		}
		// A new symlink mode refuses on sight, whatever a later line says.
		if m == symlinkMode && (strings.HasPrefix(line, "new file mode ") || strings.HasPrefix(line, "new mode ")) {
			return nil, false, errSymlink(path)
		}
	}
	if err := endHeader(); err != nil {
		return nil, false, err
	}
	if blocks == 0 {
		return nil, false, fmt.Errorf("no diff in the patch")
	}
	return modes, false, nil
}

// headerPath reads the two names after "diff --git " and returns the path
// when they are a/P and b/P. differ reports a/P b/Q: a rename or copy in
// disguise. Names are either both C-quoted, the way git quotes them, or both
// bare; for bare names holding spaces the split is the symmetric one.
func headerPath(rest string) (path string, differ bool, err error) {
	bad := func() (string, bool, error) {
		return "", false, fmt.Errorf("%w: unreadable diff header %q", ErrEscape, "diff --git "+rest)
	}
	if !strings.HasPrefix(rest, `"`) {
		body, ok := strings.CutPrefix(rest, "a/")
		if !ok || len(body) < 3 || (len(body)-3)%2 != 0 {
			return bad()
		}
		n := (len(body) - 3) / 2
		if body != body[:n]+" b/"+body[:n] {
			if strings.Contains(rest, " b/") {
				return "", true, nil
			}
			return bad()
		}
		return body[:n], false, nil
	}
	a, tail, ok := unquoteC(rest)
	if !ok {
		return bad()
	}
	if tail, ok = strings.CutPrefix(tail, " "); !ok || !strings.HasPrefix(tail, `"`) {
		return bad() // mixed quoting: git quotes both names or neither
	}
	b, tail, ok := unquoteC(tail)
	if !ok || tail != "" {
		return bad()
	}
	a, aok := strings.CutPrefix(a, "a/")
	b, bok := strings.CutPrefix(b, "b/")
	if !aok || !bok || a == "" || b == "" {
		return bad()
	}
	if a != b {
		return "", true, nil
	}
	return a, false, nil
}

// unquoteC reads one C-quoted name at the start of s, as git's
// unquote_c_style does: \a \b \f \n \r \t \v \\ \" and three-digit octal
// \000–\377. It returns the name and what follows the closing quote.
func unquoteC(s string) (name, rest string, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	var out []byte
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return string(out), s[i+1:], true
		case '\\':
			i++
			if i >= len(s) {
				return "", "", false
			}
			if e := strings.IndexByte(`abfnrtv\"`, s[i]); e >= 0 {
				out = append(out, "\a\b\f\n\r\t\v\\\""[e])
				continue
			}
			if i+2 >= len(s) || s[i] < '0' || s[i] > '3' || s[i+1] < '0' || s[i+1] > '7' || s[i+2] < '0' || s[i+2] > '7' {
				return "", "", false
			}
			out = append(out, (s[i]-'0')<<6|(s[i+1]-'0')<<3|(s[i+2]-'0'))
			i += 2
		default:
			out = append(out, c)
		}
	}
	return "", "", false // no closing quote
}

// checkDiffAgainstChanges requires the diff's own statement of every path's
// modes to equal Changes exactly, and refuses renames and copies (whose
// hidden source path would escape the path and deny checks) and any symlink
// creation or change (diffHeaders). A gitlink the diff carries but Changes
// doesn't mention fails the comparison.
func checkDiffAgainstChanges(diff []byte, changes []Change) error {
	got, renames, err := diffHeaders(diff)
	if errors.Is(err, ErrEscape) {
		return err
	}
	if err != nil {
		return fmt.Errorf("invalid patch: %w", err)
	}
	if renames {
		return fmt.Errorf("%w: the diff holds a rename or copy", ErrEscape)
	}
	want := map[string]fileModes{}
	for _, c := range changes {
		want[c.Path] = fileModes{c.OldMode, c.NewMode}
	}
	if len(got) != len(want) {
		return fmt.Errorf("%w: the diff changes %d paths, the patch lists %d", ErrEscape, len(got), len(want))
	}
	for path, m := range got {
		if w, ok := want[path]; !ok || w != m {
			return fmt.Errorf("%w: %s is %s→%s in the diff, %s→%s in the patch", ErrEscape, path, m.old, m.new, w.old, w.new)
		}
	}
	return nil
}

// checkSummary runs `git apply --summary` and requires its create, delete
// and mode-change lines to be exactly what Changes imply, and no rename or
// copy line at all: git's own reading of the diff must agree with ours.
func checkSummary(ctx context.Context, g Git, dir, file string, changes []Change) error {
	out, err := g.Output(ctx, dir, ceiling(dir), "apply", "--summary", file)
	if err != nil {
		return fmt.Errorf("invalid patch: %w", err)
	}
	var want, got []string
	for _, c := range changes {
		switch c.Status {
		case "A":
			want = append(want, fmt.Sprintf(" create mode %s %s", c.NewMode, c.Path))
		case "D":
			want = append(want, fmt.Sprintf(" delete mode %s %s", c.OldMode, c.Path))
		case "T":
			want = append(want, fmt.Sprintf(" delete mode %s %s", c.OldMode, c.Path), fmt.Sprintf(" create mode %s %s", c.NewMode, c.Path))
		default:
			if c.OldMode != c.NewMode {
				want = append(want, fmt.Sprintf(" mode change %s => %s %s", c.OldMode, c.NewMode, c.Path))
			}
		}
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, " rename ") || strings.HasPrefix(line, " copy ") {
			return fmt.Errorf("%w: the diff holds a rename or copy", ErrEscape)
		}
		got = append(got, line)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		return fmt.Errorf("%w: git reads the diff's modes differently from the patch (%q vs %q)", ErrEscape, got, want)
	}
	return nil
}

// ceiling stops git from discovering a repository above dir (a TMPDIR inside
// some project): inside one, `git apply` skips paths outside its prefix.
func ceiling(dir string) []string {
	return []string{"GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir)}
}

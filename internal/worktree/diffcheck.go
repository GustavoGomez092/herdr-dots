package worktree

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// fileModes is what a diff says about one path: the mode before and after.
type fileModes struct{ old, new string }

// diffHeaders reads the extended headers of a `git diff` (no content lines)
// and returns each path's modes as the diff itself states them, plus whether
// any block is a rename or a copy. A path split into a delete and a create
// (a type change) merges: old from the first block, new from the last. It
// fails closed on anything it can't read (a quoted path, a header that isn't
// "a/P b/P").
func diffHeaders(diff []byte) (modes map[string]fileModes, renames bool, err error) {
	modes = map[string]fileModes{}
	var (
		path             string
		old, new, idx    string
		open             bool
		haveOld, haveNew bool
		finish           = func() error { return nil }
		blocks           int
	)
	finish = func() error {
		if !open {
			return nil
		}
		open = false
		if !haveOld && !haveNew {
			if idx == "" {
				return fmt.Errorf("%w: no mode for %s in the diff", ErrEscape, path)
			}
			old, new = idx, idx
		}
		if prev, ok := modes[path]; ok {
			old = prev.old
		}
		modes[path] = fileModes{old, new}
		return nil
	}
	for _, line := range strings.Split(string(diff), "\n") {
		if rest, ok := strings.CutPrefix(line, "diff --git "); ok {
			if err := finish(); err != nil {
				return nil, false, err
			}
			body, ok := strings.CutPrefix(rest, "a/")
			if !ok || len(body) < 3 || (len(body)-3)%2 != 0 {
				return nil, false, fmt.Errorf("%w: unreadable diff header %q", ErrEscape, line)
			}
			n := (len(body) - 3) / 2
			if body != body[:n]+" b/"+body[:n] {
				if strings.Contains(rest, " b/") {
					return nil, true, nil // two different paths in one header: a rename or copy in disguise
				}
				return nil, false, fmt.Errorf("%w: unreadable diff header %q", ErrEscape, line)
			}
			path, open, blocks = body[:n], true, blocks+1
			old, new, idx, haveOld, haveNew = "", "", "", false, false
			continue
		}
		if !open {
			continue
		}
		switch {
		case strings.HasPrefix(line, "rename "), strings.HasPrefix(line, "copy "),
			strings.HasPrefix(line, "similarity index"), strings.HasPrefix(line, "dissimilarity index"):
			return nil, true, nil
		case strings.HasPrefix(line, "new file mode "):
			old, new, haveOld, haveNew = "000000", strings.TrimPrefix(line, "new file mode "), true, true
		case strings.HasPrefix(line, "deleted file mode "):
			old, new, haveOld, haveNew = strings.TrimPrefix(line, "deleted file mode "), "000000", true, true
		case strings.HasPrefix(line, "old mode "):
			old, haveOld = strings.TrimPrefix(line, "old mode "), true
		case strings.HasPrefix(line, "new mode "):
			new, haveNew = strings.TrimPrefix(line, "new mode "), true
		case strings.HasPrefix(line, "index "):
			if f := strings.Fields(line); len(f) == 3 {
				idx = f[2]
			}
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "@@"),
			strings.HasPrefix(line, "GIT binary patch"), strings.HasPrefix(line, "Binary files"):
			// The header ends; content follows until the next "diff --git ".
			if err := finish(); err != nil {
				return nil, false, err
			}
		}
	}
	if err := finish(); err != nil {
		return nil, false, err
	}
	if blocks == 0 {
		return nil, false, fmt.Errorf("no diff in the patch")
	}
	return modes, false, nil
}

// checkDiffAgainstChanges requires the diff's own statement of every path's
// modes to equal Changes exactly, and refuses renames and copies (whose
// hidden source path would escape the path and deny checks). A symlink or
// gitlink the diff carries but Changes doesn't mention fails the comparison.
func checkDiffAgainstChanges(diff []byte, changes []Change) error {
	got, renames, err := diffHeaders(diff)
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

package worktree

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Patch flags shown on a patch card.
const (
	FlagBinary           = "binary"            // a binary file changed
	FlagMode             = "mode"              // a file mode changed (e.g. made executable)
	FlagExecutable       = "executable"        // a file became executable (new or chmod +x)
	FlagSymlink          = "symlink"           // a symlink was deleted or replaced by a file (Validate refuses creating or changing one)
	FlagSymlinkKept      = "symlink-kept"      // the dot edited an escaping symlink's stand-in; the edit was dropped
	FlagSubmoduleSkipped = "submodule-skipped" // the repo has submodules; they were not snapshotted
)

// Change is one file in a patch.
type Change struct {
	Path    string // slash-separated, relative to the repository root
	Status  string // A, M, D or T (type change)
	NewSHA  string // blob id of the new content (all zeros when deleted)
	OldMode string // "000000" when added
	NewMode string // "000000" when deleted
	Added   int    // lines; -1 for a binary file
	Deleted int
}

// Patch is what a draft changed, relative to its snapshot.
type Patch struct {
	Diff    []byte // `git diff --binary` output, ready for `git apply`
	Stat    string // "3 files +42 −7"
	Files   int
	Bytes   int
	Flags   []string
	Changes []Change
	// Truncated: CaptureMax cut Diff at its cap (Bytes is then cap+1).
	// Validate rejects such a patch as too large; it is never applied.
	Truncated bool
}

// Paths returns the changed paths in diff order.
func (p Patch) Paths() []string {
	out := make([]string, len(p.Changes))
	for i, c := range p.Changes {
		out[i] = c.Path
	}
	return out
}

// Capture stages everything the dot changed in the worktree's own index
// (never the user's) and returns the diff against the snapshot. Escaping
// symlinks are restored first, so they never show up as changed.
//
// Every git command goes through co.Git/co.GitOutput: the repository is named
// explicitly and every filter driver is off. No command here is `git status`
// or a worktree-vs-index diff (both would consult the model-written
// .gitattributes and .git file); every diff passes --no-ext-diff and
// --no-textconv.
//
// The Git parameter is unused (the draft's own runner, inside co, does the
// work) and kept so callers need not change.
func Capture(ctx context.Context, g Git, co Checkout, s Snapshot) (Patch, error) {
	return CaptureMax(ctx, g, co, s, 0)
}

// CaptureMax is Capture keeping at most maxBytes+1 bytes of the patch text
// (0 = no cap): git's output past that is read and dropped, never buffered.
// A longer patch comes back Truncated, for Validate to reject as too large.
func CaptureMax(ctx context.Context, _ Git, co Checkout, s Snapshot, maxBytes int) (Patch, error) {
	kept, err := restoreDefanged(co)
	if err != nil {
		return Patch{}, fmt.Errorf("restore symlinks: %w", err)
	}
	if _, err := co.GitOutput(ctx, "add", "-A", "--", "."); err != nil {
		return Patch{}, err
	}
	// The user's diff.* config must not shape the patch: a fixed a/ b/ prefix
	// (git apply -p1 reads the paths from the headers) and three lines of
	// context, whatever diff.noprefix, diff.mnemonicPrefix or diff.context say.
	//
	// The index is read once, by write-tree; the three diffs compare the
	// snapshot with that immutable tree, so a process that changes the draft's
	// index meanwhile cannot make them disagree.
	tree, err := co.Git(ctx, "write-tree")
	if err != nil {
		return Patch{}, err
	}
	if !shaRe.MatchString(tree) {
		return Patch{}, fmt.Errorf("unexpected git write-tree output %q", tree)
	}
	base := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color",
		"--src-prefix=a/", "--dst-prefix=b/", "-U3"}
	with := func(extra ...string) []string {
		return append(append(append([]string(nil), base...), extra...), s.Commit, tree, "--")
	}
	raw, err := co.GitOutput(ctx, with("--raw", "-z", "--no-abbrev")...)
	if err != nil {
		return Patch{}, err
	}
	num, err := co.GitOutput(ctx, with("--numstat", "-z")...)
	if err != nil {
		return Patch{}, err
	}
	capped := co
	if maxBytes > 0 {
		capped.git.MaxOutput = maxBytes + 1
	}
	diff, err := capped.GitOutput(ctx, with("--binary")...)
	if err != nil {
		return Patch{}, err
	}
	changes, err := parseRaw(raw)
	if err != nil {
		return Patch{}, err
	}
	counts := parseNumstat(num)
	p := Patch{Diff: diff, Files: len(changes), Bytes: len(diff), Truncated: maxBytes > 0 && len(diff) > maxBytes}
	add, del := 0, 0
	flags := map[string]bool{}
	for i := range changes {
		c := &changes[i]
		n := counts[c.Path]
		c.Added, c.Deleted = n[0], n[1]
		if c.Added < 0 {
			flags[FlagBinary] = true
		} else {
			add, del = add+c.Added, del+c.Deleted
		}
		if c.OldMode == symlinkMode || c.NewMode == symlinkMode {
			flags[FlagSymlink] = true
		}
		if c.Status == "M" && c.OldMode != c.NewMode {
			flags[FlagMode] = true
		}
		if c.NewMode == "100755" && c.OldMode != "100755" {
			flags[FlagExecutable] = true
		}
	}
	if len(kept) > 0 {
		flags[FlagSymlinkKept] = true
	}
	if s.Submodules {
		flags[FlagSubmoduleSkipped] = true
	}
	for f := range flags {
		p.Flags = append(p.Flags, f)
	}
	sort.Strings(p.Flags)
	p.Changes = changes
	p.Stat = Stat(len(changes), add, del)
	return p, nil
}

// Stat formats a diffstat: "1 file +3 −0", "12 files +42 −7".
func Stat(files, added, deleted int) string {
	noun := "files"
	if files == 1 {
		noun = "file"
	}
	return fmt.Sprintf("%d %s +%d −%d", files, noun, added, deleted)
}

// parseRaw reads `git diff --raw -z --no-renames`:
// ":<old mode> <new mode> <old sha> <new sha> <status>\0<path>\0" per file.
func parseRaw(out []byte) ([]Change, error) {
	fields := bytes.Split(out, []byte{0})
	var cs []Change
	for i := 0; i+1 < len(fields); i += 2 {
		head := string(fields[i])
		if head == "" {
			break
		}
		parts := strings.Fields(strings.TrimPrefix(head, ":"))
		if len(parts) != 5 {
			return nil, fmt.Errorf("unexpected git diff --raw record %q", head)
		}
		cs = append(cs, Change{Path: string(fields[i+1]), OldMode: parts[0], NewMode: parts[1], NewSHA: parts[3], Status: parts[4][:1]})
	}
	return cs, nil
}

// parseNumstat reads `git diff --numstat -z --no-renames`:
// "<added>\t<deleted>\t<path>\0" per file, "-" counts for binary files.
func parseNumstat(out []byte) map[string][2]int {
	m := map[string][2]int{}
	for _, rec := range bytes.Split(out, []byte{0}) {
		parts := strings.SplitN(string(rec), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		a, aerr := strconv.Atoi(parts[0])
		d, derr := strconv.Atoi(parts[1])
		if aerr != nil || derr != nil {
			a, d = -1, -1
		}
		m[parts[2]] = [2]int{a, d}
	}
	return m
}

package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/state"
	"herdr-dots/internal/view"
)

// Project dots (Phase 9) reach the engine ONLY through
// projectdots.Item.Runnable: Item.Dot and Item.Effective are previews, filled
// in for untrusted files too, and the engine reads nothing from them but
// plain fields for display (TestProjectDotsRunOnlyThroughRunnable).

var (
	// ErrUntrusted: a project dot whose exact file content isn't trusted.
	ErrUntrusted = errors.New("project dot is not trusted")
	// ErrProjectDot: project dots are changed in their repository, not here.
	ErrProjectDot = errors.New("project dots are defined by their repository")
)

// projectState is one repository's scanned project dots.
type projectState struct {
	pr        memory.Project
	scan      projectdots.Scan
	trustedAt map[string]string // slug -> where its trust was given, when that was another checkout ("~"-abbreviated)
}

// projectDotsOn: a trust store exists and config.toml allows project dots
// (project_dots_enabled, false on a malformed config).
func (e *Engine) projectDotsOn() bool {
	return e.d.Trust != nil && e.config().ProjectDotsEnabled
}

// projectDotGone reports whether d is a project dot that is no longer loaded
// as it was when its run was queued (its file changed, lost its trust, or
// project dots were turned off): such a run never starts. Background jobs
// (Phase 8), drafts and council members carry a copy of the dot, so runDot,
// runBackground and the draft run check this right before they call the
// backend.
//
// Trust is checked fresh, not only as of the last poll: the repository's
// files and the trust store are fingerprinted again (projectdots.Stamp) and
// rescanned when either changed. That I/O runs off every engine lock.
func (e *Engine) projectDotGone(d dots.Dot) bool {
	if d.Origin != dots.OriginProject {
		return false
	}
	if !e.projectDotsOn() {
		return true
	}
	pr, stamp, found := e.ownerOf(d.Slug)
	if !found {
		return true
	}
	if projectdots.Stamp(pr.Root, e.d.Trust) != stamp {
		e.scanRoot(pr)
		e.broadcast()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	cur, ok := e.dots[d.Slug]
	return !e.projOn || !ok || !reflect.DeepEqual(cur, d)
}

// ownerOf is the repository whose trusted copy of slug is loaded (the first
// by root, as in rebuildLocked) and the stamp of its last scan.
func (e *Engine) ownerOf(slug string) (memory.Project, string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, root := range slices.Sorted(maps.Keys(e.projects)) {
		ps := e.projects[root]
		if it, ok := ps.scan.Find(slug); ok && it.Trusted {
			return ps.pr, ps.scan.Stamp, true
		}
	}
	return memory.Project{}, "", false
}

// rebuildLocked recomputes e.dots and e.invalid from the user dots and the
// trusted project dots. Callers hold e.mu. The first repository (by root) to
// define a slug wins; another copy is reported as invalid. A project dot
// enters e.dots only as Item.Runnable returns it: a copy of the clamped dot,
// and only for trusted content.
func (e *Engine) rebuildLocked() {
	m := maps.Clone(e.userDots)
	if m == nil {
		m = map[string]dots.Dot{}
	}
	inv := slices.Clone(e.userBad)
	e.projOn = e.projectDotsOn()
	if e.projOn {
		owner := map[string]string{}
		for _, root := range slices.Sorted(maps.Keys(e.projects)) {
			ps := e.projects[root]
			label := projectLabel(ps)
			for _, le := range ps.scan.Invalid {
				le.File = label + ": " + le.File
				inv = append(inv, le)
			}
			for _, it := range ps.scan.Items {
				run := it.Runnable()
				if run == nil {
					continue // pending trust: listed, never loaded
				}
				slug := it.Dot.Slug
				if other, dup := owner[slug]; dup {
					inv = append(inv, dots.LoadError{File: label + ": " + it.Rel, Err: fmt.Errorf("the same project dot is loaded from %s; only that copy runs", shortHome(other))})
					continue
				}
				owner[slug] = root
				m[slug] = *run
			}
		}
	}
	e.dots, e.invalid = m, inv
}

func projectLabel(ps *projectState) string {
	if ps.pr.Name != "" {
		return ps.pr.Name
	}
	return filepath.Base(ps.pr.Root)
}

// resolveRoot finds a working directory's git repository. Project dots need
// a real repository identity (decision #11: the root commit), so path
// identities (no git, no commits, shallow clones) have none.
func (e *Engine) resolveRoot(cwd string) memory.Project {
	if cwd == "" || e.d.ResolveRoot == nil {
		return memory.Project{}
	}
	pr, err := e.d.ResolveRoot(cwd)
	if err != nil || pr.Root == "" || dots.ProjectTag(pr.ID) == "" || !strings.HasPrefix(pr.ID, "g-") {
		return memory.Project{}
	}
	return pr
}

// knowPane records the pane's repository and scans it the first time. It
// runs git and reads files: never under a lock. A pane dropped meanwhile
// (its generation moved) is not recorded: a reused id is scanned again.
func (e *Engine) knowPane(paneID, cwd string) memory.Project {
	e.mu.Lock()
	gen := e.gens[paneID]
	e.mu.Unlock()
	pr := e.resolveRoot(cwd)
	if pr.Root == "" {
		return pr
	}
	e.mu.Lock()
	if e.gens[paneID] == gen {
		e.paneRoots[paneID] = pr.Root
	}
	_, known := e.projects[pr.Root]
	e.mu.Unlock()
	if !known {
		e.scanRoot(pr)
	}
	return pr
}

// ScanPane makes the pane's repository's project dots visible in the library
// (the manage popup calls it through reload_dots before anything is attached).
func (e *Engine) ScanPane(ctx context.Context, paneID string) error {
	if !e.projectDotsOn() {
		return nil
	}
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return fmt.Errorf("pane %s: %w", paneID, err)
	}
	e.knowPane(paneID, p.WorkDir())
	e.broadcast()
	return nil
}

// scanRoot reads one repository's project dots (outside every lock), swaps
// them in, and pauses attached dots whose trusted file changed, vanished or
// lost its trust. It reports whether anything changed.
func (e *Engine) scanRoot(pr memory.Project) bool {
	s := projectdots.ScanProject(pr.Root, pr.ID, e.d.Trust)
	trustedAt := map[string]string{}
	for _, it := range s.Items {
		// Clones share a project id (decision #11), so trust may come from
		// another checkout: say where. The same file reached through a
		// symlink is this checkout (sameRepo stats both: off every lock).
		if it.Trusted && it.TrustedPath != "" && !sameRepo(it.TrustedPath, itemPath(pr.Root, it.Rel)) {
			trustedAt[it.Dot.Slug] = shortHome(it.TrustedPath)
		}
	}
	e.mu.Lock()
	old := e.projects[pr.Root]
	e.projects[pr.Root] = &projectState{pr: pr, scan: s, trustedAt: trustedAt}
	var revoked []dots.Dot
	if old != nil {
		for _, it := range old.scan.Items {
			was := it.Runnable()
			if was == nil {
				continue
			}
			if now, ok := s.Find(was.Slug); !ok || !now.Trusted || now.Sum != it.Sum {
				revoked = append(revoked, *was)
			}
		}
		if old.scan.Stamp != s.Stamp {
			// Phase 8: a changed or newly trusted schedule is planned afresh.
			for _, it := range append(slices.Clone(old.scan.Items), s.Items...) {
				delete(e.plan, it.Dot.Slug)
			}
		}
	}
	e.rebuildLocked()
	e.mu.Unlock()
	for _, d := range revoked {
		e.pauseRevoked(d)
	}
	return old == nil || old.scan.Stamp != s.Stamp
}

// pauseRevoked pauses a project dot on every pane it is attached to and says
// why once. d is the copy that ran until now (glyph and name for the toast).
func (e *Engine) pauseRevoked(d dots.Dot) {
	attached := false
	for _, a := range e.d.Attachments.List() {
		if a.Find(d.Slug) == nil {
			continue
		}
		attached = true
		_ = e.d.Attachments.Update(a.PaneID, func(x *state.Attachment) {
			if da := x.Find(d.Slug); da != nil {
				da.Paused = true
			}
		})
	}
	if attached {
		e.notify(d, fmt.Sprintf("project dot %s changed or lost its trust and is paused: review it in the library (T) to trust it again", dots.DisplaySlug(d.Slug)))
	}
}

// rescanProjects scans every known repository whose files or trust store
// changed (all of them with force). It also applies a project_dots_enabled
// change. It reports whether the library changed.
func (e *Engine) rescanProjects(force bool) bool {
	e.mu.Lock()
	var prs []memory.Project
	var stamps []string
	for _, ps := range e.projects {
		prs = append(prs, ps.pr)
		stamps = append(stamps, ps.scan.Stamp)
	}
	flipped := e.projOn != e.projectDotsOn()
	if flipped {
		e.rebuildLocked()
	}
	e.mu.Unlock()
	changed := flipped
	if !e.projectDotsOn() {
		return changed
	}
	for i, pr := range prs {
		if force || projectdots.Stamp(pr.Root, e.d.Trust) != stamps[i] {
			if e.scanRoot(pr) {
				changed = true
			}
		}
	}
	return changed
}

// findProjectItem returns the pane's repository and the scanned item for
// slug, reading the files fresh.
func (e *Engine) findProjectItem(ctx context.Context, paneID, slug string) (memory.Project, projectdots.Item, error) {
	if !e.projectDotsOn() {
		return memory.Project{}, projectdots.Item{}, errors.New("project dots are turned off (project_dots_enabled = false)")
	}
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return memory.Project{}, projectdots.Item{}, fmt.Errorf("pane %s: %w", paneID, err)
	}
	pr := e.knowPane(paneID, p.WorkDir())
	if pr.Root == "" {
		return pr, projectdots.Item{}, fmt.Errorf("pane %s is not in a git repository with commits", paneID)
	}
	it, ok := projectdots.ScanProject(pr.Root, pr.ID, e.d.Trust).Find(slug)
	if !ok {
		return pr, it, fmt.Errorf("no project dot %s in %s", dots.DisplaySlug(slug), pr.Name)
	}
	return pr, it, nil
}

// Trust trusts a project dot exactly as the developer reviewed it: sumHex is
// the hash of the bytes they saw; a file changed since is refused. Only
// capabilities the file asks for can be granted.
func (e *Engine) Trust(ctx context.Context, paneID, slug, sumHex string, caps []string) error {
	if err := dots.CheckCapabilities(caps); err != nil {
		return err
	}
	pr, it, err := e.findProjectItem(ctx, paneID, slug)
	if err != nil {
		return err
	}
	if projectdots.Hex(it.Sum) != sumHex {
		return fmt.Errorf("%s changed since you reviewed it: review it again", it.Rel)
	}
	var grant []string
	for _, c := range caps {
		if slices.Contains(it.Requested, c) {
			grant = append(grant, c)
		}
	}
	if err := e.d.Trust.Trust(pr.ID, it.Rel, itemPath(pr.Root, it.Rel), it.Sum, grant); err != nil {
		return err
	}
	e.scanRoot(pr)
	e.broadcast()
	return nil
}

// Untrust forgets a project dot's trust; attached copies are paused.
func (e *Engine) Untrust(ctx context.Context, paneID, slug string) error {
	pr, it, err := e.findProjectItem(ctx, paneID, slug)
	if err != nil {
		return err
	}
	if _, err := e.d.Trust.Revoke(pr.ID, it.Rel); err != nil {
		return err
	}
	e.scanRoot(pr)
	e.broadcast()
	return nil
}

// untrustedLocked reports whether slug is a scanned, untrusted project dot.
func (e *Engine) untrustedLocked(slug string) bool {
	if !e.projOn {
		return false
	}
	for _, ps := range e.projects {
		if it, ok := ps.scan.Find(slug); ok && !it.Trusted {
			return true
		}
	}
	return false
}

// itemPath is a project dot file's absolute path in this checkout.
func itemPath(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// projectLibraryLocked lists project dots for the snapshot: trusted and
// untrusted, one entry per slug, repositories in root order. The display
// fields are what would run (the clamped preview); they are plain values,
// never a dot to run.
func (e *Engine) projectLibraryLocked() []view.LibraryDot {
	if !e.projOn {
		return nil
	}
	var out []view.LibraryDot
	seen := map[string]bool{}
	for _, root := range slices.Sorted(maps.Keys(e.projects)) {
		ps := e.projects[root]
		for _, it := range ps.scan.Items {
			slug := it.Dot.Slug
			if seen[slug] {
				continue
			}
			seen[slug] = true
			var clamped []string
			for _, c := range it.Clamps {
				// The capabilities trust could grant; budgets, args and the
				// backend are always reset for shared dots (Task 2 rulings).
				if c.Keepable && !slices.Contains(clamped, c.Capability) {
					clamped = append(clamped, c.Capability)
				}
			}
			path := itemPath(root, it.Rel)
			out = append(out, view.LibraryDot{Slug: slug, Name: it.Effective.Name, Glyph: it.Effective.Glyph, Color: it.Effective.Color,
				Backend: it.Effective.Backend.Kind, Model: it.Effective.Backend.Model, Mode: string(it.Effective.Delivery.Mode),
				Write:               it.Effective.Capabilities.Write && dots.CanWrite(it.Effective.Backend.Kind),
				WriteTimeoutSeconds: it.Effective.Capabilities.WriteTimeoutSeconds,
				Origin:              dots.OriginProject, Trusted: it.Trusted, Clamped: clamped, Requested: slices.Clone(it.Requested),
				ProjectID: ps.pr.ID, ProjectName: ps.pr.Name, Path: path, SHA256: projectdots.Hex(it.Sum),
				Rel: it.Rel, TrustedAt: ps.trustedAt[slug]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// paneProjectsLocked maps scanned panes to their repository's project id.
func (e *Engine) paneProjectsLocked() map[string]string {
	if !e.projOn || len(e.paneRoots) == 0 {
		return nil
	}
	out := map[string]string{}
	for pane, root := range e.paneRoots {
		if ps, ok := e.projects[root]; ok {
			out[pane] = ps.pr.ID
		}
	}
	return out
}

// mcpMembers splits the pane's attached dots into those the MCP server may
// ask (capabilities.mcp) and reports whether any was left out. Imported and
// project dots start without the capability (dots.ClampDot); an attached dot
// that isn't loaded (an untrusted project dot) is left out too.
func (e *Engine) mcpMembers(paneID string) (allowed []string, blocked bool) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, da := range att.Dots {
		d, ok := e.dots[da.Slug]
		if !ok {
			blocked = blocked || e.untrustedLocked(da.Slug)
			continue
		}
		if !d.Capabilities.MCP {
			blocked = true
			continue
		}
		allowed = append(allowed, da.Slug)
	}
	return allowed, blocked
}

// shortHome abbreviates the home directory in a path shown to the developer.
func shortHome(p string) string {
	home, _ := os.UserHomeDir()
	return shortHomeIn(p, home)
}

func shortHomeIn(p, home string) string {
	if home == "" {
		return p
	}
	home = filepath.Clean(home)
	if pathsEqual(filepath.Clean(p), home) {
		return "~"
	}
	prefix := home + string(filepath.Separator)
	if len(p) > len(prefix) && pathsEqual(p[:len(prefix)], prefix) {
		return "~" + string(filepath.Separator) + p[len(prefix):]
	}
	return p
}

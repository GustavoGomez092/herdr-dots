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
	"time"

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/view"
)

// Project dots (Phase 9) reach the engine ONLY through
// projectdots.Item.Runnable: Item.Dot and Item.Effective are previews, filled
// in for untrusted files too, and the engine reads nothing from them but
// display fields (TestProjectDotsRunOnlyThroughRunnable).
//
// A project dot is (checkout root, slug) (ruling P9-R13): clones and
// worktrees of one repository share a project id and so a slug, but each
// checkout sees, trusts and runs its own files. A pane runs the copy of the
// checkout it is in (paneRoots). Trust stays keyed by project id, file and
// content (ruling 11): identical bytes trusted in one checkout are trusted
// in the other.

var (
	// ErrUntrusted: a project dot whose exact file content isn't trusted.
	ErrUntrusted = errors.New("project dot is not trusted")
	// ErrProjectDot: project dots are changed in their repository, not here.
	ErrProjectDot = errors.New("project dots are defined by their repository")
)

// dotUnavailable is list_dots' status for an attached dot the MCP server
// can't ask (capabilities.mcp off, or an untrusted project dot).
const dotUnavailable = "unavailable to MCP"

// lookWindow is how long a repository only looked at (ScanPane, nothing
// attached on the pane) stays scanned.
const lookWindow = 10 * time.Minute

// projectState is one checkout's scanned project dots.
type projectState struct {
	pr        memory.Project
	scan      projectdots.Scan
	seq       uint64              // the scan that produced this state (scanRoot)
	runnable  map[string]dots.Dot // slug -> Item.Runnable(): trusted content only, clamped
	sums      map[string][32]byte // slug -> hash of the trusted content in runnable
	trustedAt map[string]string   // slug -> where its trust was given, when that was another checkout
}

// projectDotsOn: a trust store exists and config.toml allows project dots
// (project_dots_enabled, false on a malformed config).
func (e *Engine) projectDotsOn() bool {
	return e.d.Trust != nil && e.config().ProjectDotsEnabled
}

// paneDotLocked is the dot slug runs as on paneID: a user dot, or the
// project dot of the checkout the pane is in. Callers hold e.mu.
func (e *Engine) paneDotLocked(paneID, slug string) (dots.Dot, bool) {
	if !dots.IsProjectSlug(slug) {
		d, ok := e.dots[slug]
		return d, ok
	}
	return e.rootDotLocked(e.paneRoots[paneID], slug)
}

// rootDotLocked is the runnable project dot slug of the checkout root.
func (e *Engine) rootDotLocked(root, slug string) (dots.Dot, bool) {
	ps := e.projects[root]
	if !e.projOn || root == "" || ps == nil {
		return dots.Dot{}, false
	}
	d, ok := ps.runnable[slug]
	return d, ok
}

// cardDotLocked is the dot a card's outcome is recorded for: as on its pane,
// else any loaded copy (the pane may be gone).
func (e *Engine) cardDotLocked(paneID, slug string) (dots.Dot, bool) {
	if d, ok := e.paneDotLocked(paneID, slug); ok {
		return d, true
	}
	d, ok := e.dots[slug]
	return d, ok
}

// projectDotGone reports whether d is a project dot that is no longer loaded
// as it was when its run was queued (its file changed, lost its trust, or
// project dots were turned off): such a run never starts. Background jobs
// (Phase 8), drafts and council members carry a copy of the dot, so runDot,
// runBackground and the draft run check this right before they call the
// backend, and an inject card is queued instead (routeCard).
//
// Trust is checked fresh, not only as of the last poll: each checkout that
// loads the slug is fingerprinted again (projectdots.Stamp: its files and the
// trust store) and rescanned when that changed. That I/O runs off every
// engine lock. A copy runs only if a checkout still loads exactly it.
func (e *Engine) projectDotGone(d dots.Dot) bool {
	if d.Origin != dots.OriginProject {
		return false
	}
	if !e.projectDotsOn() {
		return true
	}
	type loaded struct {
		pr    memory.Project
		stamp string
	}
	var roots []loaded
	e.mu.Lock()
	for _, ps := range e.projects {
		if _, ok := ps.runnable[d.Slug]; ok {
			roots = append(roots, loaded{ps.pr, ps.scan.Stamp})
		}
	}
	e.mu.Unlock()
	changed := false
	for _, l := range roots {
		if projectdots.Stamp(l.pr.Root, e.d.Trust) != l.stamp {
			changed = e.scanRoot(l.pr) || changed
		}
	}
	if changed {
		e.broadcast()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.projOn {
		return true
	}
	for _, ps := range e.projects {
		if cur, ok := ps.runnable[d.Slug]; ok && reflect.DeepEqual(cur, d) {
			return false
		}
	}
	return true
}

// cleanLine makes repository text (a file name, an error naming it) safe for
// a terminal: no control or bidi characters, one line.
func cleanLine(s string) string {
	return strings.Join(strings.Fields(textutil.SanitizeModelText(s)), " ")
}

// rebuildLocked recomputes e.dots and e.invalid from the user dots and the
// checkouts' project dots. Callers hold e.mu. e.dots holds, per project slug,
// the copy of the first checkout by root: it serves slug-wide work
// (schedules, SetMode's refusal); a pane runs its own checkout's copy
// (paneDotLocked).
func (e *Engine) rebuildLocked() {
	m := maps.Clone(e.userDots)
	if m == nil {
		m = map[string]dots.Dot{}
	}
	inv := slices.Clone(e.userBad)
	e.projOn = e.projectDotsOn()
	if e.projOn {
		for _, root := range slices.Sorted(maps.Keys(e.projects)) {
			ps := e.projects[root]
			label := projectLabel(ps)
			for _, le := range ps.scan.Invalid {
				// A repository names these files: a hostile name must not
				// reach a terminal raw (an OSC 52 clipboard write).
				le.File = label + ": " + cleanLine(le.File)
				if le.Err != nil {
					le.Err = errors.New(cleanLine(le.Err.Error()))
				}
				inv = append(inv, le)
			}
			for _, slug := range slices.Sorted(maps.Keys(ps.runnable)) {
				if _, taken := m[slug]; !taken {
					m[slug] = ps.runnable[slug]
				}
			}
		}
	}
	e.dots, e.invalid = m, inv
}

func projectLabel(ps *projectState) string {
	if ps.pr.Name != "" {
		return cleanLine(ps.pr.Name)
	}
	return cleanLine(filepath.Base(ps.pr.Root))
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

// knowPane records the pane's checkout and scans it the first time. It runs
// git and reads files: never under a lock. A pane dropped meanwhile (its
// generation moved) is not recorded: a reused id is resolved again.
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
		e.paneSeen[paneID] = e.now()
	}
	_, known := e.projects[pr.Root]
	e.mu.Unlock()
	if !known {
		e.scanRoot(pr)
	}
	return pr
}

// ScanPane makes the pane's checkout's project dots visible in the library
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

// rootWantedLocked: some pane is in this checkout.
func (e *Engine) rootWantedLocked(root string) bool {
	for _, r := range e.paneRoots {
		if r == root {
			return true
		}
	}
	return false
}

// scanRoot reads one checkout's project dots (outside every lock), installs
// them, and pauses attached dots whose trusted file changed, vanished or lost
// its trust. Scans are numbered per checkout: an older scan that finishes
// after a newer one is dropped, and so is a scan of a checkout no pane is in
// any more (pruned meanwhile). It reports whether anything changed.
func (e *Engine) scanRoot(pr memory.Project) bool {
	e.mu.Lock()
	e.scanSeq[pr.Root]++
	seq := e.scanSeq[pr.Root]
	e.mu.Unlock()
	s := projectdots.ScanProject(pr.Root, pr.ID, e.d.Trust)
	ps := &projectState{pr: pr, scan: s, seq: seq, runnable: map[string]dots.Dot{}, sums: map[string][32]byte{}, trustedAt: map[string]string{}}
	for _, it := range s.Items {
		r := it.Runnable()
		if r == nil {
			continue
		}
		ps.runnable[r.Slug], ps.sums[r.Slug] = *r, it.Sum
		// Clones share a project id (decision #11), so trust may come from
		// another checkout: say where. The same file reached through a
		// symlink is this checkout (sameRepo stats both: off every lock).
		if it.TrustedPath != "" && !sameRepo(it.TrustedPath, itemPath(pr.Root, it.Rel)) {
			ps.trustedAt[r.Slug] = cleanLine(shortHome(it.TrustedPath))
		}
	}
	e.hook("scan-read")
	e.mu.Lock()
	old := e.projects[pr.Root]
	if (old != nil && old.seq > seq) || !e.rootWantedLocked(pr.Root) {
		e.mu.Unlock()
		return false
	}
	e.projects[pr.Root] = ps
	var revoked []dots.Dot
	var narrowed []string // same bytes, inject no longer granted
	if old != nil {
		for _, slug := range slices.Sorted(maps.Keys(old.runnable)) {
			sum, ok := ps.sums[slug]
			switch {
			case !ok || sum != old.sums[slug]:
				revoked = append(revoked, old.runnable[slug])
			case old.runnable[slug].Delivery.Mode == dots.ModeInject && ps.runnable[slug].Delivery.Mode != dots.ModeInject:
				narrowed = append(narrowed, slug)
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
		e.pauseRevoked(pr.Root, d)
	}
	for _, slug := range narrowed {
		e.requeueAuto(e.panesIn(pr.Root), slug)
	}
	return old == nil || old.scan.Stamp != s.Stamp
}

// pauseRevoked pauses a project dot on every pane of its checkout it is
// attached to, puts its inject cards still waiting for the agent back in the
// queue (under e.dmu, so no flush delivers them meanwhile), and says why
// once. d is the copy that ran until now (glyph and name for the toast).
func (e *Engine) pauseRevoked(root string, d dots.Dot) {
	panes := e.panesIn(root)
	attached := false
	for _, p := range panes {
		a, ok := e.d.Attachments.Get(p)
		if !ok || a.Find(d.Slug) == nil {
			continue
		}
		attached = true
		_ = e.d.Attachments.Update(p, func(x *state.Attachment) {
			if da := x.Find(d.Slug); da != nil {
				da.Paused = true
			}
		})
	}
	e.requeueAuto(panes, d.Slug)
	if attached {
		e.notify(d, fmt.Sprintf("project dot %s changed or lost its trust and is paused: review it in the library (T) to trust it again", dots.DisplaySlug(d.Slug)))
	}
}

// panesIn lists the panes in checkout root.
func (e *Engine) panesIn(root string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var panes []string
	for p, r := range e.paneRoots {
		if r == root {
			panes = append(panes, p)
		}
	}
	return panes
}

// requeueAuto puts slug's unapproved inject cards still waiting for the
// agent on panes back in the queue, under e.dmu so no flush delivers them
// meanwhile (the dot lost its trust, changed, or lost the inject grant).
// Callers hold no lock.
func (e *Engine) requeueAuto(panes []string, slug string) {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	for _, p := range panes {
		for _, c := range e.d.Queue.Pending(p) {
			if c.Dot == slug && c.Status == queue.StatusAuto && !c.Approved {
				if err := e.d.Queue.SetStatus(c.ID, queue.StatusPending); err != nil {
					e.d.Logf("card %s: back to the queue: %v", c.ID, err)
				}
			}
		}
	}
}

// projectCardStale reports whether an unapproved inject card's dot is a
// project dot its pane no longer loads (untrusted, changed, or project dots
// off) or that no longer injects (re-trusted without the inject grant).
// flushAuto keeps such a card for the developer. In memory only: it runs
// under dmu.
func (e *Engine) projectCardStale(paneID, slug string) bool {
	if !dots.IsProjectSlug(slug) {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	d, ok := e.paneDotLocked(paneID, slug)
	return !ok || d.Delivery.Mode != dots.ModeInject
}

// pruneLocked forgets panes only looked at (no attachment) longer than
// lookWindow ago, then checkouts no pane is in. Callers hold e.mu.
func (e *Engine) pruneLocked(now time.Time) {
	for p := range e.paneRoots {
		if _, ok := e.d.Attachments.Get(p); ok {
			continue
		}
		if now.Sub(e.paneSeen[p]) < lookWindow {
			continue
		}
		delete(e.paneRoots, p)
		delete(e.paneSeen, p)
	}
	pruned := false
	for root := range e.projects {
		if !e.rootWantedLocked(root) {
			delete(e.projects, root)
			pruned = true
		}
	}
	if pruned {
		e.rebuildLocked()
	}
}

// rescanProjects prunes, then scans every known checkout whose files or
// trust store changed (all of them with force). It also applies a
// project_dots_enabled change: turned on, the attached panes' checkouts are
// resolved (git, in a spawned goroutine). It reports whether the library
// changed.
func (e *Engine) rescanProjects(force bool) bool {
	e.mu.Lock()
	before := len(e.projects)
	e.pruneLocked(e.now())
	var prs []memory.Project
	var stamps []string
	for _, ps := range e.projects {
		prs = append(prs, ps.pr)
		stamps = append(stamps, ps.scan.Stamp)
	}
	on := e.projectDotsOn()
	flipped := e.projOn != on
	if flipped {
		e.rebuildLocked()
	}
	e.mu.Unlock()
	if flipped && on {
		e.spawn(e.knowAttachedPanes)
	}
	changed := flipped || len(prs) != before
	if !on {
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

// knowAttachedPanes resolves and scans every attached pane's checkout.
func (e *Engine) knowAttachedPanes() {
	for _, a := range e.d.Attachments.List() {
		e.knowPane(a.PaneID, a.Cwd)
	}
	e.broadcast()
}

// paneRepo is the checkout paneID is in, refusing when the developer
// reviewed an entry from another one (root, from LibraryDot.Root).
func (e *Engine) paneRepo(ctx context.Context, paneID, root string) (memory.Project, error) {
	if !e.projectDotsOn() {
		return memory.Project{}, errors.New("project dots are turned off (project_dots_enabled = false)")
	}
	p, err := e.d.Herdr.PaneGet(ctx, paneID)
	if err != nil {
		return memory.Project{}, fmt.Errorf("pane %s: %w", paneID, err)
	}
	pr := e.knowPane(paneID, p.WorkDir())
	if pr.Root == "" {
		return pr, fmt.Errorf("pane %s is not in a git repository with commits", paneID)
	}
	if root != "" && !pathsEqual(filepath.Clean(root), filepath.Clean(pr.Root)) {
		return pr, fmt.Errorf("pane %s is in %s, not %s: review the dot there again", paneID, cleanLine(shortHome(pr.Root)), cleanLine(shortHome(root)))
	}
	return pr, nil
}

// Trust trusts a project dot of paneID's checkout exactly as the developer
// reviewed it: sumHex is the hash of the bytes they saw; a file changed
// since is refused. Only capabilities the file asks for can be granted.
func (e *Engine) Trust(ctx context.Context, paneID, slug, sumHex string, caps []string) error {
	return e.trustAt(ctx, paneID, "", slug, sumHex, caps)
}

// trustAt is Trust for the entry of checkout root ("" = the pane's).
func (e *Engine) trustAt(ctx context.Context, paneID, root, slug, sumHex string, caps []string) error {
	if err := dots.CheckCapabilities(caps); err != nil {
		return err
	}
	pr, err := e.paneRepo(ctx, paneID, root)
	if err != nil {
		return err
	}
	it, ok := projectdots.ScanProject(pr.Root, pr.ID, e.d.Trust).Find(slug) // fresh bytes
	if !ok {
		return fmt.Errorf("no project dot %s in %s", dots.DisplaySlug(slug), cleanLine(pr.Name))
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
	e.rescanProject(pr)
	return nil
}

// rescanProject rescans every known checkout of pr's project after a trust
// change (identical bytes are trusted in all of them), pr's first.
func (e *Engine) rescanProject(pr memory.Project) {
	e.scanRoot(pr)
	e.mu.Lock()
	var others []memory.Project
	for root, ps := range e.projects {
		if root != pr.Root && ps.pr.ID == pr.ID {
			others = append(others, ps.pr)
		}
	}
	e.mu.Unlock()
	for _, o := range others {
		e.scanRoot(o)
	}
	e.broadcast()
}

// Untrust forgets a project dot's trust in paneID's checkout; attached
// copies are paused.
func (e *Engine) Untrust(ctx context.Context, paneID, slug string) error {
	return e.untrustAt(ctx, paneID, "", slug)
}

// untrustAt revokes by (checkout, slug). A file that parses loses the trust
// of its current bytes (in every checkout that holds them). A file that is
// gone or no longer parses loses this checkout's entry (by path; the file
// name is the slug's base), or every entry of the file when none was given
// at this path (trusted through another path to the same checkout).
func (e *Engine) untrustAt(ctx context.Context, paneID, root, slug string) error {
	pr, err := e.paneRepo(ctx, paneID, root)
	if err != nil {
		return err
	}
	base, tag, ok := dots.SplitProjectSlug(slug)
	if !ok || tag != dots.ProjectTag(pr.ID) {
		return fmt.Errorf("%s is not a project dot of %s", dots.DisplaySlug(slug), cleanLine(pr.Name))
	}
	rel := filepath.ToSlash(dots.ProjectDir) + "/" + base + ".toml"
	var found bool
	if it, ok := projectdots.ScanProject(pr.Root, pr.ID, e.d.Trust).Find(slug); ok {
		found, err = e.d.Trust.RevokeSum(pr.ID, rel, it.Sum)
	} else if found, err = e.d.Trust.RevokePath(pr.ID, rel, itemPath(pr.Root, rel)); err == nil && !found {
		found, err = e.d.Trust.Revoke(pr.ID, rel)
	}
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s was not trusted", dots.DisplaySlug(slug))
	}
	e.rescanProject(pr)
	return nil
}

// untrustedAtLocked reports whether slug is a scanned, untrusted project dot
// of checkout root.
func (e *Engine) untrustedAtLocked(root, slug string) bool {
	ps := e.projects[root]
	if !e.projOn || ps == nil {
		return false
	}
	it, ok := ps.scan.Find(slug)
	return ok && !it.Trusted
}

// itemPath is a project dot file's absolute path in a checkout.
func itemPath(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// projectLibraryLocked lists project dots for the snapshot, one entry per
// (checkout, slug), trusted or not. The display fields are what would run
// (the clamped preview); they are plain values, never a dot to run.
func (e *Engine) projectLibraryLocked() []view.LibraryDot {
	if !e.projOn {
		return nil
	}
	var out []view.LibraryDot
	for _, root := range slices.Sorted(maps.Keys(e.projects)) {
		ps := e.projects[root]
		for _, it := range ps.scan.Items {
			slug := it.Dot.Slug
			var clamped []string
			for _, c := range it.Clamps {
				// The capabilities trust could grant; budgets, args and the
				// backend are always reset for shared dots (Task 2 rulings).
				if c.Keepable && !slices.Contains(clamped, c.Capability) {
					clamped = append(clamped, c.Capability)
				}
			}
			out = append(out, view.LibraryDot{Slug: slug, Name: it.Effective.Name, Glyph: it.Effective.Glyph, Color: it.Effective.Color,
				Backend: it.Effective.Backend.Kind, Model: it.Effective.Backend.Model, Mode: string(it.Effective.Delivery.Mode),
				Write:               it.Effective.Capabilities.Write && dots.CanWrite(it.Effective.Backend.Kind),
				WriteTimeoutSeconds: it.Effective.Capabilities.WriteTimeoutSeconds,
				Origin:              dots.OriginProject, Trusted: it.Trusted, Clamped: clamped, Requested: slices.Clone(it.Requested),
				ProjectID: ps.pr.ID, ProjectName: cleanLine(ps.pr.Name), Path: itemPath(root, it.Rel), SHA256: projectdots.Hex(it.Sum),
				Rel: it.Rel, TrustedAt: ps.trustedAt[slug], Root: root,
				// What runs, for the profile: the file may have changed since.
				Purpose: ps.runnable[slug].Purpose})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); a != b {
			return a < b
		}
		return out[i].Root < out[j].Root
	})
	return out
}

// paneProjectsLocked maps scanned panes to their checkout's project id and
// root.
func (e *Engine) paneProjectsLocked() (ids, roots map[string]string) {
	if !e.projOn || len(e.paneRoots) == 0 {
		return nil, nil
	}
	ids, roots = map[string]string{}, map[string]string{}
	for pane, root := range e.paneRoots {
		if ps, ok := e.projects[root]; ok {
			ids[pane], roots[pane] = ps.pr.ID, root
		}
	}
	return ids, roots
}

// projectTargetLocked is a scheduled project dot's one target: the checkout
// whose copy it is (each checkout runs its own files).
func (e *Engine) projectTargetLocked(d dots.Dot) []schedTarget {
	for _, root := range slices.Sorted(maps.Keys(e.projects)) {
		ps := e.projects[root]
		if cur, ok := ps.runnable[d.Slug]; ok && reflect.DeepEqual(cur, d) {
			return []schedTarget{{id: ps.pr.ID, name: projectLabel(ps), cwd: root}}
		}
	}
	return nil
}

// mcpMembers lists the pane's attached dots the MCP server may ask
// (capabilities.mcp) and reports whether any was left out. Imported and
// project dots start without the capability (dots.ClampDot); an attached
// project dot its checkout doesn't load (untrusted) is left out too.
func (e *Engine) mcpMembers(paneID string) (allowed []string, blocked bool) {
	att, ok := e.d.Attachments.Get(paneID)
	if !ok {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, da := range att.Dots {
		d, ok := e.paneDotLocked(paneID, da.Slug)
		if !ok {
			blocked = blocked || e.untrustedAtLocked(e.paneRoots[paneID], da.Slug)
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

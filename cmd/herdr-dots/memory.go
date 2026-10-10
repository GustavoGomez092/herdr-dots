package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/platform"
	"herdr-dots/internal/textutil"
)

const memUsage = `usage: herdr-dots memory <command> [flags]
  projects                                          list projects with memory
  list   [--project P] [--dot SLUG] [--scope S] [--kind K] [--all] [--json]
  add    --scope S [--dot SLUG] [--kind K] [--paths GLOBS] [--pin] [--force] "text"
  edit   [--project P] [--dot SLUG] [--scope S] [--yes]  edit as TOML in $EDITOR
  forget REF [--dot SLUG] [--project P] [--scrub]
  forget-all --scope S [--dot SLUG] [--project P] [--yes]
  pin|unpin REF [--dot SLUG] [--project P]
  approve REF [--dot SLUG] [--project P] [--yes]    show the full text, then confirm
  export [--project P] [--dot SLUG] [--scope S]     TOML on stdout
  import FILE [--project P] [--yes]                 restore a TOML export (proposed pack entries wait for approval)
  purge  --project P [--yes]                        delete a project's whole memory
P is a project path or id (default: the current directory's project).
K is one of convention, decision, finding, dismissed, preference, note, research, question.
REF looks like p:m3 (project), d:m9 or g:m2 (dot and global; need --dot).`

// memCLI is `herdr-dots memory`. It uses the memory store directly under its
// file lock, so it works with or without a running engine.
type memCLI struct {
	store       *memory.Store
	resolve     func(dir string) (memory.Project, error)
	cwd         string
	editor      string
	out, errOut io.Writer
	in          io.Reader
	interactive bool
}

func runMemory(args []string) error {
	p, err := paths.Discover()
	if err != nil {
		return err
	}
	cfg, cerr := config.Load(p.ConfigFile())
	if cerr != nil {
		fmt.Fprintln(os.Stderr, "herdr-dots: config:", cerr, "(using defaults)")
	}
	cwd, _ := os.Getwd()
	st, _ := os.Stdin.Stat()
	c := &memCLI{
		store: memory.Open(p.MemoryDir(), memory.LimitsFromConfig(cfg.MemoryMaxEntries, cfg.MemoryRetentionDays, cfg.MemoryAutoApproveProject)),
		resolve: func(dir string) (memory.Project, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return memory.ResolveProject(ctx, dir, memory.ExecGit)
		},
		cwd: cwd, editor: os.Getenv("EDITOR"), out: os.Stdout, errOut: os.Stderr, in: os.Stdin,
		interactive: st != nil && st.Mode()&os.ModeCharDevice != 0,
	}
	return c.run(args)
}

func (c *memCLI) run(args []string) (err error) {
	defer func() {
		if errors.Is(err, flag.ErrHelp) { // -h: the flag set printed its usage
			err = nil
		}
	}()
	if len(args) == 0 {
		return errors.New(memUsage)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "projects":
		return c.projects()
	case "list":
		return c.list(rest)
	case "add":
		return c.add(rest)
	case "edit":
		return c.edit(rest)
	case "forget":
		return c.forget(rest)
	case "forget-all":
		return c.forgetAll(rest)
	case "pin", "unpin":
		return c.mark(cmd, rest)
	case "approve":
		return c.approve(rest)
	case "export":
		return c.export(rest)
	case "import":
		return c.importFile(rest)
	case "purge":
		return c.purge(rest)
	case "help", "-h", "--help":
		fmt.Fprintln(c.out, memUsage)
		return nil
	}
	return fmt.Errorf("unknown memory command %q\n%s", cmd, memUsage)
}

type memFlags struct {
	fs                                  *flag.FlagSet
	project, dot, scope, kind, globs    string
	all, asJSON, pin, force, scrub, yes bool
}

func (c *memCLI) flags(name string) *memFlags {
	f := &memFlags{fs: flag.NewFlagSet("memory "+name, flag.ContinueOnError)}
	f.fs.SetOutput(c.errOut)
	f.fs.StringVar(&f.project, "project", "", "project path or id")
	f.fs.StringVar(&f.dot, "dot", "", "dot slug")
	f.fs.StringVar(&f.scope, "scope", "", "project, dot or global")
	f.fs.StringVar(&f.kind, "kind", "", "convention, decision, finding, dismissed, preference, note, research or question")
	f.fs.StringVar(&f.globs, "paths", "", "comma-separated path globs")
	f.fs.BoolVar(&f.all, "all", false, "include archived entries")
	f.fs.BoolVar(&f.asJSON, "json", false, "print JSON")
	f.fs.BoolVar(&f.pin, "pin", false, "pin the new entry")
	f.fs.BoolVar(&f.force, "force", false, "store text even if it looks like a secret")
	f.fs.BoolVar(&f.scrub, "scrub", false, "also delete matching ledger lines")
	f.fs.BoolVar(&f.yes, "yes", false, "don't ask for confirmation")
	return f
}

// parse accepts flags before and after positional arguments.
func (f *memFlags) parse(args []string) ([]string, error) {
	var pos []string
	for {
		if err := f.fs.Parse(args); err != nil {
			return nil, err
		}
		args = f.fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}

func (f *memFlags) scopeValue() (memory.Scope, error) {
	sc := memory.Scope(f.scope)
	if f.scope != "" && !sc.Valid() {
		return "", fmt.Errorf("--scope must be project, dot or global, got %q", f.scope)
	}
	if (sc == memory.ScopeDot || sc == memory.ScopeGlobal) && f.dot == "" {
		return "", fmt.Errorf("--scope %s needs --dot", sc)
	}
	return sc, nil
}

var allStatuses = []memory.Status{memory.StatusActive, memory.StatusProposed, memory.StatusArchived}

// project resolves --project (an id or a path) or the current directory.
func (c *memCLI) project(arg string) (memory.Project, error) {
	if memory.ValidProjectID(arg) {
		m, ok, err := c.store.FindProject(arg)
		if ok {
			return memory.Project{ID: m.ID, Name: m.Name}, nil
		}
		if err != nil {
			return memory.Project{}, err
		}
		return memory.Project{ID: arg}, nil
	}
	dir := arg
	if dir == "" {
		dir = c.cwd
	} else if !filepath.IsAbs(dir) && c.cwd != "" {
		dir = filepath.Join(c.cwd, dir)
	}
	// Git first: a repository's g- id is the project, even when it sits inside
	// another recorded project (a vendored repo or submodule nobody attached
	// yet) or under a stale deeper path project. The recorded roots are only
	// asked for a path identity (no repository, git missing), where a recorded
	// project (say, a moved or not-yet-committed checkout) beats a fresh
	// path hash, but only one recorded inside the repository the path
	// belongs to (insideRepoBoundary), never an outer project.
	pr, err := c.resolve(dir)
	if strings.HasPrefix(pr.ID, "g-") {
		return pr, nil
	}
	if m, ok, ferr := c.store.FindProject(dir); ferr == nil && ok && insideRepoBoundary(m, dir) {
		return memory.Project{ID: m.ID, Name: m.Name}, nil
	}
	if pr.ID == "" {
		return pr, fmt.Errorf("can't identify the project for %s: %v", dir, err)
	}
	return pr, nil
}

// insideRepoBoundary reports whether recorded project m may stand for dir
// when git gave dir only a path identity (no commits, a shallow clone, git
// failing or timing out). The nearest directory at or above dir that holds a
// .git entry is the repository dir belongs to; a recorded root above it is
// another (outer) project and doesn't count. With no .git on the way up, any
// recorded root that holds dir counts, as before.
func insideRepoBoundary(m memory.ProjectMeta, dir string) bool {
	real := realDir(dir)
	boundary := ""
	for d := real; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			boundary = d
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	for _, r := range m.Roots {
		holds := real == r || strings.HasPrefix(real, r+string(filepath.Separator))
		atOrBelow := boundary == "" || r == boundary || strings.HasPrefix(r, boundary+string(filepath.Separator))
		if holds && atOrBelow {
			return true
		}
	}
	return false
}

// realDir is dir made absolute with symlinks resolved, as the store records
// project roots (a path that doesn't exist is only cleaned).
func realDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return filepath.Clean(dir)
}

func (c *memCLI) pidFor(sc memory.Scope, project string) (string, error) {
	if sc == memory.ScopeGlobal {
		return "", nil
	}
	pr, err := c.project(project)
	return pr.ID, err
}

func (c *memCLI) confirm(prompt, want string) bool {
	if !c.interactive {
		return false
	}
	fmt.Fprintf(c.out, "%s Type %q to confirm: ", prompt, want)
	line, _ := bufio.NewReader(c.in).ReadString('\n')
	return strings.TrimSpace(line) == want
}

// secretGate warns about entries whose text looks like a credential and asks
// the developer to confirm (--yes or --force skips the question). It never
// refuses outright: without a terminal and without --yes it stops so nothing
// is stored unseen.
func (c *memCLI) secretGate(verb string, entries []memory.Entry, f *memFlags) error {
	hits := memory.SecretHits(entries)
	if len(hits) == 0 {
		return nil
	}
	fmt.Fprintf(c.errOut, "warning: %d %s entries look like they contain a secret\n", len(hits), verb)
	if f.yes || f.force {
		return nil
	}
	if !c.interactive {
		return fmt.Errorf("%s: entries look like secrets; rerun with --yes to store them anyway", verb)
	}
	fmt.Fprint(c.out, "Store them anyway? [y/N]: ")
	line, _ := bufio.NewReader(c.in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		return fmt.Errorf("%s: not confirmed (nothing changed)", verb)
	}
	return nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func defaultKindFor(sc memory.Scope) memory.Kind {
	switch sc {
	case memory.ScopeProject:
		return memory.KindConvention
	case memory.ScopeGlobal:
		return memory.KindPreference
	}
	return memory.KindNote
}

func (c *memCLI) projects() error {
	ps, err := c.store.Projects()
	if err != nil {
		fmt.Fprintln(c.errOut, "warning:", err)
	}
	if len(ps) == 0 {
		fmt.Fprintln(c.out, "no projects with memory yet")
		return nil
	}
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tROOTS")
	for _, p := range ps {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", p.ID, p.Name, strings.Join(p.Roots, ", "))
	}
	return tw.Flush()
}

type listItem struct {
	Ref string `json:"ref"`
	memory.Entry
}

func (c *memCLI) list(args []string) error {
	f := c.flags("list")
	if _, err := f.parse(args); err != nil {
		return err
	}
	sc, err := f.scopeValue()
	if err != nil {
		return err
	}
	pid, err := c.pidFor(sc, f.project)
	if err != nil {
		return err
	}
	filter := memory.Filter{Scope: sc, Dot: f.dot}
	if f.kind != "" {
		filter.Kinds = []memory.Kind{memory.Kind(f.kind)}
	}
	if f.all {
		filter.Statuses = allStatuses
	}
	es, lerr := c.store.List(pid, filter)
	if lerr != nil {
		fmt.Fprintln(c.errOut, "warning:", lerr)
	}
	if f.asJSON {
		items := make([]listItem, 0, len(es))
		for _, e := range es {
			items = append(items, listItem{Ref: e.Ref().String(), Entry: e})
		}
		enc := json.NewEncoder(c.out)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}
	if len(es) == 0 {
		fmt.Fprintln(c.out, "no memory entries")
		return nil
	}
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REF\tDOT\tKIND\tSTATUS\tUSES\tTEXT")
	for _, e := range es {
		pin := ""
		if e.Pinned {
			pin = "★ "
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s%s\n", e.Ref(), e.Dot, e.Kind, e.Status, e.Uses, pin, e.Text)
	}
	return tw.Flush()
}

func (c *memCLI) add(args []string) error {
	f := c.flags("add")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(pos, " "))
	if text == "" {
		return errors.New("add: missing text")
	}
	if f.scope == "" {
		return errors.New("add: --scope is required (project, dot or global)")
	}
	sc, err := f.scopeValue()
	if err != nil {
		return err
	}
	kind := memory.Kind(f.kind)
	if kind == "" {
		kind = defaultKindFor(sc)
	}
	globs := splitCSV(f.globs)
	if found, what := memory.ScanSecrets(text + " " + strings.Join(globs, " ")); found {
		if !f.force {
			return fmt.Errorf("refusing: the text or paths look like a secret (%s); rerun with --force to store it anyway", what)
		}
		fmt.Fprintf(c.errOut, "warning: storing text that looks like a secret (%s)\n", what)
	}
	var pid string
	if sc != memory.ScopeGlobal {
		pr, err := c.project(f.project)
		if err != nil {
			return err
		}
		pid = pr.ID
		if pr.Root != "" {
			_ = c.store.SeeProject(pr)
		}
	}
	e := memory.Entry{Scope: sc, Kind: kind, Text: text, Paths: globs, Source: memory.SourceUser, Status: memory.StatusActive, Pinned: f.pin}
	if sc != memory.ScopeProject {
		e.Dot = f.dot
	}
	got, err := c.store.Add(pid, e)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "added %s\n", got.Ref())
	return nil
}

func (c *memCLI) ref(f *memFlags, pos []string, verb string) (memory.Ref, string, error) {
	if len(pos) != 1 {
		return memory.Ref{}, "", fmt.Errorf("%s: want exactly one REF (like d:m9)", verb)
	}
	ref, err := memory.ParseRef(pos[0], f.dot)
	if err != nil {
		return ref, "", err
	}
	pid, err := c.pidFor(ref.Scope, f.project)
	return ref, pid, err
}

func (c *memCLI) forget(args []string) error {
	f := c.flags("forget")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	ref, pid, err := c.ref(f, pos, "forget")
	if err != nil {
		return err
	}
	if err := c.store.Forget(pid, ref, f.scrub); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "forgot %s\n", ref)
	return nil
}

func (c *memCLI) forgetAll(args []string) error {
	f := c.flags("forget-all")
	if _, err := f.parse(args); err != nil {
		return err
	}
	if f.scope == "" {
		return errors.New("forget-all: --scope is required (project, dot or global)")
	}
	sc, err := f.scopeValue()
	if err != nil {
		return err
	}
	var pr memory.Project
	if sc != memory.ScopeGlobal {
		if pr, err = c.project(f.project); err != nil {
			return err
		}
	}
	if !f.yes {
		// The prompt names the project (name and id); the developer types
		// the project's name for project scope, the dot for dot and global.
		pname := strings.TrimSpace(textutil.SanitizeModelText(pr.Name))
		if pname == "" {
			pname = pr.ID
		}
		name, what := f.dot, fmt.Sprintf("dot %s on every project", f.dot)
		switch sc {
		case memory.ScopeProject:
			name, what = pname, fmt.Sprintf("project %s (%s)", pname, pr.ID)
		case memory.ScopeDot:
			what = fmt.Sprintf("dot %s on project %s (%s)", f.dot, pname, pr.ID)
		}
		if !c.interactive {
			return errors.New("forget-all: refusing without --yes (not a terminal)")
		}
		if !c.confirm(fmt.Sprintf("Forget ALL %s memory for %s?", sc, what), name) {
			return errors.New("forget-all: not confirmed")
		}
	}
	n, err := c.store.ForgetAll(pr.ID, sc, f.dot)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "forgot %d entries\n", n)
	return nil
}

func (c *memCLI) mark(verb string, args []string) error {
	f := c.flags(verb)
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	ref, pid, err := c.ref(f, pos, verb)
	if err != nil {
		return err
	}
	was := false
	got, err := c.store.Update(pid, ref, func(e *memory.Entry) error {
		was = e.Status == memory.StatusArchived
		switch verb {
		case "pin":
			e.Pinned = true
		case "unpin":
			e.Pinned = false
		}
		return nil
	})
	if err != nil {
		return err
	}
	past := map[string]string{"pin": "pinned", "unpin": "unpinned"}[verb]
	if got.Status == memory.StatusArchived && !was {
		fmt.Fprintln(c.out, c.archivedAtOnce(past, got))
		return nil
	}
	fmt.Fprintf(c.out, "%s %s\n", past, ref)
	return nil
}

// errApproveChanged refuses an approve when the stored text is no longer the
// text that was shown.
var errApproveChanged = errors.New("this proposal changed since it was shown; nothing approved, review it again")

// approve shows the proposal's full text, kind, paths and proposing dot, asks
// to confirm (--yes without a terminal), and approves only the text it showed.
func (c *memCLI) approve(args []string) error {
	f := c.flags("approve")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	ref, pid, err := c.ref(f, pos, "approve")
	if err != nil {
		return err
	}
	e, err := c.store.Get(pid, ref)
	if err != nil {
		return err
	}
	if e.Status != memory.StatusProposed {
		return fmt.Errorf("%s is not a proposal", ref)
	}
	by := e.Dot
	switch {
	case e.Source == memory.SourceAgent:
		by = "the main agent"
	case by == "":
		by = "the developer"
	}
	paths := strings.Join(e.Paths, ", ")
	if paths == "" {
		paths = "(none)"
	}
	origin := "proposed here"
	if e.Source == memory.SourceImport {
		origin = "restored from a backup of your own export"
		if e.From != "" {
			origin = "imported from a shared dot pack (" + textutil.SanitizeModelText(e.From) + "), not written by you"
		}
	}
	fmt.Fprintf(c.out, "Proposal %s\n  origin:      %s\n  proposed by: %s\n  kind:        %s\n  paths:       %s\n  text:\n%s\n",
		ref, origin, textutil.SanitizeModelText(by), e.Kind, textutil.SanitizeModelText(paths), textutil.SanitizeModelText(e.Text))
	if !f.yes {
		if !c.interactive {
			return errors.New("approve: refusing without --yes (not a terminal)")
		}
		fmt.Fprint(c.out, "Approve this proposal? [y/N]: ")
		line, _ := bufio.NewReader(c.in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			return errors.New("approve: not confirmed (nothing changed)")
		}
	}
	seen := e.Text
	got, err := c.store.Update(pid, ref, func(x *memory.Entry) error {
		if x.Text != seen {
			return errApproveChanged
		}
		if x.Status != memory.StatusProposed {
			return fmt.Errorf("%s is not a proposal", ref)
		}
		x.Status = memory.StatusActive
		return nil
	})
	if err != nil {
		return err
	}
	if got.Status == memory.StatusArchived {
		fmt.Fprintln(c.out, c.archivedAtOnce("approved", got))
		return nil
	}
	fmt.Fprintf(c.out, "approved %s\n", ref)
	return nil
}

// archivedAtOnce says that an approval or unpin took a full scope over its
// limit and the entry, an import, was archived by the cap (P9-R15).
func (c *memCLI) archivedAtOnce(verb string, e memory.Entry) string {
	whose := "the project"
	switch e.Scope {
	case memory.ScopeDot:
		whose = textutil.SanitizeModelText(e.Dot)
	case memory.ScopeGlobal:
		whose = "global"
	}
	if e.Source != memory.SourceImport {
		return fmt.Sprintf("%s was archived at once: %s's memory is full (limit %d); research notes give way first. Forget an entry or raise the limit.",
			e.Ref(), whose, c.store.Cap(e.Scope))
	}
	return fmt.Sprintf("%s %s, but it was archived at once: %s's memory is full (limit %d) and imported entries give way to yours. Forget an entry or raise the limit, then import it again.",
		verb, e.Ref(), whose, c.store.Cap(e.Scope))
}

func (c *memCLI) export(args []string) error {
	f := c.flags("export")
	if _, err := f.parse(args); err != nil {
		return err
	}
	sc, err := f.scopeValue()
	if err != nil {
		return err
	}
	pid, err := c.pidFor(sc, f.project)
	if err != nil {
		return err
	}
	es, err := c.store.List(pid, memory.Filter{Scope: sc, Dot: f.dot, Statuses: allStatuses})
	if err != nil {
		return err
	}
	data, err := memory.ExportTOML(es)
	if err != nil {
		return err
	}
	_, err = c.out.Write(data)
	return err
}

func (c *memCLI) importFile(args []string) error {
	f := c.flags("import")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("import: want exactly one FILE")
	}
	data, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}
	es, err := memory.ImportTOML(data)
	if err != nil {
		return err
	}
	// Resolve the project before anything is written, so an unknown
	// project never leaves a partial import; an all-global import needs none.
	var pr memory.Project
	if memory.NeedsProject(es) {
		if pr, err = c.project(f.project); err != nil {
			return fmt.Errorf("import: %w (nothing imported)", err)
		}
	}
	// Every entry is added (the store dedupes), so every entry is checked.
	if err := c.secretGate("import", es, f); err != nil {
		return err
	}
	res := c.store.ImportEntries(pr.ID, es)
	for _, line := range res.Skipped {
		fmt.Fprintln(c.errOut, line)
	}
	fmt.Fprintf(c.out, "added or merged %d entries\n", res.Added)
	return nil
}

func (c *memCLI) edit(args []string) error {
	f := c.flags("edit")
	if _, err := f.parse(args); err != nil {
		return err
	}
	if c.editor == "" {
		return errors.New("edit: set $EDITOR (for example EDITOR=vim)")
	}
	sc, err := f.scopeValue()
	if err != nil {
		return err
	}
	var pr memory.Project
	if sc != memory.ScopeGlobal {
		if pr, err = c.project(f.project); err != nil {
			return err
		}
	}
	before, lerr := c.store.List(pr.ID, memory.Filter{Scope: sc, Dot: f.dot, Statuses: allStatuses})
	if lerr != nil {
		fmt.Fprintln(c.errOut, "warning:", lerr)
	}
	data, err := memory.ExportTOML(before)
	if err != nil {
		return err
	}
	// A private 0700 directory, removed with everything the editor leaves
	// in it (swap and backup files hold the same memory text).
	dir, err := os.MkdirTemp("", "herdr-dots-memory-*") // 0700
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := platform.SecureDir(dir); err != nil {
		return err
	}
	file := filepath.Join(dir, "memory.toml")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return err
	}
	cmd := platform.EditorCommand(c.editor, file) // sh -c on Unix, cmd.exe on Windows
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.in, c.out, c.errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("edit: the editor failed: %w (nothing changed)", err)
	}
	edited, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	after, err := memory.ImportTOML(edited)
	if err != nil {
		return fmt.Errorf("edit: %w (nothing changed)", err)
	}
	adds, updates, deletes := memory.DiffTOML(before, after)
	if err := c.secretGate("edit", append(append([]memory.Entry{}, adds...), updates...), f); err != nil {
		return err
	}
	pidOf := func(s memory.Scope) string {
		if s == memory.ScopeGlobal {
			return ""
		}
		return pr.ID
	}
	var na, nu, nd int
	var errs []error
	for _, e := range adds {
		e.Source, e.From = memory.SourceUser, "" // the developer wrote it
		if _, err := c.store.Add(pidOf(e.Scope), e); err != nil {
			errs = append(errs, err)
			continue
		}
		na++
	}
	for _, e := range updates {
		got, err := c.store.Update(pidOf(e.Scope), e.Ref(), func(x *memory.Entry) error {
			imported := x.Source == memory.SourceImport
			textChanged := strings.TrimSpace(e.Text) != strings.TrimSpace(x.Text)
			// An import is someone else's conclusion and never suppresses
			// repeats (P9-R8). Flipping its status in the editor must not
			// relabel it as the developer's words: approving is a separate,
			// reviewed step.
			if imported && !textChanged && x.Status == memory.StatusProposed && e.Status != memory.StatusProposed {
				return fmt.Errorf("%s is an imported proposal: approve it with herdr-dots memory approve", x.Ref())
			}
			x.Text, x.Kind, x.Paths, x.Pinned, x.Status = e.Text, e.Kind, e.Paths, e.Pinned, e.Status
			// Only an actual rewrite makes it the developer's (as in the
			// memory screen, where saving one approves it too: a dot's scope
			// holds only proposals that are imports).
			if !imported || textChanged {
				x.Source = memory.SourceUser
				if x.Status == memory.StatusProposed && x.Scope != memory.ScopeProject {
					x.Status = memory.StatusActive
				}
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if got.Status == memory.StatusArchived && e.Status != memory.StatusArchived {
			fmt.Fprintln(c.out, c.archivedAtOnce("saved", got))
		}
		nu++
	}
	for _, r := range deletes {
		if err := c.store.Forget(pidOf(r.Scope), r, false); err != nil {
			errs = append(errs, err)
			continue
		}
		nd++
	}
	fmt.Fprintf(c.out, "applied: %d added, %d updated, %d forgotten\n", na, nu, nd)
	return errors.Join(errs...)
}

func (c *memCLI) purge(args []string) error {
	f := c.flags("purge")
	if _, err := f.parse(args); err != nil {
		return err
	}
	if f.project == "" {
		return errors.New("purge: --project is required (a path or an id)")
	}
	if !memory.ValidProjectID(f.project) {
		// A mistyped path must not resolve to whatever project holds it.
		p := f.project
		if !filepath.IsAbs(p) && c.cwd != "" {
			p = filepath.Join(c.cwd, p)
		}
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("purge: %s doesn't exist (give an existing path, or the project id from `memory projects`)", f.project)
		}
	}
	pr, err := c.project(f.project)
	if err != nil {
		return err
	}
	if !f.yes {
		if !c.interactive {
			return errors.New("purge: refusing without --yes (not a terminal)")
		}
		if !c.confirm(fmt.Sprintf("Delete ALL memory of project %s (%s)?", pr.Name, pr.ID), pr.ID) {
			return errors.New("purge: not confirmed")
		}
	}
	if err := c.store.Purge(pr.ID); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "purged %s\n", pr.ID)
	return nil
}

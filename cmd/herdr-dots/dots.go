package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dotpack"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/history"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/textutil"
	"herdr-dots/internal/ui/profile"
	"herdr-dots/internal/ui/style"
	"herdr-dots/internal/version"
)

const dotsUsage = `usage: herdr-dots dots <command> [flags]
  list                                         your dots, and this repository's project dots with their trust
  profile SLUG [--rows N]                      what a dot did lately: stats, memory, hints, history
  export SLUG [--with-memory dot|dot+project] [--include-user] [--force] [--project P] [--note TEXT] [-o FILE]
  import FILE|URL [--as NAME] [--keep CAP,...] [--no-memory] [--project P] [--yes]
  trust FILE [--allow CAP,...] [--yes]         trust a .herdr-dots/dots/*.toml file exactly as it is now
  untrust FILE
Imported and project dots start with every risky capability off. CAP is one of
inject, write, mcp, research, web, schedule, commit, base_url.
P is a project path or id (default: the current directory's project).`

// dotsCLI is `herdr-dots dots`: dot packs, project dot trust and dot
// profiles. It works on the files directly, with or without a running engine
// (the engine notices trust changes on its next poll; open the library to see
// an imported dot).
type dotsCLI struct {
	dotsDir     string
	mem         *memory.Store // nil: memory off (imports skip memory, exports can't carry it)
	hist        *history.Store
	trust       *projectdots.TrustStore
	resolve     func(dir string) (memory.Project, error)
	fetch       func(ctx context.Context, url string) ([]byte, string, error)
	cwd         string
	home        string // abbreviates paths in output ("" = don't)
	out, errOut io.Writer
	in          io.Reader
	interactive bool
	now         func() time.Time
}

func runDots(args []string) error {
	p, err := paths.Discover()
	if err != nil {
		return err
	}
	cfg, cerr := config.Load(p.ConfigFile())
	if cerr != nil {
		fmt.Fprintln(os.Stderr, "herdr-dots: config:", cerr, "(using defaults)")
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	st, _ := os.Stdin.Stat()
	client := &http.Client{Timeout: 20 * time.Second}
	c := &dotsCLI{
		dotsDir: p.DotsDir(),
		mem:     memoryStoreFor(p, cfg, nil),
		hist:    history.Open(p.HistoryDir()),
		trust:   &projectdots.TrustStore{Path: p.TrustFile()},
		resolve: resolveProject,
		fetch: func(ctx context.Context, url string) ([]byte, string, error) {
			return dotpack.Fetch(ctx, client, url, dotpack.MaxBytes)
		},
		cwd: cwd, home: home, out: os.Stdout, errOut: os.Stderr, in: os.Stdin,
		interactive: st != nil && st.Mode()&os.ModeCharDevice != 0,
		now:         time.Now,
	}
	return c.run(args)
}

func (c *dotsCLI) run(args []string) (err error) {
	defer func() {
		if errors.Is(err, flag.ErrHelp) { // -h: the flag set printed its usage
			err = nil
		}
	}()
	if len(args) == 0 {
		return errors.New(dotsUsage)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		return c.list()
	case "profile":
		return c.profile(rest)
	case "export":
		return c.export(rest)
	case "import":
		return c.importPack(rest)
	case "trust":
		return c.trustFile(rest)
	case "untrust":
		return c.untrustFile(rest)
	case "help", "-h", "--help":
		fmt.Fprintln(c.out, dotsUsage)
		return nil
	}
	return fmt.Errorf("unknown dots command %q\n%s", cmd, dotsUsage)
}

// parseAnywhere accepts flags before and after positional arguments.
func parseAnywhere(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}

func (c *dotsCLI) flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("dots "+name, flag.ContinueOnError)
	fs.SetOutput(c.errOut)
	return fs
}

// ask returns true when --yes was given or the developer typed y.
func (c *dotsCLI) ask(yes bool, question string) (bool, error) {
	if yes {
		return true, nil
	}
	if !c.interactive {
		return false, errors.New("not confirmed: read the preview above, then rerun with --yes")
	}
	fmt.Fprint(c.out, question+" [y/N]: ")
	line, _ := bufio.NewReader(c.in).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes", nil
}

// projectID resolves --project (an id or a path) or the current directory.
func (c *dotsCLI) projectID(arg string) (memory.Project, error) {
	if memory.ValidProjectID(arg) {
		return memory.Project{ID: arg, Name: arg}, nil
	}
	dir := arg
	if dir == "" {
		dir = c.cwd
	}
	pr, err := c.resolve(dir)
	if pr.ID == "" {
		return pr, fmt.Errorf("can't identify the project for %s: %v", c.show(dir), err)
	}
	return pr, nil
}

// show is how a path appears in output: relative to the current directory
// when it is inside it, ~-abbreviated under the home directory, and always
// free of control and bidi characters.
func (c *dotsCLI) show(p string) string {
	p = textutil.SanitizeModelText(p)
	if !filepath.IsAbs(p) {
		return filepath.ToSlash(p)
	}
	for _, base := range []struct{ dir, prefix string }{{c.cwd, ""}, {c.home, "~"}} {
		if base.dir == "" {
			continue
		}
		if rel, err := filepath.Rel(base.dir, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				rel = ""
			}
			if base.prefix == "" {
				return filepath.ToSlash(rel)
			}
			return base.prefix + "/" + filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(p)
}

func indentBlock(text string) string {
	lines := strings.Split(textutil.SanitizeModelText(strings.TrimSpace(text)), "\n")
	return "    " + strings.Join(lines, "\n    ")
}

// one is text cleaned for a single line of output.
func one(s string) string { return style.Clean(s) }

func (c *dotsCLI) list() error {
	ds, bad := dots.LoadAll(c.dotsDir)
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "YOUR DOTS\t\t\t\t")
	for _, d := range ds {
		origin := ""
		if d.Origin != dots.OriginUser {
			origin = "(" + one(d.Origin) + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s/%s\t%s\t%s\n", d.Slug, one(d.Name), d.Backend.Kind, one(d.Backend.Model), d.Delivery.Mode, origin)
	}
	for _, le := range bad {
		fmt.Fprintf(tw, "  ⚠ %s\t\t\t\t\n", one(le.Error()))
	}
	pr, err := c.resolve(c.cwd)
	if err == nil && pr.Root != "" && dots.ProjectTag(pr.ID) != "" {
		s := projectdots.ScanProject(pr.Root, pr.ID, c.trust)
		if len(s.Items)+len(s.Invalid) > 0 {
			fmt.Fprintf(tw, "PROJECT DOTS (%s)\t\t\t\t\n", one(pr.Name))
		}
		elsewhere := c.trustedElsewhere(pr)
		for _, it := range s.Items {
			trust := "untrusted"
			if it.Trusted {
				trust = "trusted"
				if len(it.Granted) > 0 {
					trust += " with " + strings.Join(it.Granted, ", ")
				}
				if at := elsewhere(it); at != "" {
					trust += ", trusted at " + at
				}
			}
			asks := "asks for nothing risky"
			if len(it.Requested) > 0 {
				asks = "asks for " + strings.Join(it.Requested, ", ")
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", dots.DisplaySlug(it.Dot.Slug), one(it.Dot.Name), trust, asks, one(it.Rel))
		}
		for _, le := range s.Invalid {
			fmt.Fprintf(tw, "  ⚠ %s\t\t\t\t\n", one(le.Error()))
		}
	}
	return tw.Flush()
}

// trustedElsewhere returns a lookup that gives, for a trusted project dot
// whose trust was granted at a different path than this checkout's (clones
// of a repository share a project id), where that was. "" otherwise.
func (c *dotsCLI) trustedElsewhere(pr memory.Project) func(projectdots.Item) string {
	entries, _ := c.trust.List()
	return func(it projectdots.Item) string {
		here := filepath.Join(pr.Root, filepath.FromSlash(it.Rel))
		for _, e := range entries {
			if e.Project == pr.ID && e.Rel == it.Rel && e.SHA256 == projectdots.Hex(it.Sum) && e.Path != "" && !samePath(e.Path, here) {
				return c.show(e.Path)
			}
		}
		return ""
	}
}

func samePath(a, b string) bool {
	if ea, err := filepath.EvalSymlinks(a); err == nil {
		a = ea
	}
	if eb, err := filepath.EvalSymlinks(b); err == nil {
		b = eb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// ---- profile

func (c *dotsCLI) profile(args []string) error {
	fs := c.flagSet("profile")
	rows := fs.Int("rows", 10, "history rows to show")
	pos, err := parseAnywhere(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: herdr-dots dots profile SLUG [--rows N]")
	}
	d, err := c.loadDot(pos[0])
	if err != nil {
		return fmt.Errorf("dot %s: %w", one(pos[0]), err)
	}
	w := c.out
	origin := d.Origin
	if origin == "" {
		origin = dots.OriginUser
	}
	fmt.Fprintf(w, "%s (%s)  %s/%s  %s  origin: %s\n", one(d.Name), d.Slug, d.Backend.Kind, one(d.Backend.Model), d.Delivery.Mode, one(origin))
	if strings.TrimSpace(d.Purpose) != "" {
		fmt.Fprintf(w, "purpose:\n%s\n", indentBlock(d.Purpose))
	}
	now := c.now()
	var month history.Stats
	var share float64
	switch {
	case c.hist == nil:
		fmt.Fprintln(w, "history is off")
	default:
		prof, err := c.hist.Profile(d.Slug, now, max(*rows, 0))
		if err != nil {
			fmt.Fprintln(w, "history unavailable:", one(err.Error()))
			break
		}
		month = prof.Month
		share, _, _ = c.hist.CostShare(d.Slug, now, 30*24*time.Hour)
		fmt.Fprintln(w, "LAST 7 DAYS")
		fmt.Fprintln(w, statsLines(prof.Week))
		fmt.Fprintln(w, "LAST 30 DAYS")
		fmt.Fprintln(w, statsLines(prof.Month))
		if prof.Month.Skipped > 0 {
			fmt.Fprintf(w, "  (%d unreadable history lines skipped)\n", prof.Month.Skipped)
		}
		c.memoryLines(d.Slug)
		if hints := profile.Hints(month, share); len(hints) > 0 {
			fmt.Fprintln(w, "HINTS")
			for _, h := range hints {
				fmt.Fprintln(w, "  -", h)
			}
		}
		fmt.Fprintln(w, "HISTORY")
		if len(prof.Rows) == 0 {
			fmt.Fprintln(w, "  nothing recorded yet")
		}
		for _, r := range prof.Rows {
			res := r.Resolution
			if res == "" && r.Card != "" {
				res = "pending"
			}
			sev := r.Severity
			if sev == "" {
				sev = r.Outcome
			}
			fmt.Fprintf(w, "  %s  %-9s %-9s %-10s %s\n", r.At.Local().Format("01-02 15:04"), one(r.Kind), one(sev), one(res), one(r.Excerpt))
		}
		return nil
	}
	c.memoryLines(d.Slug)
	return nil
}

func statsLines(st history.Stats) string {
	var kinds []string
	for k, n := range st.Runs {
		kinds = append(kinds, fmt.Sprintf("%s %d", one(k), n))
	}
	sort.Strings(kinds)
	line := fmt.Sprintf("  runs %d", st.TotalRuns())
	if len(kinds) > 0 {
		line += " (" + strings.Join(kinds, ", ") + ")"
	}
	line += fmt.Sprintf("  flagged %.0f%%\n  accepted %.0f%% (%d / %d)  dismissed %d  suppressed %d  errors %d\n  cost $%.2f", st.FlaggedRate()*100,
		st.AcceptanceRate()*100, st.Accepted, st.Resolved, st.Dismissed, st.Suppressed, st.Errors, st.CostUSD)
	if st.CostUnknown > 0 {
		line += fmt.Sprintf(" (+%d runs with unknown cost)", st.CostUnknown)
	}
	if st.MedianMs > 0 {
		line += fmt.Sprintf("  median %.1f s", float64(st.MedianMs)/1000)
	}
	return line
}

// memoryLines prints the dot's memory summary across projects, and how many
// proposals (imports included) wait in the current project.
func (c *dotsCLI) memoryLines(slug string) {
	fmt.Fprintln(c.out, "MEMORY")
	if c.mem == nil {
		fmt.Fprintln(c.out, "  memory is turned off")
		return
	}
	sum, err := profile.Summarize(c.mem, slug)
	if err != nil {
		fmt.Fprintln(c.out, "  memory unavailable:", one(err.Error()))
		return
	}
	fmt.Fprintf(c.out, "  %d projects, %d entries, %d pinned", sum.Projects, sum.Entries, sum.Pinned)
	if sum.Partial {
		fmt.Fprint(c.out, " (partial: some memory files could not be read)")
	}
	fmt.Fprintln(c.out)
	if pr, err := c.projectID(""); err == nil {
		if st, _ := c.mem.Stats(pr.ID); st.Proposals > 0 {
			fmt.Fprintf(c.out, "  %d %s waiting for approval in %s (every dot, imports included); see herdr-dots memory approve\n",
				st.Proposals, plural(st.Proposals, "proposal", "proposals"), one(pr.Name))
		}
	}
}

// ---- export

func (c *dotsCLI) export(args []string) error {
	fs := c.flagSet("export")
	with := fs.String("with-memory", "", "include memory: dot or dot+project")
	includeUser := fs.Bool("include-user", false, "also include memory you wrote yourself")
	force := fs.Bool("force", false, "leave out memory entries that look like secrets instead of refusing")
	project := fs.String("project", "", "project path or id for --with-memory")
	note := fs.String("note", "", "a one-line note for whoever imports it")
	outFile := fs.String("o", "", "write to this file (default: stdout)")
	pos, err := parseAnywhere(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: herdr-dots dots export SLUG [flags]")
	}
	d, err := c.loadDot(pos[0])
	if err != nil {
		return fmt.Errorf("dot %s: %w", one(pos[0]), err)
	}
	var mem []memory.Entry
	if *with != "" {
		pr, err := c.projectID(*project)
		if err != nil {
			return err
		}
		if mem, err = dotpack.SelectMemory(c.mem, pr.ID, d.Slug, *with, *includeUser); err != nil {
			return err
		}
	}
	opts := dotpack.ExportOptions{Note: *note, Version: version.Version, Now: c.now()}
	data, err := dotpack.Export(d, mem, withForce(opts, *force))
	if err != nil {
		return exportRefusal(err, *force)
	}
	dropped := 0
	if *force {
		// Force drops what looks like a secret. Ask once more without it
		// for the names (never the values) of what was dropped.
		_, werr := dotpack.Export(d, mem, opts)
		var se *dotpack.SecretError
		if errors.As(werr, &se) {
			dropped = len(se.Where)
			fmt.Fprintf(c.errOut, "--force left out %d memory %s (%s); they looked like secrets\n", dropped, plural(dropped, "entry", "entries"), strings.Join(se.Where, ", "))
		}
	}
	if *outFile == "" {
		_, err := c.out.Write(data)
		return err
	}
	f, err := os.OpenFile(*outFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s exists: choose another file name", c.show(*outFile))
	}
	if err != nil {
		return fmt.Errorf("can't write %s: %w", c.show(*outFile), errors.Unwrap(err))
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(*outFile)
		return fmt.Errorf("can't write %s: %w", c.show(*outFile), werr)
	}
	fmt.Fprintf(c.errOut, "exported %s with %d memory %s to %s\n", one(d.Name), len(mem)-dropped, plural(len(mem)-dropped, "entry", "entries"), c.show(*outFile))
	return nil
}

func withForce(o dotpack.ExportOptions, force bool) dotpack.ExportOptions {
	o.Force = force
	return o
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// exportRefusal explains a refused export: which parts, never their values.
func exportRefusal(err error, forced bool) error {
	var se *dotpack.SecretError
	if !errors.As(err, &se) {
		return err
	}
	var other []string
	for _, w := range se.Where {
		if !strings.HasPrefix(w, "memory ") {
			other = append(other, w)
		}
	}
	switch {
	case len(other) > 0:
		return fmt.Errorf("%w: remove it from the dot first (--force only leaves out memory entries, never the dot's own text); the values are not shown", err)
	case forced:
		return err
	}
	return fmt.Errorf("%w: check those entries, drop --with-memory, or rerun with --force to leave them out; the values are not shown", err)
}

// ---- import

// shownSource is where a pack came from, without credentials or a query.
func (c *dotsCLI) shownSource(src string) string {
	if u, err := url.Parse(src); err == nil && u.Host != "" {
		u.User, u.Fragment = nil, ""
		if u.RawQuery != "" {
			u.RawQuery = "…"
		}
		return textutil.SanitizeModelText(strings.Replace(u.String(), "%E2%80%A6", "…", 1))
	}
	return c.show(src)
}

func (c *dotsCLI) readPack(src string) ([]byte, string, error) {
	if strings.Contains(src, "://") {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, final, err := c.fetch(ctx, src)
		if final == "" {
			final = src
		}
		return data, c.shownSource(final), err
	}
	abs, _ := filepath.Abs(src)
	notRegular := fmt.Errorf("%s is not a regular file", c.show(abs))
	// Stat before opening: opening a FIFO would block.
	st, err := os.Stat(src)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", c.show(src), errors.Unwrap(err))
	}
	if !st.Mode().IsRegular() {
		return nil, "", notRegular
	}
	f, err := os.Open(src)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", c.show(src), errors.Unwrap(err))
	}
	defer f.Close()
	if st, err = f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, "", notRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, dotpack.MaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", c.show(src), err)
	}
	if len(data) > dotpack.MaxBytes {
		return nil, "", dotpack.ErrTooLarge
	}
	return data, c.show(abs), nil
}

// loadDot loads one of your dots by slug. The slug names a file, so
// anything that isn't a plain slug is refused before any path is built.
func (c *dotsCLI) loadDot(slug string) (dots.Dot, error) {
	if !memory.ValidSlug(slug) {
		return dots.Dot{}, fmt.Errorf("dot %q is not a valid dot name (lowercase letters, digits and dashes)", one(slug))
	}
	return dots.Load(c.dotsDir, slug)
}

func (c *dotsCLI) importPack(args []string) error {
	fs := c.flagSet("import")
	as := fs.String("as", "", "save under this name (needed when the name is taken)")
	keepCSV := fs.String("keep", "", "capabilities to keep, comma-separated (default: none)")
	noMemory := fs.Bool("no-memory", false, "don't import the pack's memory")
	project := fs.String("project", "", "project path or id for the pack's memory")
	yes := fs.Bool("yes", false, "import without asking (after reading the preview)")
	pos, err := parseAnywhere(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: herdr-dots dots import FILE|URL [flags]")
	}
	data, from, err := c.readPack(pos[0])
	if err != nil {
		return err
	}
	p, err := dotpack.Parse(data)
	if err != nil {
		return err
	}
	if *as != "" {
		p.Dot.Name = *as
	}
	keep := splitCSV(*keepCSV)
	d, clamps, err := dotpack.Prepare(p, keep)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(c.dotsDir, d.Slug+".toml")); err == nil {
		return fmt.Errorf("a dot named %q already exists: rerun with --as \"<new name>\" (nothing is overwritten)", one(d.Name))
	}
	var pr memory.Project
	memNote := ""
	switch {
	case len(p.Memory) == 0:
	case *noMemory:
		memNote = "not imported (--no-memory)"
	case c.mem == nil:
		memNote = "not imported (memory is off)"
	default:
		if pr, err = c.projectID(*project); err != nil {
			memNote = "not imported (" + one(err.Error()) + ")"
		}
	}
	c.preview(p, d, from, clamps, keep, pr, memNote)
	ok, err := c.ask(*yes, "Import "+one(d.Name)+"?")
	if err != nil || !ok {
		if err == nil {
			err = errors.New("not imported")
		}
		return err
	}
	if err := dots.Save(c.dotsDir, d, true); err != nil {
		if errors.Is(err, dots.ErrExists) {
			return fmt.Errorf("a dot named %q already exists: rerun with --as \"<new name>\"", one(d.Name))
		}
		return err
	}
	fmt.Fprintf(c.out, "imported %s as %s; open the library (prefix+d) to attach it\n", one(d.Name), d.Slug)
	if memNote == "" && len(p.Memory) > 0 {
		res, err := c.mem.ImportPack(pr.ID, d.Slug, p.Memory)
		fmt.Fprintf(c.out, "memory: %d added, %d merged into existing entries; they wait for your approval (herdr-dots memory approve)\n", res.Added, res.Updated)
		for _, why := range res.Rejected {
			fmt.Fprintln(c.errOut, "memory: skipped", one(why))
		}
		if err != nil {
			return fmt.Errorf("memory import: %w", err)
		}
	}
	return nil
}

// preview prints everything the import will do, in full: what is saved, what
// reaches a prompt, what was turned off, and every memory entry.
func (c *dotsCLI) preview(p dotpack.Pack, d dots.Dot, from string, clamps []dots.Clamp, keep []string, pr memory.Project, memNote string) {
	w := c.out
	fmt.Fprintf(w, "IMPORT %s from %s\n", one(d.Name), from)
	if p.Meta.Note != "" {
		fmt.Fprintf(w, "note: %s\n", one(p.Meta.Note))
	}
	exported := "an unknown date"
	if !p.Meta.ExportedAt.IsZero() {
		exported = p.Meta.ExportedAt.Format("2006-01-02")
	}
	fmt.Fprintf(w, "exported %s by herdr-dots %s\n", exported, one(p.Meta.Version))
	model := d.Backend.Model
	if model == "" {
		model = "(default)"
	}
	budget := fmt.Sprintf("%d calls per session", d.Budget.MaxCallsPerSession)
	if d.Budget.MaxUSDPerSession > 0 {
		budget += fmt.Sprintf(", $%g per session", d.Budget.MaxUSDPerSession)
	}
	fmt.Fprintf(w, "saves as: %s (%s)\nbackend:  %s, model %s\nbudget:   %s\nmode:     %s\npurpose (becomes this dot's system prompt):\n%s\n",
		one(d.Name), d.Slug, d.Backend.Kind, one(model), budget, d.Delivery.Mode, indentBlock(d.Purpose))
	if len(d.Backend.Args) > 0 {
		fmt.Fprintf(w, "args:     %s\n", one(strings.Join(d.Backend.Args, " ")))
	}
	if d.Backend.Effort != "" {
		fmt.Fprintf(w, "effort:   %s\n", one(d.Backend.Effort))
	}
	// Phase 8 text that reaches a prompt once research or a schedule is kept.
	if len(d.Research.Topics) > 0 {
		fmt.Fprintf(w, "research topics:\n%s\n", indentBlock(strings.Join(d.Research.Topics, "\n")))
	}
	if strings.TrimSpace(d.Schedule.Task) != "" {
		fmt.Fprintf(w, "schedule task:\n%s\n", indentBlock(d.Schedule.Task))
	}
	if len(keep) > 0 {
		fmt.Fprintf(w, "kept (you asked): %s\n", one(strings.Join(keep, ", ")))
	} else {
		fmt.Fprintln(w, "kept: nothing (every risky capability stays off unless you pass --keep CAP)")
	}
	var changes []dots.Clamp
	for _, cl := range clamps {
		if !cl.Fact {
			changes = append(changes, cl)
		}
	}
	if len(changes) > 0 {
		fmt.Fprintln(w, "turned off:")
		for _, cl := range changes {
			hint := "  (always changed)"
			if cl.Keepable {
				hint = "  (keep with --keep " + cl.Capability + ")"
			}
			fmt.Fprintf(w, "  - %s%s\n", one(cl.String()), hint)
		}
	}
	if len(p.Memory) > 0 {
		where := memNote
		if where == "" {
			where = "into " + one(pr.Name) + "; every entry arrives as a proposal for you to approve"
		}
		fmt.Fprintf(w, "memory: %d entries, %s\n", len(p.Memory), where)
		for i, e := range p.Memory {
			fmt.Fprintf(w, "  %d. [%s %s] %s\n", i+1, e.Scope, e.Kind, strings.TrimLeft(indentBlock(e.Text), " "))
			if len(e.Paths) > 0 {
				fmt.Fprintf(w, "     paths: %s\n", one(strings.Join(e.Paths, ", ")))
			}
		}
	}
}

// ---- project dot trust

// projectFile finds a project dot file and its repository.
func (c *dotsCLI) projectFile(path string) (memory.Project, dots.ProjectFile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return memory.Project{}, dots.ProjectFile{}, err
	}
	pr, err := c.resolve(filepath.Dir(abs))
	if pr.Root == "" || !strings.HasPrefix(pr.ID, "g-") {
		return pr, dots.ProjectFile{}, fmt.Errorf("%s is not in a git repository with commits (%v)", c.show(abs), err)
	}
	rel, err := filepath.Rel(pr.Root, abs)
	if err != nil || filepath.Dir(rel) != dots.ProjectDir {
		return pr, dots.ProjectFile{}, fmt.Errorf("%s is not in %s of this repository", c.show(abs), filepath.ToSlash(dots.ProjectDir))
	}
	files, bad := dots.ReadProjectFiles(pr.Root)
	want := filepath.ToSlash(rel)
	for _, f := range files {
		if f.Rel == want {
			return pr, f, nil
		}
	}
	for _, le := range bad {
		if le.File == want {
			return pr, dots.ProjectFile{}, le
		}
	}
	return pr, dots.ProjectFile{}, fmt.Errorf("%s: no such project dot", c.show(abs))
}

func (c *dotsCLI) trustFile(args []string) error {
	fs := c.flagSet("trust")
	allowCSV := fs.String("allow", "", "capabilities to grant, comma-separated (each must be one the file asks for)")
	yes := fs.Bool("yes", false, "trust without asking (after reading the preview)")
	pos, err := parseAnywhere(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: herdr-dots dots trust FILE [--allow CAP,...] [--yes]")
	}
	pr, f, err := c.projectFile(pos[0])
	if err != nil {
		return err
	}
	d, err := dots.DecodeProject(f, pr.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Rel, err)
	}
	allow := splitCSV(*allowCSV)
	if err := dots.CheckCapabilities(allow); err != nil {
		return err
	}
	requested := dots.Requested(d)
	for _, a := range allow {
		if !slices.Contains(requested, a) {
			return fmt.Errorf("%s doesn't ask for %s", f.Rel, a)
		}
	}
	// Trust binds exactly the bytes previewed: the hash is of f.Data, which
	// is also what was parsed and what is shown below.
	sum := projectdots.Sum(f.Data)
	_, clamps := dots.ClampDot(d, allow)
	// The whole file, not just the purpose: research topics and a schedule
	// task (Phase 8) reach prompts too, and trust covers every byte.
	fmt.Fprintf(c.out, "TRUST %s in %s\nsha256:  %s\nname:    %s (%s)\nbackend: %s/%s\nthe file, exactly as it will be trusted:\n%s\n",
		one(f.Rel), one(pr.Name), projectdots.Hex(sum), one(d.Name), dots.DisplaySlug(d.Slug), d.Backend.Kind, one(d.Backend.Model), indentBlock(string(f.Data)))
	if at := c.trustedElsewhere(pr)(projectdots.Item{Rel: f.Rel, Sum: sum}); at != "" {
		fmt.Fprintf(c.out, "this exact file is already trusted at %s (clones of a repository share trust)\n", at)
	}
	if len(requested) > 0 {
		fmt.Fprintf(c.out, "asks for: %s\n", strings.Join(requested, ", "))
	}
	if len(allow) > 0 {
		fmt.Fprintf(c.out, "granting: %s\n", strings.Join(allow, ", "))
	}
	for _, cl := range clamps {
		if !cl.Fact {
			fmt.Fprintf(c.out, "stays off: %s\n", one(cl.String()))
		}
	}
	fmt.Fprintln(c.out, "Any change to this file makes it untrusted again.")
	ok, err := c.ask(*yes, "Trust it?")
	if err != nil || !ok {
		if err == nil {
			err = errors.New("not trusted")
		}
		return err
	}
	// The file must still be what was shown.
	if _, again, err := c.projectFile(pos[0]); err != nil || projectdots.Sum(again.Data) != sum {
		return errors.New("the file changed while you were reading it: nothing was trusted; run trust again")
	}
	if err := c.trust.Trust(pr.ID, f.Rel, f.Path, sum, allow); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "trusted %s; a running engine picks it up within seconds\n", dots.DisplaySlug(d.Slug))
	return nil
}

func (c *dotsCLI) untrustFile(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: herdr-dots dots untrust FILE")
	}
	abs, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	pr, err := c.resolve(filepath.Dir(abs))
	if pr.Root == "" {
		return fmt.Errorf("%s is not in a git repository (%v)", c.show(abs), err)
	}
	rel, err := filepath.Rel(pr.Root, abs)
	if err != nil {
		return err
	}
	found, err := c.trust.Revoke(pr.ID, filepath.ToSlash(rel))
	if err != nil {
		return err
	}
	if !found {
		fmt.Fprintf(c.out, "%s was not trusted\n", c.show(abs))
		return nil
	}
	fmt.Fprintf(c.out, "untrusted %s; attached copies pause\n", c.show(abs))
	return nil
}

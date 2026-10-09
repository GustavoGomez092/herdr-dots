// Package dotpack exports a dot (and optionally some of its memory) as one
// .dotpack.toml file and reads such files back, from disk or an https URL.
// Packs never carry credentials or history. Everything in an imported pack is
// someone else's work: Prepare clamps every risky capability (dots.ClampDot)
// and memory lands through memory.Store.ImportPack.
package dotpack

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/secrets"
	"herdr-dots/internal/textutil"
)

// Format is the pack format this version writes and reads.
const Format = 1

// MaxBytes caps a pack file or download.
const MaxBytes = 256 << 10

// Ext is the conventional file extension.
const Ext = ".dotpack.toml"

// Limits on one memory entry in a pack.
const (
	maxMemoryRunes     = 500
	maxMemoryPaths     = 10
	maxMemoryPathRunes = 256
	maxNoteRunes       = 2000
	maxVersionRunes    = 64
	maxMemoryErrors    = 5 // memory problems Parse lists; the rest are counted
)

// metaProblem is why the note or version can't travel, or nil.
func metaProblem(name, text string, max int) error {
	if utf8.RuneCountInString(text) > max {
		return fmt.Errorf("%s is over %d characters", name, max)
	}
	if err := dots.UnsafeText(text, false); err != nil {
		return fmt.Errorf("%s %w", name, err)
	}
	return nil
}

// formatErr is a decode or scan error from an untrusted file, escaped like
// dots.CleanText (the library echoes input) and capped.
func formatErr(err error) error {
	return fmt.Errorf("%w: %s", ErrFormat, dots.CleanText(err.Error(), 300))
}

var (
	ErrFormat         = errors.New("not a dot pack this herdr-dots can read")
	ErrTooLarge       = fmt.Errorf("dot pack is larger than %d KB", MaxBytes>>10)
	ErrNotHTTPS       = errors.New("dot packs are fetched over https:// only")
	ErrContentType    = errors.New("the server did not send text")
	ErrSecretInExport = errors.New("the export looks like it contains a secret")
)

// SecretError names what looked like a secret ("purpose", "memory 2", …),
// never the text.
type SecretError struct{ Where []string }

func (e *SecretError) Error() string {
	return ErrSecretInExport.Error() + " (" + strings.Join(e.Where, ", ") + ")"
}

func (e *SecretError) Unwrap() error { return ErrSecretInExport }

// Meta is the [pack] table.
type Meta struct {
	Format     int       `toml:"format"`
	ExportedAt time.Time `toml:"exported_at"`
	Version    string    `toml:"herdr_dots_version"`
	Note       string    `toml:"note,omitempty"`
}

// MemoryItem is one [[memory]] block: only what travels.
type MemoryItem struct {
	Scope string   `toml:"scope"`
	Kind  string   `toml:"kind"`
	Text  string   `toml:"text"`
	Paths []string `toml:"paths"`
}

// Pack is a parsed pack. Dot is as written (not clamped or validated) except
// that its origin is always import; Memory entries carry Scope, Kind, Text
// and Paths only.
type Pack struct {
	Meta   Meta
	Dot    dots.Dot
	Memory []memory.Entry
}

type fileDoc struct {
	Pack   Meta         `toml:"pack"`
	Dot    dots.Dot     `toml:"dot"`
	Memory []MemoryItem `toml:"memory"`
}

// ExportOptions tune Export.
type ExportOptions struct {
	Note    string
	Version string    // herdr_dots_version
	Now     time.Time // zero = time.Now
	// Force exports the pack without the memory entries that look like they
	// hold a secret instead of refusing it. Such an entry is never written,
	// and a secret in the dot, the note or the version is refused anyway.
	Force bool
}

const header = "# herdr-dots dot pack. Import it with: herdr-dots dots import <file>\n" +
	"# Imported dots start with inject, write, MCP, research, web, schedules, commit\n" +
	"# triggers and custom endpoints turned off until you keep each one.\n"

func looksSecret(text string) bool {
	found, _ := secrets.Scan(text)
	return found
}

// looksSecretText is looksSecret for free text that travels beside the dot
// (note, version, memory), with the checks dots.Encode and ImportPack apply:
// the text as written and sanitized (a control byte must not split a key
// from the scanner and then vanish), and the shapes credentials.Redact
// scrubs.
func looksSecretText(text string) bool {
	clean := textutil.SanitizeModelText(text)
	return looksSecret(text) || looksSecret(clean) || credentials.Redact(text) != text || credentials.Redact(clean) != clean
}

// Export renders d and mem as a pack. mem should already be the entries the
// developer chose (SelectMemory): active, dot or project scope, an ImportKind,
// dot scope only for d itself, at most memory.MaxPendingImports per scope;
// anything else is refused. Memory text is sanitized
// (textutil.SanitizeModelText), and absolute memory paths and schedule
// projects (places on the author's machine) are left out. It refuses with a
// *SecretError when the dot, the note, the version or any entry looks like
// it holds a secret (with Force, flagged entries are left out instead), and
// scans the whole rendered pack once more before returning it.
func Export(d dots.Dot, mem []memory.Entry, o ExportOptions) ([]byte, error) {
	slug := d.Slug
	if slug == "" {
		slug = dots.Slugify(d.Name)
	}
	// Mirror ClampDot: only "attached" travels.
	d.Schedule.Projects = slices.DeleteFunc(slices.Clone(d.Schedule.Projects), func(p string) bool { return p != dots.ScheduleAttached })
	vd := d
	vd.Slug = slug
	if err := vd.Validate(); err != nil {
		return nil, fmt.Errorf("the dot is not valid: %w", err)
	}
	meta := []struct {
		name, text string
		max        int
	}{{"note", o.Note, maxNoteRunes}, {"version", o.Version, maxVersionRunes}}
	for _, f := range meta {
		if err := metaProblem(f.name, f.text, f.max); err != nil {
			return nil, err
		}
	}
	items, err := travelling(slug, mem)
	if err != nil {
		return nil, err
	}
	var where []string // always refused
	scan := func(label, text string) {
		if looksSecret(text) {
			where = append(where, label)
		}
	}
	scan("name", d.Name)
	scan("purpose", d.Purpose)
	scan("backend", strings.Join(d.Backend.Args, " ")+" "+d.Backend.BaseURL)
	scan("research topics", strings.Join(d.Research.Topics, " ")) // Phase 8 free text travels too
	scan("schedule task", d.Schedule.Task)
	if len(where) == 0 {
		// Every other string of the dot, as dots.Encode checks it (by
		// reflection, so a field added later is covered).
		if _, err := dots.Encode(d); err != nil {
			var de *dots.SecretError
			if !errors.As(err, &de) {
				return nil, err
			}
			where = append(where, de.Field)
		}
	}
	for _, f := range meta {
		if looksSecretText(f.text) {
			where = append(where, f.name)
		}
	}
	var memWhere []string // refused unless Force, which leaves them out
	kept := make([]MemoryItem, 0, len(items))
	for i, e := range mem {
		// The entry as given, absolute paths included: a secret in what is
		// left out still means the developer should look.
		if looksSecretText(e.Text + " " + strings.Join(e.Paths, " ")) {
			memWhere = append(memWhere, fmt.Sprintf("memory %d", i+1))
			continue
		}
		kept = append(kept, items[i])
	}
	if len(where) > 0 || (len(memWhere) > 0 && !o.Force) {
		return nil, &SecretError{Where: append(where, memWhere...)}
	}
	out, err := render(d, kept, o)
	if err != nil {
		return nil, err
	}
	// Defence in depth (P9-R6): whatever the field checks missed, the file
	// itself must pass the scanner.
	if looksSecret(string(out)) {
		return nil, &SecretError{Where: []string{"pack"}}
	}
	return out, nil
}

// travelling returns mem as pack items: text sanitized, absolute paths left
// out. It refuses memory a pack must never carry (proposals and archived
// entries, global scope, personal kinds such as preferences, another dot's
// entries, more than memory.MaxPendingImports per scope) and anything Parse
// would refuse.
func travelling(slug string, mem []memory.Entry) ([]MemoryItem, error) {
	items := make([]MemoryItem, 0, len(mem))
	perScope := map[memory.Scope]int{}
	for i, e := range mem {
		text := strings.TrimSpace(textutil.SanitizeModelText(e.Text))
		paths := []string{}
		for _, p := range e.Paths {
			if p = strings.TrimSpace(textutil.SanitizeModelText(p)); p != "" && !absPath(p) {
				paths = append(paths, p)
			}
		}
		perScope[e.Scope]++
		why := ""
		switch {
		case e.Status != "" && e.Status != memory.StatusActive:
			why = "only active memory travels"
		case e.Scope != memory.ScopeDot && e.Scope != memory.ScopeProject:
			why = "only dot and project memory travels"
		case !slices.Contains(memory.ImportKinds, e.Kind):
			why = fmt.Sprintf("kind must be one of %v", memory.ImportKinds)
		case e.Scope == memory.ScopeDot && e.Dot != "" && e.Dot != slug:
			why = "it belongs to another dot"
		default:
			why = memoryProblem(text, paths)
		}
		if why != "" {
			return nil, fmt.Errorf("memory %d can't be exported: %s", i+1, why)
		}
		items = append(items, MemoryItem{Scope: string(e.Scope), Kind: string(e.Kind), Text: text, Paths: paths})
	}
	if err := scopeCounts(perScope); err != nil {
		return nil, fmt.Errorf("memory can't be exported: %w", err)
	}
	return items, nil
}

// memoryProblem is why a pack can't carry an entry's text and paths, or ""
// when it can. It never quotes them.
func memoryProblem(text string, paths []string) string {
	switch {
	case text == "":
		return "text is empty"
	case utf8.RuneCountInString(text) > maxMemoryRunes:
		return fmt.Sprintf("text is over %d characters", maxMemoryRunes)
	case dots.UnsafeText(text, true) != nil:
		return "text " + dots.ErrUnsafeText.Error()
	case len(paths) > maxMemoryPaths:
		return fmt.Sprintf("more than %d paths", maxMemoryPaths)
	}
	for j, p := range paths {
		switch {
		case utf8.RuneCountInString(p) > maxMemoryPathRunes:
			return fmt.Sprintf("path %d is over %d characters", j+1, maxMemoryPathRunes)
		case dots.UnsafeText(p, false) != nil:
			return fmt.Sprintf("path %d %s", j+1, dots.ErrUnsafeText)
		case absPath(p):
			return fmt.Sprintf("path %d is absolute (packs carry repository-relative paths only)", j+1)
		}
	}
	return ""
}

// scopeCounts refuses more than memory.MaxPendingImports entries in a scope:
// ImportPack would refuse the rest anyway.
func scopeCounts(n map[memory.Scope]int) error {
	for _, sc := range []memory.Scope{memory.ScopeDot, memory.ScopeProject} {
		if n[sc] > memory.MaxPendingImports {
			return fmt.Errorf("%d %s-scope entries (at most %d per scope)", n[sc], sc, memory.MaxPendingImports)
		}
	}
	return nil
}

// absPath reports a path that names a place on someone's machine rather than
// in the repository: /x (and //server), ~x, \x and \\server, or a drive
// letter (C:).
func absPath(p string) bool {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || strings.HasPrefix(p, `\`) {
		return true
	}
	return len(p) >= 2 && p[1] == ':' && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}

func render(d dots.Dot, items []MemoryItem, o ExportOptions) ([]byte, error) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(struct {
		Pack Meta `toml:"pack"`
	}{Meta{Format: Format, ExportedAt: now.UTC().Truncate(time.Second), Version: o.Version, Note: o.Note}}); err != nil {
		return nil, err
	}
	buf.WriteString("\n")
	d.Slug, d.Origin, d.SourcePath = "", "", ""
	if d.Backend.Args == nil {
		d.Backend.Args = []string{}
	}
	if d.Trigger.FileGlobs == nil {
		d.Trigger.FileGlobs = []string{}
	}
	purpose := d.Purpose
	d.Purpose = ""
	var dotBuf bytes.Buffer
	denc := toml.NewEncoder(&dotBuf)
	denc.Indent = ""
	if err := denc.Encode(struct {
		Dot dots.Dot `toml:"dot"`
	}{d}); err != nil {
		return nil, err
	}
	buf.WriteString(insertBeforeSubtable(dotBuf.String(), dots.PurposeBlock(purpose)))
	if len(items) > 0 {
		buf.WriteString("\n")
		menc := toml.NewEncoder(&buf)
		menc.Indent = ""
		if err := menc.Encode(struct {
			Memory []MemoryItem `toml:"memory"`
		}{items}); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// insertBeforeSubtable puts block after [dot]'s own keys: before the first
// "[dot." header the encoder wrote (no string value of its output spans
// lines, so no other line starts with "[").
func insertBeforeSubtable(doc, block string) string {
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[dot.") {
			return strings.Join(append(append(slices.Clone(lines[:i]), block), lines[i:]...), "\n")
		}
	}
	return strings.TrimRight(doc, "\n") + "\n" + block + "\n"
}

// Parse reads a pack. It refuses oversized files, nesting and key paths no
// pack needs (dots.PreScanTOML), other formats (checked first, so a newer pack says
// so), keys this version doesn't know (a capability it doesn't know can't be
// clamped), unsafe text in the note, version and memory, and memory it
// wouldn't import. The dot is returned as written, except that its origin is
// import whatever the file says (P9-R7).
func Parse(data []byte) (Pack, error) {
	if len(data) > MaxBytes {
		return Pack{}, ErrTooLarge
	}
	text := string(data)
	if err := dots.PreScanTOML(data); err != nil {
		return Pack{}, formatErr(err)
	}
	var probe struct {
		Pack struct {
			Format int `toml:"format"`
		} `toml:"pack"`
	}
	if _, err := toml.Decode(text, &probe); err != nil {
		return Pack{}, formatErr(err)
	}
	if probe.Pack.Format != Format {
		return Pack{}, fmt.Errorf("%w: format %d (this version reads format %d)", ErrFormat, probe.Pack.Format, Format)
	}
	doc := fileDoc{Dot: dots.New("")}
	md, err := toml.Decode(text, &doc)
	if err != nil {
		return Pack{}, formatErr(err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return Pack{}, fmt.Errorf("%w: unknown keys %s (exported by a newer herdr-dots?)", ErrFormat, dots.UnknownKeysText(keys))
	}
	var errs []error
	for _, f := range []struct {
		name, text string
		max        int
	}{{"note", doc.Pack.Note, maxNoteRunes}, {"herdr_dots_version", doc.Pack.Version, maxVersionRunes}} {
		if err := metaProblem(f.name, f.text, f.max); err != nil {
			errs = append(errs, fmt.Errorf("pack %w", err))
		}
	}
	p := Pack{Meta: doc.Pack, Dot: doc.Dot}
	p.Dot.Slug, p.Dot.Origin, p.Dot.SourcePath = dots.Slugify(p.Dot.Name), dots.OriginImport, ""
	perScope := map[memory.Scope]int{}
	badMemory := 0
	for i, it := range doc.Memory {
		e := memory.Entry{Scope: memory.Scope(strings.TrimSpace(it.Scope)), Kind: memory.Kind(strings.TrimSpace(it.Kind)),
			Text: strings.TrimSpace(strings.ReplaceAll(it.Text, "\r\n", "\n")), Paths: it.Paths}
		perScope[e.Scope]++
		why := ""
		switch {
		case e.Scope != memory.ScopeDot && e.Scope != memory.ScopeProject:
			why = "scope must be dot or project"
		case !slices.Contains(memory.ImportKinds, e.Kind):
			why = fmt.Sprintf("kind must be one of %v", memory.ImportKinds)
		default:
			why = memoryProblem(e.Text, e.Paths)
		}
		if why != "" {
			if badMemory++; badMemory <= maxMemoryErrors {
				errs = append(errs, fmt.Errorf("memory %d: %s", i+1, why))
			}
			continue
		}
		p.Memory = append(p.Memory, e)
	}
	if badMemory > maxMemoryErrors {
		errs = append(errs, fmt.Errorf("and %d more memory problems", badMemory-maxMemoryErrors))
	}
	if err := scopeCounts(perScope); err != nil {
		errs = append(errs, fmt.Errorf("memory: %w", err))
	}
	if len(errs) > 0 {
		return Pack{}, fmt.Errorf("%w: %w", ErrFormat, errors.Join(errs...))
	}
	return p, nil
}

// Prepare turns a parsed pack's dot into the dot to save: every risky
// capability clamped except keep, origin "import", slug from the name, and
// validated in full. It returns the clamps it applied, ending with the
// preview facts (backend kind and effective model). Saving it must not
// replace an existing dot: callers save with dots.Save(create=true), which
// refuses a slug that exists.
func Prepare(p Pack, keep []string) (dots.Dot, []dots.Clamp, error) {
	if err := dots.CheckCapabilities(keep); err != nil {
		return dots.Dot{}, nil, err
	}
	d, cs := dots.ClampDot(p.Dot, keep)
	d.Origin, d.Slug, d.SourcePath = dots.OriginImport, dots.Slugify(d.Name), ""
	if err := d.Validate(); err != nil {
		return dots.Dot{}, cs, err
	}
	return d, cs, nil
}

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

	"herdr-dots/internal/dots"
	"herdr-dots/internal/memory"
	"herdr-dots/internal/secrets"
)

// Format is the pack format this version writes and reads.
const Format = 1

// MaxBytes caps a pack file or download.
const MaxBytes = 256 << 10

// Ext is the conventional file extension.
const Ext = ".dotpack.toml"

// maxMemoryRunes caps one memory entry's text in a pack.
const maxMemoryRunes = 500

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

// Export renders d and mem as a pack. mem should already be the entries the
// developer chose (SelectMemory): active, dot or project scope, an ImportKind,
// and dot scope only for d itself; anything else is refused. It refuses with
// a *SecretError when the dot, the note or any entry looks like it holds a
// secret (with Force, flagged entries are left out instead), and scans the
// whole rendered pack once more before returning it.
func Export(d dots.Dot, mem []memory.Entry, o ExportOptions) ([]byte, error) {
	slug := d.Slug
	if slug == "" {
		slug = dots.Slugify(d.Name)
	}
	if err := checkTravels(slug, mem); err != nil {
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
			field, _, ok := strings.Cut(err.Error(), " looks like it holds a secret")
			if !ok {
				return nil, err
			}
			where = append(where, field)
		}
	}
	scan("note", o.Note)
	scan("version", o.Version)
	var memWhere []string // refused unless Force, which leaves them out
	items := make([]MemoryItem, 0, len(mem))
	for i, e := range mem {
		if found, _ := memory.ScanSecrets(e.Text + " " + strings.Join(e.Paths, " ")); found {
			memWhere = append(memWhere, fmt.Sprintf("memory %d", i+1))
			continue
		}
		paths := e.Paths
		if paths == nil {
			paths = []string{}
		}
		items = append(items, MemoryItem{Scope: string(e.Scope), Kind: string(e.Kind), Text: e.Text, Paths: paths})
	}
	if len(where) > 0 || (len(memWhere) > 0 && !o.Force) {
		return nil, &SecretError{Where: append(where, memWhere...)}
	}
	out, err := render(d, items, o)
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

// checkTravels refuses memory a pack must never carry: proposals and archived
// entries, global scope, personal kinds (preferences), another dot's entries,
// and text Parse would refuse.
func checkTravels(slug string, mem []memory.Entry) error {
	for i, e := range mem {
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
		case strings.TrimSpace(e.Text) == "":
			why = "text is empty"
		case utf8.RuneCountInString(strings.TrimSpace(e.Text)) > maxMemoryRunes:
			why = fmt.Sprintf("text is over %d characters", maxMemoryRunes)
		}
		if why != "" {
			return fmt.Errorf("memory %d can't be exported: %s", i+1, why)
		}
	}
	return nil
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

// Parse reads a pack. It refuses oversized files, other formats, keys this
// version doesn't know (a capability it doesn't know can't be clamped) and
// memory it wouldn't import. The dot is returned as written, except that its
// origin is import whatever the file says (P9-R7).
func Parse(data []byte) (Pack, error) {
	if len(data) > MaxBytes {
		return Pack{}, ErrTooLarge
	}
	doc := fileDoc{Dot: dots.New("")}
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return Pack{}, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return Pack{}, fmt.Errorf("%w: unknown keys %s (exported by a newer herdr-dots?)", ErrFormat, strings.Join(keys, ", "))
	}
	if doc.Pack.Format != Format {
		return Pack{}, fmt.Errorf("%w: format %d (this version reads format %d)", ErrFormat, doc.Pack.Format, Format)
	}
	var errs []error
	p := Pack{Meta: doc.Pack, Dot: doc.Dot}
	p.Dot.Slug, p.Dot.Origin, p.Dot.SourcePath = dots.Slugify(p.Dot.Name), dots.OriginImport, ""
	for i, it := range doc.Memory {
		e := memory.Entry{Scope: memory.Scope(strings.TrimSpace(it.Scope)), Kind: memory.Kind(strings.TrimSpace(it.Kind)),
			Text: strings.TrimSpace(it.Text), Paths: it.Paths}
		switch {
		case e.Scope != memory.ScopeDot && e.Scope != memory.ScopeProject:
			errs = append(errs, fmt.Errorf("memory %d: scope must be dot or project", i+1))
		case !slices.Contains(memory.ImportKinds, e.Kind):
			errs = append(errs, fmt.Errorf("memory %d: kind must be one of %v", i+1, memory.ImportKinds))
		case e.Text == "":
			errs = append(errs, fmt.Errorf("memory %d: text is empty", i+1))
		case utf8.RuneCountInString(e.Text) > maxMemoryRunes:
			errs = append(errs, fmt.Errorf("memory %d: text is over %d characters", i+1, maxMemoryRunes))
		default:
			p.Memory = append(p.Memory, e)
		}
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

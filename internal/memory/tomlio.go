package memory

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/dots"
)

const exportHeader = `# herdr-dots memory. Edit text, kind, paths, pinned or status (active, proposed, archived).
# Delete an [[entry]] block to forget it. Add a block without "ref" to create an entry.
# Leave "ref", "scope" and "dot" alone on existing entries.
`

var dotSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

type tomlEntry struct {
	Ref    string   `toml:"ref,omitempty"`
	Scope  string   `toml:"scope"`
	Dot    string   `toml:"dot,omitempty"`
	Kind   string   `toml:"kind"`
	Status string   `toml:"status"`
	Source string   `toml:"source,omitempty"`
	Pinned bool     `toml:"pinned"`
	Paths  []string `toml:"paths"`
	Text   string   `toml:"text"`
	From   string   `toml:"from,omitempty"` // the dot pack an imported entry came from (Phase 9)
}

type tomlDoc struct {
	Version int         `toml:"version"`
	Entry   []tomlEntry `toml:"entry"`
}

// ExportTOML renders entries as an editable TOML document (memory export,
// memory edit).
func ExportTOML(entries []Entry) ([]byte, error) {
	doc := tomlDoc{Version: fileVersion}
	for _, e := range entries {
		paths := e.Paths
		if paths == nil {
			paths = []string{}
		}
		doc.Entry = append(doc.Entry, tomlEntry{Ref: e.Ref().String(), Scope: string(e.Scope), Dot: e.Dot,
			Kind: string(e.Kind), Status: string(e.Status), Source: string(e.Source), Pinned: e.Pinned, Paths: paths, Text: e.Text, From: e.From})
	}
	var buf bytes.Buffer
	buf.WriteString(exportHeader)
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImportTOML parses an exported (and maybe edited) document. Entries keep
// their id when "ref" matches their scope; new blocks have no id.
func ImportTOML(data []byte) ([]Entry, error) {
	// The file may have come from someone else: refuse nesting bombs before
	// the decoder, and cap what the decoder echoes back.
	if err := dots.PreScanTOML(data); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, dots.CleanText(err.Error(), 300))
	}
	var doc tomlDoc
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, dots.CleanText(err.Error(), 300))
	}
	if doc.Version > fileVersion {
		return nil, fmt.Errorf("%w (version %d)", ErrVersion, doc.Version)
	}
	var out []Entry
	for i, te := range doc.Entry {
		e := Entry{Scope: Scope(strings.TrimSpace(te.Scope)), Dot: strings.TrimSpace(te.Dot), Kind: Kind(strings.TrimSpace(te.Kind)),
			Status: Status(strings.TrimSpace(te.Status)), Source: Source(strings.TrimSpace(te.Source)), Pinned: te.Pinned,
			Paths: te.Paths, Text: strings.TrimSpace(te.Text), From: strings.TrimSpace(te.From)}
		if !e.Scope.Valid() {
			return nil, fmt.Errorf("%w: entry %d: scope %q must be project, dot or global", ErrInvalid, i+1, te.Scope)
		}
		if !e.Kind.Valid() {
			return nil, fmt.Errorf("%w: entry %d: kind %q", ErrInvalid, i+1, te.Kind)
		}
		if e.Dot != "" && !dotSlugRe.MatchString(e.Dot) {
			return nil, fmt.Errorf("%w: entry %d: dot %q is not a valid dot name", ErrInvalid, i+1, e.Dot)
		}
		if e.From != "" && !dotSlugRe.MatchString(e.From) {
			return nil, fmt.Errorf("%w: entry %d: from %q is not a valid dot name", ErrInvalid, i+1, e.From)
		}
		if e.Status != "" && !e.Status.Valid() {
			return nil, fmt.Errorf("%w: entry %d: status %q must be active, proposed or archived", ErrInvalid, i+1, e.Status)
		}
		if e.Source != "" && !e.Source.Valid() {
			return nil, fmt.Errorf("%w: entry %d: source %q", ErrInvalid, i+1, e.Source)
		}
		if e.Scope != ScopeProject && e.Dot == "" {
			return nil, fmt.Errorf("%w: entry %d: %s memory needs a dot", ErrInvalid, i+1, e.Scope)
		}
		if e.Status == "" {
			e.Status = StatusActive
		}
		if te.Ref != "" {
			r, err := ParseRef(te.Ref, e.Dot)
			if err != nil {
				return nil, fmt.Errorf("entry %d: %w", i+1, err)
			}
			if r.Scope == e.Scope {
				e.ID = r.ID // a changed scope makes it a new entry
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// DiffTOML compares the entries that were exported with the ones read back:
// blocks without an id (or with an id that no longer matches) are adds,
// changed blocks are updates, and missing blocks are deletes.
func DiffTOML(before, after []Entry) (adds, updates []Entry, deletes []Ref) {
	old := map[Ref]Entry{}
	for _, e := range before {
		old[e.Ref()] = e
	}
	seen := map[Ref]bool{}
	for _, e := range after {
		if e.ID == "" {
			adds = append(adds, e)
			continue
		}
		r := e.Ref()
		o, ok := old[r]
		if !ok || seen[r] {
			e.ID = ""
			adds = append(adds, e)
			continue
		}
		seen[r] = true
		if o.Text != e.Text || o.Kind != e.Kind || o.Pinned != e.Pinned || o.Status != e.Status || !slices.Equal(o.Paths, e.Paths) {
			updates = append(updates, e)
		}
	}
	for _, e := range before {
		if !seen[e.Ref()] {
			deletes = append(deletes, e.Ref())
		}
	}
	return adds, updates, deletes
}

// SecretHits returns the refs (or, for entries without an id, a ref with an
// empty ID) of entries whose text or paths look like a credential. Importing never
// refuses such text; callers run this on the adds and updates from DiffTOML
// and ask the developer to confirm.
func SecretHits(entries []Entry) []Ref {
	var hits []Ref
	for _, e := range entries {
		if found, _ := ScanSecrets(e.Text + " " + strings.Join(e.Paths, " ")); found {
			hits = append(hits, e.Ref())
		}
	}
	return hits
}

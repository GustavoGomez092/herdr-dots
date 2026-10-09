package memory

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const exportHeader = `# herdr-dots memory. Edit text, kind, paths, pinned or status (active, proposed, archived).
# Delete an [[entry]] block to forget it. Add a block without "ref" to create an entry.
# Leave "ref", "scope" and "dot" alone on existing entries.
`

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
			Kind: string(e.Kind), Status: string(e.Status), Source: string(e.Source), Pinned: e.Pinned, Paths: paths, Text: e.Text})
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
	var doc tomlDoc
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if doc.Version > fileVersion {
		return nil, fmt.Errorf("%w (version %d)", ErrVersion, doc.Version)
	}
	var out []Entry
	for i, te := range doc.Entry {
		e := Entry{Scope: Scope(strings.TrimSpace(te.Scope)), Dot: strings.TrimSpace(te.Dot), Kind: Kind(strings.TrimSpace(te.Kind)),
			Status: Status(strings.TrimSpace(te.Status)), Source: Source(strings.TrimSpace(te.Source)), Pinned: te.Pinned,
			Paths: te.Paths, Text: strings.TrimSpace(te.Text)}
		if !e.Scope.Valid() {
			return nil, fmt.Errorf("%w: entry %d: scope %q must be project, dot or global", ErrInvalid, i+1, te.Scope)
		}
		// Imported text is untrusted: it never lands in memory with a credential in it.
		if found, kind := ScanSecrets(e.Text); found {
			return nil, fmt.Errorf("%w: entry %d: text looks like a secret (%s)", ErrInvalid, i+1, kind)
		}
		if !e.Kind.Valid() {
			return nil, fmt.Errorf("%w: entry %d: kind %q", ErrInvalid, i+1, te.Kind)
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

package dots

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/fsutil"
)

var ErrExists = errors.New("a dot with this name already exists")

type LoadError struct {
	File string // base name, e.g. "security.toml"
	Line int    // 0 when not a parse error
	Err  error
}

func (e LoadError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %v", e.File, e.Line, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.File, e.Err)
}

func path(dir, slug string) string { return filepath.Join(dir, slug+".toml") }

func decode(file string) (Dot, error) {
	d := New("")
	if _, err := toml.DecodeFile(file, &d); err != nil {
		return Dot{}, err
	}
	d.Slug = strings.TrimSuffix(filepath.Base(file), ".toml")
	return d, nil
}

// LoadAll returns valid dots sorted by name, plus one LoadError per bad file.
// A missing directory is not an error.
func LoadAll(dir string) ([]Dot, []LoadError) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []LoadError{{File: dir, Err: err}}
	}
	var out []Dot
	var bad []LoadError
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		d, err := decode(filepath.Join(dir, e.Name()))
		if err != nil {
			le := LoadError{File: e.Name(), Err: err}
			var pe toml.ParseError
			if errors.As(err, &pe) {
				le.Line = pe.Position.Line
			}
			bad = append(bad, le)
			continue
		}
		if err := d.Validate(); err != nil {
			bad = append(bad, LoadError{File: e.Name(), Err: err})
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, bad
}

func Load(dir, slug string) (Dot, error) {
	d, err := decode(path(dir, slug))
	if err != nil {
		return Dot{}, err
	}
	return d, d.Validate()
}

// Save validates and writes d. With create=true it refuses to overwrite an
// existing file (slug collision); with create=false it replaces it.
func Save(dir string, d Dot, create bool) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if create || d.Slug == "" {
		d.Slug = Slugify(d.Name)
	}
	p := path(dir, d.Slug)
	if create {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("%w (%s)", ErrExists, d.Slug)
		}
	}
	if d.Backend.Args == nil {
		d.Backend.Args = []string{}
	}
	if d.Trigger.FileGlobs == nil {
		d.Trigger.FileGlobs = []string{}
	}

	// Encode a copy of the dot with Purpose omitted (empty string omits due to omitempty tag)
	dCopy := d
	purpose := d.Purpose
	dCopy.Purpose = ""

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(dCopy); err != nil {
		return err
	}

	// Manually insert the purpose field as a multiline basic string after top-level keys,
	// before the first section (line starting with [)
	output := insertPurpose(buf.String(), purpose)
	return fsutil.WriteFileAtomic(p, []byte(output), 0o600)
}

// multilineBasic escapes a string for use in a TOML multiline basic string (""" ... """).
// Keeps newlines literal, escapes backslashes, quotes, and control characters.
func multilineBasic(s string) string {
	var result strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			result.WriteString("\\\\")
		case '"':
			result.WriteString("\\\"")
		case '\r':
			result.WriteString("\\r")
		case '\t', '\n':
			// Keep tabs and newlines literal
			result.WriteRune(r)
		default:
			if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
				// Control characters (except \t and \n) as \uXXXX
				fmt.Fprintf(&result, "\\u%04x", r)
			} else {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}

// insertPurpose inserts the purpose field as a multiline basic string after top-level keys,
// before the first section.
func insertPurpose(tomlStr, purpose string) string {
	if purpose == "" {
		return tomlStr
	}

	lines := strings.Split(tomlStr, "\n")
	var result []string
	inserted := false

	escaped := multilineBasic(purpose)
	for _, line := range lines {
		// Insert before the first section (line starting with [)
		if !inserted && strings.HasPrefix(strings.TrimSpace(line), "[") {
			// Add purpose as multiline string (closing """ on same line as content)
			result = append(result, "purpose = \"\"\"")
			// Split escaped content by newlines and add them
			contentLines := strings.Split(escaped, "\n")
			for i, cl := range contentLines {
				if i == len(contentLines)-1 {
					// Last line: append closing quotes
					result = append(result, cl+"\"\"\"")
				} else {
					result = append(result, cl)
				}
			}
			inserted = true
		}
		result = append(result, line)
	}

	// If no section was found, append at the end
	if !inserted {
		result = append(result, "purpose = \"\"\"")
		contentLines := strings.Split(escaped, "\n")
		for i, cl := range contentLines {
			if i == len(contentLines)-1 {
				result = append(result, cl+"\"\"\"")
			} else {
				result = append(result, cl)
			}
		}
	}

	return strings.Join(result, "\n")
}

func Delete(dir, slug string) error { return os.Remove(path(dir, slug)) }

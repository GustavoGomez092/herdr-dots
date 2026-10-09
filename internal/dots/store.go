package dots

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/secrets"
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
	if d.Origin == "" || d.Origin == OriginProject {
		d.Origin = OriginUser // only DecodeProject makes a project dot
	}
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
	data, err := encode(d)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(p, data, 0o600)
}

// Encode renders d as a dot file to share (packs): the TOML encoder's output,
// with purpose as a multi-line string before the first table, nil lists as []
// and a user origin left out. Slug and SourcePath are never written. A dot
// whose text holds something shaped like a credential is refused: the error
// names the field, never the value.
func Encode(d Dot) ([]byte, error) {
	if field := secretField(d); field != "" {
		return nil, fmt.Errorf("%s looks like it holds a secret (a key, token or password); remove it before sharing this dot", field)
	}
	return encode(d)
}

// encode is Encode without the secret check: Save writes the developer's own
// file on this machine (0600), which may hold what they put there.
func encode(d Dot) ([]byte, error) {
	if d.Backend.Args == nil {
		d.Backend.Args = []string{}
	}
	if d.Trigger.FileGlobs == nil {
		d.Trigger.FileGlobs = []string{}
	}
	if d.Origin == OriginUser {
		d.Origin = ""
	}
	purpose := d.Purpose
	d.Purpose = "" // omitempty: written by insertPurpose instead
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(d); err != nil {
		return nil, err
	}
	return []byte(insertPurpose(buf.String(), purpose)), nil
}

// looksSecret reports text the secret scanner flags (secrets.Scan, the
// patterns behind memory.ScanSecrets, PEM private keys included) or that
// credentials.Redact would change.
func looksSecret(s string) bool {
	if found, _ := secrets.Scan(s); found {
		return true
	}
	return credentials.Redact(s) != s
}

// secretField returns the TOML key of the first serialized string of d that
// looks secret, or "". Walking by reflection (eachString) means a text field
// added later is checked too.
func secretField(d Dot) string {
	hit := ""
	eachString(reflect.ValueOf(d), "", func(key, s string) {
		if hit == "" && looksSecret(s) {
			hit = key
		}
	})
	return hit
}

// eachString calls fn with the TOML key and value of every serialized string
// in v (a Dot or one of its tables), list elements included. Fields tagged
// toml:"-" (Slug, SourcePath) are skipped.
func eachString(v reflect.Value, key string, fn func(key, s string)) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			if key != "" {
				name = key + "." + name
			}
			eachString(v.Field(i), name, fn)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			eachString(v.Index(i), key, fn)
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			eachString(it.Value(), key, fn)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			eachString(v.Elem(), key, fn)
		}
	case reflect.String:
		fn(key, v.String())
	}
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

// PurposeBlock is `purpose = """…"""` as TOML lines: a multi-line basic
// string with the closing quotes on the last content line.
func PurposeBlock(purpose string) string {
	return "purpose = \"\"\"\n" + multilineBasic(purpose) + "\"\"\""
}

// insertPurpose inserts the purpose block before the first table header of
// an encoder-produced document (no other line there starts with "["), or
// appends it when there is no table.
func insertPurpose(tomlStr, purpose string) string {
	if purpose == "" {
		return tomlStr
	}
	lines := strings.Split(tomlStr, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			out := append(append(slices.Clone(lines[:i]), PurposeBlock(purpose)), lines[i:]...)
			return strings.Join(out, "\n")
		}
	}
	return strings.Join(append(lines, PurposeBlock(purpose)), "\n")
}

func Delete(dir, slug string) error { return os.Remove(path(dir, slug)) }

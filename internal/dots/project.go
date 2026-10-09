package dots

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Dot origins (Dot.Origin). User files store template and import; a user
// origin is never written. Project is set only by DecodeProject.
const (
	OriginUser     = "user"
	OriginTemplate = "template"
	OriginImport   = "import"
	OriginProject  = "project"
)

// ProjectPrefix starts every project dot slug: proj--<name>--<project tag>.
// Slugify never produces "--", so no user dot can take a project slug, and
// the slug stays a valid file name and memory slug on every OS.
const ProjectPrefix = "proj--"

// ProjectDir is where a repository keeps its team dots, relative to its root.
var ProjectDir = filepath.Join(".herdr-dots", "dots")

// MaxProjectFileBytes caps one project dot file.
const MaxProjectFileBytes = 64 << 10

var (
	projectBaseRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	projectTagRe  = regexp.MustCompile(`^[0-9a-f]{8}$`)
)

// ProjectTag is the 8 hex characters of a memory project id (g-/p- + hex)
// that make project slugs unique per repository. "" for anything else.
func ProjectTag(projectID string) string {
	if len(projectID) < 10 || (projectID[:2] != "g-" && projectID[:2] != "p-") {
		return ""
	}
	if tag := projectID[2:10]; projectTagRe.MatchString(tag) {
		return tag
	}
	return ""
}

// ProjectSlug is the engine slug of a project dot file base name in a project.
func ProjectSlug(base, projectID string) string {
	return ProjectPrefix + base + "--" + ProjectTag(projectID)
}

// SplitProjectSlug returns a project slug's file base name and project tag.
func SplitProjectSlug(slug string) (base, tag string, ok bool) {
	rest, found := strings.CutPrefix(slug, ProjectPrefix)
	if !found {
		return "", "", false
	}
	i := strings.LastIndex(rest, "--")
	if i <= 0 {
		return "", "", false
	}
	base, tag = rest[:i], rest[i+2:]
	if !projectBaseRe.MatchString(base) || !projectTagRe.MatchString(tag) {
		return "", "", false
	}
	return base, tag, true
}

// IsProjectSlug reports whether slug belongs to a project dot.
func IsProjectSlug(slug string) bool {
	_, _, ok := SplitProjectSlug(slug)
	return ok || strings.HasPrefix(slug, ProjectPrefix)
}

// DisplaySlug is how people see a slug: proj:<name> for project dots.
func DisplaySlug(slug string) string {
	if base, _, ok := SplitProjectSlug(slug); ok {
		return "proj:" + base
	}
	return slug
}

// ProjectFile is one project dot file as read from disk. Data is exactly the
// bytes that were parsed, so a hash of Data is a hash of what runs.
type ProjectFile struct {
	Base string // file name without .toml
	Path string // absolute path
	Rel  string // path from the repository root, with forward slashes
	Data []byte
}

// ReadProjectFiles reads root/.herdr-dots/dots/*.toml. Symlinked directories
// or files, files over MaxProjectFileBytes and names that can't become a slug
// are refused with a LoadError. A missing directory is not an error.
func ReadProjectFiles(root string) ([]ProjectFile, []LoadError) {
	dir := filepath.Join(root, ProjectDir)
	for _, d := range []string{filepath.Join(root, ".herdr-dots"), dir} {
		st, err := os.Lstat(d)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, []LoadError{{File: d, Err: err}}
		}
		if !st.IsDir() {
			return nil, []LoadError{{File: d, Err: errors.New("must be a directory, not a link or a file")}}
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, []LoadError{{File: dir, Err: err}}
	}
	var out []ProjectFile
	var bad []LoadError
	for _, e := range ents {
		base, ok := strings.CutSuffix(e.Name(), ".toml")
		if !ok || e.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(ProjectDir, e.Name()))
		if !projectBaseRe.MatchString(base) || len(base) > 64 {
			bad = append(bad, LoadError{File: rel, Err: errors.New("file name must be lowercase letters, digits and single dashes (at most 64)")})
			continue
		}
		if e.Type()&fs.ModeType != 0 {
			bad = append(bad, LoadError{File: rel, Err: errors.New("must be a regular file, not a link")})
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := readCapped(p, MaxProjectFileBytes)
		if err != nil {
			bad = append(bad, LoadError{File: rel, Err: err})
			continue
		}
		out = append(out, ProjectFile{Base: base, Path: p, Rel: rel, Data: data})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out, bad
}

func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file is over %d bytes, the cap for project dots", limit)
	}
	return data, nil
}

// DecodeProject parses one project dot file. Keys this version doesn't know
// are refused (a newer capability can't be clamped if it isn't understood),
// and the dot must validate.
func DecodeProject(f ProjectFile, projectID string) (Dot, error) {
	if ProjectTag(projectID) == "" {
		return Dot{}, fmt.Errorf("%s: project id %q can't name project dots", f.Rel, projectID)
	}
	d, err := DecodeStrict(f.Data)
	if err != nil {
		return Dot{}, err
	}
	d.Slug = ProjectSlug(f.Base, projectID)
	d.Origin = OriginProject
	d.SourcePath = f.Path
	return d, d.Validate()
}

// DecodeStrict decodes a dot from TOML over the defaults and refuses keys it
// doesn't know. It does not validate.
func DecodeStrict(data []byte) (Dot, error) {
	d := New("")
	md, err := toml.Decode(string(data), &d)
	if err != nil {
		return Dot{}, err
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return Dot{}, fmt.Errorf("unknown keys %s: this herdr-dots can't tell what they do, so it won't load them", strings.Join(keys, ", "))
	}
	return d, nil
}

// LoadProject loads root's project dots with project-namespaced slugs.
func LoadProject(root, projectID string) ([]Dot, []LoadError) {
	files, bad := ReadProjectFiles(root)
	var out []Dot
	for _, f := range files {
		d, err := DecodeProject(f, projectID)
		if err != nil {
			le := LoadError{File: f.Rel, Err: err}
			var pe toml.ParseError
			if errors.As(err, &pe) {
				le.Line = pe.Position.Line
			}
			bad = append(bad, le)
			continue
		}
		out = append(out, d)
	}
	return out, bad
}

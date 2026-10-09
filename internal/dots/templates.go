package dots

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed templates/*.toml
var templateFS embed.FS

// Templates returns the built-in dots, sorted by name. Slug = file name.
func Templates() []Dot {
	entries, _ := fs.ReadDir(templateFS, "templates")
	var out []Dot
	for _, e := range entries {
		data, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			continue
		}
		d := New("")
		if _, err := toml.Decode(string(data), &d); err != nil {
			continue
		}
		d.Slug = strings.TrimSuffix(e.Name(), ".toml")
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func FromTemplate(slug string) (Dot, error) {
	for _, d := range Templates() {
		if d.Slug == slug {
			return d, nil
		}
	}
	return Dot{}, fmt.Errorf("unknown template %q", slug)
}

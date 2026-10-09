package toolloop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// adapterSource reads one API adapter's source file.
func adapterSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every API adapter picks its tools through Select, so a draft gets the
// write tools and a review or memory pass never does.
func TestEveryAPIAdapterPicksItsToolsThroughSelect(t *testing.T) {
	for _, rel := range []string{"anthropicapi/anthropic.go", "openaiapi/openai.go", "openrouter/openrouter.go", "ollama/ollama.go"} {
		src := adapterSource(t, rel)
		if !strings.Contains(src, "toolloop.Select(r.Cwd, r.RepoRead && r.Pass != backend.PassMemory, backend.WriteMode(r),") {
			t.Errorf("%s does not pick its tools with toolloop.Select", rel)
		}
		for _, banned := range []string{"toolloop.RepoTools(", "toolloop.WriteTools(", "toolloop.SystemNote"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s still uses %s directly", rel, banned)
			}
		}
	}
}

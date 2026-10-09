package dotpack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fetch refuses a redirect to another host (TestFetch… above); the README
// must say so and what to do instead, since some raw-file hosts redirect to
// a CDN.
func TestREADMEExplainsSameHostRedirects(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	r := strings.Join(strings.Fields(string(data)), " ")
	for _, s := range []string{
		"a redirect to another host is refused too",
		"download the file and import it from disk",
	} {
		if !strings.Contains(r, s) {
			t.Errorf("README doesn't say %q", s)
		}
	}
}

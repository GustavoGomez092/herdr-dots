package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// P7-R2 (forgetting is real): a dot's own corrupt asides hold only its own
// entries, so deleting the dot deletes them too.
func TestDeleteDotRemovesItsCorruptAsidesOnly(t *testing.T) {
	s := testStore(t)
	var mine, theirs []string
	for _, dir := range []string{filepath.Join(s.Dir, pid, "dots"), filepath.Join(s.Dir, GlobalID, "dots")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"security.json.corrupt-20261008T120000.000000000Z", "security.json.corrupt-20261009T120000.000000000Z"} {
			mine = append(mine, filepath.Join(dir, n))
		}
		for _, n := range []string{"securityx.json.corrupt-20261008T120000.000000000Z", "frontend.json.corrupt-20261008T120000.000000000Z", "frontend.json"} {
			theirs = append(theirs, filepath.Join(dir, n))
		}
	}
	// a mixed project-level aside can't be scrubbed; purge clears it
	theirs = append(theirs, filepath.Join(s.Dir, pid, "project.json.corrupt-20261008T120000.000000000Z"))
	for _, p := range append(append([]string(nil), mine...), theirs...) {
		if err := os.WriteFile(p, []byte(`{"entries":[{"text":"Tokens live in httpOnly cookies."}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DeleteDot("security"); err != nil {
		t.Fatal(err)
	}
	for _, p := range mine {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("aside left: %s", p)
		}
	}
	for _, p := range theirs {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("another file was removed: %s: %v", p, err)
		}
	}
}

func TestDeleteDotReportsAnAsideItCouldNotRemove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions don't stop a delete on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	s := testStore(t)
	dir := filepath.Join(s.Dir, pid, "dots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	aside := filepath.Join(dir, "security.json.corrupt-20261008T120000.000000000Z")
	if err := os.WriteFile(aside, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := s.DeleteDot("security")
	if err == nil || !strings.Contains(err.Error(), "corrupt-") {
		t.Fatalf("DeleteDot = %v, want the aside's removal error", err)
	}
}

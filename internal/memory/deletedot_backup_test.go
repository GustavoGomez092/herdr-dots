package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// Forgetting is real for migration backups too (internal/migrate saves
// <file>.bak-v<old> before a rewrite): a dot's own backups go with it, and so
// do meta and schedule backups that name it, like the corrupt asides.
// Mixed project backups and other dots' backups stay.
func TestDeleteDotRemovesItsMigrationBackups(t *testing.T) {
	s := testStore(t)
	proj, glob := filepath.Join(s.Dir, pid), filepath.Join(s.Dir, GlobalID)
	mine := []string{
		filepath.Join(proj, "dots", "security.json.bak-v1"),
		filepath.Join(glob, "dots", "security.json.bak-v1-2"),
		filepath.Join(proj, "meta.json.bak-v1"),
		filepath.Join(proj, "schedule.json.bak-v1"),
	}
	theirs := []string{
		filepath.Join(proj, "dots", "securityx.json.bak-v1"),
		filepath.Join(proj, "dots", "frontend.json.bak-v1"),
		filepath.Join(proj, "meta.json.bak-v1-2"),
		filepath.Join(proj, "schedule.json.bak-v1-2"),
		filepath.Join(proj, "project.json.bak-v1"),
	}
	bodies := map[string]string{
		mine[2]:   `{"version":1,"id":"` + pid + `","dots_seen":{"security":"2026-10-08T12:00:00Z"}}`,
		mine[3]:   `{"version":1,"last":{"security":"2026-10-08T09:00:00Z"}}`,
		theirs[2]: `{"version":1,"id":"` + pid + `","name":"security","dots_seen":{"frontend":"2026-10-08T12:00:00Z"}}`,
		theirs[3]: `{"version":1,"last":{"frontend":"2026-10-08T09:00:00Z"}}`,
	}
	for _, p := range append(append([]string(nil), mine...), theirs...) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		body, ok := bodies[p]
		if !ok {
			body = `{"version":1,"entries":[{"text":"Tokens live in httpOnly cookies."}]}`
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DeleteDot("security"); err != nil {
		t.Fatal(err)
	}
	for _, p := range mine {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("backup left: %s", p)
		}
	}
	for _, p := range theirs {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("another file was removed: %s: %v", p, err)
		}
	}
}

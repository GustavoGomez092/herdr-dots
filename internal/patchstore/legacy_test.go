package patchstore

import (
	"os"
	"path/filepath"
	"testing"
)

// Records written before summary_raw was removed still load (the field is
// ignored).
func TestARecordWithTheOldSummaryRawFieldStillLoads(t *testing.T) {
	s := Open(t.TempDir())
	id := "0123456789abcdef"
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"id":"` + id + `","dot":"security","pane":"w1:p1","repo":"/r","summary":"Rotated [REDACTED].","summary_raw":"Rotated sk-ant-x.","files":1,"bytes":0,"status":"ready","created_at":"2026-10-08T00:00:00Z","updated_at":"2026-10-08T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(s.Dir, id+".json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := s.Get(id)
	if err != nil || m.Summary != "Rotated [REDACTED]." || m.Status != StatusReady {
		t.Fatalf("Get = %+v, %v", m, err)
	}
}

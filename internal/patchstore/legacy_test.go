package patchstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A record that still carries summary_raw loads with SummaryHadSecret set,
// and the next rewrite drops the raw field.
func TestOldSummaryRawSetsHadSecretAndIsDroppedOnRewrite(t *testing.T) {
	s := Open(t.TempDir())
	id := "0123456789abcdef"
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"id":"` + id + `","dot":"security","pane":"w1:p1","repo":"/r","summary":"Rotated x.","summary_raw":"Rotated ghp_secret.","files":1,"bytes":0,"status":"ready","created_at":"2026-10-08T00:00:00Z","updated_at":"2026-10-08T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(s.Dir, id+".json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := s.Get(id)
	if err != nil || !m.SummaryHadSecret {
		t.Fatalf("Get = %+v, %v; want SummaryHadSecret", m, err)
	}
	if _, err := s.Update(id, func(m *Meta) { m.Stat = "x" }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Dir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "summary_raw") || strings.Contains(string(raw), "ghp_secret") {
		t.Fatalf("rewrite kept the raw summary: %s", raw)
	}
	if !strings.Contains(string(raw), `"summary_had_secret": true`) {
		t.Fatalf("rewrite lost the flag: %s", raw)
	}
}

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

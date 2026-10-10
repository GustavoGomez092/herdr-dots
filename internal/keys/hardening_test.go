package keys

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// L9 (post-rc hardening): a plugin action the developer already bound to
// another key counts as bound; setup-keys doesn't add a second key for it.
func TestPlanSeesAnActionAlreadyBoundToAnotherKey(t *testing.T) {
	ex, _ := Existing([]byte(userConfig + "\n[[keys.command]]\nkey = \"prefix+m\"\ntype = \"plugin_action\"\ncommand = \"herdr-dots.manage\"\n"))
	add, already, _ := Plan(Defaults(), ex)
	for _, b := range add {
		if b.Action == "herdr-dots.manage" {
			t.Fatalf("manage is already on prefix+m, yet the plan adds %s", b.Key)
		}
	}
	if len(already) != 1 || already[0].Key != "prefix+m" || already[0].Action != "herdr-dots.manage" {
		t.Fatalf("already %v", already)
	}
}

// L8 (post-rc hardening): two runs in the same second keep both backups;
// the second never overwrites the first (which holds the original).
func TestAppendBackupsInTheSameSecondAreUnique(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	b1, err := Append(p, Defaults()[2:3], now)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := Append(p, Defaults()[3:], now)
	if err != nil {
		t.Fatal(err)
	}
	if b1 == b2 {
		t.Fatalf("both runs wrote backup %s", b1)
	}
	if orig, _ := os.ReadFile(b1); string(orig) != userConfig {
		t.Fatalf("the first backup must still hold the original:\n%s", orig)
	}
}

// L8 (post-rc hardening): with no config yet there is nothing to back up, so
// no empty backup file is written.
func TestAppendWithoutAConfigWritesNoBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	backup, err := Append(p, Defaults()[:1], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Fatalf("backup %q for a config that didn't exist", backup)
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Fatalf("want only config.toml, got %v", es)
	}
}

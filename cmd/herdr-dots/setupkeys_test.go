package main

import "testing"

// L10 (post-rc hardening): `setup-keys -h` prints its usage and exits
// cleanly; it reads no config and isn't reported as an error.
func TestSetupKeysHelpIsNotAnError(t *testing.T) {
	t.Setenv("HERDR_CONFIG_PATH", "/nonexistent/herdr/config.toml")
	if err := runSetupKeys([]string{"-h"}); err != nil {
		t.Fatalf("-h: %v", err)
	}
}

// L8 (post-rc hardening): setup-keys names the backup, or says there was no
// config to back up (Append writes no empty backup).
func TestSetupKeysAppendedLine(t *testing.T) {
	if l := appendedLine("/h/.config/herdr/config.toml.bak-20261008-120000-2"); l != "Appended. Backup: /h/.config/herdr/config.toml.bak-20261008-120000-2" {
		t.Fatalf("with a backup: %q", l)
	}
	if l := appendedLine(""); l != "Appended. There was no config to back up." {
		t.Fatalf("without a backup: %q", l)
	}
}

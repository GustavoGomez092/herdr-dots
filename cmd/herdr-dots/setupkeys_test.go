package main

import "testing"

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

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/patchstore"
)

func TestDiffModelRefusesAForeignCard(t *testing.T) {
	store := patchstore.Open(filepath.Join(t.TempDir(), "patches"))
	const id = "0123456789abcdef"
	diff := []byte("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n")
	if err := store.PutPatch(patchstore.Meta{ID: id, Dot: "security", Card: "7", Stat: "1 file", Paths: []string{"x.go"}, Status: patchstore.StatusReady}, diff); err != nil {
		t.Fatal(err)
	}
	if v := diffModel(store, id, "8", true, nil).View(); !strings.Contains(v, "is not the card of patch") {
		t.Fatalf("foreign card:\n%s", v)
	}
	if v := diffModel(store, id, "7", false, nil).View(); strings.Contains(v, "is not the card") {
		t.Fatalf("own card refused:\n%s", v)
	}
}

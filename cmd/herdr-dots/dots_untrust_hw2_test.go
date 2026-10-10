package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/projectdots"
)

// H7: `dots untrust FILE` revokes the trust of this checkout's file, as the
// engine's untrust does (ruling P9-R13): the current bytes (in every checkout
// that holds those same bytes) when the file parses, else the entry given at
// this path. Another checkout's trust of other content stays.
func TestUntrustFileLeavesAnotherCheckoutsTrust(t *testing.T) {
	const rel = ".herdr-dots/dots/helper.toml"
	for _, name := range []string{"parses", "deleted", "broken"} {
		t.Run(name, func(t *testing.T) {
			r := newDotsCLI(t)
			file := projectDotFile(t, r)
			data, _ := os.ReadFile(file)
			here := projectdots.Sum(data)
			if err := r.c.trust.Trust(dotsTestPID, rel, file, here, nil); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(r.root, "other-clone", filepath.FromSlash(rel))
			otherSum := projectdots.Sum([]byte("name = \"Helper\"\npurpose = \"Other checkout.\"\n"))
			if err := r.c.trust.Trust(dotsTestPID, rel, other, otherSum, nil); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "deleted":
				os.Remove(file)
			case "broken":
				os.WriteFile(file, []byte("name = = 1"), 0o644)
			}
			r.run(t, "untrust", file)
			if ok, _, _ := r.c.trust.Check(dotsTestPID, rel, here); ok {
				t.Fatal("this checkout's file is still trusted")
			}
			if ok, _, _ := r.c.trust.Check(dotsTestPID, rel, otherSum); !ok {
				t.Fatal("untrust in this checkout revoked another checkout's trust")
			}
		})
	}
}

// H7: a file trusted only through another path to this checkout (no entry
// at this path) still loses its trust, as before.
func TestUntrustFileFallsBackToEveryEntryWhenNoneIsAtThisPath(t *testing.T) {
	const rel = ".herdr-dots/dots/helper.toml"
	r := newDotsCLI(t)
	file := projectDotFile(t, r)
	os.Remove(file)
	sum := projectdots.Sum([]byte("old bytes"))
	if err := r.c.trust.Trust(dotsTestPID, rel, filepath.Join(r.root, "link-to-repo", filepath.FromSlash(rel)), sum, nil); err != nil {
		t.Fatal(err)
	}
	r.run(t, "untrust", file)
	if es, _ := r.c.trust.List(); len(es) != 0 {
		t.Fatalf("entries left: %+v", es)
	}
}

// H11: `dots trust` refuses a file over the key and table cap, as the scan
// does, so nothing is trusted that the library would never load.
func TestTrustRefusesAFileOverTheKeyAndTableCap(t *testing.T) {
	r := newDotsCLI(t)
	file := projectDotFile(t, r)
	data, _ := os.ReadFile(file)
	data = append(data, []byte(strings.Repeat("[t]\n", projectdots.MaxKeysAndTables))...)
	os.WriteFile(file, data, 0o644)
	err := r.c.run([]string{"trust", file, "--yes"})
	if err == nil || !strings.Contains(err.Error(), "keys or tables") {
		t.Fatalf("err %v", err)
	}
	if es, _ := r.c.trust.List(); len(es) != 0 {
		t.Fatalf("trusted: %+v", es)
	}
}

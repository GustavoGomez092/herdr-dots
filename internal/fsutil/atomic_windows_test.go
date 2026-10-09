//go:build windows

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"herdr-dots/internal/platform"
)

// A reader holding the old file open (Go opens without FILE_SHARE_DELETE)
// makes the replacing rename fail for a moment; the write must wait it out.
func TestWriteFileAtomicWaitsForAReader(t *testing.T) {
	p := filepath.Join(t.TempDir(), "queue.json")
	if err := WriteFileAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(150 * time.Millisecond); f.Close() }()
	if err := WriteFileAtomic(p, []byte("two"), 0o600); err != nil {
		t.Fatalf("write while a reader had the file open: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "two" {
		t.Fatalf("content %q", got)
	}
}

// A private write into a directory that lets Everyone in still ends up
// owner-only: the DACL is set on the file itself, not left to inheritance.
func TestPrivateWritesGetAnOwnerOnlyDACLEvenInAnOpenDir(t *testing.T) {
	dir := t.TempDir()
	sd, err := windows.SecurityDescriptorFromString("D:(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(dir, "attachments.json")
	if err := WriteFileAtomic(priv, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := platform.CheckPrivate(priv); err != nil {
		t.Fatalf("0600 write in an open dir: %v", err)
	}
}

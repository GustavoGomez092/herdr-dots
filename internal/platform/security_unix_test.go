//go:build unix

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPrivateReadsModeBits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(p); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("0644: %v, want ErrNotPrivate", err)
	}
	if err := SecureFile(p); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(p); err != nil {
		t.Fatalf("after SecureFile: %v", err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("SecureFile mode %v", st.Mode().Perm())
	}
}

func TestCheckPrivateFileChecksTheOpenFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := CheckPrivateFile(f); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("0644: %v, want ErrNotPrivate", err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateFile(f); err != nil {
		t.Fatalf("0600: %v", err)
	}
}

func TestSecureDirIs0700(t *testing.T) {
	d := filepath.Join(t.TempDir(), "memory")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SecureDir(d); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(d); st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestOpenNoFollowRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	os.WriteFile(real, []byte("ok"), 0o600)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if f, err := OpenNoFollow(real); err != nil {
		t.Fatal(err)
	} else {
		f.Close()
	}
	if f, err := OpenNoFollow(link); err == nil {
		f.Close()
		t.Fatal("symlink opened")
	}
	if ReservedPathName(".env::$DATA") || ReservedPathName("NUL") {
		t.Fatal("Unix has no reserved names")
	}
}

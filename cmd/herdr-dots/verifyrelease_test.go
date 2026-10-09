package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vrAsset = "herdr-dots_1.0.0-rc.1_darwin_arm64.tar.gz"

func vrTarGz(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "herdr-dots", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// vrRoot is an installed release: archive kept in bin/release, binary in bin/.
func vrRoot(t *testing.T, binary string, bundle bool) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	archive := vrTarGz(t, "the release binary")
	s := sha256.Sum256(archive)
	files := map[string][]byte{
		"bin/release/" + vrAsset: archive,
		"bin/herdr-dots":         []byte(binary),
		"bin/INSTALL_SOURCE":     []byte("source=release\nversion=1.0.0-rc.1\nrepo=acme/herdr-dots\nasset=" + vrAsset + "\nsha256=" + hex.EncodeToString(s[:]) + "\n"),
		"release/SHA256SUMS":     []byte(hex.EncodeToString(s[:]) + "  " + vrAsset + "\n"),
	}
	if bundle {
		files["bin/release/"+vrAsset+".sigstore.json"] = []byte(`{}`)
	}
	for rel, b := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, archive
}

type vrRun struct {
	calls [][]string
	err   error
}

func (r *vrRun) io(root string, out *bytes.Buffer, cosign bool) verifyIO {
	return verifyIO{root: root, out: out,
		lookPath: func(n string) (string, error) {
			if cosign && n == "cosign" {
				return "/usr/local/bin/cosign", nil
			}
			return "", errors.New("not found")
		},
		run: func(name string, a ...string) (string, error) {
			r.calls = append(r.calls, append([]string{name}, a...))
			return "Verified OK", r.err
		}}
}

func TestVerifyReleaseChecksArchiveAndBinary(t *testing.T) {
	root, _ := vrRoot(t, "the release binary", false)
	var out bytes.Buffer
	if err := verifyRelease((&vrRun{}).io(root, &out, false)); err != nil {
		t.Fatalf("verify: %v\n%s", err, out.String())
	}
	// Without cosign it says so plainly instead of failing open.
	for _, want := range []string{"checksum ok", "binary ok", "signature: not checked (install cosign", "cosign not installed; checksums only"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestVerifyReleaseRefusesATamperedArchive(t *testing.T) {
	root, archive := vrRoot(t, "the release binary", false)
	archive[len(archive)-1] ^= 0xff
	if err := os.WriteFile(filepath.Join(root, "bin", "release", vrAsset), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := verifyRelease((&vrRun{}).io(root, &out, true))
	if err == nil || !strings.Contains(err.Error(), "does not match release/SHA256SUMS") {
		t.Fatalf("tampered archive: %v", err)
	}
}

func TestVerifyReleaseRefusesAReplacedBinary(t *testing.T) {
	root, _ := vrRoot(t, "something else", false)
	var out bytes.Buffer
	err := verifyRelease((&vrRun{}).io(root, &out, false))
	if err == nil || !strings.Contains(err.Error(), "replaced after the install") {
		t.Fatalf("replaced binary: %v", err)
	}
}

func TestVerifyReleaseRunsCosignWithTheWorkflowIdentity(t *testing.T) {
	root, _ := vrRoot(t, "the release binary", true)
	r := &vrRun{}
	var out bytes.Buffer
	if err := verifyRelease(r.io(root, &out, true)); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("cosign calls %v", r.calls)
	}
	got := strings.Join(r.calls[0], " ")
	for _, want := range []string{"/usr/local/bin/cosign verify-blob --bundle", vrAsset + ".sigstore.json",
		`--certificate-identity-regexp ^https://github\.com/acme/herdr-dots/\.github/workflows/release\.yml@refs/tags/v`,
		"--certificate-oidc-issuer https://token.actions.githubusercontent.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("cosign args lack %q:\n%s", want, got)
		}
	}
	r.err = fmt.Errorf("exit status 1")
	if err := verifyRelease(r.io(root, &out, true)); err == nil || !strings.Contains(err.Error(), "signature check failed") {
		t.Fatalf("a failed signature must fail: %v", err)
	}
}

func TestVerifyReleaseOnASourceBuild(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "INSTALL_SOURCE"), []byte("source=source\nversion=1.0.0-rc.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := verifyRelease((&vrRun{}).io(root, &out, true)); err != nil || !strings.Contains(out.String(), "Built from source") {
		t.Fatalf("source build: %v\n%s", err, out.String())
	}
	if err := verifyRelease((&vrRun{}).io(t.TempDir(), &out, true)); err == nil || !strings.Contains(err.Error(), "reinstall") {
		t.Fatalf("no record: %v", err)
	}
}

func TestVerifyReleaseRejectsLeftoverArgs(t *testing.T) {
	err := runVerifyRelease([]string{"--dir", t.TempDir(), "extra"})
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("leftover args: %v", err)
	}
}

// vrRewriteArchive replaces the kept archive (and its SHA256SUMS line) with
// one made of the given members, in order.
func vrRewriteArchive(t *testing.T, root string, members ...*tar.Header) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range members {
		body := h.Linkname
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
			h.Linkname = ""
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(buf.Bytes())
	if err := os.WriteFile(filepath.Join(root, "bin", "release", vrAsset), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "release", "SHA256SUMS"), []byte(hex.EncodeToString(s[:])+"  "+vrAsset+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tar -x keeps the last of two same-named members, so verify-release must
// compare against that one. (Test-only convention: a regular member's body is
// passed in Linkname.)
func TestVerifyReleaseHashesTheLastMemberLikeTar(t *testing.T) {
	reg := func(body string) *tar.Header {
		return &tar.Header{Name: "herdr-dots", Mode: 0o755, Typeflag: tar.TypeReg, Linkname: body}
	}
	root, _ := vrRoot(t, "second", false)
	vrRewriteArchive(t, root, reg("first"), reg("second"))
	var out bytes.Buffer
	if err := verifyRelease((&vrRun{}).io(root, &out, false)); err != nil {
		t.Fatalf("the binary tar extracts (the last member) must verify: %v", err)
	}
	root, _ = vrRoot(t, "first", false)
	vrRewriteArchive(t, root, reg("first"), reg("second"))
	if err := verifyRelease((&vrRun{}).io(root, &out, false)); err == nil || !strings.Contains(err.Error(), "replaced after the install") {
		t.Fatalf("the first of two members is not what tar installs: %v", err)
	}
	root, _ = vrRoot(t, "first", false)
	vrRewriteArchive(t, root, reg("first"), &tar.Header{Name: "herdr-dots", Typeflag: tar.TypeSymlink, Linkname: "/bin/sh"})
	if err := verifyRelease((&vrRun{}).io(root, &out, false)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a last member that is a symlink: %v", err)
	}
}

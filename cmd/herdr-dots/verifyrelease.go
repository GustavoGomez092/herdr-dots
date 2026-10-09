package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herdr-dots/internal/platform"
	"herdr-dots/internal/version"
)

func init() { commands["verify-release"] = runVerifyRelease }

// verifyIO is what verify-release touches, so tests can fake it.
type verifyIO struct {
	root     string // the plugin root (bin/, release/)
	out      io.Writer
	lookPath func(string) (string, error)
	run      func(name string, args ...string) (string, error) // combined output
}

var (
	vrAssetRe = regexp.MustCompile(`^herdr-dots_[0-9A-Za-z.+-]+_(darwin|linux|windows)_(amd64|arm64)\.(tar\.gz|zip)$`)
	vrRepoRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
)

// vrSigIssuer is the OIDC issuer of GitHub Actions' keyless signing certificates.
const vrSigIssuer = "https://token.actions.githubusercontent.com"

// vrCosignTimeout bounds the cosign child, which may query the transparency log.
const vrCosignTimeout = 2 * time.Minute

func runVerifyRelease(args []string) error {
	fs := flag.NewFlagSet("verify-release", flag.ContinueOnError)
	dir := fs.String("dir", "", "plugin root (default: the directory above this binary's bin/)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("verify-release: unexpected argument %q (usage: herdr-dots verify-release [--dir <plugin root>])", fs.Arg(0))
	}
	root := *dir
	if root == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		root = filepath.Dir(filepath.Dir(exe))
	}
	return verifyRelease(verifyIO{root: root, out: os.Stdout, lookPath: exec.LookPath,
		run: func(name string, a ...string) (string, error) {
			// Through internal/platform, like every child process (Phase 6's
			// portability guard): Windows .cmd shims, no console window.
			ctx, cancel := context.WithTimeout(context.Background(), vrCosignTimeout)
			defer cancel()
			cmd, err := platform.CommandContext(ctx, name, a)
			if err != nil {
				return "", err
			}
			cmd.Env = platform.ChildEnv(os.Environ())
			platform.HideWindow(cmd)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}})
}

// verifyRelease re-checks an installed release: the kept archive against
// release/SHA256SUMS, the installed binary against the one in the archive,
// and, when cosign is installed and the release had a signature bundle, the
// archive's keyless signature from the repository's release workflow.
func verifyRelease(v verifyIO) error {
	info, err := vrReadRecord(filepath.Join(v.root, "bin", "INSTALL_SOURCE"))
	if err != nil {
		return fmt.Errorf("no install record (bin/INSTALL_SOURCE): %w; reinstall the plugin", err)
	}
	fmt.Fprintln(v.out, version.String())
	if info["source"] != "release" {
		fmt.Fprintf(v.out, "Built from source (version %s): there is no release archive to verify.\n", info["version"])
		return nil
	}
	asset, repo := info["asset"], info["repo"]
	if !vrAssetRe.MatchString(asset) || !vrRepoRe.MatchString(repo) {
		return fmt.Errorf("bin/INSTALL_SOURCE names an unexpected asset %q or repository %q", asset, repo)
	}
	want, err := vrChecksum(filepath.Join(v.root, "release", "SHA256SUMS"), asset)
	if err != nil {
		return err
	}
	archive := filepath.Join(v.root, "bin", "release", asset)
	got, err := vrFileSHA256(archive)
	if err != nil {
		return fmt.Errorf("read the kept release archive: %w", err)
	}
	if got != want {
		return fmt.Errorf("%s: checksum %s does not match release/SHA256SUMS (%s)", asset, got, want)
	}
	fmt.Fprintf(v.out, "checksum ok: %s matches release/SHA256SUMS\n", asset)

	exe := "herdr-dots"
	if strings.HasSuffix(asset, ".zip") {
		exe = "herdr-dots.exe"
	}
	inArchive, err := vrMemberSHA256(archive, exe)
	if err != nil {
		return err
	}
	installed, err := vrFileSHA256(filepath.Join(v.root, "bin", exe))
	if err != nil {
		return fmt.Errorf("read bin/%s: %w", exe, err)
	}
	if installed != inArchive {
		return fmt.Errorf("bin/%s is not the binary in %s: it was replaced after the install", exe, asset)
	}
	fmt.Fprintf(v.out, "binary ok: bin/%s is the one in %s\n", exe, asset)

	bundle := archive + ".sigstore.json"
	cosign, err := v.lookPath("cosign")
	switch {
	case err != nil:
		fmt.Fprintln(v.out, "signature: not checked (install cosign): cosign not installed; checksums only")
	case !vrExists(bundle):
		fmt.Fprintln(v.out, "signature: not checked (the release had no .sigstore.json bundle)")
	default:
		identity := "^https://github\\.com/" + regexp.QuoteMeta(repo) + "/\\.github/workflows/release\\.yml@refs/tags/v"
		out, err := v.run(cosign, "verify-blob", "--bundle", bundle,
			"--certificate-identity-regexp", identity, "--certificate-oidc-issuer", vrSigIssuer, archive)
		if err != nil {
			return fmt.Errorf("signature check failed: %v\n%s", err, strings.TrimSpace(out))
		}
		fmt.Fprintf(v.out, "signature ok: signed by the release workflow of github.com/%s\n", repo)
	}
	return nil
}

func vrExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// vrReadRecord reads key=value lines.
func vrReadRecord(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok {
			m[k] = val
		}
	}
	return m, sc.Err()
}

// vrChecksum finds asset's line in a SHA256SUMS file ("<hex>  <name>").
func vrChecksum(path, asset string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("release/SHA256SUMS has no checksum for %s", asset)
}

func vrFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// vrMemberSHA256 hashes the file called name at the top of a .tar.gz or .zip.
// When the name appears more than once it hashes the LAST one, which is what
// extraction leaves on disk, and that last member must be a regular file.
func vrMemberSHA256(archive, name string) (string, error) {
	sum, found, regular := "", false, false
	hashOf := func(r io.Reader) (string, error) {
		h := sha256.New()
		if _, err := io.Copy(h, r); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			found, regular, sum = true, f.Mode().IsRegular(), ""
			if !regular {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			sum, err = hashOf(rc)
			rc.Close()
			if err != nil {
				return "", err
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return "", err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		tr := tar.NewReader(gz)
		for {
			hd, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", err
			}
			if hd.Name != name {
				continue
			}
			found, regular, sum = true, hd.Typeflag == tar.TypeReg, ""
			if regular {
				if sum, err = hashOf(tr); err != nil {
					return "", err
				}
			}
		}
	}
	switch {
	case !found:
		return "", fmt.Errorf("%s has no %s", filepath.Base(archive), name)
	case !regular:
		return "", fmt.Errorf("%s: %s is not a regular file", filepath.Base(archive), name)
	}
	return sum, nil
}

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/config"
)

// Ruling P10-R9: a commit review never sends the content of a file the
// deny set covers (toolloop.DefaultDeny, DraftDeny and config
// tool_deny_globs, case-folded). The file list may name it; the patch leaves
// its hunks out and says how many files were left out.
func TestCommitReviewLeavesDeniedFilesOut(t *testing.T) {
	r, repo := commitRigWith(t, proOpts{cfg: func(c *config.Config) { c.ToolDenyGlobs = []string{"*.vault"} }}, commitDot("Tests"))
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		".env":          "API_TOKEN=envsecretcontent\n",
		"certs/TLS.PEM": "pemsecretcontent\n",
		"prod.vault":    "vaultsecretcontent\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	head := proCommit(t, repo, filepath.Join("src", "a.go"), "package a\n", "add a.go and secrets")
	// From a pane in a subdirectory too: the paths are the repository's.
	sub, err := r.e.commitContext(filepath.Join(repo, "src"), head)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sub, "envsecretcontent") || !strings.Contains(sub, "+package a") || !strings.Contains(sub, "3 denied file(s)") {
		t.Fatalf("from a subdirectory:\n%s", sub)
	}
	r.checkHeads(t)
	eventually(t, "a commit review", func() bool { return len(commitRuns(r)) == 1 })
	q := commitRuns(r)[0].Question
	for _, secret := range []string{"envsecretcontent", "pemsecretcontent", "vaultsecretcontent"} {
		if strings.Contains(q, secret) {
			t.Errorf("the review got a denied file's content (%s):\n%s", secret, q)
		}
	}
	for _, want := range []string{"+package a", "3 denied file(s) left out of this review"} {
		if !strings.Contains(q, want) {
			t.Errorf("the review lacks %q:\n%s", want, q)
		}
	}
}

// An invalid tool_deny_globs entry means the deny set is unknown: the patch
// is left out entirely (the message and file list still go).
func TestCommitReviewWithAnInvalidDenyGlobSendsNoPatch(t *testing.T) {
	r, repo := commitRigWith(t, proOpts{cfg: func(c *config.Config) { c.InvalidDenyGlobs = []string{"[unclosed"} }}, commitDot("Tests"))
	proCommit(t, repo, "auth.go", "package auth\n\nfunc Secretish() {}\n", "add Secretish")
	r.checkHeads(t)
	eventually(t, "a commit review", func() bool { return len(commitRuns(r)) == 1 })
	q := commitRuns(r)[0].Question
	if strings.Contains(q, "+func Secretish") || !strings.Contains(q, "add Secretish") || !strings.Contains(q, "patch left out") {
		t.Fatalf("question:\n%s", q)
	}
}

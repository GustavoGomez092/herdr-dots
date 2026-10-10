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

// S25 (P10-R9 rename gap): a denied file renamed in the commit to a
// non-denied name is denied too. Its new name may be listed, but no line of
// its content goes in the review: not as the rename's context lines, and
// not as a new file when other denied files force the pathspec route.
func TestCommitReviewLeavesARenamedDeniedFileOut(t *testing.T) {
	r, repo := commitRigWith(t, proOpts{}, commitDot("Tests"))
	secret := "API_TOKEN=renamedsecretcontent\nLINE2=keep\nLINE3=keep\nLINE4=keep\n"
	proCommit(t, repo, ".env", secret, "add env")

	// A rename with an edit: its patch would carry the old lines as context.
	proGit(t, repo, "mv", ".env", "notes.txt")
	head := proCommit(t, repo, "notes.txt", secret+"LINE5=new\n", "rename env with an edit")
	q, err := r.e.commitContext(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "renamedsecretcontent") || !strings.Contains(q, "1 denied file(s)") {
		t.Fatalf("an edited rename of a denied file:\n%s", q)
	}

	// A pure rename next to another denied file: the pathspec route would
	// show the new name as a whole new file.
	proCommit(t, repo, ".env.prod", "API_TOKEN=puresecretcontent\n", "add env.prod")
	proGit(t, repo, "mv", ".env.prod", "notes2.txt")
	if err := os.WriteFile(filepath.Join(repo, "tls.pem"), []byte("pemsecretcontent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	head = proCommit(t, repo, "b.go", "package b\n", "pure rename next to a denied file")
	q, err = r.e.commitContext(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "puresecretcontent") || strings.Contains(q, "pemsecretcontent") ||
		!strings.Contains(q, "+package b") || !strings.Contains(q, "2 denied file(s)") {
		t.Fatalf("a pure rename of a denied file next to a denied file:\n%s", q)
	}
}

// parseNameStatus pairs a rename's or copy's source with its destination and
// drops a record the output cap cut short.
func TestParseNameStatus(t *testing.T) {
	got := parseNameStatus("M\x00a.go\x00R086\x00.env\x00notes.txt\x00C100\x00k.pem\x00copy.txt\x00D\x00old.go\x00R100\x00x.key\x00cut")
	want := []commitChange{{path: "a.go"}, {from: ".env", path: "notes.txt"}, {from: "k.pem", path: "copy.txt"}, {path: "old.go"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
	if got := parseNameStatus("R100\x00x.key\x00"); len(got) != 0 {
		t.Fatalf("a rename without its destination: %+v", got)
	}
	if got := parseNameStatus("A\x00"); len(got) != 0 {
		t.Fatalf("a status without its path: %+v", got)
	}
}

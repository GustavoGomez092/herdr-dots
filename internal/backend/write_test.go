package backend

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func writeReq() Request {
	r := req()
	r.Pass, r.Write, r.Cwd = PassWrite, true, "/state/worktrees/0123456789abcdef"
	r.Question = "Move the JWT out of localStorage."
	r.Memory = "MEMORY (reference data):\n- d:m1 Tokens live in httpOnly cookies."
	return r
}

func TestClaudeDraftGetsFileToolsAndNoShell(t *testing.T) {
	cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(writeReq())
	for flag, want := range map[string]string{
		"--tools":           "Read,Grep,Glob,Edit,Write",
		"--allowedTools":    "Read,Grep,Glob", // never Edit or Write: a bare allow rule approves any path
		"--permission-mode": "acceptEdits",
		"--disallowedTools": "Bash,WebFetch,WebSearch",
		"--settings":        `{"disableAllHooks":true}`,
	} {
		if got, _ := argAfter(cmd.Args, flag); got != want {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}
	if cmd.Dir != "/state/worktrees/0123456789abcdef" {
		t.Fatalf("a draft runs in its worktree, got %q", cmd.Dir)
	}
	sys, _ := argAfter(cmd.Args, "--append-system-prompt")
	for _, want := range []string{"isolated copy of the repository", "Do not run commands", `"message": "<what you changed and why`} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if strings.Contains(sys, "You are read-only") || strings.Contains(sys, "MEMORY") {
		t.Error("the draft contract replaces the review contract, and memory never goes in the system prompt")
	}
	if !strings.HasPrefix(cmd.Stdin, "MEMORY (reference data):") || !strings.Contains(cmd.Stdin, "FIX REQUESTED BY THE DEVELOPER:\nMove the JWT out of localStorage.") {
		t.Fatalf("user prompt:\n%s", cmd.Stdin)
	}
}

func TestWriteToolsNeedBothWriteAndPassWrite(t *testing.T) {
	half := []Request{writeReq(), writeReq()}
	half[0].Pass = PassReview // Write without the write pass
	half[1].Write = false     // the write pass without Write
	for i, r := range half {
		cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
		if v, _ := argAfter(cmd.Args, "--tools"); v != "Read,Grep,Glob" || slices.Contains(cmd.Args, "--permission-mode") {
			t.Errorf("case %d: %v must stay read-only", i, cmd.Args)
		}
	}
}

// Review Focus: no combination of request fields and hostile dot args ever
// yields a shell, bypassPermissions or another writable directory.
func TestNoClaudeRunEverBypassesPermissions(t *testing.T) {
	hostile := []string{"--permission-mode", "bypassPermissions", "--dangerously-skip-permissions", "--add-dir", "/",
		"--tools", "Bash", "--allowedTools=Bash", "--disallowedTools", "", "--settings", "{}"}
	for _, pass := range []string{PassReview, PassMemory, PassWrite} {
		for _, write := range []bool{false, true} {
			for _, repo := range []bool{false, true} {
				for _, args := range [][]string{nil, hostile} {
					r := req()
					r.Pass, r.Write, r.RepoRead, r.Args = pass, write, repo, args
					cmd := ClaudeCLI{Environ: func() []string { return nil }}.BuildCmd(r)
					joined := strings.Join(cmd.Args, "\x00")
					for _, banned := range []string{"bypassPermissions", "--dangerously-skip-permissions", "--add-dir", "--allow-dangerously-skip-permissions"} {
						if strings.Contains(joined, banned) {
							t.Errorf("pass %q write %v repo %v: %s in %v", pass, write, repo, banned, cmd.Args)
						}
					}
					tools, _ := argAfter(cmd.Args, "--tools")
					if strings.Contains(tools, "Bash") || strings.Count(joined, "--tools\x00") != 1 {
						t.Errorf("pass %q write %v: tools %q in %v", pass, write, tools, cmd.Args)
					}
					if allowed, _ := argAfter(cmd.Args, "--allowedTools"); strings.Contains(allowed, "Edit") || strings.Contains(allowed, "Write") {
						t.Errorf("pass %q write %v: --allowedTools %q pre-approves edits anywhere", pass, write, allowed)
					}
					mode, has := argAfter(cmd.Args, "--permission-mode")
					if has != (pass == PassWrite && write) || (has && mode != "acceptEdits") {
						t.Errorf("pass %q write %v: permission mode %q (%v)", pass, write, mode, has)
					}
				}
			}
		}
	}
}

func TestCodexNeverDrafts(t *testing.T) {
	run := &fakeRunner{}
	_, err := CodexCLI{Runner: run, Environ: func() []string { return nil }}.Run(context.Background(), writeReq())
	if !errors.Is(err, ErrCodexCannotDraft) || run.got.Name != "" {
		t.Fatalf("err %v, runner called with %+v", err, run.got)
	}
	cmd := CodexCLI{Environ: func() []string { return nil }}.BuildCmd(writeReq())
	if v, _ := argAfter(cmd.Args, "--sandbox"); v != "read-only" {
		t.Fatalf("codex keeps its read-only sandbox, got %q", v)
	}
}

func TestReviewAndMemoryPromptsAreUnchangedByPhase7(t *testing.T) {
	r := req()
	r.Question = "Is this safe?"
	if s := SystemPrompt(r); !strings.Contains(s, "You are read-only.") || strings.Contains(s, "DRAFTING") {
		t.Fatalf("review prompt:\n%s", s)
	}
	if u := UserPrompt(r); !strings.Contains(u, "QUESTION FROM THE DEVELOPER:\nIs this safe?") {
		t.Fatalf("review user prompt:\n%s", u)
	}
	r.Pass = PassMemory
	if s := SystemPrompt(r); strings.Contains(s, "DRAFTING") {
		t.Fatalf("memory prompt:\n%s", s)
	}
}

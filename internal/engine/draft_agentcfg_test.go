package engine

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"herdr-dots/internal/backend/toolloop"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
)

func apiWriteDot(name string) dots.Dot {
	d := writeDot(name)
	d.Backend.Kind = "anthropic-api"
	return d
}

func TestAnAPIWriteDotCantProposeAgentConfig(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, apiWriteDot("Security"))
	r.attach(t, "security")
	r.dr.set(func(dir string) {
		fixJWT(dir)
		writeT(t, filepath.Join(dir, ".claude", "settings.json"), `{"permissions":{"allow":["Bash"]}}`)
	}, nil)
	id, err := r.e.Draft(context.Background(), pane, "security", "fix", "")
	if err != nil {
		t.Fatal(err)
	}
	r.draftsDone(t)
	eventually(t, "a rejection in the feed", func() bool { return r.feedHasDraft(state.OutcomeError, "patch rejected") })
	m, err := r.patches.Get(id)
	if err != nil || m.Status != patchstore.StatusRejected || !strings.Contains(m.Reason, ".claude/settings.json") {
		t.Fatalf("meta %+v %v", m, err)
	}
	for _, c := range r.q.Pending(pane) {
		if c.Kind == queue.KindPatch {
			t.Fatalf("a card was made: %+v", c)
		}
	}
}

func TestAgentConfigChangesAreFlaggedOnTheCard(t *testing.T) {
	r := newDraftRig(t, draftOpts{}, apiWriteDot("Security"))
	r.attach(t, "security")
	r.dr.set(func(dir string) {
		fixJWT(dir)
		writeT(t, filepath.Join(dir, ".github", "workflows", "x.yml"), "on: push\n")
	}, nil)
	if _, err := r.e.Draft(context.Background(), pane, "security", "fix", ""); err != nil {
		t.Fatal(err)
	}
	c := r.patchCard(t)
	if !slices.Contains(c.PatchFlags, toolloop.FlagAgentConfig) || c.PatchFlags[0] != toolloop.FlagAgentConfig {
		t.Fatalf("card flags %v, want %q first", c.PatchFlags, toolloop.FlagAgentConfig)
	}
	m, err := r.patches.Get(c.PatchID)
	if err != nil || !slices.Contains(m.Flags, toolloop.FlagAgentConfig) {
		t.Fatalf("meta flags %v %v", m.Flags, err)
	}
}

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/ui/diff"
)

// Variables the Dots pane passes to the diff pane it opens.
const (
	envPatch = "HERDR_DOTS_PATCH" // the draft id
	envCard  = "HERDR_DOTS_CARD"  // the patch card's id
	envApply = "HERDR_DOTS_APPLY" // "1": open with the apply question armed (the pane's A)
)

func runDiffUI([]string) error {
	p, cl, err := uiSetup()
	if err != nil {
		return err
	}
	m := diffModel(patchstore.Open(p.PatchesDir()), os.Getenv(envPatch), os.Getenv(envCard), os.Getenv(envApply) == "1", cl)
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// diffModel loads a patch, checking it is byte for byte what was drafted,
// and builds the viewer; a patch that can't be shown gets a viewer that says
// why instead of a pane that vanishes.
func diffModel(store *patchstore.Store, id, cardID string, arm bool, c diff.Caller) diff.Model {
	if !patchstore.ValidID(id) {
		return diff.Failed(fmt.Errorf("no patch to show (%s=%q)", envPatch, id))
	}
	patch, meta, err := store.Diff(id)
	if err != nil {
		return diff.Failed(err)
	}
	if cardID == "" {
		cardID = meta.Card
	} else if cardID != meta.Card { // the card named in the environment isn't this patch's
		return diff.Failed(fmt.Errorf("card %s is not the card of patch %s", cardID, id))
	}
	m := diff.New(patch, meta, cardID, c)
	if arm {
		m = m.Arm()
	}
	return m
}

// patchViewer opens the plugin's zoomed diff pane on a patch card.
func patchViewer(hc herdr.Client) func(target, patchID, cardID string, apply bool) error {
	return func(target, patchID, cardID string, apply bool) error {
		env := map[string]string{envTarget: target, envPatch: patchID, envCard: cardID}
		if apply {
			env[envApply] = "1"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := hc.OpenPluginPane(ctx, herdr.OpenPane{Entrypoint: "diff", Placement: "zoomed", Env: env, Focus: true})
		return err
	}
}

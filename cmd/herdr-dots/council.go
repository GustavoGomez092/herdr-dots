package main

import (
	"context"
	"errors"
	"time"

	"herdr-dots/internal/herdr"
)

// councilPopup opens the manifest's "council" popup (summon-ui --council)
// for a main agent pane.
func councilPopup(target string) herdr.OpenPane {
	return herdr.OpenPane{Entrypoint: "council", Placement: "popup", Width: "70%", Height: 16,
		Env: map[string]string{envTarget: target}, Focus: true}
}

// openCouncil opens the council popup for target. The action and the Dots
// pane's C key both come here.
func openCouncil(ctx context.Context, hc herdr.Client, target string) error {
	if target == "" {
		return errors.New("focus an agent pane first")
	}
	_, err := hc.OpenPluginPane(ctx, councilPopup(target))
	return err
}

// paneOpener is the Dots pane's C key. The pane process runs inside Herdr, so
// it reaches Herdr's socket from its own environment.
func paneOpener(target string) error {
	hc, err := herdr.NewFromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return openCouncil(ctx, hc, target)
}

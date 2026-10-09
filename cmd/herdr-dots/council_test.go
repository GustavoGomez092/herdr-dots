package main

import (
	"context"
	"slices"
	"testing"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/keys"
)

func TestOpenCouncilOpensThePopupForTheTarget(t *testing.T) {
	srv := herdrtest.New(t)
	hc := &herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"}
	if err := openCouncil(context.Background(), hc, "w1:p1"); err != nil {
		t.Fatal(err)
	}
	calls := srv.Calls("plugin.pane.open")
	if len(calls) != 1 {
		t.Fatalf("calls %+v", calls)
	}
	p := calls[0].Params
	env, _ := p["env"].(map[string]any)
	if p["entrypoint"] != "council" || p["placement"] != "popup" || p["width"] != "70%" || p["height"] != float64(16) ||
		p["focus"] != true || env[envTarget] != "w1:p1" {
		t.Fatalf("params %+v", p)
	}
	if err := openCouncil(context.Background(), hc, ""); err == nil {
		t.Fatal("no target: refuse")
	}
}

type manifest struct {
	Actions []struct {
		ID      string   `toml:"id"`
		Command []string `toml:"command"`
	} `toml:"actions"`
	Panes []struct {
		ID        string   `toml:"id"`
		Placement string   `toml:"placement"`
		Width     any      `toml:"width"`
		Height    any      `toml:"height"`
		Command   []string `toml:"command"`
	} `toml:"panes"`
}

func TestManifestDeclaresTheCouncilActionAndPopup(t *testing.T) {
	var m manifest
	if _, err := toml.DecodeFile("../../herdr-plugin.toml", &m); err != nil {
		t.Fatal(err)
	}
	actions := map[string][]string{}
	for _, a := range m.Actions {
		actions[a.ID] = a.Command
	}
	if !slices.Equal(actions["council"], []string{"./bin/herdr-dots", "action", "council"}) {
		t.Fatalf("council action %v", actions["council"])
	}
	for _, b := range keys.Defaults() {
		id := b.Action[len("herdr-dots."):]
		if _, ok := actions[id]; !ok {
			t.Errorf("key %s runs %s, which the manifest doesn't declare", b.Key, b.Action)
		}
	}
	want := councilPopup("x")
	for _, p := range m.Panes {
		if p.ID != want.Entrypoint {
			continue
		}
		if p.Placement != want.Placement || p.Width != want.Width || p.Height != int64(16) ||
			!slices.Equal(p.Command, []string{"./bin/herdr-dots", "summon-ui", "--council"}) {
			t.Fatalf("council pane %+v", p)
		}
		return
	}
	t.Fatal("no [[panes]] entry with id \"council\"")
}

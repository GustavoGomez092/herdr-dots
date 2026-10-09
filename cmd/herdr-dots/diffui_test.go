package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/herdr"
	"herdr-dots/internal/herdr/herdrtest"
	"herdr-dots/internal/patchstore"
)

func TestDiffModelVerifiesThePatchItShows(t *testing.T) {
	store := patchstore.Open(filepath.Join(t.TempDir(), "patches"))
	const id = "0123456789abcdef"
	diff := []byte("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n")
	if err := store.PutPatch(patchstore.Meta{ID: id, Dot: "security", Card: "7", Stat: "1 file +1 −1", Paths: []string{"x.go"}, Status: patchstore.StatusReady}, diff); err != nil {
		t.Fatal(err)
	}
	if v := diffModel(store, id, "", true, nil).View(); !strings.Contains(v, "+b") || !strings.Contains(v, "(y/N)") {
		t.Fatalf("armed viewer:\n%s", v)
	}
	if v := diffModel(store, "../../etc", "", false, nil).View(); !strings.Contains(v, "no patch to show") {
		t.Fatalf("bad id:\n%s", v)
	}
	if err := os.WriteFile(store.PatchPath(id), []byte("diff --git a/x.go b/x.go\n+evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := diffModel(store, id, "", false, nil).View(); !strings.Contains(v, "changed since it was drafted") {
		t.Fatalf("swapped file:\n%s", v)
	}
}

func TestPatchViewerOpensTheZoomedDiffPane(t *testing.T) {
	srv := herdrtest.New(t)
	open := patchViewer(&herdr.SocketClient{Path: srv.Path, PluginID: "herdr-dots"})
	if err := open("w1:p1", "0123456789abcdef", "7", true); err != nil {
		t.Fatal(err)
	}
	calls := srv.Calls("plugin.pane.open")
	if len(calls) != 1 {
		t.Fatalf("calls %+v", calls)
	}
	p := calls[0].Params
	env, _ := p["env"].(map[string]any)
	if p["entrypoint"] != "diff" || p["placement"] != "zoomed" || env[envPatch] != "0123456789abcdef" || env[envCard] != "7" || env[envApply] != "1" || env[envTarget] != "w1:p1" {
		t.Fatalf("params %+v", p)
	}
}

func TestDiffUIIsRoutedAndInTheManifest(t *testing.T) {
	if commands["diff-ui"] == nil {
		t.Fatal("diff-ui is not a command")
	}
	var m struct {
		Panes []struct {
			ID        string   `toml:"id"`
			Placement string   `toml:"placement"`
			Command   []string `toml:"command"`
		} `toml:"panes"`
	}
	if _, err := toml.DecodeFile(filepath.Join("..", "..", "herdr-plugin.toml"), &m); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Panes {
		if p.ID == "diff" {
			if p.Placement != "zoomed" || strings.Join(p.Command, " ") != "./bin/herdr-dots diff-ui" {
				t.Fatalf("diff pane %+v", p)
			}
			return
		}
	}
	t.Fatal("no diff pane in herdr-plugin.toml")
}

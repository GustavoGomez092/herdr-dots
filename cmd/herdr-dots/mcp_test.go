package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/ipc"
	"herdr-dots/internal/mcpserver"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/view"
)

// mcpDirs makes config and state dirs and the server's arguments.
func mcpDirs(t *testing.T) (cfg, state string, args []string) {
	t.Helper()
	root := t.TempDir()
	cfg, state = filepath.Join(root, "cfg"), filepath.Join(root, "state")
	return cfg, state, []string{"--config-dir", cfg, "--state-dir", state}
}

func mcpEnvOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func mcpNoEnsure(t *testing.T) func(paths.Paths) error {
	return func(paths.Paths) error { t.Error("the engine must not be started"); return nil }
}

// mcpSession runs serveMCP on in-memory transports and connects a client.
func mcpSession(t *testing.T, args []string, getenv func(string) string, ensure func(paths.Paths) error) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ct, st := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- serveMCP(ctx, args, getenv, st, ensure) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("serveMCP did not stop")
		}
	})
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func mcpToolText(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

// Review Focus 1: a codex dot run has no HERDR_PANE_ID (CleanEnv), so the
// server it spawns refuses every tool and says so in mcp.log.
func TestServeMCPOutsideHerdrRefusesEveryTool(t *testing.T) {
	_, state, args := mcpDirs(t)
	cs := mcpSession(t, args, mcpEnvOf(map[string]string{"HERDR_SOCKET_PATH": "/tmp/herdr.sock"}), mcpNoEnsure(t))
	text, isErr := mcpToolText(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "q"})
	if !isErr || text != mcpserver.NotInHerdr {
		t.Fatalf("isError=%v %q", isErr, text)
	}
	key := paths.SessionKey("/tmp/herdr.sock")
	b, err := os.ReadFile(filepath.Join(state, key, "mcp.log"))
	if err != nil || !strings.Contains(string(b), "outside a Herdr pane") || !strings.Contains(string(b), "ask_dot refused") {
		t.Fatalf("mcp.log %q %v", b, err)
	}
}

func TestServeMCPTreatsUnexpandedPlaceholdersAsUnset(t *testing.T) {
	_, _, args := mcpDirs(t)
	env := mcpEnvOf(map[string]string{"HERDR_PANE_ID": "${HERDR_PANE_ID}", "HERDR_SOCKET_PATH": "${HERDR_SOCKET_PATH:-}"})
	cs := mcpSession(t, args, env, mcpNoEnsure(t))
	if text, isErr := mcpToolText(t, cs, "list_dots", map[string]any{}); !isErr || text != mcpserver.NotInHerdr {
		t.Fatalf("isError=%v %q", isErr, text)
	}
}

func TestServeMCPWritesNothingToStdoutWithoutAClient(t *testing.T) {
	_, _, args := mcpDirs(t)
	stdinR, stdinW := io.Pipe()
	stdinW.Close() // the client went away before saying anything
	var stdout bytes.Buffer
	err := serveMCP(context.Background(), args, mcpEnvOf(nil), &mcp.IOTransport{Reader: stdinR, Writer: mcpNopCloser{&stdout}}, mcpNoEnsure(t))
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must carry protocol only: %q", stdout.String())
	}
}

type mcpNopCloser struct{ io.Writer }

func (mcpNopCloser) Close() error { return nil }

// A config.toml that can't be parsed fails closed: only list_dots is offered,
// as inject and memory turn off (reconciled; config fails closed since v1's
// review fixes).
func TestServeMCPFailsClosedOnABrokenConfig(t *testing.T) {
	cfg, _, args := mcpDirs(t)
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("[mcp\ntools = "), 0o600); err != nil {
		t.Fatal(err)
	}
	cs := mcpSession(t, args, mcpEnvOf(nil), mcpNoEnsure(t))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "list_dots" {
		t.Fatalf("tools %+v", res.Tools)
	}
}

func TestServeMCPNeedsAbsoluteDirs(t *testing.T) {
	err := serveMCP(context.Background(), []string{"--config-dir", "cfg", "--state-dir", "/s"}, mcpEnvOf(nil), &mcp.StdioTransport{}, mcpNoEnsure(t))
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("got %v", err)
	}
}

// mcpFakeEngine answers queries for one pane on the engine socket.
type mcpFakeEngine struct{ asked chan ipc.AskArgs }

func (f mcpFakeEngine) Handle(context.Context, string, json.RawMessage) error { return nil }
func (f mcpFakeEngine) Snapshot() view.Snapshot                               { return view.Snapshot{} }
func (f mcpFakeEngine) Watch() (<-chan view.Snapshot, func()) {
	return make(chan view.Snapshot), func() {}
}
func (f mcpFakeEngine) Query(_ context.Context, cmd string, raw json.RawMessage) (any, error) {
	if cmd != ipc.CmdAsk {
		return nil, errors.New("unexpected " + cmd)
	}
	var a ipc.AskArgs
	_ = json.Unmarshal(raw, &a)
	f.asked <- a
	return ipc.AskResult{Slug: a.Slug, Name: "Security", Relevant: true, Severity: "warn", Message: "Use httpOnly cookies."}, nil
}

func TestServeMCPAsksThePanesEngine(t *testing.T) {
	cfg, state, args := mcpDirs(t)
	sock := "/tmp/herdr-mcp-test-" + filepath.Base(t.TempDir()) + ".sock" // only hashed into the session key
	p := paths.New(cfg, state, sock)
	fe := mcpFakeEngine{asked: make(chan ipc.AskArgs, 1)}
	ensured := 0
	ensure := func(got paths.Paths) error {
		ensured++
		if got.EngineSock() != p.EngineSock() {
			t.Errorf("engine socket %s, want %s", got.EngineSock(), p.EngineSock())
		}
		ln, err := net.Listen("unix", got.EngineSock())
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go ipc.Serve(ctx, ln, fe)
		return nil
	}
	cs := mcpSession(t, args, mcpEnvOf(map[string]string{"HERDR_PANE_ID": "w1:p1", "HERDR_SOCKET_PATH": sock}), ensure)
	text, isErr := mcpToolText(t, cs, "ask_dot", map[string]any{"slug": "security", "question": "JWT storage?"})
	if isErr || !strings.HasPrefix(text, "[◆ Security] (warn) Use httpOnly cookies.") {
		t.Fatalf("isError=%v %q", isErr, text)
	}
	if a := <-fe.asked; a != (ipc.AskArgs{PaneID: "w1:p1", Slug: "security", Question: "JWT storage?", IncludeTurns: true}) {
		t.Fatalf("engine got %+v", a)
	}
	if ensured != 1 {
		t.Fatalf("ensure ran %d times", ensured)
	}
	t.Cleanup(func() { os.Remove(p.EngineSock()) })
}

func TestEngineEnvSetsThePluginDirs(t *testing.T) {
	p := paths.New("/cfg", "/state", "/run/herdr.sock")
	env := engineEnv([]string{"PATH=/bin", "HERDR_PLUGIN_CONFIG_DIR=/old", "HERDR_PANE_ID=w1:p1"}, p)
	for _, want := range []string{"PATH=/bin", "HERDR_PLUGIN_CONFIG_DIR=/cfg", "HERDR_PLUGIN_STATE_DIR=/state", "HERDR_SOCKET_PATH=/run/herdr.sock"} {
		if !slices.Contains(env, want) {
			t.Errorf("missing %s in %v", want, env)
		}
	}
	if slices.Contains(env, "HERDR_PLUGIN_CONFIG_DIR=/old") {
		t.Fatalf("stale dir kept: %v", env)
	}
}

func TestDispatchKnowsTheMCPCommands(t *testing.T) {
	for _, name := range []string{"mcp", "setup-mcp"} {
		if _, ok := commands[name]; !ok {
			t.Errorf("%s is not routed", name)
		}
	}
}

// An unreadable config (a non-ENOENT read error: a directory sits where
// config.toml should be) fails closed exactly like a malformed one.
func TestServeMCPFailsClosedOnAnUnreadableConfig(t *testing.T) {
	cfg, _, args := mcpDirs(t)
	if err := os.MkdirAll(filepath.Join(cfg, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	cs := mcpSession(t, args, mcpEnvOf(nil), mcpNoEnsure(t))
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "list_dots" {
		t.Fatalf("tools %+v", res.Tools)
	}
}

// The config warning goes to mcp.log, never to stdout.
func TestServeMCPStartupWarningStaysOffStdout(t *testing.T) {
	cfg, state, args := mcpDirs(t)
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("[mcp\ntools = "), 0o600); err != nil {
		t.Fatal(err)
	}
	stdinR, stdinW := io.Pipe()
	stdinW.Close()
	var stdout bytes.Buffer
	if err := serveMCP(context.Background(), args, mcpEnvOf(nil), &mcp.IOTransport{Reader: stdinR, Writer: mcpNopCloser{&stdout}}, mcpNoEnsure(t)); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must carry protocol only: %q", stdout.String())
	}
	b, err := os.ReadFile(filepath.Join(state, "mcp.log"))
	if err != nil || !strings.Contains(string(b), "offering only list_dots") {
		t.Fatalf("mcp.log %q %v", b, err)
	}
}

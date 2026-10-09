package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"herdr-dots/internal/config"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/mcpserver"
	"herdr-dots/internal/paths"
)

// runMCP serves the Dots MCP server on stdio. Claude Code and Codex start it
// (herdr-dots setup-mcp writes their config). Stdout carries only protocol
// messages; logs go to mcp.log in the session's state folder.
func runMCP(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return serveMCP(ctx, args, os.Getenv, &mcp.StdioTransport{}, ensureEngine)
}

// serveMCP is runMCP with its environment, transport and engine starter
// injected (tests never start a real engine).
func serveMCP(ctx context.Context, args []string, getenv func(string) string, t mcp.Transport, ensure func(paths.Paths) error) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr) // never stdout: it is the protocol
	configDir := fs.String("config-dir", "", "the plugin's config dir (absolute)")
	stateDir := fs.String("state-dir", "", "the plugin's state dir (absolute)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !filepath.IsAbs(*configDir) || !filepath.IsAbs(*stateDir) {
		return errors.New("mcp needs absolute --config-dir and --state-dir (herdr-dots setup-mcp writes them)")
	}
	pane, sock := herdrEnv(getenv("HERDR_PANE_ID")), herdrEnv(getenv("HERDR_SOCKET_PATH"))
	p := paths.New(filepath.Clean(*configDir), filepath.Clean(*stateDir), sock)
	logger, closeLog := mcpLogger(p)
	defer closeLog()
	// The config is read once, here: an edit to config.toml needs a server
	// restart (restart the agent) to change the tools offered.
	// Key on err, never on the returned values: they look like a healthy default.
	cfg, err := config.Load(p.ConfigFile())
	tools := cfg.MCP.Normalized().Tools
	if err != nil {
		// Fail closed, like inject and memory (config.Load turns both off):
		// a config.toml that can't be read can't say which tools the
		// developer allowed, so only list_dots is offered (reconciled).
		tools = []string{"list_dots"}
		logger.Printf("config: %v (offering only list_dots until it is fixed; restart the agent afterwards)", err)
	}
	run := cfg.RunTimeout()
	deps := mcpserver.Deps{
		Tools:          tools,
		AskTimeout:     run + 10*time.Second,
		CouncilTimeout: 3*run + 10*time.Second, // two member rounds plus the chair, at worst
		Logf:           logger.Printf,
	}
	if pane == "" || sock == "" {
		logger.Printf("started outside a Herdr pane (HERDR_PANE_ID %q, HERDR_SOCKET_PATH set: %v): every tool refuses", pane, sock != "")
	} else {
		deps.PaneID = pane
		deps.Engine = mcpserver.NewIPCEngine(ipc.Client{Path: p.EngineSock()}, pane, func() error {
			if err := p.Ensure(); err != nil {
				return err
			}
			return ensure(p)
		})
		deps.Memory = memoryStoreFor(p, cfg, logger.Printf)
		logger.Printf("serving pane %s through %s", pane, p.EngineSock())
	}
	err = mcpserver.New(deps).Run(ctx, t)
	logger.Printf("stopped: %v", err)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// herdrEnv reads a Herdr variable the client forwarded. An unexpanded
// "${VAR}" (a client without variable expansion) counts as unset.
func herdrEnv(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "${") {
		return ""
	}
	return v
}

// mcpLogger opens SessionDir/mcp.log (StateDir/mcp.log outside Herdr),
// rotated like engine.log. Logging never fails the server.
func mcpLogger(p paths.Paths) (*log.Logger, func()) {
	dir := p.StateDir
	if p.SocketPath != "" {
		dir = p.SessionDir
	}
	discard := log.New(io.Discard, "", 0)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return discard, func() {}
	}
	f, err := openEngineLog(filepath.Join(dir, "mcp.log"))
	if err != nil {
		return discard, func() {}
	}
	return log.New(f, "herdr-dots mcp ", log.LstdFlags), func() { f.Close() }
}

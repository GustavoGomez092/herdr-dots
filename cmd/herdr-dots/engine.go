package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/engine"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/history"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/patchstore"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/platform"
	"herdr-dots/internal/projectdots"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
	"herdr-dots/internal/worktree"
)

const maxLogBytes = 5 << 20

func runEngine(args []string) error {
	fs := flag.NewFlagSet("engine", flag.ContinueOnError)
	ensure := fs.Bool("ensure", false, "start the engine in the background if it isn't running, then exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := paths.FromEnv()
	if err != nil {
		return err
	}
	if err := p.Ensure(); err != nil {
		return err
	}
	if *ensure {
		return ensureEngine(p)
	}
	return serveEngine(p)
}

// ensureEngine starts a detached engine for this Herdr session unless one
// already holds the lock, then waits until its endpoint accepts connections.
func ensureEngine(p paths.Paths) error {
	if lockfile.Held(p.EngineLock()) {
		return waitForEndpoint(p.EngineEndpoint(), 3*time.Second)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := openEngineLog(p.EngineLog())
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "engine")
	cmd.Env = platform.ChildEnv(engineEnv(os.Environ(), p))
	// Don't inherit the spawner's cwd: dots with an empty attachment cwd would read it.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cmd.Dir = home
	} else {
		cmd.Dir = "/"
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	// Survive the hook/pane that started us: a new session on Unix; a hidden
	// console and process group (out of any job that allows it) on Windows.
	proc, err := platform.StartDetached(cmd)
	if err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	_ = proc.Release()
	return waitForEndpoint(p.EngineEndpoint(), 5*time.Second)
}

// engineEnv is a detached engine's environment: the caller's, with the
// plugin dirs and Herdr socket taken from p. The MCP server runs under the
// agent CLI, whose environment has no HERDR_PLUGIN_* variables, and the
// engine reads them (paths.FromEnv).
//
// The engine serves every pane, so it drops the starting pane's id and the
// Claude Code session markers (CLAUDECODE, CLAUDE_CODE_*) of the agent whose
// MCP server started it. It keeps the CLAUDE_CODE_* settings that choose a
// provider or carry auth, which claude-cli dots need (backend.CleanEnv keeps
// them too).
//
// An engine first started by Codex's MCP launch gets Codex's minimal
// environment (only the variables Codex passes to MCP servers), so API keys
// set only in the shell environment may be missing for that engine's whole
// life; keys in credentials.toml are unaffected.
//
// Names compare in platform.EnvKey form, so a Windows variable spelled in
// another case is dropped or replaced too.
func engineEnv(env []string, p paths.Paths) []string {
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		k := platform.EnvKey(name)
		switch {
		case k == "HERDR_PLUGIN_CONFIG_DIR", k == "HERDR_PLUGIN_STATE_DIR", k == "HERDR_SOCKET_PATH",
			k == "HERDR_PANE_ID", k == "CLAUDECODE":
			continue
		case strings.HasPrefix(k, "CLAUDE_CODE_") && !claudeProviderSetting(k):
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HERDR_PLUGIN_CONFIG_DIR="+p.ConfigDir, "HERDR_PLUGIN_STATE_DIR="+p.StateDir, "HERDR_SOCKET_PATH="+p.SocketPath)
}

// claudeProviderSetting reports a CLAUDE_CODE_* variable that picks the
// model provider or carries auth (CLAUDE_CODE_USE_BEDROCK,
// CLAUDE_CODE_SKIP_VERTEX_AUTH, CLAUDE_CODE_OAUTH_TOKEN, client certs).
func claudeProviderSetting(k string) bool {
	return strings.HasPrefix(k, "CLAUDE_CODE_USE_") || strings.HasPrefix(k, "CLAUDE_CODE_SKIP_") ||
		strings.HasPrefix(k, "CLAUDE_CODE_CLIENT_") || k == "CLAUDE_CODE_OAUTH_TOKEN"
}

// waitForEndpoint polls until the engine accepts connections at endpoint.
func waitForEndpoint(endpoint string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		conn, err := ipc.Dial(ctx, endpoint)
		cancel()
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("engine endpoint %s not ready: %w", endpoint, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// openEngineLog rotates an oversized log and then opens it for the child's
// stdout/stderr. Rotation must happen before the open, never while a child
// holds the file.
func openEngineLog(path string) (*os.File, error) {
	rotateLog(path)
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func rotateLog(path string) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}
}

func buildEngine(p paths.Paths, hc herdr.Client, cfg config.Config, logf func(string, ...any)) (*engine.Engine, error) {
	d, err := engineDeps(p, hc, cfg, logf)
	if err != nil {
		return nil, err
	}
	return engine.New(d), nil
}

// engineDeps opens the session's stores and wires every engine dependency.
func engineDeps(p paths.Paths, hc herdr.Client, cfg config.Config, logf func(string, ...any)) (engine.Deps, error) {
	q, err := queue.Open(p.QueueFile())
	if err != nil {
		return engine.Deps{}, err
	}
	atts, err := state.OpenAttachments(p.AttachmentsFile())
	if err != nil {
		return engine.Deps{}, err
	}
	usage, err := state.OpenUsage(p.UsageFile())
	if err != nil {
		return engine.Deps{}, err
	}
	feed, err := state.OpenFeedLogged(p.FeedFile(), logf)
	if err != nil {
		return engine.Deps{}, err
	}
	daily, err := state.OpenDaily(p.DailyFile()) // shared by every Herdr session
	if err != nil {
		return engine.Deps{}, err
	}
	daily.Logf = logf
	home, _ := os.UserHomeDir()
	backends := newBackendSet(p, configLoaderLogged(p.ConfigFile(), logf), logf)
	return engine.Deps{
		Herdr:   hc,
		Backend: backends.Get,
		Reader: func(a state.Attachment) transcript.Reader {
			return transcript.Select(transcript.SelectOptions{
				Agent: a.Agent, SessionKind: a.SessionKind, SessionValue: a.SessionValue,
				ForceScrollback: a.ForceScrollback, Home: home, Pane: hc, PaneID: a.PaneID,
			})
		},
		LoadDots:    func() ([]dots.Dot, []dots.LoadError) { return dots.LoadAll(p.DotsDir()) },
		SaveDot:     func(d dots.Dot) error { return dots.Save(p.DotsDir(), d, false) },
		Config:      cfg,
		ConfigPath:  p.ConfigFile(),
		Queue:       q,
		Attachments: atts,
		Usage:       usage,
		Feed:        feed,
		Logf:        logf,

		Memory:         memoryStoreFor(p, cfg, logf),
		ResolveProject: resolveProject,

		History:     historyStoreFor(p),
		SessionKey:  paths.SessionKey(p.SocketPath),
		Trust:       &projectdots.TrustStore{Path: p.TrustFile()},
		ResolveRoot: resolveProject,

		Daily: daily,                          // proactive dots: daily runs and dollar budget
		Inbox: queue.OpenInbox(p.InboxFile()), // scheduled results for unattached projects

		Patches:      patchstore.Open(p.PatchesDir()),
		WorktreesDir: p.WorktreesDir(),
		Git:          worktree.Git{},
	}, nil
}

// historyStoreFor opens per-dot history (Phase 9). Excerpts are redacted with
// credentials.Redact, the history store's default (credentials.Redact is
// variadic, so it can't be assigned to Store.Redact).
func historyStoreFor(p paths.Paths) *history.Store {
	return history.Open(p.HistoryDir())
}

func serveEngine(p paths.Paths) error {
	lock, err := lockfile.TryLock(p.EngineLock())
	if errors.Is(err, lockfile.ErrLocked) {
		return nil // another engine owns this session
	}
	if err != nil {
		return err
	}
	defer lock.Release()
	logger := log.New(os.Stderr, "herdr-dots ", log.LstdFlags)

	cfg, err := config.Load(p.ConfigFile())
	if err != nil {
		logger.Print(startupConfigNote(err))
	}
	hc, err := herdr.NewFromEnv()
	if err != nil {
		return err
	}
	e, err := buildEngine(p, hc, cfg, logger.Printf)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	endpoint := p.EngineEndpoint()
	ln, err := ipc.Listen(endpoint) // replaces a stale socket: we hold the lock
	if err != nil {
		return err
	}
	defer ln.Close() // also removes the unix socket file

	e.Start(ctx)
	go watchHerdr(ctx, cancel, hc, logger)
	logger.Printf("engine started: session dir %s, endpoint %s", p.SessionDir, endpoint)
	err = ipc.Serve(ctx, ln, e)
	cancel()
	e.Wait() // no goroutine may write state files after the lock is released
	logger.Printf("engine stopped")
	return err
}

// watchHerdr exits the engine once Herdr has been unreachable for 30s.
func watchHerdr(ctx context.Context, cancel context.CancelFunc, hc herdr.Client, logger *log.Logger) {
	var downSince time.Time
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, c := context.WithTimeout(ctx, 3*time.Second)
			err := hc.Ping(pctx)
			c()
			if err == nil {
				downSince = time.Time{}
				continue
			}
			if downSince.IsZero() {
				downSince = time.Now()
			}
			if time.Since(downSince) > 30*time.Second {
				logger.Printf("herdr unreachable for 30s (%v); exiting", err)
				cancel()
				return
			}
		}
	}
}

// startupConfigNote is the log line for a malformed config.toml at engine
// start. Inject comes back when the file is fixed (config.toml reloads at
// runtime), but no memory store is opened, so memory needs a restart.
func startupConfigNote(err error) string {
	return fmt.Sprintf("config: %v (using defaults with inject and proactive work disabled until the file is fixed; memory stays off until the engine restarts)", err)
}

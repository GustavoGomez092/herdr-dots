package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"herdr-dots/internal/backend"
	"herdr-dots/internal/config"
	"herdr-dots/internal/dots"
	"herdr-dots/internal/engine"
	"herdr-dots/internal/herdr"
	"herdr-dots/internal/ipc"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/paths"
	"herdr-dots/internal/queue"
	"herdr-dots/internal/state"
	"herdr-dots/internal/transcript"
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
// already holds the lock, then waits until its socket accepts connections.
func ensureEngine(p paths.Paths) error {
	if lockfile.Held(p.EngineLock()) {
		return waitForSocket(p.EngineSock(), 3*time.Second)
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
	// Don't inherit the spawner's cwd: dots with an empty attachment cwd would read it.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cmd.Dir = home
	} else {
		cmd.Dir = "/"
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survive the hook/pane that started us
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	_ = cmd.Process.Release()
	return waitForSocket(p.EngineSock(), 5*time.Second)
}

func waitForSocket(path string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("engine socket %s not ready: %w", path, err)
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
	q, err := queue.Open(p.QueueFile())
	if err != nil {
		return nil, err
	}
	atts, err := state.OpenAttachments(p.AttachmentsFile())
	if err != nil {
		return nil, err
	}
	usage, err := state.OpenUsage(p.UsageFile())
	if err != nil {
		return nil, err
	}
	feed, err := state.OpenFeed(p.FeedFile())
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	return engine.New(engine.Deps{
		Herdr:   hc,
		Backend: func(kind string) (backend.Backend, error) { return backend.New(kind, backend.ExecRunner{}) },
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
	}), nil
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

	sock := p.EngineSock()
	_ = os.Remove(sock) // stale socket from a dead engine; we hold the lock
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	_ = os.Chmod(sock, 0o600)
	defer os.Remove(sock)

	e.Start(ctx)
	go watchHerdr(ctx, cancel, hc, logger)
	logger.Printf("engine started: session dir %s, socket %s", p.SessionDir, sock)
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
	return fmt.Sprintf("config: %v (using defaults with inject disabled until the file is fixed; memory stays off until the engine restarts)", err)
}

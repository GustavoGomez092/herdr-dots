package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"herdr-dots/internal/config"
)

// ErrDenyGlobsInvalid refuses a draft while tool_deny_globs holds a pattern
// that isn't a valid glob (config.RepoToolsOff, ruling P10-R7).
var ErrDenyGlobsInvalid = errors.New("drafts are off: tool_deny_globs in config.toml has an invalid pattern; fix it to turn them back on")

// researchOffHint is the roster's line for a research dot while
// config.RepoToolsOff holds.
const researchOffHint = "research off · fix tool_deny_globs"

// config returns the current settings. config.toml can change at runtime
// (ReloadConfig), so read it at the point of use instead of caching it.
// max_parallel_runs is the exception: it sizes the run semaphore at start.
func (e *Engine) config() config.Config { return *e.cfg.Load() }

// configStamp identifies a version of the config file ("" when it is missing).
func configStamp(path string) string {
	if path == "" {
		return ""
	}
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", st.ModTime().UnixNano(), st.Size())
}

// ReloadConfig re-reads config.toml. On an unreadable or malformed file it
// keeps the last good settings but forces InjectEnabled, MemoryEnabled,
// WriteEnabled and ProactiveEnabled off (the kill switches fail closed) and
// logs the error.
func (e *Engine) ReloadConfig() {
	if e.d.ConfigPath == "" {
		return
	}
	e.cfgMu.Lock()
	e.cfgStamp, e.cfgChecked = configStamp(e.d.ConfigPath), time.Now()
	e.cfgMu.Unlock()
	c, err := config.Load(e.d.ConfigPath)
	if err != nil {
		c = e.config()
		c.InjectEnabled, c.MemoryEnabled, c.WriteEnabled, c.ProactiveEnabled = false, false, false, false
		e.d.Logf("config: %v (keeping the last good settings; inject, memory, drafts and proactive work disabled until it is fixed)", err)
	}
	e.cfg.Store(&c)
	e.noteChairConfig(c)
	e.noteDenyConfig(c)
	e.broadcast()
}

// noteDenyConfig logs and toasts once (per stretch of bad settings, not per
// reload) when tool_deny_globs holds an invalid pattern: repository tools,
// drafts and research stay off until it is fixed (ruling P10-R7). The roster
// shows researchOffHint for research dots meanwhile.
func (e *Engine) noteDenyConfig(c config.Config) {
	e.cfgMu.Lock()
	warn := c.RepoToolsOff() && !e.denyWarned
	e.denyWarned = c.RepoToolsOff()
	e.cfgMu.Unlock()
	if !warn {
		return
	}
	msg := fmt.Sprintf("config.toml tool_deny_globs has %d invalid pattern(s) %q: repository tools, drafts and research are off until it is fixed",
		len(c.InvalidDenyGlobs), c.InvalidDenyGlobs)
	e.d.Logf("config: %s", msg)
	if e.d.Herdr == nil {
		return
	}
	e.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := e.d.Herdr.Notify(ctx, "Herdr Dots", msg); err != nil {
			e.d.Logf("notify: %v", err)
		}
	})
}

// maybeReloadConfig reloads config.toml when its mtime or size changed,
// checking at most once per ConfigCheck. Called from poll.
func (e *Engine) maybeReloadConfig() {
	if e.d.ConfigPath == "" {
		return
	}
	e.cfgMu.Lock()
	if time.Since(e.cfgChecked) < e.d.ConfigCheck {
		e.cfgMu.Unlock()
		return
	}
	e.cfgChecked = time.Now()
	changed := configStamp(e.d.ConfigPath) != e.cfgStamp
	e.cfgMu.Unlock()
	if changed {
		e.ReloadConfig()
	}
}

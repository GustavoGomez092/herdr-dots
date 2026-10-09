package engine

import (
	"fmt"
	"os"
	"time"

	"herdr-dots/internal/config"
)

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
// keeps the last good settings but forces InjectEnabled off (the kill switch
// fails closed) and logs the error.
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
		c.InjectEnabled = false
		e.d.Logf("config: %v (keeping the last good settings; inject disabled until it is fixed)", err)
	}
	e.cfg.Store(&c)
	e.broadcast()
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

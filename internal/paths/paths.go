// Package paths resolves every file herdr-dots reads or writes. Herdr gives
// each plugin a config dir (user-editable) and a state dir (runtime). State is
// shared across named Herdr sessions, so runtime files live under a per-session
// subdirectory keyed by the Herdr socket path.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"herdr-dots/internal/platform"
)

type Paths struct {
	ConfigDir  string
	StateDir   string
	SocketPath string // Herdr's socket
	SessionDir string // StateDir/<SessionKey(SocketPath)>
}

func New(configDir, stateDir, herdrSocket string) Paths {
	return Paths{
		ConfigDir:  configDir,
		StateDir:   stateDir,
		SocketPath: herdrSocket,
		SessionDir: filepath.Join(stateDir, SessionKey(herdrSocket)),
	}
}

// FromEnv reads the directories Herdr injects into every plugin command.
func FromEnv() (Paths, error) {
	c := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	s := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	h := os.Getenv("HERDR_SOCKET_PATH")
	if c == "" || s == "" || h == "" {
		return Paths{}, errors.New("HERDR_PLUGIN_CONFIG_DIR, HERDR_PLUGIN_STATE_DIR and HERDR_SOCKET_PATH must be set; are you running inside herdr?")
	}
	return New(c, s, h), nil
}

func SessionKey(socket string) string {
	sum := sha256.Sum256([]byte(socket))
	return hex.EncodeToString(sum[:])[:12]
}

func (p Paths) DotsDir() string         { return filepath.Join(p.ConfigDir, "dots") }
func (p Paths) ConfigFile() string      { return filepath.Join(p.ConfigDir, "config.toml") }
func (p Paths) EngineLock() string      { return filepath.Join(p.SessionDir, "engine.lock") }
func (p Paths) EngineLog() string       { return filepath.Join(p.SessionDir, "engine.log") }
func (p Paths) AttachmentsFile() string { return filepath.Join(p.SessionDir, "attachments.json") }
func (p Paths) QueueFile() string       { return filepath.Join(p.SessionDir, "queue.json") }
func (p Paths) FeedFile() string        { return filepath.Join(p.SessionDir, "feed.jsonl") }
func (p Paths) UsageFile() string       { return filepath.Join(p.SessionDir, "usage.json") }
func (p Paths) DotsPanesFile() string   { return filepath.Join(p.SessionDir, "dotspanes.json") }

func (p Paths) Ensure() error {
	for _, d := range []string{p.ConfigDir, p.DotsDir(), p.SessionDir, p.MemoryDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := platform.SecureDir(d); err != nil {
			return err
		}
	}
	if err := p.writeStatePointer(); err != nil {
		return err
	}
	return p.ensureEndpoint()
}

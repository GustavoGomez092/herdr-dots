// Package credentials stores API keys for the API backends in
// CONFIG_DIR/credentials.toml (0600, owner-only) and looks them up with
// environment variables first. Keys never leave this package and the backend
// adapters; Redact scrubs anything key-shaped from text that might be shown.
package credentials

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/fsutil"
	"herdr-dots/internal/lockfile"
	"herdr-dots/internal/platform"
)

var (
	// ErrNoCredentials means no key was found for a provider.
	ErrNoCredentials = errors.New("no credentials")
	// ErrInsecurePerms means credentials.toml is readable by other users
	// (Unix group/other bits; on Windows, a DACL entry for anyone but the
	// owner, SYSTEM and Administrators). The error carries the OS's fix.
	ErrInsecurePerms = errors.New("credentials.toml is readable by others")
	// ErrNotOwner means credentials.toml belongs to another user.
	ErrNotOwner = errors.New("credentials.toml is owned by another user")
	// ErrBadKey means a key failed the shape check in Set.
	ErrBadKey = errors.New("invalid key")
)

// Providers that take an API key, in display order. Ollama needs none.
var Providers = []string{"anthropic", "openai", "openrouter"}

// DefaultEnv is the environment variable read for each provider when
// config.toml sets no [providers.<name>] api_key_env.
var DefaultEnv = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
}

// Source resolves a provider's key. from is "env:NAME" or "file".
type Source interface {
	Lookup(provider string) (key, from string, err error)
}

// Status is one provider's key source for display. Only the last 4
// characters of a key ever leave this package.
type Status struct {
	Provider string
	From     string // "env:NAME", "file" or "missing"
	Last4    string
}

// File is the credentials.toml store plus environment lookup.
type File struct {
	Path     string
	Env      func(string) string // nil → os.Getenv
	EnvNames map[string]string   // provider → env var (config api_key_env); missing → DefaultEnv
}

type entry struct {
	APIKey string `toml:"api_key"`
}

// String and GoString keep a stray %v, %+v or %#v of the parsed file from
// printing a key.
func (entry) String() string   { return Redacted }
func (entry) GoString() string { return Redacted }

const header = "# Managed by herdr-dots. Do not commit. Mode 0600.\n"

func (f File) env(name string) string {
	if f.Env != nil {
		return f.Env(name)
	}
	return os.Getenv(name)
}

// EnvName is the environment variable consulted for provider.
func (f File) EnvName(provider string) string {
	if n := strings.TrimSpace(f.EnvNames[provider]); n != "" {
		return n
	}
	return DefaultEnv[provider]
}

// Lookup returns the provider's key: the environment variable first, then
// credentials.toml. A missing key is ErrNoCredentials with a hint; an unsafe
// file is ErrInsecurePerms or ErrNotOwner and is never read.
func (f File) Lookup(provider string) (string, string, error) {
	if name := f.EnvName(provider); name != "" {
		if v := strings.TrimSpace(f.env(name)); v != "" {
			return v, "env:" + name, nil
		}
	}
	m, err := f.read()
	if err != nil {
		return "", "", err
	}
	if k := strings.TrimSpace(m[provider].APIKey); k != "" {
		return k, "file", nil
	}
	hint := fmt.Sprintf("run `herdr-dots keys set %s`", provider)
	if name := f.EnvName(provider); name != "" {
		hint += " or set " + name
	}
	return "", "", fmt.Errorf("%w for %s: %s", ErrNoCredentials, provider, hint)
}

// read loads credentials.toml. A missing file is empty. The file must be a
// regular file owned by this user that no other user can read
// (platform.CheckPrivateFile on the opened file: mode bits on Unix, the DACL
// on Windows).
func (f File) read() (map[string]entry, error) {
	fd, st, err := openChecked(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	if fd != nil {
		defer fd.Close()
	}
	if fd == nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials.toml must be a regular file (%s)", f.Path)
	}
	if err := platform.CheckPrivateFile(fd); err != nil {
		switch {
		case errors.Is(err, platform.ErrNotOwner):
			return nil, ErrNotOwner
		case errors.Is(err, platform.ErrNotPrivate):
			return nil, fmt.Errorf("%w: %s", ErrInsecurePerms, platform.FixPrivateHint(f.Path))
		}
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(fd, 1<<20))
	if err != nil {
		return nil, err
	}
	m := map[string]entry{}
	if _, err := toml.Decode(string(data), &m); err != nil {
		// toml errors quote the offending line, which may hold a key.
		return nil, errors.New("credentials.toml is not valid TOML: fix or remove it")
	}
	return m, nil
}

func (f File) write(m map[string]entry) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(f.Path, buf.Bytes(), 0o600)
}

func knownProvider(p string) bool {
	for _, x := range Providers {
		if x == p {
			return true
		}
	}
	return false
}

// CheckKey rejects keys that can't be real: empty, too long, or holding
// whitespace or control characters.
func CheckKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty", ErrBadKey)
	}
	if len(key) > 512 {
		return fmt.Errorf("%w: longer than 512 characters", ErrBadKey)
	}
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r > unicode.MaxASCII {
			return fmt.Errorf("%w: it contains spaces or non-ASCII characters", ErrBadKey)
		}
	}
	return nil
}

// Set stores provider's key in credentials.toml (atomic, 0600).
func (f File) Set(provider, key string) error {
	if !knownProvider(provider) {
		return fmt.Errorf("unknown provider (want one of %v)", Providers)
	}
	key = strings.TrimSpace(key)
	if err := CheckKey(key); err != nil {
		return err
	}
	return f.update(func(m map[string]entry) bool {
		m[provider] = entry{APIKey: key}
		return true
	})
}

// lockTimeout bounds the wait for another process's credentials update.
const lockTimeout = 5 * time.Second

// update is a read-modify-write of credentials.toml under its lock
// (<file>.lock, S10), so two `keys set` at the same instant never lose a
// key. change reports whether to write.
func (f File) update(change func(map[string]entry) bool) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	l, err := lockfile.Acquire(f.Path+".lock", lockTimeout)
	if err != nil {
		return fmt.Errorf("credentials.toml is being changed by another herdr-dots: %w", err)
	}
	defer l.Release()
	m, err := f.read()
	if err != nil {
		return err
	}
	if !change(m) {
		return nil
	}
	return f.write(m)
}

// Remove deletes provider's key from credentials.toml. Removing a missing key
// is not an error.
func (f File) Remove(provider string) error {
	return f.update(func(m map[string]entry) bool {
		if _, ok := m[provider]; !ok {
			return false
		}
		delete(m, provider)
		return true
	})
}

// List reports where each provider's key comes from, with the last 4
// characters only.
func (f File) List() ([]Status, error) {
	m, err := f.read()
	if err != nil {
		return nil, err
	}
	var out []Status
	for _, p := range Providers {
		s := Status{Provider: p, From: "missing"}
		if name := f.EnvName(p); name != "" && strings.TrimSpace(f.env(name)) != "" {
			s.From, s.Last4 = "env:"+name, Last4(strings.TrimSpace(f.env(name)))
		} else if k := strings.TrimSpace(m[p].APIKey); k != "" {
			s.From, s.Last4 = "file", Last4(k)
		}
		out = append(out, s)
	}
	return out, nil
}

// Last4 is "…" plus a key's last 4 characters ("" for keys shorter than 12,
// which would reveal too much of the key).
func Last4(key string) string {
	if utf8.RuneCountInString(key) < 12 {
		return ""
	}
	r := []rune(key)
	return "…" + string(r[len(r)-4:])
}

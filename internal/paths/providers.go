package paths

import "path/filepath"

// CredentialsFile holds API keys (0600, owner-only). It lives in the config
// dir next to config.toml but is never part of it.
func (p Paths) CredentialsFile() string { return filepath.Join(p.ConfigDir, "credentials.toml") }

// PricesFile overrides the embedded price table, entry by entry.
func (p Paths) PricesFile() string { return filepath.Join(p.ConfigDir, "prices.toml") }

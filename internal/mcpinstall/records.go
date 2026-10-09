package mcpinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdr-dots/internal/fsutil"
)

// Record is one install that setup-mcp made, kept in CONFIG_DIR/mcp-install.json
// so --remove, upgrades and the manage popup know what is installed.
type Record struct {
	Client     Client    `json:"client"`
	Scope      string    `json:"scope"`   // "user"
	Command    []string  `json:"command"` // the server's command line
	ConfigPath string    `json:"config_path"`
	Backups    []string  `json:"backups,omitempty"`
	At         time.Time `json:"at"`
}

type recordsFile struct {
	Version  int      `json:"version"`
	Installs []Record `json:"installs"`
}

// RecordPath is CONFIG_DIR/mcp-install.json.
func RecordPath(configDir string) string { return filepath.Join(configDir, "mcp-install.json") }

// LoadRecords reads the install records; a missing file means none.
func LoadRecords(path string) ([]Record, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f recordsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.Installs, nil
}

// SaveRecords writes the records (0600, atomic).
func SaveRecords(path string, rs []Record) error {
	if rs == nil {
		rs = []Record{}
	}
	b, err := json.MarshalIndent(recordsFile{Version: 1, Installs: rs}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, append(b, '\n'), 0o600)
}

// WithRecord replaces the record for r.Client (or adds it).
func WithRecord(rs []Record, r Record) []Record {
	return append(WithoutClient(rs, r.Client), r)
}

// WithoutClient drops the record for c.
func WithoutClient(rs []Record, c Client) []Record {
	out := []Record{}
	for _, x := range rs {
		if x.Client != c {
			out = append(out, x)
		}
	}
	return out
}

// StatusLine is the manage popup's footer line.
func StatusLine(configDir string) string {
	rs, err := LoadRecords(RecordPath(configDir))
	if err != nil {
		return "MCP: install record unreadable (" + RecordPath(configDir) + ")"
	}
	if len(rs) == 0 {
		return "MCP: not installed (run herdr-dots setup-mcp)"
	}
	var names []string
	for _, c := range []Client{ClientClaude, ClientCodex} {
		for _, r := range rs {
			if r.Client == c {
				names = append(names, c.Title())
				break
			}
		}
	}
	return "MCP: installed for " + strings.Join(names, ", ")
}

package backend

import (
	"os"
	"path/filepath"

	"herdr-dots/internal/platform"
)

// resolveBin finds the program a backend will run; tests replace it.
var resolveBin = platform.LookPath

// writeRunFiles writes files (name -> content) into a new private temp
// directory (0700 / owner-only DACL, files 0600) and returns their paths and
// a cleanup that removes the directory.
func writeRunFiles(files map[string]string) (map[string]string, func(), error) {
	dir, err := os.MkdirTemp("", "herdr-dots-run-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := platform.SecureDir(dir); err != nil {
		cleanup()
		return nil, nil, err
	}
	paths := make(map[string]string, len(files))
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			cleanup()
			return nil, nil, err
		}
		if err := platform.SecureFile(p); err != nil {
			cleanup()
			return nil, nil, err
		}
		paths[name] = p
	}
	return paths, cleanup, nil
}

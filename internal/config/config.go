package config

import (
	"os"
	"path/filepath"
)

// Config holds global daemon configuration. Values are sourced from flags
// and environment, with defaults for portable single-binary operation.
type Config struct {
	DataDir string
	Port    int
	Bind    string
	Verbose bool
}

// DefaultDataDir returns the default directory for makidoku.db, wasm cache,
// and downloaded manga: ./data relative to the current working directory,
// overridable via MAKIDOKU_DATA_DIR or --data-dir.
func DefaultDataDir() string {
	if v := os.Getenv("MAKIDOKU_DATA_DIR"); v != "" {
		return v
	}
	return filepath.Join(".", "data")
}

// ResolveDataDir ensures the data directory exists.
func ResolveDataDir(dir string) (string, error) {
	if dir == "" {
		dir = DefaultDataDir()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}

// DBPath returns the SQLite file path.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "makidoku.db")
}

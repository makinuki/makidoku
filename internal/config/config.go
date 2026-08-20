package config

import (
	"os"
	"path/filepath"
	"time"
)

// Config holds global daemon configuration. Values are sourced from flags
// and environment, with defaults for portable single-binary operation.
type Config struct {
	DataDir string
	Port    int
	Bind    string
	Verbose bool
	// RegistryURL overrides the source catalog location. An http or https URL
	// reads a published catalog; a filesystem path reads a local mirror.
	// Empty selects the public registry.
	RegistryURL string
	// ChallengeWait is how long a request blocked by an anti-bot challenge
	// waits for clearance to be submitted through the API before it fails.
	ChallengeWait time.Duration
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

// DefaultRegistryURL returns the catalog location from the environment. An
// empty result selects the host's built in registry.
func DefaultRegistryURL() string {
	return os.Getenv("MAKIDOKU_REGISTRY_URL")
}

// DefaultChallengeWait returns the anti-bot clearance wait from the
// environment. An unset or unparsable value disables waiting, so a challenged
// request fails immediately.
func DefaultChallengeWait() time.Duration {
	v := os.Getenv("MAKIDOKU_CHALLENGE_WAIT")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

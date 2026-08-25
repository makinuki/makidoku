package config

import (
	"os"
	"path/filepath"
	"strconv"
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
	ChallengeWait   time.Duration
	DownloadDir     string
	DownloadWorkers int
	PageInterval    time.Duration
	// ImageCacheMaxBytes caps the total size of the processed image cache.
	// Zero disables the size limit.
	ImageCacheMaxBytes int64
	// ImageCacheMaxAge bounds how long a cached image may remain untouched.
	// Zero disables the age limit.
	ImageCacheMaxAge time.Duration
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

func DefaultDownloadDir() string {
	return os.Getenv("MAKIDOKU_DOWNLOAD_DIR")
}

func ResolveDownloadDir(dir, dataDir string) (string, error) {
	if dir == "" {
		dir = filepath.Join(dataDir, "downloads")
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

// DefaultPort returns the HTTP port from the environment. The fallback of
// 6254 is an uncommon port chosen to avoid collisions with common local
// services; OAuth redirect registrations are port specific, so deployments
// should keep one stable value.
func DefaultPort() int {
	port, err := strconv.Atoi(os.Getenv("MAKIDOKU_PORT"))
	if err != nil || port < 1 || port > 65535 {
		return 6254
	}
	return port
}

func DefaultDownloadWorkers() int {
	workers, err := strconv.Atoi(os.Getenv("MAKIDOKU_DOWNLOAD_WORKERS"))
	if err != nil || workers < 1 {
		return 3
	}
	return workers
}

func DefaultPageInterval() time.Duration {
	value := os.Getenv("MAKIDOKU_PAGE_INTERVAL")
	if value == "" {
		return 500 * time.Millisecond
	}
	interval, err := time.ParseDuration(value)
	if err != nil || interval < 0 {
		return 500 * time.Millisecond
	}
	return interval
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

// DefaultImageCacheMaxBytes returns the processed image cache size budget
// from the environment. The default budget is 512 MiB.
func DefaultImageCacheMaxBytes() int64 {
	v := os.Getenv("MAKIDOKU_IMAGE_CACHE_MAX_BYTES")
	if v == "" {
		return 512 << 20
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil || parsed < 0 {
		return 512 << 20
	}
	return parsed
}

// DefaultImageCacheMaxAge returns the processed image cache retention age
// from the environment. The default retention is 30 days.
func DefaultImageCacheMaxAge() time.Duration {
	v := os.Getenv("MAKIDOKU_IMAGE_CACHE_MAX_AGE")
	if v == "" {
		return 30 * 24 * time.Hour
	}
	parsed, err := time.ParseDuration(v)
	if err != nil || parsed < 0 {
		return 30 * 24 * time.Hour
	}
	return parsed
}

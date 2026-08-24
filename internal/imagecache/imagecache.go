// Package imagecache enforces bounded retention for processed page images
// stored on disk. Retention combines an age limit, a byte budget, and orphan
// removal for files the database no longer references.
package imagecache

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const sweepInterval = time.Minute

// Cache sweeps a flat image cache directory. A Cache is safe for concurrent
// use; at most one sweep runs per sweepInterval.
type Cache struct {
	root     string
	maxBytes int64
	maxAge   time.Duration

	mu        sync.Mutex
	lastSweep time.Time
}

// New returns a cache manager rooted at dir. maxBytes caps the total size of
// cached files and maxAge bounds how long a file may remain untouched.
func New(dir string, maxBytes int64, maxAge time.Duration) *Cache {
	return &Cache{root: dir, maxBytes: maxBytes, maxAge: maxAge}
}

type cacheFile struct {
	path    string
	size    int64
	modTime time.Time
}

// Sweep removes files older than the retention age, files that are not in
// keep when keep is non-nil, and the oldest files until the total fits the
// byte budget. A nil keep disables orphan removal.
func (c *Cache) Sweep(keep map[string]bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	deadline := time.Now().Add(-c.maxAge)
	var files []cacheFile
	var total int64
	err := filepath.WalkDir(c.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if c.maxAge > 0 && info.ModTime().Before(deadline) {
			return os.Remove(path)
		}
		if keep != nil && !keep[path] {
			return os.Remove(path)
		}
		files = append(files, cacheFile{path: path, size: info.Size(), modTime: info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	if c.maxBytes > 0 && total > c.maxBytes {
		sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
		for _, file := range files {
			if total <= c.maxBytes {
				break
			}
			if err := os.Remove(file.path); err == nil {
				total -= file.size
			}
		}
	}
	c.lastSweep = time.Now()
	return nil
}

// AfterWrite runs a throttled sweep after a cache write. The keep set is
// resolved lazily so the sweep sees every path written up to that moment.
func (c *Cache) AfterWrite(keep func() (map[string]bool, error)) {
	c.mu.Lock()
	due := time.Since(c.lastSweep) >= sweepInterval
	c.mu.Unlock()
	if !due {
		return
	}
	paths, err := keep()
	if err != nil {
		return
	}
	_ = c.Sweep(paths)
}

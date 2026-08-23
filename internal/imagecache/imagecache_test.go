package imagecache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path string, size int, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func TestSweepRemovesAgedAndOrphanFiles(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	fresh := filepath.Join(root, "fresh.img")
	stale := filepath.Join(root, "stale.img")
	orphan := filepath.Join(root, "orphan.img")
	writeFile(t, fresh, 10, now.Add(-time.Minute))
	writeFile(t, stale, 10, now.Add(-48*time.Hour))
	writeFile(t, orphan, 10, now)

	cache := New(root, 0, 24*time.Hour)
	if err := cache.Sweep(map[string]bool{fresh: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("referenced file was removed: %v", err)
	}
	for _, path := range []string{stale, orphan} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived the sweep: %v", path, err)
		}
	}
}

func TestSweepEvictsOldestBeyondByteBudget(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := filepath.Join(root, "old.img")
	middle := filepath.Join(root, "middle.img")
	recent := filepath.Join(root, "recent.img")
	writeFile(t, old, 10, now.Add(-3*time.Hour))
	writeFile(t, middle, 10, now.Add(-2*time.Hour))
	writeFile(t, recent, 10, now.Add(-time.Hour))

	// Budget fits two files, so the oldest one must go.
	cache := New(root, 20, 0)
	if err := cache.Sweep(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("oldest file was not evicted")
	}
	for _, path := range []string{middle, recent} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s was evicted despite budget: %v", path, err)
		}
	}
}

func TestAfterWriteIsThrottled(t *testing.T) {
	root := t.TempDir()
	cache := New(root, 0, 0)
	calls := 0
	keep := func() (map[string]bool, error) {
		calls++
		return nil, nil
	}
	cache.AfterWrite(keep)
	if calls != 1 {
		t.Fatalf("first AfterWrite calls = %d", calls)
	}
	cache.AfterWrite(keep)
	if calls != 1 {
		t.Fatalf("throttled AfterWrite ran again: calls = %d", calls)
	}
}

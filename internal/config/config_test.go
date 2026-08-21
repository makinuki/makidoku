package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDownloadDefaultsFromEnvironment(t *testing.T) {
	t.Setenv("MAKIDOKU_DOWNLOAD_WORKERS", "5")
	t.Setenv("MAKIDOKU_PAGE_INTERVAL", "750ms")
	t.Setenv("MAKIDOKU_DOWNLOAD_DIR", filepath.Join("custom", "downloads"))

	if got := DefaultDownloadWorkers(); got != 5 {
		t.Fatalf("workers = %d", got)
	}
	if got := DefaultPageInterval(); got != 750*time.Millisecond {
		t.Fatalf("page interval = %s", got)
	}
	if got := DefaultDownloadDir(); got != filepath.Join("custom", "downloads") {
		t.Fatalf("download dir = %q", got)
	}
}

func TestResolveDownloadDirDefaultsUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	got, err := ResolveDownloadDir("", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataDir, "downloads")
	if got != want {
		t.Fatalf("download dir = %q, want %q", got, want)
	}
}

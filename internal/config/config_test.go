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

func TestDefaultPortPrefersEnvironmentWithStableFallback(t *testing.T) {
	t.Setenv("MAKIDOKU_PORT", "7000")
	if got := DefaultPort(); got != 7000 {
		t.Fatalf("port = %d", got)
	}
	t.Setenv("MAKIDOKU_PORT", "not-a-port")
	if got := DefaultPort(); got != 6254 {
		t.Fatalf("port = %d", got)
	}
	t.Setenv("MAKIDOKU_PORT", "")
	if got := DefaultPort(); got != 6254 {
		t.Fatalf("port = %d", got)
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

// The auto-solve toggle decides whether a classified challenge opens a window on
// its own. Only an explicit true turns it on: being wrong in that direction
// takes over the reader's screen, while being wrong the other way costs one
// button press.
func TestDefaultAutoSolve(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"true", true},
		{"TRUE", true},
		{" True ", true},
		{"false", false},
		{"1", false},
		{"yes", false},
		{"on", false},
	}
	for _, c := range cases {
		t.Setenv("MAKIDOKU_AUTO_SOLVE", c.value)
		if got := DefaultAutoSolve(); got != c.want {
			t.Errorf("MAKIDOKU_AUTO_SOLVE=%q gave %v, want %v", c.value, got, c.want)
		}
	}
}

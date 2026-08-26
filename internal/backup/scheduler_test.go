package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

func TestWriteSnapshotRotatesBackups(t *testing.T) {
	handle, err := db.Open(filepath.Join(t.TempDir(), "backup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	dir := t.TempDir()
	if err := WriteSnapshot(handle, dir, 2, time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(handle, dir, 2, time.Date(2026, 8, 26, 10, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(handle, dir, 2, time.Date(2026, 8, 26, 10, 2, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "backups", "makidoku-backup-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("backup files = %d, want 2", len(files))
	}
	data, err := os.ReadFile(files[len(files)-1])
	if err != nil {
		t.Fatal(err)
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil || document.Version != 1 {
		t.Fatalf("backup document = %+v, err=%v", document, err)
	}
}

func TestRunTickerDisablesZeroInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := RunTicker(ctx, nil, t.TempDir(), 0, 1); err != nil {
		t.Fatal(err)
	}
}

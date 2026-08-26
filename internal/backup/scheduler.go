package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jmoiron/sqlx"
)

// WriteSnapshot writes one portable backup and keeps only the newest keep
// files. The timestamp is supplied by the caller so scheduled writes and
// tests use the same deterministic naming and rotation behavior.
func WriteSnapshot(database *sqlx.DB, dataDir string, keep int, timestamp time.Time) error {
	if database == nil {
		return fmt.Errorf("backup database is nil")
	}
	if keep < 1 {
		keep = 1
	}
	root := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	payload, err := Export(database)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("makidoku-backup-%s.json", timestamp.UTC().Format("20060102-150405.000000000"))
	path := filepath.Join(root, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	files, err := filepath.Glob(filepath.Join(root, "makidoku-backup-*.json"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	if excess := len(files) - keep; excess > 0 {
		for _, old := range files[:excess] {
			if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

// RunTicker writes backups at interval until cancellation. A non-positive
// interval disables scheduled backups.
func RunTicker(ctx context.Context, database *sqlx.DB, dataDir string, interval time.Duration, keep int) error {
	if interval <= 0 {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case timestamp := <-ticker.C:
			if err := WriteSnapshot(database, dataDir, keep, timestamp); err != nil {
				// Scheduled backup failures should not stop the daemon. The next
				// tick gets another opportunity to persist a snapshot.
				slog.Warn("backup scheduled snapshot failed", "err", err)
				continue
			}
		}
	}
}

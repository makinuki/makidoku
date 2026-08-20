package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Open opens (or creates) the SQLite database at path, configures WAL mode
// and pragmas, and applies embedded migrations.
func Open(path string) (*sqlx.DB, error) {
	// modernc.org/sqlite DSN: file:path?cache=shared is for in-memory sharing;
	// for file DB we just pass the path. Use _pragma query params for foreign_keys,
	// but we also set them via PRAGMA after open for determinism.
	dsn := fmt.Sprintf("file:%s?cache=shared", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db := sqlx.NewDb(sqlDB, "sqlite")

	// Each must be set per connection; the driver pools connections so we
	// set them globally via PRAGMA and also limit to single writer via WAL.
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	// Enforce single-writer friendly pool for SQLite.
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// migrate applies *.up.sql files in lexical order. Idempotent: each file
// should be guarded by IF NOT EXISTS. We track no version table yet;
// full golang-migrate integration lands when down migrations
// and per-file history. For now, sqlite_migrations table records applied files.
func migrate(db *sqlx.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create _migrations: %w", err)
	}

	entries, err := fs.Glob(migrationFS, "migrations/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)

	for _, name := range entries {
		var exists int
		if err := db.Get(&exists, `SELECT COUNT(*) FROM _migrations WHERE name=?`, name); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		data, err := fs.ReadFile(migrationFS, name)
		if err != nil {
			return err
		}
		sqlText := string(data)
		// Split on semicolon boundaries naively; the DDL has no stray semicolons
		// in literals. We execute as a single Exec for atomicity.
		if strings.TrimSpace(sqlText) == "" {
			continue
		}
		if _, err := db.Exec(sqlText); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
		if _, err := db.Exec(`INSERT INTO _migrations(name, applied_at) VALUES(?, strftime('%s','now'))`, name); err != nil {
			return err
		}
	}
	return nil
}

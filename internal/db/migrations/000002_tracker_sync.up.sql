CREATE TABLE IF NOT EXISTS tracker_credentials (
    tracker_type TEXT PRIMARY KEY,
    access_token BLOB NOT NULL,
    refresh_token BLOB,
    expires_at INTEGER,
    metadata BLOB,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tracker_sync_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    manga_id TEXT NOT NULL,
    binding_id INTEGER NOT NULL,
    chapter_number REAL NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL,
    error_message TEXT,
    created_at INTEGER NOT NULL,
    completed_at INTEGER,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (binding_id) REFERENCES tracker_bindings(id) ON DELETE CASCADE,
    UNIQUE (manga_id, binding_id, chapter_number)
);

CREATE INDEX IF NOT EXISTS idx_tracker_sync_due ON tracker_sync_jobs(status, next_attempt_at);

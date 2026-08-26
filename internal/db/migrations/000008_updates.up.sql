CREATE TABLE IF NOT EXISTS update_log (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    chapter_id TEXT NOT NULL,
    seen_at INTEGER NOT NULL,
    acknowledged INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (chapter_id) REFERENCES chapters(id) ON DELETE CASCADE,
    UNIQUE (manga_id, chapter_id)
);
CREATE INDEX IF NOT EXISTS idx_update_log_seen ON update_log(seen_at DESC);
CREATE TABLE IF NOT EXISTS library_update_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_run_at INTEGER,
    last_status TEXT
);

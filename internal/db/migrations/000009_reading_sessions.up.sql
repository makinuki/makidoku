CREATE TABLE IF NOT EXISTS reading_sessions (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    seconds INTEGER NOT NULL CHECK (seconds > 0),
    occurred_at INTEGER NOT NULL,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_reading_sessions_manga ON reading_sessions(manga_id);
CREATE INDEX IF NOT EXISTS idx_reading_sessions_time ON reading_sessions(occurred_at DESC);

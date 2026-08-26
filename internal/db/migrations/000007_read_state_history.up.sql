CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chapter_read_state (
    chapter_id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    read INTEGER NOT NULL DEFAULT 0,
    read_at INTEGER,
    FOREIGN KEY (chapter_id) REFERENCES chapters(id) ON DELETE CASCADE,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_chapter_read_state_manga ON chapter_read_state(manga_id);

CREATE TABLE IF NOT EXISTS history_events (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    chapter_id TEXT,
    page INTEGER,
    occurred_at INTEGER NOT NULL,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (chapter_id) REFERENCES chapters(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_history_events_time ON history_events(occurred_at DESC);

INSERT OR IGNORE INTO chapter_read_state(chapter_id, manga_id, read, read_at)
SELECT last_read_chapter_id, manga_id, 1, last_read_at
FROM reading_progress
WHERE is_completed = 1;

INSERT OR IGNORE INTO history_events(id, manga_id, chapter_id, page, occurred_at)
SELECT lower(hex(randomblob(16))), manga_id, last_read_chapter_id, last_read_page, last_read_at
FROM reading_progress;

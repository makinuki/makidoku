-- MakiDoku initial schema.
-- All timestamps are INTEGER Unix seconds (or milliseconds where noted for chapters.uploaded_at).

CREATE TABLE IF NOT EXISTS sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    abi_version INTEGER NOT NULL,
    lang TEXT NOT NULL,
    base_url TEXT NOT NULL,
    icon_url TEXT,
    wasm_path TEXT NOT NULL,
    installed_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS plugin_storage (
    source_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (source_id, key),
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS manga (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    source_manga_id TEXT NOT NULL,
    title TEXT NOT NULL,
    alt_titles TEXT,
    description TEXT,
    authors TEXT,
    artists TEXT,
    genres TEXT,
    status TEXT NOT NULL,
    cover_url TEXT NOT NULL,
    in_library INTEGER NOT NULL DEFAULT 0,
    download_format TEXT NOT NULL DEFAULT 'cbz',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (source_id) REFERENCES sources(id)
);

CREATE TABLE IF NOT EXISTS manga_categories (
    manga_id TEXT NOT NULL,
    category_id INTEGER NOT NULL,
    PRIMARY KEY (manga_id, category_id),
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS chapters (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    source_chapter_id TEXT NOT NULL,
    chapter_number REAL,
    title TEXT,
    language TEXT,
    uploaded_at INTEGER,
    scanlator TEXT,
    downloaded INTEGER NOT NULL DEFAULT 0,
    download_path TEXT,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS reading_progress (
    manga_id TEXT PRIMARY KEY,
    last_read_chapter_id TEXT NOT NULL,
    last_read_page INTEGER NOT NULL DEFAULT 1,
    total_pages INTEGER NOT NULL DEFAULT 0,
    is_completed INTEGER NOT NULL DEFAULT 0,
    last_read_at INTEGER NOT NULL,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (last_read_chapter_id) REFERENCES chapters(id)
);

CREATE TABLE IF NOT EXISTS tracker_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    manga_id TEXT NOT NULL,
    tracker_type TEXT NOT NULL,
    remote_id TEXT NOT NULL,
    remote_title TEXT NOT NULL,
    remote_score REAL,
    remote_status TEXT,
    last_synced_chapter REAL NOT NULL DEFAULT 0,
    total_remote_chapters INTEGER,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    UNIQUE (manga_id, tracker_type)
);

CREATE TABLE IF NOT EXISTS download_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chapter_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL,
    progress INTEGER NOT NULL DEFAULT 0,
    total_pages INTEGER NOT NULL DEFAULT 0,
    downloaded_pages INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    queued_at INTEGER NOT NULL,
    FOREIGN KEY (chapter_id) REFERENCES chapters(id) ON DELETE CASCADE
);

-- Helpful indexes for common queries (not in the original DDL but safe to add).
CREATE INDEX IF NOT EXISTS idx_manga_source ON manga(source_id);
CREATE INDEX IF NOT EXISTS idx_manga_library ON manga(in_library);
CREATE INDEX IF NOT EXISTS idx_chapters_manga ON chapters(manga_id);
CREATE INDEX IF NOT EXISTS idx_tracker_manga ON tracker_bindings(manga_id);

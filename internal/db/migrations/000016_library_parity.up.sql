-- Fields a library backup can carry that earlier schema versions did not
-- store. Every column is nullable or defaulted so existing rows are unchanged.

ALTER TABLE manga ADD COLUMN custom_title TEXT;
ALTER TABLE manga ADD COLUMN custom_artist TEXT;
ALTER TABLE manga ADD COLUMN custom_author TEXT;
ALTER TABLE manga ADD COLUMN custom_description TEXT;
ALTER TABLE manga ADD COLUMN custom_genres TEXT;
ALTER TABLE manga ADD COLUMN custom_status TEXT;
ALTER TABLE manga ADD COLUMN custom_cover_url TEXT;
ALTER TABLE manga ADD COLUMN notes TEXT;
ALTER TABLE manga ADD COLUMN memo TEXT;
ALTER TABLE manga ADD COLUMN source_version INTEGER;
ALTER TABLE manga ADD COLUMN update_strategy TEXT;
ALTER TABLE manga ADD COLUMN favorite_modified_at INTEGER;
ALTER TABLE manga ADD COLUMN initialized INTEGER NOT NULL DEFAULT 0;
ALTER TABLE manga ADD COLUMN excluded_scanlators TEXT;
ALTER TABLE manga ADD COLUMN chapter_flags INTEGER;

ALTER TABLE chapters ADD COLUMN bookmark INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chapters ADD COLUMN source_order INTEGER;
ALTER TABLE chapters ADD COLUMN source_version INTEGER;
ALTER TABLE chapters ADD COLUMN source_last_modified_at INTEGER;
ALTER TABLE chapters ADD COLUMN source_fetched_at INTEGER;
ALTER TABLE chapters ADD COLUMN memo TEXT;

ALTER TABLE categories ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;

ALTER TABLE tracker_bindings ADD COLUMN remote_url TEXT;
ALTER TABLE tracker_bindings ADD COLUMN remote_library_id TEXT;
ALTER TABLE tracker_bindings ADD COLUMN is_private INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS source_feeds (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    is_global INTEGER NOT NULL DEFAULT 1,
    feed_order INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS saved_searches (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    feed_id TEXT,
    name TEXT NOT NULL,
    query TEXT NOT NULL DEFAULT '',
    filters TEXT NOT NULL DEFAULT '[]',
    search_order INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (source_id) REFERENCES sources(id) ON DELETE CASCADE,
    FOREIGN KEY (feed_id) REFERENCES source_feeds(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS manga_merges (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    source_manga_id TEXT NOT NULL,
    url TEXT,
    is_info_manga INTEGER NOT NULL DEFAULT 0,
    get_chapter_updates INTEGER NOT NULL DEFAULT 1,
    chapter_sort_mode INTEGER NOT NULL DEFAULT 0,
    chapter_priority INTEGER NOT NULL DEFAULT 0,
    download_chapters INTEGER NOT NULL DEFAULT 0,
    merge_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE (manga_id, source_id, source_manga_id),
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE,
    FOREIGN KEY (source_id) REFERENCES sources(id)
);

CREATE TABLE IF NOT EXISTS manga_titles (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    title TEXT NOT NULL,
    title_type INTEGER NOT NULL DEFAULT 0,
    UNIQUE (manga_id, title),
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS manga_tags (
    id TEXT PRIMARY KEY,
    manga_id TEXT NOT NULL,
    namespace TEXT,
    name TEXT NOT NULL,
    tag_type INTEGER NOT NULL DEFAULT 0,
    UNIQUE (manga_id, namespace, name),
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS manga_metadata (
    manga_id TEXT PRIMARY KEY,
    uploader TEXT,
    extra TEXT NOT NULL DEFAULT '{}',
    indexed_extra TEXT,
    extra_version INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (manga_id) REFERENCES manga(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_source_feeds_source ON source_feeds(source_id);
CREATE INDEX IF NOT EXISTS idx_saved_searches_source ON saved_searches(source_id);
CREATE INDEX IF NOT EXISTS idx_manga_merges_manga ON manga_merges(manga_id);
CREATE INDEX IF NOT EXISTS idx_manga_titles_manga ON manga_titles(manga_id);
CREATE INDEX IF NOT EXISTS idx_manga_tags_manga ON manga_tags(manga_id);

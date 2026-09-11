DROP INDEX IF EXISTS idx_manga_tags_manga;
DROP INDEX IF EXISTS idx_manga_titles_manga;
DROP INDEX IF EXISTS idx_manga_merges_manga;
DROP INDEX IF EXISTS idx_saved_searches_source;
DROP INDEX IF EXISTS idx_source_feeds_source;

DROP TABLE IF EXISTS manga_metadata;
DROP TABLE IF EXISTS manga_tags;
DROP TABLE IF EXISTS manga_titles;
DROP TABLE IF EXISTS manga_merges;
DROP TABLE IF EXISTS saved_searches;
DROP TABLE IF EXISTS source_feeds;

ALTER TABLE tracker_bindings DROP COLUMN is_private;
ALTER TABLE tracker_bindings DROP COLUMN remote_library_id;
ALTER TABLE tracker_bindings DROP COLUMN remote_url;

ALTER TABLE categories DROP COLUMN hidden;

ALTER TABLE chapters DROP COLUMN memo;
ALTER TABLE chapters DROP COLUMN source_fetched_at;
ALTER TABLE chapters DROP COLUMN source_last_modified_at;
ALTER TABLE chapters DROP COLUMN source_version;
ALTER TABLE chapters DROP COLUMN source_order;
ALTER TABLE chapters DROP COLUMN bookmark;

ALTER TABLE manga DROP COLUMN chapter_flags;
ALTER TABLE manga DROP COLUMN excluded_scanlators;
ALTER TABLE manga DROP COLUMN initialized;
ALTER TABLE manga DROP COLUMN favorite_modified_at;
ALTER TABLE manga DROP COLUMN update_strategy;
ALTER TABLE manga DROP COLUMN source_version;
ALTER TABLE manga DROP COLUMN memo;
ALTER TABLE manga DROP COLUMN notes;
ALTER TABLE manga DROP COLUMN custom_cover_url;
ALTER TABLE manga DROP COLUMN custom_status;
ALTER TABLE manga DROP COLUMN custom_genres;
ALTER TABLE manga DROP COLUMN custom_description;
ALTER TABLE manga DROP COLUMN custom_author;
ALTER TABLE manga DROP COLUMN custom_artist;
ALTER TABLE manga DROP COLUMN custom_title;

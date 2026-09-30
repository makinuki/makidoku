-- Per-source download pacing overrides. A null column follows the source's
-- own suggestion first and the global default second; an override set by the
-- user replaces both layers for that field.
CREATE TABLE IF NOT EXISTS source_download_prefs (
    source_id TEXT NOT NULL PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    interval_ms INTEGER,
    max_attempts INTEGER,
    backoff_ms INTEGER,
    updated_at INTEGER NOT NULL
);

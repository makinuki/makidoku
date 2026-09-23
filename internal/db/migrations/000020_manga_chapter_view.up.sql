-- Chapter list presentation persists per title, next to the reader overrides.
-- Empty values mean unset, and the client falls back to its defaults.
ALTER TABLE manga ADD COLUMN chapter_sort TEXT NOT NULL DEFAULT '';
ALTER TABLE manga ADD COLUMN chapter_filter TEXT NOT NULL DEFAULT '';
ALTER TABLE manga ADD COLUMN chapter_language TEXT NOT NULL DEFAULT '';

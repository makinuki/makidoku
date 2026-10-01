-- Per-source concurrency override: how many chapters of the source download
-- at the same time. A null value follows the source's burst hint first and
-- the global chapters-per-source setting second.
ALTER TABLE source_download_prefs ADD COLUMN burst INTEGER;

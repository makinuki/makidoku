-- The download queue keeps an explicit order so the queue screen can reorder
-- it. Existing rows keep their insertion order by taking their id, and new
-- rows are appended with the next highest position.
ALTER TABLE download_queue ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
UPDATE download_queue SET position = id;

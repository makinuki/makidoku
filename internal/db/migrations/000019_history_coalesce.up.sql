-- Reading history keeps one row per chapter with the latest page. Earlier
-- progress writes inserted a row per page turn, so collapse duplicates and
-- keep the newest row of each manga and chapter pair.
DELETE FROM history_events
WHERE EXISTS (
    SELECT 1 FROM history_events AS newer
    WHERE newer.manga_id = history_events.manga_id
    AND IFNULL(newer.chapter_id, '') = IFNULL(history_events.chapter_id, '')
    AND (newer.occurred_at > history_events.occurred_at
        OR (newer.occurred_at = history_events.occurred_at AND newer.rowid > history_events.rowid))
);

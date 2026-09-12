-- A source declares a chapter upload time in milliseconds, and the refresh
-- path stored it as written while the importer converted it. Every other
-- stored timestamp is in seconds, so values that can only be milliseconds are
-- folded down. The floor is above any seconds value for the observable future.
UPDATE chapters SET uploaded_at = uploaded_at / 1000 WHERE uploaded_at >= 100000000000;

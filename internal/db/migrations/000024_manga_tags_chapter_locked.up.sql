-- Manga tags and locked chapters carry optional payload fields from the 1.3.0
-- contract revision. Tags mirror the genres column shape; locked defaults to
-- open so existing rows keep their meaning.
ALTER TABLE manga ADD COLUMN tags TEXT;
ALTER TABLE chapters ADD COLUMN locked INTEGER NOT NULL DEFAULT 0;

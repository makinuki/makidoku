-- A refresh may rewrite the locator a link is read with after a source rotates
-- its identifiers. The recorded locator is kept beside it, so re-importing the
-- same backup still matches what it wrote instead of creating a second link.
ALTER TABLE manga_sources ADD COLUMN imported_locator TEXT;
ALTER TABLE chapter_sources ADD COLUMN imported_locator TEXT;

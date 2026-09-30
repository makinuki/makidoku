-- Records whether the installed binary exports get_settings, so the source
-- list can offer the per-source settings surface without compiling the
-- plugin on every request.
ALTER TABLE sources ADD COLUMN has_settings INTEGER NOT NULL DEFAULT 0;

-- Per-source chapter language selection. An empty value inherits the global
-- default; otherwise the column holds a JSON array of language codes.
ALTER TABLE sources ADD COLUMN languages TEXT NOT NULL DEFAULT '';

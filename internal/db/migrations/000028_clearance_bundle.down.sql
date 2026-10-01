-- Clearance material is keyed by source and origin, so dropping the table is
-- the only reversal. The previous material lived in plugin_storage under
-- reserved keys and is left in place so existing installs keep working.
DROP INDEX IF EXISTS clearance_bundle_source_idx;

DROP TABLE IF EXISTS clearance_bundle;
-- Containment index for custom-property value filtering (MUL-4463).
-- Separate single-statement migration: CREATE INDEX CONCURRENTLY cannot run
-- inside a transaction or share a migration, and a non-concurrent build would
-- block writes on the hot issue table for the duration of the scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_properties_gin
    ON issue USING GIN (properties jsonb_path_ops);

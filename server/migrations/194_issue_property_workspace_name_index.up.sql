-- Enforce case-insensitive property-name uniqueness without blocking writes.
-- Keep this as the migration's only statement: PostgreSQL rejects CREATE INDEX
-- CONCURRENTLY inside a transaction or multi-command string.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_property_ws_name
    ON issue_property (workspace_id, LOWER(name));

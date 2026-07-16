-- Case-insensitive status-name uniqueness per workspace, active rows only.
-- Keep this as the migration's only statement: PostgreSQL rejects CREATE INDEX
-- CONCURRENTLY inside a transaction or multi-command string.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_status_workspace_name_active_uidx
    ON issue_status (workspace_id, LOWER(name))
    WHERE archived_at IS NULL;

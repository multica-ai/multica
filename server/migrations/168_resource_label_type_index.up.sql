-- This is intentionally a single statement: concurrent index creation cannot
-- run in a transaction or a multi-command migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_label_workspace_type_idx
    ON issue_label (workspace_id, resource_type);

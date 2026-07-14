-- This is intentionally a single statement: concurrent index creation cannot
-- run in a transaction or a multi-command migration.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_label_workspace_type_name_lower_idx
    ON issue_label (workspace_id, resource_type, LOWER(name));

-- Partial index behind ListIssuePropertiesWithUsage: the usage census only
-- reads issues that carry at least one custom-property value, so it scans this
-- (typically small) slice of the workspace instead of every issue row.
-- idx_issue_properties_gin cannot help — jsonb_path_ops has no key-existence
-- support.
--
-- Single-statement file: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_with_properties
    ON issue (workspace_id)
    WHERE properties <> '{}'::jsonb;

-- Non-partial (workspace_id) index covering ALL rows, including archived ones.
-- The three catalog uniqueness indexes (203-205) are partial (active rows /
-- non-NULL system_key), so none of them can serve the workspace-scoped
-- delete/cleanup path that must remove every issue_status row for a workspace
-- regardless of archived_at. Built CONCURRENTLY in its own single-statement
-- migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_status_workspace_id_idx
    ON issue_status (workspace_id);

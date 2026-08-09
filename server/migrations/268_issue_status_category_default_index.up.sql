-- At MOST one active default status per (workspace, category). This partial
-- unique index enforces the upper bound only; it cannot guarantee that a
-- default exists. Ensuring at LEAST one default per Category is the service
-- layer's responsibility (the seed creates one, and later archival / default-
-- reassign flows must run in a transaction that never leaves a Category with
-- zero defaults). The default is what a Category alias (backlog | todo |
-- in_progress | done | cancelled) resolves to when writing an issue status.
-- Single-statement migration (CONCURRENTLY).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_status_workspace_category_default_uidx
    ON issue_status (workspace_id, category)
    WHERE is_default = TRUE AND archived_at IS NULL;

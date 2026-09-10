-- List query shape: workspace_id + scope columns, then either "mine" or
-- "shared with workspace" (OR of two branches — see 256 for the shared
-- branch; the planner combines them via BitmapOr). Kept as its own
-- single-statement file: concurrent index builds cannot run inside a
-- transaction or a multi-command migration.
CREATE INDEX CONCURRENTLY idx_issue_view_owner
    ON issue_view (workspace_id, scope_type, scope_id, owner_id);

-- Serves the per-request "which private projects can this user see" lookup and
-- the member-removal cleanup, both keyed by (workspace, user).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_member_workspace_user
    ON project_member (workspace_id, user_id);

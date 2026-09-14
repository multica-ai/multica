-- Serve workspace plan inventory and tenant-scoped cleanup.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_workspace_id_idx
    ON project_plan (workspace_id);

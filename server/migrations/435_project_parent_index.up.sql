CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_parent
    ON project(parent_project_id)
    WHERE parent_project_id IS NOT NULL;

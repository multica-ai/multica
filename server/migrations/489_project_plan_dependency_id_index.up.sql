-- Dependency identity.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_project_plan_dependency_id
    ON project_plan_dependency (id);

-- Plan identity, row locking, and child validation.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_project_plan_id
    ON project_plan (id);

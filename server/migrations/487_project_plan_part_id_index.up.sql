-- Part identity, row locking, and endpoint validation.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_project_plan_part_id
    ON project_plan_part (id);

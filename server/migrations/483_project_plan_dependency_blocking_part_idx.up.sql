-- Serve dependency lookup for blocking parts.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_dependency_blocking_part_idx
    ON project_plan_dependency (project_plan_id, blocking_part_id)
    WHERE blocking_part_id IS NOT NULL;

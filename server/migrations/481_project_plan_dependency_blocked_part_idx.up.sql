-- Serve dependency lookup for blocked parts.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_dependency_blocked_part_idx
    ON project_plan_dependency (project_plan_id, blocked_part_id)
    WHERE blocked_part_id IS NOT NULL;

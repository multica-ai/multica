-- Serve dependency lookup for blocked phases.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_dependency_blocked_phase_idx
    ON project_plan_dependency (project_plan_id, blocked_phase_id)
    WHERE blocked_phase_id IS NOT NULL;

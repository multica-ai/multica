-- Serve dependency lookup for blocking phases.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_dependency_blocking_phase_idx
    ON project_plan_dependency (project_plan_id, blocking_phase_id)
    WHERE blocking_phase_id IS NOT NULL;

-- Serve plan-wide part scans and same-plan phase validation.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_part_plan_phase_idx
    ON project_plan_part (project_plan_id, project_plan_phase_id);

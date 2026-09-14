-- Enforce phase positions within a plan and serve ordered phase reads.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_plan_phase_plan_position_key
    ON project_plan_phase (project_plan_id, position);

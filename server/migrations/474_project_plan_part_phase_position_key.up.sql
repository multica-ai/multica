-- Enforce part positions within a phase and serve ordered part reads.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_plan_part_phase_position_key
    ON project_plan_part (project_plan_phase_id, position);

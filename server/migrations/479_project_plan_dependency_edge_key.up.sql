-- Prevent duplicate dependency edges across nullable phase and part endpoints.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_plan_dependency_edge_key
    ON project_plan_dependency (
        project_plan_id,
        blocked_phase_id,
        blocked_part_id,
        blocking_phase_id,
        blocking_part_id
    ) NULLS NOT DISTINCT;

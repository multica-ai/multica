-- Enforce at most one active plan per project and serve active-plan lookup.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_plan_project_id_active_idx
    ON project_plan (project_id)
    WHERE superseded_at IS NULL;

-- Enforce one retained plan row per project version and serve history ordering.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_plan_project_version_key
    ON project_plan (project_id, version);

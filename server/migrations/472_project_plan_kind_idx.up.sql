-- Serve kind-filtered plan inventory and application-level kind validation.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_kind_idx
    ON project_plan (kind);

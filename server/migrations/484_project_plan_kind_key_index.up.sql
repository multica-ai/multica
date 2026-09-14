-- Catalog identity and kind validation lookup.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_project_plan_kind_key
    ON project_plan_kind (key);

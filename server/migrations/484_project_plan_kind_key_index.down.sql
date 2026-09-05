-- Remove catalog identity and kind validation lookup support.
DROP INDEX CONCURRENTLY IF EXISTS idx_project_plan_kind_key;

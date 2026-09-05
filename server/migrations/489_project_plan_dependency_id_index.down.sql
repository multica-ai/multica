-- Remove dependency identity support.
DROP INDEX CONCURRENTLY IF EXISTS idx_project_plan_dependency_id;

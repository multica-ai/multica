-- Remove active-plan uniqueness and lookup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_project_id_active_idx;

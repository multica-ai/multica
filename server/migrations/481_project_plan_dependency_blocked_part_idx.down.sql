-- Remove blocked-part dependency lookup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_dependency_blocked_part_idx;

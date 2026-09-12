-- Remove blocking-part dependency lookup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_dependency_blocking_part_idx;

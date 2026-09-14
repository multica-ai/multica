-- Remove kind-filtered plan inventory support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_kind_idx;

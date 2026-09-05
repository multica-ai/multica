-- Remove workspace plan inventory and cleanup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_workspace_id_idx;

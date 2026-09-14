-- Remove plan-part membership read and coverage rollup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_part_issue_plan_part_issue_idx;

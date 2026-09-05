-- Remove reverse live-issue membership lookup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_part_issue_issue_id_idx;

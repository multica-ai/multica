-- Remove membership identity support.
DROP INDEX CONCURRENTLY IF EXISTS idx_project_plan_part_issue_id;

-- Remove per-plan live issue membership uniqueness.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_part_issue_plan_issue_key;

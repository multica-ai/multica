-- Remove reverse issue-origin provenance lookup support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_source_issue_id_idx;

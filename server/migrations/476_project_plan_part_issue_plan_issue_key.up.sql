-- Prevent a live issue from being assigned to multiple parts in one plan.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_plan_part_issue_plan_issue_key
    ON project_plan_part_issue (project_plan_id, issue_id);

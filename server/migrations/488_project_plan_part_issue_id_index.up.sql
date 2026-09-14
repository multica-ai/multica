-- Membership identity.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_project_plan_part_issue_id
    ON project_plan_part_issue (id);

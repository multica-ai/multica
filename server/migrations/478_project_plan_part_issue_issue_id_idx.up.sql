-- Serve reverse live-issue membership lookup without indexing snapshots.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_part_issue_issue_id_idx
    ON project_plan_part_issue (issue_id)
    WHERE issue_id IS NOT NULL;

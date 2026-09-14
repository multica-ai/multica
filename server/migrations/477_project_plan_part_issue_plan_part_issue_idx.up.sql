-- Serve plan-part membership reads and coverage rollups.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_part_issue_plan_part_issue_idx
    ON project_plan_part_issue (
        project_plan_id,
        project_plan_part_id,
        issue_id
    );

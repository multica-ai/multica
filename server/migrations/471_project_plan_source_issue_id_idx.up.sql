-- Serve reverse provenance lookup for issue-origin plans without indexing nulls.
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_plan_source_issue_id_idx
    ON project_plan (source_issue_id)
    WHERE source_issue_id IS NOT NULL;

-- The newest run of each Librarian job in a workspace.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_health_run_job_idx
    ON knowledge_health_run (workspace_id, job, ran_at DESC);

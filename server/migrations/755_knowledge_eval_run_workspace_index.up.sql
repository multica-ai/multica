-- The newest runs of a workspace, for the digest and the run history.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_eval_run_workspace_ran_idx
    ON knowledge_eval_run (workspace_id, ran_at DESC);

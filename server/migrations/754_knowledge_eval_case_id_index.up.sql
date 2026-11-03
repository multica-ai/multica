-- Identifies an eval case within its workspace, for read, update and delete.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_eval_case_workspace_id_uidx
    ON knowledge_eval_case (workspace_id, id);

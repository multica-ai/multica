-- One row per run, scope and repository: a retried upload of the same run is a
-- no-op rather than a second card.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_code_change_task_uidx
    ON task_code_change (task_id, scope, repo_key);

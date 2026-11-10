-- One row per environment per task: the daemon's snapshot upserts on it.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_issue_environment_task_key
    ON issue_environment (task_id, local_key);

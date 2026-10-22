CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_supplement_capability_task_uidx
    ON task_supplement_capability (task_id);

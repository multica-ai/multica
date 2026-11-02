CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_supplement_task_request_uidx
    ON task_supplement (task_id, client_request_id);

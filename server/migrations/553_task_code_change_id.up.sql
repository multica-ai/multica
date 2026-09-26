CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_code_change_id_idx
    ON task_code_change (id);

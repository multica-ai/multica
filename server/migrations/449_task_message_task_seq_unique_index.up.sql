CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_message_task_id_seq_uidx ON task_message (task_id, seq);

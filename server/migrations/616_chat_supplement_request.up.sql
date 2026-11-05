CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_supplement_request ON chat_supplement (task_id, author_id, client_request_id);

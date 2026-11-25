CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_doc ON agent_task_queue (doc_id, created_at DESC) WHERE doc_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS agent_task_wakeup_lookup_idx ON agent_task_queue ((context->>'wakeup_id'),created_at DESC) WHERE context->>'wakeup_id' IS NOT NULL;

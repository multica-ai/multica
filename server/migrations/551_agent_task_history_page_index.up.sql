-- Match ListAgentTasks visibility and keyset order for bounded history reads.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_history_page
ON agent_task_queue (agent_id, created_at DESC, id DESC)
WHERE NOT (escalation_for_task_id IS NOT NULL AND started_at IS NULL
           AND status IN ('deferred', 'cancelled'));

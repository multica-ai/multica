-- Both cleanup failures and pending cancellation acknowledgements participate
-- in the start guard. Exclude ordinary history while serving the OR predicate
-- and the latest-unsafe-run ordering from one small index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_process_cleanup
    ON agent_task_queue (agent_id, runtime_id, completed_at DESC, id DESC)
    WHERE failure_reason = 'cleanup_unconfirmed'
       OR (status = 'cancelled' AND result->>'process_cleanup_pending' = 'true');

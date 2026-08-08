-- Supports the outbound reconciler's "has this failed attempt already been
-- retried?" test. Without it the candidate scan does a full pass over
-- agent_task_queue per failed candidate, on a table that only grows.
--
-- Partial: retry_of_task_id is NULL on every task that is not a retry, which is
-- almost all of them, and the lookup only ever asks about non-NULL values.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_retry_of
    ON agent_task_queue (retry_of_task_id)
    WHERE retry_of_task_id IS NOT NULL;

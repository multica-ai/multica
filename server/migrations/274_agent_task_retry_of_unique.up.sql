-- Concurrency-safe uniqueness for the system-retry successor (MUL-4809 §4.1 P0-2).
-- System retries form a linear chain — each attempt has at most one successor — so
-- retry_of_task_id is unique among non-NULL rows. Making that a real constraint lets
-- both the FailTask/HandleFailedTasks retry path and the autopilot reconcile back-fill
-- (ON CONFLICT DO NOTHING) create the missing retry idempotently without racing into a
-- duplicate attempt. Partial (retry_of_task_id IS NOT NULL) so it only covers retries;
-- built CONCURRENTLY in its own single-statement migration because agent_task_queue is
-- a hot table.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_retry_of_task_id_unique
    ON agent_task_queue (retry_of_task_id)
    WHERE retry_of_task_id IS NOT NULL;

-- Covers sampler reads over running tasks:
--   * task_stuck: status = 'running' AND started_at < now() - interval '30 minutes'
--   * task_running: the running half of the in-flight task count
--
-- agent_task_queue is hot, so this must stay in its own single-statement
-- migration and must use CONCURRENTLY. The migration runner executes files
-- outside an explicit transaction; keeping CONCURRENTLY isolated avoids
-- Postgres' implicit transaction block for multi-statement query strings.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_running_started_at
    ON agent_task_queue (started_at)
    WHERE status = 'running';

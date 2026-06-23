ALTER TABLE agent_task_queue
  ADD COLUMN prepare_lease_expires_at TIMESTAMPTZ;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_dispatched_prepare
  ON agent_task_queue (runtime_id, priority DESC, dispatched_at ASC)
  WHERE status = 'dispatched' AND started_at IS NULL;

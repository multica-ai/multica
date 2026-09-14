-- Nullable, no default: PostgreSQL only updates table metadata. Bound the
-- ACCESS EXCLUSIVE lock wait so a busy task queue cannot stall deployment.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS resource_usage JSONB;

COMMENT ON COLUMN agent_task_queue.resource_usage IS
    'Per-run cgroup/process-group accounting reported by the daemon, including memory OOM evidence and isolation fallback diagnostics.';

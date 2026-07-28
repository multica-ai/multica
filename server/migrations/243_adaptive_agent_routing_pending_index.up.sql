-- Single-statement concurrent migration: the runtime sweeper recovers pending
-- admissions by age, while ordinary claim queries exclude them.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_adaptive_pending
    ON agent_task_queue (created_at, id)
    WHERE status = 'queued' AND route_admission_state = 'pending';

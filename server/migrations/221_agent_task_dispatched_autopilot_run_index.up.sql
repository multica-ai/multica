-- Repair lookup: find the task an autopilot run dispatched (MUL-4809 §4.1). Partial
-- so it only indexes autopilot-dispatched tasks. Built CONCURRENTLY in its own
-- single-statement migration because agent_task_queue is a hot table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_dispatched_autopilot_run
    ON agent_task_queue (dispatched_autopilot_run_id)
    WHERE dispatched_autopilot_run_id IS NOT NULL;

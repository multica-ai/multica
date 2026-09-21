-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- GetAutopilotTaskByRun recovers run_only dispatches whose task INSERT committed
-- before autopilot_run.task_id was linked. Without an autopilot_run_id-leading
-- index, finding the earliest task scans and sorts the entire task queue.
-- Automatic retries inherit the same autopilot_run_id, so this index must remain
-- non-unique. The partial predicate avoids indexing ordinary non-Autopilot tasks.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_autopilot_run_created_at
    ON agent_task_queue (autopilot_run_id, created_at)
    WHERE autopilot_run_id IS NOT NULL;

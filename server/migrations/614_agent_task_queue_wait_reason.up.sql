-- Per-task queue-wait attribution (#8551, display half): the outcome of the
-- claim poll that saw this queued task and could not claim it. Written only
-- when the value changes, cleared on claim. Nullable TEXT with no CHECK and
-- a Go-side closed enum, matching failure_reason and autopilot_runs.reason_code.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS queue_wait_reason TEXT;

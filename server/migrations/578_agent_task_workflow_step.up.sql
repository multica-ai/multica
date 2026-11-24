-- The workflow step a run is working on (MUL-7420): the issue's status when
-- the task was queued, moved along by the run's own status changes. When the
-- issue has left that step — someone else moved it while the run worked — the
-- run's status changes are refused instead of undoing that move.
--
-- Written by the task INSERTs from the issue row, and by the status-change
-- path when the run moves the issue itself; nothing else writes it. NULL on
-- rows written before this migration and on tasks without an issue, and NULL
-- means "not checked", never "matches".
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS workflow_step TEXT;

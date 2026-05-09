-- MUL-1899: re-introduce the 'skipped' terminal status for autopilot_run.
-- Migration 043 removed 'skipped' along with the broken concurrency_policy
-- feature, but the offline-runtime admission gate added in this PR needs a
-- non-failure terminal status to record dispatches that were intentionally
-- declined (e.g. assignee runtime is offline). Reusing 'failed' would
-- pollute the failure-rate signal that drives the auto-pause monitor.
ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('issue_created', 'running', 'completed', 'failed', 'skipped'));

-- Partial index on status for in-flight runs is unchanged: 'skipped' is
-- terminal so the existing index (issue_created/running) still matches.

-- Partial index to make the queued-task TTL sweeper (sweepExpiredQueuedTasks
-- in cmd/server/runtime_sweeper.go) cheap. The sweeper runs every 30s and
-- looks up the oldest queued tasks with:
--   WHERE status = 'queued' AND created_at < now() - interval '...'
--   ORDER BY created_at ASC LIMIT 500
-- Without a queued-only partial index on created_at this devolves into a
-- full scan once historical terminal rows accumulate (MUL-1899 baseline:
-- ~89k+ rows). The partial index stays tiny because only in-flight rows
-- live in 'queued'.
CREATE INDEX IF NOT EXISTS idx_agent_task_queue_queued_created_at
    ON agent_task_queue (created_at)
    WHERE status = 'queued';

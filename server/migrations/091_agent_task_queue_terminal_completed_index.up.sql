-- Partial index supporting GetRuntimeRunDurationByDay (MUL-2283 — Daily
-- Runtime Duration chart). The query filters by runtime_id, restricts to
-- terminal rows ('completed' / 'failed') with non-null started_at /
-- completed_at, and buckets by completed_at in the runtime's local tz.
--
-- Without a runtime-scoped partial index on completed_at the planner falls
-- back to a runtime_id-only scan and re-evaluates the terminal-status filter
-- per row. As terminal rows accumulate over the lifetime of a runtime this
-- gets expensive; the partial index keeps the working set bounded to the
-- subset the chart actually reads.
--
-- CONCURRENTLY because agent_task_queue is hot — a plain CREATE INDEX would
-- take an ACCESS EXCLUSIVE lock and block the dispatch path during build.
-- Matches the pattern in 080; the migration runner cannot mix CONCURRENTLY
-- with other statements in the same file, so this lives in its own
-- single-statement migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_terminal_completed
    ON agent_task_queue (runtime_id, completed_at)
    WHERE status IN ('completed', 'failed')
      AND started_at IS NOT NULL
      AND completed_at IS NOT NULL;

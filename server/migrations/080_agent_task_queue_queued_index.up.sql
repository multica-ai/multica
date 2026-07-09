-- Partial index that backs the queued-task TTL sweeper added in MUL-1899
-- (sweepExpiredQueuedTasks in cmd/server/runtime_sweeper.go). The sweeper
-- runs every 30s and looks up the oldest queued tasks with:
--   WHERE status = 'queued' AND created_at < now() - interval '...'
--   ORDER BY created_at ASC LIMIT 500
-- Without a queued-only partial index on created_at this devolves into a
-- full scan once historical terminal rows accumulate (MUL-1899 baseline:
-- ~89k+ rows). The partial index stays tiny because only in-flight rows
-- live in 'queued'.
--
-- CONCURRENTLY because agent_task_queue is hot — a plain CREATE INDEX would
-- take an ACCESS EXCLUSIVE lock and block the dispatch path during build.
-- Matches the pattern in 035/067/074/075/078; 068 documents that the
-- migration runner cannot mix CONCURRENTLY with other statements in the
-- same file, so this lives in its own single-statement migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_queued_created_at
    ON agent_task_queue (created_at)
    WHERE status = 'queued';

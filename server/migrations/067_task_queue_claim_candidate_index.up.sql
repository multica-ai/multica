-- Partial index that backs ListQueuedClaimCandidatesByRuntime. Daemons poll
-- /tasks/claim every 30s per runtime; the filter "runtime_id = $1 AND
-- status = 'queued'" runs every poll and is the dominant cost on warm paths.
-- Restricting to status = 'queued' keeps the index tiny — terminal-state
-- rows (completed/failed/cancelled) accumulate forever in the table but are
-- excluded from the index, so it stays bounded by current queue depth.
-- ORDER BY priority DESC, created_at ASC mirrors the SELECT so the planner
-- can serve the query as an index-only scan without an extra sort.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_claim_candidates
    ON agent_task_queue (runtime_id, priority DESC, created_at ASC)
    WHERE status = 'queued';

-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction. Replays use this partial index to load one attempt's task set
-- in its original ordinal order without touching unrelated queue rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_claim_attempt
    ON agent_task_queue (claim_attempt_id, claim_attempt_ordinal)
    WHERE claim_attempt_id IS NOT NULL;

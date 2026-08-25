-- ALL-211 P0 (BLOCKING 3): prepare autopilot_run for the partial unique
-- index uq_autopilot_run_inflight (migration 433) by cleaning up historical
-- data that would violate it.
--
-- The index guarantees at most ONE in-flight run per autopilot, where
-- "in-flight" means status IN ('issue_created', 'running'). Historical rows
-- may already violate that invariant (a buggy schedule used to start a
-- second concurrent run before the ALL-206 mutex gate, and a run stuck in
-- issue_created/running for an autopilot whose issue never reached a
-- terminal state leaves the slot occupied forever).
--
-- Policy: for each autopilot, keep the NEWEST in-flight run and terminalize
-- every older one as failed with reason_code='migration_cleanup'. Keeping the
-- newest preserves the run that is most likely still doing real work; the
-- terminalized rows become visible to the failure monitor instead of
-- silently wedging the autopilot.
WITH ranked_runs AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY autopilot_id
               ORDER BY created_at DESC
           ) AS rn
    FROM autopilot_run
    WHERE status IN ('issue_created', 'running')
)
UPDATE autopilot_run
SET status = 'failed',
    failure_reason = 'Terminated during migration 432 to enforce unique in-flight constraint',
    reason_code = 'migration_cleanup',
    completed_at = NOW()
WHERE id IN (SELECT id FROM ranked_runs WHERE rn > 1);

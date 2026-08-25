-- Remove the documentation comment from the index (the index itself is
-- dropped by migration 433 down; this just clears the comment if 433 down
-- was skipped).
COMMENT ON INDEX uq_autopilot_run_inflight IS NULL;

-- Document uq_autopilot_run_inflight for operators. Lives in its own
-- migration because the index was created CONCURRENTLY (migration 433),
-- which the migration runner cannot mix with other statements in one file.
COMMENT ON INDEX uq_autopilot_run_inflight IS
'Ensures at most one in-flight run per autopilot. Partial index only covers non-terminal statuses (issue_created, running). 23505 conflicts are handled as already_active dispatch skip.';

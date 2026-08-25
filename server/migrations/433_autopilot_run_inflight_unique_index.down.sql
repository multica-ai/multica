-- Drop the in-flight mutual-exclusion index. After this, concurrent
-- autopilot dispatch loses its DB-level guarantee and falls back to the
-- pre-ALL-211 check-then-insert behavior — a deliberate rollback only.
DROP INDEX CONCURRENTLY IF EXISTS uq_autopilot_run_inflight;

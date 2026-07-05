-- Task-level liveness lease for the 'running' phase, consumed by the
-- FailStaleTasks sweep. While the daemon keeps renewing this lease the
-- sweeper leaves the task alone, so a healthy long run (heavy compute,
-- multi-hour research) is no longer failed on wall clock alone.
--
-- History: migration 055 added last_heartbeat_at for exactly this purpose
-- ("telling stale tasks apart from long-running ones") and migration 069
-- dropped it because no consumer was ever built. This column re-introduces
-- the signal as an expiry lease (mirroring prepare_lease_expires_at from
-- migration 124) together with its consumer.
ALTER TABLE agent_task_queue
  ADD COLUMN run_lease_expires_at TIMESTAMPTZ;

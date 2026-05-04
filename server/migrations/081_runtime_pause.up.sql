-- Pause / Unpause for agent_runtime. paused_at is the source of truth — when
-- it is non-NULL the runtime is paused, regardless of the existing online/
-- offline status (which is heartbeat-driven and orthogonal to pause).
--
-- unpause_at is optional. When set, a periodic sweeper transitions the runtime
-- back to unpaused at that instant. NULL means "stay paused until manually
-- unpaused" (useful when the rate-limit response gave us no reset hint).
--
-- pause_reason is a short slug for telemetry/UI ('rate_limit', 'manual', ...).
ALTER TABLE agent_runtime
    ADD COLUMN paused_at    TIMESTAMPTZ,
    ADD COLUMN unpause_at   TIMESTAMPTZ,
    ADD COLUMN pause_reason TEXT;

-- Backs the unpause sweeper: SELECT ... WHERE paused_at IS NOT NULL AND
-- unpause_at <= now(). The partial index keeps it tiny — only paused rows
-- are indexed, which in steady state is zero.
CREATE INDEX IF NOT EXISTS idx_agent_runtime_unpause_due
    ON agent_runtime (unpause_at)
    WHERE paused_at IS NOT NULL AND unpause_at IS NOT NULL;

ALTER TABLE agent_runtime
    ADD COLUMN hold_until TIMESTAMPTZ,
    ADD COLUMN hold_reason TEXT;

-- Partial index supporting the runtime sweeper's ClearExpiredHolds scan
-- (WHERE hold_until IS NOT NULL AND hold_until <= now()). Only runtimes
-- currently on hold are indexed, so the index stays tiny and the sweep
-- cost is bounded by the held-runtime count rather than the whole fleet.
CREATE INDEX IF NOT EXISTS idx_agent_runtime_hold_until
    ON agent_runtime (hold_until)
    WHERE hold_until IS NOT NULL;

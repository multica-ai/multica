-- Cerebro-only (FIR-4508): pause / unpause for agent rows.
--
-- Multi-provider runtimes (Hermes) host many independent LLM backends behind
-- one Multica runtime. A single backend's auth/quota failure must pause only
-- the agent that hit it — not the whole runtime and every sibling agent.
--
-- Mirrors 9016_cerebro_runtime_pause + 9050_cerebro_runtime_auto_pause_count
-- on the agent table. paused_at is the source of truth; unpause_at schedules
-- auto-resume via the agent unpause sweeper; pause_reason is a short slug
-- (rate_limit, auth_error, manual, ...); auto_pause_count drives the same
-- 2h/4h/6h circuit breaker used for runtime auto-pause.
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS paused_at         TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS unpause_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pause_reason      TEXT,
    ADD COLUMN IF NOT EXISTS auto_pause_count  INTEGER NOT NULL DEFAULT 0;

-- Backs the agent unpause sweeper: SELECT ... WHERE paused_at IS NOT NULL AND
-- unpause_at <= now(). Partial index keeps it tiny — only paused rows.
CREATE INDEX IF NOT EXISTS idx_agent_unpause_due
    ON agent (unpause_at)
    WHERE paused_at IS NOT NULL AND unpause_at IS NOT NULL;

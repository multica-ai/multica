-- Cerebro-only (FIR-2476): circuit-breaker counter for the auto-pause loop.
--
-- auto_pause_count tracks how many consecutive auto-pauses a runtime has hit
-- WITHOUT an intervening successful task completion. The auto-pause path
-- (cerebro/runtime/auto_pause.go) increments it on every rate-limit / usage-cap
-- pause and CompleteTask resets it to 0 on the next successful run. Two things
-- key on it:
--
--   1. Growing backoff — when the provider error carries no parseable reset
--      time, the fallback pause grows 5m → 10m → 20m … up to a 2h cap instead
--      of bouncing every fixed 5 minutes (the FIR-2476 retry-storm).
--   2. Circuit breaker — after N consecutive auto-pauses the path stops
--      scheduling auto-resume (leaves unpause_at NULL so the sweeper never
--      unpauses) and posts one comment so a human can intervene.
ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS auto_pause_count INTEGER NOT NULL DEFAULT 0;

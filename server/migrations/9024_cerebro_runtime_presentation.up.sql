-- Cerebro: per-runtime presentation mode.
-- 'headless' (default) preserves upstream stream-json behaviour. 'interactive'
-- spawns the runtime under a PTY so a Multica client can attach and stream
-- stdin/stdout. Pure data column; no FK changes.

ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS presentation_mode TEXT NOT NULL DEFAULT 'headless'
        CHECK (presentation_mode IN ('headless', 'interactive'));

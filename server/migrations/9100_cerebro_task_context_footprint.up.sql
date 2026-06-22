-- FIR-1856: Codex runtimes measured the context window wrong.
--
-- The "context used" indicator (FIR-1741, server/internal/cerebro/sessions)
-- reads task_usage and treats input + cache_read + cache_write as the size of
-- the prompt the model last read — i.e. how full the window is. That is correct
-- for Claude, where the recorded figure is the final turn's footprint, but wrong
-- for Codex/gpt runtimes: codex.go records Codex' `total_token_usage`, the SUM
-- of every turn in the thread. Dividing that lifetime sum by the 272k window
-- pins the indicator at 100% on any multi-turn Codex run (observed 1955k/272k).
--
-- The lifetime sum is correct for cost (task_usage stays untouched), so we keep
-- it there and store the LAST turn's footprint separately, here, for the
-- window-fullness measurement only. One row per task: the latest run's last-turn
-- prompt size (input already includes cached for OpenAI) and its cached subset.
CREATE TABLE IF NOT EXISTS cerebro_task_context_footprint (
    task_id UUID PRIMARY KEY REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    model TEXT NOT NULL DEFAULT '',
    input_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

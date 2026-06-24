-- FIR-1931: per-run context-footprint history so the session sheet can draw how
-- the context window filled up over a session, and mark where it dropped
-- (compaction / fresh turn).
--
-- The existing cerebro_task_context_footprint keeps ONE row per task (the last
-- turn, for the gauge). The footprint is recorded once per agent run, at the
-- run's completion — confirmed in firtal_gateway_executor.recordTaskUsage and
-- the daemon ReportTaskUsage handler. So this table APPENDS one row per run
-- (= per recorded footprint); a session has many runs, and plotting each run's
-- footprint over time is the development curve over that session. (True
-- per-turn-within-a-run granularity would require emitting every turn from each
-- runtime backend — a larger, separate change.)
CREATE TABLE IF NOT EXISTS cerebro_task_context_footprint_history (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id           UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    model             TEXT NOT NULL DEFAULT '',
    input_tokens      BIGINT NOT NULL DEFAULT 0,   -- whole-prompt footprint that run (already includes cached for OpenAI/gateway accounting)
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    observed_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ctx_footprint_history_task
    ON cerebro_task_context_footprint_history (task_id, observed_at);

-- Latest context-window occupancy reported by the run, so surfaces
-- that list runs can show it without fetching each run's transcript.
--
-- Nullable on purpose: NULL means the runtime reported nothing, which is not
-- the same as an empty window. Tokens, not a percentage — the limit can be
-- unknown even when the occupancy is not, and a percentage derived from a
-- guessed window would read as a measurement.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS context_used_tokens BIGINT,
    ADD COLUMN IF NOT EXISTS context_limit_tokens BIGINT;

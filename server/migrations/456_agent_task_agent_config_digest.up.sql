-- agent_config_digest fingerprints the agent configuration this run actually
-- delivered to the model. The next run on the same (agent, issue) pair compares
-- it against its own digest and refuses to resume a session that was built from
-- a different configuration (GH #8070 / MUL-7082).
--
-- Nullable because rows written before this column exists have no answer, and
-- because one task row can be delivered twice under different configurations
-- (a stale reclaim refreshes dispatched_at) — that case stores an explicit
-- ambiguity marker instead. Both read as "do not resume": only an exact match
-- resumes, so every (agent, issue) pair with a live session takes one cold
-- start on the deploy that ships this, then records a real digest per run.
ALTER TABLE agent_task_queue
    ADD COLUMN agent_config_digest TEXT;

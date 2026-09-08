-- agent_config_digest fingerprints the agent configuration this run actually
-- delivered to the model. The next run on the same (agent, issue) pair compares
-- it against its own digest and refuses to resume a session that was built from
-- a different configuration (GH #8070 / MUL-7082).
--
-- Nullable because rows written before this column exists have no answer. A row
-- that cannot identify the delivery behind it stores an explicit ambiguity
-- marker instead: a redelivery carrying a different configuration, and a
-- reclaim of a row this migration left in flight — there NULL means "delivered
-- by the old server", not "never delivered", and the delivery it already made
-- can still be the one that starts.
--
-- Both read as "do not resume": only an exact match resumes, so every
-- (agent, issue) pair with a live session takes one cold start on the deploy
-- that ships this, then records a real digest per run.
ALTER TABLE agent_task_queue
    ADD COLUMN agent_config_digest TEXT;

-- agent_config_digest fingerprints the agent configuration this run actually
-- delivered to the model. The next run on the same (agent, issue) pair compares
-- it against its own digest and refuses to resume a session that was built from
-- a different configuration (GH #8070 / MUL-7082).
--
-- Nullable on purpose: rows written before this column exists carry NULL, which
-- the claim handler reads as "unknown" and resumes, so deploying the gate does
-- not cold-start every live conversation at once. Each task records its digest
-- at claim time, so the gate becomes effective for a pair on its next run.
ALTER TABLE agent_task_queue
    ADD COLUMN agent_config_digest TEXT;

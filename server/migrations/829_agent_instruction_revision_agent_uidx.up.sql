-- One row per agent and revision number. Writers take the agent row lock before
-- choosing the next number, and this key makes a lost race fail instead of
-- duplicating a revision. It also serves the history list, newest first.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_instruction_revision_agent_uidx
    ON agent_instruction_revision (workspace_id, agent_id, revision);

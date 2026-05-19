-- Cerebro agent-pass uniqueness (JEH-1731).
--
-- 9030 created a non-unique partial index on (agent_id, issue_id) WHERE
-- status='active'. That keeps the gate lookup O(log n) but does not
-- prevent two concurrent INSERTs from creating two active passes for
-- the same (agent, issue) pair — the second pass would silently shadow
-- the first and the gate would only see one of them.
--
-- The admin UI introduced in JEH-1731 lets users create passes directly,
-- which makes a real-world race possible. Turn the index into a unique
-- one so the database refuses the duplicate and the handler returns 409.
--
-- We drop the old non-unique index first so the catalog ends up with a
-- single index (idempotent on re-up — IF EXISTS guards both drop and
-- recreate).

DROP INDEX IF EXISTS idx_cerebro_agent_pass_active_agent_issue;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_agent_pass_active_agent_issue
    ON cerebro_agent_pass (agent_id, issue_id)
    WHERE status = 'active';

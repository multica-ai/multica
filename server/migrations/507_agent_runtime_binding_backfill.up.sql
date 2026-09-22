-- Every agent that currently names a runtime keeps it as its priority-0
-- binding; an agent with no runtime (NULL) starts with an empty pool and is
-- unaffected, exactly as an unbound agent is today.
--
-- Idempotent by construction: NOT EXISTS makes a re-run insert nothing, and
-- the (agent_id, runtime_id) / (agent_id, priority) unique indexes created
-- above are a second guard. Kept in its own migration, separate from the DDL,
-- so the data pass never shares a transaction with a concurrent index build.
INSERT INTO agent_runtime_binding (workspace_id, agent_id, runtime_id, priority)
SELECT a.workspace_id, a.id, a.runtime_id, 0
FROM agent a
WHERE a.runtime_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM agent_runtime_binding b
      WHERE b.agent_id = a.id AND b.runtime_id = a.runtime_id
  );

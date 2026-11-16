-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Workspace teardown enumerates the workspace's runtimes in bounded pages —
-- `workspace_id = $1 AND id > $cursor ORDER BY id LIMIT n` — so a workspace with
-- a very large owner set never has its whole id list held in the API process
-- (MUL-5999). The existing single-column workspace_id index cannot produce id
-- order, which would put a Sort over the entire owner set in front of every page.
--
-- id rather than a natural key on purpose: a keyset column has to be immutable,
-- and renaming an agent_runtime row mid-teardown would otherwise let it sort behind the
-- cursor and escape the sweep.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_workspace_id_keyset
    ON agent_runtime (workspace_id, id);

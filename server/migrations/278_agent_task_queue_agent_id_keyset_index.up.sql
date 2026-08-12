-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Workspace teardown pages a busy agent's tasks with
-- `agent_id = $1 AND id > $cursor ORDER BY id LIMIT n` (MUL-5999). The existing
-- idx_agent_task_queue_agent is (agent_id, status), which cannot produce id
-- order, so every page would read the agent's entire task set and sort it before
-- applying LIMIT — quadratic in the number of pages, which is exactly the busy
-- agent this issue is about.
--
-- (agent_id, status) stays: the hot dispatch and claim queries filter on status,
-- and this index does not serve them.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_agent_id_keyset
    ON agent_task_queue (agent_id, id);

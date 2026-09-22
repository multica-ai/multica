-- Ordered runtime pool for one agent, lowest priority first. The dispatcher
-- walks this order and sends work to the first live runtime; agent.runtime_id
-- is the priority-0 projection kept for legacy read paths and installed
-- clients. An agent with no rows here falls back to the singleton pool implied
-- by its agent.runtime_id (empty when that is NULL), so this table is
-- authoritative only once populated.
-- name: ListAgentRuntimeBindings :many
SELECT * FROM agent_runtime_binding
WHERE agent_id = $1
ORDER BY priority ASC, id ASC;

-- name: ListAgentRuntimeBindingsForAgentForUpdate :many
-- Serializes an atomic replace of one agent's pool against runtime teardown and
-- a concurrent replace: both lock the agent's binding rows before mutating them.
SELECT * FROM agent_runtime_binding
WHERE agent_id = $1
ORDER BY priority ASC, id ASC
FOR UPDATE;

-- name: DeleteAgentRuntimeBindingsForAgent :exec
DELETE FROM agent_runtime_binding WHERE agent_id = $1;

-- name: CreateAgentRuntimeBinding :one
INSERT INTO agent_runtime_binding (workspace_id, agent_id, runtime_id, priority)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: DeleteAgentRuntimeBindingsByRuntime :many
-- Runtime teardown removes the deleted runtime from every pool it appears in
-- and reports which agents were affected, so the caller can promote each one's
-- next binding. RETURNING the agent ids (not the rows) is all promotion needs.
DELETE FROM agent_runtime_binding
WHERE runtime_id = $1
RETURNING agent_id;

-- name: PromoteAgentRuntimeToLowestBinding :many
-- After a runtime is removed from the given agents' pools, repoint each agent's
-- legacy runtime_id projection at its lowest-priority remaining binding. Agents
-- with no binding left are not matched here; their runtime_id keeps pointing at
-- the deleted runtime so the caller's existing unbind-by-runtime pass nulls them
-- and pauses their autopilots (an emptied pool is a real unbind, invariant I14).
UPDATE agent a
SET runtime_id = b.runtime_id, updated_at = now()
FROM (
    SELECT DISTINCT ON (agent_id) agent_id, runtime_id
    FROM agent_runtime_binding
    WHERE agent_id = ANY(@agent_ids::uuid[])
    ORDER BY agent_id, priority ASC, id ASC
) b
WHERE a.id = b.agent_id AND a.kind = 'user'
RETURNING a.*;

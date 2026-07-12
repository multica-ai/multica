-- Agent invocation permission targets (MUL-3963). Rows are the allow-list for
-- agents whose permission_mode = 'public_to'. See migration 130.

-- name: ListAgentInvocationTargets :many
SELECT * FROM agent_invocation_target
WHERE agent_id = $1
ORDER BY target_type ASC, created_at ASC;

-- name: ListAgentInvocationTargetsByAgentIDs :many
-- Batch load for the agent list endpoint so we don't N+1 per agent.
SELECT * FROM agent_invocation_target
WHERE agent_id = ANY(@agent_ids::uuid[])
ORDER BY agent_id, target_type ASC, created_at ASC;

-- name: CreateAgentInvocationTarget :exec
-- Idempotent upsert: re-adding an existing (agent, target_type, target)
-- refreshes created_by/created_at rather than erroring. Callers replace the
-- whole set via DeleteAgentInvocationTargets + a series of these, so the
-- ON CONFLICT is belt-and-suspenders against races.
INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
VALUES ($1, $2, $3, sqlc.narg('created_by'))
ON CONFLICT (agent_id, target_type, target_id) DO UPDATE SET
    created_by = EXCLUDED.created_by,
    created_at = now();

-- name: DeleteAgentInvocationTargets :exec
-- Clears every target for an agent. Used before re-writing the allow-list so
-- the update is a wholesale replace, matching the composio_toolkit_allowlist
-- write model.
DELETE FROM agent_invocation_target
WHERE agent_id = $1;

-- name: DeleteAgentInvocationTargetsByMember :exec
-- Removes member-target grants pointing at a leaving user, SCOPED to a single
-- workspace. A user may belong to multiple workspaces; removing them from one
-- must NOT touch their invocation grants on agents in another workspace. Joins
-- through agent (agent_invocation_target has no workspace_id column and no FK)
-- to bound the delete to @workspace_id.
DELETE FROM agent_invocation_target ait
USING agent a
WHERE ait.agent_id = a.id
  AND a.workspace_id = @workspace_id
  AND ait.target_type = 'member'
  AND ait.target_id = @target_id;

-- name: DeleteAgentInvocationTargetsByArchivedRuntimeAgents :exec
-- Application-layer replacement for the (deliberately absent) agent_id ON
-- DELETE CASCADE: removes invocation targets for the archived agents a runtime
-- delete is about to hard-delete. MUST run in the same tx as, and BEFORE,
-- DeleteArchivedAgentsByRuntime so no orphan target rows survive the agent
-- rows they belonged to. Mirrors the agent hard-delete predicate exactly.
DELETE FROM agent_invocation_target
WHERE agent_id IN (
    SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL
);

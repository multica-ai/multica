-- A2A invocation whitelist (NEX-24). Rows back agents whose
-- a2a_invocation_mode = 'specific_agents'. See migration 266.

-- name: ListAgentInvocationGrants :many
SELECT * FROM agent_invocation_grant
WHERE agent_id = $1
ORDER BY grantee_agent_id ASC;

-- name: ListAgentInvocationGrantsByAgentIDs :many
-- Batch load for the agent list endpoint so we don't N+1 per agent.
SELECT * FROM agent_invocation_grant
WHERE agent_id = ANY(@agent_ids::uuid[])
ORDER BY agent_id, grantee_agent_id ASC;

-- name: IsAgentInvocationGranted :one
-- Whether (agent_id, grantee_agent_id) is on the owner's A2A whitelist.
-- Backs the `specific_agents` mode of the A2A invoke gate (a2aInvocationAllowed).
SELECT EXISTS(
    SELECT 1 FROM agent_invocation_grant
    WHERE agent_id = $1 AND grantee_agent_id = $2
) AS is_granted;

-- name: CreateAgentInvocationGrant :exec
-- Idempotent upsert: re-adding an existing (agent_id, grantee_agent_id)
-- refreshes created_by/created_at rather than erroring. Callers replace the
-- whole set via DeleteAgentInvocationGrants + a series of these, so the
-- ON CONFLICT is belt-and-suspenders against races.
INSERT INTO agent_invocation_grant (agent_id, grantee_agent_id, created_by)
VALUES ($1, $2, sqlc.narg('created_by'))
ON CONFLICT (agent_id, grantee_agent_id) DO UPDATE SET
    created_by = EXCLUDED.created_by,
    created_at = now();

-- name: DeleteAgentInvocationGrants :exec
-- Clears every grant for an agent. Used before re-writing the whitelist so
-- the update is a wholesale replace, matching the invocation-target write
-- model.
DELETE FROM agent_invocation_grant
WHERE agent_id = $1;

-- name: DeleteAgentInvocationGrantsByAgent :exec
-- Application-layer replacement for the (deliberately absent) agent_id ON
-- DELETE CASCADE: removes the A2A whitelist rows owned by an agent about to be
-- hard-deleted. MUST run in the same tx as, and BEFORE, the agent delete so no
-- orphan grant rows survive (Stage 3 cleanup wiring).
DELETE FROM agent_invocation_grant
WHERE agent_id = $1;

-- name: DeleteAgentInvocationGrantsByGrantee :exec
-- Application-layer replacement for the (deliberately absent) grantee_agent_id
-- ON DELETE CASCADE: removes every whitelist row that names the deleted agent
-- as a grantee (Stage 3 cleanup wiring).
DELETE FROM agent_invocation_grant
WHERE grantee_agent_id = $1;

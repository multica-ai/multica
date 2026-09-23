-- Global agents (#8775). A global_agent row is owned by a user; the agents
-- that run it are ordinary workspace `agent` rows linked back through
-- agent.global_agent_id. Every query here is owner-scoped: a global agent is
-- never readable or writable by anyone but its owner.

-- name: CreateGlobalAgent :one
INSERT INTO global_agent (
    owner_id, name, description, instructions, avatar_url, conversation_starters
) VALUES (
    @owner_id, @name, @description, @instructions, sqlc.narg('avatar_url'),
    COALESCE(sqlc.narg('conversation_starters')::jsonb, '[]'::jsonb)
)
RETURNING *;

-- name: GetGlobalAgentForOwner :one
SELECT * FROM global_agent
WHERE id = @id AND owner_id = @owner_id;

-- name: LockGlobalAgentForOwner :one
-- Serializes writes to one global agent. Every path that rewrites the linked
-- agent rows (edit, enable, make-global, delete) takes this lock first, so two
-- concurrent edits cannot leave workspaces holding different copies.
SELECT * FROM global_agent
WHERE id = @id AND owner_id = @owner_id
FOR UPDATE;

-- name: ListGlobalAgentsByOwner :many
SELECT * FROM global_agent
WHERE owner_id = @owner_id
ORDER BY created_at ASC;

-- name: UpdateGlobalAgent :one
UPDATE global_agent SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    conversation_starters = COALESCE(sqlc.narg('conversation_starters')::jsonb, conversation_starters),
    updated_at = now()
WHERE id = @id AND owner_id = @owner_id
RETURNING *;

-- name: DeleteGlobalAgent :exec
DELETE FROM global_agent
WHERE id = @id AND owner_id = @owner_id;

-- name: SyncLinkedAgentsFromGlobalAgent :many
-- Rewrites the synced fields on every workspace copy, archived ones included,
-- so a copy restored later comes back current. Runs in the transaction that
-- changed the global row. Only copies in workspaces the owner still belongs to
-- are reachable: leaving a workspace unlinks the copies there, and the
-- membership check keeps that true even for a copy that slipped through.
UPDATE agent SET
    name = @name,
    description = @description,
    instructions = @instructions,
    avatar_url = sqlc.narg('avatar_url'),
    conversation_starters = @conversation_starters::jsonb,
    updated_at = now()
WHERE agent.global_agent_id = @global_agent_id
  AND agent.kind = 'user'
  AND EXISTS (
      SELECT 1 FROM member m
      JOIN global_agent g ON g.id = agent.global_agent_id
      WHERE m.workspace_id = agent.workspace_id AND m.user_id = g.owner_id
  )
RETURNING *;

-- name: ListAgentsByGlobalAgent :many
SELECT * FROM agent
WHERE global_agent_id = @global_agent_id AND kind = 'user'
ORDER BY created_at ASC;

-- name: GetAgentByGlobalAgentAndWorkspace :one
SELECT * FROM agent
WHERE global_agent_id = @global_agent_id
  AND workspace_id = @workspace_id
  AND kind = 'user';

-- name: LinkAgentToGlobalAgent :one
-- Links only an unlinked agent: no row comes back when a concurrent request
-- linked it first.
UPDATE agent SET global_agent_id = @global_agent_id, updated_at = now()
WHERE id = @id AND kind = 'user' AND global_agent_id IS NULL
RETURNING *;

-- name: UnlinkGlobalAgentCopiesInWorkspace :many
-- A member leaving a workspace takes their global agents with them: the copies
-- there become regular workspace agents, so the owner's later edits no longer
-- reach a workspace they are not in, and its admins can manage the agents
-- again. Runs in the membership-removal transaction.
UPDATE agent SET global_agent_id = NULL, updated_at = now()
WHERE workspace_id = @workspace_id
  AND global_agent_id IN (SELECT g.id FROM global_agent g WHERE g.owner_id = @owner_id)
RETURNING *;

-- name: UnlinkAgentsFromGlobalAgent :many
-- Deleting a global agent keeps its workspace copies as regular agents.
UPDATE agent SET global_agent_id = NULL, updated_at = now()
WHERE global_agent_id = @global_agent_id
RETURNING *;

-- name: ListGlobalAgentLinksByOwner :many
-- One row per workspace copy of the owner's global agents (or of one of them
-- when global_agent_id is given), with the workspace's display identity for
-- the settings list. Workspaces the owner no longer belongs to are left out.
SELECT a.global_agent_id,
       a.id AS agent_id,
       a.workspace_id,
       a.archived_at,
       a.runtime_id,
       w.name AS workspace_name,
       w.slug AS workspace_slug
FROM agent a
JOIN global_agent g ON g.id = a.global_agent_id
JOIN workspace w ON w.id = a.workspace_id
JOIN member m ON m.workspace_id = a.workspace_id AND m.user_id = g.owner_id
WHERE g.owner_id = @owner_id
  AND a.kind = 'user'
  AND (sqlc.narg('global_agent_id')::uuid IS NULL OR g.id = sqlc.narg('global_agent_id')::uuid)
ORDER BY w.created_at ASC, a.created_at ASC;

-- name: FindAgentNameConflictForGlobalAgent :one
-- Names are unique per workspace (agent_workspace_name_unique, archived rows
-- included). Before renaming every copy, find a workspace where another agent
-- already holds the new name so the error can say which workspace blocks it.
SELECT w.name AS workspace_name
FROM agent a
JOIN workspace w ON w.id = a.workspace_id
WHERE a.name = @name
  AND a.workspace_id IN (
      SELECT linked.workspace_id FROM agent linked
      JOIN global_agent g ON g.id = linked.global_agent_id
      JOIN member m ON m.workspace_id = linked.workspace_id AND m.user_id = g.owner_id
      WHERE linked.global_agent_id = @global_agent_id
  )
  AND a.global_agent_id IS DISTINCT FROM @global_agent_id
LIMIT 1;

-- name: ListUsableRuntimesForUserInWorkspaces :many
-- The runtimes a user may see in each of several workspaces: their own and
-- the ones shared with the workspace. canUseRuntimeForAgent narrows this to
-- the ones they may bind agents to.
SELECT * FROM agent_runtime
WHERE workspace_id = ANY(@workspace_ids::uuid[])
  AND (owner_id = @owner_id OR visibility = 'public')
ORDER BY created_at ASC;

-- name: CreateAgentTag :one
INSERT INTO agent_tag (workspace_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetAgentTag :one
SELECT * FROM agent_tag
WHERE id = $1 AND workspace_id = $2;

-- name: GetAgentTagByName :one
SELECT * FROM agent_tag
WHERE workspace_id = $1 AND name = $2;

-- name: ListAgentTags :many
SELECT * FROM agent_tag
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: DeleteAgentTag :one
DELETE FROM agent_tag
WHERE id = $1 AND workspace_id = $2
RETURNING id;

-- name: AddTagToAgent :exec
-- Workspace-guarded: both the agent and tag must belong to the same workspace.
INSERT INTO agent_to_tag (agent_id, tag_id)
SELECT sqlc.arg('agent_id')::uuid, sqlc.arg('tag_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM agent a
    WHERE a.id = sqlc.arg('agent_id')::uuid
      AND a.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM agent_tag t
    WHERE t.id = sqlc.arg('tag_id')::uuid
      AND t.workspace_id = sqlc.arg('workspace_id')::uuid
)
ON CONFLICT DO NOTHING;

-- name: RemoveTagFromAgent :exec
DELETE FROM agent_to_tag
WHERE agent_id = sqlc.arg('agent_id')::uuid
  AND tag_id = sqlc.arg('tag_id')::uuid
  AND EXISTS (
      SELECT 1 FROM agent a
      WHERE a.id = sqlc.arg('agent_id')::uuid
        AND a.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: ListTagsByAgent :many
SELECT t.id, t.workspace_id, t.name, t.created_at
FROM agent_tag t
JOIN agent_to_tag att ON att.tag_id = t.id
WHERE att.agent_id = sqlc.arg('agent_id')::uuid
  AND t.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY t.name ASC;

-- name: ListAgentsByTag :many
-- Resolves @@ tag-scoped broadcast mentions to a list of agents.
-- Returns all non-archived workspace agents that hold the named tag.
SELECT a.*
FROM agent a
JOIN agent_to_tag att ON att.agent_id = a.id
JOIN agent_tag t ON t.id = att.tag_id
WHERE t.workspace_id = sqlc.arg('workspace_id')::uuid
  AND t.name = sqlc.arg('tag_name')::text
  AND a.archived_at IS NULL
ORDER BY a.created_at ASC;

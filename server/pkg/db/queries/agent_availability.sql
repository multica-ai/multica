-- Agent location availability. This is intentionally separate from
-- agent_invocation_target: availability says WHERE an agent may be used;
-- invocation targets retain backward-compatible WHO semantics.

-- name: ListAgentAvailableSpaces :many
SELECT * FROM agent_available_space
WHERE agent_id = $1
ORDER BY created_at ASC, space_id ASC;

-- name: ListAgentAvailableSpacesByAgentIDs :many
SELECT * FROM agent_available_space
WHERE agent_id = ANY(@agent_ids::uuid[])
ORDER BY agent_id ASC, created_at ASC, space_id ASC;

-- name: ReplaceAgentAvailableSpaces :exec
-- One SQL statement keeps a selected-Space replacement atomic: callers never
-- observe a partially rewritten location allow-list.
WITH deleted AS (
    DELETE FROM agent_available_space
    WHERE agent_id = @agent_id
      AND NOT (
          space_id = ANY(COALESCE(@space_ids::uuid[], ARRAY[]::uuid[]))
      )
)
INSERT INTO agent_available_space (
    agent_id, workspace_id, space_id, created_by
)
SELECT
    @agent_id, @workspace_id, selected.space_id, sqlc.narg('created_by')
FROM unnest(COALESCE(@space_ids::uuid[], ARRAY[]::uuid[])) AS selected(space_id)
ON CONFLICT (agent_id, space_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    created_by = EXCLUDED.created_by,
    created_at = now();

-- name: IsAgentAvailableInActiveSpace :one
SELECT EXISTS (
    SELECT 1
    FROM agent_available_space aas
    JOIN agent a
      ON a.workspace_id = aas.workspace_id
     AND a.id = aas.agent_id
    JOIN workspace_space ws
      ON ws.workspace_id = aas.workspace_id
     AND ws.id = aas.space_id
    WHERE aas.agent_id = $1
      AND aas.workspace_id = $2
      AND aas.space_id = $3
      AND ws.archived_at IS NULL
)::boolean;

-- name: ListActiveVisibleSpaceIDsForUser :many
-- Discovery follows CanViewWorkspaceSpace, not only explicit membership:
-- Open Spaces are discoverable to regular workspace members; Private Spaces
-- require membership or workspace owner/admin governance visibility.
SELECT ws.id
FROM workspace_space ws
JOIN member wm
  ON wm.workspace_id = ws.workspace_id
 AND wm.user_id = $2
LEFT JOIN workspace_space_member sm
  ON sm.space_id = ws.id
 AND sm.user_id = $2
WHERE ws.workspace_id = $1
  AND ws.archived_at IS NULL
  AND (ws.visibility = 'open' OR sm.user_id IS NOT NULL OR wm.role IN ('owner', 'admin'))
ORDER BY ws.id ASC;

-- name: ListAgentAllowedPrincipals :many
SELECT ap.id, ap.workspace_id, ap.agent_id, ap.principal_type, ap.principal_id,
       ap.created_by, ap.created_at, u.name AS user_name, u.email AS user_email,
       u.avatar_url AS user_avatar_url
FROM agent_allowed_principal ap
JOIN "user" u ON u.id = ap.principal_id
WHERE ap.agent_id = $1
ORDER BY ap.created_at ASC;

-- name: IsAgentAllowedPrincipal :one
SELECT EXISTS (
    SELECT 1
    FROM agent_allowed_principal
    WHERE agent_id = $1
      AND principal_type = 'member'
      AND principal_id = $2
) AS allowed;

-- name: ListAgentAllowedPrincipalIDsByWorkspace :many
SELECT agent_id, principal_id
FROM agent_allowed_principal
WHERE workspace_id = $1
  AND principal_type = 'member'
ORDER BY agent_id, principal_id;

-- name: ReplaceAgentAllowedPrincipals :exec
WITH deleted AS (
    DELETE FROM agent_allowed_principal
    WHERE agent_id = sqlc.arg('agent_id')::uuid
      AND principal_type = 'member'
)
INSERT INTO agent_allowed_principal (
    workspace_id, agent_id, principal_type, principal_id, created_by
)
SELECT sqlc.arg('workspace_id')::uuid,
       sqlc.arg('agent_id')::uuid,
       'member',
       unnest(sqlc.arg('principal_ids')::uuid[]),
       sqlc.arg('created_by')::uuid
ON CONFLICT (agent_id, principal_type, principal_id) DO NOTHING;

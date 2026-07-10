-- name: ListBindableIntegrationConnections :many
SELECT 'github'::text AS provider,
       gi.id AS connection_id,
       gi.account_login::text AS display_name,
       'active'::text AS status
FROM github_installation gi
WHERE gi.workspace_id = sqlc.arg('workspace_id')
UNION ALL
SELECT ci.channel_type::text AS provider,
       ci.id AS connection_id,
       COALESCE(
         NULLIF(ci.config->>'team_name', ''),
         NULLIF(ci.config->>'app_name', ''),
         NULLIF(ci.config->>'app_id', ''),
         ci.channel_type
       )::text AS display_name,
       ci.status::text AS status
FROM channel_installation ci
WHERE ci.workspace_id = sqlc.arg('workspace_id')
  AND ci.channel_type IN ('slack', 'feishu')
  AND ci.status = 'active'
ORDER BY provider, display_name, connection_id;

-- name: IntegrationConnectionExists :one
SELECT EXISTS (
  SELECT 1 FROM github_installation gi
  WHERE sqlc.arg('provider')::text = 'github'
    AND gi.id = sqlc.arg('connection_id')::uuid
    AND gi.workspace_id = sqlc.arg('workspace_id')::uuid
  UNION ALL
  SELECT 1 FROM channel_installation ci
  WHERE ci.channel_type = sqlc.arg('provider')::text
    AND ci.id = sqlc.arg('connection_id')::uuid
    AND ci.workspace_id = sqlc.arg('workspace_id')::uuid
    AND ci.status = 'active'
)::boolean;

-- name: ListIntegrationSpaceBindings :many
SELECT * FROM integration_space_binding
WHERE workspace_id = $1
ORDER BY provider, connection_id, created_at;

-- name: ListIntegrationBindingsForSpace :many
SELECT b.provider,
       b.connection_id,
       CASE
         WHEN b.provider = 'github' THEN gi.account_login
         ELSE COALESCE(
           NULLIF(ci.config->>'team_name', ''),
           NULLIF(ci.config->>'app_name', ''),
           NULLIF(ci.config->>'app_id', ''),
           b.provider
         )
       END::text AS display_name
FROM integration_space_binding b
LEFT JOIN github_installation gi
  ON b.provider = 'github' AND gi.id = b.connection_id AND gi.workspace_id = b.workspace_id
LEFT JOIN channel_installation ci
  ON b.provider = ci.channel_type
 AND ci.id = b.connection_id
 AND ci.workspace_id = b.workspace_id
 AND ci.status = 'active'
WHERE b.workspace_id = $1
  AND b.space_id = $2
  AND (gi.id IS NOT NULL OR ci.id IS NOT NULL)
ORDER BY b.provider, display_name;

-- name: DeleteIntegrationSpaceBindings :exec
DELETE FROM integration_space_binding
WHERE workspace_id = $1
  AND provider = $2
  AND connection_id = $3;

-- name: CreateIntegrationSpaceBinding :exec
INSERT INTO integration_space_binding (
  workspace_id, provider, connection_id, space_id, created_by
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT DO NOTHING;

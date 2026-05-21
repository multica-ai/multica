-- name: CreateWorkspaceIntegration :one
INSERT INTO workspace_integration (
    workspace_id, provider, enabled, config, default_agent_id, webhook_secret
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetWorkspaceIntegration :one
SELECT * FROM workspace_integration
WHERE id = $1;

-- name: GetWorkspaceIntegrationByProvider :one
SELECT * FROM workspace_integration
WHERE workspace_id = $1 AND provider = $2;

-- name: ListWorkspaceIntegrations :many
SELECT * FROM workspace_integration
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: UpdateWorkspaceIntegration :one
UPDATE workspace_integration SET
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    config = COALESCE(sqlc.narg('config'), config),
    default_agent_id = COALESCE(sqlc.narg('default_agent_id'), default_agent_id),
    webhook_secret = COALESCE(sqlc.narg('webhook_secret'), webhook_secret),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkspaceIntegration :exec
DELETE FROM workspace_integration
WHERE id = $1;

-- name: CreateExternalIssueLink :one
INSERT INTO external_issue_link (
    workspace_id, issue_id, provider, external_id,
    external_identifier, external_url, sync_direction
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetExternalIssueLinkByExternalID :one
SELECT * FROM external_issue_link
WHERE workspace_id = $1 AND provider = $2 AND external_id = $3;

-- name: GetExternalIssueLinkByIssueID :one
SELECT * FROM external_issue_link
WHERE issue_id = $1;

-- name: ListExternalIssueLinksByWorkspace :many
SELECT * FROM external_issue_link
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateExternalIssueLinkSyncedAt :exec
UPDATE external_issue_link SET last_synced_at = now()
WHERE id = $1;

-- name: DeleteExternalIssueLink :exec
DELETE FROM external_issue_link
WHERE id = $1;

-- name: ListWorkspaceIntegrationsByProvider :many
SELECT wi.* FROM workspace_integration wi
JOIN workspace w ON w.id = wi.workspace_id
WHERE wi.provider = $1 AND wi.enabled = true
ORDER BY wi.created_at ASC;

-- name: GetWorkspaceIntegration :one
SELECT * FROM workspace_integration
WHERE workspace_id = $1 AND provider = $2;

-- name: ListWorkspaceIntegrations :many
SELECT * FROM workspace_integration
WHERE workspace_id = $1
ORDER BY provider ASC;

-- name: UpsertWorkspaceIntegration :one
INSERT INTO workspace_integration (workspace_id, provider, instance_url, settings)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, provider) DO UPDATE SET
    instance_url = EXCLUDED.instance_url,
    settings     = EXCLUDED.settings,
    updated_at   = now()
RETURNING *;

-- name: DeleteWorkspaceIntegration :exec
DELETE FROM workspace_integration
WHERE workspace_id = $1 AND provider = $2;

-- name: GetUserIntegrationCredential :one
SELECT * FROM user_integration_credential
WHERE workspace_id = $1 AND user_id = $2 AND provider = $3;

-- name: UpsertUserIntegrationCredential :one
INSERT INTO user_integration_credential (workspace_id, user_id, provider, api_key)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, user_id, provider) DO UPDATE SET
    api_key    = EXCLUDED.api_key,
    updated_at = now()
RETURNING *;

-- name: DeleteUserIntegrationCredential :exec
DELETE FROM user_integration_credential
WHERE workspace_id = $1 AND user_id = $2 AND provider = $3;

-- name: GetProjectIntegrationLink :one
SELECT * FROM project_integration_link
WHERE workspace_id = $1 AND project_id = $2 AND provider = $3;

-- name: ListProjectIntegrationLinks :many
SELECT * FROM project_integration_link
WHERE workspace_id = $1 AND project_id = $2
ORDER BY provider ASC;

-- name: UpsertProjectIntegrationLink :one
INSERT INTO project_integration_link (workspace_id, project_id, provider, external_project_id, external_project_name)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, project_id, provider) DO UPDATE SET
    external_project_id   = EXCLUDED.external_project_id,
    external_project_name = EXCLUDED.external_project_name,
    updated_at            = now()
RETURNING *;

-- name: DeleteProjectIntegrationLink :exec
DELETE FROM project_integration_link
WHERE workspace_id = $1 AND project_id = $2 AND provider = $3;

-- name: GetIssueIntegrationLink :one
SELECT * FROM issue_integration_link
WHERE workspace_id = $1 AND issue_id = $2 AND provider = $3;

-- name: ListIssueIntegrationLinks :many
SELECT * FROM issue_integration_link
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY provider ASC;

-- name: UpsertIssueIntegrationLink :one
INSERT INTO issue_integration_link (workspace_id, issue_id, provider, external_issue_id, external_issue_url, external_issue_title)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, issue_id, provider) DO UPDATE SET
    external_issue_id    = EXCLUDED.external_issue_id,
    external_issue_url   = EXCLUDED.external_issue_url,
    external_issue_title = EXCLUDED.external_issue_title,
    updated_at           = now()
RETURNING *;

-- name: DeleteIssueIntegrationLink :exec
DELETE FROM issue_integration_link
WHERE workspace_id = $1 AND issue_id = $2 AND provider = $3;

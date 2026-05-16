-- name: GetFeishuProjectIntegration :one
SELECT * FROM feishu_project_integration
WHERE workspace_id = $1
ORDER BY updated_at DESC, created_at DESC
LIMIT 1;

-- name: GetFeishuProjectIntegrationByID :one
SELECT * FROM feishu_project_integration
WHERE id = $1;

-- name: ListEnabledFeishuProjectIntegrations :many
SELECT * FROM feishu_project_integration
WHERE enabled = true
ORDER BY updated_at ASC;

-- name: UpsertFeishuProjectIntegration :one
INSERT INTO feishu_project_integration (
    workspace_id, project_key, plugin_id, plugin_secret, actor_user_key,
    enabled, sync_story, sync_issue, mql_filter, status_mapping,
    reverse_status_mapping, created_by_id
) VALUES (
    $1, $2, $3, $4, sqlc.narg('actor_user_key'),
    $5, $6, $7, $8, $9, $10, sqlc.narg('created_by_id')
)
ON CONFLICT (workspace_id) DO UPDATE SET
    project_key = EXCLUDED.project_key,
    plugin_id = EXCLUDED.plugin_id,
    plugin_secret = EXCLUDED.plugin_secret,
    actor_user_key = EXCLUDED.actor_user_key,
    enabled = EXCLUDED.enabled,
    sync_story = EXCLUDED.sync_story,
    sync_issue = EXCLUDED.sync_issue,
    mql_filter = EXCLUDED.mql_filter,
    status_mapping = EXCLUDED.status_mapping,
    reverse_status_mapping = EXCLUDED.reverse_status_mapping,
    updated_at = now()
RETURNING *;

-- name: UpdateFeishuProjectIntegrationByID :one
UPDATE feishu_project_integration
SET project_key = $3,
    plugin_id = $4,
    plugin_secret = $5,
    actor_user_key = sqlc.narg('actor_user_key'),
    enabled = $6,
    sync_story = $7,
    sync_issue = $8,
    mql_filter = $9,
    status_mapping = $10,
    reverse_status_mapping = $11,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteFeishuProjectIntegration :exec
DELETE FROM feishu_project_integration
WHERE id = $1 AND workspace_id = $2;

-- name: MarkFeishuProjectIntegrationSynced :exec
UPDATE feishu_project_integration
SET last_synced_at = now(), last_error = NULL, updated_at = now()
WHERE id = $1;

-- name: MarkFeishuProjectIntegrationError :exec
UPDATE feishu_project_integration
SET last_error = $2, updated_at = now()
WHERE id = $1;

-- name: GetFeishuProjectIssueBindingByExternal :one
SELECT * FROM feishu_project_issue_binding
WHERE integration_id = $1 AND work_item_type = $2 AND work_item_id = $3;

-- name: GetFeishuProjectIssueBindingByIssue :one
SELECT * FROM feishu_project_issue_binding
WHERE workspace_id = $1 AND issue_id = $2;

-- name: UpsertFeishuProjectIssueBinding :one
INSERT INTO feishu_project_issue_binding (
    workspace_id, integration_id, issue_id, project_key, work_item_type,
    work_item_id, external_identifier, external_url, external_status_label,
    last_external_updated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, sqlc.narg('external_url'), sqlc.narg('external_status_label'),
    sqlc.narg('last_external_updated_at')
)
ON CONFLICT (integration_id, work_item_type, work_item_id) DO UPDATE SET
    issue_id = EXCLUDED.issue_id,
    external_url = EXCLUDED.external_url,
    external_status_label = EXCLUDED.external_status_label,
    last_external_updated_at = EXCLUDED.last_external_updated_at,
    last_synced_at = now(),
    updated_at = now()
RETURNING *;

-- name: CreateFeishuProjectSyncRun :one
INSERT INTO feishu_project_sync_run (
    integration_id, workspace_id, status, trigger
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: FinishFeishuProjectSyncRun :exec
UPDATE feishu_project_sync_run
SET status = $2,
    created_count = $3,
    updated_count = $4,
    skipped_count = $5,
    error_count = $6,
    error = sqlc.narg('error'),
    finished_at = now()
WHERE id = $1;

-- name: ListFeishuProjectSyncRuns :many
SELECT * FROM feishu_project_sync_run
WHERE integration_id = $1
ORDER BY started_at DESC
LIMIT $2;

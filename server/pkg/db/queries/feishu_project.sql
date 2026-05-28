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
    reverse_status_mapping, assign_open_items_to_owner_agent, created_by_id,
    business_line_field_key, business_line_field_name
) VALUES (
    $1, $2, $3, $4, sqlc.narg('actor_user_key'),
    $5, $6, $7, $8, $9, $10, $11, sqlc.narg('created_by_id'),
    $12, $13
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
    assign_open_items_to_owner_agent = EXCLUDED.assign_open_items_to_owner_agent,
    business_line_field_key = EXCLUDED.business_line_field_key,
    business_line_field_name = EXCLUDED.business_line_field_name,
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
    assign_open_items_to_owner_agent = $12,
    business_line_field_key = $13,
    business_line_field_name = $14,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteFeishuProjectIntegration :exec
DELETE FROM feishu_project_integration
WHERE id = $1 AND workspace_id = $2;

-- name: MarkFeishuProjectIntegrationSynced :exec
-- Advance the high-watermark to the larger of the previously-stored value and
-- the run's observed max(updated_at), so a no-op run can't drag the watermark
-- backwards. Caller passes the observed value as unix-millis; pass 0 (or
-- existing) when the run touched zero items.
UPDATE feishu_project_integration
SET last_synced_at = now(),
    last_seen_updated_at_ms = GREATEST(
        COALESCE(last_seen_updated_at_ms, 0),
        sqlc.arg('observed_updated_at_ms')::BIGINT
    ),
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: MarkFeishuProjectIntegrationReconciled :exec
-- Stamps a successful 6h reconcile run. The watermark advance runs through
-- MarkFeishuProjectIntegrationSynced (called in the same code path); this
-- query is purely the reconcile-cadence stamp.
UPDATE feishu_project_integration
SET last_reconciled_at = now(),
    updated_at = now()
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
    processed_count = $3::integer + $4::integer + $5::integer + $6::integer,
    error = sqlc.narg('error'),
    finished_at = now()
WHERE id = $1;

-- name: UpdateFeishuProjectSyncRunProgress :exec
UPDATE feishu_project_sync_run
SET created_count = $2,
    updated_count = $3,
    skipped_count = $4,
    error_count = $5,
    processed_count = $2::integer + $3::integer + $4::integer + $5::integer,
    total_count = GREATEST(total_count, $6),
    current_page = $7,
    current_type = $8
WHERE id = $1;

-- name: GetLatestFeishuProjectSyncRun :one
SELECT * FROM feishu_project_sync_run
WHERE integration_id = $1
ORDER BY started_at DESC
LIMIT 1;

-- name: GetLatestFeishuProjectManualSyncRun :one
SELECT * FROM feishu_project_sync_run
WHERE integration_id = $1 AND trigger = 'manual'
ORDER BY started_at DESC
LIMIT 1;

-- name: ListFeishuProjectSyncRuns :many
SELECT * FROM feishu_project_sync_run
WHERE integration_id = $1
ORDER BY started_at DESC
LIMIT $2;

-- name: ListFeishuProjectBusinessLineRoutes :many
SELECT * FROM feishu_project_business_line_route
WHERE integration_id = $1
ORDER BY business_line_name ASC, business_line_id ASC;

-- name: UpsertFeishuProjectBusinessLineRoute :one
INSERT INTO feishu_project_business_line_route (
    integration_id, workspace_id, project_id,
    business_line_id, business_line_name,
    parent_business_line_id, parent_business_line_name,
    fallback_agent_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, sqlc.narg('fallback_agent_id')
)
ON CONFLICT (integration_id, business_line_id) DO UPDATE SET
    project_id = EXCLUDED.project_id,
    business_line_name = EXCLUDED.business_line_name,
    parent_business_line_id = EXCLUDED.parent_business_line_id,
    parent_business_line_name = EXCLUDED.parent_business_line_name,
    fallback_agent_id = EXCLUDED.fallback_agent_id,
    updated_at = now()
RETURNING *;

-- name: DeleteFeishuProjectBusinessLineRoutesByIntegration :exec
DELETE FROM feishu_project_business_line_route
WHERE integration_id = $1;

-- name: DeleteFeishuProjectBusinessLineRoute :exec
DELETE FROM feishu_project_business_line_route
WHERE integration_id = $1 AND business_line_id = $2;

-- name: ListFeishuProjectAttachmentBindingsByIssue :many
SELECT * FROM feishu_project_attachment_binding
WHERE integration_id = $1 AND issue_id = $2;

-- name: CreateFeishuProjectAttachmentBinding :one
INSERT INTO feishu_project_attachment_binding (
    workspace_id, integration_id, issue_id, attachment_id,
    external_attachment_id, external_filename
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (integration_id, external_attachment_id) DO UPDATE SET
    attachment_id = EXCLUDED.attachment_id,
    external_filename = EXCLUDED.external_filename
RETURNING *;
